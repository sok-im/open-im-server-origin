package rtc

import (
	"context"
	"strings"

	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
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
	callOfflinePushMissedDesc = "未接通话"

	signalCallActionInvite  = "invite"
	signalCallActionCancel  = "cancel"
	signalCallActionReject  = "reject"
	signalCallActionTimeout = "timeout"
	signalCallActionHungUp  = "hungup"
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

// resolveInviteOfflinePushInfo builds OfflinePushInfo for call invites.
// Server-side title/desc are authoritative; client-provided copy is replaced.
// calleeUserID is the push recipient; when set, caller display name uses callee-side remark.
func (s *rtcServer) resolveInviteOfflinePushInfo(ctx context.Context, inv *rtc.InvitationInfo, clientPush *sdkws.OfflinePushInfo, calleeUserID string) *sdkws.OfflinePushInfo {
	return s.resolveSignalingOfflinePushInfo(ctx, inv, clientPush, signalCallActionInvite, inv.GetInviterUserID(), calleeUserID)
}

// resolveSignalingOfflinePushInfo builds offline push info for invite / cancel / reject / timeout signaling.
func (s *rtcServer) resolveSignalingOfflinePushInfo(ctx context.Context, inv *rtc.InvitationInfo, clientPush *sdkws.OfflinePushInfo, action, actorUserID, calleeUserID string) *sdkws.OfflinePushInfo {
	if inv == nil {
		log.ZWarn(ctx, "resolveSignalingOfflinePushInfo: invitation is nil", nil, "action", action, "clientPushProvided", clientPush != nil)
		return clientPush
	}
	cfg := s.config.NotificationConfig.SignalingInvite
	log.ZInfo(ctx, "resolveSignalingOfflinePushInfo start",
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
		log.ZInfo(ctx, "resolveSignalingOfflinePushInfo skipped: config disabled and no client offlinePushInfo",
			"action", action,
			"roomID", inv.RoomID,
		)
		return nil
	}
	if clientPush != nil && !cfg.OfflinePush.Enable {
		push := cloneOfflinePushInfo(clientPush)
		sessionType := invitationSessionType(inv)
		if action != signalCallActionInvite {
			if sessionType == int32(constant.SingleChatType) {
				s.applySingleChatMissedCallOfflinePushCopy(ctx, push, inv, actorUserID, calleeUserID)
			} else {
				s.applyGroupChatMissedCallOfflinePushCopy(push)
			}
			push.IOSPushSound = ""
		} else if sessionType == int32(constant.SingleChatType) {
			s.applySingleChatInviteOfflinePushCopy(ctx, push, inv, calleeUserID)
		} else {
			s.applyGroupChatInviteOfflinePushCopy(ctx, push, inv, calleeUserID)
		}
		push.Ex = syncCallWakePushEx(inv, sessionType, push.Ex)
		log.ZInfo(ctx, "resolveSignalingOfflinePushInfo: using client offlinePushInfo only (config disabled)",
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

	if action != signalCallActionInvite {
		if sessionType == int32(constant.SingleChatType) {
			s.applySingleChatMissedCallOfflinePushCopy(ctx, push, inv, actorUserID, calleeUserID)
		} else {
			s.applyGroupChatMissedCallOfflinePushCopy(push)
		}
		push.IOSPushSound = ""
	} else {
		if sessionType == int32(constant.SingleChatType) {
			s.applySingleChatInviteOfflinePushCopy(ctx, push, inv, calleeUserID)
		} else {
			s.applyGroupChatInviteOfflinePushCopy(ctx, push, inv, calleeUserID)
		}
		if push.IOSPushSound == "" {
			push.IOSPushSound = callIOSPushSound
		}
	}
	if push.Ex == "" && cfg.OfflinePush.Ext != "" {
		push.Ex = cfg.OfflinePush.Ext
	}
	push.Ex = syncCallWakePushEx(inv, sessionType, push.Ex)
	// Call invite push should wake the app, not increment the IM unread badge.
	push.IOSBadgeCount = false
	log.ZInfo(ctx, "resolveSignalingOfflinePushInfo done",
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

func callActionDefaultTitle(action, mediaLabel string) string {
	switch action {
	case signalCallActionCancel:
		return "未接来电"
	case signalCallActionReject:
		return "未接来电"
	case signalCallActionTimeout:
		return "未接来电"
	default:
		return "来电"
	}
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
	if groupID != "" {
		return s.resolveGroupDisplayName(ctx, groupID)
	}
	return s.resolveSingleChatUserDisplayName(ctx, userID)
}

func (s *rtcServer) resolveGroupDisplayName(ctx context.Context, groupID string) string {
	if groupID == "" {
		return ""
	}
	if s.groupClient != nil {
		if groupInfo, err := s.groupClient.GetGroupInfoCache(ctx, groupID); err == nil {
			if name := strings.TrimSpace(groupInfo.GroupName); name != "" {
				return name
			}
		} else {
			log.ZDebug(ctx, "resolveGroupDisplayName: GetGroupInfoCache failed", "groupID", groupID, "err", err)
		}
	}
	return groupID
}

func (s *rtcServer) resolveSingleChatUserDisplayName(ctx context.Context, userID string) string {
	return s.resolveSingleChatCallDisplayName(ctx, "", userID)
}

func (s *rtcServer) resolveSingleChatCallDisplayName(ctx context.Context, calleeUserID, callerUserID string) string {
	if callerUserID == "" {
		return ""
	}
	var user *sdkws.UserInfo
	if s.userClient != nil {
		if u, err := s.userClient.GetUserInfo(ctx, callerUserID); err == nil {
			user = u
		} else {
			log.ZDebug(ctx, "resolveSingleChatCallDisplayName: GetUserInfo failed", "callerUserID", callerUserID, "err", err)
		}
	}
	remark := s.resolveFriendRemark(ctx, calleeUserID, callerUserID)
	return callPushDisplayName(remark, user, callerUserID)
}

func (s *rtcServer) resolveFriendRemark(ctx context.Context, ownerUserID, friendUserID string) string {
	if ownerUserID == "" || friendUserID == "" || s.relationClient == nil {
		return ""
	}
	friends, err := s.relationClient.GetFriendsInfo(ctx, ownerUserID, []string{friendUserID})
	if err != nil {
		log.ZDebug(ctx, "resolveFriendRemark: GetFriendsInfo failed", "ownerUserID", ownerUserID, "friendUserID", friendUserID, "err", err)
		return ""
	}
	if len(friends) == 0 || friends[0] == nil {
		return ""
	}
	return friends[0].Remark
}

func callPushDisplayName(remark string, user *sdkws.UserInfo, userIDFallback string) string {
	if name := convert.DisplayNickname(remark, user); name != "" {
		return name
	}
	if userIDFallback != "" {
		return userIDFallback
	}
	return ""
}

func callPushActorNameFromUser(user *sdkws.UserInfo) string {
	if user == nil {
		return ""
	}
	if name := convert.MemberDisplayNickname(user); name != "" {
		return name
	}
	return user.UserID
}

func (s *rtcServer) applySingleChatInviteOfflinePushCopy(ctx context.Context, push *sdkws.OfflinePushInfo, inv *rtc.InvitationInfo, calleeUserID string) {
	if push == nil || inv == nil {
		return
	}
	mediaLabel := callMediaLabel(inv.MediaType)
	push.Title = singleChatInviteOfflinePushTitle(mediaLabel)
	inviterName := s.resolveSingleChatCallDisplayName(ctx, calleeUserID, inv.InviterUserID)
	push.Desc = singleChatInviteOfflinePushDesc(inviterName, mediaLabel)
}

func singleChatInviteOfflinePushTitle(mediaLabel string) string {
	return "SOK" + mediaLabel
}

func singleChatInviteOfflinePushDesc(inviterName, mediaLabel string) string {
	if inviterName != "" {
		return inviterName + "邀请你" + mediaLabel + "通话"
	}
	return "邀请你" + mediaLabel + "通话"
}

func (s *rtcServer) applyGroupChatInviteOfflinePushCopy(ctx context.Context, push *sdkws.OfflinePushInfo, inv *rtc.InvitationInfo, calleeUserID string) {
	if push == nil || inv == nil {
		return
	}
	push.Title = "群组"
	inviterName := s.resolveSingleChatCallDisplayName(ctx, calleeUserID, inv.InviterUserID)
	push.Desc = inviterName + "邀请你多人通话"
}

func (s *rtcServer) applyGroupChatMissedCallOfflinePushCopy(push *sdkws.OfflinePushInfo) {
	if push == nil {
		return
	}
	push.Title = "群组"
	push.Desc = callOfflinePushMissedDesc
}

func (s *rtcServer) applySingleChatMissedCallOfflinePushCopy(ctx context.Context, push *sdkws.OfflinePushInfo, inv *rtc.InvitationInfo, actorUserID, calleeUserID string) {
	if push == nil || inv == nil {
		return
	}
	titleUserID := inv.InviterUserID
	if titleUserID == "" {
		titleUserID = actorUserID
	}
	push.Title = s.resolveSingleChatCallDisplayName(ctx, calleeUserID, titleUserID)
	push.Desc = callOfflinePushMissedDesc
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
