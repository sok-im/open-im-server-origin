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
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/livekit/protocol/auth"
	livekit "github.com/livekit/protocol/livekit"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/constant"
	pbmsg "github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/rtc"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/jsonutil"
	"go.mongodb.org/mongo-driver/mongo"
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
	default:
		return nil, errs.ErrArgs.WrapMsg("unknown signal payload type")
	}
	if respErr != nil {
		log.ZError(ctx, "SignalMessageAssemble", respErr, "err", respErr.Error())
		return nil, respErr
	}
	return &rtc.SignalMessageAssembleResp{SignalResp: &resp}, nil
}

// handleInvite processes a 1-to-1 call invitation.
func (s *rtcServer) handleInvite(ctx context.Context, req *rtc.SignalInviteReq, signalReq *rtc.SignalReq) (*rtc.SignalInviteResp, error) {
	inv := req.Invitation
	if inv == nil {
		log.ZError(ctx, "handleInvite", errs.ErrArgs, "r", "invitation is nil")
		return nil, errs.ErrArgs.WrapMsg("invitation is nil")
	}
	inv.RoomID = newRoomID()
	inv.InviterUserID = req.UserID
	inv.InitiateTime = time.Now().UnixMilli()

	if len(inv.InviteeUserIDList) == 0 {
		return nil, errs.ErrArgs.WrapMsg("no invitees", "inviteeUserIDList", inv.InviteeUserIDList)
	}

	notAllowUserIDs, notAllowSet, err := s.filterNotAllowedInvitees(ctx, req.UserID, inv.InviteeUserIDList)
	if err != nil {
		return nil, err
	}
	inv.NotAllowUserIDList = notAllowUserIDs

	if len(notAllowUserIDs) == len(inv.InviteeUserIDList) {
		return nil, errs.ErrNoPermission.WrapMsg("all invitees do not accept calls from you", "inviteeUserIDList", inv.InviteeUserIDList)
	}

	// 检测哪些被叫用户正忙（已在通话中），记录到 BusyLineUserIDList
	busyUserIDs, err := s.db.GetBusyUserIDs(ctx, inv.InviteeUserIDList)
	if err != nil {
		log.ZWarn(ctx, "handleInvite: GetBusyUserIDs failed (non-fatal)", err)
	}
	busySet := make(map[string]struct{}, len(busyUserIDs))
	for _, uid := range busyUserIDs {
		busySet[uid] = struct{}{}
	}
	inv.BusyLineUserIDList = busyUserIDs

	if len(busyUserIDs) == len(inv.InviteeUserIDList) {
		return nil, servererrs.ErrAllUserBusy.WrapMsg("all invitees are busy", "inviteeUserIDList", inv.InviteeUserIDList)
	}

	// 从主叫用户资料获取铃声 URL，注入到邀请信息中，被叫方收到后播放主叫方铃声
	if inviterInfo, err := s.userClient.GetUserInfo(ctx, req.UserID); err == nil && inviterInfo.CallRingtoneURL != "" {
		inv.CallerRingtoneURL = inviterInfo.CallRingtoneURL
	}

	// 查询被叫方铃声 URL，供主叫方在等待时播放
	var calleeRingtoneURL string
	for _, inviteeID := range inv.InviteeUserIDList {
		if _, notAllow := notAllowSet[inviteeID]; notAllow {
			continue
		}
		if _, busy := busySet[inviteeID]; busy {
			continue
		}
		if inviteeInfo, err := s.userClient.GetUserInfo(ctx, inviteeID); err == nil {
			calleeRingtoneURL = inviteeInfo.CallRingtoneURL
		}
		break
	}

	if _, err := s.roomClient.CreateRoom(ctx, &livekit.CreateRoomRequest{Name: inv.RoomID}); err != nil {
		log.ZError(ctx, "handleInvite", err, "r", err.Error())
		return nil, errs.WrapMsg(err, "LiveKit CreateRoom failed", "roomID", inv.RoomID)
	}

	token, err := s.genToken(inv.RoomID, req.UserID)
	if err != nil {
		if _, delErr := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: inv.RoomID}); delErr != nil {
			log.ZWarn(ctx, "handleInvite: rollback DeleteRoom failed", delErr, "roomID", inv.RoomID)
		}
		return nil, err
	}

	if err := s.db.CreateInvitation(ctx, invitationToModel(inv, req.OfflinePushInfo)); err != nil {
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
		return nil, err
	}

	for _, inviteeID := range inv.InviteeUserIDList {
		if _, notAllow := notAllowSet[inviteeID]; notAllow {
			log.ZInfo(ctx, "handleInvite: skip not-allowed invitee", "inviteeID", inviteeID)
			continue
		}
		if _, busy := busySet[inviteeID]; busy {
			log.ZInfo(ctx, "handleInvite: skip busy invitee", "inviteeID", inviteeID)
			continue
		}
		log.ZInfo(ctx, "sendSignalingNotification to invitee", "sendID", req.UserID, "recvID", inviteeID)
		if err := s.sendSignalingNotification(ctx, req.UserID, inviteeID, int32(constant.SingleChatType), "", req.OfflinePushInfo, content); err != nil {
			log.ZError(ctx, "sendSignalingNotification to invitee failed", err, "inviteeID", inviteeID)
			return nil, errs.WrapMsg(err, "failed to notify invitee", "inviteeID", inviteeID)
		}
	}

	log.ZDebug(ctx, "handleInvite", "token", token, "roomID", inv.RoomID, "liveURL", s.config.RpcConfig.LiveKit.ExternalAddress)
	return &rtc.SignalInviteResp{
		Token:              token,
		RoomID:             inv.RoomID,
		LiveURL:            s.config.RpcConfig.LiveKit.ExternalAddress,
		BusyLineUserIDList: busyUserIDs,
		NotAllowUserIDList: notAllowUserIDs,
		CalleeRingtoneURL:  calleeRingtoneURL,
	}, nil
}

