// Copyright © 2023 OpenIM. All rights reserved.
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

package group

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/versionctx"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/open-im-server/v3/pkg/notification"
	"github.com/openimsdk/open-im-server/v3/pkg/notification/common_user"
	"github.com/openimsdk/protocol/constant"
	pbgroup "github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/stringutil"
	"go.mongodb.org/mongo-driver/mongo"
)

// GroupApplicationReceiver
const (
	applicantReceiver = iota
	adminReceiver
)

func NewNotificationSender(db controller.GroupDatabase, config *Config, userClient *rpcli.UserClient, relationClient *rpcli.RelationClient, msgClient *rpcli.MsgClient, conversationClient *rpcli.ConversationClient) *NotificationSender {
	resolveDisplayNickname := func(ctx context.Context, viewerUserID, targetUserID string) (string, error) {
		var alias convert.FriendAliasInfo
		if relationClient != nil && viewerUserID != "" {
			friends, err := relationClient.GetFriendsInfo(ctx, viewerUserID, []string{targetUserID})
			if err == nil && len(friends) > 0 && friends[0] != nil {
				alias = convert.FriendAliasInfo{
					Remark:          friends[0].GetRemark(),
					FriendFirstName: friends[0].GetFirstName(),
					FriendLastName:  friends[0].GetLastName(),
				}
			}
		}
		u, err := userClient.GetUserInfo(ctx, targetUserID)
		if err != nil {
			return "", err
		}
		return convert.DisplayNicknameForFriend(alias.Remark, alias.FriendFirstName, alias.FriendLastName, u), nil
	}
	return &NotificationSender{
		NotificationSender: notification.NewNotificationSender(&config.NotificationConfig,
			notification.WithRpcClient(func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error) {
				return msgClient.SendMsg(ctx, req)
			}),
			notification.WithUserRpcClient(userClient.GetUserInfo),
			notification.WithDisplayNicknameResolver(resolveDisplayNickname),
		),
		getUsersInfo: func(ctx context.Context, userIDs []string) ([]common_user.CommonUser, error) {
			users, err := userClient.GetUsersInfo(ctx, userIDs)
			if err != nil {
				return nil, err
			}
			return datautil.Slice(users, func(e *sdkws.UserInfo) common_user.CommonUser { return e }), nil
		},
		userClient:             userClient,
		relationClient:         relationClient,
		resolveDisplayNickname: resolveDisplayNickname,
		db:                     db,
		config:                 config,
		msgClient:              msgClient,
		conversationClient:     conversationClient,
	}
}

type NotificationSender struct {
	*notification.NotificationSender
	getUsersInfo           func(ctx context.Context, userIDs []string) ([]common_user.CommonUser, error)
	userClient             *rpcli.UserClient
	relationClient         *rpcli.RelationClient
	resolveDisplayNickname func(ctx context.Context, viewerUserID, targetUserID string) (string, error)
	db                     controller.GroupDatabase
	config                 *Config
	msgClient              *rpcli.MsgClient
	conversationClient     *rpcli.ConversationClient
}

// applyPbMemberDisplayNicknames 按操作者视角重设 Nickname：remark > firstName+lastName > nickname。
func (g *NotificationSender) applyPbMemberDisplayNicknames(ctx context.Context, members ...*sdkws.GroupMemberFullInfo) error {
	if len(members) == 0 || g.userClient == nil {
		return nil
	}
	viewerID := mcontext.GetOpUserID(ctx)
	if viewerID == "" {
		return nil
	}
	userIDs := make([]string, 0, len(members))
	for _, m := range members {
		if m != nil && m.UserID != "" {
			userIDs = append(userIDs, m.UserID)
		}
	}
	if len(userIDs) == 0 {
		return nil
	}
	users, err := g.userClient.GetUsersInfoMap(ctx, userIDs)
	if err != nil {
		return err
	}
	remarkMap := make(map[string]convert.FriendAliasInfo)
	if g.relationClient != nil {
		friendInfos, err := g.relationClient.GetFriendsInfo(ctx, viewerID, userIDs)
		if err != nil {
			return err
		}
		remarkMap = convert.FriendAliasMapFromFriendInfos(friendInfos)
	}
	for _, m := range members {
		if m == nil {
			continue
		}
		alias := remarkMap[m.UserID]
		m.Nickname = convert.DisplayNicknameForFriend(alias.Remark, alias.FriendFirstName, alias.FriendLastName, users[m.UserID])
	}
	return nil
}

