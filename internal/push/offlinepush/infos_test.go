package offlinepush

import (
	"testing"

	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
)

func TestGetOfflinePushInfos_WakePushPassthrough(t *testing.T) {
	msg := &sdkws.MsgData{
		ContentType: constant.Text,
		Content:     []byte(`{"content":"secret plaintext"}`),
		ClientMsgID: "client-1",
		OfflinePushInfo: &sdkws.OfflinePushInfo{
			Title:         "SOK",
			Desc:          "你收到了一条新消息",
			Ex:            `{"schemaVersion":1,"pushType":"e2ee_message"}`,
			IOSPushSound:  "default",
			IOSBadgeCount: true,
		},
	}

	title, content, opts, err := GetOfflinePushInfos(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "SOK" {
		t.Fatalf("title = %q, want SOK", title)
	}
	if content != "你收到了一条新消息" {
		t.Fatalf("content = %q, want placeholder desc", content)
	}
	if opts.Ex != msg.OfflinePushInfo.Ex {
		t.Fatalf("ex not passed through: %q", opts.Ex)
	}
}

func TestGetOfflinePushInfos_WakePushEmptyTitleUsesPlaceholder(t *testing.T) {
	msg := &sdkws.MsgData{
		ContentType: constant.Text,
		Content:     []byte(`{"content":"secret"}`),
		OfflinePushInfo: &sdkws.OfflinePushInfo{
			Ex: `{"schemaVersion":1,"pushType":"plain_message"}`,
		},
	}

	title, content, _, err := GetOfflinePushInfos(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != WakePushPlaceholderTitle {
		t.Fatalf("title = %q, want placeholder", title)
	}
	if content != WakePushPlaceholderDesc {
		t.Fatalf("content = %q, want placeholder", content)
	}
}

func TestGetOfflinePushInfos_LegacyFallbackWithoutEx(t *testing.T) {
	msg := &sdkws.MsgData{
		ContentType: constant.Picture,
	}

	title, content, opts, err := GetOfflinePushInfos(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != constant.ContentType2PushContent[int64(constant.Picture)] {
		t.Fatalf("unexpected legacy title: %q", title)
	}
	if content != title {
		t.Fatalf("content = %q, want %q", content, title)
	}
	if opts.Ex != "" {
		t.Fatalf("ex should be empty for legacy push")
	}
}