// handleInviteInGroup processes a group call invitation.
func (s *rtcServer) handleInviteInGroup(ctx context.Context, req *rtc.SignalInviteInGroupReq, signalReq *rtc.SignalReq) (*rtc.SignalInviteInGroupResp, error) {
	inv := req.Invitation
	if inv == nil {
		return nil, errs.ErrArgs.WrapMsg("invitation is nil")
	}
	if inv.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is empty")
	}

	inv.RoomID = newRoomID()
	inv.InviterUserID = req.UserID
	inv.InitiateTime = time.Now().UnixMilli()

	notAllowUserIDs, notAllowSet, err := s.filterNotAllowedInvitees(ctx, req.UserID, inv.InviteeUserIDList)
	if err != nil {
		return nil, err
	}
	inv.NotAllowUserIDList = notAllowUserIDs

	if len(notAllowUserIDs) == len(inv.InviteeUserIDList) {
		return nil, errs.ErrNoPermission.WrapMsg("all invitees do not accept calls from you", "inviteeUserIDList", inv.InviteeUserIDList)
	}

	// 检测哪些被叫用户正忙（已在通话中），记录到 BusyLineUserIDList
	busyUserIDs, err := s.db.GetBusyUserIDs(ctx, inv.InviteeUserIDList)
	if err != nil {
		log.ZWarn(ctx, "handleInviteInGroup: GetBusyUserIDs failed (non-fatal)", err)
	}
	busySet := make(map[string]struct{}, len(busyUserIDs))
	for _, uid := range busyUserIDs {
		busySet[uid] = struct{}{}
	}
	inv.BusyLineUserIDList = busyUserIDs

	if len(busyUserIDs) == len(inv.InviteeUserIDList) {
		return nil, servererrs.ErrAllUserBusy.WrapMsg("all invitees are busy", "inviteeUserIDList", inv.InviteeUserIDList)
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
		if _, busy := busySet[inviteeID]; busy {
			continue
		}
		if inviteeInfo, err := s.userClient.GetUserInfo(ctx, inviteeID); err == nil {
			calleeRingtoneURL = inviteeInfo.CallRingtoneURL
		}
		break
	}

	if _, err := s.roomClient.CreateRoom(ctx, &livekit.CreateRoomRequest{Name: inv.RoomID}); err != nil {
		return nil, errs.WrapMsg(err, "LiveKit CreateRoom failed", "roomID", inv.RoomID)
	}

	token, err := s.genToken(inv.RoomID, req.UserID)
	if err != nil {
		if _, delErr := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: inv.RoomID}); delErr != nil {
			log.ZWarn(ctx, "handleInviteInGroup: rollback DeleteRoom failed", delErr, "roomID", inv.RoomID)
		}
		return nil, err
	}

	if err := s.db.CreateInvitation(ctx, invitationToModel(inv, req.OfflinePushInfo)); err != nil {
		if !mongo.IsDuplicateKeyError(err) {
			if _, delErr := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: inv.RoomID}); delErr != nil {
				log.ZWarn(ctx, "handleInviteInGroup: rollback DeleteRoom failed", delErr, "roomID", inv.RoomID)
			}
			return nil, errs.WrapMsg(err, "CreateInvitation failed", "roomID", inv.RoomID)
		}
		log.ZWarn(ctx, "handleInviteInGroup: duplicate invitation (idempotent retry)", err, "roomID", inv.RoomID)
	}

	content, err := marshalSignalReq(signalReq)
	if err != nil {
		return nil, err
	}
	for _, inviteeID := range inv.InviteeUserIDList {
		if _, notAllow := notAllowSet[inviteeID]; notAllow {
			log.ZInfo(ctx, "handleInviteInGroup: skipping invitee (call setting blocked)", "inviteeID", inviteeID)
			continue
		}
		if _, busy := busySet[inviteeID]; busy {
			log.ZInfo(ctx, "handleInviteInGroup: skip busy invitee", "inviteeID", inviteeID)
			continue
		}
		if err := s.sendSignalingNotification(ctx, req.UserID, inviteeID, int32(constant.ReadGroupChatType), inv.GroupID, req.OfflinePushInfo, content); err != nil {
			log.ZWarn(ctx, "handleInviteInGroup to group invitee failed", err, "inviteeID", inviteeID)
		}
	}

	// Notify every group member who was NOT explicitly invited so they can
	// render the "call in progress" banner and optionally join.
	// Run in a goroutine so large groups don't block the caller's response.
	go s.broadcastGroupCallStatusToNonInvited(context.WithoutCancel(ctx), inv.GroupID, inv.RoomID, inv.MediaType, inv.InviterUserID, inv.InviteeUserIDList, GroupCallStatusOngoing)

	// Send a group-chat timeline notification to all members: "XXX started an audio/video call".
	go s.sendGroupCallStartedNotification(context.WithoutCancel(ctx), inv.GroupID, inv.InviterUserID, inv.MediaType)

	resp := &rtc.SignalInviteInGroupResp{
		Token:              token,
		RoomID:             inv.RoomID,
		LiveURL:            s.config.RpcConfig.LiveKit.ExternalAddress,
		BusyLineUserIDList: busyUserIDs,
		NotAllowUserIDList: notAllowUserIDs,
		CalleeRingtoneURL:  calleeRingtoneURL,
	}

	log.ZDebug(ctx, "handleInviteInGroup", "req", req, "resp", resp)

	return resp, nil
}