func (g *NotificationSender) PopulateGroupMember(ctx context.Context, members ...*model.GroupMember) error {
	if len(members) == 0 {
		return nil
	}
	emptyUserIDs := make(map[string]struct{})
	for _, member := range members {
		if member.Nickname == "" || member.FaceURL == "" {
			emptyUserIDs[member.UserID] = struct{}{}
		}
	}
	if len(emptyUserIDs) > 0 {
		users, err := g.getUsersInfo(ctx, datautil.Keys(emptyUserIDs))
		if err != nil {
			return err
		}
		userMap := make(map[string]common_user.CommonUser)
		for i, user := range users {
			userMap[user.GetUserID()] = users[i]
		}
		for i, member := range members {
			user, ok := userMap[member.UserID]
			if !ok {
				continue
			}
			if member.Nickname == "" {
				if g.resolveDisplayNickname != nil {
					if name, err := g.resolveDisplayNickname(ctx, mcontext.GetOpUserID(ctx), member.UserID); err == nil {
						members[i].Nickname = name
					}
				} else if ui, ok := user.(*sdkws.UserInfo); ok {
					members[i].Nickname = convert.DisplayNickname("", ui)
				} else {
					members[i].Nickname = user.GetNickname()
				}
			}
			if member.FaceURL == "" {
				members[i].FaceURL = user.GetFaceURL()
			}
		}
	}
	return nil
}

func (g *NotificationSender) getUser(ctx context.Context, userID string) (*sdkws.PublicUserInfo, error) {
	users, err := g.getUsersInfo(ctx, []string{userID})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, servererrs.ErrUserIDNotFound.WrapMsg(fmt.Sprintf("user %s not found", userID))
	}
	return &sdkws.PublicUserInfo{
		UserID:   users[0].GetUserID(),
		Nickname: users[0].GetNickname(),
		FaceURL:  users[0].GetFaceURL(),
		Ex:       users[0].GetEx(),
	}, nil
}

func (g *NotificationSender) getGroupInfo(ctx context.Context, groupID string) (*sdkws.GroupInfo, error) {
	gm, err := g.db.TakeGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	num, err := g.db.FindGroupMemberNum(ctx, groupID)
	if err != nil {
		return nil, err
	}
	ownerUserIDs, err := g.db.GetGroupRoleLevelMemberIDs(ctx, groupID, constant.GroupOwner)
	if err != nil {
		return nil, err
	}
	var ownerUserID string
	if len(ownerUserIDs) > 0 {
		ownerUserID = ownerUserIDs[0]
	}

	return convert.Db2PbGroupInfo(gm, ownerUserID, num), nil
}

func (g *NotificationSender) getGroupMembers(ctx context.Context, groupID string, userIDs []string) ([]*sdkws.GroupMemberFullInfo, error) {
	members, err := g.db.FindGroupMembers(ctx, groupID, userIDs)
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	log.ZDebug(ctx, "getGroupMembers", "members", members)
	res := make([]*sdkws.GroupMemberFullInfo, 0, len(members))
	for _, member := range members {
		res = append(res, g.groupMemberDB2PB(member, 0))
	}
	if err := g.applyPbMemberDisplayNicknames(ctx, res...); err != nil {
		return nil, err
	}
	return res, nil
}

func (g *NotificationSender) getGroupMemberMap(ctx context.Context, groupID string, userIDs []string) (map[string]*sdkws.GroupMemberFullInfo, error) {
	members, err := g.getGroupMembers(ctx, groupID, userIDs)
	if err != nil {
		return nil, err
	}
	m := make(map[string]*sdkws.GroupMemberFullInfo)
	for i, member := range members {
		m[member.UserID] = members[i]
	}
	return m, nil
}

func (g *NotificationSender) getGroupMember(ctx context.Context, groupID string, userID string) (*sdkws.GroupMemberFullInfo, error) {
	members, err := g.getGroupMembers(ctx, groupID, []string{userID})
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, errs.ErrInternalServer.WrapMsg(fmt.Sprintf("group %s member %s not found", groupID, userID))
	}
	return members[0], nil
}

func (g *NotificationSender) getGroupOwnerAndAdminUserID(ctx context.Context, groupID string) ([]string, error) {
	members, err := g.db.FindGroupMemberRoleLevels(ctx, groupID, []int32{constant.GroupOwner, constant.GroupAdmin})
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	fn := func(e *model.GroupMember) string { return e.UserID }
	return datautil.Slice(members, fn), nil
}

func (g *NotificationSender) groupMemberDB2PB(member *model.GroupMember, appMangerLevel int32) *sdkws.GroupMemberFullInfo {
	return &sdkws.GroupMemberFullInfo{
		GroupID:        member.GroupID,
		UserID:         member.UserID,
		RoleLevel:      member.RoleLevel,
		JoinTime:       member.JoinTime.UnixMilli(),
		Nickname:       member.Nickname,
		FaceURL:        member.FaceURL,
		AppMangerLevel: appMangerLevel,
		JoinSource:     member.JoinSource,
		OperatorUserID: member.OperatorUserID,
		Ex:             member.Ex,
		MuteEndTime:    member.MuteEndTime.UnixMilli(),
		InviterUserID:  member.InviterUserID,
	}
}

