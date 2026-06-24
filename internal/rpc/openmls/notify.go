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

// mlsExtensionGroupInitTrigger is the CustomElem.extension value for the
// server-side group-init trigger. When the Group RPC creates a new group it
// calls InitGroupTrigger which sends this notification to the creator's
// devices, prompting them to perform the MLS group-creation flow (fetch key
// packages, create MLS group, submit initial Commit + Welcome messages).
const mlsExtensionGroupInitTrigger = "mls_group_init_trigger"

// mlsExtensionAddMemberTrigger is the CustomElem.extension value sent to the
// operator's devices when new members are invited to a group. The operator's
// MLS client must fetch KeyPackages for the new members, create an Add-Commit
// + Welcome bundle, and call SubmitCommit.
const mlsExtensionAddMemberTrigger = "mls_add_member_trigger"

// mlsExtensionRemoveMemberTrigger is the CustomElem.extension value sent to
// the operator's devices when members are kicked from a group. The operator's
// MLS client must create a Remove-Commit and call SubmitCommit so that kicked
// members lose access to subsequent messages (forward secrecy).
const mlsExtensionRemoveMemberTrigger = "mls_remove_member_trigger"

// mlsHandshakeMsgOptions returns message routing options for MLS handshake
// messages (Welcome / Commit broadcast). These messages are protocol-level and
// must NOT appear in chat history, unread counts, or conversation lists — they
// route through the notification (n_) conversation channel, identical to RTC
// signaling messages.
//
// IsHistory=true persists handshake messages so offline peers can catch up MLS
// epoch state after reconnecting (e.g. 1:1 sessions where Commit is not group-
// broadcast). Without persistence, a Welcome/Commit delivered while the peer is
// offline is lost and subsequent E2EE messages cannot be decrypted.
func mlsHandshakeMsgOptions() map[string]bool {
	opts := make(map[string]bool, 7)
	// IsNotNotification=false routes through the n_ notification conversation,
	// bypassing friend/block checks that apply to normal chat messages.
	datautil.SetSwitchFromOptions(opts, constant.IsNotNotification, false)
	datautil.SetSwitchFromOptions(opts, constant.IsSendMsg, false)
	datautil.SetSwitchFromOptions(opts, constant.IsHistory, true)
	datautil.SetSwitchFromOptions(opts, constant.IsPersistent, true)
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

// sendGroupInitTrigger delivers a mls_group_init_trigger CustomMessage to the
// creator's user account (all their online devices). The payload is a JSON
// object containing the groupID and the list of member userIDs that the
// creator's device must fetch KeyPackages for and add into the MLS group.
//
// This is a fire-and-log call: errors are logged but not returned so that the
// gRPC handler can proceed without failing the Group RPC.
func (s *openMLSServer) sendGroupInitTrigger(ctx context.Context, groupID, creatorUserID string, memberUserIDs []string) {
	type triggerPayload struct {
		GroupID       string   `json:"groupID"`
		MemberUserIDs []string `json:"memberUserIDs"`
	}
	payload, err := json.Marshal(triggerPayload{
		GroupID:       groupID,
		MemberUserIDs: memberUserIDs,
	})
	if err != nil {
		log.ZError(ctx, "sendGroupInitTrigger: marshal payload failed", err, "groupID", groupID)
		return
	}
	contentMap := map[string]string{
		"data":        string(payload),
		"description": "[MLS Group Init]",
		"extension":   mlsExtensionGroupInitTrigger,
	}
	content, err := json.Marshal(contentMap)
	if err != nil {
		log.ZError(ctx, "sendGroupInitTrigger: marshal content failed", err, "groupID", groupID)
		return
	}
	now := time.Now().UnixMilli()
	msgData := &sdkws.MsgData{
		SendID:      creatorUserID,
		RecvID:      creatorUserID,
		GroupID:     "",
		SessionType: int32(constant.SingleChatType),
		ContentType: int32(constant.Custom),
		MsgFrom:     int32(constant.SysMsgType),
		Content:     content,
		CreateTime:  now,
		SendTime:    now,
		ServerMsgID: uuid.New().String(),
		ClientMsgID: uuid.New().String(),
		Options:     mlsHandshakeMsgOptions(),
		OfflinePushInfo: &sdkws.OfflinePushInfo{
			Title: "[加密群组初始化]",
			Desc:  "[MLS Group Init]",
		},
	}
	if _, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData}); err != nil {
		log.ZError(ctx, "sendGroupInitTrigger: SendMsg failed", err,
			"groupID", groupID, "creatorUserID", creatorUserID)
		return
	}
	log.ZDebug(ctx, "sendGroupInitTrigger: ok",
		"groupID", groupID, "creatorUserID", creatorUserID, "memberCount", len(memberUserIDs))
}

