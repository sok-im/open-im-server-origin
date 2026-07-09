package database

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/pagination"
)

// GroupInviteLink 群邀请链接数据库操作接口（每群仅一条记录）。
type GroupInviteLink interface {
	// Create 插入一条新邀请链接记录。
	Create(ctx context.Context, link *model.GroupInviteLink) error
	// Save 按 group_id 插入或覆盖群唯一邀请链接。
	Save(ctx context.Context, link *model.GroupInviteLink) error
	// GetByLinkID 按 linkID 查询一条记录；不存在时返回 ErrRecordNotFound。
	GetByLinkID(ctx context.Context, linkID string) (*model.GroupInviteLink, error)
	// GetByGroupID 按群 ID 查询唯一邀请链接；不存在时返回 ErrRecordNotFound。
	GetByGroupID(ctx context.Context, groupID string) (*model.GroupInviteLink, error)
	// IncrUsedCount 原子性地将 used_count 加 1。
	IncrUsedCount(ctx context.Context, linkID string) error
	// Revoke 将指定链接标记为已吊销（revoked=true）。
	Revoke(ctx context.Context, linkID string) error
	// DeleteByGroupID 删除指定群的邀请链接；不存在时不报错。
	DeleteByGroupID(ctx context.Context, groupID string) error
	// ListByGroupID 按群 ID 分页查询邀请链接（每群最多一条，兼容 list 接口）。
	ListByGroupID(ctx context.Context, groupID string, pg pagination.Pagination) (int64, []*model.GroupInviteLink, error)
}
