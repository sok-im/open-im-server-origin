package engagelab

import (
	"context"
	"strconv"
	"strings"
	"time"

	el "github.com/engagelab-mt/engagelab-apppush-go"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/options"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/tools/log"
)

const pushFrom = "sokim"

type EngageLab struct {
	client *el.Client
	conf   *config.Push
}

// baseURL strips any trailing API path that may have been copied from old configs
// (e.g. "https://pushapi-sgp.engagelab.com/v4/push" → "https://pushapi-sgp.engagelab.com").
// The SDK appends /v4/push itself, so only the data-center root is needed.
func baseURL(raw string) string {
	if idx := strings.Index(raw, "/v4/"); idx != -1 {
		return raw[:idx]
	}
	return strings.TrimRight(raw, "/")
}

func NewClient(pushConf *config.Push) *EngageLab {
	opts := []el.Option{}
	if pushConf.EngageLab.PushURL != "" {
		opts = append(opts, el.WithBaseURL(baseURL(pushConf.EngageLab.PushURL)))
	}
	return &EngageLab{
		client: el.NewClient(pushConf.EngageLab.AppKey, pushConf.EngageLab.MasterSecret, opts...),
		conf:   pushConf,
	}
}

func (e *EngageLab) Push(ctx context.Context, userIDs []string, title, content string, opts *options.Opts) error {
	log.ZInfo(ctx, "lintao engagelab push start", "userIDs", userIDs, "title", title)
	extras := map[string]interface{}{"ex": opts.Ex}
	if opts.Signal.ClientMsgID != "" {
		extras["ClientMsgID"] = opts.Signal.ClientMsgID
	}

	mutableContent := true
	apnsProduction := e.conf.IOSPush.Production

	iosNotif := &el.IOSNotification{
		Alert: map[string]string{
			"title": title,
			"body":  content,
		},
		Sound:          e.conf.IOSPush.PushSound,
		MutableContent: &mutableContent,
		Extras:         extras,
	}
	if opts.IOSBadgeCount {
		iosNotif.Badge = "+1"
	}

	androidNotif := &el.AndroidNotification{
		Alert:  content,
		Title:  title,
		Extras: extras,
	}
	if intent := e.conf.EngageLab.PushIntent; intent != "" {
		androidNotif.Intent = &el.AndroidIntent{URL: intent}
	}

	param := &el.PushParam{
		From: pushFrom,
		To: &el.PushTo{
			Alias: userIDs,
		},
		Body: &el.PushBody{
			Platform: "all",
			Notification: &el.NotificationMessage{
				Alert:   content,
				Android: androidNotif,
				IOS:     iosNotif,
			},
			Options: &el.Options{
				APNSProduction: &apnsProduction,
			},
		},
		RequestID: strconv.FormatInt(time.Now().UnixNano(), 10),
	}

	resp, err := e.client.Push.Send(ctx, param)
	if err != nil {
		log.ZError(ctx, "lintao engagelab push failed", err, "param", param)
		return err
	}
	log.ZInfo(ctx, "lintao engagelab push success", "userIDs", userIDs, "title", title, "content", content, "resp", resp)
	return nil
}
