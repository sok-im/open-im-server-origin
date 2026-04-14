package crypto

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	pbcrypto "github.com/openimsdk/protocol/crypto"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
)

// GetGroupKeyVersion returns the latest group key version for the given group.
// Clients use this to detect whether they need to re-fetch the group session key.
func (s *cryptoServer) GetGroupKeyVersion(ctx context.Context, req *pbcrypto.GetGroupKeyVersionReq) (*pbcrypto.GetGroupKeyVersionResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}

	version, err := s.db.GetLatestGroupKeyVersion(ctx, req.GroupID)
	if err != nil {
		return nil, errs.WrapMsg(err, "GetLatestGroupKeyVersion failed", "groupID", req.GroupID)
	}

	return &pbcrypto.GetGroupKeyVersionResp{
		GroupID:         req.GroupID,
		GroupKeyVersion: version,
	}, nil
}

// BumpGroupKeyVersion atomically increments the group key version and records
// the rotation event. This is typically called when group membership changes
// (member added/removed) or when an admin explicitly rotates the group key.
//
// Write path:
//  1. Atomic $inc on version counter (FindOneAndUpdate + upsert)
//  2. Write GroupKeyEvent with the returned new version
//  3. Return new version to caller
//
// The caller (group service or admin API) is responsible for notifying group
// members about the rotation via the msg service Notification channel.
func (s *cryptoServer) BumpGroupKeyVersion(ctx context.Context, req *pbcrypto.BumpGroupKeyVersionReq) (*pbcrypto.BumpGroupKeyVersionResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}
	if req.OperatorUserID == "" {
		return nil, errs.ErrArgs.WrapMsg("operatorUserID is required")
	}

	newVersion, err := s.db.AtomicBumpGroupKeyVersion(ctx, req.GroupID)
	if err != nil {
		return nil, errs.WrapMsg(err, "AtomicBumpGroupKeyVersion failed", "groupID", req.GroupID)
	}

	eventType := req.EventType
	if eventType == "" {
		eventType = "rotated"
	}

	event := &model.GroupKeyEvent{
		EventID:         newRequestID(),
		GroupID:         req.GroupID,
		GroupKeyVersion: newVersion,
		EventType:       eventType,
		OperatorUserID:  req.OperatorUserID,
		CreateTime:      time.Now().UnixMilli(),
	}

	if err := s.db.CreateGroupKeyEvent(ctx, event); err != nil {
		return nil, errs.WrapMsg(err, "CreateGroupKeyEvent failed", "groupID", req.GroupID, "newVersion", newVersion)
	}

	log.ZInfo(ctx, "BumpGroupKeyVersion",
		"groupID", req.GroupID,
		"newVersion", newVersion,
		"eventType", eventType,
		"operator", req.OperatorUserID,
	)

	return &pbcrypto.BumpGroupKeyVersionResp{
		GroupID:         req.GroupID,
		GroupKeyVersion: newVersion,
	}, nil
}

// GetGroupKeyEvents returns all group key events that occurred after the given
// version. Clients use this to sync group key rotation history during bootstrap
// or after reconnect.
func (s *cryptoServer) GetGroupKeyEvents(ctx context.Context, req *pbcrypto.GetGroupKeyEventsReq) (*pbcrypto.GetGroupKeyEventsResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}

	events, err := s.db.GetGroupKeyEventsSince(ctx, req.GroupID, req.SinceVersion)
	if err != nil {
		return nil, errs.WrapMsg(err, "GetGroupKeyEventsSince failed", "groupID", req.GroupID)
	}

	pbEvents := make([]*pbcrypto.GroupKeyEventInfo, 0, len(events))
	for _, e := range events {
		pbEvents = append(pbEvents, &pbcrypto.GroupKeyEventInfo{
			EventID:         e.EventID,
			GroupID:         e.GroupID,
			GroupKeyVersion: e.GroupKeyVersion,
			EventType:       e.EventType,
			OperatorUserID:  e.OperatorUserID,
			CreateTime:      e.CreateTime,
		})
	}

	return &pbcrypto.GetGroupKeyEventsResp{Events: pbEvents}, nil
}
