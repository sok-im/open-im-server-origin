package api

import (
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

func buildPaymentNotification(sendUserID, recvUserID string, content apistruct.PaymentNotificationContent) *model.PaymentNotification {
	doc := &model.PaymentNotification{
		SendUserID:        sendUserID,
		RecvUserID:        recvUserID,
		Title:             content.Title,
		Amount:            content.Amount,
		TransactionType:   content.TransactionType,
		TransactionTime:   content.TransactionTime,
		Currency:          content.Currency,
		CurrencyIconURL:   content.CurrencyIconURL,
		DetailURL:         content.DetailURL,
		DetailText:        content.DetailText,
		OrderNo:           content.OrderNo,
		BizID:             content.BizID,
		ChainID:           content.ChainID,
		PacketID:          content.PacketID,
		ChainKey:          content.ChainKey,
		GroupID:           content.GroupID,
		ContentSendUserID: content.SendUserID,
		ContentRecvUserID: content.RecvUserID,
		CreateTime:        time.Now(),
	}
	if content.SecondaryAction != nil {
		doc.SecondaryAction = &model.PaymentNotificationAction{
			Text: content.SecondaryAction.Text,
			URL:  content.SecondaryAction.URL,
		}
	}
	return doc
}