func (s *rtcServer) filterNotAllowedInvitees(ctx context.Context, inviterID string, inviteeIDs []string) ([]string, map[string]struct{}, error) {
	notAllowUserIDs := make([]string, 0)
	notAllowSet := make(map[string]struct{})
	for _, inviteeID := range inviteeIDs {
		allowed, err := s.isCallAllowed(ctx, inviterID, inviteeID)
		if err != nil {
			log.ZError(ctx, "filterNotAllowedInvitees: isCallAllowed failed", err, "inviteeID", inviteeID)
			return nil, nil, err
		}
		if !allowed {
			notAllowUserIDs = append(notAllowUserIDs, inviteeID)
			notAllowSet[inviteeID] = struct{}{}
		}
	}
	return notAllowUserIDs, notAllowSet, nil
}

func hasReachableInvitee(inviteeIDs []string, notAllowSet, busySet map[string]struct{}) bool {
	for _, inviteeID := range inviteeIDs {
		if _, notAllow := notAllowSet[inviteeID]; notAllow {
			continue
		}
		if _, busy := busySet[inviteeID]; busy {
			continue
		}
		return true
	}
	return false
}

// isCallAllowed 判断 inviterID 是否被允许向 inviteeID 发起音视频通话。
// 规则：
//   - CallAcceptSettingPublic(0)  → 所有人均可
//   - CallAcceptSettingFriends(1) → 仅当 inviterID 在 inviteeID 好友列表中
//   - CallAcceptSettingNobody(2)  → 任何人均不可
func (s *rtcServer) isCallAllowed(ctx context.Context, inviterID, inviteeID string) (bool, error) {
	userInfo, err := s.userClient.GetUserInfo(ctx, inviteeID)
	if err != nil {
		return false, err
	}
	switch userInfo.CallAcceptSetting {
	case model.CallAcceptSettingNobody:
		return false, nil
	case model.CallAcceptSettingFriends:
		isFriend, err := s.relationClient.IsFriend(ctx, inviteeID, inviterID)
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

	// 从 DB 获取权威邀请数据，验证邀请存在且 userID 在被邀请人列表中
	dbInv, err := s.db.GetInvitationByRoomID(ctx, req.Invitation.RoomID)
	if err != nil {
		return nil, errs.WrapMsg(err, "invitation not found or expired", "roomID", req.Invitation.RoomID)
	}
	if !datautil.Contain(req.UserID, dbInv.InviteeUserIDList...) {
		return nil, errs.ErrNoPermission.WrapMsg("user not in invitee list", "userID", req.UserID)
	}

	token, err := s.genToken(dbInv.RoomID, req.UserID)
	if err != nil {
		return nil, err
	}

	sessionType := int32(constant.SingleChatType)
	if dbInv.GroupID != "" {
		sessionType = int32(constant.ReadGroupChatType)
	}

	content, err := marshalSignalReq(signalReq)
	if err != nil {
		return nil, err
	}

	if err := s.sendSignalingNotification(ctx, req.UserID, dbInv.InviterUserID, sessionType, dbInv.GroupID, req.OfflinePushInfo, content); err != nil {
		log.ZWarn(ctx, "sendSignalingNotification accept to inviter failed", err, "inviterID", dbInv.InviterUserID)
	}

	// 接受邀请后不删除 invitation：通话仍在进行，双方应被标记为忙线（BusyLineUserIDList）。
	// invitation 的清理由以下路径负责：
	//   - 主动挂断：handleHungUp → DeleteInvitation
	//   - 主叫取消：handleCancel → DeleteInvitation
	//   - 被叫拒绝：handleReject → DeleteInvitation / RemoveInvitee
	//   - 异常中断：MongoDB TTL 索引（expire_at 字段）自动清理

	return &rtc.SignalAcceptResp{
		Token:   token,
		RoomID:  dbInv.RoomID,
		LiveURL: s.config.RpcConfig.LiveKit.ExternalAddress,
	}, nil
}

// handleReject processes a call rejection.
func (s *rtcServer) handleReject(ctx context.Context, req *rtc.SignalRejectReq, signalReq *rtc.SignalReq) (*rtc.SignalRejectResp, error) {
	if req.Invitation == nil {
		return nil, errs.ErrArgs.WrapMsg("invitation is nil")
	}

	dbInv, err := s.db.GetInvitationByRoomID(ctx, req.Invitation.RoomID)
	if err != nil {
		return nil, errs.WrapMsg(err, "invitation not found or expired", "roomID", req.Invitation.RoomID)
	}
	if !datautil.Contain(req.UserID, dbInv.InviteeUserIDList...) {
		return nil, errs.ErrNoPermission.WrapMsg("user not in invitee list", "userID", req.UserID)
	}

	sessionType := int32(constant.SingleChatType)
	if dbInv.GroupID != "" {
		sessionType = int32(constant.ReadGroupChatType)
	}
	content, err := marshalSignalReq(signalReq)
	if err != nil {
		return nil, err
	}
	if err := s.sendSignalingNotification(ctx, req.UserID, dbInv.InviterUserID, sessionType, dbInv.GroupID, req.OfflinePushInfo, content); err != nil {
		log.ZWarn(ctx, "sendSignalingNotification reject to inviter failed", err, "inviterID", dbInv.InviterUserID)
	}

	if dbInv.GroupID != "" {
		if err := s.db.RemoveInvitee(ctx, dbInv.RoomID, req.UserID); err != nil {
			log.ZWarn(ctx, "RemoveInvitee failed", err, "roomID", dbInv.RoomID, "userID", req.UserID)
		}

		// Check whether any participant other than the inviter has actually
		// joined the LiveKit room.  Rejecters never enter LiveKit, so a
		// participant count > 0 (excluding the inviter who waits in the room)
		// means at least one invitee accepted and the call is ongoing.
		// If nobody joined yet (all pending invitees rejected), tear the call
		// down so non-invited members' banners are dismissed promptly instead
		// of waiting for the MongoDB TTL to expire.
		lp, listErr := s.roomClient.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: dbInv.RoomID})
		joinedCount := 0
		if listErr != nil {
			log.ZWarn(ctx, "handleReject: ListParticipants failed", listErr, "roomID", dbInv.RoomID)
		} else {
			for _, p := range lp.Participants {
				if p.GetIdentity() != dbInv.InviterUserID {
					joinedCount++
				}
			}
		}

		if joinedCount > 0 {
			// At least one invitee has already joined; the call continues.
			log.ZInfo(ctx, "handleReject: group call continues", "roomID", dbInv.RoomID, "joinedCount", joinedCount)
			return &rtc.SignalRejectResp{}, nil
		}

		// No one else is in the room — all reachable invitees have rejected.
		// Terminate the call so the "in progress" banner is dismissed for
		// non-invited members.
		if _, err := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: dbInv.RoomID}); err != nil {
			log.ZWarn(ctx, "handleReject: DeleteRoom failed", err, "roomID", dbInv.RoomID)
		}
		if err := s.db.DeleteInvitation(ctx, dbInv.RoomID); err != nil {
			log.ZWarn(ctx, "handleReject: DeleteInvitation failed", err, "roomID", dbInv.RoomID)
		}

		s.sendCallRecordChatMsg(ctx, dbInv, callStatusRejected, 0)

		log.ZInfo(ctx, "lintao handleReject", "dbInv", dbInv)

		go s.broadcastGroupCallStatusToNonInvited(context.WithoutCancel(ctx), dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, dbInv.InviterUserID, dbInv.InviteeUserIDList, GroupCallStatusEnded)

		go s.sendGroupCallEndedNotification(context.WithoutCancel(ctx), dbInv.GroupID, dbInv.InviterUserID, dbInv.MediaType, 0)
	} else {
		if err := s.db.DeleteInvitation(ctx, dbInv.RoomID); err != nil {
			log.ZWarn(ctx, "DeleteInvitation failed", err, "roomID", dbInv.RoomID)
		}

		s.sendCallRecordChatMsg(ctx, dbInv, callStatusRejected, 0)

		log.ZInfo(ctx, "lintao handleReject", "dbInv", dbInv)
	}

	return &rtc.SignalRejectResp{}, nil
}

