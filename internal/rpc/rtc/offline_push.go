package rtc

import (
	"context"
	"strings"

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
	if inv == nil {
		log.ZWarn(ctx, "lintao resolveInviteOfflinePushInfo: invitation is nil", nil, "clientPushProvided", clientPush != nil)
		return clientPush
	}
	cfg := s.config.NotificationConfig.SignalingInvite
	log.ZInfo(ctx, "lintao resolveInviteOfflinePushInfo start",
		"roomID", inv.RoomID,
		"inviterUserID", inv.InviterUserID,
		"groupID", inv.GroupID,
		"mediaType", inv.MediaType,
		"sessionType", inv.SessionType,
		"clientPushProvided", clientPush != nil,
		"configOfflinePushEnable", cfg.OfflinePush.Enable,
	)
	if !cfg.OfflinePush.Enable && clientPush == nil {
		log.ZInfo(ctx, "lintao resolveInviteOfflinePushInfo skipped: config disabled and no client offlinePushInfo",
			"roomID", inv.RoomID,
		)
		return nil
	}
	if clientPush != nil && !cfg.OfflinePush.Enable {
		push := cloneOfflinePushInfo(clientPush)
		sessionType := inv.SessionType
		if sessionType == 0 {
			if inv.GroupID != "" {
				sessionType = int32(constant.ReadGroupChatType)
			} else {
				sessionType = int32(constant.SingleChatType)
			}
		}
		push.Ex = syncCallWakePushEx(inv, sessionType, push.Ex)
		log.ZInfo(ctx, "lintao resolveInviteOfflinePushInfo: using client offlinePushInfo only (config disabled)",
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

	sessionType := inv.SessionType
	if sessionType == 0 {
		if inv.GroupID != "" {
			sessionType = int32(constant.ReadGroupChatType)
		} else {
			sessionType = int32(constant.SingleChatType)
		}
	}

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
		inviterName := s.resolveInviterDisplayName(ctx, inv)
		if inviterName != "" {
			push.Desc = inviterName + "邀请你" + mediaLabel + "通话"
		} else {
			push.Desc = "你收到一条" + mediaLabel + "通话邀请"
		}
	}
	if push.Ex == "" && cfg.OfflinePush.Ext != "" {
		push.Ex = cfg.OfflinePush.Ext
	}
	push.Ex = syncCallWakePushEx(inv, sessionType, push.Ex)
	if push.IOSPushSound == "" {
		push.IOSPushSound = callIOSPushSound
	}
	log.ZInfo(ctx, "lintao resolveInviteOfflinePushInfo done",
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

func (s *rtcServer) resolveInviterDisplayName(ctx context.Context, inv *rtc.InvitationInfo) string {
	if inv == nil || inv.InviterUserID == "" {
		return ""
	}
	if inv.GroupID != "" {
		if member, err := s.groupClient.GetGroupMemberCache(ctx, inv.GroupID, inv.InviterUserID); err == nil {
			if name := strings.TrimSpace(member.Nickname); name != "" {
				return name
			}
		} else {
			log.ZDebug(ctx, "lintao resolveInviterDisplayName: GetGroupMemberCache failed", "groupID", inv.GroupID, "inviterUserID", inv.InviterUserID, "err", err)
		}
	}
	if user, err := s.userClient.GetUserInfo(ctx, inv.InviterUserID); err == nil {
		if name := strings.TrimSpace(user.Nickname); name != "" {
			log.ZDebug(ctx, "lintao resolveInviterDisplayName: use user nickname", "inviterUserID", inv.InviterUserID, "nickname", name)
			return name
		}
		log.ZDebug(ctx, "lintao resolveInviterDisplayName: user nickname empty, fallback userID", "inviterUserID", inv.InviterUserID)
		return user.UserID
	} else {
		log.ZDebug(ctx, "lintao resolveInviterDisplayName: GetUserInfo failed, fallback userID", "inviterUserID", inv.InviterUserID, "err", err)
	}
	return inv.InviterUserID
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
	ex := callWakePushEx{
		SchemaVersion: callWakePushSchemaVersion,
		PushType:      callWakePushType,
		RoomID:        inv.RoomID,
		SessionType:   sessionType,
		MediaType:     inv.MediaType,
		SourceID:      inv.InviterUserID,
	}
	if inv.GroupID != "" {
		ex.GroupID = inv.GroupID
	}
	return jsonutil.StructToJsonString(ex)
}

// syncCallWakePushEx ensures wake-push ex carries the server-authoritative roomID.
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
	ex.RoomID = inv.RoomID
	return jsonutil.StructToJsonString(ex)
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