/* func (g *NotificationSender) getUsersInfoMap(ctx context.Context, userIDs []string) (map[string]*sdkws.UserInfo, error) {
	users, err := g.getUsersInfo(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]*sdkws.UserInfo)
	for _, user := range users {
		result[user.GetUserID()] = user.(*sdkws.UserInfo)
	}
	return result, nil
} */

func (g *NotificationSender) fillOpUser(ctx context.Context, targetUser **sdkws.GroupMemberFullInfo, groupID string) (err error) {
	return g.fillUserByUserID(ctx, mcontext.GetOpUserID(ctx), targetUser, groupID)
}

func (g *NotificationSender) fillUserByUserID(ctx context.Context, userID string, targetUser **sdkws.GroupMemberFullInfo, groupID string) error {
	if targetUser == nil {
		return errs.ErrInternalServer.WrapMsg("**sdkws.GroupMemberFullInfo is nil")
	}
	if groupID != "" {
		if authverify.IsManagerUserID(userID, g.config.Share.IMAdminUserID) {
			*targetUser = &sdkws.GroupMemberFullInfo{
				GroupID:        groupID,
				UserID:         userID,
				RoleLevel:      constant.GroupAdmin,
				AppMangerLevel: constant.AppAdmin,
			}
		} else {
			member, err := g.db.TakeGroupMember(ctx, groupID, userID)
			if err == nil {
				*targetUser = g.groupMemberDB2PB(member, 0)
			} else if !(errors.Is(err, mongo.ErrNoDocuments) || errs.ErrRecordNotFound.Is(err)) {
				return err
			}
		}
	}
	user, err := g.getUser(ctx, userID)
	if err != nil {
		return err
	}
	displayName := user.Nickname
	if g.resolveDisplayNickname != nil {
		if name, err := g.resolveDisplayNickname(ctx, mcontext.GetOpUserID(ctx), userID); err == nil && name != "" {
			displayName = name
		}
	}
	if *targetUser == nil {
		*targetUser = &sdkws.GroupMemberFullInfo{
			GroupID:        groupID,
			UserID:         userID,
			Nickname:       displayName,
			FaceURL:        user.FaceURL,
			OperatorUserID: userID,
		}
	} else {
		(*targetUser).Nickname = displayName
		if (*targetUser).FaceURL == "" {
			(*targetUser).FaceURL = user.FaceURL
		}
	}
	return nil
}

func (g *NotificationSender) setVersion(ctx context.Context, version *uint64, versionID *string, collName string, id string) {
	versions := versionctx.GetVersionLog(ctx).Get()
	for i := len(versions) - 1; i >= 0; i-- {
		coll := versions[i]
		if coll.Name == collName && coll.Doc.DID == id {
			*version = uint64(coll.Doc.Version)
			*versionID = coll.Doc.ID.Hex()
			return
		}
	}
}

func (g *NotificationSender) setSortVersion(ctx context.Context, version *uint64, versionID *string, collName string, id string, sortVersion *uint64) {
	versions := versionctx.GetVersionLog(ctx).Get()
	for _, coll := range versions {
		if coll.Name == collName && coll.Doc.DID == id {
			*version = uint64(coll.Doc.Version)
			*versionID = coll.Doc.ID.Hex()
			for _, elem := range coll.Doc.Logs {
				if elem.EID == model.VersionSortChangeID {
					*sortVersion = uint64(elem.Version)
				}
			}
		}
	}
}

func (g *NotificationSender) GroupCreatedNotification(ctx context.Context, tips *sdkws.GroupCreatedTips, SendMessage *bool) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	displayMembers := tips.MemberList
	if tips.GroupOwnerUser != nil {
		displayMembers = append(displayMembers, tips.GroupOwnerUser)
	}
	if tips.OpUser != nil {
		displayMembers = append(displayMembers, tips.OpUser)
	}
	if err = g.applyPbMemberDisplayNicknames(ctx, displayMembers...); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupCreatedNotification, tips, notification.WithSendMessage(SendMessage))
	//g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupE2EENotification, tips, notification.WithSendMessage(SendMessage))
}

func (g *NotificationSender) GroupInfoSetNotification(ctx context.Context, tips *sdkws.GroupInfoSetTips) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupInfoSetNotification, tips, notification.WithRpcGetUserName())
}

func (g *NotificationSender) GroupInfoSetNameNotification(ctx context.Context, tips *sdkws.GroupInfoSetNameTips) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	tips.DefaultTips = groupInfoSetNameDefaultTips(tips.OpUser, tips.Group)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupInfoSetNameNotification, tips)
}

