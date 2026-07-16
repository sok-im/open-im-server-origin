// Copyright © 2026 OpenIM. All rights reserved.
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

package openmls

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/constant"
	pbopenmls "github.com/openimsdk/protocol/openmls"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
)

// credentialPayload 是 MLS Credential 的内容结构（JSON 序列化后再签名）。
type credentialPayload struct {
	Identity   string `json:"identity"`
	LeafPubKey string `json:"leaf_pub_key"`
	IssuedAt   int64  `json:"issued_at"`
	ExpiresAt  int64  `json:"expires_at"`
	Issuer     string `json:"issuer"`
}

// credentialEnvelope 是编码后存储的 Credential 外层结构。
type credentialEnvelope struct {
	Payload string `json:"payload"` // base64(JSON credentialPayload)
	Sig     string `json:"sig"`     // base64(Ed25519 signature)
}

// ==================== KeyPackage ====================

func (s *openMLSServer) UploadKeyPackage(ctx context.Context, req *pbopenmls.UploadKeyPackageReq) (*pbopenmls.UploadKeyPackageResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}
	if req.UserID == "" || req.DeviceID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID and deviceID are required")
	}
	if req.UserID != opUserID {
		return nil, errs.ErrNoPermission.WrapMsg("userID mismatch with token")
	}
	if req.KeyPackage == "" {
		return nil, errs.ErrArgs.WrapMsg("keyPackage is required")
	}
	kpBytes, err := base64.StdEncoding.DecodeString(req.KeyPackage)
	if err != nil {
		return nil, errs.ErrArgs.WrapMsg("keyPackage must be valid base64")
	}
	// Verify the credential embedded in the KeyPackage was issued by this server
	// and is bound to this user/device (no-op when credential issuing is disabled).
	meta, err := s.verifyKeyPackageCredential(ctx, kpBytes, req.UserID, req.DeviceID)
	if err != nil {
		return nil, err
	}
	// If credential verification is active, the platform field is authoritative;
	// otherwise fall back to whatever the client sent.
	if meta.Platform != "" {
		req.Platform = meta.Platform
	}

	// Check per-device limit
	maxKP := s.config.RpcConfig.MaxKeyPackagesPerDevice
	if maxKP <= 0 {
		maxKP = 20
	}
	count, err := s.db.CountByDevice(ctx, req.UserID, req.DeviceID)
	if err != nil {
		return nil, err
	}
	if int(count) >= maxKP {
		return nil, errs.New("max KeyPackage limit reached").Wrap()
	}

	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if req.ExpiresAt > 0 {
		expiresAt = time.Unix(req.ExpiresAt, 0)
	}

	kp := &model.MLSKeyPackage{
		KpID:           uuid.New().String(),
		UserID:         req.UserID,
		DeviceID:       req.DeviceID,
		Platform:       req.Platform,
		KeyPackage:     req.KeyPackage,
		Ciphersuite:    req.Ciphersuite,
		CredentialType: req.CredentialType,
		Consumed:       false,
		CreatedAt:      time.Now(),
		ExpiresAt:      expiresAt,
	}
	if err := s.db.Insert(ctx, kp); err != nil {
		return nil, err
	}

	total, err := s.db.CountByDevice(ctx, req.UserID, req.DeviceID)
	if err != nil {
		return nil, err
	}

	return &pbopenmls.UploadKeyPackageResp{
		KpID:       kp.KpID,
		TotalCount: total,
	}, nil
}

func (s *openMLSServer) GetKeyPackages(ctx context.Context, req *pbopenmls.GetKeyPackagesReq) (*pbopenmls.GetKeyPackagesResp, error) {
	if req.UserID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID is required")
	}
	// Require an authenticated caller; anonymous KP consumption is forbidden.
	if mcontext.GetOpUserID(ctx) == "" {
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}

	countPerDevice := int(req.CountPerDevice)
	if countPerDevice <= 0 {
		countPerDevice = 1
	}

	kps, err := s.db.ConsumeByUserID(ctx, req.UserID, req.ExcludeDeviceID, countPerDevice)
	if err != nil {
		return nil, err
	}

	items := make([]*pbopenmls.KeyPackageItem, len(kps))
	for i, kp := range kps {
		items[i] = &pbopenmls.KeyPackageItem{
			KpID:       kp.KpID,
			DeviceID:   kp.DeviceID,
			Platform:   kp.Platform,
			KeyPackage: kp.KeyPackage,
			Consumed:   kp.Consumed,
		}
	}

	return &pbopenmls.GetKeyPackagesResp{
		UserID:      req.UserID,
		KeyPackages: items,
	}, nil
}

