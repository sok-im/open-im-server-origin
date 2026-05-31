// Copyright © 2026 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/tx"
	"github.com/openimsdk/tools/errs"
)

// VirgilSecurityDatabase 提供 1v1 E2EE 服务所需的全部存储原子操作组合。
type VirgilSecurityDatabase interface {
	// device
	RegisterDevice(ctx context.Context, userID, deviceID, cardID, platform, clientVersion, idempotencyKey string) (*model.VirgilDevice, bool, error)
	GetDevices(ctx context.Context, userID string, includeRevoked bool) ([]*model.VirgilDevice, int64, error)
	GetDevice(ctx context.Context, userID, deviceID string) (*model.VirgilDevice, error)
	RevokeDevice(ctx context.Context, userID, deviceID, reason string) (*model.VirgilDevice, error)
	TouchDevice(ctx context.Context, userID, deviceID string) error

	// conversation
	EnsureOneToOneConversation(ctx context.Context, userA, userB string) (*model.VirgilConversation, error)

	// events
	GetEventsSince(ctx context.Context, userID string, sinceVersion int64, limit int64) ([]*model.VirgilDeviceEvent, int64, error)

	// files
	CreateFileRef(ctx context.Context, ref *model.VirgilFileRef) error
}

type virgilSecurityDatabase struct {
	deviceDB  database.VirgilDevice
	versionDB database.VirgilUserDeviceVersion
	eventDB   database.VirgilDeviceEvent
	convDB    database.VirgilConversation
	fileRefDB database.VirgilFileRef
	tx        tx.Tx
}

func NewVirgilSecurityDatabase(
	deviceDB database.VirgilDevice,
	versionDB database.VirgilUserDeviceVersion,
	eventDB database.VirgilDeviceEvent,
	convDB database.VirgilConversation,
	fileRefDB database.VirgilFileRef,
	tx tx.Tx,
) VirgilSecurityDatabase {
	return &virgilSecurityDatabase{
		deviceDB:  deviceDB,
		versionDB: versionDB,
		eventDB:   eventDB,
		convDB:    convDB,
		fileRefDB: fileRefDB,
		tx:        tx,
	}
}

func (v *virgilSecurityDatabase) RegisterDevice(ctx context.Context, userID, deviceID, cardID, platform, clientVersion, idempotencyKey string) (*model.VirgilDevice, bool, error) {
	if idempotencyKey != "" {
		existing, err := v.deviceDB.FindByIdempotency(ctx, userID, idempotencyKey)
		if err == nil {
			if existing.DeviceID == deviceID && existing.CardID == cardID {
				return existing, true, nil
			}
			return nil, false, errs.ErrArgs.WrapMsg("idempotency key reused with different payload")
		} else if !errs.ErrRecordNotFound.Is(err) {
			return nil, false, err
		}
	}

	if existing, err := v.deviceDB.FindOne(ctx, userID, deviceID); err == nil {
		if existing.CardID != cardID {
			return nil, false, errs.ErrArgs.WrapMsg("device already bound to a different cardID")
		}
		now := time.Now()
		existing.Status = "active"
		existing.Platform = platform
		existing.ClientVersion = clientVersion
		existing.LastSeenAt = now
		existing.UpdateTime = now
		existing.IdempotencyKey = idempotencyKey
		if err := v.deviceDB.Upsert(ctx, existing); err != nil {
			return nil, false, err
		}
		return existing, true, nil
	} else if !errs.ErrRecordNotFound.Is(err) {
		return nil, false, err
	}

	now := time.Now()
	device := &model.VirgilDevice{
		UserID:         userID,
		DeviceID:       deviceID,
		CardID:         cardID,
		Platform:       platform,
		ClientVersion:  clientVersion,
		Status:         "active",
		LastSeenAt:     now,
		CreateTime:     now,
		UpdateTime:     now,
		IdempotencyKey: idempotencyKey,
	}

	if err := v.tx.Transaction(ctx, func(ctx context.Context) error {
		if err := v.deviceDB.Upsert(ctx, device); err != nil {
			return err
		}
		version, err := v.versionDB.IncrVersion(ctx, userID)
		if err != nil {
			return err
		}
		return v.eventDB.Append(ctx, &model.VirgilDeviceEvent{
			EventID:    uuid.NewString(),
			UserID:     userID,
			Version:    version,
			Type:       "device_changed",
			DeviceID:   deviceID,
			Status:     "active",
			CreateTime: now,
		})
	}); err != nil {
		return nil, false, err
	}

	return device, false, nil
}

func (v *virgilSecurityDatabase) GetDevices(ctx context.Context, userID string, includeRevoked bool) ([]*model.VirgilDevice, int64, error) {
	devices, err := v.deviceDB.FindByUserID(ctx, userID, includeRevoked)
	if err != nil {
		return nil, 0, err
	}
	version, err := v.versionDB.GetVersion(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	return devices, version, nil
}

func (v *virgilSecurityDatabase) GetDevice(ctx context.Context, userID, deviceID string) (*model.VirgilDevice, error) {
	return v.deviceDB.FindOne(ctx, userID, deviceID)
}

func (v *virgilSecurityDatabase) RevokeDevice(ctx context.Context, userID, deviceID, reason string) (*model.VirgilDevice, error) {
	now := time.Now()
	existing, err := v.deviceDB.FindOne(ctx, userID, deviceID)
	if err != nil {
		return nil, err
	}
	if existing.Status == "revoked" {
		// 已撤销则幂等返回当前记录，不再写事件。
		return existing, nil
	}

	if err := v.tx.Transaction(ctx, func(ctx context.Context) error {
		if err := v.deviceDB.UpdateStatus(ctx, userID, deviceID, "revoked", reason); err != nil {
			return err
		}
		version, err := v.versionDB.IncrVersion(ctx, userID)
		if err != nil {
			return err
		}
		return v.eventDB.Append(ctx, &model.VirgilDeviceEvent{
			EventID:    uuid.NewString(),
			UserID:     userID,
			Version:    version,
			Type:       "device_changed",
			DeviceID:   deviceID,
			Status:     "revoked",
			CreateTime: now,
		})
	}); err != nil {
		return nil, err
	}

	existing.Status = "revoked"
	existing.RevokedAt = now
	existing.RevokeReason = reason
	existing.UpdateTime = now
	return existing, nil
}

func (v *virgilSecurityDatabase) TouchDevice(ctx context.Context, userID, deviceID string) error {
	return v.deviceDB.UpdateLastSeen(ctx, userID, deviceID)
}

func (v *virgilSecurityDatabase) EnsureOneToOneConversation(ctx context.Context, userA, userB string) (*model.VirgilConversation, error) {
	return v.convDB.EnsureOneToOne(ctx, userA, userB)
}

func (v *virgilSecurityDatabase) GetEventsSince(ctx context.Context, userID string, sinceVersion int64, limit int64) ([]*model.VirgilDeviceEvent, int64, error) {
	events, err := v.eventDB.FindSince(ctx, userID, sinceVersion, limit)
	if err != nil {
		return nil, 0, err
	}
	version, err := v.versionDB.GetVersion(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	return events, version, nil
}

func (v *virgilSecurityDatabase) CreateFileRef(ctx context.Context, ref *model.VirgilFileRef) error {
	return v.fileRefDB.Create(ctx, ref)
}
