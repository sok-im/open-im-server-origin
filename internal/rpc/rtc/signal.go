// Copyright © 2024 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rtc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/livekit/protocol/auth"
	livekit "github.com/livekit/protocol/livekit"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/protocol/constant"
	pbmsg "github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/rtc"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/jsonutil"
	"github.com/twitchtv/twirp"
	"go.mongodb.org/mongo-driver/mongo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// SignalMessageAssemble processes a signal request from the WebSocket gateway
// and assembles the appropriate signal response, sending notifications to peers.
func (s *rtcServer) SignalMessageAssemble(ctx context.Context, req *rtc.SignalMessageAssembleReq) (*rtc.SignalMessageAssembleResp, error) {
	if req.SignalReq == nil {
		return nil, errs.ErrArgs.WrapMsg("signalReq is nil")
	}
	var (
		resp    rtc.SignalResp
		respErr error
	)
	switch payload := req.SignalReq.Payload.(type) {
	case *rtc.SignalReq_Invite:
		log.ZInfo(ctx, "SignalMessageAssemble", "payload", payload.Invite)
		r, err := s.handleInvite(ctx, payload.Invite, req.SignalReq)
		resp.Payload = &rtc.SignalResp_Invite{Invite: r}
		respErr = err
	case *rtc.SignalReq_InviteInGroup:
		r, err := s.handleInviteInGroup(ctx, payload.InviteInGroup, req.SignalReq)
		resp.Payload = &rtc.SignalResp_InviteInGroup{InviteInGroup: r}
		respErr = err
	case *rtc.SignalReq_Cancel:
		r, err := s.handleCancel(ctx, payload.Cancel, req.SignalReq)
		resp.Payload = &rtc.SignalResp_Cancel{Cancel: r}
		respErr = err
	case *rtc.SignalReq_Accept:
		r, err := s.handleAccept(ctx, payload.Accept, req.SignalReq)
		resp.Payload = &rtc.SignalResp_Accept{Accept: r}
		respErr = err
	case *rtc.SignalReq_HungUp:
		r, err := s.handleHungUp(ctx, payload.HungUp, req.SignalReq)
		resp.Payload = &rtc.SignalResp_HungUp{HungUp: r}
		respErr = err
	case *rtc.SignalReq_Reject:
		r, err := s.handleReject(ctx, payload.Reject, req.SignalReq)
		resp.Payload = &rtc.SignalResp_Reject{Reject: r}
		respErr = err
	case *rtc.SignalReq_GetTokenByRoomID:
		r, err := s.handleGetTokenByRoomID(ctx, payload.GetTokenByRoomID)
		resp.Payload = &rtc.SignalResp_GetTokenByRoomID{GetTokenByRoomID: r}
		respErr = err
	case *rtc.SignalReq_Timeout:
		r, err := s.handleTimeout(ctx, payload.Timeout, req.SignalReq)
		resp.Payload = &rtc.SignalResp_Timeout{Timeout: r}
		respErr = err
	case *rtc.SignalReq_Join:
		r, err := s.handleJoin(ctx, payload.Join, req.SignalReq)
		resp.Payload = &rtc.SignalResp_Join{Join: r}
		respErr = err
	case *rtc.SignalReq_Heartbeat:
		r, err := s.handleHeartbeat(ctx, payload.Heartbeat)
		resp.Payload = &rtc.SignalResp_Heartbeat{Heartbeat: r}
		respErr = err
	default:
		return nil, errs.ErrArgs.WrapMsg("unknown signal payload type")
	}
	if respErr != nil {
		log.ZError(ctx, "SignalMessageAssemble", respErr, "req", req)
		return nil, respErr
	}
	return &rtc.SignalMessageAssembleResp{SignalResp: &resp}, nil
}

// handleInvite processes a 1-to-1 call invitation.
func (s *rtcServer) handleInvite(ctx context.Context, req *rtc.SignalInviteReq, signalReq *rtc.SignalReq) (*rtc.SignalInviteResp, error) {
	log.ZDebug(ctx, "handleInvite: start", "req", req)

	inv := req.Invitation
	if inv == nil {
		log.ZError(ctx, "handleInvite", errs.ErrArgs, "r", "invitation is nil", "req", req)
		return nil, errs.ErrArgs.WrapMsg("invitation is nil")
	}
	inv.RoomID = newRoomID()
	inv.InviterUserID = req.UserID
	inv.InitiateTime = time.Now().UnixMilli()

	if len(inv.InviteeUserIDList) == 0 {
		log.ZError(ctx, "handleInvite", errs.ErrArgs, "r", "no invitees", "req", req)
		return nil, errs.ErrArgs.WrapMsg("no invitees", "inviteeUserIDList", inv.InviteeUserIDList)
	}

	if err := s.verifyInviterGlobalStatus(ctx, req.UserID); err != nil {
		log.ZError(ctx, "handleInvite: verifyInviterGlobalStatus failed", err, "req", req)
		return nil, err
	}

	notAllowUserIDs, notAllowSet, blacklistedSet, globalBlockedSet, missingUserSet, err := s.filterNotAllowedInvitees(ctx, req.UserID, inv.InviteeUserIDList, false)
	if err != nil {
		log.ZError(ctx, "handleInvite: filterNotAllowedInvitees failed", err, "req", req)
		return nil, err
	}
	inv.NotAllowUserIDList = notAllowUserIDs

	if len(notAllowUserIDs) == len(inv.InviteeUserIDList) {
		return nil, callInviteAllNotAllowedErr(blacklistedSet, globalBlockedSet, missingUserSet, inv.InviteeUserIDList)
	}

	// 从主叫用户资料获取铃声 URL，注入到邀请信息中，被叫方收到后播放主叫方铃声
	if inviterInfo, err := s.userClient.GetUserInfo(ctx, req.UserID); err == nil && inviterInfo.CallRingtoneURL != "" {
		if model.NotificationSwitchToBool(inviterInfo.AvCallRingtone) {
			inv.CallerRingtoneURL = inviterInfo.CallRingtoneURL
		}
	}

	// 查询被叫方铃声 URL，供主叫方在等待时播放
	var calleeRingtoneURL string
	for _, inviteeID := range inv.InviteeUserIDList {
		if _, notAllow := notAllowSet[inviteeID]; notAllow {
			continue
		}
		if inviteeInfo, err := s.userClient.GetUserInfo(ctx, inviteeID); err == nil {
			if model.NotificationSwitchToBool(inviteeInfo.PlayCalleeRingtoneOnAnswer) {
				calleeRingtoneURL = inviteeInfo.CallRingtoneURL
			}
		}
		break
	}

	// 单聊忙线检查：若被叫方当前处于振铃中（Connecting）或通话中（InCall），
	// 立即发送"忙线"通话记录并终止本次呼叫。仅对单聊生效。
	if inv.GroupID == "" {
		for _, inviteeID := range inv.InviteeUserIDList {
			if _, notAllow := notAllowSet[inviteeID]; notAllow {
				continue
			}
			if s.isCalleeOnActiveCall(ctx, inviteeID) {
				log.ZInfo(ctx, "handleInvite: invitee is busy, sending busy record", "inviteeID", inviteeID, "roomID", inv.RoomID)
				// 构造最小化的邀请模型用于发送通话记录消息（不写 DB、不创建 LiveKit 房间）。
				busyInv := &model.SignalInvitation{
					RoomID:            inv.RoomID,
					InviterUserID:     inv.InviterUserID,
					InviteeUserIDList: inv.InviteeUserIDList,
					MediaType:         inv.MediaType,
					GroupID:           inv.GroupID,
				}
				s.sendCallRecordChatMsg(ctx, busyInv, callStatusBusy, 0)
				return nil, servererrs.ErrAllUserBusy.WrapMsg("invitee is already on a call", "inviteeID", inviteeID)
			}
		}
	}

	if _, err := s.roomClient.CreateRoom(ctx, &livekit.CreateRoomRequest{Name: inv.RoomID}); err != nil {
		log.ZError(ctx, "handleInvite: LiveKit CreateRoom failed", err, "roomID", inv.RoomID, "req", req)
		return nil, errs.WrapMsg(err, "LiveKit CreateRoom failed", "roomID", inv.RoomID)
	}

	token, err := s.genToken(inv.RoomID, req.UserID)
	if err != nil {
		if _, delErr := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: inv.RoomID}); delErr != nil {
			log.ZWarn(ctx, "handleInvite: rollback DeleteRoom failed", delErr, "roomID", inv.RoomID)
		}
		log.ZError(ctx, "handleInvite: genToken failed", err, "roomID", inv.RoomID, "req", req)
		return nil, err
	}

	var storeCalleeID string
	for _, inviteeID := range inv.InviteeUserIDList {
		if _, notAllow := notAllowSet[inviteeID]; !notAllow {
			storeCalleeID = inviteeID
			break
		}
	}
	inviteOfflinePush := s.resolveInviteOfflinePushInfo(ctx, inv, req.OfflinePushInfo, storeCalleeID)
	if inviteOfflinePush == nil {
		log.ZWarn(ctx, "handleInvite: invite offline push info is nil, callee may not receive offline call push",
			nil, "roomID", inv.RoomID, "inviterUserID", req.UserID, "inviteeUserIDList", inv.InviteeUserIDList)
	}

	if err := s.db.CreateInvitation(ctx, invitationToModel(inv, inviteOfflinePush)); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			log.ZWarn(ctx, "handleInvite: duplicate invitation (idempotent retry)", err, "roomID", inv.RoomID)
		} else {
			if _, delErr := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: inv.RoomID}); delErr != nil {
				log.ZWarn(ctx, "handleInvite: rollback DeleteRoom failed", delErr, "roomID", inv.RoomID)
			}
			return nil, errs.WrapMsg(err, "CreateInvitation failed", "roomID", inv.RoomID)
		}
	}

	content, err := marshalSignalReq(signalReq)
	if err != nil {
		log.ZError(ctx, "handleInvite: marshalSignalReq failed", err, "roomID", inv.RoomID, "req", req)
		return nil, err
	}

	for _, inviteeID := range inv.InviteeUserIDList {
		if _, notAllow := notAllowSet[inviteeID]; notAllow {
			log.ZInfo(ctx, "handleInvite: skip not-allowed invitee", "inviteeID", inviteeID)
			continue
		}
		log.ZInfo(ctx, "sendSignalingNotification to invitee", "sendID", req.UserID, "recvID", inviteeID)
		inviteeOfflinePush := s.resolveInviteOfflinePushInfo(ctx, inv, req.OfflinePushInfo, inviteeID)
		if err := s.sendSignalingNotification(ctx, req.UserID, inviteeID, int32(constant.SingleChatType), "", inviteeOfflinePush, content); err != nil {
			log.ZError(ctx, "sendSignalingNotification to invitee failed", err, "inviteeID", inviteeID)
			return nil, errs.WrapMsg(err, "failed to notify invitee", "inviteeID", inviteeID)
		}
	}

	// Mark inviter and all reachable invitees as "connecting".
	s.setCallStatusConnecting(ctx, inv, notAllowSet)

	log.ZDebug(ctx, "handleInvite", "token", token, "roomID", inv.RoomID, "liveURL", s.config.RpcConfig.LiveKit.ExternalAddress)
	return &rtc.SignalInviteResp{
		Token:              token,
		RoomID:             inv.RoomID,
		LiveURL:            s.config.RpcConfig.LiveKit.ExternalAddress,
		NotAllowUserIDList: notAllowUserIDs,
		CalleeRingtoneURL:  calleeRingtoneURL,
		CallerRingtoneURL:  inv.CallerRingtoneURL,
	}, nil
}

// handleInviteInGroup processes a group call invitation.
func (s *rtcServer) handleInviteInGroup(ctx context.Context, req *rtc.SignalInviteInGroupReq, signalReq *rtc.SignalReq) (*rtc.SignalInviteInGroupResp, error) {

	log.ZDebug(ctx, "handleInviteInGroup: start", "req", req)

	inv := req.Invitation
	if inv == nil {
		log.ZError(ctx, "handleInviteInGroup", errs.ErrArgs, "r", "invitation is nil", "req", req)
		return nil, errs.ErrArgs.WrapMsg("invitation is nil")
	}
	if inv.GroupID == "" {
		log.ZError(ctx, "handleInviteInGroup", errs.ErrArgs, "r", "groupID is empty", "req", req)
		return nil, errs.ErrArgs.WrapMsg("groupID is empty")
	}

	inv.RoomID = newRoomID()
	inv.InviterUserID = req.UserID
	inv.InitiateTime = time.Now().UnixMilli()

	log.ZDebug(ctx, "handleInviteInGroup: start", "req", req)

	if err := s.verifyInviterGlobalStatus(ctx, req.UserID); err != nil {
		log.ZError(ctx, "handleInviteInGroup: verifyInviterGlobalStatus failed", err, "req", req)
		return nil, err
	}

	notAllowUserIDs, notAllowSet, blacklistedSet, globalBlockedSet, missingUserSet, err := s.filterNotAllowedInvitees(ctx, req.UserID, inv.InviteeUserIDList, true)
	if err != nil {
		log.ZError(ctx, "handleInviteInGroup: filterNotAllowedInvitees failed", err, "req", req)
		return nil, err
	}
	inv.NotAllowUserIDList = notAllowUserIDs

	if len(notAllowUserIDs) == len(inv.InviteeUserIDList) {
		err := callInviteAllNotAllowedErr(blacklistedSet, globalBlockedSet, missingUserSet, inv.InviteeUserIDList)
		log.ZError(ctx, "handleInviteInGroup: all invitees not allowed", err, "inviteeUserIDList", inv.InviteeUserIDList, "req", req)
		return nil, err
	}

	// 群聊忙线过滤：将当前处于振铃中（Connecting）或通话中（InCall）的被叫方加入 notAllowSet，
	// 使其在后续通知循环与 setCallStatusConnecting 中被自动跳过。
	// 若过滤后所有被叫均不可达，返回 ErrAllUserBusy。
	var busyUserIDs []string
	for _, inviteeID := range inv.InviteeUserIDList {
		if _, notAllow := notAllowSet[inviteeID]; notAllow {
			log.ZDebug(ctx, "handleInviteInGroup: skip not-allowed invitee", "inviteeID", inviteeID)
			continue
		}
		if s.isCalleeOnActiveCall(ctx, inviteeID) {
			busyUserIDs = append(busyUserIDs, inviteeID)
			notAllowSet[inviteeID] = struct{}{}
		}
	}
	if len(busyUserIDs) > 0 {
		notAllowUserIDs = append(notAllowUserIDs, busyUserIDs...)
		inv.NotAllowUserIDList = notAllowUserIDs
		log.ZDebug(ctx, "handleInviteInGroup: filtered busy invitees", "busyUserIDs", busyUserIDs, "roomID", inv.RoomID)
		// 如果所有被叫都忙线，终止呼叫。
		reachable := 0
		for _, uid := range inv.InviteeUserIDList {
			if _, skip := notAllowSet[uid]; !skip {
				reachable++
			}
		}
		if reachable == 0 {

			s.sendGroupCallStartedNotification(ctx, inv.GroupID, inv.InviterUserID, inv.MediaType)

			s.sendGroupCallEndedNotification(ctx, inv.GroupID, inv.InviterUserID, inv.MediaType, 0, signalCallActionTimeout)

			log.ZError(ctx, "handleInviteInGroup: all invitees are in a call", servererrs.ErrAllUserBusy, "inviteeUserIDList", inv.InviteeUserIDList)

			return nil, servererrs.ErrAllUserBusy.WrapMsg("all invitees are already in a call", "inviteeUserIDList", inv.InviteeUserIDList)
		}
	}

	// 从主叫用户资料获取铃声 URL，注入到邀请s信息中，被叫方收到后播放主叫方铃声
	if inviterInfo, err := s.userClient.GetUserInfo(ctx, req.UserID); err == nil && inviterInfo.CallRingtoneURL != "" {
		inv.CallerRingtoneURL = inviterInfo.CallRingtoneURL
	}

	// 查询第一位可邀请被叫的铃声 URL，供主叫方在等待时播放
	var calleeRingtoneURL string
	for _, inviteeID := range inv.InviteeUserIDList {
		if _, notAllow := notAllowSet[inviteeID]; notAllow {
			continue
		}
		if inviteeInfo, err := s.userClient.GetUserInfo(ctx, inviteeID); err == nil {
			calleeRingtoneURL = inviteeInfo.CallRingtoneURL
		}
		break
	}

	if _, err := s.roomClient.CreateRoom(ctx, &livekit.CreateRoomRequest{Name: inv.RoomID}); err != nil {
		log.ZError(ctx, "handleInviteInGroup: LiveKit CreateRoom failed", err, "roomID", inv.RoomID, "req", req)
		return nil, errs.WrapMsg(err, "LiveKit CreateRoom failed", "roomID", inv.RoomID)
	}

	token, err := s.genToken(inv.RoomID, req.UserID)
	if err != nil {
		if _, delErr := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: inv.RoomID}); delErr != nil {
			log.ZWarn(ctx, "handleInviteInGroup: rollback DeleteRoom failed", delErr, "roomID", inv.RoomID)
		}
		log.ZError(ctx, "handleInviteInGroup: genToken failed", err, "roomID", inv.RoomID, "req", req)
		return nil, err
	}

	var storeCalleeID string
	for _, inviteeID := range inv.InviteeUserIDList {
		if _, notAllow := notAllowSet[inviteeID]; !notAllow {
			storeCalleeID = inviteeID
			break
		}
	}

	inviteOfflinePush := s.resolveInviteOfflinePushInfo(ctx, inv, req.OfflinePushInfo, storeCalleeID)
	if inviteOfflinePush == nil {
		log.ZWarn(ctx, "handleInviteInGroup: invite offline push info is nil, callee may not receive offline call push",
			nil, "roomID", inv.RoomID, "groupID", inv.GroupID, "inviterUserID", req.UserID, "inviteeUserIDList", inv.InviteeUserIDList)
	}

	if err := s.db.CreateInvitation(ctx, invitationToModel(inv, inviteOfflinePush)); err != nil {
		if !mongo.IsDuplicateKeyError(err) {
			if _, delErr := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: inv.RoomID}); delErr != nil {
				log.ZWarn(ctx, "handleInviteInGroup: rollback DeleteRoom failed", delErr, "roomID", inv.RoomID)
			} else {
				log.ZWarn(ctx, "handleInviteInGroup: DeleteRoom failed", delErr, "roomID", inv.RoomID)
			}
			return nil, errs.WrapMsg(err, "CreateInvitation failed", "roomID", inv.RoomID)
		}
		log.ZWarn(ctx, "handleInviteInGroup: duplicate invitation (idempotent retry)", err, "roomID", inv.RoomID)
	}

	content, err := marshalSignalReq(signalReq)
	if err != nil {
		log.ZError(ctx, "handleInviteInGroup: marshalSignalReq failed", err, "roomID", inv.RoomID, "req", req)
		return nil, err
	}
	for _, inviteeID := range inv.InviteeUserIDList {
		if _, notAllow := notAllowSet[inviteeID]; notAllow {
			log.ZInfo(ctx, "handleInviteInGroup: skipping invitee (call setting blocked)", "inviteeID", inviteeID)
			continue
		}

		log.ZInfo(ctx, "handleInviteInGroup: sending signaling notification to invitee", "req", req, "inviteOfflinePush", inviteOfflinePush)

		inviteeOfflinePush := s.resolveInviteOfflinePushInfo(ctx, inv, req.OfflinePushInfo, inviteeID)
		if err := s.sendSignalingNotification(ctx, req.UserID, inviteeID, int32(constant.ReadGroupChatType), inv.GroupID, inviteeOfflinePush, content); err != nil {
			log.ZWarn(ctx, "handleInviteInGroup to group invitee failed", err, "inviteeID", inviteeID)
			return nil, errs.WrapMsg(err, "failed to notify invitee", "inviteeID", inviteeID)
		}
	}

	// Mark inviter and all reachable invitees as "connecting".
	s.setCallStatusConnecting(ctx, inv, notAllowSet)

	// Notify every group member who was NOT explicitly invited so they can
	// render the "call in progress" banner and optionally join.
	// Run in a goroutine so large groups don't block the caller's response.

	log.ZDebug(ctx, "handleInviteInGroup: broadcastGroupCallStatusToNonInvited", "inv", inv)

	s.broadcastGroupCallStatusToNonInvited(ctx, inv.GroupID, inv.RoomID, inv.MediaType, inv.InviterUserID, inv.InviteeUserIDList, GroupCallStatusOngoing)

	// Send a group-chat timeline notification to all members: "XXX started an audio/video call".
	s.sendGroupCallStartedNotification(ctx, inv.GroupID, inv.InviterUserID, inv.MediaType)

	resp := &rtc.SignalInviteInGroupResp{
		Token:              token,
		RoomID:             inv.RoomID,
		LiveURL:            s.config.RpcConfig.LiveKit.ExternalAddress,
		NotAllowUserIDList: notAllowUserIDs,
		CalleeRingtoneURL:  calleeRingtoneURL,
	}

	log.ZDebug(ctx, "handleInviteInGroup", "req", req, "resp", resp)

	return resp, nil
}

