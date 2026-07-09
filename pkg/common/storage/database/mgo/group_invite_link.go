package mgo

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/pagination"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func NewGroupInviteLinkMongo(db *mongo.Database) (database.GroupInviteLink, error) {
	coll := db.Collection("group_invite_link")
	// Early releases created a non-unique group_id_1 index; drop it before the unique index.
	_, _ = coll.Indexes().DropOne(context.Background(), "group_id_1")

	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "link_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "group_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("group_id_unique"),
		},
		{
			Keys: bson.D{{Key: "created_at", Value: -1}},
		},
	})
	if err != nil {
		return nil, err
	}
	return &groupInviteLinkMgo{coll: coll}, nil
}

type groupInviteLinkMgo struct {
	coll *mongo.Collection
}

func (m *groupInviteLinkMgo) Create(ctx context.Context, link *model.GroupInviteLink) error {
	_, err := m.coll.InsertOne(ctx, link)
	return err
}

func (m *groupInviteLinkMgo) Save(ctx context.Context, link *model.GroupInviteLink) error {
	_, err := m.coll.UpdateOne(ctx,
		bson.M{"group_id": link.GroupID},
		bson.M{"$set": link},
		options.Update().SetUpsert(true),
	)
	return err
}

func (m *groupInviteLinkMgo) GetByLinkID(ctx context.Context, linkID string) (*model.GroupInviteLink, error) {
	var link model.GroupInviteLink
	err := m.coll.FindOne(ctx, bson.M{"link_id": linkID}).Decode(&link)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.ErrRecordNotFound.WrapMsg("invite link not found", "linkID", linkID)
		}
		return nil, err
	}
	return &link, nil
}

func (m *groupInviteLinkMgo) GetByGroupID(ctx context.Context, groupID string) (*model.GroupInviteLink, error) {
	var link model.GroupInviteLink
	err := m.coll.FindOne(ctx, bson.M{"group_id": groupID}).Decode(&link)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.ErrRecordNotFound.WrapMsg("invite link not found", "groupID", groupID)
		}
		return nil, err
	}
	return &link, nil
}

func (m *groupInviteLinkMgo) IncrUsedCount(ctx context.Context, linkID string) error {
	res, err := m.coll.UpdateOne(ctx,
		bson.M{"link_id": linkID},
		bson.M{"$inc": bson.M{"used_count": 1}, "$set": bson.M{"_updated_at": time.Now()}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return errs.ErrRecordNotFound.WrapMsg("invite link not found", "linkID", linkID)
	}
	return nil
}

func (m *groupInviteLinkMgo) Revoke(ctx context.Context, linkID string) error {
	res, err := m.coll.UpdateOne(ctx,
		bson.M{"link_id": linkID},
		bson.M{"$set": bson.M{"revoked": true}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return errs.ErrRecordNotFound.WrapMsg("invite link not found", "linkID", linkID)
	}
	return nil
}

func (m *groupInviteLinkMgo) DeleteByGroupID(ctx context.Context, groupID string) error {
	_, err := m.coll.DeleteOne(ctx, bson.M{"group_id": groupID})
	return err
}

func (m *groupInviteLinkMgo) ListByGroupID(ctx context.Context, groupID string, pg pagination.Pagination) (int64, []*model.GroupInviteLink, error) {
	link, err := m.GetByGroupID(ctx, groupID)
	if err != nil {
		if errs.Unwrap(err) == errs.ErrRecordNotFound {
			return 0, nil, nil
		}
		return 0, nil, err
	}
	if pg != nil && pg.GetPageNumber() > 1 {
		return 1, nil, nil
	}
	return 1, []*model.GroupInviteLink{link}, nil
}
