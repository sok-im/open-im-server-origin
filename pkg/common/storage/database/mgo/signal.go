// Copyright © 2024 OpenIM. All rights reserved.
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

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/pagination"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func NewSignalMongo(db *mongo.Database) (database.SignalDatabase, error) {
	invColl := db.Collection(database.SignalInvitationName)
	// Earlier releases created a TTL index on expire_at; drop it so invitations
	// are only removed by explicit call-end paths (reject/cancel/hangup/timeout).
	_, _ = invColl.Indexes().DropOne(context.Background(), "expire_at_1")

	_, err := invColl.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "room_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "invitee_user_id_list", Value: 1}},
		},
		// Busy-line / inviter-side lookups (GetBusyUserIDs $or).
		{
			Keys: bson.D{{Key: "inviter_user_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "create_time", Value: -1}},
		},
	})
	if err != nil {
		return nil, err
	}

	recColl := db.Collection(database.SignalRecordName)
	_, err = recColl.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "sid", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "send_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "recv_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "create_time", Value: -1}},
		},
	})
	if err != nil {
		return nil, err
	}

	return &signalMgo{invColl: invColl, recColl: recColl}, nil
}

type signalMgo struct {
	invColl *mongo.Collection
	recColl *mongo.Collection
}

func (s *signalMgo) CreateInvitation(ctx context.Context, inv *model.SignalInvitation) error {
	return mongoutil.InsertMany(ctx, s.invColl, []*model.SignalInvitation{inv})
}

func (s *signalMgo) GetInvitationByRoomID(ctx context.Context, roomID string) (*model.SignalInvitation, error) {
	return mongoutil.FindOne[*model.SignalInvitation](ctx, s.invColl, bson.M{"room_id": roomID})
}

func (s *signalMgo) GetInvitationByInviteeUserID(ctx context.Context, userID string) (*model.SignalInvitation, error) {
	opts := options.FindOne().SetSort(bson.M{"create_time": -1})
	return mongoutil.FindOne[*model.SignalInvitation](ctx, s.invColl, bson.M{"invitee_user_id_list": userID}, opts)
}

func (s *signalMgo) GetInvitationByUserID(ctx context.Context, userID string) (*model.SignalInvitation, error) {
	filter := bson.M{
		"$or": bson.A{
			bson.M{"inviter_user_id": userID},
			bson.M{"invitee_user_id_list": userID},
		},
	}
	opts := options.FindOne().SetSort(bson.M{"create_time": -1})
	return mongoutil.FindOne[*model.SignalInvitation](ctx, s.invColl, filter, opts)
}

func (s *signalMgo) DeleteInvitation(ctx context.Context, roomID string) error {
	return mongoutil.DeleteMany(ctx, s.invColl, bson.M{"room_id": roomID})
}

