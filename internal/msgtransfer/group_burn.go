package msgtransfer

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"
)

// recordGroupBurnOnSend 在群消息分配 seq 后写入定时删除截止时间。
//
// 销毁时长优先级（与单聊 recordBurnDeadlines 一致）：
//  1. 发送者在该群会话上的 BurnDuration（/conversation/set_burn 或 UpdateConversation）；
//  2. 发送者用户全局 MsgBurnDuration。
//
// 通知类消息（ContentType 1000~5000，含群内 tips）不参与阅后即焚。
// 失败仅记日志，不影响消息主流程。
func (och *OnlineHistoryRedisConsumerHandler) recordGroupBurnOnSend(ctx context.Context, conversationID string, msgs []*sdkws.MsgData) {
	if och.groupMsgBurnRecordDB == nil || len(msgs) == 0 {
		log.ZDebug(ctx, "recordGroupBurnOnSend", "reason", "groupMsgBurnRecordDB is nil or msgs is empty")
		return
	}
	msg := msgs[0]
	if msg.SessionType != constant.ReadGroupChatType || msg.GroupID == "" {
		log.ZDebug(ctx, "recordGroupBurnOnSend", "reason", "msg is not a group chat message")
		return
	}

	seqSenderID := make(map[int64]string, len(msgs))
	msgSendTimeMs := make(map[int64]int64, len(msgs))
	for _, m := range msgs {
		if m == nil || m.Seq <= 0 {
			continue
		}
		if msgprocessor.IsNotificationContentType(m.ContentType) {
			continue
		}
		seqSenderID[m.Seq] = m.SendID
		sendTimeMs := m.SendTime
		if sendTimeMs <= 0 {
			sendTimeMs = msg.SendTime
		}
		if sendTimeMs <= 0 {
			sendTimeMs = time.Now().UnixMilli()
		}
		msgSendTimeMs[m.Seq] = sendTimeMs
	}
	if len(seqSenderID) == 0 {
		log.ZDebug(ctx, "recordGroupBurnOnSend", "reason", "seqSenderID is empty")
		return
	}

	senderBurnSeconds, ok := och.getSenderConversationBurnSeconds(ctx, conversationID, seqSenderID)
	if !ok {
		log.ZDebug(ctx, "recordGroupBurnOnSend", "reason", "getSenderConversationBurnSeconds failed",
			"conversationID", conversationID, "groupID", msg.GroupID)
		return
	}

	seqBurnEndTimeMs := make(map[int64]int64, len(seqSenderID))
	for seq, senderID := range seqSenderID {
		burnSeconds, ok := senderBurnSeconds[senderID]
		if !ok || burnSeconds <= 0 {
			continue
		}
		seqBurnEndTimeMs[seq] = msgSendTimeMs[seq] + int64(burnSeconds)*1000
	}
	if len(seqBurnEndTimeMs) == 0 {
		log.ZDebug(ctx, "recordGroupBurnOnSend", "reason", "no burn duration configured",
			"conversationID", conversationID, "groupID", msg.GroupID, "senderBurnSeconds", senderBurnSeconds)
		return
	}

	if err := och.groupMsgBurnRecordDB.UpsertOnSend(ctx, msg.GroupID, seqSenderID, seqBurnEndTimeMs); err != nil {
		log.ZError(ctx, "recordGroupBurnOnSend UpsertOnSend failed", err,
			"groupID", msg.GroupID, "seqBurnEndTimeMs", seqBurnEndTimeMs)
	} else {
		log.ZDebug(ctx, "recordGroupBurnOnSend UpsertOnSend success",
			"groupID", msg.GroupID, "seqBurnEndTimeMs", seqBurnEndTimeMs)
	}
}

func (och *OnlineHistoryRedisConsumerHandler) getSenderConversationBurnSeconds(ctx context.Context, conversationID string, seqSenderID map[int64]string) (map[string]int32, bool) {
	if och.conversationClient == nil {
		return nil, false
	}
	seen := make(map[string]struct{}, len(seqSenderID))
	senderIDs := make([]string, 0, len(seqSenderID))
	for _, senderID := range seqSenderID {
		if senderID == "" {
			continue
		}
		if _, ok := seen[senderID]; ok {
			continue
		}
		seen[senderID] = struct{}{}
		senderIDs = append(senderIDs, senderID)
	}
	if len(senderIDs) == 0 {
		log.ZDebug(ctx, "getSenderConversationBurnSeconds", "reason", "senderIDs is empty")
		return nil, false
	}

	senderBurnSeconds := make(map[string]int32, len(senderIDs))
	needUserFallback := make([]string, 0, len(senderIDs))
	for _, senderID := range senderIDs {
		conv, err := och.conversationClient.GetConversation(ctx, conversationID, senderID)
		if err != nil {
			log.ZWarn(ctx, "recordGroupBurnOnSend GetConversation failed", err,
				"conversationID", conversationID, "senderID", senderID)
			needUserFallback = append(needUserFallback, senderID)
			continue
		}
		if conv != nil && conv.BurnDuration > 0 {
			senderBurnSeconds[senderID] = conv.BurnDuration
			log.ZDebug(ctx, "getSenderConversationBurnSeconds", "reason", "found conversation burn",
				"burnDuration", conv.BurnDuration, "senderID", senderID, "conversationID", conversationID)
		} else {
			needUserFallback = append(needUserFallback, senderID)
			log.ZDebug(ctx, "getSenderConversationBurnSeconds", "reason", "no conversation burn", "senderID", senderID, "conversationID", conversationID)
		}
	}

	log.ZDebug(ctx, "getSenderConversationBurnSeconds", "reason", "after conversation lookup",
		"senderBurnSeconds", senderBurnSeconds, "needUserFallback", needUserFallback, "conversationID", conversationID)

	if len(needUserFallback) > 0 && och.userClient != nil {
		users, err := och.userClient.GetUsersInfo(ctx, needUserFallback)
		if err != nil {
			log.ZWarn(ctx, "recordGroupBurnOnSend GetUsersInfo failed", err, "senderIDs", needUserFallback)
		} else {
			for _, u := range users {
				if u != nil && u.MsgBurnDuration > 0 {
					senderBurnSeconds[u.UserID] = u.MsgBurnDuration
				}
			}
		}
	}

	log.ZDebug(ctx, "getSenderConversationBurnSeconds", "reason", "resolved burn seconds",
		"senderBurnSeconds", senderBurnSeconds, "needUserFallback", needUserFallback, "conversationID", conversationID)

	return senderBurnSeconds, true
}