func (s *openMLSServer) GetKeyPackageCount(ctx context.Context, req *pbopenmls.GetKeyPackageCountReq) (*pbopenmls.GetKeyPackageCountResp, error) {
	if req.UserID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID is required")
	}

	counts, err := s.db.CountByUserID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	var total int32
	devices := make([]*pbopenmls.DeviceCount, 0, len(counts))
	for deviceID, cnt := range counts {
		devices = append(devices, &pbopenmls.DeviceCount{
			DeviceID:       deviceID,
			AvailableCount: cnt,
		})
		total += cnt
	}

	return &pbopenmls.GetKeyPackageCountResp{
		UserID:         req.UserID,
		Devices:        devices,
		TotalAvailable: total,
	}, nil
}

func (s *openMLSServer) RefreshKeyPackages(ctx context.Context, req *pbopenmls.RefreshKeyPackagesReq) (*pbopenmls.RefreshKeyPackagesResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}
	if req.UserID != opUserID {
		return nil, errs.ErrNoPermission.WrapMsg("userID mismatch with token")
	}
	if req.UserID == "" || req.DeviceID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID and deviceID are required")
	}
	if len(req.KeyPackages) == 0 {
		return nil, errs.ErrArgs.WrapMsg("keyPackages must not be empty")
	}

	maxKP := s.config.RpcConfig.MaxKeyPackagesPerDevice
	if maxKP <= 0 {
		maxKP = 20
	}

	existing, err := s.db.CountByDevice(ctx, req.UserID, req.DeviceID)
	if err != nil {
		return nil, err
	}
	remaining := maxKP - int(existing)
	if remaining <= 0 {
		return nil, errs.New("max KeyPackage limit reached").Wrap()
	}
	toInsert := req.KeyPackages
	if len(toInsert) > remaining {
		toInsert = toInsert[:remaining]
	}

	kps := make([]*model.MLSKeyPackage, len(toInsert))
	now := time.Now()
	expiresAt := now.Add(30 * 24 * time.Hour)
	for i, kpB64 := range toInsert {
		kpBytes, err := base64.StdEncoding.DecodeString(kpB64)
		if err != nil {
			return nil, errs.ErrArgs.WrapMsg("keyPackages[" + strconv.Itoa(i) + "] must be valid base64")
		}
		meta, err := s.verifyKeyPackageCredential(ctx, kpBytes, req.UserID, req.DeviceID)
		if err != nil {
			return nil, err
		}
		kps[i] = &model.MLSKeyPackage{
			KpID:        uuid.New().String(),
			UserID:      req.UserID,
			DeviceID:    req.DeviceID,
			Platform:    meta.Platform,
			Ciphersuite: meta.Ciphersuite,
			KeyPackage:  kpB64,
			Consumed:    false,
			CreatedAt:   now,
			ExpiresAt:   expiresAt,
		}
	}

	if err := s.db.BatchInsert(ctx, kps); err != nil {
		return nil, err
	}

	total, err := s.db.CountByDevice(ctx, req.UserID, req.DeviceID)
	if err != nil {
		return nil, err
	}

	return &pbopenmls.RefreshKeyPackagesResp{
		UploadedCount: int32(len(kps)),
		TotalCount:    total,
	}, nil
}

// ==================== Group Commit ====================

