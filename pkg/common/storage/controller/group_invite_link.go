package controller

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/pagination"
)

// GroupInviteLinkDatabase 群邀请链接业务层操作接口（每群唯一）。
type GroupInviteLinkDatabase interface {
	Create(ctx context.Context, link *model.GroupInviteLink) error
	Save(ctx context.Context, link *model.GroupInviteLink) error
	GetByLinkID(ctx context.Context, linkID string) (*model.GroupInviteLink, error)
	GetByGroupID(ctx context.Context, groupID string) (*model.GroupInviteLink, error)
	IncrUsedCount(ctx context.Context, linkID string) error
	Revoke(ctx context.Context, linkID string) error
	DeleteByGroupID(ctx context.Context, groupID string) error
	ListByGroupID(ctx context.Context, groupID string, pg pagination.Pagination) (int64, []*model.GroupInviteLink, error)
}

type groupInviteLinkDatabase struct {
	db database.GroupInviteLink
}

func NewGroupInviteLinkDatabase(db database.GroupInviteLink) GroupInviteLinkDatabase {
	return &groupInviteLinkDatabase{db: db}
}

func (g *groupInviteLinkDatabase) Create(ctx context.Context, link *model.GroupInviteLink) error {
	return g.db.Create(ctx, link)
}

func (g *groupInviteLinkDatabase) Save(ctx context.Context, link *model.GroupInviteLink) error {
	return g.db.Save(ctx, link)
}

func (g *groupInviteLinkDatabase) GetByLinkID(ctx context.Context, linkID string) (*model.GroupInviteLink, error) {
	return g.db.GetByLinkID(ctx, linkID)
}

func (g *groupInviteLinkDatabase) GetByGroupID(ctx context.Context, groupID string) (*model.GroupInviteLink, error) {
	return g.db.GetByGroupID(ctx, groupID)
}

func (g *groupInviteLinkDatabase) IncrUsedCount(ctx context.Context, linkID string) error {
	return g.db.IncrUsedCount(ctx, linkID)
}

func (g *groupInviteLinkDatabase) Revoke(ctx context.Context, linkID string) error {
	return g.db.Revoke(ctx, linkID)
}

func (g *groupInviteLinkDatabase) DeleteByGroupID(ctx context.Context, groupID string) error {
	return g.db.DeleteByGroupID(ctx, groupID)
}

func (g *groupInviteLinkDatabase) ListByGroupID(ctx context.Context, groupID string, pg pagination.Pagination) (int64, []*model.GroupInviteLink, error) {
	return g.db.ListByGroupID(ctx, groupID, pg)
}
