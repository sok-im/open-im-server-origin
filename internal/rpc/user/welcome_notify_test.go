package user

import (
	"context"
	"testing"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/msg"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeWelcomeLanguage(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"zh", "zh-CN"},
		{"zh-CN", "zh-CN"},
		{"zh_CN", "zh-CN"},
		{"ZH-cn", "zh-CN"},
		{"zh-Hans", "zh-CN"},
		{"zh-TW", "zh-TW"},
		{"zh_TW", "zh-TW"},
		{"zh-HK", "zh-TW"},
		{"zh-Hant", "zh-TW"},
		{"en", "en"},
		{"en-US", "en"},
		{"en_GB", "en"},
		{"fr-FR", "fr-fr"}, // unknown: lowercased form; picker falls back
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			assert.Equal(t, c.want, NormalizeWelcomeLanguage(c.in))
		})
	}
}

func TestPickWelcomeTemplate(t *testing.T) {
	templates := map[string]WelcomeTemplate{
		"zh-CN": {Title: "欢迎来到 SOK", Content: "简中正文"},
		"zh-TW": {Title: "歡迎來到 SOK", Content: "繁中正文"},
		"en":    {Title: "Welcome to SOK", Content: "EN body"},
	}

	key, tmpl, ok := PickWelcomeTemplate("zh-HK", "en", templates)
	assert.True(t, ok)
	assert.Equal(t, "zh-TW", key)
	assert.Equal(t, "繁中正文", tmpl.Content)

	key, tmpl, ok = PickWelcomeTemplate("", "en", templates)
	assert.True(t, ok)
	assert.Equal(t, "en", key)
	assert.Equal(t, "EN body", tmpl.Content)

	key, tmpl, ok = PickWelcomeTemplate("fr", "en", templates)
	assert.True(t, ok)
	assert.Equal(t, "en", key)

	_, _, ok = PickWelcomeTemplate("fr", "de", templates)
	assert.False(t, ok)
}

// Viper lowercases YAML map keys (zh-CN → zh-cn); lookup must still resolve via NormalizeWelcomeLanguage.
func TestPickWelcomeTemplateViperLowercasedKeys(t *testing.T) {
	templates := map[string]WelcomeTemplate{
		"zh-cn": {Title: "欢迎来到 SOK", Content: "简中正文"},
		"zh-tw": {Title: "歡迎來到 SOK", Content: "繁中正文"},
		"en":    {Title: "Welcome to SOK", Content: "EN body"},
	}

	key, tmpl, ok := PickWelcomeTemplate("zh-CN", "en", templates)
	assert.True(t, ok)
	assert.Equal(t, "zh-cn", key)
	assert.Equal(t, "简中正文", tmpl.Content)

	key, tmpl, ok = PickWelcomeTemplate("zh-HK", "en", templates)
	assert.True(t, ok)
	assert.Equal(t, "zh-tw", key)
	assert.Equal(t, "繁中正文", tmpl.Content)

	key, tmpl, ok = PickWelcomeTemplate("zh-TW", "en", templates)
	assert.True(t, ok)
	assert.Equal(t, "zh-tw", key)

	key, tmpl, ok = PickWelcomeTemplate("zh-Hans", "en", templates)
	assert.True(t, ok)
	assert.Equal(t, "zh-cn", key)

	key, tmpl, ok = PickWelcomeTemplate("fr", "en", templates)
	assert.True(t, ok)
	assert.Equal(t, "en", key)
}

func TestWelcomeClientMsgIDStable(t *testing.T) {
	a := WelcomeClientMsgID("userA")
	b := WelcomeClientMsgID("userA")
	c := WelcomeClientMsgID("userB")
	assert.NotEmpty(t, a)
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
}

func TestWelcomeSenderDisabledDoesNotSend(t *testing.T) {
	called := false
	w := &welcomeSender{
		cfg: config.WelcomeServiceNotification{Enable: false},
		sendMsg: func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error) {
			called = true
			return &msg.SendMsgResp{}, nil
		},
	}
	w.sendSync(context.Background(), "u1", "zh-CN")
	assert.False(t, called)
}

func TestBuildWelcomeSendMsgReqFields(t *testing.T) {
	cfg := config.WelcomeServiceNotification{
		Enable:          true,
		SendUserID:      "service_notification_bot",
		DefaultLanguage: "en",
		SubType:         2,
		Templates: map[string]config.WelcomeServiceNotificationTemplate{
			"en": {Title: "Welcome to SOK", Content: "EN body"},
		},
	}
	req, err := buildWelcomeSendMsgReq(cfg, "u1", "en")
	assert.NoError(t, err)
	assert.Equal(t, "service_notification_bot", req.MsgData.SendID)
	assert.Equal(t, "u1", req.MsgData.RecvID)
	assert.Equal(t, int32(constant.NotificationChatType), req.MsgData.SessionType)
	assert.Equal(t, int32(constant.ServiceNotification), req.MsgData.ContentType)
	assert.Equal(t, int32(constant.SysMsgType), req.MsgData.MsgFrom)
	assert.Equal(t, WelcomeClientMsgID("u1"), req.MsgData.ClientMsgID)
	assert.NotEmpty(t, req.MsgData.Content)
}