func (s *openMLSServer) SubmitCommit(ctx context.Context, req *pbopenmls.SubmitCommitReq) (*pbopenmls.SubmitCommitResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}
	if req.SenderUserID == "" || req.SenderDeviceID == "" {
		return nil, errs.ErrArgs.WrapMsg("senderUserID and senderDeviceID are required")
	}
	if req.CommitMessage == "" {
		return nil, errs.ErrArgs.WrapMsg("commitMessage is required")
	}
	if _, err := base64.StdEncoding.DecodeString(req.CommitMessage); err != nil {
		return nil, errs.ErrArgs.WrapMsg("commitMessage must be valid base64")
	}
	// The commit sender must be the authenticated user (or an IM admin); a token
	// holder must not be able to forge commits on behalf of another user.
	if err := authverify.CheckAccessV3(ctx, req.SenderUserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	if req.IdempotencyKey != "" {
		if prev, err := s.db.FindByIdempotencyKey(ctx, req.IdempotencyKey); err == nil && prev != nil {
			log.ZInfo(ctx, "SubmitCommit: duplicate idempotencyKey",
				"groupID", req.GroupID, "idempotencyKey", req.IdempotencyKey,
				"acceptedEpoch", prev.Epoch, "commitID", prev.ID, "senderUserID", req.SenderUserID)
			return &pbopenmls.SubmitCommitResp{
				Accepted:       true,
				Duplicate:      true,
				AcceptedEpoch:  prev.Epoch,
				CommitID:       prev.ID,
				NewEpoch:       prev.Epoch,
				SequenceNumber: prev.SequenceNumber,
			}, nil
		} else if err != nil && !errs.ErrRecordNotFound.Is(err) {
			return nil, err
		}
	}

	// Ensure group state exists (create if first commit)
	_, err := s.db.GetState(ctx, req.GroupID)
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			if err := s.db.UpsertState(ctx, &model.MLSGroupState{
				GroupID:      req.GroupID,
				CurrentEpoch: 0,
				MemberCount:  1,
				CreatedAt:    time.Now(),
				LastCommitAt: time.Now(),
			}); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	// Optimistic concurrency: increment epoch
	newEpoch, err := s.db.IncrEpoch(ctx, req.GroupID, req.FromEpoch)
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			st, getErr := s.db.GetState(ctx, req.GroupID)
			var expected uint64
			if getErr == nil && st != nil {
				expected = st.CurrentEpoch
			}
			log.ZWarn(ctx, "SubmitCommit: epoch conflict", err,
				"groupID", req.GroupID, "fromEpoch", req.FromEpoch,
				"expectedFromEpoch", expected, "senderUserID", req.SenderUserID,
				"idempotencyKey", req.IdempotencyKey)
			return &pbopenmls.SubmitCommitResp{
				Accepted:          false,
				ExpectedFromEpoch: expected,
			}, servererrs.ErrMLSEpochConflict.WrapMsg(fmt.Sprintf("epoch conflict expectedFromEpoch=%d", expected))
		}
		return nil, err
	}

	// Persist commit record.
	// Use the epoch as the sequence number — it is already strictly monotonic per
	// group and avoids the non-monotonic behaviour of wall-clock UnixMilli().
	seqNum := int64(newEpoch)
	commit := &model.MLSCommit{
		ID:             uuid.New().String(),
		GroupID:        req.GroupID,
		Epoch:          newEpoch,
		FromEpoch:      req.FromEpoch,
		CommitHash:     req.CommitHash,
		IdempotencyKey: req.IdempotencyKey,
		SequenceNumber: seqNum,
		CommitMessage:  req.CommitMessage,
		SenderUserID:   req.SenderUserID,
		SenderDeviceID: req.SenderDeviceID,
		CreatedAt:      time.Now(),
	}
	if err := s.db.AppendCommit(ctx, commit); err != nil {
		return nil, err
	}
	log.ZInfo(ctx, "SubmitCommit: accepted",
		"groupID", req.GroupID, "fromEpoch", req.FromEpoch, "acceptedEpoch", newEpoch,
		"commitID", commit.ID, "commitHash", req.CommitHash, "senderUserID", req.SenderUserID,
		"idempotencyKey", req.IdempotencyKey)

	// ---------- Broadcast the commit to all current group members ----------
	//
	// Strategy: use the OpenIM group service to resolve member IDs, then send a
	// single ReadGroupChatType CustomMessage so the msg pipeline fans it out to
	// every member via the existing push infrastructure. If the MLS group_id
	// does not correspond to a real OpenIM group (e.g. 1:1 sessions), the
	// lookup returns nothing and we skip the broadcast — clients fall back to
	// polling GetCommits.
	var broadcastCount int32
	memberIDs, err := s.groupClient.GetGroupMemberUserIDs(ctx, req.GroupID)
	if err != nil {
		log.ZWarn(ctx, "SubmitCommit: GetGroupMemberUserIDs failed, skipping commit broadcast",
			err, "groupID", req.GroupID)
	} else if len(memberIDs) > 0 {
		// Sync the authoritative member count into the MLS group state so that
		// GetGroupState returns a useful value.
		if err := s.db.UpdateMemberCount(ctx, req.GroupID, int32(len(memberIDs))); err != nil {
			log.ZWarn(ctx, "SubmitCommit: UpdateMemberCount failed", err,
				"groupID", req.GroupID, "memberCount", len(memberIDs))
		}
		// One group-channel send; the msg service fans out to all members.
		if err := s.sendMLSMsg(ctx,
			req.SenderUserID,
			req.GroupID,
			req.GroupID,
			constant.ReadGroupChatType,
			req.CommitMessage,
			"[MLS Commit]",
		); err != nil {
			log.ZWarn(ctx, "SubmitCommit: commit broadcast failed", err,
				"groupID", req.GroupID, "epoch", newEpoch)
		} else {
			broadcastCount = int32(len(memberIDs))
		}
	}

	// 1:1 MLS sessions use si_/c_1v1_ group IDs that do not map to OpenIM
	// groups, so the group broadcast above is skipped. Deliver the Commit
	// point-to-point so the peer can advance epoch without polling GetCommits.
	if broadcastCount == 0 {
		for _, peerID := range singleChatPeerUserIDs(req.GroupID, req.SenderUserID) {
			if err := s.sendMLSMsg(ctx,
				req.SenderUserID,
				peerID,
				"",
				constant.SingleChatType,
				req.CommitMessage,
				"[MLS Commit]",
			); err != nil {
				log.ZWarn(ctx, "SubmitCommit: 1:1 commit delivery failed", err,
					"groupID", req.GroupID, "peerID", peerID, "epoch", newEpoch)
			} else {
				broadcastCount++
			}
		}
	}

	// ---------- Deliver Welcome to newly added members (if any) ----------
	//
	// welcomeMessages carries per-device Welcome blobs for members being added
	// in this Commit. Deliver each point-to-point so the new member can
	// initialise their MLS group state without being in the group yet.
	for _, w := range req.WelcomeMessages {
		if w.RecipientUserID == "" || w.WelcomeMessage == "" {
			continue
		}
		if err := s.sendMLSMsg(ctx,
			req.SenderUserID,
			w.RecipientUserID,
			"",
			constant.SingleChatType,
			w.WelcomeMessage,
			"[MLS Welcome]",
		); err != nil {
			log.ZWarn(ctx, "SubmitCommit: welcome delivery failed", err,
				"groupID", req.GroupID,
				"recipientUserID", w.RecipientUserID,
				"recipientDeviceID", w.RecipientDeviceID,
			)
		}
	}

	return &pbopenmls.SubmitCommitResp{
		NewEpoch:       newEpoch,
		SequenceNumber: seqNum,
		BroadcastCount: broadcastCount,
		Accepted:       true,
		Duplicate:      false,
		AcceptedEpoch:  newEpoch,
		CommitID:       commit.ID,
	}, nil
}

