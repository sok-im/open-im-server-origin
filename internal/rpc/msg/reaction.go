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

package msg

import (
	"context"
	"strings"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
)

// defaultReactionEmojiAllowlist 表情反应白名单（v1 仅允许 Unicode 快捷集合）。
// 与客户端 Phase 1 的快捷条保持一致，并预留少量常用 emoji。
var defaultReactionEmojiAllowlist = map[string]struct{}{
	"👍": {}, "👎": {}, "❤️": {}, "🔥": {}, "🥰": {}, "👏": {}, "😁": {},
	"😂": {}, "😮": {}, "😢": {}, "🎉": {}, "🙏": {}, "💯": {}, "👀": {},
}

const (
	reactionActionAdd    = "add"
	reactionActionRemove = "remove"
)

// reactionAccess 描述一次反应操作解析出的会话上下文。
type reactionAccess struct {
	sessionType int32
	groupID     string
	recvID      string // 通知接收方：单聊为对端 userID，群聊为 groupID
}

// checkReactionAccess 校验调用者是否有权对该会话内消息进行反应，并解析通知路由信息。
func (m *msgServer) checkReactionAccess(ctx context.Context, conversationID, opUserID string) (*reactionAccess, error) {
	switch {
	case strings.HasPrefix(conversationID, "si_"):
		ids := strings.Split(strings.TrimPrefix(conversationID, "si_"), "_")
		if len(ids) != 2 || ids[0] == "" || ids[1] == "" {
			return nil, errs.ErrArgs.WrapMsg("invalid single conversationID", "conversationID", conversationID)
		}
		if !authverify.IsAppManagerUid(ctx, m.config.Share.IMAdminUserID) {
			if opUserID != ids[0] && opUserID != ids[1] {
				return nil, errs.ErrNoPermission.WrapMsg("not a participant of the conversation")
			}
		}
		recvID := ids[0]
		if recvID == opUserID {
			recvID = ids[1]
		}
		return &reactionAccess{sessionType: constant.SingleChatType, recvID: recvID}, nil
	case strings.HasPrefix(conversationID, "sg_"), strings.HasPrefix(conversationID, "g_"):
		groupID := strings.TrimPrefix(strings.TrimPrefix(conversationID, "sg_"), "g_")
		if groupID == "" {
			return nil, errs.ErrArgs.WrapMsg("invalid group conversationID", "conversationID", conversationID)
		}
		if !authverify.IsAppManagerUid(ctx, m.config.Share.IMAdminUserID) {
			if _, err := m.GroupLocalCache.GetGroupMember(ctx, groupID, opUserID); err != nil {
				return nil, errs.ErrNoPermission.WrapMsg("not a member of the group", "groupID", groupID)
			}
		}
		return &reactionAccess{sessionType: constant.ReadGroupChatType, groupID: groupID, recvID: groupID}, nil
	default:
		return nil, errs.ErrArgs.WrapMsg("unsupported conversationID", "conversationID", conversationID)
	}
}

// aggregateReactions 将反应记录按 emoji 首次出现顺序聚合为快照，并返回版本号。
// version 取所有记录 update_time 的最大毫秒值；无记录时为 0。
func aggregateReactions(rows []*model.MessageReaction, opUserID string, detail bool) ([]*msg.ReactionInfo, int64) {
	order := make([]string, 0)
	index := make(map[string]*msg.ReactionInfo)
	var version int64
	for _, r := range rows {
		if v := r.UpdateTime.UnixMilli(); v > version {
			version = v
		}
		info, ok := index[r.Emoji]
		if !ok {
			info = &msg.ReactionInfo{Emoji: r.Emoji}
			index[r.Emoji] = info
			order = append(order, r.Emoji)
		}
		info.Count++
		if detail {
			info.UserIDs = append(info.UserIDs, r.UserID)
		}
		if r.UserID == opUserID {
			info.IncludesMe = true
		}
	}
	result := make([]*msg.ReactionInfo, 0, len(order))
	for _, emoji := range order {
		result = append(result, index[emoji])
	}
	return result, version
}

