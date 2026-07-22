package user

import (
	"strings"

	"github.com/openimsdk/tools/utils/encrypt"
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
	try := func(key string) (string, WelcomeTemplate, bool) {
		if key == "" {
			return "", WelcomeTemplate{}, false
		}
		if t, ok := templates[key]; ok && t.Title != "" && t.Content != "" {
			return key, t, true
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
