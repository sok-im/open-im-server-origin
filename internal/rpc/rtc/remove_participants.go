package rtc

import (
	"context"

	"github.com/livekit/protocol/livekit"
	"github.com/openimsdk/protocol/rtc"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
)

// SignalRemoveParticipants removes users from an active group call's LiveKit room.
func (s *rtcServer) SignalRemoveParticipants(ctx context.Context, req *rtc.SignalRemoveParticipantsReq) (*rtc.SignalRemoveParticipantsResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is empty")
	}
	if len(req.UserIDs) == 0 {
		return &rtc.SignalRemoveParticipantsResp{}, nil
	}
	inv, err := s.db.GetInvitationByGroupID(ctx, req.GroupID)
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			log.ZDebug(ctx, "SignalRemoveParticipants: no active invitation", "groupID", req.GroupID)
			return &rtc.SignalRemoveParticipantsResp{}, nil
		}
		return nil, err
	}
	log.ZInfo(ctx, "SignalRemoveParticipants: start",
		"groupID", req.GroupID, "roomID", inv.RoomID, "userIDs", req.UserIDs, "e2eeRequired", inv.E2EERequired)
	removed := 0
	for _, uid := range req.UserIDs {
		if uid == "" {
			continue
		}
		_, err := s.roomClient.RemoveParticipant(ctx, &livekit.RoomParticipantIdentity{
			Room:     inv.RoomID,
			Identity: uid,
		})
		if err != nil {
			log.ZWarn(ctx, "RemoveParticipant failed", err, "roomID", inv.RoomID, "userID", uid, "groupID", req.GroupID)
			continue
		}
		removed++
	}
	log.ZInfo(ctx, "SignalRemoveParticipants: done",
		"groupID", req.GroupID, "roomID", inv.RoomID, "removed", removed, "requested", len(req.UserIDs))
	return &rtc.SignalRemoveParticipantsResp{}, nil
}
