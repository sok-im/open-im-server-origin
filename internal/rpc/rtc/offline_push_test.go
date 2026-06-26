package rtc

import (
	"testing"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/rtc"
	"github.com/openimsdk/protocol/sdkws"
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

func TestSyncCallWakePushExAddsGroupID(t *testing.T) {
	clientEx := `{"pushType":"call","roomID":"client-room","sessionType":3}`
	got := syncCallWakePushEx(&rtc.InvitationInfo{
		RoomID:        "room-server",
		MediaType:     "video",
		InviterUserID: "caller",
		GroupID:       "g1",
	}, int32(constant.ReadGroupChatType), clientEx)

	var ex callWakePushEx
	if err := jsonutil.JsonStringToStruct(got, &ex); err != nil {
		t.Fatalf("unmarshal ex: %v", err)
	}
	if ex.RoomID != "room-server" {
		t.Fatalf("roomID=%q", ex.RoomID)
	}
	if ex.GroupID != "g1" {
		t.Fatalf("groupID=%q", ex.GroupID)
	}
}

func TestResolveInviteOfflinePushInfoAddsGroupID(t *testing.T) {
	s := &rtcServer{
		config: &Config{
			NotificationConfig: config.Notification{
				SignalingInvite: config.NotificationConfig{
					OfflinePush: config.OfflinePushConfig{Enable: true},
				},
			},
		},
	}
	inv := &rtc.InvitationInfo{
		RoomID:        "room-server",
		InviterUserID: "caller",
		MediaType:     "video",
		GroupID:       "g1",
		SessionType:   int32(constant.ReadGroupChatType),
	}
	push := s.resolveInviteOfflinePushInfo(t.Context(), inv, &sdkws.OfflinePushInfo{
		Title: "群通话邀请",
		Desc:  "邀请你加入群视频通话",
		Ex:    `{"pushType":"call","roomID":"client-room","sessionType":3}`,
	})
	if push == nil {
		t.Fatal("expected offline push info")
	}
	var ex callWakePushEx
	if err := jsonutil.JsonStringToStruct(push.Ex, &ex); err != nil {
		t.Fatalf("unmarshal ex: %v", err)
	}
	if ex.GroupID != "g1" {
		t.Fatalf("groupID=%q", ex.GroupID)
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

func TestResolveSignalingOfflinePushInfoOverridesInviteCopyForMissedCall(t *testing.T) {
	s := &rtcServer{
		config: &Config{
			NotificationConfig: config.Notification{
				SignalingInvite: config.NotificationConfig{
					OfflinePush: config.OfflinePushConfig{Enable: true},
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
	invitePush := &sdkws.OfflinePushInfo{
		Title:        "通话邀请",
		Desc:         "邀请你进行语音通话",
		IOSPushSound: callIOSPushSound,
		Ex:           `{"pushType":"call","roomID":"room-abc","sessionType":1}`,
	}

	timeoutPush := s.resolveSignalingOfflinePushInfo(t.Context(), inv, invitePush, signalCallActionTimeout, "caller")
	if timeoutPush == nil {
		t.Fatal("expected offline push info")
	}
	if timeoutPush.Title != "未接来电" {
		t.Fatalf("title=%q, want 未接来电", timeoutPush.Title)
	}
	if timeoutPush.Desc != "未接caller的语音通话" {
		t.Fatalf("desc=%q, want 未接caller的语音通话", timeoutPush.Desc)
	}
	if timeoutPush.IOSPushSound != "" {
		t.Fatalf("ios sound=%q, want empty for missed call", timeoutPush.IOSPushSound)
	}

	cancelPush := s.resolveSignalingOfflinePushInfo(t.Context(), inv, invitePush, signalCallActionCancel, "caller")
	if cancelPush.Title != "通话已取消" {
		t.Fatalf("cancel title=%q", cancelPush.Title)
	}
	if cancelPush.Desc != "caller已取消语音通话" {
		t.Fatalf("cancel desc=%q", cancelPush.Desc)
	}
}
