// Copyright © 2024 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rtc

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	livekit "github.com/livekit/protocol/livekit"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/rtc"
	"github.com/openimsdk/tools/log"
)

// watchdogSuspects tracks answered invitations first observed with an empty
// LiveKit room and no lingering Redis call-status, keyed by roomID, so a call
// is only force-ended after being confirmed stale across a grace period
// rather than on a single observation (avoids tearing down a call during a
// brief reconnect). It is shared and mutex-protected because both the
// periodic scan goroutine and the optional NotifyRoomEvent webhook fast-path
// (called from gRPC handler goroutines) read and update it. In-memory only:
// losing leadership or restarting simply resets the grace timer, never
// causing incorrect early teardown.
type watchdogSuspects struct {
	mu        sync.Mutex
	firstSeen map[string]time.Time
}

func newWatchdogSuspects() *watchdogSuspects {
	return &watchdogSuspects{firstSeen: make(map[string]time.Time)}
}

// observe records roomID as suspect on first call and reports how long it has
// been continuously suspect since. clear(roomID) resets tracking.
func (w *watchdogSuspects) observe(roomID string) time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	first, ok := w.firstSeen[roomID]
	if !ok {
		w.firstSeen[roomID] = time.Now()
		return 0
	}
	return time.Since(first)
}

func (w *watchdogSuspects) clear(roomID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.firstSeen, roomID)
}

// retainOnly drops tracked rooms not present in keep, bounding memory usage
// after invitations end through a normal (non-watchdog) path.
func (w *watchdogSuspects) retainOnly(keep map[string]struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for roomID := range w.firstSeen {
		if _, ok := keep[roomID]; !ok {
			delete(w.firstSeen, roomID)
		}
	}
}

// startCallWatchdog launches the background scan that force-ends calls the
// client-driven signaling flow failed to clean up, e.g.:
//   - an unanswered invitation whose ring timeout elapsed because the caller's
//     app crashed before it could send SignalTimeout;
//   - an answered call whose LiveKit room emptied out (both peers dropped or
//     crashed) without either side sending SignalHungUp.
//
// It never re-implements teardown: once a stale invitation is identified, it
// synthesizes the same request types handleTimeout / handleHungUp already
// handle for real client traffic, so notifications, idempotent Mongo deletes,
// Redis cleanup, and LiveKit room teardown all go through the exact same,
// already-tested code paths.
//
// Multiple rtc replicas may run this loop concurrently; a Redis-backed
// leader lock (s.watchdogLock) ensures only one replica scans at a time.
func (s *rtcServer) startCallWatchdog(ctx context.Context) {
	cfg := s.config.RpcConfig.Watchdog
	if !cfg.Enabled {
		log.ZInfo(ctx, "call watchdog disabled")
		return
	}
	if s.watchdogLock == nil {
		log.ZWarn(ctx, "call watchdog enabled but no lock configured, skipping", nil)
		return
	}

	scanInterval := time.Duration(cfg.ScanIntervalSeconds) * time.Second
	if scanInterval <= 0 {
		scanInterval = 15 * time.Second
	}
	lockTTL := time.Duration(cfg.LockTTLSeconds) * time.Second
	if lockTTL <= scanInterval {
		lockTTL = scanInterval * 2
	}
	s.watchdogZombieGrace = time.Duration(cfg.ZombieGraceSeconds) * time.Second
	if s.watchdogZombieGrace <= 0 {
		s.watchdogZombieGrace = 45 * time.Second
	}
	s.watchdogSuspectState = newWatchdogSuspects()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.ZError(ctx, "call watchdog goroutine panicked", nil, "recover", r)
			}
		}()

		token := uuid.NewString()
		isLeader := false

		ticker := time.NewTicker(scanInterval)
		defer ticker.Stop()

		log.ZInfo(ctx, "call watchdog started", "scanInterval", scanInterval, "lockTTL", lockTTL, "zombieGrace", s.watchdogZombieGrace)

		for {
			select {
			case <-ctx.Done():
				if isLeader {
					_ = s.watchdogLock.Release(context.Background(), token)
				}
				return
			case <-ticker.C:
				var acquired bool
				var err error
				if isLeader {
					acquired, err = s.watchdogLock.Renew(ctx, token, lockTTL)
				} else {
					acquired, err = s.watchdogLock.TryAcquire(ctx, token, lockTTL)
				}
				if err != nil {
					log.ZWarn(ctx, "call watchdog lock operation failed", err, "wasLeader", isLeader)
					isLeader = false
					continue
				}
				if !acquired {
					if isLeader {
						log.ZWarn(ctx, "call watchdog lost leadership", nil)
					}
					isLeader = false
					continue
				}
				if !isLeader {
					log.ZInfo(ctx, "call watchdog acquired leader lock", "token", token)
				}
				isLeader = true
				s.watchdogTick(ctx)
			}
		}
	}()
}

// watchdogTick runs a single scan pass. It is only invoked while the caller
// holds the leader lock.
func (s *rtcServer) watchdogTick(ctx context.Context) {
	invitations, err := s.db.ListAllInvitations(ctx, 5000)
	if err != nil {
		log.ZWarn(ctx, "call watchdog: ListAllInvitations failed", err)
		return
	}

	seenRoomIDs := make(map[string]struct{}, len(invitations))
	for _, inv := range invitations {
		if inv == nil || inv.RoomID == "" {
			continue
		}
		seenRoomIDs[inv.RoomID] = struct{}{}
		s.watchdogCheckInvitation(ctx, inv)
	}

	s.watchdogSuspectState.retainOnly(seenRoomIDs)
}

