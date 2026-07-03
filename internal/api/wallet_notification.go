package api

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/idutil"
)

type walletDualPartyNotifyReq struct {
	SenderUserID   string                             `json:"senderUserID" binding:"required"`
	ReceiverUserID string                             `json:"receiverUserID" binding:"required"`
	BizID          string                             `json:"bizID" binding:"required"`
	ReceiverText   string                             `json:"receiverText" binding:"required"`
	SenderText     string                             `json:"senderText" binding:"required"`
	DetailURL      string                             `json:"detailURL"`
	DetailRoute    string                             `json:"detailRoute"`
	DetailExtra    *apistruct.WalletActionDetailExtra `json:"detailExtra"`
	GroupID        string                             `json:"groupID"`
	GroupText      string                             `json:"groupText"`
}

type walletExpiredNotifyReq struct {
	SenderUserID string                             `json:"senderUserID" binding:"required"`
	BizID        string                             `json:"bizID" binding:"required"`
	Text         string                             `json:"text" binding:"required"`
	DetailURL    string                             `json:"detailURL"`
	DetailRoute  string                             `json:"detailRoute"`
	DetailExtra  *apistruct.WalletActionDetailExtra `json:"detailExtra"`
	GroupID      string                             `json:"groupID"`
	GroupText    string                             `json:"groupText"`
}

func (m *MessageApi) requireWalletNotifyParticipant(c *gin.Context, senderUserID, receiverUserID string) error {
	opUserID := mcontext.GetOpUserID(c)
	if opUserID == "" {
		return errs.ErrNoPermission.WrapMsg("op user id is empty")
	}
	/*
		if opUserID != senderUserID && opUserID != receiverUserID {
			return errs.ErrNoPermission.WrapMsg("only sender or receiver can send wallet action notification")
		}
	*/
	return nil
}

func (m *MessageApi) requireWalletNotifySender(c *gin.Context, senderUserID string) error {
	opUserID := mcontext.GetOpUserID(c)
	if opUserID == "" {
		return errs.ErrNoPermission.WrapMsg("op user id is empty")
	}
	/*
		if opUserID != senderUserID {
			return errs.ErrNoPermission.WrapMsg("only sender can send wallet expired notification")
		}
	*/
	return nil
}

// resolveWalletSenderName 解析发送方展示名：
// 若 viewer 与 sender 为好友，优先 remark > 好友备注名(friendFirstName+friendLastName)；
// 否则使用用户资料 FirstName+LastName（再退化到 nickname）。
func (m *MessageApi) resolveWalletSenderName(c *gin.Context, viewerUserID, senderUserID string) string {
	if senderUserID == "" {
		return ""
	}
	var alias convert.FriendAliasInfo
	if viewerUserID != "" && m.relationClient != nil {
		if friendInfos, err := m.relationClient.GetFriendsInfo(c, viewerUserID, []string{senderUserID}); err == nil && len(friendInfos) > 0 && friendInfos[0] != nil {
			alias = convert.FriendAliasInfo{
				Remark:          friendInfos[0].GetRemark(),
				FriendFirstName: friendInfos[0].GetFirstName(),
				FriendLastName:  friendInfos[0].GetLastName(),
			}
		}
	}
	var user *sdkws.UserInfo
	if users, err := m.userClient.GetUsersInfo(c, []string{senderUserID}); err == nil && len(users) > 0 {
		user = users[0]
	}
	return convert.DisplayNicknameForFriend(alias.Remark, alias.FriendFirstName, alias.FriendLastName, user)
}

// fillWalletSenderName 当 detailExtra.SenderName 为空时，按发送方解析并回填展示名。
func (m *MessageApi) fillWalletSenderName(c *gin.Context, detailExtra *apistruct.WalletActionDetailExtra, viewerUserID, senderUserID string) {
	if detailExtra == nil || detailExtra.SenderName != "" {
		return
	}
	detailExtra.SenderName = m.resolveWalletSenderName(c, viewerUserID, senderUserID)
}

func (m *MessageApi) sendWalletActionNotification(
	c *gin.Context,
	senderUserID string,
	recvUserID string,
	contentType int32,
	content apistruct.WalletActionNotificationContent,
) error {
	sendUserID := senderUserID
	if sendUserID == "" {
		return errs.ErrArgs.WrapMsg("senderUserID is empty")
	}
	sendMsgReq, err := m.buildNotificationChatSendMsgReq(
		sendUserID,
		recvUserID,
		contentType,
		content,
		idutil.GetMsgIDByMD5(sendUserID+recvUserID+content.BizID+fmt.Sprint(contentType)),
		&sdkws.OfflinePushInfo{Title: "SOK", Desc: content.Text},
	)
	if err != nil {
		return err
	}
	if _, err := m.Client.SendMsg(c, sendMsgReq); err != nil {
		return err
	}
	return nil
}