// groupInfoSetNameDefaultTips returns an English notification text, e.g.
// "Alice has changed the group name to NewName".
func groupInfoSetNameDefaultTips(opUser *sdkws.GroupMemberFullInfo, group *sdkws.GroupInfo) string {
	name := ""
	if opUser != nil {
		name = opUser.Nickname
		if name == "" {
			name = opUser.UserID
		}
	}
	if name == "" {
		name = "Someone"
	}
	newName := ""
	if group != nil {
		newName = group.GroupName
	}
	if newName == "" {
		return name + " has changed the group name"
	}
	return name + " has changed the group name to " + newName
}

// opUserName returns the display name for an operator: Nickname → UserID → "Someone".
func opUserName(u *sdkws.GroupMemberFullInfo) string {
	if u == nil {
		return "Someone"
	}
	if u.Nickname != "" {
		return u.Nickname
	}
	if u.UserID != "" {
		return u.UserID
	}
	return "Someone"
}

// burnDurationText converts a duration in seconds to a human-readable English
// string using the coarsest whole unit, e.g. 86400 → "1 day", 7200 → "2 hours".
func burnDurationText(secs int32) string {
	if secs <= 0 {
		return ""
	}
	if secs%86400 == 0 {
		d := secs / 86400
		if d == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", d)
	}
	if secs%3600 == 0 {
		h := secs / 3600
		if h == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", h)
	}
	if secs%60 == 0 {
		m := secs / 60
		if m == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", m)
	}
	if secs == 1 {
		return "1 second"
	}
	return fmt.Sprintf("%d seconds", secs)
}

// groupBurnDurationDefaultTips returns an English notification text, e.g.
// "Alice set disappearing messages to 1 day" or "Alice turned off disappearing messages".
func groupBurnDurationDefaultTips(opUser *sdkws.GroupMemberFullInfo, durationSecs int32) string {
	name := ""
	if opUser != nil {
		name = opUser.Nickname
		if name == "" {
			name = opUser.UserID
		}
	}
	if name == "" {
		name = "Someone"
	}
	if durationSecs <= 0 {
		return name + " turned off disappearing messages"
	}
	return name + " set disappearing messages to " + burnDurationText(durationSecs)
}

// GroupBurnDurationSetNotification broadcasts a GroupBurnDurationSetNotification (1524)
// to all group members when an admin changes the disappearing-message duration.
func (g *NotificationSender) GroupBurnDurationSetNotification(ctx context.Context, groupID string, durationSecs int32) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	groupInfo, err := g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	groupInfo.MsgBurnDuration = durationSecs
	var opUser *sdkws.GroupMemberFullInfo
	if err = g.fillOpUser(ctx, &opUser, groupID); err != nil {
		return
	}
	tips := &sdkws.GroupBurnDurationSetTips{
		Group:        groupInfo,
		OpUser:       opUser,
		DurationSecs: durationSecs,
		DefaultTips:  groupBurnDurationDefaultTips(opUser, durationSecs),
	}
	g.Notification(ctx, mcontext.GetOpUserID(ctx), groupID, constant.GroupBurnDurationSetNotification, tips)
}

// groupNeedVerificationDefaultTips returns an English text for a NeedVerification change.
// 仅影响分享链接入群：0/1=需审批，2(Directly)=免审直接入群。
func groupNeedVerificationDefaultTips(opUser *sdkws.GroupMemberFullInfo, needVerification int32) string {
	name := opUserName(opUser)
	if needVerification == constant.Directly {
		return name + " disabled invite-link join approval"
	}
	return name + " enabled invite-link join approval"
}

// GroupNeedVerificationSetNotification sends a GroupNeedVerificationSetNotification (1526)
// to all group members when an admin changes the join-approval setting.
func (g *NotificationSender) GroupNeedVerificationSetNotification(ctx context.Context, groupID string, needVerification int32) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	groupInfo, err := g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var opUser *sdkws.GroupMemberFullInfo
	if err = g.fillOpUser(ctx, &opUser, groupID); err != nil {
		return
	}
	tips := &sdkws.GroupNeedVerificationSetTips{
		Group:            groupInfo,
		OpUser:           opUser,
		NeedVerification: needVerification,
		DefaultTips:      groupNeedVerificationDefaultTips(opUser, needVerification),
	}
	g.Notification(ctx, mcontext.GetOpUserID(ctx), groupID, constant.GroupNeedVerificationSetNotification, tips)
}

// GroupPermissionChangedNotification broadcasts GroupPermissionChangedNotification (1530)
// when allowSendMsg / allowAddMember / allowPinMsg / allowMemberBurn actually change.
func (g *NotificationSender) GroupPermissionChangedNotification(ctx context.Context, tips *sdkws.GroupPermissionChangedTips) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if tips == nil || tips.Group == nil || len(tips.ChangedFields) == 0 {
		return
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	if tips.OperationTime == 0 {
		tips.OperationTime = time.Now().UnixMilli()
	}
	log.ZInfo(ctx, "GroupPermissionChangedNotification",
		"groupID", tips.Group.GroupID,
		"opUserID", mcontext.GetOpUserID(ctx),
		"changedFields", tips.ChangedFields,
	)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupPermissionChangedNotification, tips)
}

