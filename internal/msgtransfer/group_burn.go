package msgtransfer

import (
	"context"
	"time"

	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"
)

// recordGroupBurnOnSend 在群消息分配 seq 后写入定时删除截止时间。
//
// 销毁时长优先级：
//  1. 群 MsgBurnDuration（群阅后即焚设置）；
//  2. 发送者（seqSenderID）用户全局 MsgBurnDuration。
//
// 失败仅记日志，不影响消息主流程。
func (och *OnlineHistoryRedisConsumerHandler) recordGroupBurnOnSend(ctx context.Context, msgs []*sdkws.MsgData) {
	if och.groupMsgBurnRecordDB == nil || len(msgs) == 0 {
		return
	}
	msg := msgs[0]
	if msg.SessionType != constant.ReadGroupChatType || msg.GroupID == "" {
		return
	}
	groupInfo, err := och.groupClient.GetGroupInfo(ctx, msg.GroupID)
	if err != nil {
		log.ZWarn(ctx, "recordGroupBurnOnSend GetGroupInfo failed", err, "groupID", msg.GroupID)
		return
	}
	groupBurnSeconds := groupInfo.GetMsgBurnDuration()

	seqSenderID := make(map[int64]string, len(msgs))
	msgSendTimeMs := make(map[int64]int64, len(msgs))
	for _, m := range msgs {
		if m == nil || m.Seq <= 0 {
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
		return
	}

	seqBurnEndTimeMs := make(map[int64]int64, len(seqSenderID))
	if groupBurnSeconds > 0 {
		for seq, sendTimeMs := range msgSendTimeMs {
			seqBurnEndTimeMs[seq] = sendTimeMs + int64(groupBurnSeconds)*1000
		}
	} else {
		senderBurnSeconds, ok := och.getSenderBurnSeconds(ctx, seqSenderID)
		if !ok {
			return
		}
		for seq, senderID := range seqSenderID {
			burnSeconds, ok := senderBurnSeconds[senderID]
			if !ok || burnSeconds <= 0 {
				continue
			}
			seqBurnEndTimeMs[seq] = msgSendTimeMs[seq] + int64(burnSeconds)*1000
		}
	}
	if len(seqBurnEndTimeMs) == 0 {
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

func (och *OnlineHistoryRedisConsumerHandler) getSenderBurnSeconds(ctx context.Context, seqSenderID map[int64]string) (map[string]int32, bool) {
	if och.userClient == nil {
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
		return nil, false
	}
	users, err := och.userClient.GetUsersInfo(ctx, senderIDs)
	if err != nil {
		log.ZWarn(ctx, "recordGroupBurnOnSend GetUsersInfo failed", err, "senderIDs", senderIDs)
		return nil, false
	}
	senderBurnSeconds := make(map[string]int32, len(users))
	for _, u := range users {
		if u != nil && u.MsgBurnDuration > 0 {
			senderBurnSeconds[u.UserID] = u.MsgBurnDuration
		}
	}
	return senderBurnSeconds, true
}
