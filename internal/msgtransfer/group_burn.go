package msgtransfer

import (
	"context"
	"time"

	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"
)

// recordGroupBurnOnSend 在群消息分配 seq 后写入定时删除截止时间（发送时间 + 群 MsgBurnDuration）。
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
	burnSeconds := groupInfo.GetMsgBurnDuration()
	if burnSeconds <= 0 {
		return
	}
	seqs := make([]int64, 0, len(msgs))
	seqSenderID := make(map[int64]string, len(msgs))
	for _, m := range msgs {
		if m == nil || m.Seq <= 0 {
			continue
		}
		seqs = append(seqs, m.Seq)
		seqSenderID[m.Seq] = m.SendID
	}
	if len(seqs) == 0 {
		return
	}
	sendTimeMs := msg.SendTime
	if sendTimeMs <= 0 {
		sendTimeMs = time.Now().UnixMilli()
	}
	burnEndTimeMs := sendTimeMs + int64(burnSeconds)*1000
	if err := och.groupMsgBurnRecordDB.UpsertOnSend(ctx, msg.GroupID, seqs, seqSenderID, burnEndTimeMs); err != nil {
		log.ZError(ctx, "recordGroupBurnOnSend UpsertOnSend failed", err,
			"groupID", msg.GroupID, "seqs", seqs)
	}
}
