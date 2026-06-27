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

package msg

import (
	"context"

	conversationutil "github.com/openimsdk/open-im-server/v3/pkg/util/conversationutil"
	"github.com/openimsdk/protocol/constant"
	pbconversation "github.com/openimsdk/protocol/conversation"
	"github.com/openimsdk/protocol/wrapperspb"
	"github.com/openimsdk/tools/log"
)

// ensureSenderSingleChatBurn 发送单聊消息前，若发送者已设置个人阅后即焚且会话尚未开启，
// 提前写入会话级 BurnDuration 并通知客户端（早于 msgtransfer 创建会话的路径）。
func (m *msgServer) ensureSenderSingleChatBurn(ctx context.Context, sendID, recvID string) {
	if sendID == "" || recvID == "" || sendID == recvID {
		return
	}
	sender, err := m.UserLocalCache.GetUserInfo(ctx, sendID)
	if err != nil {
		log.ZWarn(ctx, "ensureSenderSingleChatBurn GetUserInfo failed", err, "sendID", sendID)
		return
	}
	if sender == nil || sender.MsgBurnDuration <= 0 {
		return
	}
	conversationID := conversationutil.GenConversationIDForSingle(sendID, recvID)
	conv, err := m.ConversationLocalCache.GetConversation(ctx, sendID, conversationID)
	if err == nil && conv != nil && conv.BurnDuration > 0 {
		return
	}
	convReq := &pbconversation.ConversationReq{
		ConversationID:   conversationID,
		ConversationType: constant.SingleChatType,
		UserID:           recvID,
		BurnDuration:     &wrapperspb.Int32Value{Value: sender.MsgBurnDuration},
	}
	if err := m.conversationClient.SetConversations(ctx, []string{sendID}, convReq); err != nil {
		log.ZWarn(ctx, "ensureSenderSingleChatBurn SetConversations failed", err,
			"sendID", sendID, "recvID", recvID, "conversationID", conversationID,
			"burnDuration", sender.MsgBurnDuration)
	}
}
