package database

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

// GroupBlock persists per-user group chat message push block settings.
type GroupBlock interface {
	Upsert(ctx context.Context, block *model.GroupBlock) error
	Delete(ctx context.Context, ownerUserID, groupID string) error
	// DeleteByUserIDs deletes block docs for the given users in a group (kick/quit).
	DeleteByUserIDs(ctx context.Context, groupID string, ownerUserIDs []string) error
	// DeleteByGroupID deletes all block docs for a group (dismiss).
	DeleteByGroupID(ctx context.Context, groupID string) error
	// ListBlockedUserIDs returns which of candidateUserIDs currently block this group.
	ListBlockedUserIDs(ctx context.Context, groupID string, candidateUserIDs []string) ([]string, error)
	// ListGroupIDsByOwner returns group IDs the owner has blocked.
	ListGroupIDsByOwner(ctx context.Context, ownerUserID string) ([]string, error)
	// Get returns one document by owner + group; nil if not found.
	Get(ctx context.Context, ownerUserID, groupID string) (*model.GroupBlock, error)
}