func (s *openMLSServer) GetCommits(ctx context.Context, req *pbopenmls.GetCommitsReq) (*pbopenmls.GetCommitsResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
	}

	commits, err := s.db.FindSinceEpoch(ctx, req.GroupID, req.SinceEpoch, limit)
	if err != nil {
		return nil, err
	}

	state, err := s.db.GetState(ctx, req.GroupID)
	if err != nil && !errs.ErrRecordNotFound.Is(err) {
		return nil, err
	}
	var currentEpoch uint64
	if state != nil {
		currentEpoch = state.CurrentEpoch
	}

	records := make([]*pbopenmls.CommitRecord, len(commits))
	for i, c := range commits {
		records[i] = &pbopenmls.CommitRecord{
			Epoch:          c.Epoch,
			SequenceNumber: c.SequenceNumber,
			CommitMessage:  c.CommitMessage,
			SenderUserID:   c.SenderUserID,
			CreatedAt:      c.CreatedAt.Unix(),
		}
	}

	hasMore := len(commits) == limit
	return &pbopenmls.GetCommitsResp{
		GroupID:      req.GroupID,
		Commits:      records,
		CurrentEpoch: currentEpoch,
		HasMore:      hasMore,
	}, nil
}

func (s *openMLSServer) SendWelcome(ctx context.Context, req *pbopenmls.SendWelcomeReq) (*pbopenmls.SendWelcomeResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}
	if len(req.Recipients) == 0 {
		return nil, errs.ErrArgs.WrapMsg("recipients must not be empty")
	}
	if req.SenderUserID == "" {
		return nil, errs.ErrArgs.WrapMsg("senderUserID is required")
	}
	// Only the authenticated user (or an IM admin) may send Welcome messages on
	// behalf of a given sender identity.
	if err := authverify.CheckAccessV3(ctx, req.SenderUserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	var delivered int32
	for _, r := range req.Recipients {
		if r.RecipientUserID == "" || r.WelcomeMessage == "" {
			continue
		}
		err := s.sendMLSMsg(ctx,
			req.SenderUserID,
			r.RecipientUserID,
			"",                            // Welcome is point-to-point; no OpenIM group yet
			constant.SingleChatType,
			r.WelcomeMessage,
			"[MLS Welcome]",
		)
		if err != nil {
			// Best-effort: log and continue so partial delivery is possible.
			log.ZWarn(ctx, "SendWelcome: failed to deliver to recipient", err,
				"groupID", req.GroupID,
				"recipientUserID", r.RecipientUserID,
				"recipientDeviceID", r.RecipientDeviceID,
			)
			continue
		}
		delivered++
	}

	return &pbopenmls.SendWelcomeResp{
		DeliveredCount: delivered,
	}, nil
}

