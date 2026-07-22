package user

import (
	"testing"

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

func TestWelcomeClientMsgIDStable(t *testing.T) {
	a := WelcomeClientMsgID("userA")
	b := WelcomeClientMsgID("userA")
	c := WelcomeClientMsgID("userB")
	assert.NotEmpty(t, a)
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
}
