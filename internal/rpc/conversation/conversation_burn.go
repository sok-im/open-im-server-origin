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

	dbModel "github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/log"
)

func (c *conversationServer) senderMsgBurnDuration(ctx context.Context, sendID string) int32 {
	if sendID == "" {
		return 0
	}
	sender, err := c.userClient.GetUserInfo(ctx, sendID)
	if err != nil {
		log.ZWarn(ctx, "senderMsgBurnDuration GetUserInfo failed", err, "sendID", sendID)
		return 0
	}
	if sender == nil {
		return 0
	}
	return sender.MsgBurnDuration
}

func applySenderBurnToConversation(conv *dbModel.Conversation, burnDuration int32) {
	if burnDuration <= 0 {
		return
	}
	conv.BurnDuration = burnDuration
	conv.IsPrivateChat = true
}

// syncSenderConversationBurnOnCreateSingleChat 发起单聊时，若发送者已设置个人阅后即焚（用户全局 MsgBurnDuration），
// 为新会话静默同步会话级 BurnDuration（不下发 1701），供 recordBurnDeadlines 读取。
func (c *conversationServer) syncSenderConversationBurnOnCreateSingleChat(
	ctx context.Context, sendID, recvID, conversationID string, burnDuration int32,
) {
	if burnDuration <= 0 {
		return
	}
	conv := dbModel.Conversation{
		ConversationID:   conversationID,
		ConversationType: constant.SingleChatType,
		BurnDuration:     burnDuration,
		IsPrivateChat:    true,
		UserID:           recvID,
	}
	if err := c.syncSingleChatPrivateSettings(
		ctx,
		[]string{sendID},
		recvID,
		conversationID,
		constant.SingleChatType,
		conv,
		true,
		false,
	); err != nil {
		log.ZWarn(ctx, "syncSenderConversationBurnOnCreateSingleChat syncSingleChatPrivateSettings failed", err,
			"sendID", sendID, "recvID", recvID, "conversationID", conversationID, "burnDuration", burnDuration)
	}
}
