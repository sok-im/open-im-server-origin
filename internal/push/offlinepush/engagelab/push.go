package engagelab

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	el "github.com/engagelab-mt/engagelab-apppush-go"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/options"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/tools/log"
)

const (
	pushFrom             = "sokim"
	engagelabErrNoTarget = 21011 // no alias/tag/regid for the requested platform
)

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

	extras := options.WakeExtras(opts)
	if !opts.IsWakePush() {
		extras = map[string]interface{}{"ex": opts.Ex}
		if opts.Signal != nil && opts.Signal.ClientMsgID != "" {
			extras["ClientMsgID"] = opts.Signal.ClientMsgID
		}
	}

	apnsProduction := e.conf.IOSPush.Production
	pushOpts := &el.Options{APNSProduction: &apnsProduction}
	if collapseID := opts.APNsCollapseID(); collapseID != "" {
		pushOpts.APNSCollapseID = collapseID
	}

	if opts.IsWakePush() {
		// EngageLab rejects requests that include both notification and custom message (error 21036).
		// Android wake push uses custom message passthrough; iOS uses notification + mutable-content.
		// Each platform is sent separately; 21011 (no device on that platform) is non-fatal.
		err := e.sendWakePush(ctx, userIDs, title, content, extras, opts, pushOpts)
		if err != nil {
			log.ZError(ctx, "lintao engagelab wake push failed", err, "userIDs", userIDs, "title", title, "content", content, "extras", extras, "pushOpts", pushOpts)
			return err
		}
		log.ZDebug(ctx, "lintao engagelab wake push success", "userIDs", userIDs, "title", title, "content", content, "extras", extras, "pushOpts", pushOpts)
		return nil
	}

	androidNotif := &el.AndroidNotification{
		Alert:  content,
		Title:  title,
		Extras: extras,
	}
	if intent := e.conf.EngageLab.PushIntent; intent != "" {
		androidNotif.Intent = &el.AndroidIntent{URL: intent}
	}

	body := &el.PushBody{
		Platform: "all",
		Notification: &el.NotificationMessage{
			Alert:   content,
			Android: androidNotif,
			IOS:     e.buildIOSNotification(title, content, extras, opts),
		},
		Options: pushOpts,
	}

	err := e.send(ctx, userIDs, title, content, body)
	if err != nil {
		log.ZError(ctx, "lintao engagelab push failed", err, "userIDs", userIDs, "title", title, "content", content, "body", body)
		return err
	}

	log.ZDebug(ctx, "lintao engagelab push success", "userIDs", userIDs, "title", title, "content", content, "body", body)
	return nil
}

func (e *EngageLab) buildIOSNotification(title, content string, extras map[string]interface{}, opts *options.Opts) *el.IOSNotification {
	mutableContent := true
	contentAvailable := true

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
	return iosNotif
}

func (e *EngageLab) sendWakePush(ctx context.Context, userIDs []string, title, content string, extras map[string]interface{}, opts *options.Opts, pushOpts *el.Options) error {
	bodies := []*el.PushBody{
		{
			Platform: "android",
			Message: &el.CustomMessage{
				Title:      title,
				MsgContent: content,
				Extras:     extras,
			},
			Options: pushOpts,
		},
		{
			Platform: "ios",
			Notification: &el.NotificationMessage{
				Alert: content,
				IOS:   e.buildIOSNotification(title, content, extras, opts),
			},
			Options: pushOpts,
		},
	}

	var sent, noTarget int
	var lastErr error
	for _, body := range bodies {
		log.ZDebug(ctx, "lintao engagelab wake push send", "userIDs", userIDs, "title", title, "content", content, "body", body)
		err := e.send(ctx, userIDs, title, content, body)
		if err == nil {
			sent++
			continue
		}
		if isNoTargetError(err) {
			noTarget++
			log.ZWarn(ctx, "lintao engagelab wake push skipped: no device on platform", nil, "platform", body.Platform, "userIDs", userIDs)
			continue
		}
		lastErr = err
	}
	if sent > 0 {
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	if noTarget > 0 {
		log.ZWarn(ctx, "lintao engagelab wake push: no devices for alias on any platform", nil, "userIDs", userIDs)
		return nil
	}
	return nil
}

func isNoTargetError(err error) bool {
	var apiErr *el.ApiError
	return errors.As(err, &apiErr) && apiErr.ErrorBody.Code == engagelabErrNoTarget
}

func (e *EngageLab) send(ctx context.Context, userIDs []string, title, content string, body *el.PushBody) error {
	param := &el.PushParam{
		From: pushFrom,
		To: &el.PushTo{
			Alias: userIDs,
		},
		Body:      body,
		RequestID: strconv.FormatInt(time.Now().UnixNano(), 10),
	}

	resp, err := e.client.Push.Send(ctx, param)
	if err != nil {
		if isNoTargetError(err) {
			log.ZDebug(ctx, "engagelab push no target", "platform", body.Platform, "userIDs", userIDs)
		} else {
			log.ZError(ctx, "engagelab push failed", err, "param", param)
		}
		return err
	}

	log.ZDebug(ctx, "engagelab push success", "userIDs", userIDs, "param", param, "title", title, "content", content, "platform", body.Platform, "resp", resp)
	return nil
}
