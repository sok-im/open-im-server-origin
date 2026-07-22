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

package user

import (
	"context"
	"strings"

	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	pbmsg "github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/encrypt"
	"github.com/openimsdk/tools/utils/jsonutil"
	"github.com/openimsdk/tools/utils/timeutil"
)

// welcome 语言桶键（小写，与 Viper 将 YAML map key 小写化后的结果一致）
const (
	welcomeBucketZhHans = "zh-hans"
	welcomeBucketZhHant = "zh-hant"
)

// tryFirstOnlineWelcome 在用户「首次上线」时，由官方服务号推送一条欢迎通知（每个用户仅发一次）。
// 调用点：SetUserOnlineStatus 中检测到某用户本次有平台上线时。SetUserOnlineStatus 会被续约周期性调用，
// 因此这里先走缓存快路径判断，命中已发送则直接返回，避免写放大；未发送时再用原子 CAS 抢占发送权。
func (s *userServer) tryFirstOnlineWelcome(ctx context.Context, userID string) {
	cfg := s.config.RpcConfig.WelcomeNotification
	if !cfg.Enable || cfg.SendID == "" || len(cfg.Languages) == 0 {
		log.ZInfo(ctx, "welcome notification: not enabled", "userID", userID)
		return
	}
	// 快路径：读缓存，已发送则直接返回（续约高频调用不打库）。
	user, err := s.db.GetUserByID(ctx, userID)
	if err != nil {
		log.ZWarn(ctx, "welcome notification: get user failed", err, "userID", userID)
		return
	}
	if user.WelcomeNotificationSent {
		log.ZInfo(ctx, "welcome notification: already sent", "userID", userID)
		return
	}
	// 不给服务号/通知/机器人等系统账号发送欢迎语。
	if user.AppMangerLevel >= constant.AppNotificationAdmin {
		log.ZInfo(ctx, "welcome notification: app manager level is greater than or equal to admin", "userID", userID, "appMangerLevel", user.AppMangerLevel)
		return
	}
	text, bucket, ok := resolveWelcomeText(cfg, user.Language)
	if !ok {
		log.ZWarn(ctx, "welcome notification: no text matched and no fallback", nil, "userID", userID, "language", user.Language)
		return
	}
	// 原子抢占：仅当此前未发送时置位成功，claimed=true 的调用负责发送，保证并发多端上线只发一次。
	claimed, err := s.db.MarkWelcomeNotificationSent(ctx, userID)
	if err != nil {
		log.ZWarn(ctx, "welcome notification: mark sent failed", err, "userID", userID)
		return
	}
	if !claimed {
		log.ZInfo(ctx, "welcome notification: already sent", "userID", userID)
		return
	}
	// 异步发送，避免阻塞在线状态主链路；使用 detached context 防止随本次 RPC 结束被取消。
	detachedCtx := mcontext.NewCtx("welcome_" + mcontext.GetOperationID(ctx))
	s.sendWelcomeNotification(detachedCtx, userID, cfg, bucket, text)
}

// sendWelcomeNotification 发送欢迎服务号消息；失败时回滚已发送标记，允许下次上线重试。
func (s *userServer) sendWelcomeNotification(ctx context.Context, userID string, cfg config.WelcomeNotification, bucket string, text config.WelcomeNotificationText) {
	content := apistruct.ServiceNotificationContent{
		Title:   text.Title,
		Content: text.Content,
		SubType: cfg.SubType,
	}
	// ClientMsgID 固定为发送方+接收方的确定性哈希，保证即便回滚重试也不会在会话内产生重复消息。
	clientMsgID := encrypt.Md5("welcome_" + cfg.SendID + "_" + userID)
	opts := config.GetOptionsByNotification(config.NotificationConfig{
		IsSendMsg:        true,
		ReliabilityLevel: constant.ReliableNotificationNoMsg,
	}, nil)
	req := &pbmsg.SendMsgReq{
		MsgData: &sdkws.MsgData{
			SendID: cfg.SendID,
			RecvID: userID,
			Content: []byte(jsonutil.StructToJsonString(&sdkws.NotificationElem{
				Detail: jsonutil.StructToJsonString(content),
			})),
			MsgFrom:         constant.SysMsgType,
			ContentType:     constant.ServiceNotification,
			SessionType:     constant.NotificationChatType,
			CreateTime:      timeutil.GetCurrentTimestampByMill(),
			ClientMsgID:     clientMsgID,
			Options:         opts,
			OfflinePushInfo: &sdkws.OfflinePushInfo{},
		},
	}
	if _, err := s.msgClient.SendMsg(ctx, req); err != nil {
		log.ZWarn(ctx, "welcome notification: send failed, rollback flag", err, "userID", userID, "sendID", cfg.SendID, "bucket", bucket)
		if rbErr := s.db.SetWelcomeNotificationSent(ctx, userID, false); rbErr != nil {
			log.ZWarn(ctx, "welcome notification: rollback flag failed", rbErr, "userID", userID)
		}
		return
	}
	log.ZInfo(ctx, "welcome notification: sent", "userID", userID, "sendID", cfg.SendID, "bucket", bucket, "clientMsgID", clientMsgID)
}

// resolveWelcomeText 将用户语言归一化到文案桶并返回对应文案。
// 归一化规则：zh-CN/zh-Hans/zh/zh-SG -> zh-hans；zh-TW/zh-HK/zh-MO/zh-Hant -> zh-hant；其余 -> defaultLanguage。
// 查找时大小写不敏感（兼容 Viper 将 YAML map key 小写化，如 zh-Hans → zh-hans）。
// 若归一化后的桶不存在，则回退到 defaultLanguage；仍不存在则返回 ok=false。
func resolveWelcomeText(cfg config.WelcomeNotification, language string) (config.WelcomeNotificationText, string, bool) {
	bucket := normalizeWelcomeLanguage(language, cfg.DefaultLanguage)
	if text, ok := findWelcomeText(cfg.Languages, bucket); ok {
		return text, bucket, true
	}
	if cfg.DefaultLanguage != "" && !strings.EqualFold(cfg.DefaultLanguage, bucket) {
		if text, ok := findWelcomeText(cfg.Languages, cfg.DefaultLanguage); ok {
			return text, strings.ToLower(cfg.DefaultLanguage), true
		}
	}
	return config.WelcomeNotificationText{}, bucket, false
}

// findWelcomeText 按大小写不敏感匹配 languages map key，且要求 title/content 非空。
func findWelcomeText(languages map[string]config.WelcomeNotificationText, key string) (config.WelcomeNotificationText, bool) {
	if key == "" || len(languages) == 0 {
		return config.WelcomeNotificationText{}, false
	}
	if text, ok := languages[key]; ok && text.Title != "" && text.Content != "" {
		return text, true
	}
	want := strings.ToLower(key)
	for k, text := range languages {
		if strings.ToLower(k) == want && text.Title != "" && text.Content != "" {
			return text, true
		}
	}
	return config.WelcomeNotificationText{}, false
}

// normalizeWelcomeLanguage 归一化语言标签到文案桶键；大小写不敏感、兼容下划线。
func normalizeWelcomeLanguage(language, defaultLanguage string) string {
	lang := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(language), "_", "-"))
	switch {
	case lang == "zh-hans" || lang == "zh-cn" || lang == "zh" || lang == "zh-sg":
		return welcomeBucketZhHans
	case lang == "zh-hant" || lang == "zh-tw" || lang == "zh-hk" || lang == "zh-mo":
		return welcomeBucketZhHant
	default:
		return defaultLanguage
	}
}
