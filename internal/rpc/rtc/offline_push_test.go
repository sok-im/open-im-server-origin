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

func TestResolveInviteOfflinePushInfoSingleChatDefaults(t *testing.T) {
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
	push := s.resolveInviteOfflinePushInfo(t.Context(), inv, nil, "callee")
	if push == nil {
		t.Fatal("expected offline push info")
	}
	if push.Title != "SOK语音" {
		t.Fatalf("title=%q", push.Title)
	}
	if push.Desc != "caller邀请你语音通话" {
		t.Fatalf("desc=%q", push.Desc)
	}
	if push.IOSPushSound != callIOSPushSound {
		t.Fatalf("sound=%q", push.IOSPushSound)
	}
	if push.Ex == "" {
		t.Fatal("expected wake push ex")
	}
}

func TestResolveInviteOfflinePushInfoOverridesClientCopy(t *testing.T) {
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
		RoomID:        "room-6b96d6c5-4f7c-43bb-b997-03e3bcef94e0",
		InviterUserID: "8511557336",
		MediaType:     "video",
		SessionType:   int32(constant.SingleChatType),
	}
	push := s.resolveInviteOfflinePushInfo(t.Context(), inv, &sdkws.OfflinePushInfo{
		Title:        "通话邀请",
		Desc:         "邀请你进行视频通话",
		IOSPushSound: "default",
		Ex:           `{"pushType":"call","roomID":"room-6b96d6c5-4f7c-43bb-b997-03e3bcef94e0","sessionType":1,"mediaType":"video","sourceID":"8511557336"}`,
	}, "3602044002")
	if push == nil {
		t.Fatal("expected offline push info")
	}
	if push.Title != "SOK视频" {
		t.Fatalf("title=%q, want SOK视频", push.Title)
	}
	if push.Desc != "8511557336邀请你视频通话" {
		t.Fatalf("desc=%q, want 8511557336邀请你视频通话", push.Desc)
	}
	if push.IOSPushSound != "default" {
		t.Fatalf("ios sound should be preserved from client, got %q", push.IOSPushSound)
	}
}

func TestResolveInviteOfflinePushInfoGroupChatDefaults(t *testing.T) {
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
	push := s.resolveInviteOfflinePushInfo(t.Context(), inv, nil, "callee")
	if push == nil {
		t.Fatal("expected offline push info")
	}
	if push.Title != "群组" {
		t.Fatalf("title=%q", push.Title)
	}
	if push.Desc != "caller邀请你多人通话" {
		t.Fatalf("desc=%q", push.Desc)
	}
}

