package api

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/idutil"
)

type walletDualPartyNotifyReq struct {
	SenderUserID   string `json:"senderUserID" binding:"required"`
	ReceiverUserID string `json:"receiverUserID" binding:"required"`
	BizID          string `json:"bizID" binding:"required"`
	ReceiverText   string `json:"receiverText" binding:"required"`
	SenderText     string `json:"senderText" binding:"required"`
	DetailURL      string `json:"detailURL"`
}

type walletExpiredNotifyReq struct {
	SenderUserID string `json:"senderUserID" binding:"required"`
	BizID        string `json:"bizID" binding:"required"`
	Text         string `json:"text" binding:"required"`
	DetailURL    string `json:"detailURL"`
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

func (m *MessageApi) sendWalletActionNotification(
	c *gin.Context,
	recvUserID string,
	contentType int32,
	content apistruct.WalletActionNotificationContent,
) error {
	sendUserID := mcontext.GetOpUserID(c)
	if sendUserID == "" {
		return errs.ErrNoPermission.WrapMsg("op user id is empty")
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
	receiverContent := apistruct.WalletActionNotificationContent{
		Text:           req.ReceiverText,
		BizID:          req.BizID,
		DetailURL:      req.DetailURL,
		SenderUserID:   req.SenderUserID,
		ReceiverUserID: req.ReceiverUserID,
	}
	if err := m.sendWalletActionNotification(c, req.ReceiverUserID, constant.RedPacketClaimNotification, receiverContent); err != nil {
		apiresp.GinError(c, err)
		return
	}
	senderContent := apistruct.WalletActionNotificationContent{
		Text:           req.SenderText,
		BizID:          req.BizID,
		DetailURL:      req.DetailURL,
		SenderUserID:   req.SenderUserID,
		ReceiverUserID: req.ReceiverUserID,
	}
	if err := m.sendWalletActionNotification(c, req.SenderUserID, constant.RedPacketClaimNotification, senderContent); err != nil {
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
	receiverContent := apistruct.WalletActionNotificationContent{
		Text:           req.ReceiverText,
		BizID:          req.BizID,
		DetailURL:      req.DetailURL,
		SenderUserID:   req.SenderUserID,
		ReceiverUserID: req.ReceiverUserID,
	}
	if err := m.sendWalletActionNotification(c, req.ReceiverUserID, constant.TransferReceiveNotification, receiverContent); err != nil {
		apiresp.GinError(c, err)
		return
	}
	senderContent := apistruct.WalletActionNotificationContent{
		Text:           req.SenderText,
		BizID:          req.BizID,
		DetailURL:      req.DetailURL,
		SenderUserID:   req.SenderUserID,
		ReceiverUserID: req.ReceiverUserID,
	}
	if err := m.sendWalletActionNotification(c, req.SenderUserID, constant.TransferReceiveNotification, senderContent); err != nil {
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
	content := apistruct.WalletActionNotificationContent{
		Text:         req.Text,
		BizID:        req.BizID,
		DetailURL:    req.DetailURL,
		SenderUserID: req.SenderUserID,
	}
	if err := m.sendWalletActionNotification(c, req.SenderUserID, constant.RedPacketExpiredNotification, content); err != nil {
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
	content := apistruct.WalletActionNotificationContent{
		Text:         req.Text,
		BizID:        req.BizID,
		DetailURL:    req.DetailURL,
		SenderUserID: req.SenderUserID,
	}
	if err := m.sendWalletActionNotification(c, req.SenderUserID, constant.TransferExpiredNotification, content); err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, nil)
}
