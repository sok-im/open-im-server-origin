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

package conversation

import (
	"context"
	"fmt"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/notification"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/protocol/msg"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
)

type ConversationNotificationSender struct {
	*notification.NotificationSender
	groupClient  *rpcli.GroupClient
	userClient   *rpcli.UserClient
	adminUserIDs []string
}

func NewConversationNotificationSender(
	conf *config.Notification,
	msgClient *rpcli.MsgClient,
	groupClient *rpcli.GroupClient,
	userClient *rpcli.UserClient,
	adminUserIDs []string,
) *ConversationNotificationSender {
	return &ConversationNotificationSender{
		NotificationSender: notification.NewNotificationSender(conf, notification.WithRpcClient(func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error) {
			return msgClient.SendMsg(ctx, req)
		})),
		groupClient:  groupClient,
		userClient:   userClient,
		adminUserIDs: adminUserIDs,
	}
}

// SetPrivate invote.
func (c *ConversationNotificationSender) ConversationSetPrivateNotification(ctx context.Context, sendID, recvID string,
	isPrivateChat bool, conversationID string,
) {
	tips := &sdkws.ConversationSetPrivateTips{
		RecvID:         recvID,
		SendID:         sendID,
		IsPrivate:      isPrivateChat,
		ConversationID: conversationID,
	}

	c.Notification(ctx, sendID, recvID, constant.ConversationPrivateChatNotification, tips)
}

func (c *ConversationNotificationSender) ConversationChangeNotification(ctx context.Context, userID string, conversationIDs []string) {
	tips := &sdkws.ConversationUpdateTips{
		UserID:             userID,
		ConversationIDList: conversationIDs,
	}

	c.Notification(ctx, userID, userID, constant.ConversationChangeNotification, tips)
}

// ConversationE2EENotification 单聊会话创建时下发 E2EE 信令（contentType=1705），不依赖专用 Listener 回调。
func (c *ConversationNotificationSender) ConversationE2EENotification(ctx context.Context, sendID, recvID, conversationID string) {
	tips := &sdkws.ConversationSetPrivateTips{
		SendID:         sendID,
		RecvID:         recvID,
		ConversationID: conversationID,
	}
	c.Notification(ctx, sendID, sendID, constant.ConversationE2EENotification, tips)
	c.Notification(ctx, sendID, recvID, constant.ConversationE2EENotification, tips)
}

func (c *ConversationNotificationSender) ConversationUnreadChangeNotification(
	ctx context.Context,
	userID, conversationID string,
	unreadCountTime, hasReadSeq int64,
) {
	tips := &sdkws.ConversationHasReadTips{
		UserID:          userID,
		ConversationID:  conversationID,
		HasReadSeq:      hasReadSeq,
		UnreadCountTime: unreadCountTime,
	}

	c.Notification(ctx, userID, userID, constant.ConversationUnreadNotification, tips)
}

// BurnMsgsDeleteNotification 通知 recvUserID 按 seqs 删除本地消息。
// sendUserID 为触发焚烧的一方（阅读方），recvUserID 为需要执行删除的一方。
// 双方各调用一次，保证单聊两端客户端都删除本地缓存。
func (c *ConversationNotificationSender) BurnMsgsDeleteNotification(
	ctx context.Context,
	sendUserID, recvUserID, conversationID string,
	seqs []int64,
) {
	tips := &sdkws.DeleteMsgsTips{
		UserID:         sendUserID,
		ConversationID: conversationID,
		Seqs:           seqs,
	}
	c.Notification(ctx, sendUserID, recvUserID, constant.DeleteMsgsNotification, tips)
}

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

func (c *ConversationNotificationSender) fillOpUser(ctx context.Context, groupID string) (*sdkws.GroupMemberFullInfo, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, fmt.Errorf("op user id empty")
	}
	if authverify.IsManagerUserID(opUserID, c.adminUserIDs) {
		return &sdkws.GroupMemberFullInfo{
			GroupID:        groupID,
			UserID:         opUserID,
			RoleLevel:      constant.GroupAdmin,
			AppMangerLevel: constant.AppAdmin,
		}, nil
	}
	if c.groupClient != nil {
		member, err := c.groupClient.GetGroupMemberInfo(ctx, groupID, opUserID)
		if err == nil && member != nil {
			return member, nil
		}
	}
	if c.userClient == nil {
		return &sdkws.GroupMemberFullInfo{GroupID: groupID, UserID: opUserID}, nil
	}
	user, err := c.userClient.GetUserInfo(ctx, opUserID)
	if err != nil {
		return nil, err
	}
	return &sdkws.GroupMemberFullInfo{
		GroupID:  groupID,
		UserID:   opUserID,
		Nickname: user.Nickname,
		FaceURL:  user.FaceURL,
	}, nil
}

// GroupBurnDurationSetNotification broadcasts GroupBurnDurationSetNotification (1524) to the group.
func (c *ConversationNotificationSender) GroupBurnDurationSetNotification(ctx context.Context, groupID string, durationSecs int32) {
	if c.groupClient == nil || groupID == "" {
		return
	}
	groupInfo, err := c.groupClient.GetGroupInfo(ctx, groupID)
	if err != nil {
		log.ZWarn(ctx, "GroupBurnDurationSetNotification GetGroupInfo failed", err, "groupID", groupID)
		return
	}
	groupInfo.MsgBurnDuration = durationSecs
	opUser, err := c.fillOpUser(ctx, groupID)
	if err != nil {
		log.ZWarn(ctx, "GroupBurnDurationSetNotification fillOpUser failed", err, "groupID", groupID)
		return
	}
	tips := &sdkws.GroupBurnDurationSetTips{
		Group:        groupInfo,
		OpUser:       opUser,
		DurationSecs: durationSecs,
		DefaultTips:  groupBurnDurationDefaultTips(opUser, durationSecs),
	}
	c.Notification(ctx, mcontext.GetOpUserID(ctx), groupID, constant.GroupBurnDurationSetNotification, tips)
}
