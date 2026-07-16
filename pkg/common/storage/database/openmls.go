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

// MLSKeyPackageDatabase 管理设备上传的 KeyPackage，支持原子消费（一次性使用）。
type MLSKeyPackageDatabase interface {
	Insert(ctx context.Context, kp *model.MLSKeyPackage) error
	BatchInsert(ctx context.Context, kps []*model.MLSKeyPackage) error
	// ConsumeByUserID 原子性地标记 KP 为已消费并返回这批 KP。
	// excludeDeviceID 非空时跳过该设备的 KP（调用方自己的设备）。
	// countPerDevice 为 0 时每台设备取 1 份。
	ConsumeByUserID(ctx context.Context, userID, excludeDeviceID string, countPerDevice int) ([]*model.MLSKeyPackage, error)
	// CountByUserID 统计各设备未消费 KP 数量，key=deviceID, value=count。
	CountByUserID(ctx context.Context, userID string) (map[string]int32, error)
	// CountByDevice 统计指定设备未消费 KP 数量。
	CountByDevice(ctx context.Context, userID, deviceID string) (int32, error)
}

// MLSGroupDatabase 管理 MLS Group 状态，提供 epoch 乐观并发控制。
type MLSGroupDatabase interface {
	// UpsertState 创建或更新 Group 状态（用于首次创建群组）。
	UpsertState(ctx context.Context, state *model.MLSGroupState) error
	GetState(ctx context.Context, groupID string) (*model.MLSGroupState, error)
	DeleteGroup(ctx context.Context, groupID string) error
	// IncrEpoch 乐观锁推进 epoch：仅当当前 epoch == fromEpoch 时才成功，
	// 返回新 epoch；若 epoch 不匹配则返回 errs.ErrRecordNotFound。
	IncrEpoch(ctx context.Context, groupID string, fromEpoch uint64) (uint64, error)
	// UpdateMemberCount 更新 Group 成员数量（在 Commit 后同步真实成员数）。
	UpdateMemberCount(ctx context.Context, groupID string, memberCount int32) error
}

// MLSCommitDatabase 追加和查询 Commit 历史。
type MLSCommitDatabase interface {
	AppendCommit(ctx context.Context, c *model.MLSCommit) error
	FindByIdempotencyKey(ctx context.Context, key string) (*model.MLSCommit, error)
	// FindSinceEpoch 返回 epoch > sinceEpoch 的 Commit，按 epoch 升序，最多 limit 条。
	FindSinceEpoch(ctx context.Context, groupID string, sinceEpoch uint64, limit int) ([]*model.MLSCommit, error)
	DeleteByGroupID(ctx context.Context, groupID string) error
}
