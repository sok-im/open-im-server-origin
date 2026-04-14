package crypto

import (
	"context"
	"fmt"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	pbcrypto "github.com/openimsdk/protocol/crypto"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"go.mongodb.org/mongo-driver/mongo"
)

// RegisterDevice creates a new E2EE device record. The Virgil Identity is
// derived as "{userID}:{deviceID}" and stored alongside the device metadata.
// If the device already exists (duplicate key), the existing record is returned
// to ensure idempotency.
func (s *cryptoServer) RegisterDevice(ctx context.Context, req *pbcrypto.RegisterDeviceReq) (*pbcrypto.RegisterDeviceResp, error) {
	if req.UserID == "" || req.DeviceID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID and deviceID are required")
	}

	now := time.Now().UnixMilli()
	virgilIdentity := fmt.Sprintf("%s:%s", req.UserID, req.DeviceID)

	device := &model.CryptoDevice{
		DeviceID:       req.DeviceID,
		UserID:         req.UserID,
		Platform:       req.Platform,
		DeviceModel:    req.DeviceModel,
		AppVersion:     req.AppVersion,
		VirgilIdentity: virgilIdentity,
		Status:         model.DeviceStatusActive,
		LastSeenAt:     now,
		CreateTime:     now,
	}

	if err := s.db.CreateDevice(ctx, device); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			existing, getErr := s.db.GetDeviceByID(ctx, req.DeviceID)
			if getErr != nil {
				return nil, errs.WrapMsg(getErr, "failed to retrieve existing device", "deviceID", req.DeviceID)
			}
			if existing.UserID != req.UserID {
				return nil, errs.ErrNoPermission.WrapMsg("device already registered by another user", "deviceID", req.DeviceID)
			}
			log.ZWarn(ctx, "RegisterDevice: duplicate device (idempotent retry)", err, "deviceID", req.DeviceID)
			return &pbcrypto.RegisterDeviceResp{Device: deviceModelToProto(existing)}, nil
		}
		return nil, errs.WrapMsg(err, "CreateDevice failed", "deviceID", req.DeviceID)
	}

	return &pbcrypto.RegisterDeviceResp{Device: deviceModelToProto(device)}, nil
}

// GetDevices returns all devices registered by the given user.
func (s *cryptoServer) GetDevices(ctx context.Context, req *pbcrypto.GetDevicesReq) (*pbcrypto.GetDevicesResp, error) {
	if req.UserID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID is required")
	}

	devices, err := s.db.GetDevicesByUserID(ctx, req.UserID)
	if err != nil {
		return nil, errs.WrapMsg(err, "GetDevicesByUserID failed", "userID", req.UserID)
	}

	pbDevices := make([]*pbcrypto.DeviceInfo, 0, len(devices))
	for _, d := range devices {
		pbDevices = append(pbDevices, deviceModelToProto(d))
	}
	return &pbcrypto.GetDevicesResp{Devices: pbDevices}, nil
}

// RevokeDevice marks a device as revoked so it can no longer obtain Virgil JWTs.
// This does not delete the record (audit trail), but prevents any further crypto
// operations for the device.
func (s *cryptoServer) RevokeDevice(ctx context.Context, req *pbcrypto.RevokeDeviceReq) (*pbcrypto.RevokeDeviceResp, error) {
	if req.UserID == "" || req.DeviceID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID and deviceID are required")
	}

	device, err := s.db.GetDeviceByID(ctx, req.DeviceID)
	if err != nil {
		return nil, errs.WrapMsg(err, "device not found", "deviceID", req.DeviceID)
	}
	if device.UserID != req.UserID {
		return nil, errs.ErrNoPermission.WrapMsg("device does not belong to user", "deviceID", req.DeviceID, "userID", req.UserID)
	}
	if device.Status == model.DeviceStatusRevoked {
		log.ZWarn(ctx, "RevokeDevice: device already revoked (idempotent)", nil, "deviceID", req.DeviceID)
		return &pbcrypto.RevokeDeviceResp{}, nil
	}

	if err := s.db.UpdateDeviceStatus(ctx, req.DeviceID, model.DeviceStatusRevoked); err != nil {
		return nil, errs.WrapMsg(err, "UpdateDeviceStatus failed", "deviceID", req.DeviceID)
	}

	log.ZInfo(ctx, "RevokeDevice: device revoked successfully", "deviceID", req.DeviceID, "userID", req.UserID)
	return &pbcrypto.RevokeDeviceResp{}, nil
}

// deviceModelToProto converts a storage model CryptoDevice to proto DeviceInfo.
func deviceModelToProto(d *model.CryptoDevice) *pbcrypto.DeviceInfo {
	if d == nil {
		return nil
	}
	return &pbcrypto.DeviceInfo{
		DeviceID:       d.DeviceID,
		UserID:         d.UserID,
		Platform:       d.Platform,
		DeviceModel:    d.DeviceModel,
		AppVersion:     d.AppVersion,
		VirgilIdentity: d.VirgilIdentity,
		Status:         d.Status,
		LastSeenAt:     d.LastSeenAt,
		CreateTime:     d.CreateTime,
	}
}
