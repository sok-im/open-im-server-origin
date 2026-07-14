package controller

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

// GroupBlockDatabase per-user group chat message push block.
type GroupBlockDatabase interface {
	Upsert(ctx context.Context, block *model.GroupBlock) error
	Delete(ctx context.Context, ownerUserID, groupID string) error
	DeleteByUserIDs(ctx context.Context, groupID string, ownerUserIDs []string) error
	DeleteByGroupID(ctx context.Context, groupID string) error
	ListBlockedUserIDs(ctx context.Context, groupID string, candidateUserIDs []string) ([]string, error)
	ListGroupIDsByOwner(ctx context.Context, ownerUserID string) ([]string, error)
	Get(ctx context.Context, ownerUserID, groupID string) (*model.GroupBlock, error)
}

type groupBlockDatabase struct {
	db database.GroupBlock
}

func NewGroupBlockDatabase(db database.GroupBlock) GroupBlockDatabase {
	return &groupBlockDatabase{db: db}
}

func (g *groupBlockDatabase) Upsert(ctx context.Context, block *model.GroupBlock) error {
	return g.db.Upsert(ctx, block)
}

func (g *groupBlockDatabase) Delete(ctx context.Context, ownerUserID, groupID string) error {
	return g.db.Delete(ctx, ownerUserID, groupID)
}

func (g *groupBlockDatabase) DeleteByUserIDs(ctx context.Context, groupID string, ownerUserIDs []string) error {
	return g.db.DeleteByUserIDs(ctx, groupID, ownerUserIDs)
}

func (g *groupBlockDatabase) DeleteByGroupID(ctx context.Context, groupID string) error {
	return g.db.DeleteByGroupID(ctx, groupID)
}

func (g *groupBlockDatabase) ListBlockedUserIDs(ctx context.Context, groupID string, candidateUserIDs []string) ([]string, error) {
	return g.db.ListBlockedUserIDs(ctx, groupID, candidateUserIDs)
}

func (g *groupBlockDatabase) ListGroupIDsByOwner(ctx context.Context, ownerUserID string) ([]string, error) {
	return g.db.ListGroupIDsByOwner(ctx, ownerUserID)
}

func (g *groupBlockDatabase) Get(ctx context.Context, ownerUserID, groupID string) (*model.GroupBlock, error) {
	return g.db.Get(ctx, ownerUserID, groupID)
}