// handleCancel processes a call cancellation.
func (s *rtcServer) handleCancel(ctx context.Context, req *rtc.SignalCancelReq, signalReq *rtc.SignalReq) (*rtc.SignalCancelResp, error) {
	if req.Invitation == nil {
		return nil, errs.ErrArgs.WrapMsg("invitation is nil")
	}

	dbInv, err := s.db.GetInvitationByRoomID(ctx, req.Invitation.RoomID)
	if err != nil {
		return nil, errs.WrapMsg(err, "invitation not found or expired", "roomID", req.Invitation.RoomID)
	}
	if req.UserID != dbInv.InviterUserID {
		return nil, errs.ErrNoPermission.WrapMsg("only the inviter can cancel", "userID", req.UserID, "inviterUserID", dbInv.InviterUserID)
	}

	sessionType := int32(constant.SingleChatType)
	if dbInv.GroupID != "" {
		sessionType = int32(constant.ReadGroupChatType)
	}
	content, err := marshalSignalReq(signalReq)
	if err != nil {
		return nil, err
	}
	for _, inviteeID := range dbInv.InviteeUserIDList {
		if err := s.sendSignalingNotification(ctx, req.UserID, inviteeID, sessionType, dbInv.GroupID, req.OfflinePushInfo, content); err != nil {
			log.ZWarn(ctx, "sendSignalingNotification cancel to invitee failed", err, "inviteeID", inviteeID)
		}
	}

	if err := s.db.DeleteInvitation(ctx, dbInv.RoomID); err != nil {
		log.ZWarn(ctx, "DeleteInvitation failed", err, "roomID", dbInv.RoomID)
	}

	// For group calls, notify non-invited members that the call was cancelled.
	if dbInv.GroupID != "" {
		go s.broadcastGroupCallStatusToNonInvited(context.WithoutCancel(ctx), dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, dbInv.InviterUserID, dbInv.InviteeUserIDList, GroupCallStatusEnded)
		go s.sendGroupCallEndedNotification(context.WithoutCancel(ctx), dbInv.GroupID, dbInv.InviterUserID, dbInv.MediaType, 0)
	}

	s.sendCallRecordChatMsg(ctx, dbInv, callStatusCancelled, 0)

	log.ZInfo(ctx, "lintao handleCancel", "dbInv", dbInv)

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
		return nil, errs.ErrArgs.WrapMsg("invitation is nil")
	}

	dbInv, err := s.db.GetInvitationByRoomID(ctx, req.Invitation.RoomID)
	if err != nil {
		return nil, errs.WrapMsg(err, "invitation not found or expired", "roomID", req.Invitation.RoomID)
	}
	if req.UserID != dbInv.InviterUserID && !datautil.Contain(req.UserID, dbInv.InviteeUserIDList...) {
		return nil, errs.ErrNoPermission.WrapMsg("user is not a participant of this call", "userID", req.UserID)
	}

	sessionType := int32(constant.SingleChatType)
	if dbInv.GroupID != "" {
		sessionType = int32(constant.ReadGroupChatType)
	}
	content, err := marshalSignalReq(signalReq)
	if err != nil {
		return nil, err
	}
	// Notify peers using the authoritative DB participant list.
	for _, peerID := range hungUpPeerIDsFromDB(dbInv, req.UserID) {
		if err := s.sendSignalingNotification(ctx, req.UserID, peerID, sessionType, dbInv.GroupID, req.OfflinePushInfo, content); err != nil {
			log.ZWarn(ctx, "sendSignalingNotification hungUp to peer failed", err, "peerID", peerID)
		}
	}

	if dbInv.GroupID != "" {
		// Group call: the client disconnects from LiveKit before sending HungUp,
		// so ListParticipants already reflects the post-hangup state.
		// Count participants excluding the user who just hung up (covers the
		// rare race where the client hasn't fully left the LiveKit room yet).
		lp, listErr := s.roomClient.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: dbInv.RoomID})
		remaining := 0
		if listErr != nil {
			log.ZWarn(ctx, "handleHungUp: ListParticipants failed, assuming call ended", listErr, "roomID", dbInv.RoomID)
		} else {
			for _, p := range lp.Participants {
				if p.GetIdentity() != req.UserID {
					remaining++
				}
			}
		}

		if remaining > 0 {
			// Other participants are still in the call; just remove this user
			// from the DB invitee list so they are no longer tracked as busy.
			if datautil.Contain(req.UserID, dbInv.InviteeUserIDList...) {
				if err := s.db.RemoveInvitee(ctx, dbInv.RoomID, req.UserID); err != nil {
					log.ZWarn(ctx, "handleHungUp: RemoveInvitee failed", err, "roomID", dbInv.RoomID, "userID", req.UserID)
				}
			}
			log.ZInfo(ctx, "handleHungUp: group call continues", "roomID", dbInv.RoomID, "remaining", remaining)
			return &rtc.SignalHungUpResp{}, nil
		}
		// remaining == 0: fall through to tear-down logic below.
	}

	// Terminate the LiveKit room (1:1 always; group only when last participant left).
	if _, err := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: dbInv.RoomID}); err != nil {
		log.ZWarn(ctx, "LiveKit DeleteRoom failed", err, "roomID", dbInv.RoomID)
	}

	if err := s.db.DeleteInvitation(ctx, dbInv.RoomID); err != nil {
		log.ZWarn(ctx, "DeleteInvitation failed", err, "roomID", dbInv.RoomID)
	}

	// Notify non-invited group members that the call has ended so they dismiss the banner.
	if dbInv.GroupID != "" {
		go s.broadcastGroupCallStatusToNonInvited(context.WithoutCancel(ctx), dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, dbInv.InviterUserID, dbInv.InviteeUserIDList, GroupCallStatusEnded)
	}

	duration := int64(0)
	if dbInv.InitiateTime > 0 {
		if nowMs := time.Now().UnixMilli(); nowMs > dbInv.InitiateTime {
			duration = (nowMs - dbInv.InitiateTime) / 1000
		}
	}

	s.sendCallRecordChatMsg(ctx, dbInv, callStatusAnswered, duration)

	log.ZInfo(ctx, "lintao handleHungUp", "dbInv", dbInv, "duration", duration)

	// Send a group-chat timeline notification to all members with duration, e.g.
	// "Alice ended an audio/video call (10 minutes 30 seconds)".
	if dbInv.GroupID != "" {
		go s.sendGroupCallEndedNotification(context.WithoutCancel(ctx), dbInv.GroupID, dbInv.InviterUserID, dbInv.MediaType, duration)
	}

	return &rtc.SignalHungUpResp{}, nil
}

