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

package mgo

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ==================== MLSKeyPackage ====================

type mlsKeyPackageMgo struct {
	coll *mongo.Collection
}

func NewMLSKeyPackageMongo(db *mongo.Database) (database.MLSKeyPackageDatabase, error) {
	coll := db.Collection("mls_key_package")
	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "kp_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "device_id", Value: 1}, {Key: "consumed", Value: 1}},
		},
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
	})
	if err != nil {
		return nil, err
	}
	return &mlsKeyPackageMgo{coll: coll}, nil
}

func (m *mlsKeyPackageMgo) Insert(ctx context.Context, kp *model.MLSKeyPackage) error {
	_, err := m.coll.InsertOne(ctx, kp)
	return err
}

func (m *mlsKeyPackageMgo) BatchInsert(ctx context.Context, kps []*model.MLSKeyPackage) error {
	docs := make([]any, len(kps))
	for i, kp := range kps {
		docs[i] = kp
	}
	_, err := m.coll.InsertMany(ctx, docs)
	return err
}

func (m *mlsKeyPackageMgo) ConsumeByUserID(ctx context.Context, userID, excludeDeviceID string, countPerDevice int) ([]*model.MLSKeyPackage, error) {
	if countPerDevice <= 0 {
		countPerDevice = 1
	}

	// Find all unconsumed device IDs for this user.
	matchFilter := bson.M{"user_id": userID, "consumed": false}
	if excludeDeviceID != "" {
		matchFilter["device_id"] = bson.M{"$ne": excludeDeviceID}
	}
	deviceIDs, err := m.coll.Distinct(ctx, "device_id", matchFilter)
	if err != nil {
		return nil, err
	}

	// Claim each KeyPackage atomically via FindOneAndUpdate so that two concurrent
	// consumers can never receive the same KeyPackage (one-time-use guarantee).
	claimOpts := options.FindOneAndUpdate().
		SetSort(bson.D{{Key: "created_at", Value: 1}}).
		SetReturnDocument(options.Before)

	var result []*model.MLSKeyPackage
	for _, did := range deviceIDs {
		deviceID, ok := did.(string)
		if !ok {
			continue
		}
		for i := 0; i < countPerDevice; i++ {
			filter := bson.M{"user_id": userID, "device_id": deviceID, "consumed": false}
			update := bson.M{"$set": bson.M{"consumed": true}}
			var kp model.MLSKeyPackage
			err := m.coll.FindOneAndUpdate(ctx, filter, update, claimOpts).Decode(&kp)
			if err == mongo.ErrNoDocuments {
				// No more unconsumed KeyPackages for this device.
				break
			}
			if err != nil {
				return nil, err
			}
			result = append(result, &kp)
		}
	}
	return result, nil
}

