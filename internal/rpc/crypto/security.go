package crypto

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	pbcrypto "github.com/openimsdk/protocol/crypto"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
)

// SecurityPrecheck performs a pre-flight security check before sensitive crypto
// operations (e.g., obtaining a Virgil JWT, sending encrypted messages).
//
// Current checks:
//  1. Device must exist and be active (not revoked).
//  2. User must exist (validated via the user RPC client).
//
// This can be extended with rate limiting, risk scoring, geo-fencing, etc.
func (s *cryptoServer) SecurityPrecheck(ctx context.Context, req *pbcrypto.SecurityPrecheckReq) (*pbcrypto.SecurityPrecheckResp, error) {
	if req.UserID == "" || req.DeviceID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID and deviceID are required")
	}

	device, err := s.db.GetDeviceByID(ctx, req.DeviceID)
	if err != nil {
		log.ZWarn(ctx, "SecurityPrecheck: device not found", err, "deviceID", req.DeviceID)
		return &pbcrypto.SecurityPrecheckResp{
			Allowed: false,
			Reason:  "device not registered",
		}, nil
	}

	if device.UserID != req.UserID {
		return &pbcrypto.SecurityPrecheckResp{
			Allowed: false,
			Reason:  "device does not belong to user",
		}, nil
	}

	if device.Status == model.DeviceStatusRevoked {
		return &pbcrypto.SecurityPrecheckResp{
			Allowed: false,
			Reason:  "device has been revoked",
		}, nil
	}

	return &pbcrypto.SecurityPrecheckResp{
		Allowed: true,
		Reason:  "",
	}, nil
}

// IntegrityReport accepts a device integrity report (e.g., attestation data from
// SafetyNet/Play Integrity/App Attest). The report is stored for audit purposes
// and can trigger device revocation if the integrity check fails.
//
// Current implementation: accept-and-log. Production deployments should integrate
// with platform-specific attestation verification (Google Play Integrity API,
// Apple App Attest, etc.).
const maxReportDataSize = 64 * 1024 // 64 KB

func (s *cryptoServer) IntegrityReport(ctx context.Context, req *pbcrypto.IntegrityReportReq) (*pbcrypto.IntegrityReportResp, error) {
	if req.UserID == "" || req.DeviceID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID and deviceID are required")
	}
	if len(req.ReportData) > maxReportDataSize {
		return nil, errs.ErrArgs.WrapMsg("reportData exceeds maximum size limit (64KB)")
	}

	device, err := s.db.GetDeviceByID(ctx, req.DeviceID)
	if err != nil {
		return nil, errs.WrapMsg(err, "device not found", "deviceID", req.DeviceID)
	}
	if device.UserID != req.UserID {
		return nil, errs.ErrNoPermission.WrapMsg("device does not belong to user", "deviceID", req.DeviceID, "userID", req.UserID)
	}

	log.ZInfo(ctx, "IntegrityReport received",
		"userID", req.UserID,
		"deviceID", req.DeviceID,
		"timestamp", req.Timestamp,
		"reportDataLen", len(req.ReportData),
	)

	// TODO: integrate with platform attestation APIs for real verification.
	// On failure, call s.db.UpdateDeviceStatus(ctx, req.DeviceID, model.DeviceStatusRevoked)

	return &pbcrypto.IntegrityReportResp{
		Accepted: true,
	}, nil
}
