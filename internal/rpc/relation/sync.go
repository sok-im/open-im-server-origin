package relation

import (
	"context"
	"slices"

	"github.com/openimsdk/open-im-server/v3/pkg/util/hashutil"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"

	"github.com/openimsdk/open-im-server/v3/internal/rpc/incrversion"
	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/relation"
)

func (s *friendServer) NotificationUserInfoUpdate(ctx context.Context, req *relation.NotificationUserInfoUpdateReq) (*relation.NotificationUserInfoUpdateResp, error) {
	// Users who have req.UserID in their own friend list (e.g. A one-way added B).
	// Their local friend card for req.UserID must be version-bumped and refreshed.
	ownerUserIDs, err := s.db.FindFriendUserID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// Users listed in req.UserID's own friend list (e.g. B one-way added A).
	// They should also be notified when req.UserID updates profile, even though
	// req.UserID is not in their friend list.
	inFriendListUserIDs, err := s.db.FindFriendUserIDs(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	notifyUserIDs := datautil.Distinct(append(ownerUserIDs, inFriendListUserIDs...))

	log.ZInfo(ctx, "NotificationUserInfoUpdate",
		"ownerUserIDs", ownerUserIDs,
		"inFriendListUserIDs", inFriendListUserIDs,
		"notifyUserIDs", notifyUserIDs,
		"user", req)

	if len(notifyUserIDs) > 0 {
		friendUserIDs := []string{req.UserID}
		noCancelCtx := context.WithoutCancel(ctx)
		err := s.queue.PushCtx(ctx, func() {
			for _, ownerUserID := range ownerUserIDs {
				if err := s.db.OwnerIncrVersion(noCancelCtx, ownerUserID, friendUserIDs, model.VersionStateUpdate); err != nil {
					log.ZError(ctx, "OwnerIncrVersion", err, "ownerUserID", ownerUserID, "friendUserIDs", friendUserIDs)
				}
			}
			for _, notifyUserID := range notifyUserIDs {
				s.notificationSender.FriendInfoUpdatedNotification(noCancelCtx, req.UserID, notifyUserID)
			}
		})
		if err != nil {
			log.ZError(ctx, "NotificationUserInfoUpdate timeout", err, "userID", req.UserID)
		}
	}
	return &relation.NotificationUserInfoUpdateResp{}, nil
}

func (s *friendServer) GetFullFriendUserIDs(ctx context.Context, req *relation.GetFullFriendUserIDsReq) (*relation.GetFullFriendUserIDsResp, error) {
	if err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}
	vl, err := s.db.FindMaxFriendVersionCache(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	userIDs, err := s.db.FindFriendUserIDs(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	idHash := hashutil.IdHash(userIDs)
	if req.IdHash == idHash {
		userIDs = nil
	}
	return &relation.GetFullFriendUserIDsResp{
		Version:   uint64(vl.Version),
		VersionID: vl.ID.Hex(),
		Equal:     req.IdHash == idHash,
		UserIDs:   userIDs,
	}, nil
}

func (s *friendServer) GetIncrementalFriends(ctx context.Context, req *relation.GetIncrementalFriendsReq) (*relation.GetIncrementalFriendsResp, error) {
	if err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}
	var sortVersion uint64
	opt := incrversion.Option[*sdkws.FriendInfo, relation.GetIncrementalFriendsResp]{
		Ctx:           ctx,
		VersionKey:    req.UserID,
		VersionID:     req.VersionID,
		VersionNumber: req.Version,
		Version: func(ctx context.Context, ownerUserID string, version uint, limit int) (*model.VersionLog, error) {
			vl, err := s.db.FindFriendIncrVersion(ctx, ownerUserID, version, limit)
			if err != nil {
				return nil, err
			}
			vl.Logs = slices.DeleteFunc(vl.Logs, func(elem model.VersionLogElem) bool {
				if elem.EID == model.VersionSortChangeID {
					vl.LogLen--
					sortVersion = uint64(elem.Version)
					return true
				}
				return false
			})
			return vl, nil
		},
		CacheMaxVersion: s.db.FindMaxFriendVersionCache,
		Find: func(ctx context.Context, ids []string) ([]*sdkws.FriendInfo, error) {
			return s.getFriend(ctx, req.UserID, ids)
		},
		Resp: func(version *model.VersionLog, deleteIds []string, insertList, updateList []*sdkws.FriendInfo, full bool) *relation.GetIncrementalFriendsResp {
			return &relation.GetIncrementalFriendsResp{
				VersionID:   version.ID.Hex(),
				Version:     uint64(version.Version),
				Full:        full,
				Delete:      deleteIds,
				Insert:      insertList,
				Update:      updateList,
				SortVersion: sortVersion,
			}
		},
	}
	return opt.Build()
}