// handleGetTokenByRoomID returns a LiveKit token for an existing room.
func (s *rtcServer) handleGetTokenByRoomID(ctx context.Context, req *rtc.SignalGetTokenByRoomIDReq) (*rtc.SignalGetTokenByRoomIDResp, error) {
	return s.getTokenByRoomID(ctx, req)
}

// SignalGetRoomByGroupID returns room information for a group.
func (s *rtcServer) SignalGetRoomByGroupID(ctx context.Context, req *rtc.SignalGetRoomByGroupIDReq) (*rtc.SignalGetRoomByGroupIDResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is empty")
	}
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrArgs.WrapMsg("op user id is empty")
	}
	if _, err := s.groupClient.GetGroupMemberCache(ctx, req.GroupID, opUserID); err != nil {
		return nil, err
	}

	inv, err := s.db.GetInvitationByGroupID(ctx, req.GroupID)
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			return &rtc.SignalGetRoomByGroupIDResp{InCall: false}, nil
		}
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
// 且不写历史、不计未读、不更新会话。离线推送根据 offlinePushInfo 控制，此处不强制关闭。
func signalingMsgOptions() map[string]bool {
	opts := make(map[string]bool, 8)
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
	return opts
}

// sendSignalingNotification sends a SignalingNotification message to a user via the msg service.
// groupID 在 SessionType 为群类型（如 ReadGroupChatType）时必须非空，否则 msg 服务群聊校验会失败。
func (s *rtcServer) sendSignalingNotification(ctx context.Context, sendID, recvID string, sessionType int32, groupID string, offlinePush *sdkws.OfflinePushInfo, content []byte) error {
	now := time.Now().UnixMilli()
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
		Options:     signalingMsgOptions(),
	}
	if offlinePush != nil {
		msgData.OfflinePushInfo = offlinePush
	}

	_, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData})
	if err != nil {
		log.ZError(ctx, "sendSignalingNotification", err, "msgdata", msgData)
		return err
	}
	log.ZInfo(ctx, "sendSignalingNotification", "msgData", msgData)

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

