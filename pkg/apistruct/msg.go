// Copyright © 2023 OpenIM. All rights reserved.
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

package apistruct

type PictureBaseInfo struct {
	UUID   string `mapstructure:"uuid"`
	Type   string `mapstructure:"type"   validate:"required"`
	Size   int64  `mapstructure:"size"`
	Width  int32  `mapstructure:"width"  validate:"required"`
	Height int32  `mapstructure:"height" validate:"required"`
	Url    string `mapstructure:"url"    validate:"required"`
}

type PictureElem struct {
	SourcePath      string          `mapstructure:"sourcePath"`
	SourcePicture   PictureBaseInfo `mapstructure:"sourcePicture"   validate:"required"`
	BigPicture      PictureBaseInfo `mapstructure:"bigPicture"      validate:"required"`
	SnapshotPicture PictureBaseInfo `mapstructure:"snapshotPicture" validate:"required"`
}

type SoundElem struct {
	UUID      string `mapstructure:"uuid"`
	SoundPath string `mapstructure:"soundPath"`
	SourceURL string `mapstructure:"sourceUrl" validate:"required"`
	DataSize  int64  `mapstructure:"dataSize"`
	Duration  int64  `mapstructure:"duration"  validate:"required,min=1"`
}

type VideoElem struct {
	VideoPath      string `mapstructure:"videoPath"`
	VideoUUID      string `mapstructure:"videoUUID"`
	VideoURL       string `mapstructure:"videoUrl"       validate:"required"`
	VideoType      string `mapstructure:"videoType"      validate:"required"`
	VideoSize      int64  `mapstructure:"videoSize"      validate:"required"`
	Duration       int64  `mapstructure:"duration"       validate:"required"`
	SnapshotPath   string `mapstructure:"snapshotPath"`
	SnapshotUUID   string `mapstructure:"snapshotUUID"`
	SnapshotSize   int64  `mapstructure:"snapshotSize"`
	SnapshotURL    string `mapstructure:"snapshotUrl"    validate:"required"`
	SnapshotWidth  int32  `mapstructure:"snapshotWidth"  validate:"required"`
	SnapshotHeight int32  `mapstructure:"snapshotHeight" validate:"required"`
}

type FileElem struct {
	FilePath  string `mapstructure:"filePath"`
	UUID      string `mapstructure:"uuid"`
	SourceURL string `mapstructure:"sourceUrl" validate:"required"`
	FileName  string `mapstructure:"fileName"  validate:"required"`
	FileSize  int64  `mapstructure:"fileSize"  validate:"required"`
}
type AtElem struct {
	Text       string   `mapstructure:"text"`
	AtUserList []string `mapstructure:"atUserList" validate:"required,max=1000"`
	IsAtSelf   bool     `mapstructure:"isAtSelf"`
}
type LocationElem struct {
	Description string  `mapstructure:"description"`
	Longitude   float64 `mapstructure:"longitude"   validate:"required"`
	Latitude    float64 `mapstructure:"latitude"    validate:"required"`
}

type CustomElem struct {
	Data        string `mapstructure:"data"        validate:"required"`
	Description string `mapstructure:"description"`
	Extension   string `mapstructure:"extension"`
}

type TextElem struct {
	Content string `json:"content" validate:"required"`
}

type RevokeElem struct {
	RevokeMsgClientID string `mapstructure:"revokeMsgClientID" validate:"required"`
}

const (
	ServiceNotificationSubTypeSecurity = 1 // 安全提醒
	ServiceNotificationSubTypeAccount  = 2 // 账号通知
	ServiceNotificationSubTypeSystem   = 3 // 系统公告
	ServiceNotificationSubTypeUpdate   = 4 // 版本更新
)

const (
	// PaymentTransactionType 钱包通知「类型」展示文案（与 UI 一致）。
	PaymentTransactionTypeTransfer          = "转账"
	PaymentTransactionTypeRedPacket         = "红包"
	PaymentTransactionTypeRedPacketTransfer = "红包/转账"
)

const (
	DefaultNotificationDetailText = "查看详情"
)

// PaymentNotificationAction 钱包通知底部次要操作（如「去赎回」）。
type PaymentNotificationAction struct {
	Text string `json:"text" validate:"required"`
	URL  string `json:"url" validate:"required"`
}

// ServiceNotificationContent SOK 服务通知卡片内容。
// UI：标题 + 正文 + 底部「查看详情」；右上角时间为消息 sendTime，由客户端渲染。
type ServiceNotificationContent struct {
	Title      string `json:"title" validate:"required"`   // 卡片标题，如「版本更新」
	Content    string `json:"content" validate:"required"` // 通知正文
	DetailURL  string `json:"detailURL,omitempty"`         // 「查看详情」跳转链接
	DetailText string `json:"detailText,omitempty"`        // 底部操作文案，默认「查看详情」
	SubType    int32  `json:"subType,omitempty"`           // 可选分类：1安全 2账号 3系统 4版本更新
}

