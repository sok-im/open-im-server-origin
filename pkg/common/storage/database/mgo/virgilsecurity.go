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

// ---------- VirgilDevice ----------

type virgilDeviceMgo struct {
	coll *mongo.Collection
}

func NewVirgilDeviceMongo(db *mongo.Database) (database.VirgilDevice, error) {
	coll := db.Collection("virgil_device")
	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "device_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "status", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "idempotency_key", Value: 1}},
			Options: options.Index().
				SetSparse(true).
				SetPartialFilterExpression(bson.M{"idempotency_key": bson.M{"$gt": ""}}),
		},
	})
	if err != nil {
		return nil, err
	}
	return &virgilDeviceMgo{coll: coll}, nil
}

func (m *virgilDeviceMgo) Upsert(ctx context.Context, d *model.VirgilDevice) error {
	filter := bson.M{"user_id": d.UserID, "device_id": d.DeviceID}
	set := bson.M{
		"user_id":         d.UserID,
		"device_id":       d.DeviceID,
		"card_id":         d.CardID,
		"platform":        d.Platform,
		"status":          d.Status,
		"client_version":  d.ClientVersion,
		"last_seen_at":    d.LastSeenAt,
		"update_time":     d.UpdateTime,
		"idempotency_key": d.IdempotencyKey,
	}
	setOnInsert := bson.M{
		"create_time": d.CreateTime,
	}
	update := bson.M{"$set": set, "$setOnInsert": setOnInsert}
	_, err := m.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (m *virgilDeviceMgo) FindByIdempotency(ctx context.Context, userID, key string) (*model.VirgilDevice, error) {
	if key == "" {
		return nil, errs.ErrRecordNotFound.WrapMsg("idempotency key empty")
	}
	var d model.VirgilDevice
	err := m.coll.FindOne(ctx, bson.M{"user_id": userID, "idempotency_key": key}).Decode(&d)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.ErrRecordNotFound.WrapMsg("virgil device not found by idempotency key")
		}
		return nil, err
	}
	return &d, nil
}

func (m *virgilDeviceMgo) FindByUserID(ctx context.Context, userID string, includeRevoked bool) ([]*model.VirgilDevice, error) {
	filter := bson.M{"user_id": userID}
	if !includeRevoked {
		filter["status"] = "active"
	}
	cursor, err := m.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	var devices []*model.VirgilDevice
	if err := cursor.All(ctx, &devices); err != nil {
		return nil, err
	}
	return devices, nil
}

func (m *virgilDeviceMgo) FindOne(ctx context.Context, userID, deviceID string) (*model.VirgilDevice, error) {
	var d model.VirgilDevice
	err := m.coll.FindOne(ctx, bson.M{"user_id": userID, "device_id": deviceID}).Decode(&d)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.ErrRecordNotFound.WrapMsg("virgil device not found", "userID", userID, "deviceID", deviceID)
		}
		return nil, err
	}
	return &d, nil
}

func (m *virgilDeviceMgo) UpdateStatus(ctx context.Context, userID, deviceID, status, reason string) error {
	now := time.Now()
	set := bson.M{"status": status, "update_time": now}
	if status == "revoked" {
		set["revoked_at"] = now
		set["revoke_reason"] = reason
	}
	res, err := m.coll.UpdateOne(ctx, bson.M{"user_id": userID, "device_id": deviceID}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return errs.ErrRecordNotFound.WrapMsg("virgil device not found", "userID", userID, "deviceID", deviceID)
	}
	return nil
}

func (m *virgilDeviceMgo) UpdateLastSeen(ctx context.Context, userID, deviceID string) error {
	res, err := m.coll.UpdateOne(ctx,
		bson.M{"user_id": userID, "device_id": deviceID},
		bson.M{"$set": bson.M{"last_seen_at": time.Now()}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return errs.ErrRecordNotFound.WrapMsg("virgil device not found", "userID", userID, "deviceID", deviceID)
	}
	return nil
}

// ---------- VirgilUserDeviceVersion ----------

type virgilUserDeviceVersionMgo struct {
	coll *mongo.Collection
}

func NewVirgilUserDeviceVersionMongo(db *mongo.Database) (database.VirgilUserDeviceVersion, error) {
	coll := db.Collection("virgil_user_device_version")
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, err
	}
	return &virgilUserDeviceVersionMgo{coll: coll}, nil
}

