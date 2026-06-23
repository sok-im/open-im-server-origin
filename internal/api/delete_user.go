package api

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	relationrpc "github.com/openimsdk/open-im-server/v3/internal/rpc/relation"
	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/relation"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
)

// DeleteUserApi handles real account deletion (hard delete).
// It follows the same direct-DB pattern as UserGlobalBlackApi.
type DeleteUserApi struct {
	userDB           controller.UserDatabase
	friendDB         database.Friend
	phoneSNDB        database.PhoneSN
	totpDB           database.UserTotp
	totpRecoveryDB   database.UserTotpRecovery
	authClient       *rpcli.AuthClient
	groupClient      group.GroupClient
	friendClient     relation.FriendClient
	friendNotifier   *relationrpc.FriendNotificationSender
	imAdminUserIDs   []string
}

func NewDeleteUserApi(
	userDB controller.UserDatabase,
	friendDB database.Friend,
	phoneSNDB database.PhoneSN,
	totpDB database.UserTotp,
	totpRecoveryDB database.UserTotpRecovery,
	authClient *rpcli.AuthClient,
	groupClient group.GroupClient,
	friendClient relation.FriendClient,
	friendNotifier *relationrpc.FriendNotificationSender,
	imAdminUserIDs []string,
) *DeleteUserApi {
	return &DeleteUserApi{
		userDB:           userDB,
		friendDB:         friendDB,
		phoneSNDB:        phoneSNDB,
		totpDB:           totpDB,
		totpRecoveryDB:   totpRecoveryDB,
		authClient:       authClient,
		groupClient:      groupClient,
		friendClient:     friendClient,
		friendNotifier:   friendNotifier,
		imAdminUserIDs:   imAdminUserIDs,
	}
}

type deleteUserReq struct {
	UserID string `json:"userID" binding:"required"`
}

