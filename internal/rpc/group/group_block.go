package group

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	pbgroup "github.com/openimsdk/protocol/group"
	"github.com/openimsdk/tools/mcontext"
)

func (s *groupServer) SetGroupBlock(ctx context.Context, req *pbgroup.SetGroupBlockReq) (*pbgroup.SetGroupBlockResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, servererrs.ErrNoPermission.WrapMsg("op user id is empty")
	}
	if _, err := s.db.TakeGroupMember(ctx, req.GroupID, opUserID); err != nil {
		return nil, err
	}
	if !req.Block {
		return &pbgroup.SetGroupBlockResp{}, s.groupBlockDB.Delete(ctx, opUserID, req.GroupID)
	}
	return &pbgroup.SetGroupBlockResp{}, s.groupBlockDB.Upsert(ctx, &model.GroupBlock{
		OwnerUserID: opUserID,
		GroupID:     req.GroupID,
		CreateTime:  time.Now(),
	})
}

func (s *groupServer) GetGroupBlock(ctx context.Context, req *pbgroup.GetGroupBlockReq) (*pbgroup.GetGroupBlockResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, servererrs.ErrNoPermission.WrapMsg("op user id is empty")
	}
	if _, err := s.db.TakeGroupMember(ctx, req.GroupID, opUserID); err != nil {
		return nil, err
	}
	rec, err := s.groupBlockDB.Get(ctx, opUserID, req.GroupID)
	if err != nil {
		return nil, err
	}
	return &pbgroup.GetGroupBlockResp{Blocked: rec != nil}, nil
}

func (s *groupServer) GetBlockGroup(ctx context.Context, req *pbgroup.GetBlockGroupReq) (*pbgroup.GetBlockGroupResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, servererrs.ErrNoPermission.WrapMsg("op user id is empty")
	}
	groupIDs, err := s.groupBlockDB.ListGroupIDsByOwner(ctx, opUserID)
	if err != nil {
		return nil, err
	}
	return &pbgroup.GetBlockGroupResp{GroupIDs: groupIDs}, nil
}