// sendAddMemberTrigger delivers a mls_add_member_trigger CustomMessage to the
// operator's user account (all their online devices). The payload contains the
// groupID and the list of newly added member userIDs that the operator's device
// must fetch KeyPackages for, create an Add-Commit + Welcome bundle, and submit
// via SubmitCommit.
//
// Fire-and-log: errors are logged but not returned so that the Group RPC
// continues without being blocked by MLS signalling failures.
func (s *openMLSServer) sendAddMemberTrigger(ctx context.Context, groupID, operatorUserID string, newMemberUserIDs []string) {
	type triggerPayload struct {
		GroupID          string   `json:"groupID"`
		NewMemberUserIDs []string `json:"newMemberUserIDs"`
	}
	payload, err := json.Marshal(triggerPayload{
		GroupID:          groupID,
		NewMemberUserIDs: newMemberUserIDs,
	})
	if err != nil {
		log.ZError(ctx, "sendAddMemberTrigger: marshal payload failed", err, "groupID", groupID)
		return
	}
	contentMap := map[string]string{
		"data":        string(payload),
		"description": "[MLS Add Member]",
		"extension":   mlsExtensionAddMemberTrigger,
	}
	content, err := json.Marshal(contentMap)
	if err != nil {
		log.ZError(ctx, "sendAddMemberTrigger: marshal content failed", err, "groupID", groupID)
		return
	}
	now := time.Now().UnixMilli()
	msgData := &sdkws.MsgData{
		SendID:      operatorUserID,
		RecvID:      operatorUserID,
		GroupID:     "",
		SessionType: int32(constant.SingleChatType),
		ContentType: int32(constant.Custom),
		MsgFrom:     int32(constant.SysMsgType),
		Content:     content,
		CreateTime:  now,
		SendTime:    now,
		ServerMsgID: uuid.New().String(),
		ClientMsgID: uuid.New().String(),
		Options:     mlsHandshakeMsgOptions(),
		OfflinePushInfo: &sdkws.OfflinePushInfo{
			Title: "[加密群组成员添加]",
			Desc:  "[MLS Add Member]",
		},
	}
	if _, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData}); err != nil {
		log.ZError(ctx, "sendAddMemberTrigger: SendMsg failed", err,
			"groupID", groupID, "operatorUserID", operatorUserID)
		return
	}
	log.ZDebug(ctx, "sendAddMemberTrigger: ok",
		"groupID", groupID, "operatorUserID", operatorUserID, "newMemberCount", len(newMemberUserIDs))
}

// sendRemoveMemberTrigger delivers a mls_remove_member_trigger CustomMessage to
// the operator's user account (all their online devices). The payload contains
// the groupID and the list of removed member userIDs. The operator's MLS client
// must create a Remove-Commit and call SubmitCommit to rotate the group epoch,
// preventing kicked members from decrypting future messages.
//
// Fire-and-log: errors are logged but not returned so that the Group RPC
// continues without being blocked by MLS signalling failures.
func (s *openMLSServer) sendRemoveMemberTrigger(ctx context.Context, groupID, operatorUserID string, removedMemberUserIDs []string) {
	type triggerPayload struct {
		GroupID              string   `json:"groupID"`
		RemovedMemberUserIDs []string `json:"removedMemberUserIDs"`
	}
	payload, err := json.Marshal(triggerPayload{
		GroupID:              groupID,
		RemovedMemberUserIDs: removedMemberUserIDs,
	})
	if err != nil {
		log.ZError(ctx, "sendRemoveMemberTrigger: marshal payload failed", err, "groupID", groupID)
		return
	}
	contentMap := map[string]string{
		"data":        string(payload),
		"description": "[MLS Remove Member]",
		"extension":   mlsExtensionRemoveMemberTrigger,
	}
	content, err := json.Marshal(contentMap)
	if err != nil {
		log.ZError(ctx, "sendRemoveMemberTrigger: marshal content failed", err, "groupID", groupID)
		return
	}
	now := time.Now().UnixMilli()
	msgData := &sdkws.MsgData{
		SendID:      operatorUserID,
		RecvID:      operatorUserID,
		GroupID:     "",
		SessionType: int32(constant.SingleChatType),
		ContentType: int32(constant.Custom),
		MsgFrom:     int32(constant.SysMsgType),
		Content:     content,
		CreateTime:  now,
		SendTime:    now,
		ServerMsgID: uuid.New().String(),
		ClientMsgID: uuid.New().String(),
		Options:     mlsHandshakeMsgOptions(),
		OfflinePushInfo: &sdkws.OfflinePushInfo{
			Title: "[加密群组成员移除]",
			Desc:  "[MLS Remove Member]",
		},
	}
	if _, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData}); err != nil {
		log.ZError(ctx, "sendRemoveMemberTrigger: SendMsg failed", err,
			"groupID", groupID, "operatorUserID", operatorUserID)
		return
	}
	log.ZDebug(ctx, "sendRemoveMemberTrigger: ok",
		"groupID", groupID, "operatorUserID", operatorUserID, "removedMemberCount", len(removedMemberUserIDs))
}