func (s *signalMgo) TryDeleteInvitation(ctx context.Context, roomID string) (bool, error) {
	res, err := s.invColl.DeleteOne(ctx, bson.M{"room_id": roomID})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

// DeleteInvitationIfNotAccepted atomically deletes the invitation only when it
// has not been accepted yet (accept_time absent or 0). Returns true when a
// document was actually deleted.
//
// This mirrors the conditional filter used by SetAcceptTime, so "cancel" and
// "accept" race for the same single document and MongoDB serializes them:
//   - if SetAcceptTime commits first, accept_time becomes > 0 and this delete
//     matches nothing (deleted=false) → caller knows the call was accepted;
//   - if this delete commits first, the document is gone and SetAcceptTime
//     matches nothing → the accept did not take effect on the record.
//
// It lets handleCancel decide answered-vs-not without the TOCTOU window of
// reading accept_time from an earlier snapshot.
func (s *signalMgo) DeleteInvitationIfNotAccepted(ctx context.Context, roomID string) (bool, error) {
	filter := bson.M{
		"room_id": roomID,
		"$or": bson.A{
			bson.M{"accept_time": bson.M{"$exists": false}},
			bson.M{"accept_time": int64(0)},
		},
	}
	res, err := s.invColl.DeleteOne(ctx, filter)
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

func (s *signalMgo) RemoveInvitee(ctx context.Context, roomID string, userID string) error {
	filter := bson.M{"room_id": roomID}
	update := bson.M{"$pull": bson.M{"invitee_user_id_list": userID}}
	if _, err := s.invColl.UpdateOne(ctx, filter, update); err != nil {
		return err
	}
	_, err := s.invColl.DeleteOne(ctx, bson.M{
		"room_id":              roomID,
		"invitee_user_id_list": bson.M{"$size": 0},
	})
	return err
}

// PullInvitee removes userID from the invitee list without auto-deleting the
// invitation record when the list becomes empty.  Use this when the call is
// still ongoing so the invitation survives for TryDeleteInvitation to claim.
func (s *signalMgo) PullInvitee(ctx context.Context, roomID string, userID string) error {
	filter := bson.M{"room_id": roomID}
	update := bson.M{"$pull": bson.M{"invitee_user_id_list": userID}}
	_, err := s.invColl.UpdateOne(ctx, filter, update)
	return err
}

func (s *signalMgo) AddInvitee(ctx context.Context, roomID string, userID string) error {
	filter := bson.M{"room_id": roomID, "inviter_user_id": bson.M{"$ne": userID}}
	update := bson.M{"$addToSet": bson.M{"invitee_user_id_list": userID}}
	return mongoutil.UpdateOne(ctx, s.invColl, filter, update, false)
}

func (s *signalMgo) SetAcceptTime(ctx context.Context, roomID string, acceptTime int64) error {
	filter := bson.M{
		"room_id": roomID,
		"$or": bson.A{
			bson.M{"accept_time": bson.M{"$exists": false}},
			bson.M{"accept_time": int64(0)},
		},
	}
	update := bson.M{"$set": bson.M{"accept_time": acceptTime}}
	return mongoutil.UpdateOne(ctx, s.invColl, filter, update, false)
}

func (s *signalMgo) GetInvitationByGroupID(ctx context.Context, groupID string) (*model.SignalInvitation, error) {
	opts := options.FindOne().SetSort(bson.M{"create_time": -1})
	return mongoutil.FindOne[*model.SignalInvitation](ctx, s.invColl, bson.M{"group_id": groupID}, opts)
}

func (s *signalMgo) GetInvitationsByRoomIDs(ctx context.Context, roomIDs []string) ([]*model.SignalInvitation, error) {
	return mongoutil.Find[*model.SignalInvitation](ctx, s.invColl, bson.M{"room_id": bson.M{"$in": roomIDs}})
}

func (s *signalMgo) GetBusyUserIDs(ctx context.Context, userIDs []string) ([]string, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	filter := bson.M{
		"$or": bson.A{
			bson.M{"inviter_user_id": bson.M{"$in": userIDs}},
			bson.M{"invitee_user_id_list": bson.M{"$in": userIDs}},
		},
	}
	invitations, err := mongoutil.Find[*model.SignalInvitation](ctx, s.invColl, filter,
		options.Find().SetProjection(bson.M{"inviter_user_id": 1, "invitee_user_id_list": 1}),
	)
	if err != nil {
		return nil, err
	}
	requested := make(map[string]struct{}, len(userIDs))
	for _, uid := range userIDs {
		requested[uid] = struct{}{}
	}
	busySet := make(map[string]struct{})
	for _, inv := range invitations {
		if _, ok := requested[inv.InviterUserID]; ok {
			busySet[inv.InviterUserID] = struct{}{}
		}
		for _, uid := range inv.InviteeUserIDList {
			if _, ok := requested[uid]; ok {
				busySet[uid] = struct{}{}
			}
		}
	}
	busy := make([]string, 0, len(busySet))
	for uid := range busySet {
		busy = append(busy, uid)
	}
	return busy, nil
}

func (s *signalMgo) CreateRecord(ctx context.Context, record *model.SignalRecord) error {
	return mongoutil.InsertMany(ctx, s.recColl, []*model.SignalRecord{record})
}

func (s *signalMgo) SearchRecords(ctx context.Context, sendID, recvID string, sessionType int32, startTime, endTime int64, pagination pagination.Pagination) (int64, []*model.SignalRecord, error) {
	filter := bson.M{}
	if sendID != "" {
		filter["send_id"] = sendID
	}
	if recvID != "" {
		filter["recv_id"] = recvID
	}
	if sessionType != 0 {
		filter["session_type"] = sessionType
	}
	if startTime > 0 || endTime > 0 {
		timeFilter := bson.M{}
		if startTime > 0 {
			timeFilter["$gte"] = startTime
		}
		if endTime > 0 {
			timeFilter["$lte"] = endTime
		}
		filter["create_time"] = timeFilter
	}
	return mongoutil.FindPage[*model.SignalRecord](ctx, s.recColl, filter, pagination, options.Find().SetSort(bson.M{"create_time": -1}))
}

func (s *signalMgo) DeleteRecords(ctx context.Context, sIDs []string) error {
	return mongoutil.DeleteMany(ctx, s.recColl, bson.M{"sid": bson.M{"$in": sIDs}})
}