func (m *virgilUserDeviceVersionMgo) IncrVersion(ctx context.Context, userID string) (int64, error) {
	var result model.VirgilUserDeviceVersion
	err := m.coll.FindOneAndUpdate(ctx,
		bson.M{"user_id": userID},
		bson.M{"$inc": bson.M{"version": int64(1)}, "$setOnInsert": bson.M{"user_id": userID}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&result)
	if err != nil {
		return 0, err
	}
	return result.Version, nil
}

func (m *virgilUserDeviceVersionMgo) GetVersion(ctx context.Context, userID string) (int64, error) {
	var v model.VirgilUserDeviceVersion
	err := m.coll.FindOne(ctx, bson.M{"user_id": userID}).Decode(&v)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return 0, nil
		}
		return 0, err
	}
	return v.Version, nil
}

// ---------- VirgilDeviceEvent ----------

type virgilDeviceEventMgo struct {
	coll *mongo.Collection
}

func NewVirgilDeviceEventMongo(db *mongo.Database) (database.VirgilDeviceEvent, error) {
	coll := db.Collection("virgil_device_event")
	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "version", Value: 1}},
		},
		{
			Keys:    bson.D{{Key: "event_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
	})
	if err != nil {
		return nil, err
	}
	return &virgilDeviceEventMgo{coll: coll}, nil
}

func (m *virgilDeviceEventMgo) Append(ctx context.Context, ev *model.VirgilDeviceEvent) error {
	_, err := m.coll.InsertOne(ctx, ev)
	return err
}

func (m *virgilDeviceEventMgo) FindSince(ctx context.Context, userID string, sinceVersion int64, limit int64) ([]*model.VirgilDeviceEvent, error) {
	if limit <= 0 {
		limit = 256
	}
	cursor, err := m.coll.Find(ctx,
		bson.M{"user_id": userID, "version": bson.M{"$gt": sinceVersion}},
		options.Find().
			SetSort(bson.D{{Key: "version", Value: 1}}).
			SetLimit(limit))
	if err != nil {
		return nil, err
	}
	var events []*model.VirgilDeviceEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, err
	}
	return events, nil
}

// ---------- VirgilConversation ----------

type virgilConversationMgo struct {
	coll *mongo.Collection
}

func NewVirgilConversationMongo(db *mongo.Database) (database.VirgilConversation, error) {
	coll := db.Collection("virgil_conversation")
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "conversation_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, err
	}
	if _, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "user_a", Value: 1}, {Key: "user_b", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return nil, err
	}
	return &virgilConversationMgo{coll: coll}, nil
}

func (m *virgilConversationMgo) EnsureOneToOne(ctx context.Context, userA, userB string) (*model.VirgilConversation, error) {
	// 规范化排序：保证 (userA, userB) 与 (userB, userA) 落到同一条记录。
	a, b := normalizePair(userA, userB)
	conversationID := buildConversationID(a, b)
	now := time.Now()
	filter := bson.M{"user_a": a, "user_b": b}
	update := bson.M{
		"$setOnInsert": bson.M{
			"conversation_id": conversationID,
			"user_a":          a,
			"user_b":          b,
			"create_time":     now,
		},
	}
	var conv model.VirgilConversation
	err := m.coll.FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&conv)
	if err != nil {
		return nil, err
	}
	return &conv, nil
}

func normalizePair(a, b string) (string, string) {
	if a <= b {
		return a, b
	}
	return b, a
}

func buildConversationID(a, b string) string {
	return "c_1v1_" + a + "_" + b
}

// ---------- VirgilFileRef ----------

type virgilFileRefMgo struct {
	coll *mongo.Collection
}

func NewVirgilFileRefMongo(db *mongo.Database) (database.VirgilFileRef, error) {
	coll := db.Collection("virgil_file_ref")
	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "file_ref_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "owner_user_id", Value: 1}, {Key: "create_time", Value: -1}},
		},
	})
	if err != nil {
		return nil, err
	}
	return &virgilFileRefMgo{coll: coll}, nil
}

func (m *virgilFileRefMgo) Create(ctx context.Context, ref *model.VirgilFileRef) error {
	_, err := m.coll.InsertOne(ctx, ref)
	return err
}