func (s *openMLSServer) GetGroupState(ctx context.Context, req *pbopenmls.GetGroupStateReq) (*pbopenmls.GetGroupStateResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}

	state, err := s.db.GetState(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	return &pbopenmls.GetGroupStateResp{
		GroupID:      state.GroupID,
		CurrentEpoch: state.CurrentEpoch,
		MemberCount:  state.MemberCount,
		LastCommitAt: state.LastCommitAt.Unix(),
		CreatedAt:    state.CreatedAt.Unix(),
	}, nil
}

func (s *openMLSServer) DeleteGroup(ctx context.Context, req *pbopenmls.DeleteGroupReq) (*pbopenmls.DeleteGroupResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}
	// Group dissolution is a privileged, server-side triggered operation; only IM
	// admins may purge a group's MLS state and commit history.
	if err := authverify.CheckAdmin(ctx, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// DeleteGroupAll wraps both the mls_group_state and mls_commit deletions in
	// a single MongoDB transaction so that a partial failure cannot leave orphan
	// commit records.
	if err := s.db.DeleteGroupAll(ctx, req.GroupID); err != nil {
		return nil, err
	}

	return &pbopenmls.DeleteGroupResp{}, nil
}

// ==================== Credential ====================

func (s *openMLSServer) IssueCredential(ctx context.Context, req *pbopenmls.IssueCredentialReq) (*pbopenmls.IssueCredentialResp, error) {
	if s.signingKey == nil {
		return nil, errs.New("credential issuing is not configured (signingKey is empty)").Wrap()
	}

	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}
	if req.UserID != opUserID {
		return nil, errs.ErrNoPermission.WrapMsg("userID mismatch with token")
	}
	if req.UserID == "" || req.DeviceID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID and deviceID are required")
	}
	if req.LeafPublicKey == "" {
		return nil, errs.ErrArgs.WrapMsg("leafPublicKey is required")
	}
	if _, err := base64.StdEncoding.DecodeString(req.LeafPublicKey); err != nil {
		return nil, errs.ErrArgs.WrapMsg("leafPublicKey must be valid base64")
	}

	now := time.Now()
	expiresAt := now.Add(30 * 24 * time.Hour)

	identity := req.UserID + ":" + req.DeviceID + ":" + req.Platform
	payload := credentialPayload{
		Identity:   identity,
		LeafPubKey: req.LeafPublicKey,
		IssuedAt:   now.Unix(),
		ExpiresAt:  expiresAt.Unix(),
		Issuer:     s.config.RpcConfig.SigningKey.Issuer,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, errs.New("marshal credential payload").Wrap()
	}

	sig := ed25519.Sign(s.signingKey, payloadBytes)
	envelope := credentialEnvelope{
		Payload: base64.StdEncoding.EncodeToString(payloadBytes),
		Sig:     base64.StdEncoding.EncodeToString(sig),
	}
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		return nil, errs.New("marshal credential envelope").Wrap()
	}
	credentialB64 := base64.StdEncoding.EncodeToString(envelopeBytes)

	return &pbopenmls.IssueCredentialResp{
		Credential:     credentialB64,
		CredentialType: "basic",
		IssuedAt:       now.Unix(),
		ExpiresAt:      expiresAt.Unix(),
		Issuer:         s.config.RpcConfig.SigningKey.Issuer,
	}, nil
}