func (m *MessageApi) sendWalletGroupActionNotification(
	c *gin.Context,
	groupID string,
	contentType int32,
	content apistruct.WalletActionNotificationContent,
) error {
	sendUserID := content.SenderUserID
	if sendUserID == "" {
		return errs.ErrArgs.WrapMsg("senderUserID is empty")
	}
	sendMsgReq, err := m.buildGroupNotificationSendMsgReq(
		sendUserID,
		groupID,
		contentType,
		content,
		idutil.GetMsgIDByMD5(sendUserID+groupID+content.BizID+fmt.Sprint(contentType)),
		&sdkws.OfflinePushInfo{Title: "SOK", Desc: content.Text},
	)
	if err != nil {
		return err
	}
	if _, err := m.Client.SendMsg(c, sendMsgReq); err != nil {
		return err
	}
	return nil
}

func (m *MessageApi) NotifyRedPacketClaimed(c *gin.Context) {
	var req walletDualPartyNotifyReq
	if err := c.BindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WithDetail(err.Error()).Wrap())
		return
	}
	if err := m.requireWalletNotifyParticipant(c, req.SenderUserID, req.ReceiverUserID); err != nil {
		apiresp.GinError(c, err)
		return
	}
	m.fillWalletSenderName(c, req.DetailExtra, req.ReceiverUserID, req.SenderUserID)
	if req.GroupID != "" {
		if req.GroupText == "" {
			apiresp.GinError(c, errs.ErrArgs.WrapMsg("groupText is required when groupID is set"))
			return
		}
		groupContent := apistruct.WalletActionNotificationContent{
			Text:           req.GroupText,
			BizID:          req.BizID,
			DetailURL:      req.DetailURL,
			DetailRoute:    req.DetailRoute,
			DetailExtra:    req.DetailExtra,
			SenderUserID:   req.SenderUserID,
			ReceiverUserID: req.ReceiverUserID,
			GroupID:        req.GroupID,
		}
		if err := m.sendWalletGroupActionNotification(c, req.GroupID, constant.RedPacketClaimNotification, groupContent); err != nil {
			apiresp.GinError(c, err)
			return
		}
		apiresp.GinSuccess(c, nil)
		return
	}
	receiverContent := apistruct.WalletActionNotificationContent{
		Text:           req.ReceiverText,
		BizID:          req.BizID,
		DetailURL:      req.DetailURL,
		DetailRoute:    req.DetailRoute,
		DetailExtra:    req.DetailExtra,
		SenderUserID:   req.SenderUserID,
		ReceiverUserID: req.ReceiverUserID,
	}
	if err := m.sendWalletActionNotification(c, req.SenderUserID, req.ReceiverUserID, constant.RedPacketClaimNotification, receiverContent); err != nil {
		apiresp.GinError(c, err)
		return
	}
	senderContent := apistruct.WalletActionNotificationContent{
		Text:           req.SenderText,
		BizID:          req.BizID,
		DetailURL:      req.DetailURL,
		DetailRoute:    req.DetailRoute,
		DetailExtra:    req.DetailExtra,
		SenderUserID:   req.SenderUserID,
		ReceiverUserID: req.ReceiverUserID,
	}
	if err := m.sendWalletActionNotification(c, req.SenderUserID, req.SenderUserID, constant.RedPacketClaimNotification, senderContent); err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, nil)
}