// filterNotAllowedInvitees 过滤不可邀请的被叫用户。
// skipCallAcceptSetting 为 true 时跳过 call_accept_setting 校验（群聊通话不受该设置影响）。
func (s *rtcServer) filterNotAllowedInvitees(ctx context.Context, inviterID string, inviteeIDs []string, skipCallAcceptSetting bool) ([]string, map[string]struct{}, map[string]struct{}, map[string]struct{}, map[string]struct{}, error) {
	notAllowUserIDs := make([]string, 0)
	notAllowSet := make(map[string]struct{})
	blacklistedSet := make(map[string]struct{})
	globalBlockedSet := make(map[string]struct{})
	missingUserSet := make(map[string]struct{})

	globalBlockedUsers, err := s.globalBlackDB.FindBlocked(ctx, inviteeIDs)
	if err != nil {
		log.ZError(ctx, "filterNotAllowedInvitees: FindBlocked failed", err, "inviteeIDs", inviteeIDs)
		return nil, nil, nil, nil, nil, err
	}
	for _, b := range globalBlockedUsers {
		globalBlockedSet[b.UserID] = struct{}{}
	}

	for _, inviteeID := range inviteeIDs {
		if _, restricted := globalBlockedSet[inviteeID]; restricted {
			notAllowUserIDs = append(notAllowUserIDs, inviteeID)
			notAllowSet[inviteeID] = struct{}{}
			continue
		}
		dbUser, err := s.userDB.Take(ctx, inviteeID)
		if err != nil {
			if errs.ErrRecordNotFound.Is(err) {
				notAllowUserIDs = append(notAllowUserIDs, inviteeID)
				notAllowSet[inviteeID] = struct{}{}
				missingUserSet[inviteeID] = struct{}{}
				continue
			}
			log.ZError(ctx, "filterNotAllowedInvitees: Take user failed", err, "inviteeID", inviteeID)
			return nil, nil, nil, nil, nil, err
		}
		userInfo := &sdkws.UserInfo{
			UserID:            dbUser.UserID,
			CallAcceptSetting: dbUser.CallAcceptSetting,
		}
		blocked, err := s.relationClient.IsBlack(ctx, inviterID, inviteeID)
		if err != nil {
			log.ZError(ctx, "filterNotAllowedInvitees: IsBlack failed", err, "inviterID", inviterID, "inviteeID", inviteeID)
			return nil, nil, nil, nil, nil, err
		}
		if blocked {
			notAllowUserIDs = append(notAllowUserIDs, inviteeID)
			notAllowSet[inviteeID] = struct{}{}
			blacklistedSet[inviteeID] = struct{}{}
			continue
		}
		if !skipCallAcceptSetting {
			allowed, err := s.isCallAllowedWithUserInfo(ctx, inviterID, userInfo)
			if err != nil {
				log.ZError(ctx, "filterNotAllowedInvitees: isCallAllowed failed", err, "inviteeID", inviteeID)
				return nil, nil, nil, nil, nil, err
			}
			if !allowed {
				notAllowUserIDs = append(notAllowUserIDs, inviteeID)
				notAllowSet[inviteeID] = struct{}{}
			}
		}
	}
	return notAllowUserIDs, notAllowSet, blacklistedSet, globalBlockedSet, missingUserSet, nil
}

// verifyInviterGlobalStatus 校验主叫方全局账号状态，冻结/全局黑名单用户不可发起通话。
func (s *rtcServer) verifyInviterGlobalStatus(ctx context.Context, inviterID string) error {
	if datautil.Contain(inviterID, s.config.Share.IMAdminUserID...) {
		return nil
	}
	st, err := s.globalBlackDB.GetStatus(ctx, inviterID)
	if err != nil {
		log.ZWarn(ctx, "verifyInviterGlobalStatus: GetStatus failed", err, "inviterID", inviterID)
		return nil
	}
	if st == model.UserStatusFrozen || st == model.UserStatusBlacklist {
		return servererrs.ErrUserBlocked.WithDetail("sender is restricted, status=" + strconv.Itoa(int(st)))
	}
	return nil
}

func callInviteAllNotAllowedErr(blacklistedSet, globalBlockedSet, missingUserSet map[string]struct{}, inviteeIDs []string) error {
	if len(missingUserSet) == len(inviteeIDs) {
		return servererrs.ErrUserIDNotFound.WrapMsg("invitee user not found", "inviteeUserIDList", inviteeIDs)
	}
	if len(blacklistedSet) > 0 {
		return servererrs.ErrBlockedByPeer.Wrap()
	}
	if len(globalBlockedSet) > 0 {
		return servererrs.ErrMsgReceiveNotAllowed.WrapMsg("invitee is restricted")
	}
	return errs.ErrNoPermission.WrapMsg("all invitees do not accept calls from you", "inviteeUserIDList", inviteeIDs)
}

// isCallAllowedWithUserInfo 判断 inviterID 是否被允许向 inviteeID 发起单聊音视频通话。
// 群聊通话不受 call_accept_setting 影响，不调用本方法。
// 好友黑名单、全局黑名单与被叫用户存在性校验在 filterNotAllowedInvitees 中优先执行。
// 规则：
//   - CallAcceptSettingPublic(0)  → 所有人均可
//   - CallAcceptSettingFriends(1) → 仅当 inviterID 在 inviteeID 好友列表中
//   - CallAcceptSettingNobody(2)  → 任何人均不可
func (s *rtcServer) isCallAllowedWithUserInfo(ctx context.Context, inviterID string, userInfo *sdkws.UserInfo) (bool, error) {
	if userInfo == nil {
		return false, nil
	}
	switch userInfo.CallAcceptSetting {
	case model.CallAcceptSettingNobody:
		return false, nil
	case model.CallAcceptSettingFriends:
		isFriend, err := s.relationClient.IsFriend(ctx, userInfo.UserID, inviterID)
		if err != nil {
			return false, err
		}
		return isFriend, nil
	default: // CallAcceptSettingPublic
		return true, nil
	}
}

func (s *rtcServer) handleAccept(ctx context.Context, req *rtc.SignalAcceptReq, signalReq *rtc.SignalReq) (*rtc.SignalAcceptResp, error) {
	if req.Invitation == nil {
		return nil, errs.ErrArgs.WrapMsg("invitation is nil")
	}

	log.ZDebug(ctx, "handleAccept: start", "req", req)

	// 从 DB 获取权威邀请数据，验证邀请存在且 userID 在被邀请人列表中
	dbInv, err := s.db.GetInvitationByRoomID(ctx, req.Invitation.RoomID)
	if err != nil {
		log.ZWarn(ctx, "handleAccept: GetInvitationByRoomID failed", err, "req", req)
		return nil, errs.WrapMsg(err, "invitation not found or expired", "roomID", req.Invitation.RoomID)
	}
	if !datautil.Contain(req.UserID, dbInv.InviteeUserIDList...) {
		log.ZWarn(ctx, "handleAccept: user not in invitee list", errs.ErrNoPermission.WrapMsg("user not in invitee list"), "req", req)
		return nil, errs.ErrNoPermission.WrapMsg("user not in invitee list", "userID", req.UserID)
	}

	token, err := s.genToken(dbInv.RoomID, req.UserID)
	if err != nil {
		log.ZWarn(ctx, "handleAccept: genToken failed", err, "req", req)
		return nil, err
	}

	sessionType := int32(constant.SingleChatType)
	if dbInv.GroupID != "" {
		sessionType = int32(constant.ReadGroupChatType)
	}

	content, err := marshalSignalReq(signalReq)
	if err != nil {
		log.ZWarn(ctx, "handleAccept: marshalSignalReq failed", err, "req", req)
		return nil, err
	}

	if err := s.sendSignalingNotification(ctx, req.UserID, dbInv.InviterUserID, sessionType, dbInv.GroupID, req.OfflinePushInfo, content); err != nil {
		log.ZWarn(ctx, "handleAccept: sendSignalingNotification accept to inviter failed", err, "inviterID", dbInv.InviterUserID)
	}

	if err := s.db.SetAcceptTime(ctx, dbInv.RoomID, time.Now().UnixMilli()); err != nil {
		log.ZWarn(ctx, "handleAccept: SetAcceptTime failed", err, "roomID", dbInv.RoomID)
	}

	// Transition inviter and acceptor to "in-call".
	s.setCallStatusInCall(ctx, dbInv, req.UserID)

	// For group calls, notify all members that the participant count has increased.
	if dbInv.GroupID != "" {
		lp, listErr := s.roomClient.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: dbInv.RoomID})
		var participantUserIDs []string
		if listErr == nil {
			// The accepting user has not yet joined LiveKit, so include current participants + acceptor.
			for _, p := range lp.Participants {
				participantUserIDs = append(participantUserIDs, p.GetIdentity())
			}
		} else {
			log.ZWarn(ctx, "handleAccept: ListParticipants failed (falling back to inviter only)", listErr, "roomID", dbInv.RoomID)
			// Fallback: at minimum the inviter is in the call.
			participantUserIDs = append(participantUserIDs, dbInv.InviterUserID)
		}
		// The accepting user hasn't joined LiveKit yet but is about to; include them now.
		participantUserIDs = append(participantUserIDs, req.UserID)
		s.goSendGroupCallParticipantCountUpdated(ctx, dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, participantUserIDs)
		log.ZDebug(ctx, "handleAccept: goSendGroupCallParticipantCountUpdated", "groupID", dbInv.GroupID, "roomID", dbInv.RoomID, "mediaType", dbInv.MediaType, "participantUserIDs", participantUserIDs)
	}

	// 接受邀请后不删除 invitation：通话仍在进行，双方应被标记为忙线（BusyLineUserIDList）。
	// invitation 的清理由以下路径负责：
	//   - 主动挂断：handleHungUp → TryDeleteInvitation
	//   - 主叫取消：handleCancel → TryDeleteInvitation
	//   - 被叫拒绝：handleReject → TryDeleteInvitation
	//   - 超时未接：handleTimeout → TryDeleteInvitation

	log.ZDebug(ctx, "handleAccept: end", "req", req)

	return &rtc.SignalAcceptResp{
		Token:   token,
		RoomID:  dbInv.RoomID,
		LiveURL: s.config.RpcConfig.LiveKit.ExternalAddress,
	}, nil
}

// handleJoin lets a group member join an ongoing group call without a prior invite.
func (s *rtcServer) handleJoin(ctx context.Context, req *rtc.SignalJoinReq, signalReq *rtc.SignalReq) (*rtc.SignalJoinResp, error) {
	if req.Invitation == nil || req.Invitation.RoomID == "" {
		return nil, errs.ErrArgs.WrapMsg("invitation is nil or roomID is empty")
	}
	if req.UserID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID is empty")
	}

	log.ZDebug(ctx, "handleJoin: start", "req", req)

	dbInv, err := s.db.GetInvitationByRoomID(ctx, req.Invitation.RoomID)
	if err != nil {
		log.ZWarn(ctx, "handleJoin: GetInvitationByRoomID failed", err, "req", req)
		return nil, errs.WrapMsg(err, "invitation not found or expired", "roomID", req.Invitation.RoomID)
	}
	if dbInv.GroupID == "" {
		log.ZWarn(ctx, "handleJoin: groupID is empty", errs.ErrArgs.WrapMsg("join is only supported for group calls"), "req", req)
		return nil, errs.ErrArgs.WrapMsg("join is only supported for group calls", "roomID", dbInv.RoomID)
	}
	if _, err := s.groupClient.GetGroupMemberCache(ctx, dbInv.GroupID, req.UserID); err != nil {
		log.ZWarn(ctx, "handleJoin: GetGroupMemberCache failed", err, "req", req)
		return nil, errs.ErrNoPermission.WrapMsg("user is not a group member", "userID", req.UserID, "groupID", dbInv.GroupID)
	}
	// Idempotent re-join: repeated join clicks while already in this room should
	// return a fresh token instead of failing or tearing down the call.
	if callSt, busy := s.getCalleeActiveCallStatus(ctx, req.UserID); busy && callSt.RoomID == dbInv.RoomID {
		token, err := s.genToken(dbInv.RoomID, req.UserID)
		if err != nil {
			log.ZWarn(ctx, "handleJoin: genToken failed (re-join)", err, "req", req)
			return nil, err
		}
		log.ZDebug(ctx, "handleJoin: idempotent re-join", "roomID", dbInv.RoomID, "userID", req.UserID)
		participants, inCall := s.joinCallParticipants(ctx, dbInv.RoomID, req.UserID)
		return &rtc.SignalJoinResp{
			Token:       token,
			RoomID:      dbInv.RoomID,
			LiveURL:     s.config.RpcConfig.LiveKit.ExternalAddress,
			Participant: participants,
			InCall:      inCall,
		}, nil
	}
	if !s.isInvitationPending(ctx, dbInv) {
		log.ZWarn(ctx, "handleJoin: invitation is not pending", errs.ErrRecordNotFound.WrapMsg("group call not active"), "req", req)
		return nil, errs.ErrRecordNotFound.WrapMsg("group call not active", "roomID", dbInv.RoomID)
	}
	if s.isCalleeBusyOnAnotherCall(ctx, req.UserID, dbInv.RoomID) {
		log.ZWarn(ctx, "handleJoin: user is already on another call", servererrs.ErrAllUserBusy.WrapMsg("user is already on another call"), "req", req)
		return nil, servererrs.ErrAllUserBusy.WrapMsg("user is already on another call", "userID", req.UserID)
	}
	if err := s.ensureCallParticipant(ctx, dbInv, req.UserID); err != nil {
		log.ZWarn(ctx, "handleJoin: ensureCallParticipant failed", err, "req", req)
		return nil, errs.WrapMsg(err, "ensureCallParticipant failed", "roomID", dbInv.RoomID, "userID", req.UserID)
	}

	token, err := s.genToken(dbInv.RoomID, req.UserID)
	if err != nil {
		log.ZWarn(ctx, "handleJoin: genToken failed", err, "req", req)
		return nil, err
	}

	if dbInv.AcceptTime <= 0 {
		if err := s.db.SetAcceptTime(ctx, dbInv.RoomID, time.Now().UnixMilli()); err != nil {
			log.ZWarn(ctx, "handleJoin: SetAcceptTime failed", err, "roomID", dbInv.RoomID)
		}
	}

	s.setCallStatusInCall(ctx, dbInv, req.UserID)

	content, err := marshalSignalReq(signalReq)
	if err != nil {
		log.ZWarn(ctx, "handleJoin: marshalSignalReq failed", err, "req", req)
		return nil, err
	}
	notifyPeers := make(map[string]struct{}, len(dbInv.InviteeUserIDList)+1)
	notifyPeers[dbInv.InviterUserID] = struct{}{}
	for _, uid := range dbInv.InviteeUserIDList {
		if uid != req.UserID {
			notifyPeers[uid] = struct{}{}
		}
	}
	for peerID := range notifyPeers {
		if err := s.sendSignalingNotification(ctx, req.UserID, peerID, int32(constant.ReadGroupChatType), dbInv.GroupID, nil, content); err != nil {
			log.ZWarn(ctx, "handleJoin: sendSignalingNotification failed", err, "peerID", peerID)
		}
	}

	participants, inCall := s.joinCallParticipants(ctx, dbInv.RoomID, req.UserID)
	s.goSendGroupCallParticipantCountUpdated(ctx, dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, participantUserIDsFromMeta(participants))

	log.ZDebug(ctx, "handleJoin: end", "roomID", dbInv.RoomID, "userID", req.UserID, "participantCount", len(participants), "inCall", inCall)

	return &rtc.SignalJoinResp{
		Token:       token,
		RoomID:      dbInv.RoomID,
		LiveURL:     s.config.RpcConfig.LiveKit.ExternalAddress,
		Participant: participants,
		InCall:      inCall,
	}, nil
}