func (s *openMLSServer) VerifyCredential(ctx context.Context, req *pbopenmls.VerifyCredentialReq) (*pbopenmls.VerifyCredentialResp, error) {
	if s.signingKey == nil {
		return nil, errs.New("credential verification is not configured (signingKey is empty)").Wrap()
	}
	if req.Credential == "" {
		return nil, errs.ErrArgs.WrapMsg("credential is required")
	}

	payload, ok := s.verifyCredentialEnvelope(ctx, req.Credential)
	if !ok {
		return &pbopenmls.VerifyCredentialResp{Valid: false}, nil
	}

	// Parse identity: userID:deviceID:platform
	var userID, deviceID string
	parts := splitIdentity(payload.Identity)
	if len(parts) >= 2 {
		userID = parts[0]
		deviceID = parts[1]
	}

	return &pbopenmls.VerifyCredentialResp{
		Valid:     true,
		UserID:    userID,
		DeviceID:  deviceID,
		ExpiresAt: payload.ExpiresAt,
	}, nil
}

// verifyCredentialEnvelope decodes a base64 credential envelope, verifies its
// Ed25519 signature against the root signing key, and checks expiry. It returns
// the decoded payload and true only when the credential is authentic and valid.
// Each failure path logs a distinct reason to aid production debugging.
func (s *openMLSServer) verifyCredentialEnvelope(ctx context.Context, credentialB64 string) (*credentialPayload, bool) {
	const logPrefix = "verifyCredentialEnvelope"
	if s.signingKey == nil {
		log.ZWarn(ctx, logPrefix+": signing key not configured", nil)
		return nil, false
	}
	if credentialB64 == "" {
		log.ZWarn(ctx, logPrefix+": empty credential", nil)
		return nil, false
	}
	envelopeBytes, err := base64.StdEncoding.DecodeString(credentialB64)
	if err != nil {
		log.ZWarn(ctx, logPrefix+": outer base64 decode failed (credential must be base64(JSON envelope))", err,
			"credentialLen", len(credentialB64))
		return nil, false
	}
	var envelope credentialEnvelope
	if err := json.Unmarshal(envelopeBytes, &envelope); err != nil {
		log.ZWarn(ctx, logPrefix+": envelope JSON unmarshal failed", err,
			"envelopeBytesLen", len(envelopeBytes))
		return nil, false
	}
	payloadBytes, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil {
		log.ZWarn(ctx, logPrefix+": payload base64 decode failed", err)
		return nil, false
	}
	sig, err := base64.StdEncoding.DecodeString(envelope.Sig)
	if err != nil {
		log.ZWarn(ctx, logPrefix+": signature base64 decode failed", err)
		return nil, false
	}
	pubKey := s.signingKey.Public().(ed25519.PublicKey)
	if !ed25519.Verify(pubKey, payloadBytes, sig) {
		log.ZWarn(ctx, logPrefix+": Ed25519 signature verification failed (wrong signing key or tampered credential)", nil,
			"payloadBytesLen", len(payloadBytes), "sigBytesLen", len(sig))
		return nil, false
	}
	var payload credentialPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		log.ZWarn(ctx, logPrefix+": payload JSON unmarshal failed", err,
			"payloadBytesLen", len(payloadBytes))
		return nil, false
	}
	now := time.Now().Unix()
	if now > payload.ExpiresAt {
		log.ZWarn(ctx, logPrefix+": credential expired", nil,
			"identity", payload.Identity,
			"issuedAt", payload.IssuedAt,
			"expiresAt", payload.ExpiresAt,
			"now", now)
		return nil, false
	}
	return &payload, true
}