func (g *NotificationSender) GroupFaceURLSetNotification(ctx context.Context, tips *sdkws.GroupFaceURLSetTips) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	tips.DefaultTips = opUserName(tips.OpUser) + " has changed the group avatar"
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupFaceURLSetNotification, tips)
}

// memberInvitedDefaultTips returns an English notification text, e.g.
// "Alice invited Bob to join the group" or
// "Alice invited Bob, Charlie and 2 others to join the group".
func memberInvitedDefaultTips(inviter *sdkws.GroupMemberFullInfo, invited []*sdkws.GroupMemberFullInfo) string {
	inviterName := ""
	if inviter != nil {
		inviterName = inviter.Nickname
		if inviterName == "" {
			inviterName = inviter.UserID
		}
	}
	if inviterName == "" {
		inviterName = "Someone"
	}

	if len(invited) == 0 {
		return inviterName + " invited members to join the group"
	}

	const maxNames = 3
	names := make([]string, 0, len(invited))
	for _, u := range invited {
		if u == nil {
			continue
		}
		n := u.Nickname
		if n == "" {
			n = u.UserID
		}
		names = append(names, n)
	}
	if len(names) == 0 {
		return inviterName + " invited members to join the group"
	}

	var invitedStr string
	if len(names) <= maxNames {
		invitedStr = joinNames(names)
	} else {
		invitedStr = joinNames(names[:maxNames]) + fmt.Sprintf(" and %d others", len(names)-maxNames)
	}
	return inviterName + " invited " + invitedStr + " to join the group"
}

// joinNames joins a slice of names with commas and "and" before the last element.
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		result := ""
		for i, n := range names {
			if i == len(names)-1 {
				result += " and " + n
			} else if i == 0 {
				result = n
			} else {
				result += ", " + n
			}
		}
		return result
	}
}

func (g *NotificationSender) GroupInfoSetAnnouncementNotification(ctx context.Context, tips *sdkws.GroupInfoSetAnnouncementTips, sendMessage *bool) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupInfoSetAnnouncementNotification, tips, notification.WithRpcGetUserName(), notification.WithSendMessage(sendMessage))
}

func (g *NotificationSender) uuid() string {
	return uuid.New().String()
}

func (g *NotificationSender) getGroupRequest(ctx context.Context, groupID string, userID string) (*sdkws.GroupRequest, error) {
	request, err := g.db.TakeGroupRequest(ctx, groupID, userID)
	if err != nil {
		return nil, err
	}
	users, err := g.getUsersInfo(ctx, []string{userID})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, servererrs.ErrUserIDNotFound.WrapMsg(fmt.Sprintf("user %s not found", userID))
	}
	info, ok := users[0].(*sdkws.UserInfo)
	if !ok {
		info = &sdkws.UserInfo{
			UserID:   users[0].GetUserID(),
			Nickname: users[0].GetNickname(),
			FaceURL:  users[0].GetFaceURL(),
			Ex:       users[0].GetEx(),
		}
	}
	if g.resolveDisplayNickname != nil {
		if name, err := g.resolveDisplayNickname(ctx, mcontext.GetOpUserID(ctx), userID); err == nil && name != "" {
			info = &sdkws.UserInfo{
				UserID:   info.UserID,
				Nickname: name,
				FaceURL:  info.FaceURL,
				Ex:       info.Ex,
			}
		}
	}
	return convert.Db2PbGroupRequest(request, info, nil), nil
}

func (g *NotificationSender) JoinGroupApplicationNotification(ctx context.Context, req *pbgroup.JoinGroupReq, dbReq *model.GroupRequest) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	request, err := g.getGroupRequest(ctx, dbReq.GroupID, dbReq.UserID)
	if err != nil {
		log.ZError(ctx, "JoinGroupApplicationNotification getGroupRequest", err, "dbReq", dbReq)
		return
	}
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, req.GroupID)
	if err != nil {
		return
	}
	var user *sdkws.PublicUserInfo
	user, err = g.getUser(ctx, req.InviterUserID)
	if err != nil {
		return
	}
	userIDs, err := g.getGroupOwnerAndAdminUserID(ctx, req.GroupID)
	if err != nil {
		return
	}
	userIDs = append(userIDs, req.InviterUserID, mcontext.GetOpUserID(ctx))
	tips := &sdkws.JoinGroupApplicationTips{
		Group:     group,
		Applicant: user,
		ReqMsg:    req.ReqMessage,
		Uuid:      g.uuid(),
		Request:   request,
	}
	for _, userID := range datautil.Distinct(userIDs) {
		g.Notification(ctx, mcontext.GetOpUserID(ctx), userID, constant.JoinGroupApplicationNotification, tips)
	}
}

