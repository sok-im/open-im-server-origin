package user

import (
	"context"
	"strings"

	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	tablerelation "github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/encrypt"
	"github.com/openimsdk/tools/utils/jsonutil"
	"github.com/openimsdk/tools/utils/timeutil"
)

type WelcomeTemplate struct {
	Title   string
	Content string
}

func NormalizeWelcomeLanguage(lang string) string {
	s := strings.TrimSpace(lang)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "_", "-")
	lower := strings.ToLower(s)

	switch lower {
	case "zh", "zh-cn", "zh-hans":
		return "zh-CN"
	case "zh-tw", "zh-hk", "zh-hant":
		return "zh-TW"
	case "en", "en-us", "en-gb":
		return "en"
	default:
		return lower
	}
}

func PickWelcomeTemplate(lang, defaultLanguage string, templates map[string]WelcomeTemplate) (string, WelcomeTemplate, bool) {
	if templates == nil {
		return "", WelcomeTemplate{}, false
	}
	// Exact key first, then normalized map-key match (Viper lowercases YAML keys: zh-CN → zh-cn).
	try := func(wantedKey string) (string, WelcomeTemplate, bool) {
		if wantedKey == "" {
			return "", WelcomeTemplate{}, false
		}
		if t, ok := templates[wantedKey]; ok && t.Title != "" && t.Content != "" {
			return wantedKey, t, true
		}
		for mapKey, t := range templates {
			if t.Title == "" || t.Content == "" {
				continue
			}
			if NormalizeWelcomeLanguage(mapKey) == wantedKey {
				return mapKey, t, true
			}
		}
		return "", WelcomeTemplate{}, false
	}
	if key, t, ok := try(NormalizeWelcomeLanguage(lang)); ok {
		return key, t, true
	}
	defKey := NormalizeWelcomeLanguage(defaultLanguage)
	if defKey == "" {
		defKey = defaultLanguage
	}
	if key, t, ok := try(defKey); ok {
		return key, t, true
	}
	return "", WelcomeTemplate{}, false
}

func WelcomeClientMsgID(userID string) string {
	return encrypt.Md5("welcome_service_notification:" + userID)
}

type welcomeSender struct {
	cfg     config.WelcomeServiceNotification
	getUser func(ctx context.Context, userID string) (*tablerelation.User, error)
	sendMsg func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error)
}

func newWelcomeSender(
	cfg config.WelcomeServiceNotification,
	getUser func(ctx context.Context, userID string) (*tablerelation.User, error),
	sendMsg func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error),
) *welcomeSender {
	return &welcomeSender{cfg: cfg, getUser: getUser, sendMsg: sendMsg}
}

func (w *welcomeSender) SendAfterRegister(ctx context.Context, userID, language string) {
	if w == nil || !w.cfg.Enable || userID == "" {
		return
	}
	opUserID := mcontext.GetOpUserID(ctx)
	operationID := mcontext.GetOperationID(ctx)
	go func() {
		asyncCtx := mcontext.SetOperationID(context.Background(), operationID)
		if opUserID != "" {
			asyncCtx = mcontext.WithOpUserIDContext(asyncCtx, opUserID)
		}
		w.sendSync(asyncCtx, userID, language)
	}()
}

func (w *welcomeSender) sendSync(ctx context.Context, userID, language string) {
	if !w.cfg.Enable {
		return
	}
	sendID := w.cfg.SendUserID
	if sendID == "" {
		log.ZWarn(ctx, "welcome notify: sendUserID empty", nil, "userID", userID)
		return
	}
	u, err := w.getUser(ctx, sendID)
	if err != nil {
		log.ZWarn(ctx, "welcome notify: get send user failed", err, "sendUserID", sendID, "userID", userID)
		return
	}
	if u.AppMangerLevel < constant.AppNotificationAdmin && u.AppMangerLevel != constant.AppAdmin {
		log.ZWarn(ctx, "welcome notify: sendUserID is not notification account", nil,
			"sendUserID", sendID, "level", u.AppMangerLevel, "userID", userID)
		return
	}

	req, err := buildWelcomeSendMsgReq(w.cfg, userID, language)
	if err != nil {
		log.ZWarn(ctx, "welcome notify: build req failed", err, "userID", userID, "language", language)
		return
	}
	if _, err := w.sendMsg(ctx, req); err != nil {
		log.ZWarn(ctx, "welcome notify: SendMsg failed", err,
			"userID", userID, "language", language, "clientMsgID", req.MsgData.ClientMsgID)
		return
	}
	log.ZInfo(ctx, "welcome notify: sent",
		"userID", userID, "language", language, "clientMsgID", req.MsgData.ClientMsgID)
}

func buildWelcomeSendMsgReq(cfg config.WelcomeServiceNotification, recvUserID, language string) (*msg.SendMsgReq, error) {
	templates := make(map[string]WelcomeTemplate, len(cfg.Templates))
	for k, v := range cfg.Templates {
		templates[k] = WelcomeTemplate{Title: v.Title, Content: v.Content}
	}
	_, tmpl, ok := PickWelcomeTemplate(language, cfg.DefaultLanguage, templates)
	if !ok {
		return nil, errs.ErrArgs.WrapMsg("welcome template not found")
	}
	subType := cfg.SubType
	if subType == 0 {
		subType = apistruct.ServiceNotificationSubTypeAccount
	}
	content := apistruct.ServiceNotificationContent{
		Title:   tmpl.Title,
		Content: tmpl.Content,
		SubType: subType,
	}
	notifCfg := config.NotificationConfig{
		IsSendMsg:        true,
		ReliabilityLevel: constant.ReliableNotificationNoMsg,
	}
	opts := config.GetOptionsByNotification(notifCfg, nil)
	return &msg.SendMsgReq{
		MsgData: &sdkws.MsgData{
			SendID: cfg.SendUserID,
			RecvID: recvUserID,
			Content: []byte(jsonutil.StructToJsonString(&sdkws.NotificationElem{
				Detail: jsonutil.StructToJsonString(content),
			})),
			MsgFrom:     constant.SysMsgType,
			ContentType: constant.ServiceNotification,
			SessionType: constant.NotificationChatType,
			CreateTime:  timeutil.GetCurrentTimestampByMill(),
			ClientMsgID: WelcomeClientMsgID(recvUserID),
			Options:     opts,
		},
	}, nil
}
