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

// ──────────────────────────────────────────────────────────────────────────────
// UserTotp
// ──────────────────────────────────────────────────────────────────────────────

type userTotpMgo struct {
	coll *mongo.Collection
}

// NewUserTotpMongo creates the user_totp collection accessor and ensures indexes.
func NewUserTotpMongo(db *mongo.Database) (database.UserTotp, error) {
	coll := db.Collection(database.UserTotpName)
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, err
	}
	return &userTotpMgo{coll: coll}, nil
}

func (u *userTotpMgo) Get(ctx context.Context, userID string) (*model.UserTotp, error) {
	return mongoutil.FindOne[*model.UserTotp](ctx, u.coll, bson.M{"user_id": userID})
}

func (u *userTotpMgo) Upsert(ctx context.Context, doc *model.UserTotp) error {
	now := time.Now()
	if doc.ID.IsZero() {
		doc.ID = primitive.NewObjectID()
		doc.CreateTime = now
	}
	doc.UpdateTime = now
	filter := bson.M{"user_id": doc.UserID}
	update := bson.M{"$set": doc}
	_, err := u.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (u *userTotpMgo) Delete(ctx context.Context, userID string) error {
	_, err := u.coll.DeleteOne(ctx, bson.M{"user_id": userID})
	return err
}

// ──────────────────────────────────────────────────────────────────────────────
// UserTotpRecovery
// ──────────────────────────────────────────────────────────────────────────────

type userTotpRecoveryMgo struct {
	coll *mongo.Collection
}

// NewUserTotpRecoveryMongo creates the user_totp_recovery collection accessor and ensures indexes.
func NewUserTotpRecoveryMongo(db *mongo.Database) (database.UserTotpRecovery, error) {
	coll := db.Collection(database.UserTotpRecoveryName)
	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "user_id", Value: 1}}},
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "used", Value: 1}}},
	})
	if err != nil {
		return nil, err
	}
	return &userTotpRecoveryMgo{coll: coll}, nil
}

func (u *userTotpRecoveryMgo) InsertMany(ctx context.Context, docs []*model.UserTotpRecovery) error {
	now := time.Now()
	ifaces := make([]interface{}, len(docs))
	for i, d := range docs {
		if d.ID.IsZero() {
			d.ID = primitive.NewObjectID()
		}
		if d.CreateTime.IsZero() {
			d.CreateTime = now
		}
		ifaces[i] = d
	}
	_, err := u.coll.InsertMany(ctx, ifaces)
	return err
}

func (u *userTotpRecoveryMgo) FindUnused(ctx context.Context, userID string) ([]*model.UserTotpRecovery, error) {
	return mongoutil.Find[*model.UserTotpRecovery](ctx, u.coll, bson.M{"user_id": userID, "used": false})
}

func (u *userTotpRecoveryMgo) MarkUsed(ctx context.Context, id interface{}, usedAt int64) error {
	_, err := u.coll.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"used": true, "used_at": usedAt}},
	)
	return err
}

func (u *userTotpRecoveryMgo) CountUnused(ctx context.Context, userID string) (int64, error) {
	return u.coll.CountDocuments(ctx, bson.M{"user_id": userID, "used": false})
}

func (u *userTotpRecoveryMgo) DeleteByUser(ctx context.Context, userID string) error {
	_, err := u.coll.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}
