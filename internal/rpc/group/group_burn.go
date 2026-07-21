package group

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/protocol/constant"
	pbconversation "github.com/openimsdk/protocol/conversation"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/protocol/wrapperspb"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
)

// syncOwnerConversationBurnOnCreateGroup 群主若已设置个人阅后即焚（用户全局 MsgBurnDuration），
// 建群后为群主同步群会话级 BurnDuration。群级 MsgBurnDuration 与 1524 由 CreateGroup 负责。
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
	// 服务端内部同步，使用 admin 身份绕过阅后即焚权限校验（操作者未必是群主）。
	// 新建会话不发 1524；CreateGroup 会在 1501 后以群主身份下发。
	adminCtx := ctx
	if len(s.config.Share.IMAdminUserID) > 0 {
		adminCtx = mcontext.WithOpUserIDContext(ctx, s.config.Share.IMAdminUserID[0])
	}
	if err := s.conversationClient.SetConversations(adminCtx, []string{ownerUserID}, conv); err != nil {
		log.ZWarn(ctx, "syncOwnerConversationBurnOnCreateGroup SetConversations failed", err,
			"groupID", groupID, "ownerUserID", ownerUserID, "burnDuration", owner.MsgBurnDuration)
	}
}