func (m *MessageApi) NotifyTransferReceived(c *gin.Context) {
	var req walletDualPartyNotifyReq
	if err := c.BindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WithDetail(err.Error()).Wrap())
		return
	}
	if err := m.requireWalletNotifyParticipant(c, req.SenderUserID, req.ReceiverUserID); err != nil {
		apiresp.GinError(c, err)
		return
	}
	m.fillWalletSenderName(c, req.DetailExtra, req.ReceiverUserID, req.SenderUserID)
	if req.GroupID != "" {
		if req.GroupText == "" {
			apiresp.GinError(c, errs.ErrArgs.WrapMsg("groupText is required when groupID is set"))
			return
		}
		groupContent := apistruct.WalletActionNotificationContent{
			Text:           req.GroupText,
			BizID:          req.BizID,
			DetailURL:      req.DetailURL,
			DetailRoute:    req.DetailRoute,
			DetailExtra:    req.DetailExtra,
			SenderUserID:   req.SenderUserID,
			ReceiverUserID: req.ReceiverUserID,
			GroupID:        req.GroupID,
		}
		if err := m.sendWalletGroupActionNotification(c, req.GroupID, constant.TransferReceiveNotification, groupContent); err != nil {
			apiresp.GinError(c, err)
			return
		}
		apiresp.GinSuccess(c, nil)
		return
	}
	receiverContent := apistruct.WalletActionNotificationContent{
		Text:           req.ReceiverText,
		BizID:          req.BizID,
		DetailURL:      req.DetailURL,
		DetailRoute:    req.DetailRoute,
		DetailExtra:    req.DetailExtra,
		SenderUserID:   req.SenderUserID,
		ReceiverUserID: req.ReceiverUserID,
	}
	if err := m.sendWalletActionNotification(c, req.SenderUserID, req.ReceiverUserID, constant.TransferReceiveNotification, receiverContent); err != nil {
		apiresp.GinError(c, err)
		return
	}
	senderContent := apistruct.WalletActionNotificationContent{
		Text:           req.SenderText,
		BizID:          req.BizID,
		DetailURL:      req.DetailURL,
		DetailRoute:    req.DetailRoute,
		DetailExtra:    req.DetailExtra,
		SenderUserID:   req.SenderUserID,
		ReceiverUserID: req.ReceiverUserID,
	}
	if err := m.sendWalletActionNotification(c, req.SenderUserID, req.SenderUserID, constant.TransferReceiveNotification, senderContent); err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, nil)
}

func (m *MessageApi) NotifyRedPacketExpired(c *gin.Context) {
	var req walletExpiredNotifyReq
	if err := c.BindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WithDetail(err.Error()).Wrap())
		return
	}
	if err := m.requireWalletNotifySender(c, req.SenderUserID); err != nil {
		apiresp.GinError(c, err)
		return
	}
	m.fillWalletSenderName(c, req.DetailExtra, "", req.SenderUserID)
	if req.GroupID != "" {
		if req.GroupText == "" {
			apiresp.GinError(c, errs.ErrArgs.WrapMsg("groupText is required when groupID is set"))
			return
		}
		groupContent := apistruct.WalletActionNotificationContent{
			Text:         req.GroupText,
			BizID:        req.BizID,
			DetailURL:    req.DetailURL,
			DetailRoute:  req.DetailRoute,
			DetailExtra:  req.DetailExtra,
			SenderUserID: req.SenderUserID,
			GroupID:      req.GroupID,
		}
		if err := m.sendWalletGroupActionNotification(c, req.GroupID, constant.RedPacketExpiredNotification, groupContent); err != nil {
			apiresp.GinError(c, err)
			return
		}
		apiresp.GinSuccess(c, nil)
		return
	}
	content := apistruct.WalletActionNotificationContent{
		Text:         req.Text,
		BizID:        req.BizID,
		DetailURL:    req.DetailURL,
		DetailRoute:  req.DetailRoute,
		DetailExtra:  req.DetailExtra,
		SenderUserID: req.SenderUserID,
	}
	if err := m.sendWalletActionNotification(c, req.SenderUserID, req.SenderUserID, constant.RedPacketExpiredNotification, content); err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, nil)
}

func (m *MessageApi) NotifyTransferExpired(c *gin.Context) {
	var req walletExpiredNotifyReq
	if err := c.BindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WithDetail(err.Error()).Wrap())
		return
	}
	if err := m.requireWalletNotifySender(c, req.SenderUserID); err != nil {
		apiresp.GinError(c, err)
		return
	}
	m.fillWalletSenderName(c, req.DetailExtra, "", req.SenderUserID)
	if req.GroupID != "" {
		if req.GroupText == "" {
			apiresp.GinError(c, errs.ErrArgs.WrapMsg("groupText is required when groupID is set"))
			return
		}
		groupContent := apistruct.WalletActionNotificationContent{
			Text:         req.GroupText,
			BizID:        req.BizID,
			DetailURL:    req.DetailURL,
			DetailRoute:  req.DetailRoute,
			DetailExtra:  req.DetailExtra,
			SenderUserID: req.SenderUserID,
			GroupID:      req.GroupID,
		}
		if err := m.sendWalletGroupActionNotification(c, req.GroupID, constant.TransferExpiredNotification, groupContent); err != nil {
			apiresp.GinError(c, err)
			return
		}
		apiresp.GinSuccess(c, nil)
		return
	}
	content := apistruct.WalletActionNotificationContent{
		Text:         req.Text,
		BizID:        req.BizID,
		DetailURL:    req.DetailURL,
		DetailRoute:  req.DetailRoute,
		DetailExtra:  req.DetailExtra,
		SenderUserID: req.SenderUserID,
	}
	if err := m.sendWalletActionNotification(c, req.SenderUserID, req.SenderUserID, constant.TransferExpiredNotification, content); err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, nil)
}