func (g *NotificationSender) MemberQuitNotification(ctx context.Context, member *sdkws.GroupMemberFullInfo) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, member.GroupID)
	if err != nil {
		return
	}
	tips := &sdkws.MemberQuitTips{Group: group, QuitUser: member}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, member.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), member.GroupID, constant.MemberQuitNotification, tips)
}

func (g *NotificationSender) GroupApplicationAcceptedNotification(ctx context.Context, req *pbgroup.GroupApplicationResponseReq) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	request, err := g.getGroupRequest(ctx, req.GroupID, req.FromUserID)
	if err != nil {
		log.ZError(ctx, "GroupApplicationAcceptedNotification getGroupRequest", err, "req", req)
		return
	}
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, req.GroupID)
	if err != nil {
		return
	}
	var userIDs []string
	userIDs, err = g.getGroupOwnerAndAdminUserID(ctx, req.GroupID)
	if err != nil {
		return
	}

	var opUser *sdkws.GroupMemberFullInfo
	if err = g.fillOpUser(ctx, &opUser, group.GroupID); err != nil {
		return
	}
	uid := g.uuid()
	for _, userID := range append(userIDs, req.FromUserID) {
		tips := &sdkws.GroupApplicationAcceptedTips{
			Group:     group,
			OpUser:    opUser,
			HandleMsg: req.HandledMsg,
			Uuid:      uid,
			Request:   request,
		}
		if userID == req.FromUserID {
			tips.ReceiverAs = applicantReceiver
		} else {
			tips.ReceiverAs = adminReceiver
		}
		g.Notification(ctx, mcontext.GetOpUserID(ctx), userID, constant.GroupApplicationAcceptedNotification, tips)
	}
}

func (g *NotificationSender) GroupApplicationRejectedNotification(ctx context.Context, req *pbgroup.GroupApplicationResponseReq) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	request, err := g.getGroupRequest(ctx, req.GroupID, req.FromUserID)
	if err != nil {
		log.ZError(ctx, "GroupApplicationAcceptedNotification getGroupRequest", err, "req", req)
		return
	}
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, req.GroupID)
	if err != nil {
		return
	}
	var userIDs []string
	userIDs, err = g.getGroupOwnerAndAdminUserID(ctx, req.GroupID)
	if err != nil {
		return
	}

	var opUser *sdkws.GroupMemberFullInfo
	if err = g.fillOpUser(ctx, &opUser, group.GroupID); err != nil {
		return
	}
	uid := g.uuid()
	for _, userID := range append(userIDs, req.FromUserID) {
		tips := &sdkws.GroupApplicationRejectedTips{
			Group:     group,
			OpUser:    opUser,
			HandleMsg: req.HandledMsg,
			Uuid:      uid,
			Request:   request,
		}
		if userID == req.FromUserID {
			tips.ReceiverAs = applicantReceiver
		} else {
			tips.ReceiverAs = adminReceiver
		}
		g.Notification(ctx, mcontext.GetOpUserID(ctx), userID, constant.GroupApplicationRejectedNotification, tips)
	}
}

func (g *NotificationSender) GroupOwnerTransferredNotification(ctx context.Context, req *pbgroup.TransferGroupOwnerReq) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, req.GroupID)
	if err != nil {
		return
	}
	opUserID := mcontext.GetOpUserID(ctx)
	var member map[string]*sdkws.GroupMemberFullInfo
	member, err = g.getGroupMemberMap(ctx, req.GroupID, []string{opUserID, req.NewOwnerUserID, req.OldOwnerUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupOwnerTransferredTips{
		Group:             group,
		OpUser:            member[opUserID],
		NewGroupOwner:     member[req.NewOwnerUserID],
		OldGroupOwnerInfo: member[req.OldOwnerUserID],
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, req.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupOwnerTransferredNotification, tips)
}

func (g *NotificationSender) MemberKickedNotification(ctx context.Context, tips *sdkws.MemberKickedTips, SendMessage *bool) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.MemberKickedNotification, tips, notification.WithSendMessage(SendMessage))
}

func (g *NotificationSender) GroupApplicationAgreeMemberEnterNotification(ctx context.Context, groupID string, SendMessage *bool, invitedOpUserID string, entrantUserID ...string) error {
	return g.groupApplicationAgreeMemberEnterNotification(ctx, groupID, SendMessage, invitedOpUserID, entrantUserID...)
}