// groupCallNotificationConfig returns notification.yml settings for group call timeline events.
func (s *rtcServer) groupCallNotificationConfig(contentType int32) config.NotificationConfig {
	switch contentType {
	case constant.GroupCallStartedNotification:
		return s.config.NotificationConfig.GroupCallStarted
	case constant.GroupCallEndedNotification:
		return s.config.NotificationConfig.GroupCallEnded
	default:
		return config.NotificationConfig{}
	}
}

// groupCallTimelineMsgOptions builds MsgData.Options from notification.yml and routes
// the message into the group chat timeline (sg_), not the n_ notification session.
func (s *rtcServer) groupCallTimelineMsgOptions(contentType int32) map[string]bool {
	cfg := s.groupCallNotificationConfig(contentType)
	opts := config.GetOptionsByNotification(cfg, nil)
	datautil.SetSwitchFromOptions(opts, constant.IsNotNotification, true)
	return opts
}

func offlinePushInfoFromConfig(cfg config.NotificationConfig) *sdkws.OfflinePushInfo {
	return &sdkws.OfflinePushInfo{
		Title: cfg.OfflinePush.Title,
		Desc:  cfg.OfflinePush.Desc,
		Ex:    cfg.OfflinePush.Ext,
	}
}

// sendGroupCallStartedNotification sends a GroupCallStartedNotification (1522) to the
// group chat timeline so all members see a system message, e.g. "Alice started a video call".
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
		Options:         s.groupCallTimelineMsgOptions(constant.GroupCallStartedNotification),
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