// handleReject processes a call rejection.
func (s *rtcServer) handleReject(ctx context.Context, req *rtc.SignalRejectReq, signalReq *rtc.SignalReq) (*rtc.SignalRejectResp, error) {

	log.ZDebug(ctx, "handleReject: start", "req", req)

	if req.Invitation == nil {
		log.ZWarn(ctx, "handleReject", errs.ErrArgs.WrapMsg("invitation is nil"), "req", req)
		return nil, errs.ErrArgs.WrapMsg("invitation is nil")
	}

	dbInv, err := s.db.GetInvitationByRoomID(ctx, req.Invitation.RoomID)
	if err != nil {
		log.ZWarn(ctx, "handleReject", err, "get invitation by roomID failed", "req", req)
		return nil, errs.WrapMsg(err, "invitation not found or expired", "roomID", req.Invitation.RoomID)
	}

	if !datautil.Contain(req.UserID, dbInv.InviteeUserIDList...) {
		log.ZWarn(ctx, "handleReject", errs.ErrNoPermission.WrapMsg("user not in invitee list"), "req", req, "dbInv", dbInv)
		return nil, errs.ErrNoPermission.WrapMsg("user not in invitee list", "userID", req.UserID)
	}

	sessionType := int32(constant.SingleChatType)
	if dbInv.GroupID != "" {
		sessionType = int32(constant.ReadGroupChatType)
	}
	content, err := marshalSignalReq(signalReq)
	if err != nil {
		log.ZWarn(ctx, "handleReject", err, "marshal signal req failed", "req", req)
		return nil, err
	}
	invInfo := modelToInvitationInfo(dbInv)
	clientPush := req.OfflinePushInfo
	if clientPush == nil {
		clientPush = offlinePushInfoFromInvitationModel(dbInv)
	}
	rejectOfflinePush := s.resolveSignalingOfflinePushInfo(ctx, invInfo, clientPush, signalCallActionReject, req.UserID, dbInv.InviterUserID)
	if err := s.sendSignalingNotification(ctx, req.UserID, dbInv.InviterUserID, sessionType, dbInv.GroupID, rejectOfflinePush, content); err != nil {
		log.ZWarn(ctx, "handleReject: sendSignalingNotification reject to inviter failed", err, "inviterID", dbInv.InviterUserID, "req", req, "dbInv", dbInv)
	}

	if dbInv.GroupID != "" {
		// Use PullInvitee (not RemoveInvitee) so the invitation record is NOT
		// auto-deleted when the invitee list becomes empty. TryDeleteInvitation
		// below must be able to claim the record to send the group-call-ended
		// notification exactly once.
		if err := s.db.PullInvitee(ctx, dbInv.RoomID, req.UserID); err != nil {
			log.ZWarn(ctx, "handleReject: PullInvitee failed", err, "roomID", dbInv.RoomID, "userID", req.UserID, "req", req, "dbInv", dbInv)
		}

		go s.sendGroupCallParticipantDeclinedNotification(context.WithoutCancel(ctx), dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, signalCallActionReject, req.UserID)

		// Check whether any participant other than the inviter has actually
		// joined the LiveKit room.  Rejecters never enter LiveKit, so a
		// participant count > 0 (excluding the inviter who waits in the room)
		// means at least one invitee accepted and the call is ongoing.
		// If nobody joined yet and every reachable invitee has rejected, tear the call
		// down so non-invited members' banners are dismissed promptly.
		lp, listErr := s.roomClient.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: dbInv.RoomID})
		joinedCount := 0
		if listErr != nil {
			log.ZWarn(ctx, "handleReject: ListParticipants failed", listErr, "roomID", dbInv.RoomID, "req", req, "dbInv", dbInv)
		} else {
			for _, p := range lp.Participants {
				if p.GetIdentity() != dbInv.InviterUserID {
					joinedCount++
				}
			}
		}

		if joinedCount > 0 {
			// At least one invitee has already joined; the call continues.
			// Remove only the rejecter's call-status entry; others stay connected.
			log.ZInfo(ctx, "handleReject: group call continues", "roomID", dbInv.RoomID, "joinedCount", joinedCount, "req", req, "dbInv", dbInv)
			s.deleteCallStatusForUser(ctx, req.UserID, dbInv.RoomID)
			s.goSendGroupCallParticipantCountUpdated(ctx, dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, liveKitParticipantUserIDs(lp.Participants))
			log.ZDebug(ctx, "handleReject: goSendGroupCallParticipantCountUpdated", "groupID", dbInv.GroupID, "roomID", dbInv.RoomID, "mediaType", dbInv.MediaType, "participantUserIDs", liveKitParticipantUserIDs(lp.Participants))
			return &rtc.SignalRejectResp{}, nil
		}

		remainingInv, remainErr := s.db.GetInvitationByRoomID(ctx, dbInv.RoomID)
		if remainErr != nil {
			if errs.ErrRecordNotFound.Is(remainErr) {
				log.ZInfo(ctx, "handleReject: invitation already ended", "roomID", dbInv.RoomID, "req", req)
				return &rtc.SignalRejectResp{}, nil
			}
			log.ZWarn(ctx, "handleReject: GetInvitationByRoomID after PullInvitee failed", remainErr, "roomID", dbInv.RoomID, "req", req)
			return &rtc.SignalRejectResp{}, nil
		}
		if pending := pendingReachableInvitees(remainingInv.InviteeUserIDList, remainingInv.BusyLineUserIDList); len(pending) > 0 {
			// Some invitees are still being rung; remove only the rejecter.
			log.ZInfo(ctx, "handleReject: waiting for other invitees to respond", "roomID", dbInv.RoomID, "pendingInvitees", pending, "req", req)
			s.deleteCallStatusForUser(ctx, req.UserID, dbInv.RoomID)
			s.goSendGroupCallParticipantCountUpdated(ctx, dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, liveKitParticipantUserIDs(lp.Participants))
			log.ZDebug(ctx, "handleReject: goSendGroupCallParticipantCountUpdated", "groupID", dbInv.GroupID, "roomID", dbInv.RoomID, "mediaType", dbInv.MediaType, "participantUserIDs", liveKitParticipantUserIDs(lp.Participants))
			return &rtc.SignalRejectResp{}, nil
		}

		// No one else is in the room and every reachable invitee has rejected.
		// Terminate the call so the "in progress" banner is dismissed for
		// non-invited members.
		if _, err := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: dbInv.RoomID}); err != nil {
			log.ZWarn(ctx, "handleReject: DeleteRoom failed", err, "roomID", dbInv.RoomID, "req", req, "dbInv", dbInv)
		}

		claimed, claimErr := s.db.TryDeleteInvitation(ctx, dbInv.RoomID)
		if claimErr != nil {
			log.ZWarn(ctx, "handleReject: TryDeleteInvitation failed", claimErr, "roomID", dbInv.RoomID, "req", req, "dbInv", dbInv)
		}

		log.ZInfo(ctx, "handleReject", "req", req, "dbInv", dbInv)

		// All invitees rejected — delete call-status for all participants.
		s.deleteCallStatusForInvitation(ctx, dbInv)

		log.ZDebug(ctx, "handleReject: sendGroupCallEndedNotification", "dbInv", dbInv, "claimed", claimed)

		if claimed {
			s.goSendGroupCallParticipantCountUpdated(ctx, dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, nil)
			log.ZDebug(ctx, "handleReject: goSendGroupCallParticipantCountUpdated", "groupID", dbInv.GroupID, "roomID", dbInv.RoomID, "mediaType", dbInv.MediaType, "participantUserIDs", nil)

			s.broadcastGroupCallStatusToNonInvited(ctx, dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, dbInv.InviterUserID, dbInv.InviteeUserIDList, GroupCallStatusEnded)

			s.sendGroupCallEndedNotification(ctx, dbInv.GroupID, dbInv.InviterUserID, dbInv.MediaType, groupCallDurationFromInvitation(dbInv), signalCallActionReject)
		}

	} else {
		if _, err := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: dbInv.RoomID}); err != nil {
			log.ZWarn(ctx, "handleReject: DeleteRoom failed", err, "roomID", dbInv.RoomID)
		}
		if err := s.db.DeleteInvitation(ctx, dbInv.RoomID); err != nil {
			log.ZWarn(ctx, "handleReject: DeleteInvitation failed", err, "roomID", dbInv.RoomID)
		}

		// Single chat rejected — remove both parties' status.
		s.deleteCallStatusForInvitation(ctx, dbInv)

		s.sendCallRecordChatMsg(ctx, dbInv, callStatusRejected, 0)

	}

	log.ZDebug(ctx, "handleReject: end", "req", req)

	return &rtc.SignalRejectResp{}, nil
}

// handleCancel processes a call cancellation.
func (s *rtcServer) handleCancel(ctx context.Context, req *rtc.SignalCancelReq, signalReq *rtc.SignalReq) (*rtc.SignalCancelResp, error) {
	log.ZDebug(ctx, "handleCancel: start", "req", req)

	if req.Invitation == nil {
		log.ZWarn(ctx, "handleCancel", errs.ErrArgs.WrapMsg("invitation is nil"), "req", req)
		return nil, errs.ErrArgs.WrapMsg("invitation is nil")
	}

	dbInv, err := s.db.GetInvitationByRoomID(ctx, req.Invitation.RoomID)
	if err != nil {
		log.ZWarn(ctx, "handleCancel", err, "get invitation by roomID failed", "req", req)
		return nil, errs.WrapMsg(err, "invitation not found or expired", "roomID", req.Invitation.RoomID)
	}
	if req.UserID != dbInv.InviterUserID {
		log.ZWarn(ctx, "handleCancel", errs.ErrNoPermission.WrapMsg("only the inviter can cancel"), "req", req, "dbInv", dbInv)
		return nil, errs.ErrNoPermission.WrapMsg("only the inviter can cancel", "userID", req.UserID, "inviterUserID", dbInv.InviterUserID)
	}

	if dbInv.GroupID != "" {
		// Only skip cancel tear-down when the inviter is already in the LiveKit
		// room and at least one invitee has joined — that means the group call is
		// truly ongoing and the inviter should use hangUp to leave.
		// If invitees joined but the inviter never entered (or left before cancel),
		// proceed with cancel so accepted invitees are notified and cleaned up.
		lp, listErr := s.roomClient.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: dbInv.RoomID})
		joinedCount := 0
		inviterInRoom := false
		if listErr != nil {
			log.ZWarn(ctx, "handleCancel: ListParticipants failed", listErr, "roomID", dbInv.RoomID)
		} else {
			for _, p := range lp.Participants {
				id := p.GetIdentity()
				if id == dbInv.InviterUserID {
					inviterInRoom = true
				}
				if id != dbInv.InviterUserID {
					joinedCount++
				}
			}
		}
		if joinedCount > 0 && inviterInRoom {
			log.ZInfo(ctx, "handleCancel: group call already has participants, skip cancel tear-down", "roomID", dbInv.RoomID, "joinedCount", joinedCount, "inviterInRoom", inviterInRoom)
			return &rtc.SignalCancelResp{}, nil
		}
	}

	sessionType := int32(constant.SingleChatType)
	if dbInv.GroupID != "" {
		sessionType = int32(constant.ReadGroupChatType)
	}

	// 竞态处理（消除 TOCTOU）：主叫在振铃阶段点击挂断（发送 Cancel），被叫几乎
	// 同时接听（Accept）。handleCancel 与 handleAccept 是两个并发 RPC，若仅凭函数
	// 开头读到的 dbInv.AcceptTime 判断，会漏判“Cancel 读到旧快照 accept_time=0、
	// 而 Accept 稍后才落库”的情况，从而给已进入通话页的被叫误发 Cancel，导致其
	// 界面无法关闭、通话持续计时。
	//
	// 因此对 1v1 通话改用“仅当未接听(accept_time==0)时才删除邀请”的原子操作定夺：
	//   - 删除成功 → 确属未接听 → 发 Cancel、记为已取消；
	//   - 删除失败 → 已被并发 Accept 置为已接听 → 重新读取、改发 HungUp、记为已接听，
	//     让被叫走 OnHangUp 关闭通话页。
	// 与 handleAccept 里带相同条件的 SetAcceptTime 由 MongoDB 单文档原子性保证互斥。
	answered := false
	if dbInv.GroupID == "" {
		deleted, delErr := s.db.DeleteInvitationIfNotAccepted(ctx, dbInv.RoomID)
		switch {
		case delErr != nil:
			// DB 异常时回退到快照判断，避免完全不处理。
			log.ZWarn(ctx, "handleCancel: DeleteInvitationIfNotAccepted failed, fallback to snapshot", delErr, "roomID", dbInv.RoomID)
			answered = dbInv.AcceptTime > 0
		case deleted:
			answered = false
		default:
			// 未删除：邀请已被并发 Accept 标记为已接听（或已被其它终结路径删除）。
			if latest, ferr := s.db.GetInvitationByRoomID(ctx, dbInv.RoomID); ferr == nil && latest != nil && latest.AcceptTime > 0 {
				answered = true
				dbInv = latest
			}
		}
	}
	talkSecs, _ := singleChatCallDuration(dbInv)

	invInfo := modelToInvitationInfo(dbInv)
	content, err := marshalSignalReq(signalReq)
	if err != nil {
		log.ZWarn(ctx, "handleCancel", err, "marshal signal req failed", "req", req, "dbInv", dbInv, "signalReq", signalReq)
		return nil, err
	}
	if answered {
		hungUpSignalReq := &rtc.SignalReq{
			Payload: &rtc.SignalReq_HungUp{
				HungUp: &rtc.SignalHungUpReq{
					Invitation:   invInfo,
					UserID:       dbInv.InviterUserID,
					CallDuration: talkSecs,
				},
			},
		}
		if hungUpContent, marshalErr := marshalSignalReq(hungUpSignalReq); marshalErr != nil {
			log.ZWarn(ctx, "handleCancel: marshal hungUp signal req failed", marshalErr, "roomID", dbInv.RoomID)
		} else {
			content = hungUpContent
			log.ZInfo(ctx, "handleCancel: call already accepted, forwarding hangUp to invitees", "roomID", dbInv.RoomID, "talkSecs", talkSecs)
		}
		log.ZDebug(ctx, "handleCancel: answered", "content", content, "req", req)
	} else {
		log.ZDebug(ctx, "handleCancel: not answered", "content", content, "req", req)
	}

	for _, inviteeID := range dbInv.InviteeUserIDList {
		cancelOfflinePush := s.resolveSignalingOfflinePushInfo(ctx, invInfo, offlinePushInfoFromInvitationModel(dbInv), signalCallActionCancel, req.UserID, inviteeID)
		if err := s.sendSignalingNotification(ctx, req.UserID, inviteeID, sessionType, dbInv.GroupID, cancelOfflinePush, content); err != nil {
			log.ZWarn(ctx, "handleCancel: sendSignalingNotification cancel to invitee failed", err, "inviteeID", inviteeID)
		}
	}

	if _, err := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: dbInv.RoomID}); err != nil {
		log.ZWarn(ctx, "handleCancel: DeleteRoom failed", err, "roomID", dbInv.RoomID)
	}

	log.ZDebug(ctx, "handleCancel: DeleteRoom", "roomID", dbInv.RoomID)

	var groupCallEndedClaimed bool
	if dbInv.GroupID != "" {
		var claimErr error
		groupCallEndedClaimed, claimErr = s.db.TryDeleteInvitation(ctx, dbInv.RoomID)
		if claimErr != nil {
			log.ZWarn(ctx, "handleCancel: TryDeleteInvitation failed", claimErr, "roomID", dbInv.RoomID)
		}
	} else if err := s.db.DeleteInvitation(ctx, dbInv.RoomID); err != nil {
		log.ZWarn(ctx, "handleCancel: DeleteInvitation failed", err, "roomID", dbInv.RoomID)
	}

	// Cancel always terminates the call — remove status for all participants.
	s.deleteCallStatusForInvitation(ctx, dbInv)

	// For group calls, notify non-invited members that the call was cancelled.
	if dbInv.GroupID != "" {
		go s.sendGroupCallParticipantDeclinedNotification(context.WithoutCancel(ctx), dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, signalCallActionCancel, req.UserID)
		s.goSendGroupCallParticipantCountUpdated(ctx, dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, nil)
		log.ZDebug(ctx, "handleCancel: goSendGroupCallParticipantCountUpdated", "groupID", dbInv.GroupID, "roomID", dbInv.RoomID, "mediaType", dbInv.MediaType, "participantUserIDs", nil)

		if groupCallEndedClaimed {

			s.broadcastGroupCallStatusToNonInvited(ctx, dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, dbInv.InviterUserID, dbInv.InviteeUserIDList, GroupCallStatusEnded)

			s.sendGroupCallEndedNotification(ctx, dbInv.GroupID, dbInv.InviterUserID, dbInv.MediaType, groupCallDurationFromInvitation(dbInv), signalCallActionCancel)

			log.ZDebug(ctx, "handleCancel: sendGroupCallEndedNotification", "dbInv", dbInv, "groupCallEndedClaimed", groupCallEndedClaimed)

		}
	} else if answered {
		// 已接听后被取消：按已接听通话写入记录（时长为 accept→now）。
		s.sendCallRecordChatMsg(ctx, dbInv, callStatusAnswered, talkSecs)
	} else {
		s.sendCallRecordChatMsg(ctx, dbInv, callStatusCancelled, 0)
	}

	log.ZDebug(ctx, "handleCancel: end", "req", req)

	return &rtc.SignalCancelResp{}, nil
}

