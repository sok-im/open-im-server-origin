package controller

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

type CryptoDatabase interface {
	CreateDevice(ctx context.Context, device *model.CryptoDevice) error
	GetDeviceByID(ctx context.Context, deviceID string) (*model.CryptoDevice, error)
	GetDevicesByUserID(ctx context.Context, userID string) ([]*model.CryptoDevice, error)
	UpdateDeviceStatus(ctx context.Context, deviceID string, status string) error
	UpdateDeviceLastSeen(ctx context.Context, deviceID string, lastSeenAt int64) error

	AtomicBumpGroupKeyVersion(ctx context.Context, groupID string) (int64, error)
	CreateGroupKeyEvent(ctx context.Context, event *model.GroupKeyEvent) error
	GetLatestGroupKeyVersion(ctx context.Context, groupID string) (int64, error)
	GetGroupKeyEventsSince(ctx context.Context, groupID string, sinceVersion int64) ([]*model.GroupKeyEvent, error)
}

type cryptoDatabase struct {
	db database.CryptoDatabase
}

func NewCryptoDatabase(db database.CryptoDatabase) CryptoDatabase {
	return &cryptoDatabase{db: db}
}

func (c *cryptoDatabase) CreateDevice(ctx context.Context, device *model.CryptoDevice) error {
	return c.db.CreateDevice(ctx, device)
}

func (c *cryptoDatabase) GetDeviceByID(ctx context.Context, deviceID string) (*model.CryptoDevice, error) {
	return c.db.GetDeviceByID(ctx, deviceID)
}

func (c *cryptoDatabase) GetDevicesByUserID(ctx context.Context, userID string) ([]*model.CryptoDevice, error) {
	return c.db.GetDevicesByUserID(ctx, userID)
}

func (c *cryptoDatabase) UpdateDeviceStatus(ctx context.Context, deviceID string, status string) error {
	return c.db.UpdateDeviceStatus(ctx, deviceID, status)
}

func (c *cryptoDatabase) UpdateDeviceLastSeen(ctx context.Context, deviceID string, lastSeenAt int64) error {
	return c.db.UpdateDeviceLastSeen(ctx, deviceID, lastSeenAt)
}

func (c *cryptoDatabase) AtomicBumpGroupKeyVersion(ctx context.Context, groupID string) (int64, error) {
	return c.db.AtomicBumpGroupKeyVersion(ctx, groupID)
}

func (c *cryptoDatabase) CreateGroupKeyEvent(ctx context.Context, event *model.GroupKeyEvent) error {
	return c.db.CreateGroupKeyEvent(ctx, event)
}

func (c *cryptoDatabase) GetLatestGroupKeyVersion(ctx context.Context, groupID string) (int64, error) {
	return c.db.GetLatestGroupKeyVersion(ctx, groupID)
}

func (c *cryptoDatabase) GetGroupKeyEventsSince(ctx context.Context, groupID string, sinceVersion int64) ([]*model.GroupKeyEvent, error) {
	return c.db.GetGroupKeyEventsSince(ctx, groupID, sinceVersion)
}
