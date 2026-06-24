package rtc

import (
	"testing"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/rtc"
	"github.com/openimsdk/tools/utils/jsonutil"
)

func TestBuildCallWakePushEx(t *testing.T) {
	exJSON := buildCallWakePushEx(&rtc.InvitationInfo{
		RoomID:        "room-123",
		MediaType:     "video",
		InviterUserID: "u1",
		GroupID:       "g1",
	}, int32(constant.ReadGroupChatType))

	var ex callWakePushEx
	if err := jsonutil.JsonStringToStruct(exJSON, &ex); err != nil {
		t.Fatalf("unmarshal ex: %v", err)
	}
	if ex.SchemaVersion != callWakePushSchemaVersion {
		t.Fatalf("schemaVersion=%d", ex.SchemaVersion)
	}
	if ex.PushType != callWakePushType {
		t.Fatalf("pushType=%q", ex.PushType)
	}
	if ex.RoomID != "room-123" || ex.SessionType != int32(constant.ReadGroupChatType) {
		t.Fatalf("room/session mismatch: %+v", ex)
	}
	if ex.MediaType != "video" || ex.SourceID != "u1" || ex.GroupID != "g1" {
		t.Fatalf("media/source/group mismatch: %+v", ex)
	}
}

func TestResolveInviteOfflinePushInfoUsesConfigDefaults(t *testing.T) {
	s := &rtcServer{
		config: &Config{
			NotificationConfig: config.Notification{
				SignalingInvite: config.NotificationConfig{
					OfflinePush: config.OfflinePushConfig{
						Enable: true,
						Title:  "SOK",
						Desc:   "默认通话邀请",
					},
				},
			},
		},
	}
	inv := &rtc.InvitationInfo{
		RoomID:        "room-abc",
		InviterUserID: "caller",
		MediaType:     "audio",
		SessionType:   int32(constant.SingleChatType),
	}
	push := s.resolveInviteOfflinePushInfo(t.Context(), inv, nil)
	if push == nil {
		t.Fatal("expected offline push info")
	}
	if push.Title != "SOK" {
		t.Fatalf("title=%q", push.Title)
	}
	if push.Desc != "默认通话邀请" {
		t.Fatalf("desc=%q", push.Desc)
	}
	if push.IOSPushSound != callIOSPushSound {
		t.Fatalf("sound=%q", push.IOSPushSound)
	}
	if push.Ex == "" {
		t.Fatal("expected wake push ex")
	}
}

func TestCallMediaLabel(t *testing.T) {
	if got := callMediaLabel("video"); got != "视频" {
		t.Fatalf("video label=%q", got)
	}
	if got := callMediaLabel("audio"); got != "语音" {
		t.Fatalf("audio label=%q", got)
	}
}
