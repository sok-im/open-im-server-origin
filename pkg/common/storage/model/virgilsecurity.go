// Copyright © 2026 OpenIM. All rights reserved.
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

import "time"

// VirgilDevice 1v1 E2EE 设备目录条目，绑定业务 userID 与 Virgil cardID。
// 与 CryptoDevice 区分：本表服务于 virgil-1v1-e2ee-design 的对象模型，
// 必须包含 cardID 字段并支持 device_changed 事件广播。
type VirgilDevice struct {
	UserID         string    `bson:"user_id"`
	DeviceID       string    `bson:"device_id"`
	CardID         string    `bson:"card_id"`
	Platform       string    `bson:"platform"`
	Status         string    `bson:"status"`
	ClientVersion  string    `bson:"client_version"`
	LastSeenAt     time.Time `bson:"last_seen_at"`
	CreateTime     time.Time `bson:"create_time"`
	UpdateTime     time.Time `bson:"update_time"`
	RevokedAt      time.Time `bson:"revoked_at,omitempty"`
	RevokeReason   string    `bson:"revoke_reason,omitempty"`
	IdempotencyKey string    `bson:"idempotency_key,omitempty"`
}

// VirgilUserDeviceVersion 缓存每个用户设备目录的版本号，用于 device_changed 事件序列化。
type VirgilUserDeviceVersion struct {
	UserID  string `bson:"user_id"`
	Version int64  `bson:"version"`
}

// VirgilDeviceEvent 设备目录变更事件，供 SubscribeEvents 长轮询消费。
type VirgilDeviceEvent struct {
	EventID    string    `bson:"event_id"`
	UserID     string    `bson:"user_id"`
	Version    int64     `bson:"version"`
	Type       string    `bson:"type"`
	DeviceID   string    `bson:"device_id"`
	Status     string    `bson:"status"`
	CreateTime time.Time `bson:"create_time"`
}

// VirgilConversation 1v1 稳定会话 ID 映射。
type VirgilConversation struct {
	ConversationID string    `bson:"conversation_id"`
	UserA          string    `bson:"user_a"`
	UserB          string    `bson:"user_b"`
	CreateTime     time.Time `bson:"create_time"`
}

// VirgilFileRef 已签发的加密文件上传/下载凭据元数据。
// 仅保存可观测元数据，不保存任何密钥或明文内容。
type VirgilFileRef struct {
	FileRefID      string    `bson:"file_ref_id"`
	OwnerUserID    string    `bson:"owner_user_id"`
	OwnerDeviceID  string    `bson:"owner_device_id"`
	ConversationID string    `bson:"conversation_id"`
	Size           int64     `bson:"size"`
	ContentType    string    `bson:"content_type"`
	ObjectKey      string    `bson:"object_key"`
	ExpiresAt      time.Time `bson:"expires_at"`
	CreateTime     time.Time `bson:"create_time"`
}
