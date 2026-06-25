package rtc

import (
	"context"
	"strings"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/rtc"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/jsonutil"
)

const (
	callWakePushSchemaVersion = 1
	callWakePushType          = "call"
	callIOSPushSound          = "call.caf"

	signalCallActionInvite  = "invite"
	signalCallActionCancel  = "cancel"
	signalCallActionReject  = "reject"
	signalCallActionTimeout = "timeout"
)

type callWakePushEx struct {
	SchemaVersion int    `json:"schemaVersion"`
	PushType      string `json:"pushType"`
	RoomID        string `json:"roomID"`
	SessionType   int32  `json:"sessionType"`
	MediaType     string `json:"mediaType,omitempty"`
	SourceID      string `json:"sourceID,omitempty"`
	GroupID       string `json:"groupID,omitempty"`
}

// resolveInviteOfflinePushInfo builds or completes OfflinePushInfo for call invites.
// Client-provided fields are preserved; missing title/desc/ex/sound are filled server-side.
func (s *rtcServer) resolveInviteOfflinePushInfo(ctx context.Context, inv *rtc.InvitationInfo, clientPush *sdkws.OfflinePushInfo) *sdkws.OfflinePushInfo {
	return s.resolveSignalingOfflinePushInfo(ctx, inv, clientPush, signalCallActionInvite, inv.GetInviterUserID())
}

// resolveSignalingOfflinePushInfo builds offline push info for invite / cancel / reject / timeout signaling.
func (s *rtcServer) resolveSignalingOfflinePushInfo(ctx context.Context, inv *rtc.InvitationInfo, clientPush *sdkws.OfflinePushInfo, action, actorUserID string) *sdkws.OfflinePushInfo {
	if inv == nil {
		log.ZWarn(ctx, "lintao resolveSignalingOfflinePushInfo: invitation is nil", nil, "action", action, "clientPushProvided", clientPush != nil)
		return clientPush
	}
	cfg := s.config.NotificationConfig.SignalingInvite
	log.ZInfo(ctx, "lintao resolveSignalingOfflinePushInfo start",
		"action", action,
		"roomID", inv.RoomID,
		"inviterUserID", inv.InviterUserID,
		"actorUserID", actorUserID,
		"groupID", inv.GroupID,
		"mediaType", inv.MediaType,
		"sessionType", inv.SessionType,
		"clientPushProvided", clientPush != nil,
		"configOfflinePushEnable", cfg.OfflinePush.Enable,
	)
	if !cfg.OfflinePush.Enable && clientPush == nil {
		log.ZInfo(ctx, "lintao resolveSignalingOfflinePushInfo skipped: config disabled and no client offlinePushInfo",
			"action", action,
			"roomID", inv.RoomID,
		)
		return nil
	}
	if clientPush != nil && !cfg.OfflinePush.Enable {
		push := cloneOfflinePushInfo(clientPush)
		sessionType := invitationSessionType(inv)
		push.Ex = syncCallWakePushEx(inv, sessionType, push.Ex)
		log.ZInfo(ctx, "lintao resolveSignalingOfflinePushInfo: using client offlinePushInfo only (config disabled)",
			"action", action,
			"roomID", inv.RoomID,
			"clientTitle", clientPush.Title,
			"clientExLen", len(clientPush.Ex),
		)
		return push
	}

	push := cloneOfflinePushInfo(clientPush)
	if push == nil {
		push = &sdkws.OfflinePushInfo{}
	}
	clientTitle := ""
	clientDesc := ""
	clientExLen := 0
	clientSound := ""
	if clientPush != nil {
		clientTitle = clientPush.Title
		clientDesc = clientPush.Desc
		clientExLen = len(clientPush.Ex)
		clientSound = clientPush.IOSPushSound
	}

	sessionType := invitationSessionType(inv)
	mediaLabel := callMediaLabel(inv.MediaType)

	if push.Title == "" {
		push.Title = cfg.OfflinePush.Title
	}
	if push.Title == "" {
		push.Title = "SOK"
	}
	if push.Desc == "" {
		push.Desc = cfg.OfflinePush.Desc
	}
	if push.Desc == "" {
		actorName := s.resolveUserDisplayName(ctx, inv.GroupID, actorUserID)
		push.Desc = callActionDefaultDesc(action, mediaLabel, actorName)
	}
	if push.Ex == "" && cfg.OfflinePush.Ext != "" {
		push.Ex = cfg.OfflinePush.Ext
	}
	push.Ex = syncCallWakePushEx(inv, sessionType, push.Ex)
	if action == signalCallActionInvite && push.IOSPushSound == "" {
		push.IOSPushSound = callIOSPushSound
	}
	log.ZInfo(ctx, "lintao resolveSignalingOfflinePushInfo done",
		"action", action,
		"roomID", inv.RoomID,
		"title", push.Title,
		"desc", push.Desc,
		"exLen", len(push.Ex),
		"iosPushSound", push.IOSPushSound,
		"clientTitle", clientTitle,
		"clientDesc", clientDesc,
		"clientExLen", clientExLen,
		"clientSound", clientSound,
		"titleFromClient", push.Title == clientTitle && clientTitle != "",
		"descFromClient", push.Desc == clientDesc && clientDesc != "",
		"exFromClient", push.Ex != "" && clientExLen > 0 && len(push.Ex) == clientExLen,
	)
	return push
}