// sendGroupCallEndedNotification sends a GroupCallEndedNotification (1523) to the
// group chat timeline so all members see a system message, e.g.
// "Alice ended an audio/video call (10 minutes 30 seconds)".
// Errors are non-fatal and only logged.
func (s *rtcServer) sendGroupCallEndedNotification(ctx context.Context, groupID, inviterUserID, mediaType string, durationSecs int64) {
	if groupID == "" {
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
		Options:         s.groupCallTimelineMsgOptions(constant.GroupCallEndedNotification),
		OfflinePushInfo: offlinePushInfoFromConfig(notifyCfg),
	}
	if _, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData}); err != nil {
		log.ZWarn(ctx, "sendGroupCallEndedNotification: SendMsg failed", err, "groupID", groupID)
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
		ExpireAt:           now.Add(time.Duration(inv.Timeout+30) * time.Second),
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

const (
	callStatusAnswered     = "answered"
	callStatusCancelled    = "cancelled"
	callStatusRejected     = "rejected"
	callStatusNotConnected = "not_connected"
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
func callRecordMsgOptions() map[string]bool {
	opts := make(map[string]bool, 7)
	datautil.SetSwitchFromOptions(opts, constant.IsNotNotification, true)          // → si_/sg_ chat conversation
	datautil.SetSwitchFromOptions(opts, constant.IsHistory, true)                  // → write to history
	datautil.SetSwitchFromOptions(opts, constant.IsPersistent, true)               // → persist to storage
	datautil.SetSwitchFromOptions(opts, constant.IsUnreadCount, true)              // → increment unread count
	datautil.SetSwitchFromOptions(opts, constant.IsConversationUpdate, true)       // → update conv last message
	datautil.SetSwitchFromOptions(opts, constant.IsSenderConversationUpdate, true) // → update inviter's conv too
	datautil.SetSwitchFromOptions(opts, constant.IsSenderSync, true)               // → sync to inviter's other devices
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
	default:
		return prefix + " 未接通"
	}
}

// sendCallRecordChatMsg sends a Custom (110) chat message to the si_/sg_ conversation
// representing a completed call. Errors are non-fatal and only logged.
//
// For 1v1: SendID=inviterUserID, RecvID=inviteeUserID, SessionType=SingleChatType.
// For group: SendID=inviterUserID, GroupID=groupID, SessionType=ReadGroupChatType.
func (s *rtcServer) sendCallRecordChatMsg(ctx context.Context, inv *model.SignalInvitation, status string, duration int64) {
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
		log.ZWarn(ctx, "lintao sendCallRecordChatMsg: marshal inner failed", err)
		return
	}
	content, err := json.Marshal(map[string]string{
		"data":        string(inner),
		"description": callRecordDescription(inv.MediaType, status, duration),
		"extension":   "",
	})
	if err != nil {
		log.ZWarn(ctx, "lintao sendCallRecordChatMsg: marshal content failed", err)
		return
	}

	now := time.Now().UnixMilli()
	sessionType := int32(constant.SingleChatType)
	recvID := ""
	groupID := ""
	if inv.GroupID != "" {
		sessionType = int32(constant.ReadGroupChatType)
		groupID = inv.GroupID
	} else if len(inv.InviteeUserIDList) > 0 {
		recvID = inv.InviteeUserIDList[0]
	}

	msgData := &sdkws.MsgData{
		SendID:      inv.InviterUserID,
		RecvID:      recvID,
		GroupID:     groupID,
		SessionType: sessionType,
		ContentType: int32(constant.Custom),
		MsgFrom:     int32(constant.SysMsgType),
		Content:     content,
		CreateTime:  now,
		SendTime:    now,
		ServerMsgID: uuid.New().String(),
		ClientMsgID: uuid.New().String(),
		Options:     callRecordMsgOptions(),
	}

	log.ZInfo(ctx, "lintao sendCallRecordChatMsg", "msgData", msgData, "inv", inv)

	if _, err := s.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: msgData}); err != nil {
		log.ZWarn(ctx, "lintao sendCallRecordChatMsg: SendMsg failed", err, "roomID", inv.RoomID, "status", status)
	}
}

