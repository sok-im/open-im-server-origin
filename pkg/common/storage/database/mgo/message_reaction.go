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
	"github.com/openimsdk/tools/db/mongoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func NewMessageReactionMongo(db *mongo.Database) (database.MessageReaction, error) {
	coll := db.Collection(database.MessageReactionName)
	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			// 同一用户对同一消息只能有一条反应记录。
			Keys: bson.D{
				{Key: "conversation_id", Value: 1},
				{Key: "client_msg_id", Value: 1},
				{Key: "user_id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			// 聚合查询：按消息拉取全部反应。
			Keys: bson.D{
				{Key: "conversation_id", Value: 1},
				{Key: "client_msg_id", Value: 1},
				{Key: "create_time", Value: 1},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return &MessageReactionMgo{coll: coll}, nil
}

type MessageReactionMgo struct {
	coll *mongo.Collection
}

func (m *MessageReactionMgo) Set(ctx context.Context, conversationID, clientMsgID, userID, emoji string, now time.Time) error {
	filter := bson.M{
		"conversation_id": conversationID,
		"client_msg_id":   clientMsgID,
		"user_id":         userID,
	}
	update := bson.M{
		"$set": bson.M{
			"emoji":       emoji,
			"update_time": now,
		},
		"$setOnInsert": bson.M{
			"_id":         primitive.NewObjectID(),
			"create_time": now,
		},
	}
	return mongoutil.UpdateOne(ctx, m.coll, filter, update, false, options.Update().SetUpsert(true))
}

func (m *MessageReactionMgo) Remove(ctx context.Context, conversationID, clientMsgID, userID, emoji string) (bool, error) {
	filter := bson.M{
		"conversation_id": conversationID,
		"client_msg_id":   clientMsgID,
		"user_id":         userID,
	}
	if emoji != "" {
		filter["emoji"] = emoji
	}
	res, err := mongoutil.DeleteOneResult(ctx, m.coll, filter)
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

func (m *MessageReactionMgo) FindByMessage(ctx context.Context, conversationID, clientMsgID string) ([]*model.MessageReaction, error) {
	filter := bson.M{
		"conversation_id": conversationID,
		"client_msg_id":   clientMsgID,
	}
	return mongoutil.Find[*model.MessageReaction](ctx, m.coll, filter,
		options.Find().SetSort(bson.D{{Key: "create_time", Value: 1}}))
}

func (m *MessageReactionMgo) FindByMessages(ctx context.Context, conversationID string, clientMsgIDs []string) ([]*model.MessageReaction, error) {
	filter := bson.M{
		"conversation_id": conversationID,
		"client_msg_id":   bson.M{"$in": clientMsgIDs},
	}
	return mongoutil.Find[*model.MessageReaction](ctx, m.coll, filter,
		options.Find().SetSort(bson.D{{Key: "create_time", Value: 1}}))
}
