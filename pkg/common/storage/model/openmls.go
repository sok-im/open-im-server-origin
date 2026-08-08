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

// MLSKeyPackage 存储设备上传的 MLS KeyPackage，每个 KP 一次性消费。
// 集合名：mls_key_package
type MLSKeyPackage struct {
	KpID           string    `bson:"kp_id"`
	UserID         string    `bson:"user_id"`
	DeviceID       string    `bson:"device_id"`
	Platform       string    `bson:"platform"`
	KeyPackage     string    `bson:"key_package"` // base64 TLS-serialized KeyPackage
	Ciphersuite    string    `bson:"ciphersuite"`
	CredentialType string    `bson:"credential_type"`
	Consumed       bool      `bson:"consumed"`
	CreatedAt      time.Time `bson:"created_at"`
	ExpiresAt      time.Time `bson:"expires_at"`
}

// MLSGroupState 记录每个 MLS Group 的当前状态（epoch 等）。
// 集合名：mls_group_state
type MLSGroupState struct {
	GroupID      string    `bson:"group_id"`
	CurrentEpoch uint64    `bson:"current_epoch"`
	MemberCount  int32     `bson:"member_count"`
	LastCommitAt time.Time `bson:"last_commit_at"`
	CreatedAt    time.Time `bson:"created_at"`
}

// MLSCommit 记录每个 Group 提交的 Commit 历史，用于断线重连时追赶 epoch。
// 集合名：mls_commit
type MLSCommit struct {
	ID             string    `bson:"_id"`
	GroupID        string    `bson:"group_id"`
	Epoch          uint64    `bson:"epoch"`
	FromEpoch      uint64    `bson:"from_epoch"`
	CommitHash     string    `bson:"commit_hash,omitempty"`
	IdempotencyKey string    `bson:"idempotency_key,omitempty"`
	SequenceNumber int64     `bson:"sequence_number"`
	CommitMessage  string    `bson:"commit_message"` // base64 TLS-serialized MLSMessage(Commit)
	SenderUserID   string    `bson:"sender_user_id"`
	SenderDeviceID string    `bson:"sender_device_id"`
	CreatedAt      time.Time `bson:"created_at"`
}