func (m *mlsKeyPackageMgo) CountByUserID(ctx context.Context, userID string) (map[string]int32, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"user_id": userID, "consumed": false}}},
		{{Key: "$group", Value: bson.M{"_id": "$device_id", "count": bson.M{"$sum": 1}}}},
	}
	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	result := make(map[string]int32)
	var rows []struct {
		ID    string `bson:"_id"`
		Count int32  `bson:"count"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.ID] = r.Count
	}
	return result, nil
}

func (m *mlsKeyPackageMgo) CountByDevice(ctx context.Context, userID, deviceID string) (int32, error) {
	n, err := m.coll.CountDocuments(ctx, bson.M{"user_id": userID, "device_id": deviceID, "consumed": false})
	if err != nil {
		return 0, err
	}
	return int32(n), nil
}

// ==================== MLSGroupState ====================

type mlsGroupStateMgo struct {
	coll *mongo.Collection
}

func NewMLSGroupStateMongo(db *mongo.Database) (database.MLSGroupDatabase, error) {
	coll := db.Collection("mls_group_state")
	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "group_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
	})
	if err != nil {
		return nil, err
	}
	return &mlsGroupStateMgo{coll: coll}, nil
}

func (m *mlsGroupStateMgo) UpsertState(ctx context.Context, state *model.MLSGroupState) error {
	filter := bson.M{"group_id": state.GroupID}
	update := bson.M{"$setOnInsert": state}
	opts := options.Update().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, filter, update, opts)
	return err
}

func (m *mlsGroupStateMgo) GetState(ctx context.Context, groupID string) (*model.MLSGroupState, error) {
	var state model.MLSGroupState
	err := m.coll.FindOne(ctx, bson.M{"group_id": groupID}).Decode(&state)
	if err == mongo.ErrNoDocuments {
		return nil, errs.ErrRecordNotFound.Wrap()
	}
	return &state, err
}

func (m *mlsGroupStateMgo) DeleteGroup(ctx context.Context, groupID string) error {
	_, err := m.coll.DeleteOne(ctx, bson.M{"group_id": groupID})
	return err
}

func (m *mlsGroupStateMgo) UpdateMemberCount(ctx context.Context, groupID string, memberCount int32) error {
	_, err := m.coll.UpdateOne(ctx,
		bson.M{"group_id": groupID},
		bson.M{"$set": bson.M{"member_count": memberCount}},
	)
	return err
}

func (m *mlsGroupStateMgo) IncrEpoch(ctx context.Context, groupID string, fromEpoch uint64) (uint64, error) {
	now := time.Now()
	filter := bson.M{"group_id": groupID, "current_epoch": fromEpoch}
	update := bson.M{
		"$inc": bson.M{"current_epoch": 1},
		"$set": bson.M{"last_commit_at": now},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var updated model.MLSGroupState
	err := m.coll.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updated)
	if err == mongo.ErrNoDocuments {
		return 0, errs.ErrRecordNotFound.Wrap()
	}
	if err != nil {
		return 0, err
	}
	return updated.CurrentEpoch, nil
}

// ==================== MLSCommit ====================

type mlsCommitMgo struct {
	coll *mongo.Collection
}

func NewMLSCommitMongo(db *mongo.Database) (database.MLSCommitDatabase, error) {
	coll := db.Collection("mls_commit")
	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "group_id", Value: 1}, {Key: "epoch", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "group_id", Value: 1}, {Key: "created_at", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "idempotency_key", Value: 1}},
			Options: options.Index().
				SetUnique(true).
				SetPartialFilterExpression(bson.M{"idempotency_key": bson.M{"$ne": ""}}),
		},
	})
	if err != nil {
		return nil, err
	}
	return &mlsCommitMgo{coll: coll}, nil
}

func (m *mlsCommitMgo) AppendCommit(ctx context.Context, c *model.MLSCommit) error {
	_, err := m.coll.InsertOne(ctx, c)
	return err
}

func (m *mlsCommitMgo) FindByIdempotencyKey(ctx context.Context, key string) (*model.MLSCommit, error) {
	var commit model.MLSCommit
	if err := m.coll.FindOne(ctx, bson.M{"idempotency_key": key}).Decode(&commit); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.ErrRecordNotFound.Wrap()
		}
		return nil, err
	}
	return &commit, nil
}

func (m *mlsCommitMgo) FindSinceEpoch(ctx context.Context, groupID string, sinceEpoch uint64, limit int) ([]*model.MLSCommit, error) {
	if limit <= 0 {
		limit = 50
	}
	filter := bson.M{"group_id": groupID, "epoch": bson.M{"$gt": sinceEpoch}}
	opts := options.Find().SetSort(bson.D{{Key: "epoch", Value: 1}}).SetLimit(int64(limit))
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	var commits []*model.MLSCommit
	if err := cursor.All(ctx, &commits); err != nil {
		return nil, err
	}
	return commits, nil
}

func (m *mlsCommitMgo) DeleteByGroupID(ctx context.Context, groupID string) error {
	_, err := m.coll.DeleteMany(ctx, bson.M{"group_id": groupID})
	return err
}
