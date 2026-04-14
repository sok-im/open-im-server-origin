package mgo

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/mongoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func NewCryptoMongo(db *mongo.Database) (database.CryptoDatabase, error) {
	deviceColl := db.Collection(database.CryptoDeviceName)
	if _, err := deviceColl.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "device_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "user_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "virgil_identity", Value: 1}},
		},
	}); err != nil {
		return nil, err
	}

	eventColl := db.Collection(database.CryptoGroupKeyEventName)
	if _, err := eventColl.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "event_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "group_id", Value: 1},
				{Key: "group_key_version", Value: 1},
			},
		},
		{
			Keys: bson.D{{Key: "create_time", Value: -1}},
		},
	}); err != nil {
		return nil, err
	}

	versionColl := db.Collection(database.CryptoGroupKeyVersionName)
	if _, err := versionColl.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "group_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return nil, err
	}

	return &cryptoMgo{deviceColl: deviceColl, eventColl: eventColl, versionColl: versionColl}, nil
}

type cryptoMgo struct {
	deviceColl  *mongo.Collection
	eventColl   *mongo.Collection
	versionColl *mongo.Collection
}

func (c *cryptoMgo) CreateDevice(ctx context.Context, device *model.CryptoDevice) error {
	return mongoutil.InsertMany(ctx, c.deviceColl, []*model.CryptoDevice{device})
}

func (c *cryptoMgo) GetDeviceByID(ctx context.Context, deviceID string) (*model.CryptoDevice, error) {
	return mongoutil.FindOne[*model.CryptoDevice](ctx, c.deviceColl, bson.M{"device_id": deviceID})
}

func (c *cryptoMgo) GetDevicesByUserID(ctx context.Context, userID string, limit int64) ([]*model.CryptoDevice, error) {
	opts := options.Find()
	if limit > 0 {
		opts.SetLimit(limit)
	}
	return mongoutil.Find[*model.CryptoDevice](ctx, c.deviceColl, bson.M{"user_id": userID}, opts)
}

func (c *cryptoMgo) CountDevicesByUserID(ctx context.Context, userID string) (int64, error) {
	return c.deviceColl.CountDocuments(ctx, bson.M{"user_id": userID, "status": "active"})
}

func (c *cryptoMgo) UpdateDeviceStatus(ctx context.Context, deviceID string, status string) error {
	_, err := c.deviceColl.UpdateOne(ctx,
		bson.M{"device_id": deviceID},
		bson.M{"$set": bson.M{"status": status}},
	)
	return err
}

func (c *cryptoMgo) UpdateDeviceLastSeen(ctx context.Context, deviceID string, lastSeenAt int64) error {
	_, err := c.deviceColl.UpdateOne(ctx,
		bson.M{"device_id": deviceID},
		bson.M{"$set": bson.M{"last_seen_at": lastSeenAt}},
	)
	return err
}

func (c *cryptoMgo) AtomicRevokeDevice(ctx context.Context, deviceID string, userID string) (bool, error) {
	res, err := c.deviceColl.UpdateOne(ctx,
		bson.M{"device_id": deviceID, "user_id": userID, "status": "active"},
		bson.M{"$set": bson.M{"status": "revoked"}},
	)
	if err != nil {
		return false, err
	}
	return res.ModifiedCount > 0, nil
}

func (c *cryptoMgo) AtomicBumpGroupKeyVersion(ctx context.Context, groupID string) (int64, error) {
	filter := bson.M{"group_id": groupID}
	update := bson.M{"$inc": bson.M{"version": int64(1)}}
	opt := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After).
		SetProjection(bson.M{"_id": 0, "version": 1})
	return mongoutil.FindOneAndUpdate[int64](ctx, c.versionColl, filter, update, opt)
}

func (c *cryptoMgo) CreateGroupKeyEvent(ctx context.Context, event *model.GroupKeyEvent) error {
	return mongoutil.InsertMany(ctx, c.eventColl, []*model.GroupKeyEvent{event})
}

func (c *cryptoMgo) GetLatestGroupKeyVersion(ctx context.Context, groupID string) (int64, error) {
	type versionDoc struct {
		Version int64 `bson:"version"`
	}
	doc, err := mongoutil.FindOne[*versionDoc](ctx, c.versionColl, bson.M{"group_id": groupID})
	if err != nil {
		if IsNotFound(err) {
			return 0, nil
		}
		return 0, err
	}
	return doc.Version, nil
}

func (c *cryptoMgo) GetGroupKeyEventsSince(ctx context.Context, groupID string, sinceVersion int64, limit int64) ([]*model.GroupKeyEvent, error) {
	filter := bson.M{
		"group_id":          groupID,
		"group_key_version": bson.M{"$gt": sinceVersion},
	}
	opts := options.Find().SetSort(bson.M{"group_key_version": 1})
	if limit > 0 {
		opts.SetLimit(limit)
	}
	return mongoutil.Find[*model.GroupKeyEvent](ctx, c.eventColl, filter, opts)
}
