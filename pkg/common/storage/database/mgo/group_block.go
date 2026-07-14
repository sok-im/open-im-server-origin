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

func NewGroupBlockMongo(db *mongo.Database) (database.GroupBlock, error) {
	coll := db.Collection(database.GroupBlockName)
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "owner_user_id", Value: 1},
			{Key: "group_id", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &GroupBlockMgo{coll: coll}, nil
}

type GroupBlockMgo struct {
	coll *mongo.Collection
}

func (g *GroupBlockMgo) Upsert(ctx context.Context, block *model.GroupBlock) error {
	if block.CreateTime.IsZero() {
		block.CreateTime = time.Now()
	}
	filter := bson.M{
		"owner_user_id": block.OwnerUserID,
		"group_id":      block.GroupID,
	}
	update := bson.M{
		"$setOnInsert": bson.M{
			"owner_user_id": block.OwnerUserID,
			"group_id":      block.GroupID,
			"create_time":   block.CreateTime,
		},
	}
	_, err := g.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return errs.Wrap(err)
}

func (g *GroupBlockMgo) Delete(ctx context.Context, ownerUserID, groupID string) error {
	_, err := g.coll.DeleteOne(ctx, bson.M{
		"owner_user_id": ownerUserID,
		"group_id":      groupID,
	})
	return errs.Wrap(err)
}

func (g *GroupBlockMgo) DeleteByUserIDs(ctx context.Context, groupID string, ownerUserIDs []string) error {
	if len(ownerUserIDs) == 0 {
		return nil
	}
	_, err := g.coll.DeleteMany(ctx, bson.M{
		"group_id":      groupID,
		"owner_user_id": bson.M{"$in": ownerUserIDs},
	})
	return errs.Wrap(err)
}

func (g *GroupBlockMgo) DeleteByGroupID(ctx context.Context, groupID string) error {
	_, err := g.coll.DeleteMany(ctx, bson.M{"group_id": groupID})
	return errs.Wrap(err)
}

func (g *GroupBlockMgo) ListBlockedUserIDs(ctx context.Context, groupID string, candidateUserIDs []string) ([]string, error) {
	if len(candidateUserIDs) == 0 {
		return nil, nil
	}
	filter := bson.M{
		"group_id":      groupID,
		"owner_user_id": bson.M{"$in": candidateUserIDs},
	}
	cur, err := g.coll.Find(ctx, filter, options.Find().SetProjection(bson.M{"owner_user_id": 1, "_id": 0}))
	if err != nil {
		return nil, errs.Wrap(err)
	}
	defer cur.Close(ctx)
	var out []string
	for cur.Next(ctx) {
		var doc struct {
			OwnerUserID string `bson:"owner_user_id"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, errs.Wrap(err)
		}
		out = append(out, doc.OwnerUserID)
	}
	return out, cur.Err()
}

func (g *GroupBlockMgo) ListGroupIDsByOwner(ctx context.Context, ownerUserID string) ([]string, error) {
	if ownerUserID == "" {
		return nil, nil
	}
	cur, err := g.coll.Find(ctx, bson.M{"owner_user_id": ownerUserID},
		options.Find().SetProjection(bson.M{"group_id": 1, "_id": 0}))
	if err != nil {
		return nil, errs.Wrap(err)
	}
	defer cur.Close(ctx)
	var out []string
	for cur.Next(ctx) {
		var doc struct {
			GroupID string `bson:"group_id"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, errs.Wrap(err)
		}
		out = append(out, doc.GroupID)
	}
	return out, cur.Err()
}

func (g *GroupBlockMgo) Get(ctx context.Context, ownerUserID, groupID string) (*model.GroupBlock, error) {
	var out model.GroupBlock
	err := g.coll.FindOne(ctx, bson.M{
		"owner_user_id": ownerUserID,
		"group_id":      groupID,
	}).Decode(&out)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, errs.Wrap(err)
	}
	return &out, nil
}