func TestSyncCallWakePushExAddsGroupID(t *testing.T) {
	clientEx := `{"pushType":"call","roomID":"client-room","sessionType":3}`
	got := syncCallWakePushEx(&rtc.InvitationInfo{
		RoomID:        "room-server",
		MediaType:     "video",
		InviterUserID: "caller",
		GroupID:       "g1",
	}, int32(constant.ReadGroupChatType), clientEx, callWakePushType)

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
	}, "invitee")
	if push == nil {
		t.Fatal("expected offline push info")
	}
	if push.Title != "群组" {
		t.Fatalf("title=%q", push.Title)
	}
	if push.Desc != "caller邀请你多人通话" {
		t.Fatalf("desc=%q", push.Desc)
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

func TestCallPushDisplayName(t *testing.T) {
	user := &sdkws.UserInfo{
		UserID:    "u1",
		FirstName: "Zhang",
		LastName:  "San",
		Nickname:  "nick",
	}
	if got := callPushDisplayName("三哥", user, "u1"); got != "三哥" {
		t.Fatalf("remark priority: got %q", got)
	}
	if got := callPushDisplayName("", user, "u1"); got != "Zhang San" {
		t.Fatalf("full name: got %q", got)
	}
	nicknameOnly := &sdkws.UserInfo{UserID: "u2", Nickname: "only-nick"}
	if got := callPushDisplayName("", nicknameOnly, "u2"); got != "only-nick" {
		t.Fatalf("nickname: got %q", got)
	}
	if got := callPushDisplayName("", nil, "u-empty"); got != "u-empty" {
		t.Fatalf("fallback: got %q", got)
	}
}

func TestSingleChatInviteOfflinePushCopy(t *testing.T) {
	if got := singleChatInviteOfflinePushTitle("语音"); got != "SOK语音" {
		t.Fatalf("title=%q", got)
	}
	if got := singleChatInviteOfflinePushTitle("视频"); got != "SOK视频" {
		t.Fatalf("title=%q", got)
	}
	if got := singleChatInviteOfflinePushDesc("张三", "视频"); got != "张三邀请你视频通话" {
		t.Fatalf("desc=%q", got)
	}
}

func TestCallPushActorNameFromUser(t *testing.T) {
	fullNameUser := &sdkws.UserInfo{
		UserID:    "u6293309c.3616",
		FirstName: "Tom",
		LastName:  "Smith",
		Nickname:  "nick",
	}
	if got := callPushActorNameFromUser(fullNameUser); got != "Tom Smith" {
		t.Fatalf("full name: got %q", got)
	}

	nicknameOnly := &sdkws.UserInfo{UserID: "u1", Nickname: "only-nick"}
	if got := callPushActorNameFromUser(nicknameOnly); got != "only-nick" {
		t.Fatalf("nickname: got %q", got)
	}

	if got := callPushActorNameFromUser(&sdkws.UserInfo{UserID: "u-empty"}); got != "u-empty" {
		t.Fatalf("userID fallback: got %q", got)
	}
}

func TestResolveSignalingOfflinePushInfoSingleChatMissedCall(t *testing.T) {
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

	timeoutPush := s.resolveSignalingOfflinePushInfo(t.Context(), inv, invitePush, signalCallActionTimeout, "caller", "callee")
	if timeoutPush == nil {
		t.Fatal("expected offline push info")
	}
	if timeoutPush.Title != "caller" {
		t.Fatalf("title=%q, want caller", timeoutPush.Title)
	}
	if timeoutPush.Desc != callOfflinePushMissedDesc {
		t.Fatalf("desc=%q, want %q", timeoutPush.Desc, callOfflinePushMissedDesc)
	}
	if timeoutPush.IOSPushSound != "" {
		t.Fatalf("ios sound=%q, want empty for missed call", timeoutPush.IOSPushSound)
	}
	var timeoutEx callWakePushEx
	if err := jsonutil.JsonStringToStruct(timeoutPush.Ex, &timeoutEx); err != nil {
		t.Fatalf("unmarshal timeout ex: %v", err)
	}
	if timeoutEx.PushType != callSignalingWakePushType {
		t.Fatalf("timeout pushType=%q, want %q", timeoutEx.PushType, callSignalingWakePushType)
	}

	cancelPush := s.resolveSignalingOfflinePushInfo(t.Context(), inv, invitePush, signalCallActionCancel, "caller", "callee")
	if cancelPush.Title != "caller" {
		t.Fatalf("cancel title=%q", cancelPush.Title)
	}
	if cancelPush.Desc != callOfflinePushMissedDesc {
		t.Fatalf("cancel desc=%q", cancelPush.Desc)
	}
	var cancelEx callWakePushEx
	if err := jsonutil.JsonStringToStruct(cancelPush.Ex, &cancelEx); err != nil {
		t.Fatalf("unmarshal cancel ex: %v", err)
	}
	if cancelEx.PushType != callSignalingWakePushType {
		t.Fatalf("cancel pushType=%q, want %q", cancelEx.PushType, callSignalingWakePushType)
	}
}

func TestResolveSignalingOfflinePushInfoGroupChatMissedCall(t *testing.T) {
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
		RoomID:        "room-group",
		InviterUserID: "caller",
		MediaType:     "video",
		GroupID:       "g1",
		SessionType:   int32(constant.ReadGroupChatType),
	}
	invitePush := &sdkws.OfflinePushInfo{
		Title:        "群通话邀请",
		Desc:         "caller邀请你多人通话",
		IOSPushSound: callIOSPushSound,
		Ex:           `{"pushType":"call","roomID":"room-group","sessionType":3}`,
	}

	timeoutPush := s.resolveSignalingOfflinePushInfo(t.Context(), inv, invitePush, signalCallActionTimeout, "caller", "callee")
	if timeoutPush == nil {
		t.Fatal("expected offline push info")
	}
	if timeoutPush.Title != "群组" {
		t.Fatalf("title=%q, want 群组", timeoutPush.Title)
	}
	if timeoutPush.Desc != callOfflinePushMissedDesc {
		t.Fatalf("desc=%q, want %q", timeoutPush.Desc, callOfflinePushMissedDesc)
	}
	var timeoutEx callWakePushEx
	if err := jsonutil.JsonStringToStruct(timeoutPush.Ex, &timeoutEx); err != nil {
		t.Fatalf("unmarshal timeout ex: %v", err)
	}
	if timeoutEx.PushType != callSignalingWakePushType {
		t.Fatalf("timeout pushType=%q, want %q", timeoutEx.PushType, callSignalingWakePushType)
	}
}
