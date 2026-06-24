package offlinepush

import (
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/options"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/utils/jsonutil"
)

const (
	WakePushPlaceholderTitle = "SOK"
	WakePushPlaceholderDesc  = "你收到了一条新消息"
)

// GetOfflinePushInfos resolves title, content and push options from MsgData.
// When OfflinePushInfo.Ex is set (SOK wake push), title/desc are passed through
// from the client and must not be derived from message plaintext on the server.
func GetOfflinePushInfos(msg *sdkws.MsgData) (title, content string, opts *options.Opts, err error) {
	type atTextElem struct {
		Text       string   `json:"text,omitempty"`
		AtUserList []string `json:"atUserList,omitempty"`
		IsAtSelf   bool     `json:"isAtSelf"`
	}

	opts = &options.Opts{Signal: &options.Signal{ClientMsgID: msg.ClientMsgID, ServerMsgID: msg.ServerMsgID}}
	if msg.OfflinePushInfo != nil {
		opts.IOSBadgeCount = msg.OfflinePushInfo.IOSBadgeCount
		opts.IOSPushSound = msg.OfflinePushInfo.IOSPushSound
		opts.Ex = msg.OfflinePushInfo.Ex
		title = msg.OfflinePushInfo.Title
		content = msg.OfflinePushInfo.Desc
	}

	if opts.Ex != "" {
		if title == "" {
			title = WakePushPlaceholderTitle
		}
		if content == "" {
			content = WakePushPlaceholderDesc
		}
		return
	}

	if title == "" {
		switch msg.ContentType {
		case constant.Text:
			fallthrough
		case constant.Picture:
			fallthrough
		case constant.Voice:
			fallthrough
		case constant.Video:
			fallthrough
		case constant.File:
			title = constant.ContentType2PushContent[int64(msg.ContentType)]
		case constant.AtText:
			ac := atTextElem{}
			_ = jsonutil.JsonStringToStruct(string(msg.Content), &ac)
		case constant.SignalingNotification:
			title = constant.ContentType2PushContent[constant.SignalMsg]
		default:
			title = constant.ContentType2PushContent[constant.Common]
		}
	}
	if content == "" {
		content = title
	}
	return
}