func (g *NotificationSender) groupApplicationAgreeMemberEnterNotification(ctx context.Context, groupID string, SendMessage *bool, invitedOpUserID string, entrantUserID ...string) error {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()

	if !g.config.RpcConfig.EnableHistoryForNewMembers {
		conversationID := msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, groupID)
		maxSeq, err := g.msgClient.GetConversationMaxSeq(ctx, conversationID)
		if err != nil {
			return err
		}
		if err := g.msgClient.SetUserConversationsMinSeq(ctx, conversationID, entrantUserID, maxSeq+1); err != nil {
			return err
		}
	}
	if err := g.conversationClient.CreateGroupChatConversations(ctx, groupID, entrantUserID); err != nil {
		return err
	}

	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return err
	}
	users, err := g.getGroupMembers(ctx, groupID, entrantUserID)
	if err != nil {
		return err
	}

	tips := &sdkws.MemberInvitedTips{
		Group:           group,
		InvitedUserList: users,
	}
	opUserID := mcontext.GetOpUserID(ctx)
	if err = g.fillUserByUserID(ctx, opUserID, &tips.OpUser, tips.Group.GroupID); err != nil {
		return nil
	}
	if invitedOpUserID == opUserID {
		tips.InviterUser = tips.OpUser
	} else {
		if err = g.fillUserByUserID(ctx, invitedOpUserID, &tips.InviterUser, tips.Group.GroupID); err != nil {
			return err
		}
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	tips.DefaultTips = memberInvitedDefaultTips(tips.InviterUser, tips.InvitedUserList)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.MemberInvitedNotification, tips, notification.WithSendMessage(SendMessage))
	return nil
}

// memberEnterDefaultTips returns an English notification text based on how the
// user joined, e.g. "Alice joined the group via invite link".
func memberEnterDefaultTips(user *sdkws.GroupMemberFullInfo, joinSource int32) string {
	name := ""
	if user != nil {
		name = user.Nickname
		if name == "" {
			name = user.UserID
		}
	}
	if name == "" {
		name = "Someone"
	}
	switch joinSource {
	case constant.JoinByInviteLink:
		return name + " joined the group via invite link"
	case constant.JoinByQRCode:
		return name + " joined the group via QR code"
	case constant.JoinBySearch:
		return name + " joined the group"
	case constant.JoinByInvitation:
		return name + " joined the group"
	default:
		return name + " joined the group"
	}
}

// MemberEnterNotification sends a MemberEnterNotification (1510) to the group.
// joinSource is optional; pass constant.JoinByInviteLink (or another join-source
// constant) to include the appropriate English defaultTips.
func (g *NotificationSender) MemberEnterNotification(ctx context.Context, groupID string, entrantUserID string, joinSource ...int32) error {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()

	if !g.config.RpcConfig.EnableHistoryForNewMembers {
		conversationID := msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, groupID)
		maxSeq, err := g.msgClient.GetConversationMaxSeq(ctx, conversationID)
		if err != nil {
			return err
		}
		if err := g.msgClient.SetUserConversationsMinSeq(ctx, conversationID, []string{entrantUserID}, maxSeq+1); err != nil {
			return err
		}
	}
	if err := g.conversationClient.CreateGroupChatConversations(ctx, groupID, []string{entrantUserID}); err != nil {
		return err
	}
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return err
	}
	user, err := g.getGroupMember(ctx, groupID, entrantUserID)
	if err != nil {
		return err
	}

	src := int32(0)
	if len(joinSource) > 0 {
		src = joinSource[0]
	}
	tips := &sdkws.MemberEnterTips{
		Group:         group,
		EntrantUser:   user,
		OperationTime: time.Now().UnixMilli(),
		JoinSource:    src,
		DefaultTips:   memberEnterDefaultTips(user, src),
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.MemberEnterNotification, tips)
	return nil
}

func (g *NotificationSender) GroupDismissedNotification(ctx context.Context, tips *sdkws.GroupDismissedTips, SendMessage *bool) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupDismissedNotification, tips, notification.WithSendMessage(SendMessage))
}