// handleHungUp processes a call hang-up.
//
// For 1:1 calls the hang-up always terminates the call immediately.
// For group calls the call only ends when the LiveKit room has no remaining
// participants after this user disconnects; if others are still present the
// server only removes this user from the DB participant list and returns,
// leaving the room alive.
func (s *rtcServer) handleHungUp(ctx context.Context, req *rtc.SignalHungUpReq, signalReq *rtc.SignalReq) (*rtc.SignalHungUpResp, error) {
	if req.Invitation == nil {
		log.ZWarn(ctx, "handleHungUp", errs.ErrArgs.WrapMsg("invitation is nil"), "req", req)
		return nil, errs.ErrArgs.WrapMsg("invitation is nil")
	}

	dbInv, err := s.db.GetInvitationByRoomID(ctx, req.Invitation.RoomID)
	if err != nil {
		log.ZWarn(ctx, "handleHungUp", err, "get invitation by roomID failed", "req", req)
		return nil, errs.WrapMsg(err, "invitation not found or expired", "roomID", req.Invitation.RoomID)
	}
	if req.UserID != dbInv.InviterUserID && !datautil.Contain(req.UserID, dbInv.InviteeUserIDList...) {
		log.ZWarn(ctx, "handleHungUp", errs.ErrNoPermission.WrapMsg("user is not a participant of this call"), "req", req, "dbInv", dbInv)
		return nil, errs.ErrNoPermission.WrapMsg("user is not a participant of this call", "userID", req.UserID)
	}

	sessionType := int32(constant.SingleChatType)
	if dbInv.GroupID != "" {
		sessionType = int32(constant.ReadGroupChatType)
	}

	// For 1v1 calls, normalize the forwarded duration to accept→hangup using the
	// server AcceptTime so peers never receive invite→hangup totals from apps.
	if dbInv.GroupID == "" {
		talkSecs, callStatus := singleChatCallDuration(dbInv)
		if callStatus == callStatusAnswered {
			req.CallDuration = talkSecs
			if hungUp := signalReq.GetHungUp(); hungUp != nil {
				hungUp.CallDuration = talkSecs
			}
		} else {
			req.CallDuration = 0
			if hungUp := signalReq.GetHungUp(); hungUp != nil {
				hungUp.CallDuration = 0
			}
		}
	}

	content, err := marshalSignalReq(signalReq)
	if err != nil {
		log.ZWarn(ctx, "handleHungUp", err, "marshal signal req failed", "req", req, "dbInv", dbInv, "signalReq", signalReq)
		return nil, err
	}
	// Unanswered 1v1 hang-up: wake offline callees with a missed-call push so they sync state.
	invInfo := modelToInvitationInfo(dbInv)
	peerIDs := hungUpPeerIDsFromDB(dbInv, req.UserID)
	is1v1Unanswered := dbInv.GroupID == "" && dbInv.AcceptTime <= 0

	log.ZDebug(ctx, "handleHungUp: missed-call offline push decision",
		"roomID", dbInv.RoomID,
		"hangUpUserID", req.UserID,
		"inviterUserID", dbInv.InviterUserID,
		"inviteeUserIDList", dbInv.InviteeUserIDList,
		"peerIDs", peerIDs,
		"groupID", dbInv.GroupID,
		"acceptTime", dbInv.AcceptTime,
		"is1v1Unanswered", is1v1Unanswered,
		"mediaType", dbInv.MediaType,
	)
	var hungUpOfflinePush *sdkws.OfflinePushInfo
	if is1v1Unanswered && len(peerIDs) > 0 {
		hungUpOfflinePush = s.resolveSignalingOfflinePushInfo(ctx, invInfo, offlinePushInfoFromInvitationModel(dbInv), signalCallActionTimeout, req.UserID, peerIDs[0])
	}
	missedCallPushTitle := ""
	missedCallPushDesc := ""
	if hungUpOfflinePush != nil {
		missedCallPushTitle = hungUpOfflinePush.Title
		missedCallPushDesc = hungUpOfflinePush.Desc
	}

	log.ZDebug(ctx, "handleHungUp: sending hungUp signaling to peers",
		"roomID", dbInv.RoomID,
		"peerCount", len(peerIDs),
		"peerIDs", peerIDs,
		"missedCallPushEnabled", is1v1Unanswered,
		"missedCallPushTitle", missedCallPushTitle,
		"missedCallPushDesc", missedCallPushDesc,
		"signalingPayloadType", msgprocessor.SignalingPayloadTypeName(content),
	)
	for _, peerID := range peerIDs {
		peerOfflinePush := (*sdkws.OfflinePushInfo)(nil)
		if is1v1Unanswered {
			peerOfflinePush = s.resolveSignalingOfflinePushInfo(ctx, invInfo, offlinePushInfoFromInvitationModel(dbInv), signalCallActionTimeout, req.UserID, peerID)
		}
		if err := s.sendSignalingNotification(ctx, req.UserID, peerID, sessionType, dbInv.GroupID, peerOfflinePush, content); err != nil {
			log.ZWarn(ctx, "handleHungUp: sendSignalingNotification hungUp to peer failed", err, "peerID", peerID)
		}
	}

	if dbInv.GroupID != "" {
		// Group call: the client disconnects from LiveKit before sending HungUp,
		// so ListParticipants already reflects the post-hangup state.
		// Count participants excluding the user who just hung up (covers the
		// rare race where the client hasn't fully left the LiveKit room yet).
		lp, listErr := s.roomClient.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: dbInv.RoomID})
		remaining := 0
		var participantUserIDs []string
		if listErr != nil {
			// LiveKit query failed — fall back to the DB participant list rather
			// than assuming the call ended.  Treating a transient error as
			// remaining==0 would tear down an ongoing group call and fire a
			// spurious end notification.
			log.ZWarn(ctx, "handleHungUp: ListParticipants failed, falling back to DB participant count", listErr, "roomID", dbInv.RoomID)
			for _, uid := range dbInv.InviteeUserIDList {
				if uid != req.UserID {
					participantUserIDs = append(participantUserIDs, uid)
					remaining++
				}
			}
			if dbInv.InviterUserID != req.UserID {
				participantUserIDs = append(participantUserIDs, dbInv.InviterUserID)
				remaining++
			}
		} else {
			for _, p := range lp.Participants {
				if p.GetIdentity() != req.UserID {
					participantUserIDs = append(participantUserIDs, p.GetIdentity())
					remaining++
				}
			}
		}

		if remaining > 0 {
			// Other participants are still in the call; just remove this user
			// from the DB invitee list so they are no longer tracked as busy.
			// Use PullInvitee (not RemoveInvitee) so the invitation record is
			// NOT deleted when the list becomes empty — the inviter may still
			// be in the call, and TryDeleteInvitation must be able to claim
			// the record later to send sendGroupCallEndedNotification.
			if datautil.Contain(req.UserID, dbInv.InviteeUserIDList...) {
				if err := s.db.PullInvitee(ctx, dbInv.RoomID, req.UserID); err != nil {
					log.ZWarn(ctx, "handleHungUp: PullInvitee failed", err, "roomID", dbInv.RoomID, "userID", req.UserID)
				}
			}
			log.ZInfo(ctx, "handleHungUp: group call continues", "roomID", dbInv.RoomID, "remaining", remaining)
			// Remove only this participant's call-status; others remain in-call.
			s.deleteCallStatusForUser(ctx, req.UserID, dbInv.RoomID)
			// Notify all members that the participant count and list have changed.
			s.goSendGroupCallParticipantCountUpdated(ctx, dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, participantUserIDs)
			log.ZDebug(ctx, "handleHungUp: goSendGroupCallParticipantCountUpdated", "groupID", dbInv.GroupID, "roomID", dbInv.RoomID, "mediaType", dbInv.MediaType, "participantUserIDs", participantUserIDs)
			return &rtc.SignalHungUpResp{}, nil
		}
		// remaining == 0: fall through to tear-down logic below.
	}

	// Terminate the LiveKit room (1:1 always; group only when last participant left).
	if _, err := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: dbInv.RoomID}); err != nil {
		log.ZWarn(ctx, "handleHungUp: DeleteRoom failed", err, "roomID", dbInv.RoomID)
	}

	// Group calls keep the invitation until SignalNotifyGroupCallEnded atomically
	// claims it, so concurrent end notifications are deduplicated by roomID.
	if dbInv.GroupID == "" {
		if err := s.db.DeleteInvitation(ctx, dbInv.RoomID); err != nil {
			log.ZWarn(ctx, "handleHungUp: DeleteInvitation failed", err, "roomID", dbInv.RoomID)
		}
	}

	// Call is fully over — remove status for all participants.
	s.deleteCallStatusForInvitation(ctx, dbInv)

	duration := int64(0)
	if dbInv.GroupID == "" {
		// Always use AcceptTime→hangup on the server so chat duration excludes
		// ring/wait time and is not affected by app-layer invite→hangup timers.
		duration, callStatus := singleChatCallDuration(dbInv)
		s.sendCallRecordChatMsg(ctx, dbInv, callStatus, duration)
	} else {
		duration = groupCallDurationFromInvitation(dbInv)
		// Atomically claim the invitation so only one path (HungUp or
		// SignalNotifyGroupCallEnded) sends GroupCallEndedNotification.
		claimed, claimErr := s.db.TryDeleteInvitation(ctx, dbInv.RoomID)

		if claimErr != nil {
			log.ZWarn(ctx, "handleHungUp: TryDeleteInvitation failed", claimErr, "roomID", dbInv.RoomID)
			return &rtc.SignalHungUpResp{}, nil
		}

		log.ZDebug(ctx, "handleHungUp: sendGroupCallEndedNotification", "dbInv", dbInv, "claimed", claimed)

		if claimed {
			s.broadcastGroupCallStatusToNonInvited(ctx, dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, dbInv.InviterUserID, dbInv.InviteeUserIDList, GroupCallStatusEnded)

			s.sendGroupCallEndedNotification(ctx, dbInv.GroupID, dbInv.InviterUserID, dbInv.MediaType, duration, signalCallActionHungUp)
		}
	}

	log.ZDebug(ctx, "handleHungUp: end", "req", req)

	return &rtc.SignalHungUpResp{}, nil
}

// handleGetTokenByRoomID returns a LiveKit token for an existing room.
func (s *rtcServer) handleGetTokenByRoomID(ctx context.Context, req *rtc.SignalGetTokenByRoomIDReq) (*rtc.SignalGetTokenByRoomIDResp, error) {
	return s.getTokenByRoomID(ctx, req)
}

// SignalGetRoomByGroupID returns room information for a group.
func (s *rtcServer) SignalGetRoomByGroupID(ctx context.Context, req *rtc.SignalGetRoomByGroupIDReq) (*rtc.SignalGetRoomByGroupIDResp, error) {
	if req.GroupID == "" {
		log.ZWarn(ctx, "SignalGetRoomByGroupID", errs.ErrArgs.WrapMsg("groupID is empty"), "req", req)
		return nil, errs.ErrArgs.WrapMsg("groupID is empty")
	}
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		log.ZWarn(ctx, "SignalGetRoomByGroupID", errs.ErrArgs.WrapMsg("op user id is empty"), "req", req)
		return nil, errs.ErrArgs.WrapMsg("op user id is empty")
	}
	if _, err := s.groupClient.GetGroupMemberCache(ctx, req.GroupID, opUserID); err != nil {
		log.ZWarn(ctx, "SignalGetRoomByGroupID", err, "get group member cache failed", "req", req)
		return nil, err
	}

	inv, err := s.db.GetInvitationByGroupID(ctx, req.GroupID)
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			return &rtc.SignalGetRoomByGroupIDResp{InCall: false}, nil
		}
		log.ZWarn(ctx, "SignalGetRoomByGroupID", err, "get invitation by groupID failed", "req", req)
		return nil, err
	}

	participants, inCall, _ := s.livekitRoomParticipantsMeta(ctx, inv.RoomID)
	return &rtc.SignalGetRoomByGroupIDResp{
		Invitation:  modelToInvitationInfo(inv),
		RoomID:      inv.RoomID,
		Participant: participants,
		InCall:      inCall,
	}, nil
}

// livekitRoomParticipantsMeta lists LiveKit participants (identity = OpenIM userID) and builds ParticipantMetaData.
func (s *rtcServer) livekitRoomParticipantsMeta(ctx context.Context, roomID string) ([]*rtc.ParticipantMetaData, bool, error) {
	if roomID == "" {
		return nil, false, nil
	}
	lp, err := s.roomClient.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: roomID})
	if err != nil {
		log.ZWarn(ctx, "LiveKit ListParticipants failed", err, "roomID", roomID)
		return nil, false, err
	}
	uids := make([]string, 0, len(lp.Participants))
	for _, p := range lp.Participants {
		if id := p.GetIdentity(); id != "" {
			uids = append(uids, id)
		}
	}
	if len(uids) == 0 {
		return nil, false, nil
	}
	userMap, err := s.userClient.GetUsersInfoMap(ctx, uids)
	if err != nil {
		log.ZWarn(ctx, "GetUsersInfoMap for room participants failed", err, "roomID", roomID)
		out := make([]*rtc.ParticipantMetaData, 0, len(uids))
		for _, id := range uids {
			out = append(out, &rtc.ParticipantMetaData{UserInfo: &sdkws.PublicUserInfo{UserID: id}})
		}
		return out, true, nil
	}
	out := make([]*rtc.ParticipantMetaData, 0, len(uids))
	for _, id := range uids {
		ui := &sdkws.PublicUserInfo{UserID: id}
		if u := userMap[id]; u != nil {
			ui.Nickname = u.Nickname
			ui.FaceURL = u.FaceURL
			ui.Ex = u.Ex
		}
		out = append(out, &rtc.ParticipantMetaData{UserInfo: ui})
	}
	return out, true, nil
}

// joinCallParticipants returns LiveKit-connected participants plus the user who is
// joining (they may not have connected to LiveKit yet).
func (s *rtcServer) joinCallParticipants(ctx context.Context, roomID, joiningUserID string) ([]*rtc.ParticipantMetaData, bool) {
	participants, inCall, err := s.livekitRoomParticipantsMeta(ctx, roomID)
	if err != nil {
		log.ZWarn(ctx, "joinCallParticipants: livekitRoomParticipantsMeta failed", err, "roomID", roomID)
		participants = nil
		inCall = false
	}
	joiningUserID = strings.TrimSpace(joiningUserID)
	if joiningUserID == "" {
		return participants, inCall
	}
	for _, p := range participants {
		if p.GetUserInfo().GetUserID() == joiningUserID {
			return participants, inCall
		}
	}
	ui := &sdkws.PublicUserInfo{UserID: joiningUserID}
	if u, uerr := s.userClient.GetUserInfo(ctx, joiningUserID); uerr == nil && u != nil {
		ui.Nickname = u.Nickname
		ui.FaceURL = u.FaceURL
		ui.Ex = u.Ex
	}
	participants = append(participants, &rtc.ParticipantMetaData{UserInfo: ui})
	return participants, true
}

