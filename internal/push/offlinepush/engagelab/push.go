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
	log.ZInfo(ctx, "engagelab push start", "userIDs", userIDs, "title", title, "wakePush", opts.IsWakePush())

	extras := options.WakeExtras(opts)
	if !opts.IsWakePush() {
		extras = map[string]interface{}{"ex": opts.Ex}
		if opts.Signal != nil && opts.Signal.ClientMsgID != "" {
			extras["ClientMsgID"] = opts.Signal.ClientMsgID
		}
	}

	mutableContent := true
	contentAvailable := true
	apnsProduction := e.conf.IOSPush.Production

	sound := e.conf.IOSPush.PushSound
	if opts.IOSPushSound != "" {
		sound = opts.IOSPushSound
	}

	iosNotif := &el.IOSNotification{
		Alert: map[string]string{
			"title": title,
			"body":  content,
		},
		Sound:            sound,
		MutableContent:   &mutableContent,
		ContentAvailable: &contentAvailable,
		Extras:           extras,
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

	pushBody := &el.PushBody{
		Platform: "all",
		Notification: &el.NotificationMessage{
			Alert:   content,
			Android: androidNotif,
			IOS:     iosNotif,
		},
		Options: &el.Options{
			APNSProduction: &apnsProduction,
		},
	}
	if opts.IsWakePush() {
		pushBody.Message = &el.CustomMessage{
			Title:      title,
			MsgContent: content,
			Extras:     extras,
		}
	}

	param := &el.PushParam{
		From: pushFrom,
		To: &el.PushTo{
			Alias: userIDs,
		},
		Body:      pushBody,
		RequestID: strconv.FormatInt(time.Now().UnixNano(), 10),
	}

	resp, err := e.client.Push.Send(ctx, param)
	if err != nil {
		log.ZError(ctx, "engagelab push failed", err, "param", param)
		return err
	}
	log.ZInfo(ctx, "engagelab push success", "userIDs", userIDs, "title", title, "content", content, "resp", resp)
	return nil
}