// watchdogCheckInvitation classifies a single invitation as ringing-timeout or
// answered-call and dispatches to the matching check. Shared by the periodic
// scan and the optional NotifyRoomEvent webhook fast-path.
func (s *rtcServer) watchdogCheckInvitation(ctx context.Context, inv *model.SignalInvitation) {
	if inv.AcceptTime <= 0 {
		s.watchdogCheckRingingTimeout(ctx, inv)
		return
	}
	s.watchdogCheckZombieInCall(ctx, inv)
}

// watchdogCheckRingingTimeout force-ends an unanswered invitation once its
// ring deadline has passed, covering the case where the caller's app crashed
// before it could report the timeout itself. isInvitationPending already
// encodes the deadline check used everywhere else in signaling, so the
// watchdog reuses it rather than re-deriving the same condition.
func (s *rtcServer) watchdogCheckRingingTimeout(ctx context.Context, inv *model.SignalInvitation) {
	s.watchdogSuspectState.clear(inv.RoomID)
	if s.isInvitationPending(ctx, inv) {
		return
	}

	log.ZInfo(ctx, "call watchdog: force-ending expired ringing invitation", "roomID", inv.RoomID, "inviterUserID", inv.InviterUserID)
	req := &rtc.SignalTimeoutReq{
		Invitation: &rtc.InvitationInfo{RoomID: inv.RoomID},
		UserID:     inv.InviterUserID,
	}
	signalReq := &rtc.SignalReq{Payload: &rtc.SignalReq_Timeout{Timeout: req}}
	if _, err := s.handleTimeout(ctx, req, signalReq); err != nil {
		log.ZWarn(ctx, "call watchdog: handleTimeout failed", err, "roomID", inv.RoomID)
	}
}

// watchdogCheckZombieInCall force-ends an answered call whose LiveKit room has
// been confirmed empty (or gone) for at least the configured grace period,
// with no participant call-status left in Redis either. Both signals must
// agree so a brief reconnect blip (room momentarily empty while Redis status
// is still warm, or vice versa) never triggers a false teardown of a live call.
func (s *rtcServer) watchdogCheckZombieInCall(ctx context.Context, inv *model.SignalInvitation) {
	empty, err := s.isLiveKitRoomEmptyOrGone(ctx, inv.RoomID)
	if err != nil {
		// Transient LiveKit error: don't touch the suspect timer either way.
		return
	}
	if !empty || s.hasParticipantCallStatusForRoom(ctx, inv) {
		s.watchdogSuspectState.clear(inv.RoomID)
		return
	}

	grace := s.watchdogZombieGrace
	if grace <= 0 {
		grace = 45 * time.Second
	}
	if s.watchdogSuspectState.observe(inv.RoomID) < grace {
		return
	}

	log.ZInfo(ctx, "call watchdog: force-ending zombie in-call invitation", "roomID", inv.RoomID, "inviterUserID", inv.InviterUserID, "groupID", inv.GroupID)
	s.watchdogSuspectState.clear(inv.RoomID)
	req := &rtc.SignalHungUpReq{
		Invitation: &rtc.InvitationInfo{RoomID: inv.RoomID},
		UserID:     inv.InviterUserID,
	}
	signalReq := &rtc.SignalReq{Payload: &rtc.SignalReq_HungUp{HungUp: req}}
	if _, err := s.handleHungUp(ctx, req, signalReq); err != nil {
		log.ZWarn(ctx, "call watchdog: force handleHungUp failed", err, "roomID", inv.RoomID)
	}
}

// NotifyRoomEvent is an internal-only fast-path used by the optional LiveKit
// webhook receiver (internal/api/rtc.go) to trigger an immediate re-check of a
// single room instead of waiting for the next periodic scan tick. It shares
// the same suspects grace-period state and classification helpers as the
// periodic scan, so it can only accelerate detection (e.g. confirm a zombie
// one scan cycle earlier) and never bypasses the anti-flap grace period. It is
// a no-op if the watchdog is disabled or the room has no active invitation,
// and safe to call from any replica without holding the leader lock.
func (s *rtcServer) NotifyRoomEvent(ctx context.Context, req *rtc.NotifyRoomEventReq) (*rtc.NotifyRoomEventResp, error) {
	log.ZDebug(ctx, "NotifyRoomEvent", "req", req)
	if !s.config.RpcConfig.Watchdog.Enabled || !s.config.RpcConfig.Watchdog.WebhookEnabled || s.watchdogSuspectState == nil {
		return &rtc.NotifyRoomEventResp{}, nil
	}
	if req.GetRoomID() == "" {
		return &rtc.NotifyRoomEventResp{}, nil
	}

	inv, err := s.db.GetInvitationByRoomID(ctx, req.GetRoomID())
	if err != nil {
		// No active invitation for this room (already cleaned up, or never
		// tracked) — nothing for the watchdog to do.
		return &rtc.NotifyRoomEventResp{}, nil
	}

	s.watchdogCheckInvitation(ctx, inv)
	return &rtc.NotifyRoomEventResp{}, nil
}

// isLiveKitRoomEmptyOrGone reports whether roomID currently has zero LiveKit
// participants or no longer exists. It intentionally skips the participant
// metadata / user-info enrichment done by livekitRoomParticipantsMeta since
// the watchdog only needs the participant count, and this runs on every
// active invitation each scan tick.
func (s *rtcServer) isLiveKitRoomEmptyOrGone(ctx context.Context, roomID string) (bool, error) {
	if roomID == "" {
		return true, nil
	}
	lp, err := s.roomClient.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: roomID})
	if err != nil {
		if isLiveKitRoomGone(err) {
			return true, nil
		}
		log.ZWarn(ctx, "call watchdog: LiveKit ListParticipants failed", err, "roomID", roomID)
		return false, err
	}
	return len(lp.Participants) == 0, nil
}