// DeleteUser permanently deletes a user account and cleans up associated data.
// Steps: force-logout → delete friends → dismiss owned groups / quit others → hard-delete user doc.
// Caller must be the same user as userID, or an IM admin (see CheckAccessV3).
func (d *DeleteUserApi) DeleteUser(c *gin.Context) {
	var req deleteUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	// Only the user themselves (or an IM admin) may delete the account.
	if err := authverify.CheckAccessV3(c, req.UserID, d.imAdminUserIDs); err != nil {
		apiresp.GinError(c, err)
		return
	}

	// 1. Verify user exists
	users, err := d.userDB.Find(c, []string{req.UserID})
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	if len(users) == 0 {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("user not found", "userID", req.UserID))
		return
	}

	notifyUserIDs := d.collectAccountDeletedNotifyUserIDs(c, req.UserID)

	// 2. Force logout from every client platform (skip Admin; only IDs accepted by ForceLogout RPC).
	for platformID := range constant.PlatformID2Name {
		plf := int32(platformID)
		if plf == constant.AdminPlatformID {
			continue
		}
		if plf < constant.IOSPlatformID || plf > constant.HarmonyOSPlatformID {
			continue
		}
		if err := d.authClient.ForceLogout(c, req.UserID, plf); err != nil {
			log.ZWarn(c, "DeleteUser: ForceLogout failed", err, "userID", req.UserID, "platformID", plf)
		}
	}

	// 3. Delete friendships on the deleted user's side (owner_user_id = req.UserID).
	friendIDsResp, err := d.friendClient.GetFriendIDs(c, &relation.GetFriendIDsReq{UserID: req.UserID})
	if err != nil {
		log.ZWarn(c, "DeleteUser: GetFriendIDs failed", err, "userID", req.UserID)
	} else {
		for _, friendID := range friendIDsResp.FriendIDs {
			if _, err := d.friendClient.DeleteFriend(c, &relation.DeleteFriendReq{
				OwnerUserID:  req.UserID,
				FriendUserID: friendID,
			}); err != nil {
				log.ZWarn(c, "DeleteUser: DeleteFriend (owner→friend) failed", err,
					"ownerUserID", req.UserID, "friendUserID", friendID)
			}
		}
	}

	// 3b. Remove this user from every other user's friend list (friend_user_id = req.UserID).
	d.deleteFriendsReferencingUser(c, req.UserID)

	// 4. Leave all joined groups: dismiss groups owned by the user, quit the rest.
	// Use admin identity for group RPCs (same as deleteFriendsReferencingUser) so DismissGroup /
	// GetGroupMembersInfo succeed reliably during account deletion.
	// Always request page 1: after dismiss/quit the joined list shrinks; incrementing pageNumber
	// would skip remaining groups (e.g. 150 groups → page 2 is empty after processing page 1).
	adminCtx := d.adminCtx(c)
	const pageSize = int32(100)
	for {
		groupListResp, err := d.groupClient.GetJoinedGroupList(adminCtx, &group.GetJoinedGroupListReq{
			FromUserID: req.UserID,
			Pagination: &sdkws.RequestPagination{PageNumber: 1, ShowNumber: pageSize},
		})
		if err != nil {
			log.ZWarn(c, "DeleteUser: GetJoinedGroupList failed", err, "userID", req.UserID)
			break
		}
		if len(groupListResp.Groups) == 0 {
			break
		}
		ownerCtx := mcontext.SetOpUserID(adminCtx, req.UserID)
		for _, g := range groupListResp.Groups {
			if d.isGroupOwnerForDelete(adminCtx, g.GroupID, req.UserID, g.OwnerUserID) {
				log.ZDebug(ownerCtx, "DeleteUser: DismissGroup", "groupID", g.GroupID, "userID", req.UserID, "ownerUserID", g.OwnerUserID)
				// DeleteMember must be false so GroupDismissedNotification is sent to remaining
				// members (DeleteMember=true skips notification; push then clears members).
				if _, err := d.groupClient.DismissGroup(ownerCtx, &group.DismissGroupReq{
					GroupID: g.GroupID,
				}); err != nil {
					log.ZWarn(c, "DeleteUser: DismissGroup failed", err, "userID", req.UserID, "groupID", g.GroupID)
				}
				continue
			}
			log.ZDebug(adminCtx, "DeleteUser: QuitGroup", "groupID", g.GroupID, "userID", req.UserID, "ownerUserID", g.OwnerUserID)
			if _, err := d.groupClient.QuitGroup(adminCtx, &group.QuitGroupReq{
				GroupID: g.GroupID,
				UserID:  req.UserID,
			}); err != nil {
				// Owner detection may fail when OwnerUserID is stale; QuitGroup rejects owners.
				if errs.ErrNoPermission.Is(err) && strings.Contains(err.Error(), "group owner can't quit") {
					if _, dismissErr := d.groupClient.DismissGroup(ownerCtx, &group.DismissGroupReq{
						GroupID: g.GroupID,
					}); dismissErr != nil {
						log.ZWarn(c, "DeleteUser: DismissGroup fallback failed", dismissErr,
							"userID", req.UserID, "groupID", g.GroupID)
					}
					continue
				}
				log.ZWarn(c, "DeleteUser: QuitGroup failed", err, "userID", req.UserID, "groupID", g.GroupID)
			}
		}
		if int32(len(groupListResp.Groups)) < pageSize {
			break
		}
	}

	// 5. Delete phone_sn_info record bound to this user's phone number.
	if phone := users[0].Phone; phone != "" {
		if err := d.phoneSNDB.DeleteByPhone(c, phone); err != nil {
			log.ZWarn(c, "DeleteUser: DeleteByPhone failed", err, "userID", req.UserID, "phone", phone)
		}
	}

	// 5b. Delete TOTP binding and recovery codes.
	if d.totpDB != nil {
		if err := d.totpDB.Delete(c, req.UserID); err != nil {
			log.ZWarn(c, "DeleteUser: Delete user_totp failed", err, "userID", req.UserID)
		}
	}
	if d.totpRecoveryDB != nil {
		if err := d.totpRecoveryDB.DeleteByUser(c, req.UserID); err != nil {
			log.ZWarn(c, "DeleteUser: Delete user_totp_recovery failed", err, "userID", req.UserID)
		}
	}

	// 6. Hard-delete user document and invalidate Redis / local user-info cache.
	if err := d.userDB.Delete(c, []string{req.UserID}); err != nil {
		apiresp.GinError(c, err)
		return
	}

	d.notifyAccountDeleted(c, req.UserID, notifyUserIDs)

	log.ZInfo(c, "DeleteUser: user deleted", "userID", req.UserID)
	apiresp.GinSuccess(c, nil)
}

