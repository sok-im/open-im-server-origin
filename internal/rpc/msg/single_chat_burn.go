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
// 提前静默写入会话级 BurnDuration（不下发 1701；早于 msgtransfer 创建会话）。
// 阅后即焚提示通知仅在用户对已有会话显式设置时由 SetConversationBurn / SetConversations 下发。
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
	// 会话已存在：留给用户显式 set_burn / SetConversations，避免自动落地时误发 1701。
	if err == nil && conv != nil {
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
