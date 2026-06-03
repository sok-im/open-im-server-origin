// Copyright © 2026 OpenIM. All rights reserved.
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

package openmls

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/protocol/constant"
	pbmsg "github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
)

// mlsExtensionHandshake is the CustomElem.extension value used for MLS protocol
// messages (Welcome and Commit). Clients filter on this to distinguish MLS
// handshake messages from regular E2EE payload messages ("e2ee").
const mlsExtensionHandshake = "mls_handshake"

// mlsHandshakeMsgOptions returns message routing options for MLS handshake
// messages (Welcome / Commit broadcast). These messages are protocol-level and
// must NOT appear in chat history, unread counts, or conversation lists — they
// route through the notification (n_) conversation channel, identical to RTC
// signaling messages.
func mlsHandshakeMsgOptions() map[string]bool {
	opts := make(map[string]bool, 7)
	// IsNotNotification=false routes through the n_ notification conversation,
	// bypassing friend/block checks that apply to normal chat messages.
	datautil.SetSwitchFromOptions(opts, constant.IsNotNotification, false)
	datautil.SetSwitchFromOptions(opts, constant.IsSendMsg, false)
	datautil.SetSwitchFromOptions(opts, constant.IsHistory, false)
	datautil.SetSwitchFromOptions(opts, constant.IsPersistent, false)
	datautil.SetSwitchFromOptions(opts, constant.IsUnreadCount, false)
	datautil.SetSwitchFromOptions(opts, constant.IsConversationUpdate, false)
	datautil.SetSwitchFromOptions(opts, constant.IsSenderConversationUpdate, false)
	datautil.SetSwitchFromOptions(opts, constant.IsSenderSync, false)
	return opts
}

// mlsCustomContent marshals a CustomElem-compatible JSON payload that the client
// SDK will expose as a custom message with extension="mls_handshake".
//
//   - dataB64:  the MLS wire payload (base64-encoded TLS-serialized bytes)
//   - desc:     plain-text fallback description shown in push previews
func mlsCustomContent(dataB64, desc string) ([]byte, error) {
	m := map[string]string{
		"data":        dataB64,
		"description": desc,
		"extension":   mlsExtensionHandshake,
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, errs.New("marshal mls custom content").Wrap()
	}
	return b, nil
}

// sendMLSMsg delivers a single MLS handshake message (Welcome or Commit) to
// one recipient user via the msg RPC service. The message is routed as a
// system-issued CustomMessage that bypasses history, unread counts, and friend
// checks (notification channel).
//
//   - senderID:    the user initiating the MLS operation (commit author)
//   - recvID:      target userID (for group broadcasts, pass the groupID here
//     and set sessionType=ReadGroupChatType; msg service fans out)
//   - groupID:     empty for 1:1, set for group sessions
//   - sessionType: constant.SingleChatType or constant.ReadGroupChatType
//   - payloadB64:  base64-encoded TLS bytes (commit or welcome)
//   - desc:        push preview text, e.g. "[MLS Commit]" or "[MLS Welcome]"
func (s *openMLSServer) sendMLSMsg(ctx context.Context, senderID, recvID, groupID string, sessionType int32, payloadB64, desc string) error {
	content, err := mlsCustomContent(payloadB64, desc)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	msgData := &sdkws.MsgData{
		SendID:      senderID,
		RecvID:      recvID,
		GroupID:     groupID,
		SessionType: sessionType,
		ContentType: int32(constant.Custom),
		MsgFrom:     int32(constant.SysMsgType),
		Content:     content,
		CreateTime:  now,
		SendTime:    now,
		ServerMsgID: uuid.New().String(),
		ClientMsgID: uuid.New().String(),
		Options:     mlsHandshakeMsgOptions(),
		OfflinePushInfo: &sdkws.OfflinePushInfo{
			Title: "[加密消息]",
			Desc:  desc,
		},
	}
	if _, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData}); err != nil {
		log.ZError(ctx, "MLS sendMLSMsg failed", err,
			"senderID", senderID, "recvID", recvID, "groupID", groupID,
			"sessionType", sessionType, "desc", desc)
		return err
	}
	log.ZDebug(ctx, "MLS sendMLSMsg ok",
		"senderID", senderID, "recvID", recvID, "groupID", groupID,
		"sessionType", sessionType, "desc", desc)
	return nil
}
