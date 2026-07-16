package rtc

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/rtc"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/redis/go-redis/v9"
)

const (
	maxCustomInfoBytes     = 16 * 1024
	customSignalRateBurst  = 20
	customSignalDedupeTTL  = 6 * time.Hour
	customSignalRateWindow = time.Second
)

func validateCustomInfoSize(customInfo string) error {
	if len(customInfo) > maxCustomInfoBytes {
		return errs.ErrArgs.WrapMsg("customInfo exceeds 16KB")
	}
	return nil
}

func extractCustomMessageID(customInfo string) string {
	if strings.TrimSpace(customInfo) == "" {
		return ""
	}
	var payload struct {
		MessageID string `json:"messageID"`
	}
	if err := json.Unmarshal([]byte(customInfo), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.MessageID)
}

func (s *rtcServer) ensureCustomSignalSender(ctx context.Context, inv *model.SignalInvitation, userID string) error {
	if userID == inv.InviterUserID || datautil.Contain(userID, inv.InviteeUserIDList...) {
		return nil
	}
	if inv.GroupID != "" {
		if _, err := s.groupClient.GetGroupMemberInfo(ctx, inv.GroupID, userID); err != nil {
			return errs.ErrNoPermission.WrapMsg("sender is not a room/group member", "userID", userID)
		}
		return nil
	}
	return errs.ErrNoPermission.WrapMsg("sender is not a room member", "userID", userID)
}

// customSignalRateLimitScript 原子地完成 INCR + 首次 EXPIRE，避免 INCR 与 EXPIRE
// 之间进程崩溃导致计数 key 永久无 TTL 而卡死用户。返回自增后的计数值。
var customSignalRateLimitScript = redis.NewScript(`
local n = redis.call("INCR", KEYS[1])
if n == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return n
`)

func (s *rtcServer) rateLimitCustomSignal(ctx context.Context, roomID, userID string) error {
	if s.rdb == nil {
		return nil
	}
	key := "rtc:cs:rl:" + roomID + ":" + userID
	n, err := customSignalRateLimitScript.Run(ctx, s.rdb, []string{key}, customSignalRateWindow.Milliseconds()).Int64()
	if err != nil {
		return errs.WrapMsg(err, "custom signal rate limit script failed")
	}
	if n > int64(customSignalRateBurst) {
		log.ZWarn(ctx, "custom signal rate limited", nil, "roomID", roomID, "userID", userID, "count", n)
		return errs.ErrArgs.WrapMsg("custom signal rate limited")
	}
	return nil
}

func (s *rtcServer) markCustomSignalOnce(ctx context.Context, roomID, messageID string) (bool, error) {
	if s.rdb == nil || messageID == "" {
		return true, nil
	}
	key := "rtc:custom_signal:" + roomID + ":" + messageID
	ok, err := s.rdb.SetNX(ctx, key, "1", customSignalDedupeTTL).Result()
	if err != nil {
		return false, errs.WrapMsg(err, "custom signal dedupe failed")
	}
	return ok, nil
}

func (s *rtcServer) nextCustomSignalSeq(ctx context.Context, roomID string) int64 {
	if s.rdb == nil {
		return time.Now().UnixMilli()
	}
	n, err := s.rdb.Incr(ctx, "rtc:custom_seq:"+roomID).Result()
	if err != nil {
		return time.Now().UnixMilli()
	}
	return n
}

// SignalSendCustomSignal forwards a custom signal to all participants in a room.
func (s *rtcServer) SignalSendCustomSignal(ctx context.Context, req *rtc.SignalSendCustomSignalReq) (*rtc.SignalSendCustomSignalResp, error) {
	if err := validateCustomInfoSize(req.CustomInfo); err != nil {
		log.ZWarn(ctx, "SignalSendCustomSignal: customInfo too large", err,
			"roomID", req.RoomID, "size", len(req.CustomInfo))
		return nil, err
	}
	inv, err := s.db.GetInvitationByRoomID(ctx, req.RoomID)
	if err != nil {
		return nil, errs.WrapMsg(err, "room not found or ended", "roomID", req.RoomID)
	}
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		log.ZWarn(ctx, "SignalSendCustomSignal: missing opUserID", nil, "roomID", req.RoomID)
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}
	if err := s.ensureCustomSignalSender(ctx, inv, opUserID); err != nil {
		log.ZWarn(ctx, "SignalSendCustomSignal: sender not allowed", err,
			"roomID", inv.RoomID, "userID", opUserID, "groupID", inv.GroupID)
		return nil, err
	}
	if err := s.rateLimitCustomSignal(ctx, inv.RoomID, opUserID); err != nil {
		return nil, err
	}
	if msgID := extractCustomMessageID(req.CustomInfo); msgID != "" {
		ok, err := s.markCustomSignalOnce(ctx, inv.RoomID, msgID)
		if err != nil {
			return nil, err
		}
		if !ok {
			log.ZDebug(ctx, "SignalSendCustomSignal: duplicate messageID skipped",
				"roomID", inv.RoomID, "userID", opUserID, "messageID", msgID)
			return &rtc.SignalSendCustomSignalResp{}, nil
		}
	}

	platformID, _ := strconv.Atoi(mcontext.GetOpUserPlatform(ctx))
	serverSeq := s.nextCustomSignalSeq(ctx, req.RoomID)
	payload := map[string]any{
		"roomID":           req.RoomID,
		"senderUserID":     opUserID,
		"senderPlatformID": platformID,
		"serverSeq":        serverSeq,
	}
	var customObj any
	if err := json.Unmarshal([]byte(req.CustomInfo), &customObj); err == nil {
		payload["customInfo"] = customObj
	} else {
		payload["customInfo"] = req.CustomInfo
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return nil, errs.WrapMsg(err, "marshal custom signal content failed")
	}

	recipients := make([]string, 0, len(inv.InviteeUserIDList)+1)
	recipients = append(recipients, inv.InviteeUserIDList...)
	recipients = append(recipients, inv.InviterUserID)
	seen := make(map[string]struct{}, len(recipients))
	delivered := 0
	for _, uid := range recipients {
		if uid == "" || uid == opUserID {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		if err := s.sendCustomSignalNotification(ctx, opUserID, uid, int32(constant.SingleChatType), content); err != nil {
			log.ZWarn(ctx, "sendCustomSignalNotification failed", err, "to", uid)
			continue
		}
		delivered++
	}
	log.ZInfo(ctx, "SignalSendCustomSignal: forwarded",
		"roomID", inv.RoomID, "senderUserID", opUserID, "serverSeq", serverSeq,
		"recipientCount", delivered, "customInfoBytes", len(req.CustomInfo),
		"e2eeRequired", inv.E2EERequired)
	return &rtc.SignalSendCustomSignalResp{}, nil
}
