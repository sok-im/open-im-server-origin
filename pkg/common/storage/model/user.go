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

package model

import (
	"time"
)

// PhoneVisibility 手机号可见性枚举。
// 0=所有人可见, 1=仅好友可见, 2=完全隐藏
const (
	PhoneVisibilityPublic  int32 = 0
	PhoneVisibilityFriends int32 = 1
	PhoneVisibilityHidden  int32 = 2
)

// CallAcceptSetting 音视频通话接受权限枚举。
// 0=所有人可发起, 1=仅好友可发起, 2=不接受任何通话
const (
	CallAcceptSettingPublic  int32 = 0
	CallAcceptSettingFriends int32 = 1
	CallAcceptSettingNobody  int32 = 2
)

// MsgReceiveSetting 会话消息接收权限枚举。
// 0=所有人可发送, 1=仅好友可发送, 2=所有人不可发送
const (
	MsgReceiveSettingPublic  int32 = 0
	MsgReceiveSettingFriends int32 = 1
	MsgReceiveSettingNobody  int32 = 2
)

// GroupInviteSetting 群邀请权限枚举。
// 0=所有人可邀请, 1=仅好友可邀请, 2=所有人不可邀请
const (
	GroupInviteSettingPublic  int32 = 0
	GroupInviteSettingFriends int32 = 1
	GroupInviteSettingNobody  int32 = 2
)

// UserStatus 用户账号状态枚举。
// 0=正常；1=冻结（可登录，不能收发消息）；2=黑名单（不可登录，自动踢下线，不能收发消息）
const (
	UserStatusNormal    int32 = 0
	UserStatusFrozen    int32 = 1
	UserStatusBlacklist int32 = 2
)

// DefaultDeleteAccountIntervalSec 删除账号等待间隔的系统默认值（18 个月，按 30 天/月折算）。
// 注册时写入 MongoDB 的初始值；用户未显式修改时即为此值。
const DefaultDeleteAccountIntervalSec int32 = 18 * 30 * 24 * 3600

// 用户通知开关存储值：0=未设置（读取时视为打开），1=打开，2=关闭。
const (
	NotificationSwitchUnset int32 = 0
	NotificationSwitchOn    int32 = 1
	NotificationSwitchOff   int32 = 2
)

// NotificationSwitchToBool 将存储值转为开关状态；未设置时默认打开。
func NotificationSwitchToBool(v int32) bool {
	return v != NotificationSwitchOff
}

// BoolToNotificationSwitch 将开关状态转为存储值。
func BoolToNotificationSwitch(enable bool) int32 {
	if enable {
		return NotificationSwitchOn
	}
	return NotificationSwitchOff
}

type User struct {
	UserID             string    `bson:"user_id"`
	Nickname           string    `bson:"nickname"`
	FaceURL            string    `bson:"face_url"`
	Ex                 string    `bson:"ex"`
	AppMangerLevel     int32     `bson:"app_manger_level"`
	GlobalRecvMsgOpt   int32     `bson:"global_recv_msg_opt"`
	CreateTime         time.Time `bson:"create_time"`
	FirstName          string    `bson:"first_name"`
	LastName           string    `bson:"last_name"`
	FullName           string    `bson:"full_name"`
	Phone              string    `bson:"phone"`
	AreaCode           string    `bson:"area_code"`
	PhoneVisibility    int32     `bson:"phone_visibility"`
	CallAcceptSetting  int32     `bson:"call_accept_setting"`
	MsgReceiveSetting  int32     `bson:"msg_receive_setting"`
	GroupInviteSetting int32     `bson:"group_invite_setting"`
	// CallRingtoneURL 用户自定义来电铃声 URL；对方来电时播放此铃声
	CallRingtoneURL string `bson:"call_ringtone_url"`
	// CallRingtoneName 用户自定义来电铃声名称（展示用）
	CallRingtoneName string `bson:"call_ringtone_name"`
	// CallRingtoneCover 用户自定义来电铃声封面 URL
	CallRingtoneCover string `bson:"call_ringtone_cover"`
	// CallRingtoneAuthor 用户自定义来电铃声作者
	CallRingtoneAuthor string `bson:"call_ringtone_author"`
	// Status 账号状态：0=正常，1=冻结，2=黑名单
	Status int32 `bson:"status"`
	// MsgBurnDuration 用户全局消息阅后即焚时长（秒）；0 表示关闭
	MsgBurnDuration int32 `bson:"msg_burn_duration"`
	// DeleteAccountInterval 删除账号间隔（秒）；0 表示关闭
	DeleteAccountInterval int32 `bson:"delete_account_interval"`
	// Language 用户应用语言（如 zh-CN、en-US）
	Language string `bson:"language"`
	// MsgNotification 消息通知开关
	MsgNotification int32 `bson:"msg_notification"`
	// SokimPaymentNotification sokim 支付通知开关
	SokimPaymentNotification int32 `bson:"sokim_payment_notification"`
	// SokimServiceNotification sokim 服务通知开关
	SokimServiceNotification int32 `bson:"sokim_service_notification"`
	// AvNotification 音视频通知开关
	AvNotification int32 `bson:"av_notification"`
	// AvCallRingtone 音视频来电铃声开关
	AvCallRingtone int32 `bson:"av_call_ringtone"`
	// PlayCalleeRingtoneOnAnswer 接听时播放对方铃声开关
	PlayCalleeRingtoneOnAnswer int32 `bson:"play_callee_ringtone_on_answer"`
	// WelcomeNotificationSent 首次上线欢迎服务号是否已发送（一次性标记，保证欢迎语每人仅发一次）
	WelcomeNotificationSent bool `bson:"welcome_notification_sent"`
}

func (u *User) GetNickname() string {
	return u.Nickname
}

func (u *User) GetFaceURL() string {
	return u.FaceURL
}

func (u *User) GetUserID() string {
	return u.UserID
}

func (u *User) GetEx() string {
	return u.Ex
}