func participantUserIDsFromMeta(participants []*rtc.ParticipantMetaData) []string {
	if len(participants) == 0 {
		return nil
	}
	userIDs := make([]string, 0, len(participants))
	seen := make(map[string]struct{}, len(participants))
	for _, p := range participants {
		uid := strings.TrimSpace(p.GetUserInfo().GetUserID())
		if uid == "" {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		userIDs = append(userIDs, uid)
	}
	return userIDs
}

// isLiveKitRoomGone reports whether LiveKit indicates the room no longer exists
// (e.g. after cancel/hangup DeleteRoom). Transient errors return false.
func isLiveKitRoomGone(err error) bool {
	if err == nil {
		return false
	}
	var twerr twirp.Error
	if errors.As(err, &twerr) && twerr.Code() == twirp.NotFound {
		return true
	}
	if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist")
}

// isInvitationPending reports whether the invitation still represents an active call.
func (s *rtcServer) isInvitationPending(ctx context.Context, inv *model.SignalInvitation) bool {
	if inv == nil || inv.RoomID == "" {
		log.ZDebug(ctx, "isInvitationPending: invitation is nil or roomID is empty", "inv", inv)
		return false
	}

	// Ring timeout only applies while nobody has joined yet.
	if inv.AcceptTime <= 0 && inv.Timeout > 0 && inv.InitiateTime > 0 {
		deadlineMs := inv.InitiateTime + int64(inv.Timeout)*1000
		if time.Now().UnixMilli() > deadlineMs {
			log.ZDebug(ctx, "isInvitationPending: timeout", "inv", inv)
			return false
		}
	}

	_, inCall, lkErr := s.livekitRoomParticipantsMeta(ctx, inv.RoomID)
	if lkErr == nil && inCall {
		log.ZDebug(ctx, "isInvitationPending: in call", "inv", inv)
		return true
	}
	if lkErr != nil && isLiveKitRoomGone(lkErr) {
		if s.hasParticipantCallStatusForRoom(ctx, inv) {
			log.ZDebug(ctx, "isInvitationPending: LiveKit room gone but participant still active", "inv", inv)
			return true
		}
		log.ZDebug(ctx, "isInvitationPending: LiveKit room gone", "inv", inv)
		return false
	}

	inviterSt, inviterErr := s.callStatusCache.GetCallStatus(ctx, inv.InviterUserID)
	inviterActive := inviterErr == nil && inviterSt.RoomID == inv.RoomID

	if inviterActive {
		// Unanswered and inviter still tracked in call-status (within timeout above).
		// Some clients ring before joining LiveKit, so an empty room is normal during this phase.
		log.ZDebug(ctx, "isInvitationPending: inviter active", "inv", inv)
		return true
	}

	if s.hasParticipantCallStatusForRoom(ctx, inv) {
		log.ZDebug(ctx, "isInvitationPending: participant active", "inv", inv)
		return true
	}

	log.ZDebug(ctx, "isInvitationPending: not active", "inv", inv)
	return false
}

func (s *rtcServer) hasParticipantCallStatusForRoom(ctx context.Context, inv *model.SignalInvitation) bool {
	userIDs := append([]string{inv.InviterUserID}, inv.InviteeUserIDList...)
	seen := make(map[string]struct{}, len(userIDs))
	for _, uid := range userIDs {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		callSt, err := s.callStatusCache.GetCallStatus(ctx, uid)
		if err == nil && callSt.RoomID == inv.RoomID {
			return true
		}
	}
	return false
}

// finalizeStaleInvitation removes an invitation that is no longer active and, for
// unanswered 1v1 calls, writes a missed-call chat record so offline callees see it
// after syncing. TryDeleteInvitation ensures only one path emits the record.
func (s *rtcServer) finalizeStaleInvitation(ctx context.Context, inv *model.SignalInvitation) {
	if inv == nil || inv.RoomID == "" {
		return
	}
	claimed, err := s.db.TryDeleteInvitation(ctx, inv.RoomID)
	if err != nil {
		log.ZWarn(ctx, "finalizeStaleInvitation: TryDeleteInvitation failed", err, "roomID", inv.RoomID)
		return
	}
	if !claimed {
		return
	}
	s.deleteCallStatusForInvitation(ctx, inv)
	if inv.GroupID == "" && inv.AcceptTime <= 0 {
		s.sendCallRecordChatMsg(ctx, inv, callStatusNotConnected, 0)
		log.ZInfo(ctx, "finalizeStaleInvitation: sent missed call record",
			"roomID", inv.RoomID, "inviterUserID", inv.InviterUserID, "inviteeUserIDList", inv.InviteeUserIDList)
	}
}

// SignalGetTokenByRoomID returns a token for joining a room directly (HTTP API path).
func (s *rtcServer) SignalGetTokenByRoomID(ctx context.Context, req *rtc.SignalGetTokenByRoomIDReq) (*rtc.SignalGetTokenByRoomIDResp, error) {
	return s.getTokenByRoomID(ctx, req)
}

// getTokenByRoomID issues a LiveKit join token; users not in the original invite list are added as invitees.
func (s *rtcServer) getTokenByRoomID(ctx context.Context, req *rtc.SignalGetTokenByRoomIDReq) (*rtc.SignalGetTokenByRoomIDResp, error) {
	dbInv, err := s.db.GetInvitationByRoomID(ctx, req.RoomID)
	if err != nil {
		return nil, errs.WrapMsg(err, "room not found or expired", "roomID", req.RoomID)
	}
	if err := s.ensureCallParticipant(ctx, dbInv, req.UserID); err != nil {
		return nil, err
	}
	token, err := s.genToken(req.RoomID, req.UserID)
	if err != nil {
		return nil, err
	}
	return &rtc.SignalGetTokenByRoomIDResp{
		Token:   token,
		LiveURL: s.config.RpcConfig.LiveKit.ExternalAddress,
	}, nil
}

func (s *rtcServer) ensureCallParticipant(ctx context.Context, inv *model.SignalInvitation, userID string) error {
	if userID == inv.InviterUserID || datautil.Contain(userID, inv.InviteeUserIDList...) {
		return nil
	}
	return s.db.AddInvitee(ctx, inv.RoomID, userID)
}

// SignalGetRooms returns room info for a list of room IDs.
func (s *rtcServer) SignalGetRooms(ctx context.Context, req *rtc.SignalGetRoomsReq) (*rtc.SignalGetRoomsResp, error) {
	if len(req.RoomIDs) == 0 {
		return &rtc.SignalGetRoomsResp{}, nil
	}
	invs, err := s.db.GetInvitationsByRoomIDs(ctx, req.RoomIDs)
	if err != nil {
		return nil, err
	}
	roomList := make([]*rtc.SignalGetRoomByGroupIDResp, 0, len(invs))
	for _, inv := range invs {
		participants, inCall, _ := s.livekitRoomParticipantsMeta(ctx, inv.RoomID)
		roomList = append(roomList, &rtc.SignalGetRoomByGroupIDResp{
			Invitation:  modelToInvitationInfo(inv),
			RoomID:      inv.RoomID,
			Participant: participants,
			InCall:      inCall,
		})
	}
	return &rtc.SignalGetRoomsResp{RoomList: roomList}, nil
}

// GetSignalInvitationInfo retrieves a pending invitation by roomID.
func (s *rtcServer) GetSignalInvitationInfo(ctx context.Context, req *rtc.GetSignalInvitationInfoReq) (*rtc.GetSignalInvitationInfoResp, error) {
	inv, err := s.db.GetInvitationByRoomID(ctx, req.RoomID)
	if err != nil {
		return nil, err
	}
	if !s.isInvitationPending(ctx, inv) {
		s.finalizeStaleInvitation(ctx, inv)
		return nil, errs.ErrRecordNotFound.WrapMsg("invitation not found or expired", "roomID", inv.RoomID)
	}
	return &rtc.GetSignalInvitationInfoResp{
		InvitationInfo: modelToInvitationInfo(inv),
		OfflinePushInfo: &sdkws.OfflinePushInfo{
			Title: inv.OfflinePushTitle,
			Desc:  inv.OfflinePushDesc,
			Ex:    inv.OfflinePushEx,
		},
	}, nil
}

// GetSignalInvitationInfoStartApp retrieves a pending invitation for a user when the app starts.
func (s *rtcServer) GetSignalInvitationInfoStartApp(ctx context.Context, req *rtc.GetSignalInvitationInfoStartAppReq) (*rtc.GetSignalInvitationInfoStartAppResp, error) {
	inv, err := s.db.GetInvitationByInviteeUserID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if !s.isInvitationPending(ctx, inv) {
		s.finalizeStaleInvitation(ctx, inv)
		log.ZDebug(ctx, "GetSignalInvitationInfoStartApp: invitation not found or expired", "inv", inv)
		return nil, errs.ErrRecordNotFound.WrapMsg("invitation not found or expired", "userID", req.UserID)
	}

	log.ZDebug(ctx, "GetSignalInvitationInfoStartApp: invitation found", "inv", inv)
	return &rtc.GetSignalInvitationInfoStartAppResp{
		Invitation: modelToInvitationInfo(inv),
		OfflinePushInfo: &sdkws.OfflinePushInfo{
			Title: inv.OfflinePushTitle,
			Desc:  inv.OfflinePushDesc,
			Ex:    inv.OfflinePushEx,
		},
	}, nil
}

// SignalSendCustomSignal forwards a custom signal to all participants in a room.
func (s *rtcServer) SignalSendCustomSignal(ctx context.Context, req *rtc.SignalSendCustomSignalReq) (*rtc.SignalSendCustomSignalResp, error) {
	inv, err := s.db.GetInvitationByRoomID(ctx, req.RoomID)
	if err != nil {
		log.ZWarn(ctx, "GetInvitationByRoomID failed for custom signal", err, "roomID", req.RoomID)
		return &rtc.SignalSendCustomSignalResp{}, nil
	}
	opUserID := mcontext.GetOpUserID(ctx)
	// Fix P3: 处理 json.Marshal 错误
	content, err := json.Marshal(map[string]any{
		"roomID":     req.RoomID,
		"customInfo": req.CustomInfo,
	})
	if err != nil {
		return nil, errs.WrapMsg(err, "marshal custom signal content failed")
	}
	recipients := make([]string, 0, len(inv.InviteeUserIDList)+1)
	recipients = append(recipients, inv.InviteeUserIDList...)
	recipients = append(recipients, inv.InviterUserID)
	for _, uid := range recipients {
		if uid == opUserID {
			continue
		}
		if err := s.sendCustomSignalNotification(ctx, opUserID, uid, int32(constant.SingleChatType), content); err != nil {
			log.ZWarn(ctx, "sendCustomSignalNotification failed", err, "to", uid)
		}
	}
	return &rtc.SignalSendCustomSignalResp{}, nil
}

// SignalNotifyGroupCallEnded sends GroupCallEndedNotification (1523) to all group members.
// Call this when a group call ends (e.g. last participant left) to trigger OnGroupCallEnded on clients.
// roomID is required; TryDeleteInvitation ensures only the first caller sends the notification.
func (s *rtcServer) SignalNotifyGroupCallEnded(ctx context.Context, req *rtc.SignalNotifyGroupCallEndedReq) (*rtc.SignalNotifyGroupCallEndedResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		log.ZWarn(ctx, "SignalNotifyGroupCallEnded: missing opUserID", errs.ErrNoPermission.WrapMsg("missing opUserID"), "req", req)
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}

	log.ZDebug(ctx, "SignalNotifyGroupCallEnded", "req", req, "opUserID", opUserID)

	if req.GroupID == "" {
		log.ZWarn(ctx, "SignalNotifyGroupCallEnded: groupID is required", errs.ErrArgs.WrapMsg("groupID is required"), "req", req)
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}
	if req.RoomID == "" {
		log.ZWarn(ctx, "SignalNotifyGroupCallEnded", errs.ErrArgs.WrapMsg("roomID is required"), "req", req)
		return nil, errs.ErrArgs.WrapMsg("roomID is required")
	}

	if _, err := s.groupClient.GetGroupMemberCache(ctx, req.GroupID, opUserID); err != nil {
		log.ZWarn(ctx, "SignalNotifyGroupCallEnded: get group member cache failed", err, "get group member cache failed")
		return nil, err
	}

	inv, err := s.db.GetInvitationByRoomID(ctx, req.RoomID)
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			log.ZDebug(ctx, "SignalNotifyGroupCallEnded: invitation already ended, skip duplicate", "roomID", req.RoomID)
			return &rtc.SignalNotifyGroupCallEndedResp{}, nil
		}
		log.ZWarn(ctx, "SignalNotifyGroupCallEnded: get invitation by roomID failed", err, "get invitation by roomID failed")
		return nil, errs.WrapMsg(err, "invitation not found", "roomID", req.RoomID)
	}
	if inv.GroupID != req.GroupID {
		log.ZWarn(ctx, "SignalNotifyGroupCallEnded: groupID does not match invitation", errs.ErrArgs.WrapMsg("groupID does not match invitation"), "req", req)
		return nil, errs.ErrArgs.WrapMsg("groupID does not match invitation")
	}

	// Permission is verified by the GetGroupMemberCache call above.  Do NOT
	// re-check inv.InviteeUserIDList here: mid-call hang-ups use PullInvitee to
	// remove the departing participant from that list while the invitation stays
	// open, so former participants would be incorrectly rejected.

	inviterUserID := req.InviterUserID
	if inviterUserID == "" {
		inviterUserID = inv.InviterUserID
	}

	mediaType := req.MediaType
	if mediaType == "" {
		mediaType = inv.MediaType
	}
	if mediaType == "" {
		log.ZWarn(ctx, "SignalNotifyGroupCallEnded: mediaType is required", errs.ErrArgs.WrapMsg("mediaType is required"), "req", req)
		return nil, errs.ErrArgs.WrapMsg("mediaType is required")
	}

	// 检查 LiveKit 房间内的实时在线人数。
	// 若仍有参与者留在房间，说明通话尚未真正结束，拒绝发送结束通知。
	lp, listErr := s.roomClient.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: req.RoomID})
	if listErr != nil {
		// 查询失败通常意味着房间已不存在，视为通话已结束，继续处理。
		log.ZWarn(ctx, "SignalNotifyGroupCallEnded: ListParticipants failed, treating as empty", listErr, "roomID", req.RoomID)
	} else {
		remaining := len(lp.GetParticipants())
		log.ZInfo(ctx, "SignalNotifyGroupCallEnded: livekit participants", "roomID", req.RoomID, "remaining", remaining)
		if remaining > 0 {
			log.ZDebug(ctx, "SignalNotifyGroupCallEnded: livekit participants", "roomID", req.RoomID, "remaining", remaining)
			return &rtc.SignalNotifyGroupCallEndedResp{}, nil
		}
	}

	claimed, claimErr := s.db.TryDeleteInvitation(ctx, req.RoomID)
	if claimErr != nil {
		log.ZWarn(ctx, "SignalNotifyGroupCallEnded: TryDeleteInvitation failed", claimErr, "roomID", req.RoomID)
		return nil, claimErr
	}
	if !claimed {
		log.ZDebug(ctx, "SignalNotifyGroupCallEnded: duplicate end notify, skip", "roomID", req.RoomID)
		return &rtc.SignalNotifyGroupCallEndedResp{}, nil
	}

	if _, delErr := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: req.RoomID}); delErr != nil {
		log.ZWarn(ctx, "SignalNotifyGroupCallEnded: DeleteRoom failed (non-fatal)", delErr, "roomID", req.RoomID)
	}

	// Notify non-invited members so they dismiss the "call in progress" banner.
	// Mirror the same call made in handleHungUp's tear-down path.

	endReason := req.GetEndReason()
	if endReason == "" {
		endReason = signalCallActionHungUp
	}

	log.ZDebug(ctx, "SignalNotifyGroupCallEnded: sendGroupCallEndedNotification", "req", req, "claimed", claimed, "claimErr", claimErr)

	s.sendGroupCallEndedNotification(ctx, req.GroupID, inviterUserID, mediaType, resolveGroupCallDurationSecs(inv, req.DurationSecs), endReason)

	s.broadcastGroupCallStatusToNonInvited(ctx, req.GroupID, req.RoomID, mediaType, inviterUserID, inv.InviteeUserIDList, GroupCallStatusEnded)

	return &rtc.SignalNotifyGroupCallEndedResp{}, nil
}

// GetSignalInvitationRecords returns paginated call history.
func (s *rtcServer) GetSignalInvitationRecords(ctx context.Context, req *rtc.GetSignalInvitationRecordsReq) (*rtc.GetSignalInvitationRecordsResp, error) {
	total, records, err := s.db.SearchRecords(ctx, req.SendID, req.RecvID, req.SessionType, req.StartTime, req.EndTime, req.Pagination)
	if err != nil {
		return nil, err
	}
	signalRecords := datautil.Slice(records, func(r *model.SignalRecord) *rtc.SignalRecord {
		return &rtc.SignalRecord{
			RoomID:              r.RoomID,
			SID:                 r.SID,
			FileName:            r.FileName,
			MediaType:           r.MediaType,
			SessionType:         r.SessionType,
			InviterUserID:       r.InviterUserID,
			InviterUserNickname: r.InviterUserNickname,
			GroupID:             r.GroupID,
			GroupName:           r.GroupName,
			CreateTime:          r.CreateTime,
			EndTime:             r.EndTime,
			Size:                r.FileSize,
			FileURL:             r.FileURL,
		}
	})
	return &rtc.GetSignalInvitationRecordsResp{
		Total:         int32(total),
		SignalRecords: signalRecords,
	}, nil
}

// DeleteSignalRecords removes call history records by their SIDs.
func (s *rtcServer) DeleteSignalRecords(ctx context.Context, req *rtc.DeleteSignalRecordsReq) (*rtc.DeleteSignalRecordsResp, error) {
	if err := s.db.DeleteRecords(ctx, req.SIDs); err != nil {
		return nil, err
	}
	return &rtc.DeleteSignalRecordsResp{}, nil
}

// ---- helpers ----

