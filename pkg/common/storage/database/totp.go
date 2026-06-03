package database

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

// UserTotp manages the persistent TOTP binding for each user.
type UserTotp interface {
	// Get returns the TOTP binding for the given user; returns ErrRecordNotFound if unbound.
	Get(ctx context.Context, userID string) (*model.UserTotp, error)
	// Upsert inserts or replaces the TOTP binding document.
	Upsert(ctx context.Context, doc *model.UserTotp) error
	// Delete removes the TOTP binding for the given user.
	Delete(ctx context.Context, userID string) error
}

// UserTotpRecovery manages one-time recovery codes for each user.
type UserTotpRecovery interface {
	// InsertMany bulk-inserts recovery code documents (called once at bind time).
	InsertMany(ctx context.Context, docs []*model.UserTotpRecovery) error
	// FindUnused returns all unused recovery code documents for the user.
	FindUnused(ctx context.Context, userID string) ([]*model.UserTotpRecovery, error)
	// MarkUsed marks the document with the given ID as used at the given Unix timestamp.
	MarkUsed(ctx context.Context, id interface{}, usedAt int64) error
	// CountUnused returns the number of unused recovery codes for the user.
	CountUnused(ctx context.Context, userID string) (int64, error)
	// DeleteByUser removes all recovery code documents for the user.
	DeleteByUser(ctx context.Context, userID string) error
}