// handleTimeout processes a call timeout (no invitee answered within Timeout seconds).
// The inviter's client sends this signal; the server notifies invitees of the missed call,
// tears down the LiveKit room, and writes a "not_connected" call-record to chat history.
func (s *rtcServer) handleTimeout(ctx context.Context, req *rtc.SignalTimeoutReq, signalReq *rtc.SignalReq) (*rtc.SignalTimeoutResp, error) {
	if req.Invitation == nil {
		return nil, errs.ErrArgs.WrapMsg("invitation is nil")
	}

	dbInv, err := s.db.GetInvitationByRoomID(ctx, req.Invitation.RoomID)
	if err != nil {
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
	for _, inviteeID := range dbInv.InviteeUserIDList {
		if err := s.sendSignalingNotification(ctx, req.UserID, inviteeID, sessionType, dbInv.GroupID, req.OfflinePushInfo, content); err != nil {
			log.ZWarn(ctx, "handleTimeout: sendSignalingNotification to invitee failed", err, "inviteeID", inviteeID)
		}
	}

	if _, err := s.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: dbInv.RoomID}); err != nil {
		log.ZWarn(ctx, "handleTimeout: LiveKit DeleteRoom failed", err, "roomID", dbInv.RoomID)
	}
	if err := s.db.DeleteInvitation(ctx, dbInv.RoomID); err != nil {
		log.ZWarn(ctx, "handleTimeout: DeleteInvitation failed", err, "roomID", dbInv.RoomID)
	}

	// For group calls, notify non-invited members that the call timed out.
	if dbInv.GroupID != "" {
		go s.broadcastGroupCallStatusToNonInvited(context.WithoutCancel(ctx), dbInv.GroupID, dbInv.RoomID, dbInv.MediaType, dbInv.InviterUserID, dbInv.InviteeUserIDList, GroupCallStatusEnded)
	}

	s.sendCallRecordChatMsg(ctx, dbInv, callStatusNotConnected, 0)

	log.ZInfo(ctx, "lintao handleTimeout", "dbInv", dbInv)

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