// genToken generates a LiveKit access token for the given room and identity.
func (s *rtcServer) genToken(roomID, userID string) (string, error) {
	lk := s.config.RpcConfig.LiveKit
	at := auth.NewAccessToken(lk.APIKey, lk.APISecret)
	grant := &auth.VideoGrant{
		RoomJoin: true,
		Room:     roomID,
	}
	at.SetVideoGrant(grant).
		SetIdentity(userID).
		SetValidFor(s.tokenExpiry)
	return at.ToJWT()
}

// signalingMsgOptions 返回信令通知消息应设置的 Options。
//
// Fix P2+P2(安全): 原代码传 make(map[string]bool) 空 map，导致：
//  1. IsNotificationByMsg 将信令消息误判为普通聊天消息，触发黑名单/好友关系等权限拦截
//  2. IsHistory/IsPersistent 默认为 true，信令消息被写入历史记录占用存储
//  3. IsUnreadCount/IsConversationUpdate 默认 true，污染未读数和会话列表
//
// 信令消息应走 Notification 通道（对话 ID 前缀 "n_"），绕过聊天消息权限校验，
// 且不写历史、不计未读、不更新会话。默认关闭离线推送，仅邀请信令在携带 offlinePushInfo 时开启。
func signalingMsgOptions() map[string]bool {
	opts := make(map[string]bool, 9)
	// IsNotNotification=false 表示"这是通知消息"，让 IsNotificationByMsg 返回 true
	// 从而跳过 modifyMessageByUserMessageReceiveOpt 中的黑名单/好友关系等校验
	datautil.SetSwitchFromOptions(opts, constant.IsNotNotification, false)
	datautil.SetSwitchFromOptions(opts, constant.IsSendMsg, false)
	datautil.SetSwitchFromOptions(opts, constant.IsHistory, false)
	datautil.SetSwitchFromOptions(opts, constant.IsPersistent, false)
	datautil.SetSwitchFromOptions(opts, constant.IsUnreadCount, false)
	datautil.SetSwitchFromOptions(opts, constant.IsConversationUpdate, false)
	datautil.SetSwitchFromOptions(opts, constant.IsSenderConversationUpdate, false)
	datautil.SetSwitchFromOptions(opts, constant.IsSenderSync, false)
	datautil.SetSwitchFromOptions(opts, constant.IsOfflinePush, false)
	return opts
}

// sendSignalingNotification sends a SignalingNotification message to a user via the msg service.
// groupID 在 SessionType 为群类型（如 ReadGroupChatType）时必须非空，否则 msg 服务群聊校验会失败。
func (s *rtcServer) sendSignalingNotification(ctx context.Context, sendID, recvID string, sessionType int32, groupID string, offlinePush *sdkws.OfflinePushInfo, content []byte) error {
	now := time.Now().UnixMilli()
	opts := signalingMsgOptions()
	signalingPayloadType := msgprocessor.SignalingPayloadTypeName(content)
	offlinePushEligible := msgprocessor.IsOfflinePushSignalingContent(content)
	requestedPushTitle := ""
	requestedPushDesc := ""
	if offlinePush != nil {
		requestedPushTitle = offlinePush.Title
		requestedPushDesc = offlinePush.Desc
	}
	// Invite / cancel / reject / timeout may wake offline devices to sync call state.
	if offlinePush != nil && !offlinePushEligible {
		log.ZInfo(ctx, "sendSignalingNotification: offline push stripped (payload not eligible)",
			"sendID", sendID,
			"recvID", recvID,
			"sessionType", sessionType,
			"groupID", groupID,
			"signalingPayloadType", signalingPayloadType,
			"requestedPushTitle", requestedPushTitle,
			"requestedPushDesc", requestedPushDesc,
			"contentLen", len(content),
		)
		offlinePush = nil
	}
	if offlinePush != nil {
		datautil.SetSwitchFromOptions(opts, constant.IsOfflinePush, true)
	}
	msgData := &sdkws.MsgData{
		SendID:      sendID,
		RecvID:      recvID,
		SessionType: sessionType,
		GroupID:     groupID,
		ContentType: int32(constant.SignalingNotification),
		MsgFrom:     int32(constant.SysMsgType),
		Content:     content,
		CreateTime:  now,
		SendTime:    now,
		ServerMsgID: uuid.New().String(),
		ClientMsgID: uuid.New().String(),
		Options:     opts,
	}
	if offlinePush != nil {
		msgData.OfflinePushInfo = offlinePush
	}

	offlinePushEnabled := offlinePush != nil
	offlinePushTitle := ""
	offlinePushDesc := ""
	offlinePushExLen := 0
	offlinePushSound := ""
	if offlinePush != nil {
		offlinePushTitle = offlinePush.Title
		offlinePushDesc = offlinePush.Desc
		offlinePushExLen = len(offlinePush.Ex)
		offlinePushSound = offlinePush.IOSPushSound
	}

	_, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData})
	if err != nil {
		log.ZError(ctx, "sendSignalingNotification failed", err,
			"sendID", sendID,
			"recvID", recvID,
			"sessionType", sessionType,
			"groupID", groupID,
			"clientMsgID", msgData.ClientMsgID,
			"offlinePushEnabled", offlinePushEnabled,
		)
		return err
	}
	log.ZInfo(ctx, "sendSignalingNotification ok",
		"sendID", sendID,
		"recvID", recvID,
		"sessionType", sessionType,
		"groupID", groupID,
		"clientMsgID", msgData.ClientMsgID,
		"serverMsgID", msgData.ServerMsgID,
		"contentType", msgData.ContentType,
		"signalingPayloadType", signalingPayloadType,
		"offlinePushEligible", offlinePushEligible,
		"offlinePushEnabled", offlinePushEnabled,
		"offlinePushTitle", offlinePushTitle,
		"offlinePushDesc", offlinePushDesc,
		"offlinePushExLen", offlinePushExLen,
		"offlinePushSound", offlinePushSound,
		"requestedPushTitle", requestedPushTitle,
		"requestedPushDesc", requestedPushDesc,
	)

	return nil
}

// GroupCallStatusOngoing and GroupCallStatusEnded are the two states broadcast
// to non-invited group members via a CustomSignalNotification payload.
// The JSON field "type" is always "groupCallStatus".
const (
	GroupCallStatusOngoing = "started"
	GroupCallStatusEnded   = "ended"
)

// groupCallStatusPayload is the JSON payload sent inside CustomSignalNotification
// to non-invited group members so that they can render the "call in progress" banner.
type groupCallStatusPayload struct {
	Type          string `json:"type"`   // always "groupCallStatus"
	Status        string `json:"status"` // "started" | "ended"
	GroupID       string `json:"groupID"`
	RoomID        string `json:"roomID"`
	MediaType     string `json:"mediaType"` // "audio" | "video"
	InviterUserID string `json:"inviterUserID"`
}

// broadcastGroupCallStatusToNonInvited sends a CustomSignalNotification to every
// group member who is NOT the inviter and NOT in the invitee list.
// It is called:
//   - on call start (status = GroupCallStatusOngoing) — so non-invited members see the "join" banner
//   - on call end   (status = GroupCallStatusEnded)   — so non-invited members dismiss the banner
//
// Errors per-recipient are non-fatal and only logged.
func (s *rtcServer) broadcastGroupCallStatusToNonInvited(ctx context.Context, groupID, roomID, mediaType, inviterUserID string, inviteeUserIDList []string, status string) {
	if groupID == "" {
		return
	}

	allMemberIDs, err := s.groupClient.GetGroupMemberUserIDs(ctx, groupID)
	if err != nil {
		log.ZWarn(ctx, "broadcastGroupCallStatusToNonInvited: GetGroupMemberUserIDs failed", err, "groupID", groupID)
		return
	}

	// Build an exclusion set: inviter + all explicitly invited members.
	// They are notified through the SignalingNotification channel already.
	excluded := make(map[string]struct{}, len(inviteeUserIDList)+1)
	excluded[inviterUserID] = struct{}{}
	for _, uid := range inviteeUserIDList {
		excluded[uid] = struct{}{}
	}

	content, err := json.Marshal(groupCallStatusPayload{
		Type:          "groupCallStatus",
		Status:        status,
		GroupID:       groupID,
		RoomID:        roomID,
		MediaType:     mediaType,
		InviterUserID: inviterUserID,
	})
	if err != nil {
		log.ZWarn(ctx, "broadcastGroupCallStatusToNonInvited: marshal failed", err)
		return
	}

	for _, memberID := range allMemberIDs {
		if _, skip := excluded[memberID]; skip {
			continue
		}
		if err := s.sendCustomSignalNotification(ctx, inviterUserID, memberID, int32(constant.SingleChatType), content); err != nil {
			log.ZWarn(ctx, "broadcastGroupCallStatusToNonInvited: send failed", err, "memberID", memberID, "status", status)
		} else {
			log.ZDebug(ctx, "broadcastGroupCallStatusToNonInvited: send ok", "memberID", memberID, "status", status)
		}
	}
}

// groupCallStartedDefaultTips returns an English notification text, e.g.
// "Alice started an audio/video call".
func groupCallStartedDefaultTips(nickname, mediaType string) string {
	name := nickname
	if name == "" {
		name = "Someone"
	}
	switch {
	case strings.Contains(mediaType, "video") && strings.Contains(mediaType, "audio"):
		return name + " started an audio/video call"
	case strings.Contains(mediaType, "video"):
		return name + " started a video call"
	case strings.Contains(mediaType, "audio"):
		return name + " started an audio call"
	default:
		return name + " started an audio/video call"
	}
}

// groupCallNotificationConfig returns notification.yml settings for group call events.
func (s *rtcServer) groupCallNotificationConfig(contentType int32) config.NotificationConfig {
	switch contentType {
	case constant.GroupCallStartedNotification:
		return s.config.NotificationConfig.GroupCallStarted
	case constant.GroupCallEndedNotification:
		return s.config.NotificationConfig.GroupCallEnded
	case constant.GroupCallParticipantCountUpdatedNotification:
		return s.config.NotificationConfig.GroupCallParticipantCountUpdated
	case constant.GroupCallParticipantDeclinedNotification:
		return s.config.NotificationConfig.GroupCallParticipantDeclined
	default:
		return config.NotificationConfig{}
	}
}

// groupCallNotificationMsgOptions builds MsgData.Options from notification.yml and
// routes the message into the group notification session (n_<groupID>), not sg_.
func (s *rtcServer) groupCallNotificationMsgOptions(contentType int32) map[string]bool {
	cfg := s.groupCallNotificationConfig(contentType)
	return config.GetOptionsByNotification(cfg, nil)
}

func offlinePushInfoFromConfig(cfg config.NotificationConfig) *sdkws.OfflinePushInfo {
	return &sdkws.OfflinePushInfo{
		Title: cfg.OfflinePush.Title,
		Desc:  cfg.OfflinePush.Desc,
		Ex:    cfg.OfflinePush.Ext,
	}
}

// sendGroupCallStartedNotification sends a GroupCallStartedNotification (1522) on the
// group notification channel (n_) so online clients receive OnGroupCallStarted.
// Errors are non-fatal and only logged.
func (s *rtcServer) sendGroupCallStartedNotification(ctx context.Context, groupID, inviterUserID, mediaType string) {
	if groupID == "" {
		return
	}

	groupInfo, err := s.groupClient.GetGroupInfoCache(ctx, groupID)
	if err != nil {
		log.ZWarn(ctx, "sendGroupCallStartedNotification: GetGroupInfoCache failed", err, "groupID", groupID)
		return
	}

	var opUser *sdkws.GroupMemberFullInfo
	if member, err := s.groupClient.GetGroupMemberCache(ctx, groupID, inviterUserID); err == nil {
		opUser = member
	} else {
		log.ZWarn(ctx, "sendGroupCallStartedNotification: GetGroupMemberCache failed (non-fatal)", err,
			"groupID", groupID, "inviterUserID", inviterUserID)
		opUser = &sdkws.GroupMemberFullInfo{UserID: inviterUserID}
	}

	nickname := opUser.Nickname
	if nickname == "" {
		nickname = opUser.UserID
	}

	tips := &sdkws.GroupCallStartedTips{
		OpUser:      opUser,
		Group:       groupInfo,
		MediaType:   mediaType,
		DefaultTips: groupCallStartedDefaultTips(nickname, mediaType),
	}

	detail := jsonutil.StructToJsonString(tips)
	elem := sdkws.NotificationElem{Detail: detail}
	content, err := json.Marshal(&elem)
	if err != nil {
		log.ZWarn(ctx, "sendGroupCallStartedNotification: marshal NotificationElem failed", err)
		return
	}

	notifyCfg := s.groupCallNotificationConfig(constant.GroupCallStartedNotification)
	now := time.Now().UnixMilli()
	msgData := &sdkws.MsgData{
		SendID:          inviterUserID,
		RecvID:          groupID,
		GroupID:         groupID,
		SessionType:     int32(constant.ReadGroupChatType),
		ContentType:     int32(constant.GroupCallStartedNotification),
		MsgFrom:         int32(constant.SysMsgType),
		Content:         content,
		CreateTime:      now,
		SendTime:        now,
		ServerMsgID:     uuid.New().String(),
		ClientMsgID:     uuid.New().String(),
		Options:         s.groupCallNotificationMsgOptions(constant.GroupCallStartedNotification),
		OfflinePushInfo: offlinePushInfoFromConfig(notifyCfg),
	}
	if _, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData}); err != nil {
		log.ZWarn(ctx, "sendGroupCallStartedNotification: SendMsg failed", err, "groupID", groupID)
	}
}

// callEndedDurationText returns a human-readable English string for the call
// duration, e.g. "10 seconds", "10 minutes", "1 hour 10 minutes".
// Returns empty string when durationSecs <= 0.
func callEndedDurationText(durationSecs int64) string {
	if durationSecs <= 0 {
		return ""
	}
	hours := durationSecs / 3600
	minutes := (durationSecs % 3600) / 60
	seconds := durationSecs % 60

	if hours > 0 {
		hourWord := "hours"
		if hours == 1 {
			hourWord = "hour"
		}
		if minutes > 0 {
			minuteWord := "minutes"
			if minutes == 1 {
				minuteWord = "minute"
			}
			return fmt.Sprintf("%d %s %d %s", hours, hourWord, minutes, minuteWord)
		}
		return fmt.Sprintf("%d %s", hours, hourWord)
	}
	if minutes > 0 {
		minuteWord := "minutes"
		if minutes == 1 {
			minuteWord = "minute"
		}
		if seconds > 0 {
			secondWord := "seconds"
			if seconds == 1 {
				secondWord = "second"
			}
			return fmt.Sprintf("%d %s %d %s", minutes, minuteWord, seconds, secondWord)
		}
		return fmt.Sprintf("%d %s", minutes, minuteWord)
	}
	secondWord := "seconds"
	if seconds == 1 {
		secondWord = "second"
	}
	return fmt.Sprintf("%d %s", seconds, secondWord)
}

// groupCallEndedDefaultTips returns an English notification text, e.g.
// "Alice ended an audio/video call (10 minutes 30 seconds)".
func groupCallEndedDefaultTips(nickname, mediaType string, durationSecs int64) string {
	name := nickname
	if name == "" {
		name = "Someone"
	}
	callType := "audio/video call"
	switch {
	case strings.Contains(mediaType, "video") && strings.Contains(mediaType, "audio"):
		callType = "audio/video call"
	case strings.Contains(mediaType, "video"):
		callType = "video call"
	case strings.Contains(mediaType, "audio"):
		callType = "audio call"
	}
	base := name + " ended an " + callType
	dur := callEndedDurationText(durationSecs)
	if dur == "" {
		return base
	}
	return base + " (" + dur + ")"
}

