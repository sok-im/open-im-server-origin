package api

import (
	"testing"

	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPaymentNotification_MapsTopLevelAndContent(t *testing.T) {
	content := apistruct.PaymentNotificationContent{
		Title:           "充值",
		Amount:          "10.00",
		TransactionType: apistruct.PaymentTransactionTypeTransfer,
		TransactionTime: "2026-06-18 17:04:25",
		Currency:        "USDT",
		CurrencyIconURL: "https://icon",
		DetailURL:       "https://detail",
		DetailText:      "查看详情",
		SecondaryAction: &apistruct.PaymentNotificationAction{Text: "去赎回", URL: "https://redeem"},
		OrderNo:         "ord-1",
		BizID:           "biz-1",
		ChainID:         "1",
		PacketID:        "pkt-1",
		ChainKey:        "ethereum",
		SendUserID:      "biz_sender",
		RecvUserID:      "biz_receiver",
		GroupID:         "g1",
	}

	doc := buildPaymentNotification("notify_bot", "u1", content)
	require.NotNil(t, doc)
	assert.Equal(t, "notify_bot", doc.SendUserID)
	assert.Equal(t, "u1", doc.RecvUserID)
	assert.Equal(t, "充值", doc.Title)
	assert.Equal(t, "10.00", doc.Amount)
	assert.Equal(t, apistruct.PaymentTransactionTypeTransfer, doc.TransactionType)
	assert.Equal(t, "2026-06-18 17:04:25", doc.TransactionTime)
	assert.Equal(t, "USDT", doc.Currency)
	assert.Equal(t, "https://icon", doc.CurrencyIconURL)
	assert.Equal(t, "https://detail", doc.DetailURL)
	assert.Equal(t, "查看详情", doc.DetailText)
	require.NotNil(t, doc.SecondaryAction)
	assert.Equal(t, "去赎回", doc.SecondaryAction.Text)
	assert.Equal(t, "https://redeem", doc.SecondaryAction.URL)
	assert.Equal(t, "ord-1", doc.OrderNo)
	assert.Equal(t, "biz-1", doc.BizID)
	assert.Equal(t, "1", doc.ChainID)
	assert.Equal(t, "pkt-1", doc.PacketID)
	assert.Equal(t, "ethereum", doc.ChainKey)
	assert.Equal(t, "g1", doc.GroupID)
	assert.Equal(t, "biz_sender", doc.ContentSendUserID)
	assert.Equal(t, "biz_receiver", doc.ContentRecvUserID)
	assert.False(t, doc.CreateTime.IsZero())
}

func TestBuildPaymentNotification_NilSecondaryAction(t *testing.T) {
	doc := buildPaymentNotification("s", "r", apistruct.PaymentNotificationContent{
		Title: "t", Amount: "1", TransactionType: "转账",
		TransactionTime: "t", Currency: "USDT",
	})
	require.NotNil(t, doc)
	assert.Nil(t, doc.SecondaryAction)
}