// deleteFriendsReferencingUser 删除所有 owner 侧仍引用被删用户的好友记录（friend_user_id = deletedUserID）。
// 使用管理员身份调用 DeleteFriend，避免自删账号时 opUserID 无权操作他人 owner_user_id。
func (d *DeleteUserApi) deleteFriendsReferencingUser(ctx context.Context, deletedUserID string) {
	if d.friendDB == nil {
		log.ZWarn(ctx, "DeleteUser: friendDB is nil, skip reversal friend cleanup", nil, "userID", deletedUserID)
		return
	}
	if len(d.imAdminUserIDs) == 0 {
		log.ZWarn(ctx, "DeleteUser: no imAdminUserID, skip reversal friend cleanup", nil, "userID", deletedUserID)
		return
	}

	ownerUserIDs, err := d.friendDB.FindFriendUserID(ctx, deletedUserID)
	if err != nil {
		log.ZWarn(ctx, "DeleteUser: FindFriendUserID failed", err, "userID", deletedUserID)
		return
	}
	if len(ownerUserIDs) == 0 {
		return
	}

	for _, ownerUserID := range ownerUserIDs {
		if ownerUserID == deletedUserID {
			continue
		}
		if _, err := d.friendClient.DeleteFriend(d.adminCtx(ctx), &relation.DeleteFriendReq{
			OwnerUserID:  ownerUserID,
			FriendUserID: deletedUserID,
		}); err != nil {
			log.ZWarn(ctx, "DeleteUser: DeleteFriend (friend→owner) failed", err,
				"ownerUserID", ownerUserID, "friendUserID", deletedUserID)
		}
	}
}

func (d *DeleteUserApi) collectAccountDeletedNotifyUserIDs(ctx context.Context, deletedUserID string) []string {
	userIDSet := make(map[string]struct{})
	if d.friendDB != nil {
		if ownerUserIDs, err := d.friendDB.FindFriendUserID(ctx, deletedUserID); err == nil {
			for _, id := range ownerUserIDs {
				if id != deletedUserID {
					userIDSet[id] = struct{}{}
				}
			}
		}
	}
	if resp, err := d.friendClient.GetFriendIDs(ctx, &relation.GetFriendIDsReq{UserID: deletedUserID}); err == nil {
		for _, id := range resp.FriendIDs {
			if id != deletedUserID {
				userIDSet[id] = struct{}{}
			}
		}
	}
	notifyUserIDs := make([]string, 0, len(userIDSet))
	for id := range userIDSet {
		notifyUserIDs = append(notifyUserIDs, id)
	}
	return notifyUserIDs
}

func (d *DeleteUserApi) notifyAccountDeleted(ctx context.Context, deletedUserID string, notifyUserIDs []string) {
	if d.friendNotifier == nil || len(notifyUserIDs) == 0 {
		return
	}
	adminCtx := d.adminCtx(ctx)
	for _, notifyUserID := range notifyUserIDs {
		if notifyUserID == deletedUserID {
			continue
		}
		d.friendNotifier.FriendsInfoUpdateNotification(adminCtx, notifyUserID, []string{deletedUserID})
		d.friendNotifier.FriendInfoUpdatedNotification(adminCtx, deletedUserID, notifyUserID)
	}
	log.ZInfo(ctx, "DeleteUser: notified related users", "deletedUserID", deletedUserID, "notifyCount", len(notifyUserIDs))
}

func (d *DeleteUserApi) adminCtx(ctx context.Context) context.Context {
	if len(d.imAdminUserIDs) == 0 {
		return ctx
	}
	return mcontext.SetOpUserID(ctx, d.imAdminUserIDs[0])
}

// isGroupOwnerForDelete 判断用户是否为群主。优先用 GroupInfo.OwnerUserID；为空或不一致时回查成员角色，
// 避免 OwnerUserID 未填充时误走 QuitGroup（群主不能退群）导致群未解散。
func (d *DeleteUserApi) isGroupOwnerForDelete(ctx context.Context, groupID, userID, ownerUserID string) bool {
	if ownerUserID == userID {
		return true
	}
	resp, err := d.groupClient.GetGroupMembersInfo(ctx, &group.GetGroupMembersInfoReq{
		GroupID: groupID,
		UserIDs: []string{userID},
	})
	if err != nil {
		log.ZWarn(ctx, "DeleteUser: GetGroupMembersInfo failed", err, "userID", userID, "groupID", groupID)
		return false
	}
	if len(resp.Members) == 0 {
		return false
	}
	return resp.Members[0].RoleLevel == constant.GroupOwner
}
