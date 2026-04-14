package database

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

// CryptoDatabase defines storage operations for the E2EE crypto service.
type CryptoDatabase interface {
	// Device management
	CreateDevice(ctx context.Context, device *model.CryptoDevice) error
	GetDeviceByID(ctx context.Context, deviceID string) (*model.CryptoDevice, error)
	GetDevicesByUserID(ctx context.Context, userID string, limit int64) ([]*model.CryptoDevice, error)
	CountDevicesByUserID(ctx context.Context, userID string) (int64, error)
	UpdateDeviceStatus(ctx context.Context, deviceID string, status string) error
	UpdateDeviceLastSeen(ctx context.Context, deviceID string, lastSeenAt int64) error
	AtomicRevokeDevice(ctx context.Context, deviceID string, userID string) (bool, error)

	// Group key version & events
	AtomicBumpGroupKeyVersion(ctx context.Context, groupID string) (int64, error)
	CreateGroupKeyEvent(ctx context.Context, event *model.GroupKeyEvent) error
	GetLatestGroupKeyVersion(ctx context.Context, groupID string) (int64, error)
	GetGroupKeyEventsSince(ctx context.Context, groupID string, sinceVersion int64, limit int64) ([]*model.GroupKeyEvent, error)
}