// mlsKPMeta holds metadata extracted from a KeyPackage during credential
// verification. Used to populate storage fields without re-parsing.
type mlsKPMeta struct {
	Platform    string // from credential identity (userID:deviceID:platform)
	Ciphersuite string // hex of the 2-byte RFC 9420 ciphersuite, e.g. "0x0001"
}

// verifyKeyPackageCredential decodes the credential embedded in a KeyPackage and
// verifies that it was issued by this server, is bound to the KeyPackage's own
// leaf signature key, and matches the claimed userID/deviceID. It returns the
// extracted metadata (Platform, Ciphersuite) for use by callers.
//
// When credential issuing is disabled (no signing key) verification is skipped
// and an empty mlsKPMeta is returned.
//
// Contract: the client MUST place the exact `credential` string returned by
// IssueCredential into the KeyPackage's BasicCredential.identity field.
func (s *openMLSServer) verifyKeyPackageCredential(ctx context.Context, kpBytes []byte, userID, deviceID string) (mlsKPMeta, error) {
	if s.signingKey == nil {
		return mlsKPMeta{}, nil
	}
	cs, signatureKey, credentialIdentity, err := extractLeafCredential(kpBytes)
	if err != nil {
		return mlsKPMeta{}, errs.ErrArgs.WrapMsg("invalid keyPackage structure: " + err.Error())
	}
	payload, ok := s.verifyCredentialEnvelope(ctx, string(credentialIdentity))
	if !ok {
		log.ZWarn(ctx, "verifyKeyPackageCredential: embedded credential invalid",
			errs.ErrNoPermission.WrapMsg("credential signature verification failed or expired"),
			"userID", userID, "deviceID", deviceID,
			"identityBytesLen", len(credentialIdentity))
		return mlsKPMeta{}, errs.ErrNoPermission.WrapMsg("credential signature verification failed or expired")
	}
	if payload.LeafPubKey != base64.StdEncoding.EncodeToString(signatureKey) {
		return mlsKPMeta{}, errs.ErrNoPermission.WrapMsg("credential is not bound to this keyPackage leaf key")
	}
	parts := splitIdentity(payload.Identity)
	if len(parts) < 2 || parts[0] != userID || parts[1] != deviceID {
		return mlsKPMeta{}, errs.ErrNoPermission.WrapMsg("credential identity does not match userID/deviceID")
	}
	var platform string
	if len(parts) >= 3 {
		platform = parts[2]
	}
	return mlsKPMeta{
		Platform:    platform,
		Ciphersuite: fmt.Sprintf("0x%04x", cs),
	}, nil
}

func (s *openMLSServer) GetRootPublicKey(ctx context.Context, req *pbopenmls.GetRootPublicKeyReq) (*pbopenmls.GetRootPublicKeyResp, error) {
	if s.rootPubKey == "" {
		return nil, errs.New("signing key not configured").Wrap()
	}
	keyID := s.config.RpcConfig.SigningKey.KeyID
	if keyID == "" {
		keyID = "v1"
	}
	return &pbopenmls.GetRootPublicKeyResp{
		PublicKey: s.rootPubKey,
		KeyID:     keyID,
		Algorithm: "Ed25519",
	}, nil
}