// liveKitParticipantUserIDs extracts non-empty participant identities from a
// LiveKit ListParticipants response, optionally excluding specific user IDs.
func liveKitParticipantUserIDs(participants []*livekit.ParticipantInfo, excludeUserIDs ...string) []string {
	exclude := make(map[string]struct{}, len(excludeUserIDs))
	for _, uid := range excludeUserIDs {
		if uid != "" {
			exclude[uid] = struct{}{}
		}
	}
	ids := make([]string, 0, len(participants))
	for _, p := range participants {
		id := p.GetIdentity()
		if id == "" {
			continue
		}
		if _, skip := exclude[id]; skip {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func (s *rtcServer) goSendGroupCallParticipantCountUpdated(ctx context.Context, groupID, roomID, mediaType string, participantUserIDs []string) {
	if groupID == "" || len(participantUserIDs) == 0 {
		return
	}
	go s.sendGroupCallParticipantCountUpdatedNotification(context.WithoutCancel(ctx), groupID, roomID, mediaType, participantUserIDs)
}

// groupCallParticipantCountDefaultTips returns a display string for the current
// number of participants in an ongoing group call, e.g. "3 people in the call".
func groupCallParticipantCountDefaultTips(count int32) string {
	if count == 1 {
		return "1 person in the call"
	}
	return fmt.Sprintf("%d people in the call", count)
}

// sendGroupCallParticipantCountUpdatedNotification sends a
// GroupCallParticipantCountUpdatedNotification (1527) to the group whenever the
// in-call participant count changes (someone joined or left while the call is
// still ongoing).  It is intentionally lightweight: isSendMsg=false so no chat
// bubble is created, reliabilityLevel=1 so it is only delivered to online
// clients without being persisted.
// Errors are non-fatal and only logged.
func (s *rtcServer) sendGroupCallParticipantCountUpdatedNotification(ctx context.Context, groupID, roomID, mediaType string, participantUserIDs []string) {
	if groupID == "" {
		return
	}

	groupInfo, err := s.groupClient.GetGroupInfoCache(ctx, groupID)
	if err != nil {
		log.ZWarn(ctx, "sendGroupCallParticipantCountUpdatedNotification: GetGroupInfoCache failed", err, "groupID", groupID)
		return
	}

	participantCount := int32(len(participantUserIDs))

	publicUserList := make([]*sdkws.PublicUserInfo, 0, len(participantUserIDs))
	for _, userID := range participantUserIDs {
		publicUserList = append(publicUserList, &sdkws.PublicUserInfo{UserID: userID})
	}

	tips := &sdkws.GroupCallParticipantCountUpdatedTips{
		Group:               groupInfo,
		RoomID:              roomID,
		ParticipantCount:    participantCount,
		MediaType:           mediaType,
		DefaultTips:         groupCallParticipantCountDefaultTips(participantCount),
		ParticipantUserList: publicUserList,
	}

	detail := jsonutil.StructToJsonString(tips)
	elem := sdkws.NotificationElem{Detail: detail}
	content, err := json.Marshal(&elem)
	if err != nil {
		log.ZWarn(ctx, "sendGroupCallParticipantCountUpdatedNotification: marshal NotificationElem failed", err)
		return
	}

	notifyCfg := s.groupCallNotificationConfig(constant.GroupCallParticipantCountUpdatedNotification)
	now := time.Now().UnixMilli()
	msgData := &sdkws.MsgData{
		SendID:          groupID,
		RecvID:          groupID,
		GroupID:         groupID,
		SessionType:     int32(constant.ReadGroupChatType),
		ContentType:     int32(constant.GroupCallParticipantCountUpdatedNotification),
		MsgFrom:         int32(constant.SysMsgType),
		Content:         content,
		CreateTime:      now,
		SendTime:        now,
		ServerMsgID:     uuid.New().String(),
		ClientMsgID:     uuid.New().String(),
		Options:         s.groupCallNotificationMsgOptions(constant.GroupCallParticipantCountUpdatedNotification),
		OfflinePushInfo: offlinePushInfoFromConfig(notifyCfg),
	}
	if _, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData}); err != nil {
		log.ZWarn(ctx, "sendGroupCallParticipantCountUpdatedNotification: SendMsg failed", err, "groupID", groupID, "count", participantCount)
	}
}

// sendGroupCallParticipantDeclinedNotification sends a GroupCallParticipantDeclinedNotification
// (1528) to the group when an invitee rejects or times out, or the inviter cancels, while the
// group call is still relevant (ongoing or winding down).  Same delivery profile as 1527.
func (s *rtcServer) sendGroupCallParticipantDeclinedNotification(ctx context.Context, groupID, roomID, mediaType, actionType, opUserID string) {
	if groupID == "" || opUserID == "" {
		return
	}

	groupInfo, err := s.groupClient.GetGroupInfoCache(ctx, groupID)
	if err != nil {
		log.ZWarn(ctx, "sendGroupCallParticipantDeclinedNotification: GetGroupInfoCache failed", err, "groupID", groupID)
		return
	}

	tips := &sdkws.GroupCallParticipantDeclinedTips{
		Group:      groupInfo,
		RoomID:     roomID,
		MediaType:  mediaType,
		OpUser:     &sdkws.PublicUserInfo{UserID: opUserID},
		ActionType: actionType,
	}

	detail := jsonutil.StructToJsonString(tips)
	elem := sdkws.NotificationElem{Detail: detail}
	content, err := json.Marshal(&elem)
	if err != nil {
		log.ZWarn(ctx, "sendGroupCallParticipantDeclinedNotification: marshal NotificationElem failed", err)
		return
	}

	notifyCfg := s.groupCallNotificationConfig(constant.GroupCallParticipantDeclinedNotification)
	now := time.Now().UnixMilli()
	msgData := &sdkws.MsgData{
		SendID:          groupID,
		RecvID:          groupID,
		GroupID:         groupID,
		SessionType:     int32(constant.ReadGroupChatType),
		ContentType:     int32(constant.GroupCallParticipantDeclinedNotification),
		MsgFrom:         int32(constant.SysMsgType),
		Content:         content,
		CreateTime:      now,
		SendTime:        now,
		ServerMsgID:     uuid.New().String(),
		ClientMsgID:     uuid.New().String(),
		Options:         s.groupCallNotificationMsgOptions(constant.GroupCallParticipantDeclinedNotification),
		OfflinePushInfo: offlinePushInfoFromConfig(notifyCfg),
	}
	if _, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData}); err != nil {
		log.ZWarn(ctx, "sendGroupCallParticipantDeclinedNotification: SendMsg failed", err, "groupID", groupID, "actionType", actionType, "opUserID", opUserID)
	}
}

// sendGroupCallEndedNotification sends a GroupCallEndedNotification (1523) on the
// group notification channel (n_) so online clients receive OnGroupCallEnded.
// endReason: hungup | cancel | reject | timeout
// Errors are non-fatal and only logged.
func (s *rtcServer) sendGroupCallEndedNotification(ctx context.Context, groupID, inviterUserID, mediaType string, durationSecs int64, endReason string) {
	if groupID == "" {
		log.ZWarn(ctx, "sendGroupCallEndedNotification: groupID is empty", errs.ErrArgs.WrapMsg("groupID is empty"))
		return
	}

	groupInfo, err := s.groupClient.GetGroupInfoCache(ctx, groupID)
	if err != nil {
		log.ZWarn(ctx, "sendGroupCallEndedNotification: GetGroupInfoCache failed", err, "groupID", groupID)
		return
	}

	var opUser *sdkws.GroupMemberFullInfo
	if member, err := s.groupClient.GetGroupMemberCache(ctx, groupID, inviterUserID); err == nil {
		opUser = member
	} else {
		log.ZWarn(ctx, "sendGroupCallEndedNotification: GetGroupMemberCache failed (non-fatal)", err,
			"groupID", groupID, "inviterUserID", inviterUserID)
		opUser = &sdkws.GroupMemberFullInfo{UserID: inviterUserID}
	}

	nickname := opUser.Nickname
	if nickname == "" {
		nickname = opUser.UserID
	}

	tips := &sdkws.GroupCallEndedTips{
		OpUser:       opUser,
		Group:        groupInfo,
		MediaType:    mediaType,
		DurationSecs: durationSecs,
		DefaultTips:  groupCallEndedDefaultTips(nickname, mediaType, durationSecs),
		EndReason:    endReason,
	}

	detail := jsonutil.StructToJsonString(tips)
	elem := sdkws.NotificationElem{Detail: detail}
	content, err := json.Marshal(&elem)
	if err != nil {
		log.ZWarn(ctx, "sendGroupCallEndedNotification: marshal NotificationElem failed", err)
		return
	}

	notifyCfg := s.groupCallNotificationConfig(constant.GroupCallEndedNotification)
	now := time.Now().UnixMilli()
	msgData := &sdkws.MsgData{
		SendID:          inviterUserID,
		RecvID:          groupID,
		GroupID:         groupID,
		SessionType:     int32(constant.ReadGroupChatType),
		ContentType:     int32(constant.GroupCallEndedNotification),
		MsgFrom:         int32(constant.SysMsgType),
		Content:         content,
		CreateTime:      now,
		SendTime:        now,
		ServerMsgID:     uuid.New().String(),
		ClientMsgID:     uuid.New().String(),
		Options:         s.groupCallNotificationMsgOptions(constant.GroupCallEndedNotification),
		OfflinePushInfo: offlinePushInfoFromConfig(notifyCfg),
	}
	if _, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData}); err != nil {
		log.ZWarn(ctx, "sendGroupCallEndedNotification: SendMsg failed", err, "groupID", groupID)
	} else {
		log.ZDebug(ctx, "sendGroupCallEndedNotification: SendMsg success", "groupID", groupID)
	}
}

// sendCustomSignalNotification sends a CustomSignalNotification (1605) to a user.
func (s *rtcServer) sendCustomSignalNotification(ctx context.Context, sendID, recvID string, sessionType int32, content []byte) error {
	now := time.Now().UnixMilli()
	msgData := &sdkws.MsgData{
		SendID:      sendID,
		RecvID:      recvID,
		SessionType: sessionType,
		ContentType: int32(constant.CustomSignalNotification),
		MsgFrom:     int32(constant.SysMsgType),
		Content:     content,
		CreateTime:  now,
		SendTime:    now,
		ServerMsgID: uuid.New().String(),
		ClientMsgID: uuid.New().String(),
		Options:     signalingMsgOptions(),
	}
	_, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData})
	return err
}

// marshalSignalReq serializes a SignalReq to bytes.
// Fix P2: 原代码使用 _ 吞掉错误，序列化失败时返回 nil，导致被叫收到空 Content 消息，来电通知丢失。
func marshalSignalReq(req *rtc.SignalReq) ([]byte, error) {
	b, err := proto.Marshal(req)
	if err != nil {
		return nil, errs.WrapMsg(err, "marshal SignalReq failed")
	}
	return b, nil
}

// newRoomID generates a unique room ID.
func newRoomID() string {
	return fmt.Sprintf("room-%s", uuid.New().String())
}

// invitationToModel converts a proto InvitationInfo to the database model.
func invitationToModel(inv *rtc.InvitationInfo, push *sdkws.OfflinePushInfo) *model.SignalInvitation {
	now := time.Now()
	m := &model.SignalInvitation{
		RoomID:             inv.RoomID,
		InviterUserID:      inv.InviterUserID,
		InviteeUserIDList:  inv.InviteeUserIDList,
		CustomData:         inv.CustomData,
		GroupID:            inv.GroupID,
		Timeout:            inv.Timeout,
		MediaType:          inv.MediaType,
		PlatformID:         inv.PlatformID,
		SessionType:        inv.SessionType,
		InitiateTime:       inv.InitiateTime,
		BusyLineUserIDList: inv.BusyLineUserIDList,
		CreateTime:         now.UnixMilli(),
	}
	if push != nil {
		m.OfflinePushTitle = push.Title
		m.OfflinePushDesc = push.Desc
		m.OfflinePushEx = push.Ex
	}
	return m
}

// modelToInvitationInfo converts a database model to proto InvitationInfo.
func modelToInvitationInfo(m *model.SignalInvitation) *rtc.InvitationInfo {
	if m == nil {
		return nil
	}
	return &rtc.InvitationInfo{
		InviterUserID:      m.InviterUserID,
		InviteeUserIDList:  m.InviteeUserIDList,
		CustomData:         m.CustomData,
		GroupID:            m.GroupID,
		RoomID:             m.RoomID,
		Timeout:            m.Timeout,
		MediaType:          m.MediaType,
		PlatformID:         m.PlatformID,
		SessionType:        m.SessionType,
		InitiateTime:       m.InitiateTime,
		BusyLineUserIDList: m.BusyLineUserIDList,
	}
}

// ---- call record chat message ----

// groupCallDurationFromInvitation returns elapsed seconds since the group call was initiated.
func groupCallDurationFromInvitation(inv *model.SignalInvitation) int64 {
	if inv == nil || inv.InitiateTime <= 0 {
		return 0
	}
	if nowMs := time.Now().UnixMilli(); nowMs > inv.InitiateTime {
		return (nowMs - inv.InitiateTime) / 1000
	}
	return 0
}

// resolveGroupCallDurationSecs prefers client-reported duration when positive,
// otherwise falls back to server-side calculation from the invitation record.
func resolveGroupCallDurationSecs(inv *model.SignalInvitation, clientDuration int64) int64 {
	if clientDuration > 0 {
		return clientDuration
	}
	return groupCallDurationFromInvitation(inv)
}

// singleChatCallDuration computes 1:1 talk duration from AcceptTime.
// Returns (durationSecs, status); status is answered when AcceptTime > 0.
func singleChatCallDuration(inv *model.SignalInvitation) (int64, string) {
	if inv.AcceptTime <= 0 {
		return 0, callStatusNotConnected
	}
	nowMs := time.Now().UnixMilli()
	if nowMs <= inv.AcceptTime {
		return 0, callStatusAnswered
	}
	return (nowMs - inv.AcceptTime) / 1000, callStatusAnswered
}

const (
	callStatusAnswered     = "answered"
	callStatusCancelled    = "cancelled"
	callStatusRejected     = "rejected"
	callStatusNotConnected = "not_connected"
	callStatusBusy         = "busy"
)

// callRecordData is the JSON payload embedded in a Custom (110) chat message
// representing a completed call event in the conversation timeline.
// Clients render this as a call bubble, e.g. "[语音通话] 2分05秒".
type callRecordData struct {
	CustomType        string   `json:"customType"` // always "rtcCallRecord"
	MediaType         string   `json:"mediaType"`  // "audio" | "video"
	Status            string   `json:"status"`     // answered / cancelled / rejected / not_connected
	Duration          int64    `json:"duration"`   // seconds; 0 for unanswered calls
	InviterUserID     string   `json:"inviterUserID"`
	InviteeUserIDList []string `json:"inviteeUserIDList"`
	RoomID            string   `json:"roomID"`
}

// callRecordMsgOptions returns message Options for a persisted call-record chat message.
// Unlike signalingMsgOptions (n_ notification conversation), these options route the
// message into the si_/sg_ chat conversation and persist it to history.
// Offline push is disabled: call-related wake notifications are delivered only via
// signaling (invite/cancel/reject/timeout), which respect AvNotification. Without this,
// hang-up/cancel call records would trigger a generic [NEWMSG] banner even when the
// callee disabled AV notifications and never received the invite push.
//
// Answered calls do not increment unread count: both parties were on the call and
// should not see a new unread badge when the hang-up record is written.
func callRecordMsgOptions(status string) map[string]bool {
	opts := make(map[string]bool, 8)
	datautil.SetSwitchFromOptions(opts, constant.IsNotNotification, true)                                                     // → si_/sg_ chat conversation
	datautil.SetSwitchFromOptions(opts, constant.IsHistory, true)                                                             // → write to history
	datautil.SetSwitchFromOptions(opts, constant.IsPersistent, true)                                                          // → persist to storage
	datautil.SetSwitchFromOptions(opts, constant.IsUnreadCount, status != callStatusAnswered && status != callStatusRejected) // → unread only for missed/unanswered calls
	datautil.SetSwitchFromOptions(opts, constant.IsConversationUpdate, true)                                                  // → update conv last message
	datautil.SetSwitchFromOptions(opts, constant.IsSenderConversationUpdate, true)                                            // → update inviter's conv too
	datautil.SetSwitchFromOptions(opts, constant.IsSenderSync, true)                                                          // → sync to inviter's other devices
	datautil.SetSwitchFromOptions(opts, constant.IsOfflinePush, false)                                                        // → no offline banner for call records
	return opts
}

// callRecordDescription builds a human-readable description used as CustomElem.description.
func callRecordDescription(mediaType, status string, duration int64) string {
	prefix := "[语音通话]"
	if mediaType == "video" {
		prefix = "[视频通话]"
	}
	switch status {
	case callStatusAnswered:
		mins := duration / 60
		secs := duration % 60
		if mins > 0 {
			return fmt.Sprintf("%s %d分%02d秒", prefix, mins, secs)
		}
		return fmt.Sprintf("%s %d秒", prefix, secs)
	case callStatusCancelled:
		return prefix + " 已取消"
	case callStatusRejected:
		return prefix + " 已拒绝"
	case callStatusBusy:
		return prefix + " 忙线"
	default:
		return prefix + " 未接通"
	}
}

// callRecordClientMsgID returns a stable id so duplicate finalize/send paths are idempotent per room.
func callRecordClientMsgID(roomID string) string {
	return "rtc-call-record-" + roomID
}

// sendCallRecordChatMsg sends a Custom (110) chat message to the si_ conversation
// representing a completed 1v1 call. Errors are non-fatal and only logged.
//
// Group calls use sendGroupCallStartedNotification / sendGroupCallEndedNotification
// instead and do not write rtcCallRecord messages to the group timeline.
func (s *rtcServer) sendCallRecordChatMsg(ctx context.Context, inv *model.SignalInvitation, status string, duration int64) {
	if inv.GroupID != "" {
		return
	}

	inner, err := json.Marshal(callRecordData{
		CustomType:        "rtcCallRecord",
		MediaType:         inv.MediaType,
		Status:            status,
		Duration:          duration,
		InviterUserID:     inv.InviterUserID,
		InviteeUserIDList: inv.InviteeUserIDList,
		RoomID:            inv.RoomID,
	})
	if err != nil {
		log.ZWarn(ctx, "sendCallRecordChatMsg: marshal inner failed", err)
		return
	}
	content, err := json.Marshal(map[string]string{
		"data":        string(inner),
		"description": callRecordDescription(inv.MediaType, status, duration),
		"extension":   "",
	})
	if err != nil {
		log.ZWarn(ctx, "sendCallRecordChatMsg: marshal content failed", err)
		return
	}

	now := time.Now().UnixMilli()
	recvID := ""
	if len(inv.InviteeUserIDList) > 0 {
		recvID = inv.InviteeUserIDList[0]
	}

	msgData := &sdkws.MsgData{
		SendID:      inv.InviterUserID,
		RecvID:      recvID,
		SessionType: int32(constant.SingleChatType),
		ContentType: int32(constant.Custom),
		MsgFrom:     int32(constant.SysMsgType),
		Content:     content,
		CreateTime:  now,
		SendTime:    now,
		ServerMsgID: uuid.New().String(),
		ClientMsgID: callRecordClientMsgID(inv.RoomID),
		Options:     callRecordMsgOptions(status),
	}

	log.ZInfo(ctx, "sendCallRecordChatMsg", "msgData", msgData, "inv", inv)

	if _, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData}); err != nil {
		log.ZWarn(ctx, "sendCallRecordChatMsg: SendMsg failed", err, "roomID", inv.RoomID, "status", status)
	}
}

