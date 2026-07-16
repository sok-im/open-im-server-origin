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

package controller

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/tx"
)

// OpenMLSDatabase 聚合 MLS Delivery Service 所需的全部存储操作。
type OpenMLSDatabase interface {
	database.MLSKeyPackageDatabase
	database.MLSGroupDatabase
	database.MLSCommitDatabase

	// DeleteGroupAll atomically deletes the group state and all its commit history
	// inside a single MongoDB transaction.  Use this instead of calling
	// DeleteGroup + DeleteByGroupID separately to avoid orphan commit records when
	// the second operation fails.
	DeleteGroupAll(ctx context.Context, groupID string) error
}

type openMLSDatabase struct {
	kp     database.MLSKeyPackageDatabase
	group  database.MLSGroupDatabase
	commit database.MLSCommitDatabase
	tx     tx.Tx
}

func NewOpenMLSDatabase(
	kp database.MLSKeyPackageDatabase,
	group database.MLSGroupDatabase,
	commit database.MLSCommitDatabase,
	mongoTx tx.Tx,
) OpenMLSDatabase {
	return &openMLSDatabase{kp: kp, group: group, commit: commit, tx: mongoTx}
}

// --- MLSKeyPackageDatabase ---

func (d *openMLSDatabase) Insert(ctx context.Context, kp *model.MLSKeyPackage) error {
	return d.kp.Insert(ctx, kp)
}

func (d *openMLSDatabase) BatchInsert(ctx context.Context, kps []*model.MLSKeyPackage) error {
	return d.kp.BatchInsert(ctx, kps)
}

func (d *openMLSDatabase) ConsumeByUserID(ctx context.Context, userID, excludeDeviceID string, countPerDevice int) ([]*model.MLSKeyPackage, error) {
	return d.kp.ConsumeByUserID(ctx, userID, excludeDeviceID, countPerDevice)
}

func (d *openMLSDatabase) CountByUserID(ctx context.Context, userID string) (map[string]int32, error) {
	return d.kp.CountByUserID(ctx, userID)
}

func (d *openMLSDatabase) CountByDevice(ctx context.Context, userID, deviceID string) (int32, error) {
	return d.kp.CountByDevice(ctx, userID, deviceID)
}

// --- MLSGroupDatabase ---

func (d *openMLSDatabase) UpsertState(ctx context.Context, state *model.MLSGroupState) error {
	return d.group.UpsertState(ctx, state)
}

func (d *openMLSDatabase) GetState(ctx context.Context, groupID string) (*model.MLSGroupState, error) {
	return d.group.GetState(ctx, groupID)
}

func (d *openMLSDatabase) DeleteGroup(ctx context.Context, groupID string) error {
	return d.group.DeleteGroup(ctx, groupID)
}

func (d *openMLSDatabase) IncrEpoch(ctx context.Context, groupID string, fromEpoch uint64) (uint64, error) {
	return d.group.IncrEpoch(ctx, groupID, fromEpoch)
}

func (d *openMLSDatabase) UpdateMemberCount(ctx context.Context, groupID string, memberCount int32) error {
	return d.group.UpdateMemberCount(ctx, groupID, memberCount)
}

// --- MLSCommitDatabase ---

func (d *openMLSDatabase) AppendCommit(ctx context.Context, c *model.MLSCommit) error {
	return d.commit.AppendCommit(ctx, c)
}

func (d *openMLSDatabase) FindByIdempotencyKey(ctx context.Context, key string) (*model.MLSCommit, error) {
	return d.commit.FindByIdempotencyKey(ctx, key)
}

func (d *openMLSDatabase) FindSinceEpoch(ctx context.Context, groupID string, sinceEpoch uint64, limit int) ([]*model.MLSCommit, error) {
	return d.commit.FindSinceEpoch(ctx, groupID, sinceEpoch, limit)
}

func (d *openMLSDatabase) DeleteByGroupID(ctx context.Context, groupID string) error {
	return d.commit.DeleteByGroupID(ctx, groupID)
}

// --- Combined transactional operations ---

// DeleteGroupAll deletes mls_group_state and all mls_commit records for the
// given group in a single atomic MongoDB transaction.  On a non-replica-set
// deployment the tx wrapper falls back to a plain sequential call, which is
// acceptable since single-node MongoDB does not support multi-document
// transactions.
func (d *openMLSDatabase) DeleteGroupAll(ctx context.Context, groupID string) error {
	return d.tx.Transaction(ctx, func(ctx context.Context) error {
		if err := d.group.DeleteGroup(ctx, groupID); err != nil {
			return err
		}
		return d.commit.DeleteByGroupID(ctx, groupID)
	})
}
