package database

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

type WalletBackupInfo interface {
	Upsert(ctx context.Context, info *model.WalletBackupInfo) error
	// GetByUID 按 uid 查询；无记录时返回 (nil, nil)
	GetByUID(ctx context.Context, uid string) (*model.WalletBackupInfo, error)
}