// WalletActionDetailExtra 钱包动作通知详情页所需的业务附加信息。
type WalletActionDetailExtra struct {
	PacketID        string `json:"packetId,omitempty"`        // 红包/转账业务 ID
	ChainID         string `json:"chainId,omitempty"`         // 链 ID，如 ethereum
	ContractAddress string `json:"contractAddress,omitempty"` // 代币合约地址
	SenderName      string `json:"senderName,omitempty"`      // 发送方/领取方昵称
	Amount          string `json:"amount,omitempty"`          // 金额，如 1.00
	TokenSymbol     string `json:"tokenSymbol,omitempty"`     // 代币符号，如 USDT
}

// WalletActionNotificationContent 红包/转账动作通知（领取、接收、过期等文案提示）。
// GroupID 非空表示该动作发生在群聊场景，通知会作为群消息下发到群会话。
type WalletActionNotificationContent struct {
	Text           string                   `json:"text" validate:"required"`
	BizID          string                   `json:"bizID,omitempty"`
	DetailURL      string                   `json:"detailURL,omitempty"`
	DetailRoute    string                   `json:"detailRoute,omitempty"` // 客户端内部详情页路由，如 /redpacket-claim
	DetailExtra    *WalletActionDetailExtra `json:"detailExtra,omitempty"` // 详情页业务附加信息
	SenderUserID   string                   `json:"senderUserID,omitempty"`
	ReceiverUserID string                   `json:"receiverUserID,omitempty"`
	GroupID        string                   `json:"groupID,omitempty"`
}

// PaymentNotificationContent SOK 钱包通知卡片内容。
// UI：标题、金额（大号）、类型/时间/币种明细行，底部「查看详情」及可选次要操作。
type PaymentNotificationContent struct {
	Title           string                     `json:"title" validate:"required"`           // 卡片标题，如「红包/转账过期」「充值」
	Amount          string                     `json:"amount" validate:"required"`          // 金额，含正负号，如 -136.00、156.23
	TransactionType string                     `json:"transactionType" validate:"required"` // 类型：转账 / 红包 / 红包/转账
	TransactionTime string                     `json:"transactionTime" validate:"required"` // 交易时间，如 2026-06-18 17:04:25
	Currency        string                     `json:"currency" validate:"required"`        // 币种，如 USDT
	CurrencyIconURL string                     `json:"currencyIconURL,omitempty"`           // 币种图标 URL
	DetailURL       string                     `json:"detailURL,omitempty"`                 // 「查看详情」跳转链接
	DetailText      string                     `json:"detailText,omitempty"`                // 底部主操作文案，默认「查看详情」
	SecondaryAction *PaymentNotificationAction `json:"secondaryAction,omitempty"`           // 次要操作，如「去赎回」
	OrderNo         string                     `json:"orderNo,omitempty"`                   // 业务单号，详情页使用
	BizID           string                     `json:"bizID,omitempty"`                     // 业务 ID（红包 ID、转账 ID 等）
	ChainID         string                     `json:"chainId,omitempty"`                   // 链 ID，如 ethereum
	PacketID        string                     `json:"packetId,omitempty"`
	ChainKey        string                     `json:"chainKey,omitempty"` // 链 ID，如 ethereum
	SendUserID      string                     `json:"sendUserID" validate:"required"`
	RecvUserID      string                     `json:"recvUserID" validate:"required"`
	GroupID         string                     `json:"groupID,omitempty"`
}

type OANotificationElem struct {
	NotificationName    string       `mapstructure:"notificationName"    json:"notificationName"    validate:"required"`
	NotificationFaceURL string       `mapstructure:"notificationFaceURL" json:"notificationFaceURL"`
	NotificationType    int32        `mapstructure:"notificationType"    json:"notificationType"    validate:"required"`
	Text                string       `mapstructure:"text"                json:"text"                validate:"required"`
	Url                 string       `mapstructure:"url"                 json:"url"`
	MixType             int32        `mapstructure:"mixType"             json:"mixType"             validate:"gte=0,lte=5"`
	PictureElem         *PictureElem `mapstructure:"pictureElem"         json:"pictureElem"`
	SoundElem           *SoundElem   `mapstructure:"soundElem"           json:"soundElem"`
	VideoElem           *VideoElem   `mapstructure:"videoElem"           json:"videoElem"`
	FileElem            *FileElem    `mapstructure:"fileElem"            json:"fileElem"`
	Ex                  string       `mapstructure:"ex"                  json:"ex"`
}
type MessageRevoked struct {
	RevokerID       string `mapstructure:"revokerID"       json:"revokerID"       validate:"required"`
	RevokerRole     int32  `mapstructure:"revokerRole"     json:"revokerRole"     validate:"required"`
	ClientMsgID     string `mapstructure:"clientMsgID"     json:"clientMsgID"     validate:"required"`
	RevokerNickname string `mapstructure:"revokerNickname" json:"revokerNickname"`
	SessionType     int32  `mapstructure:"sessionType"     json:"sessionType"     validate:"required"`
	Seq             uint32 `mapstructure:"seq"             json:"seq"             validate:"required"`
}