func (m *msgServer) SetMessageReaction(ctx context.Context, req *msg.SetMessageReactionReq) (*msg.SetMessageReactionResp, error) {
	if _, ok := defaultReactionEmojiAllowlist[req.Emoji]; !ok {
		return nil, errs.ErrArgs.WrapMsg("emoji not in allowlist", "emoji", req.Emoji)
	}
	opUserID := mcontext.GetOpUserID(ctx)
	access, err := m.checkReactionAccess(ctx, req.ConversationID, opUserID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	switch req.Action {
	case reactionActionAdd:
		if err := m.messageReactionDB.Set(ctx, req.ConversationID, req.ClientMsgID, opUserID, req.Emoji, now); err != nil {
			return nil, err
		}
	case reactionActionRemove:
		if _, err := m.messageReactionDB.Remove(ctx, req.ConversationID, req.ClientMsgID, opUserID, req.Emoji); err != nil {
			return nil, err
		}
	default:
		return nil, errs.ErrArgs.WrapMsg("action must be add or remove", "action", req.Action)
	}

	rows, err := m.messageReactionDB.FindByMessage(ctx, req.ConversationID, req.ClientMsgID)
	if err != nil {
		return nil, err
	}
	reactions, _ := aggregateReactions(rows, opUserID, true)
	// 变更时刻作为权威 version：始终 >= 库中任一 update_time，便于客户端按 version 覆盖与丢弃旧信令。
	version := now.UnixMilli()

	m.sendReactionUpdatedNotification(ctx, opUserID, access, req.ConversationID, req.ClientMsgID, req.Emoji, req.Action, reactions, version)

	return &msg.SetMessageReactionResp{
		ClientMsgID: req.ClientMsgID,
		Reactions:   reactions,
		Version:     version,
	}, nil
}

func (m *msgServer) GetMessageReactions(ctx context.Context, req *msg.GetMessageReactionsReq) (*msg.GetMessageReactionsResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if _, err := m.checkReactionAccess(ctx, req.ConversationID, opUserID); err != nil {
		return nil, err
	}
	rows, err := m.messageReactionDB.FindByMessage(ctx, req.ConversationID, req.ClientMsgID)
	if err != nil {
		return nil, err
	}
	reactions, version := aggregateReactions(rows, opUserID, req.Detail)
	return &msg.GetMessageReactionsResp{
		ClientMsgID: req.ClientMsgID,
		Reactions:   reactions,
		Version:     version,
	}, nil
}

func (m *msgServer) BatchGetMessageReactions(ctx context.Context, req *msg.BatchGetMessageReactionsReq) (*msg.BatchGetMessageReactionsResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if _, err := m.checkReactionAccess(ctx, req.ConversationID, opUserID); err != nil {
		return nil, err
	}
	rows, err := m.messageReactionDB.FindByMessages(ctx, req.ConversationID, req.ClientMsgIDs)
	if err != nil {
		return nil, err
	}
	grouped := make(map[string][]*model.MessageReaction)
	for _, r := range rows {
		grouped[r.ClientMsgID] = append(grouped[r.ClientMsgID], r)
	}
	result := make(map[string]*msg.MessageReactions, len(grouped))
	for clientMsgID, msgRows := range grouped {
		reactions, version := aggregateReactions(msgRows, opUserID, req.Detail)
		result[clientMsgID] = &msg.MessageReactions{Reactions: reactions, Version: version}
	}
	return &msg.BatchGetMessageReactionsResp{Reactions: result}, nil
}

// sendReactionUpdatedNotification 推送 message.reaction.updated 全量聚合快照。
// 通过 MsgReactionUpdatedNotification 走系统通知通道：不计未读、不更新会话摘要。
func (m *msgServer) sendReactionUpdatedNotification(ctx context.Context, actorUserID string, access *reactionAccess,
	conversationID, clientMsgID, emoji, action string, reactions []*msg.ReactionInfo, version int64) {
	tips := &msg.MessageReactionUpdatedTips{
		ConversationID: conversationID,
		ClientMsgID:    clientMsgID,
		Reactions:      reactions,
		Version:        version,
		ActorUserID:    actorUserID,
		Emoji:          emoji,
		Action:         action,
	}
	m.notificationSender.NotificationWithSessionType(ctx, actorUserID, access.recvID,
		constant.MsgReactionUpdatedNotification, access.sessionType, tips)
}