func (g *NotificationSender) GroupMemberMutedNotification(ctx context.Context, groupID, groupMemberUserID string, mutedSeconds uint32) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var user map[string]*sdkws.GroupMemberFullInfo
	user, err = g.getGroupMemberMap(ctx, groupID, []string{mcontext.GetOpUserID(ctx), groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberMutedTips{
		Group: group, MutedSeconds: mutedSeconds,
		OpUser: user[mcontext.GetOpUserID(ctx)], MutedUser: user[groupMemberUserID],
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberMutedNotification, tips)
}

func (g *NotificationSender) GroupMemberCancelMutedNotification(ctx context.Context, groupID, groupMemberUserID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var user map[string]*sdkws.GroupMemberFullInfo
	user, err = g.getGroupMemberMap(ctx, groupID, []string{mcontext.GetOpUserID(ctx), groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberCancelMutedTips{Group: group, OpUser: user[mcontext.GetOpUserID(ctx)], MutedUser: user[groupMemberUserID]}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberCancelMutedNotification, tips)
}

func (g *NotificationSender) GroupMutedNotification(ctx context.Context, groupID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var users []*sdkws.GroupMemberFullInfo
	users, err = g.getGroupMembers(ctx, groupID, []string{mcontext.GetOpUserID(ctx)})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMutedTips{Group: group}
	if len(users) > 0 {
		tips.OpUser = users[0]
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, groupID)
	tips.DefaultTips = opUserName(tips.OpUser) + " muted all members"
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMutedNotification, tips)
}

func (g *NotificationSender) GroupCancelMutedNotification(ctx context.Context, groupID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var users []*sdkws.GroupMemberFullInfo
	users, err = g.getGroupMembers(ctx, groupID, []string{mcontext.GetOpUserID(ctx)})
	if err != nil {
		return
	}
	tips := &sdkws.GroupCancelMutedTips{Group: group}
	if len(users) > 0 {
		tips.OpUser = users[0]
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, groupID)
	tips.DefaultTips = opUserName(tips.OpUser) + " unmuted all members"
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupCancelMutedNotification, tips)
}

// GroupMemberRemarkUpdateNotification sends GroupMemberInfoSetNotification only to ownerUserID
// (as a single-chat notification), with ChangedUser.Nickname overridden by displayName
// (resolved as remark > firstName > nickname by the caller).
func (g *NotificationSender) GroupMemberRemarkUpdateNotification(ctx context.Context, groupID, friendUserID, ownerUserID, displayName string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	group, err := g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	changedUser, err := g.getGroupMember(ctx, groupID, friendUserID)
	if err != nil {
		return
	}
	if displayName != "" {
		changedUser.Nickname = displayName
	}
	tips := &sdkws.GroupMemberInfoSetTips{
		Group:       group,
		ChangedUser: changedUser,
	}
	g.setSortVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, groupID, &tips.GroupSortVersion)
	g.NotificationWithSessionType(ctx, ownerUserID, ownerUserID, constant.GroupMemberInfoSetNotification,
		constant.SingleChatType, tips, notification.WithGroupID(groupID))
}

func (g *NotificationSender) GroupMemberInfoSetNotification(ctx context.Context, groupID, groupMemberUserID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var user map[string]*sdkws.GroupMemberFullInfo
	user, err = g.getGroupMemberMap(ctx, groupID, []string{groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberInfoSetTips{Group: group, OpUser: user[mcontext.GetOpUserID(ctx)], ChangedUser: user[groupMemberUserID]}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setSortVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID, &tips.GroupSortVersion)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberInfoSetNotification, tips)
}

func (g *NotificationSender) GroupMemberSetToAdminNotification(ctx context.Context, groupID, groupMemberUserID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	user, err := g.getGroupMemberMap(ctx, groupID, []string{mcontext.GetOpUserID(ctx), groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberInfoSetTips{Group: group, OpUser: user[mcontext.GetOpUserID(ctx)], ChangedUser: user[groupMemberUserID]}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setSortVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID, &tips.GroupSortVersion)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberSetToAdminNotification, tips)
}

// GroupMessagePinnedNotification 向群聊下发一条置顶系统消息（ReadGroupChatType），全员可见。
// pinnedList 的按成员 minSeq/maxSeq 过滤由 GetGroupPinnedMessages 等拉取接口负责。
// pinType: 1=置顶，2=取消置顶
func (g *NotificationSender) GroupMessagePinnedNotification(ctx context.Context, groupID string, pinType int32,
	pinned *sdkws.GroupPinnedMsgInfo, pinnedList []*sdkws.GroupPinnedMsgInfo) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	groupInfo, err := g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var opUser *sdkws.GroupMemberFullInfo
	if err = g.fillOpUser(ctx, &opUser, groupID); err != nil {
		return
	}
	tips := &sdkws.GroupMessagePinnedTips{
		Group:       groupInfo,
		OpUser:      opUser,
		Type:        pinType,
		PinnedMsg:   pinned,
		PinnedList:  pinnedList,
		DefaultTips: pinnedMsgDefaultTips(opUser, pinned),
	}
	g.Notification(ctx, mcontext.GetOpUserID(ctx), groupID, constant.GroupMessagePinnedNotification, tips)
}

func (g *NotificationSender) GroupMemberSetToOrdinaryUserNotification(ctx context.Context, groupID, groupMemberUserID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var user map[string]*sdkws.GroupMemberFullInfo
	user, err = g.getGroupMemberMap(ctx, groupID, []string{mcontext.GetOpUserID(ctx), groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberInfoSetTips{Group: group, OpUser: user[mcontext.GetOpUserID(ctx)], ChangedUser: user[groupMemberUserID]}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setSortVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID, &tips.GroupSortVersion)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberSetToOrdinaryUserNotification, tips)
}
