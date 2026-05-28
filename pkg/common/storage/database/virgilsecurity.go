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

package database

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

// VirgilDevice 维护用户设备/Card 目录。
type VirgilDevice interface {
	Upsert(ctx context.Context, d *model.VirgilDevice) error
	FindByIdempotency(ctx context.Context, userID, key string) (*model.VirgilDevice, error)
	FindByUserID(ctx context.Context, userID string, includeRevoked bool) ([]*model.VirgilDevice, error)
	FindOne(ctx context.Context, userID, deviceID string) (*model.VirgilDevice, error)
	UpdateStatus(ctx context.Context, userID, deviceID, status, reason string) error
	UpdateLastSeen(ctx context.Context, userID, deviceID string) error
}

// VirgilUserDeviceVersion 用户设备目录版本号。
type VirgilUserDeviceVersion interface {
	IncrVersion(ctx context.Context, userID string) (int64, error)
	GetVersion(ctx context.Context, userID string) (int64, error)
}

// VirgilDeviceEvent 设备变更事件流，供长轮询订阅。
type VirgilDeviceEvent interface {
	Append(ctx context.Context, ev *model.VirgilDeviceEvent) error
	FindSince(ctx context.Context, userID string, sinceVersion int64, limit int64) ([]*model.VirgilDeviceEvent, error)
}

// VirgilConversation 1v1 稳定会话 ID 目录。
type VirgilConversation interface {
	EnsureOneToOne(ctx context.Context, userA, userB string) (*model.VirgilConversation, error)
}

// VirgilFileRef 加密文件上传凭据元数据。
type VirgilFileRef interface {
	Create(ctx context.Context, ref *model.VirgilFileRef) error
}