func invitationSessionType(inv *rtc.InvitationInfo) int32 {
	sessionType := inv.SessionType
	if sessionType == 0 {
		if inv.GroupID != "" {
			sessionType = int32(constant.ReadGroupChatType)
		} else {
			sessionType = int32(constant.SingleChatType)
		}
	}
	return sessionType
}

func callActionDefaultDesc(action, mediaLabel, actorName string) string {
	switch action {
	case signalCallActionCancel:
		if actorName != "" {
			return actorName + "已取消" + mediaLabel + "通话"
		}
		return "对方已取消" + mediaLabel + "通话"
	case signalCallActionReject:
		if actorName != "" {
			return actorName + "已拒绝" + mediaLabel + "通话"
		}
		return "对方已拒绝" + mediaLabel + "通话"
	case signalCallActionTimeout:
		if actorName != "" {
			return "未接" + actorName + "的" + mediaLabel + "通话"
		}
		return "未接" + mediaLabel + "通话"
	default:
		if actorName != "" {
			return actorName + "邀请你" + mediaLabel + "通话"
		}
		return "你收到一条" + mediaLabel + "通话邀请"
	}
}

func offlinePushInfoFromInvitationModel(inv *model.SignalInvitation) *sdkws.OfflinePushInfo {
	if inv == nil || (inv.OfflinePushTitle == "" && inv.OfflinePushDesc == "" && inv.OfflinePushEx == "") {
		return nil
	}
	return &sdkws.OfflinePushInfo{
		Title: inv.OfflinePushTitle,
		Desc:  inv.OfflinePushDesc,
		Ex:    inv.OfflinePushEx,
	}
}

func (s *rtcServer) resolveInviterDisplayName(ctx context.Context, inv *rtc.InvitationInfo) string {
	if inv == nil || inv.InviterUserID == "" {
		return ""
	}
	return s.resolveUserDisplayName(ctx, inv.GroupID, inv.InviterUserID)
}

func (s *rtcServer) resolveUserDisplayName(ctx context.Context, groupID, userID string) string {
	if userID == "" {
		return ""
	}
	if groupID != "" {
		if member, err := s.groupClient.GetGroupMemberCache(ctx, groupID, userID); err == nil {
			if name := strings.TrimSpace(member.Nickname); name != "" {
				return name
			}
		} else {
			log.ZDebug(ctx, "lintao resolveUserDisplayName: GetGroupMemberCache failed", "groupID", groupID, "userID", userID, "err", err)
		}
	}
	if user, err := s.userClient.GetUserInfo(ctx, userID); err == nil {
		if name := strings.TrimSpace(user.Nickname); name != "" {
			return name
		}
		return user.UserID
	}
	log.ZDebug(ctx, "lintao resolveUserDisplayName: GetUserInfo failed, fallback userID", "userID", userID)
	return userID
}

func callMediaLabel(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "video":
		return "视频"
	default:
		return "语音"
	}
}

func buildCallWakePushEx(inv *rtc.InvitationInfo, sessionType int32) string {
	ex := callWakePushEx{}
	applyServerCallWakePushEx(&ex, inv, sessionType)
	return jsonutil.StructToJsonString(ex)
}

// syncCallWakePushEx merges server-authoritative invite fields into wake-push ex.
// Client-provided ex fields are preserved when parseable; invalid ex falls back to server build.
func syncCallWakePushEx(inv *rtc.InvitationInfo, sessionType int32, exJSON string) string {
	if inv == nil || inv.RoomID == "" {
		return exJSON
	}
	if exJSON == "" {
		return buildCallWakePushEx(inv, sessionType)
	}
	var ex callWakePushEx
	if err := jsonutil.JsonStringToStruct(exJSON, &ex); err != nil {
		return buildCallWakePushEx(inv, sessionType)
	}
	applyServerCallWakePushEx(&ex, inv, sessionType)
	return jsonutil.StructToJsonString(ex)
}

func applyServerCallWakePushEx(ex *callWakePushEx, inv *rtc.InvitationInfo, sessionType int32) {
	if ex == nil || inv == nil {
		return
	}
	ex.RoomID = inv.RoomID

	ex.GroupID = inv.GroupID

	if ex.SessionType == 0 {
		ex.SessionType = sessionType
	}
	if ex.PushType == "" {
		ex.PushType = callWakePushType
	}
	if ex.SchemaVersion == 0 {
		ex.SchemaVersion = callWakePushSchemaVersion
	}
	if ex.MediaType == "" {
		ex.MediaType = inv.MediaType
	}
	if ex.SourceID == "" {
		ex.SourceID = inv.InviterUserID
	}
}

func cloneOfflinePushInfo(src *sdkws.OfflinePushInfo) *sdkws.OfflinePushInfo {
	if src == nil {
		return nil
	}
	return &sdkws.OfflinePushInfo{
		Title:         src.Title,
		Desc:          src.Desc,
		Ex:            src.Ex,
		IOSPushSound:  src.IOSPushSound,
		IOSBadgeCount: src.IOSBadgeCount,
		SignalInfo:    src.SignalInfo,
	}
}
