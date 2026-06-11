package engagelab

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/engagelab/body"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/options"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/tools/utils/httputil"
)

const pushFrom = "openim"

type EngageLab struct {
	pushConf   *config.Push
	httpClient *httputil.HTTPClient
}

func NewClient(pushConf *config.Push) *EngageLab {
	return &EngageLab{
		pushConf:   pushConf,
		httpClient: httputil.NewHTTPClient(httputil.NewClientConfig()),
	}
}

func (e *EngageLab) getAuthorization(appKey, masterSecret string) string {
	buf := []byte(fmt.Sprintf("%s:%s", appKey, masterSecret))
	return fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString(buf))
}

func (e *EngageLab) Push(ctx context.Context, userIDs []string, title, content string, opts *options.Opts) error {
	extras := map[string]string{"ex": opts.Ex}
	if opts.Signal.ClientMsgID != "" {
		extras["ClientMsgID"] = opts.Signal.ClientMsgID
	}

	notification := &body.Notification{
		Alert: content,
		Android: body.Android{
			Alert:  content,
			Title:  title,
			Extras: extras,
		},
		IOS: body.IOS{
			Alert: body.IOSAlert{
				Title: title,
				Body:  content,
			},
			Sound:          opts.IOSPushSound,
			Extras:         extras,
			MutableContent: true,
		},
	}
	if opts.IOSBadgeCount {
		notification.IOS.Badge = "+1"
	}
	if intent := e.pushConf.EngageLab.PushIntent; intent != "" {
		notification.Android.Intent.URL = intent
	}

	req := body.PushRequest{
		From: pushFrom,
		To: body.To{
			Alias: userIDs,
		},
		Body: body.Body{
			Platform:     "all",
			Notification: notification,
			Options: &body.Options{
				ApnsProduction: e.pushConf.IOSPush.Production,
			},
		},
		RequestID: strconv.FormatInt(time.Now().UnixNano(), 10),
	}

	var resp map[string]any
	return e.request(ctx, req, &resp, 5)
}

func (e *EngageLab) request(ctx context.Context, req body.PushRequest, resp *map[string]any, timeout int) error {
	err := e.httpClient.PostReturn(
		ctx,
		e.pushConf.EngageLab.PushURL,
		map[string]string{
			"Authorization": e.getAuthorization(e.pushConf.EngageLab.AppKey, e.pushConf.EngageLab.MasterSecret),
		},
		req,
		resp,
		timeout,
	)
	if err != nil {
		return err
	}
	if errObj, ok := (*resp)["error"]; ok {
		return fmt.Errorf("engagelab push failed: %v", errObj)
	}
	if _, ok := (*resp)["msg_id"]; !ok {
		return fmt.Errorf("engagelab push failed: %v", resp)
	}
	return nil
}