// handleTimeout processes a call timeout (no invitee answered within Timeout seconds).
// The inviter's client sends this signal; the server notifies invitees of the missed call,
// tears down the LiveKit room, and writes a "not_connected" call-record to chat history.
func (s *rtcServer) handleTimeout(ctx context.Context, req *rtc.SignalTimeoutReq, signalReq *rtc.SignalReq) (*rtc.SignalTimeoutResp, error) {
	if req.Invitation == nil {
		return nil, errs.ErrArgs.WrapMsg("invitation is nil")
	}

	log.ZInfo(ctx, "handleTimeout: start", "req", req)

	dbInv, err := s.db.GetInvitationByRoomID(ctx, req.Invitation.RoomID)
	if err != nil {
		log.ZWarn(ctx, "handleTimeout: GetInvitationByRoomID failed", err, "roomID", req.Invitation.RoomID)
		return nil, errs.WrapMsg(err, "invitation not found or expired", "roomID", req.Invitation.RoomID)
	}
	if req.UserID != dbInv.InviterUserID {
		return nil, errs.ErrNoPermission.WrapMsg("only the inviter can report timeout",
			"userID", req.UserID, "inviterUserID", dbInv.InviterUserID)
	}

	sessionType := int32(constant.SingleChatType)
	if dbInv.GroupID != "" {
		sessionType = int32(constant.ReadGroupChatType)
	}
	content, err := marshalSignalReq(signalReq)
	if err != nil {
		return nil, err
	}
	invInfo := modelToInvitationInfo(dbInv)
	clientPush := req.OfflinePushInfo
	if clientPush == nil {
		clientPush = offlinePushInfoFromInvitationModel(dbInv)
	}
	timeoutOfflinePush := s.resolveSignalingOfflinePushInfo(ctx, invInfo, clientPush, signalCallActionTimeout, req.UserID, "")
	for _, inviteeID := range dbInv.InviteeUserIDList {
		inviteeOfflinePush := timeoutOfflinePush
		if sessionType == int32(constant.SingleChatType) {
			inviteeOfflinePush = s.resolveSignalingOfflinePushInfo(ctx, invInfo, clientPush, signalCallActionTimeout, req.UserID, inviteeID)
		}
		if err := s.sendSignalingNotification(ctx, req.UserID, inviteeID, sessionType, dbInv.GroupID, inviteeOfflinePush, content); err != nil {
			log.ZWarn(ctx, "handleTimeout: sendSignalingNotification to invitee failed", err, "inviteeID", inviteeID)
		}
	}

	if dbInv.GroupID != "" {
		lp, listErr := s.roomClient.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: dbInv.RoomID})
		joinedCount := 0
		inRoom := make(map[string]struct{})
		if listErr != nil {
			log.ZWarn(ctx, "handleTimeout: ListParticipants failed", listErr, "roomID", dbInv.RoomID)
		} else {
			for _, p := range lp.Participants {
				id := p.GetIdentity()
				if id == "" {
					continue
				}
				inRoom[id] = struct{}{}
				if id != dbInv.InviterUserID {
					joinedCount++
				}
			}
		}

		timedOutInvitees := make([]string, 0, len(dbInv.InviteeUserIDList))
		for _, inviteeID := range dbInv.InviteeUserIDList {
			if _, joined := inRoom[inviteeID]; joined {
				continue
			}
			timedOutInvitees = append(timedOutInvitees, inviteeID)
		}

		for _, inviteeID := range timedOutInvitees {
			log.ZInfo(ctx, "handleTimeout: sendGroupCallParticipantDeclinedNotification to invitee", "inviteeID", inviteeID)
			go s.sendGroupCallParticipantDeclinedNotification(context.WithoutCancel(ctx), dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, signalCallActionTimeout, inviteeID)
		}

		// When at least one invitee has joined (or accept is recorded), the group
		// call continues. Still notify declined for offline/unanswered invitees.
		if dbInv.AcceptTime > 0 || joinedCount > 0 {
			log.ZInfo(ctx, "handleTimeout: group call continues, timed out invitees notified",
				"roomID", dbInv.RoomID, "joinedCount", joinedCount, "acceptTime", dbInv.AcceptTime, "timedOutInvitees", timedOutInvitees)
			for _, inviteeID := range timedOutInvitees {
				if err := s.db.PullInvitee(ctx, dbInv.RoomID, inviteeID); err != nil {
					log.ZWarn(ctx, "handleTimeout: PullInvitee failed", err, "roomID", dbInv.RoomID, "userID", inviteeID)
				}
				s.deleteCallStatusForUser(ctx, inviteeID, dbInv.RoomID)
			}
			s.goSendGroupCallParticipantCountUpdated(ctx, dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, liveKitParticipantUserIDs(lp.Participants))

			log.ZDebug(ctx, "handleTimeout: goSendGroupCallParticipantCountUpdated", "groupID", dbInv.GroupID, "roomID", dbInv.RoomID, "mediaType", dbInv.MediaType, "participantUserIDs", liveKitParticipantUserIDs(lp.Participants))

			return &rtc.SignalTimeoutResp{}, nil
		}

		if _, err := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: dbInv.RoomID}); err != nil {
			log.ZWarn(ctx, "handleTimeout: LiveKit DeleteRoom failed", err, "roomID", dbInv.RoomID)
		}

		claimed, claimErr := s.db.TryDeleteInvitation(ctx, dbInv.RoomID)
		if claimErr != nil {
			log.ZWarn(ctx, "handleTimeout: TryDeleteInvitation failed", claimErr, "roomID", dbInv.RoomID)
		}

		// Timeout ended the group call — remove all participants' statuses.
		s.deleteCallStatusForInvitation(ctx, dbInv)

		log.ZDebug(ctx, "handleTimeout: sendGroupCallEndedNotification", "dbInv", dbInv, "claimed", claimed)

		if claimed {
			s.goSendGroupCallParticipantCountUpdated(ctx, dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, nil)

			log.ZDebug(ctx, "handleTimeout: goSendGroupCallParticipantCountUpdated", "groupID", dbInv.GroupID, "roomID", dbInv.RoomID, "mediaType", dbInv.MediaType, "participantUserIDs", nil)

			s.broadcastGroupCallStatusToNonInvited(ctx, dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, dbInv.InviterUserID, dbInv.InviteeUserIDList, GroupCallStatusEnded)

			s.sendGroupCallEndedNotification(ctx, dbInv.GroupID, dbInv.InviterUserID, dbInv.MediaType, groupCallDurationFromInvitation(dbInv), signalCallActionTimeout)
		}
	} else {
		if dbInv.AcceptTime > 0 {
			log.ZInfo(ctx, "handleTimeout: call already answered, skip tear-down", "roomID", dbInv.RoomID, "acceptTime", dbInv.AcceptTime)
			return &rtc.SignalTimeoutResp{}, nil
		}
		if _, err := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: dbInv.RoomID}); err != nil {
			log.ZWarn(ctx, "handleTimeout: LiveKit DeleteRoom failed", err, "roomID", dbInv.RoomID)
		}
		if err := s.db.DeleteInvitation(ctx, dbInv.RoomID); err != nil {
			log.ZWarn(ctx, "handleTimeout: DeleteInvitation failed", err, "roomID", dbInv.RoomID)
		}

		// Single chat timed out — remove both parties' statuses.
		s.deleteCallStatusForInvitation(ctx, dbInv)

		s.sendCallRecordChatMsg(ctx, dbInv, callStatusNotConnected, 0)

	}

	log.ZInfo(ctx, "handleTimeout", "dbInv", dbInv)

	return &rtc.SignalTimeoutResp{}, nil
}

// hungUpPeerIDsFromDB returns the IDs that should receive the hang-up signal,
// based on the authoritative DB invitation data.
//
// For 1:1 calls the logic is simple: notify the other party.
// For group calls every participant except the caller is notified so that all
// remaining members can update their UI (e.g. remove the leaving member's
// avatar from the call banner).
func hungUpPeerIDsFromDB(inv *model.SignalInvitation, callerID string) []string {
	if inv.GroupID == "" {
		// 1:1 call
		if callerID == inv.InviterUserID {
			return inv.InviteeUserIDList
		}
		return []string{inv.InviterUserID}
	}

	// Group call: collect all participants except the caller.
	all := make([]string, 0, len(inv.InviteeUserIDList)+1)
	if inv.InviterUserID != callerID {
		all = append(all, inv.InviterUserID)
	}
	for _, uid := range inv.InviteeUserIDList {
		if uid != callerID {
			all = append(all, uid)
		}
	}
	return all
}

// pendingReachableInvitees returns invitees who were actually rung (not on busy line)
// and have not yet accepted or rejected.
func pendingReachableInvitees(inviteeUserIDList, busyLineUserIDList []string) []string {
	if len(inviteeUserIDList) == 0 {
		return nil
	}
	busySet := make(map[string]struct{}, len(busyLineUserIDList))
	for _, uid := range busyLineUserIDList {
		busySet[uid] = struct{}{}
	}
	pending := make([]string, 0, len(inviteeUserIDList))
	for _, uid := range inviteeUserIDList {
		if _, busy := busySet[uid]; busy {
			continue
		}
		pending = append(pending, uid)
	}
	return pending
}

// ─── Call-status helpers ──────────────────────────────────────────────────────
//
// These helpers translate invitation data into UserCallStatus entries and write
// them to Redis.  All errors are non-fatal: a failure to update the call-status
// cache must never break the signalling flow itself, so callers log and continue.

// isUserOnActiveCall reports whether the user is ringing or already in a call.
func isUserOnActiveCall(status int32) bool {
	return status == model.CallStatusConnecting || status == model.CallStatusInCall
}

// handleHeartbeat refreshes the caller's own call-status TTL in Redis so an
// active call is not misclassified as idle once the call-status TTL elapses.
// Each client heartbeats for itself only; both parties must send heartbeats
// during a call to keep their respective busy keys alive.
func (s *rtcServer) handleHeartbeat(ctx context.Context, req *rtc.SignalHeartbeatReq) (*rtc.SignalHeartbeatResp, error) {
	if req == nil {
		return nil, errs.ErrArgs.WrapMsg("heartbeat req is nil")
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return nil, errs.ErrArgs.WrapMsg("heartbeat userID is empty")
	}

	if err := s.callStatusCache.RefreshCallStatusTTL(ctx, userID); err != nil {
		log.ZWarn(ctx, "handleHeartbeat: RefreshCallStatusTTL failed", err, "userID", userID, "roomID", req.RoomID)
	} else {
		log.ZDebug(ctx, "handleHeartbeat: refreshed call status TTL", "userID", userID, "roomID", req.RoomID)
	}
	return &rtc.SignalHeartbeatResp{}, nil
}

// getCalleeActiveCallStatus returns the callee's Redis call status when they are
// ringing (Connecting) or in an active call (InCall).
func (s *rtcServer) getCalleeActiveCallStatus(ctx context.Context, userID string) (*model.UserCallStatus, bool) {
	callSt, err := s.callStatusCache.GetCallStatus(ctx, userID)
	if err != nil {
		log.ZWarn(ctx, "getCalleeActiveCallStatus: no active call status", err, "userID", userID)
		return nil, false
	}
	if !isUserOnActiveCall(callSt.Status) {
		return nil, false
	}
	return callSt, true
}

// isCalleeBusyOnAnotherCall reports whether userID is on a different active call than roomID.
func (s *rtcServer) isCalleeBusyOnAnotherCall(ctx context.Context, userID, roomID string) bool {
	if callSt, busy := s.getCalleeActiveCallStatus(ctx, userID); busy {
		return callSt.RoomID != roomID
	}
	inv, err := s.db.GetInvitationByUserID(ctx, userID)
	if err != nil {
		return false
	}
	if inv == nil || inv.RoomID == roomID {
		return false
	}
	return s.isInvitationPending(ctx, inv)
}

// isCalleeOnActiveCall reports whether userID should be treated as busy for a new invite.
// It checks Redis first, then falls back to Mongo invitation + isInvitationPending
// so long calls are not misclassified as idle after the call-status TTL.
func (s *rtcServer) isCalleeOnActiveCall(ctx context.Context, userID string) bool {
	if _, busy := s.getCalleeActiveCallStatus(ctx, userID); busy {
		return true
	}

	inv, err := s.db.GetInvitationByUserID(ctx, userID)
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			return false
		}
		log.ZWarn(ctx, "isCalleeOnActiveCall: GetInvitationByUserID failed", err, "userID", userID)
		return false
	}
	if inv == nil {
		return false
	}
	if !s.isInvitationPending(ctx, inv) {
		//s.finalizeStaleInvitation(ctx, inv)
		return false
	}
	log.ZInfo(ctx, "isCalleeOnActiveCall: busy via invitation fallback", "userID", userID, "roomID", inv.RoomID, "acceptTime", inv.AcceptTime)
	return true
}

// setCallStatusConnecting marks the inviter and all (allowed) invitees as
// "connecting" in Redis.  It is called immediately after an invitation is
// persisted and the invite notifications have been dispatched.
// inv is the proto InvitationInfo, which is available in handleInvite /
// handleInviteInGroup before the DB model is materialised.
func (s *rtcServer) setCallStatusConnecting(ctx context.Context, inv *rtc.InvitationInfo, notAllowSet map[string]struct{}) {
	sessionType := int32(constant.SingleChatType)
	if inv.GroupID != "" {
		sessionType = int32(constant.ReadGroupChatType)
	}
	now := time.Now().UnixMilli()

	// Collect the invitees who actually received a notification.
	reachable := make([]string, 0, len(inv.InviteeUserIDList))
	for _, uid := range inv.InviteeUserIDList {
		if _, skip := notAllowSet[uid]; !skip {
			reachable = append(reachable, uid)
		}
	}

	// Inviter's status: peers = reachable invitees.
	inviterStatus := &model.UserCallStatus{
		Status:      model.CallStatusConnecting,
		RoomID:      inv.RoomID,
		MediaType:   inv.MediaType,
		SessionType: sessionType,
		GroupID:     inv.GroupID,
		PeerIDs:     reachable,
		UpdatedAt:   now,
	}
	if err := s.callStatusCache.SetCallStatus(ctx, inv.InviterUserID, inviterStatus); err != nil {
		log.ZWarn(ctx, "setCallStatusConnecting: set inviter status failed", err, "inviterID", inv.InviterUserID, "roomID", inv.RoomID)
	}

	// Each reachable invitee's status: peers = [inviter].
	for _, uid := range reachable {
		inviteeStatus := &model.UserCallStatus{
			Status:      model.CallStatusConnecting,
			RoomID:      inv.RoomID,
			MediaType:   inv.MediaType,
			SessionType: sessionType,
			GroupID:     inv.GroupID,
			PeerIDs:     []string{inv.InviterUserID},
			UpdatedAt:   now,
		}
		if err := s.callStatusCache.SetCallStatus(ctx, uid, inviteeStatus); err != nil {
			log.ZWarn(ctx, "setCallStatusConnecting: set invitee status failed", err, "inviteeID", uid, "roomID", inv.RoomID)
		}
	}
}

// setCallStatusInCall transitions the inviter and the accepting invitee to
// "in-call".  For group calls the other invitees remain "connecting" until
// they either accept or the call ends.
func (s *rtcServer) setCallStatusInCall(ctx context.Context, inv *model.SignalInvitation, acceptorID string) {
	sessionType := int32(constant.SingleChatType)
	if inv.GroupID != "" {
		sessionType = int32(constant.ReadGroupChatType)
	}
	now := time.Now().UnixMilli()

	for _, uid := range []string{inv.InviterUserID, acceptorID} {
		peers := make([]string, 0, 2)
		if uid == inv.InviterUserID {
			peers = append(peers, acceptorID)
		} else {
			peers = append(peers, inv.InviterUserID)
		}
		st := &model.UserCallStatus{
			Status:      model.CallStatusInCall,
			RoomID:      inv.RoomID,
			MediaType:   inv.MediaType,
			SessionType: sessionType,
			GroupID:     inv.GroupID,
			PeerIDs:     peers,
			UpdatedAt:   now,
		}
		if err := s.callStatusCache.SetCallStatus(ctx, uid, st); err != nil {
			log.ZWarn(ctx, "setCallStatusInCall: set status failed", err, "userID", uid, "roomID", inv.RoomID)
		}
	}
}

// deleteCallStatusForInvitation removes call-status entries for every
// participant in a completed (or abandoned) invitation.
// It is used on cancel / reject (all rejected) / hang-up (last participant) / timeout.
func (s *rtcServer) deleteCallStatusForInvitation(ctx context.Context, inv *model.SignalInvitation) {
	all := make([]string, 0, len(inv.InviteeUserIDList)+1)
	all = append(all, inv.InviterUserID)
	all = append(all, inv.InviteeUserIDList...)
	if err := s.callStatusCache.DeleteCallStatus(ctx, all...); err != nil {
		log.ZWarn(ctx, "deleteCallStatusForInvitation: delete failed", err, "roomID", inv.RoomID, "userIDs", all)
	}
}

// deleteCallStatusForUser removes the call-status entry for a single user who
// left a group call while other participants remain.
func (s *rtcServer) deleteCallStatusForUser(ctx context.Context, userID, roomID string) {
	if err := s.callStatusCache.DeleteCallStatus(ctx, userID); err != nil {
		log.ZWarn(ctx, "deleteCallStatusForUser: delete failed", err, "userID", userID, "roomID", roomID)
	}
}
