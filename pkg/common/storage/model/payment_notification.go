package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type PaymentNotificationAction struct {
	Text string `bson:"text"`
	URL  string `bson:"url"`
}

type PaymentNotification struct {
	ID                primitive.ObjectID         `bson:"_id,omitempty"`
	SendUserID        string                     `bson:"send_user_id"`
	RecvUserID        string                     `bson:"recv_user_id"`
	Title             string                     `bson:"title"`
	Amount            string                     `bson:"amount"`
	TransactionType   string                     `bson:"transaction_type"`
	TransactionTime   string                     `bson:"transaction_time"`
	Currency          string                     `bson:"currency"`
	CurrencyIconURL   string                     `bson:"currency_icon_url,omitempty"`
	DetailURL         string                     `bson:"detail_url,omitempty"`
	DetailText        string                     `bson:"detail_text,omitempty"`
	SecondaryAction   *PaymentNotificationAction `bson:"secondary_action,omitempty"`
	OrderNo           string                     `bson:"order_no,omitempty"`
	BizID             string                     `bson:"biz_id,omitempty"`
	ChainID           string                     `bson:"chain_id,omitempty"`
	PacketID          string                     `bson:"packet_id,omitempty"`
	ChainKey          string                     `bson:"chain_key,omitempty"`
	GroupID           string                     `bson:"group_id,omitempty"`
	ContentSendUserID string                     `bson:"content_send_user_id,omitempty"`
	ContentRecvUserID string                     `bson:"content_recv_user_id,omitempty"`
	CreateTime        time.Time                  `bson:"create_time"`
}