// splitIdentity splits "userID:deviceID:platform" by the first two colons.
// singleChatPeerUserIDs returns peer user IDs for a 1:1 MLS groupID (si_ or
// c_1v1_ prefix). IDs in the groupID are lexicographically sorted.
func singleChatPeerUserIDs(groupID, senderUserID string) []string {
	var raw string
	switch {
	case strings.HasPrefix(groupID, "si_"):
		raw = strings.TrimPrefix(groupID, "si_")
	case strings.HasPrefix(groupID, "c_1v1_"):
		raw = strings.TrimPrefix(groupID, "c_1v1_")
	default:
		return nil
	}
	ids := strings.Split(raw, "_")
	if len(ids) != 2 {
		return nil
	}
	peers := make([]string, 0, 1)
	for _, id := range ids {
		if id != "" && id != senderUserID {
			peers = append(peers, id)
		}
	}
	return peers
}

func splitIdentity(identity string) []string {
	var parts []string
	start := 0
	colons := 0
	for i := 0; i < len(identity) && colons < 2; i++ {
		if identity[i] == ':' {
			parts = append(parts, identity[start:i])
			start = i + 1
			colons++
		}
	}
	parts = append(parts, identity[start:])
	return parts
}

// InitGroupTrigger is called by the Group RPC immediately after a new OpenIM
// group is created. It sends a mls_group_init_trigger notification to all of
// the creator's devices so that the client-side MLS group-creation flow starts
// automatically — the creator fetches KeyPackages, generates the initial
// Commit + Welcome messages, and calls SubmitCommit.
//
// This RPC performs no cryptographic operations; it is a pure signalling call.
// Errors are non-fatal for the caller (fire-and-return pattern): if the
// trigger fails the creator can still initiate MLS setup manually or on next
// login.
func (s *openMLSServer) InitGroupTrigger(ctx context.Context, req *pbopenmls.InitGroupTriggerReq) (*pbopenmls.InitGroupTriggerResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}
	if req.CreatorUserID == "" {
		return nil, errs.ErrArgs.WrapMsg("creatorUserID is required")
	}
	s.sendGroupInitTrigger(ctx, req.GroupID, req.CreatorUserID, req.MemberUserIDs)
	return &pbopenmls.InitGroupTriggerResp{}, nil
}

// AddMemberTrigger is called by the Group RPC immediately after new members
// are invited into an OpenIM group. It sends a mls_add_member_trigger
// notification to the operator's devices so that the client-side MLS Add flow
// starts automatically — the operator fetches KeyPackages for the new members,
// creates an Add-Commit + Welcome bundle, and calls SubmitCommit.
//
// This RPC performs no cryptographic operations; it is a pure signalling call.
func (s *openMLSServer) AddMemberTrigger(ctx context.Context, req *pbopenmls.AddMemberTriggerReq) (*pbopenmls.AddMemberTriggerResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}
	if req.OperatorUserID == "" {
		return nil, errs.ErrArgs.WrapMsg("operatorUserID is required")
	}
	if len(req.NewMemberUserIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("newMemberUserIDs must not be empty")
	}
	s.sendAddMemberTrigger(ctx, req.GroupID, req.OperatorUserID, req.NewMemberUserIDs)
	return &pbopenmls.AddMemberTriggerResp{}, nil
}

// RemoveMemberTrigger is called by the Group RPC immediately after members are
// kicked from an OpenIM group. It sends a mls_remove_member_trigger
// notification to the operator's devices so that the client-side MLS Remove
// flow starts automatically — the operator creates a Remove-Commit and calls
// SubmitCommit to rotate the group epoch, preventing kicked members from
// decrypting future messages.
//
// This RPC performs no cryptographic operations; it is a pure signalling call.
func (s *openMLSServer) RemoveMemberTrigger(ctx context.Context, req *pbopenmls.RemoveMemberTriggerReq) (*pbopenmls.RemoveMemberTriggerResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}
	if req.OperatorUserID == "" {
		return nil, errs.ErrArgs.WrapMsg("operatorUserID is required")
	}
	if len(req.RemovedMemberUserIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("removedMemberUserIDs must not be empty")
	}
	s.sendRemoveMemberTrigger(ctx, req.GroupID, req.OperatorUserID, req.RemovedMemberUserIDs)
	return &pbopenmls.RemoveMemberTriggerResp{}, nil
}
