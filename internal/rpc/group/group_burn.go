package group

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/protocol/constant"
	pbconversation "github.com/openimsdk/protocol/conversation"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/protocol/wrapperspb"
	"github.com/openimsdk/tools/log"
)

// syncOwnerConversationBurnOnCreateGroup 群主若已设置个人阅后即焚（用户全局 MsgBurnDuration），
// 创建群后为群主同步群会话级 BurnDuration，供 msgtransfer recordGroupBurnOnSend 读取。
func (s *groupServer) syncOwnerConversationBurnOnCreateGroup(ctx context.Context, groupID, ownerUserID string, owner *sdkws.UserInfo) {
	if owner == nil || owner.MsgBurnDuration <= 0 {
		return
	}
	conv := &pbconversation.ConversationReq{
		ConversationID:   msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, groupID),
		ConversationType: constant.ReadGroupChatType,
		GroupID:          groupID,
		BurnDuration:     &wrapperspb.Int32Value{Value: owner.MsgBurnDuration},
	}
	if err := s.conversationClient.SetConversations(ctx, []string{ownerUserID}, conv); err != nil {
		log.ZWarn(ctx, "syncOwnerConversationBurnOnCreateGroup SetConversations failed", err,
			"groupID", groupID, "ownerUserID", ownerUserID, "burnDuration", owner.MsgBurnDuration)
	}
}
