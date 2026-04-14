package crypto

import (
	"context"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	pbcrypto "github.com/openimsdk/protocol/crypto"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
)

// GetVirgilJWT validates the requesting device and signs a Virgil JWT so the
// client can interact with Virgil Cloud (publish/fetch cards, etc.).
//
// Security invariants:
//   - The device must belong to the requesting user.
//   - The device must be in "active" status (not revoked).
//   - The JWT identity is "{userID}:{deviceID}" to scope keys per-device.
func (s *cryptoServer) GetVirgilJWT(ctx context.Context, req *pbcrypto.GetVirgilJWTReq) (*pbcrypto.GetVirgilJWTResp, error) {
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
	if device.Status != model.DeviceStatusActive {
		return nil, errs.ErrNoPermission.WrapMsg("device is revoked", "deviceID", req.DeviceID)
	}

	virgilIdentity := fmt.Sprintf("%s:%s", req.UserID, req.DeviceID)
	ttl := s.config.RpcConfig.Virgil.TokenTTL
	if ttl <= 0 {
		ttl = 3600
	}

	jwt, err := s.signVirgilJWT(virgilIdentity, time.Duration(ttl)*time.Second)
	if err != nil {
		log.ZError(ctx, "signVirgilJWT failed", err, "identity", virgilIdentity)
		return nil, errs.WrapMsg(err, "failed to sign Virgil JWT")
	}

	// Update last seen timestamp asynchronously (best-effort)
	now := time.Now().UnixMilli()
	if updateErr := s.db.UpdateDeviceLastSeen(ctx, req.DeviceID, now); updateErr != nil {
		log.ZWarn(ctx, "UpdateDeviceLastSeen failed (non-fatal)", updateErr, "deviceID", req.DeviceID)
	}

	return &pbcrypto.GetVirgilJWTResp{
		VirgilJWT:      jwt,
		ExpiresIn:      int64(ttl),
		VirgilIdentity: virgilIdentity,
	}, nil
}

// virgilJWTHeader is the fixed header for Virgil JWTs.
type virgilJWTHeader struct {
	Algorithm   string `json:"alg"`
	Type        string `json:"typ"`
	ContentType string `json:"cty"`
	APIKeyID    string `json:"kid"`
}

// virgilJWTBody is the payload for Virgil JWTs.
type virgilJWTBody struct {
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

// signVirgilJWT creates a Virgil JWT signed with the pre-parsed API key (s.virgilPrivKey).
// The JWT format follows Virgil's specification: header.body.signature (base64url-encoded).
func (s *cryptoServer) signVirgilJWT(identity string, ttl time.Duration) (string, error) {
	cfg := s.config.RpcConfig.Virgil

	now := time.Now()
	header := virgilJWTHeader{
		Algorithm:   "VEDS512",
		Type:        "JWT",
		ContentType: "virgil-jwt;v=1",
		APIKeyID:    cfg.APIKeyID,
	}
	body := virgilJWTBody{
		Issuer:    fmt.Sprintf("virgil-%s", cfg.AppID),
		Subject:   fmt.Sprintf("identity-%s", identity),
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("marshal header: %w", err)
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal body: %w", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	bodyB64 := base64.RawURLEncoding.EncodeToString(bodyJSON)

	unsigned := headerB64 + "." + bodyB64
	// Virgil VEDS512: EdDSA signs SHA512(content) per Virgil JWT spec
	contentHash := sha512.Sum512([]byte(unsigned))
	sig := ed25519.Sign(s.virgilPrivKey, contentHash[:])
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return unsigned + "." + sigB64, nil
}

// newRequestID generates a unique request/event ID.
func newRequestID() string {
	return uuid.New().String()
}
