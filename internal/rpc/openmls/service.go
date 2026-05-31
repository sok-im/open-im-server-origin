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

package openmls

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	pbopenmls "github.com/openimsdk/protocol/openmls"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
)

// credentialPayload 是 MLS Credential 的内容结构（JSON 序列化后再签名）。
type credentialPayload struct {
	Identity   string `json:"identity"`
	LeafPubKey string `json:"leaf_pub_key"`
	IssuedAt   int64  `json:"issued_at"`
	ExpiresAt  int64  `json:"expires_at"`
	Issuer     string `json:"issuer"`
}

// credentialEnvelope 是编码后存储的 Credential 外层结构。
type credentialEnvelope struct {
	Payload string `json:"payload"` // base64(JSON credentialPayload)
	Sig     string `json:"sig"`     // base64(Ed25519 signature)
}

// ==================== KeyPackage ====================

func (s *openMLSServer) UploadKeyPackage(ctx context.Context, req *pbopenmls.UploadKeyPackageReq) (*pbopenmls.UploadKeyPackageResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}
	if req.UserID == "" || req.DeviceID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID and deviceID are required")
	}
	if req.UserID != opUserID {
		return nil, errs.ErrNoPermission.WrapMsg("userID mismatch with token")
	}
	if req.KeyPackage == "" {
		return nil, errs.ErrArgs.WrapMsg("keyPackage is required")
	}
	if _, err := base64.StdEncoding.DecodeString(req.KeyPackage); err != nil {
		return nil, errs.ErrArgs.WrapMsg("keyPackage must be valid base64")
	}

	// Check per-device limit
	maxKP := s.config.RpcConfig.MaxKeyPackagesPerDevice
	if maxKP <= 0 {
		maxKP = 20
	}
	count, err := s.db.CountByDevice(ctx, req.UserID, req.DeviceID)
	if err != nil {
		return nil, err
	}
	if int(count) >= maxKP {
		return nil, errs.New("max KeyPackage limit reached").Wrap()
	}

	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if req.ExpiresAt > 0 {
		expiresAt = time.Unix(req.ExpiresAt, 0)
	}

	kp := &model.MLSKeyPackage{
		KpID:           uuid.New().String(),
		UserID:         req.UserID,
		DeviceID:       req.DeviceID,
		Platform:       req.Platform,
		KeyPackage:     req.KeyPackage,
		Ciphersuite:    req.Ciphersuite,
		CredentialType: req.CredentialType,
		Consumed:       false,
		CreatedAt:      time.Now(),
		ExpiresAt:      expiresAt,
	}
	if err := s.db.Insert(ctx, kp); err != nil {
		return nil, err
	}

	total, err := s.db.CountByDevice(ctx, req.UserID, req.DeviceID)
	if err != nil {
		return nil, err
	}

	return &pbopenmls.UploadKeyPackageResp{
		KpID:       kp.KpID,
		TotalCount: total,
	}, nil
}

func (s *openMLSServer) GetKeyPackages(ctx context.Context, req *pbopenmls.GetKeyPackagesReq) (*pbopenmls.GetKeyPackagesResp, error) {
	if req.UserID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID is required")
	}

	countPerDevice := int(req.CountPerDevice)
	if countPerDevice <= 0 {
		countPerDevice = 1
	}

	kps, err := s.db.ConsumeByUserID(ctx, req.UserID, req.ExcludeDeviceID, countPerDevice)
	if err != nil {
		return nil, err
	}

	items := make([]*pbopenmls.KeyPackageItem, len(kps))
	for i, kp := range kps {
		items[i] = &pbopenmls.KeyPackageItem{
			KpID:       kp.KpID,
			DeviceID:   kp.DeviceID,
			Platform:   kp.Platform,
			KeyPackage: kp.KeyPackage,
			Consumed:   kp.Consumed,
		}
	}

	return &pbopenmls.GetKeyPackagesResp{
		UserID:      req.UserID,
		KeyPackages: items,
	}, nil
}

func (s *openMLSServer) GetKeyPackageCount(ctx context.Context, req *pbopenmls.GetKeyPackageCountReq) (*pbopenmls.GetKeyPackageCountResp, error) {
	if req.UserID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID is required")
	}

	counts, err := s.db.CountByUserID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	var total int32
	devices := make([]*pbopenmls.DeviceCount, 0, len(counts))
	for deviceID, cnt := range counts {
		devices = append(devices, &pbopenmls.DeviceCount{
			DeviceID:       deviceID,
			AvailableCount: cnt,
		})
		total += cnt
	}

	return &pbopenmls.GetKeyPackageCountResp{
		UserID:         req.UserID,
		Devices:        devices,
		TotalAvailable: total,
	}, nil
}

func (s *openMLSServer) RefreshKeyPackages(ctx context.Context, req *pbopenmls.RefreshKeyPackagesReq) (*pbopenmls.RefreshKeyPackagesResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}
	if req.UserID != opUserID {
		return nil, errs.ErrNoPermission.WrapMsg("userID mismatch with token")
	}
	if req.UserID == "" || req.DeviceID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID and deviceID are required")
	}
	if len(req.KeyPackages) == 0 {
		return nil, errs.ErrArgs.WrapMsg("keyPackages must not be empty")
	}

	maxKP := s.config.RpcConfig.MaxKeyPackagesPerDevice
	if maxKP <= 0 {
		maxKP = 20
	}

	existing, err := s.db.CountByDevice(ctx, req.UserID, req.DeviceID)
	if err != nil {
		return nil, err
	}
	remaining := maxKP - int(existing)
	if remaining <= 0 {
		return nil, errs.New("max KeyPackage limit reached").Wrap()
	}
	toInsert := req.KeyPackages
	if len(toInsert) > remaining {
		toInsert = toInsert[:remaining]
	}

	kps := make([]*model.MLSKeyPackage, len(toInsert))
	now := time.Now()
	expiresAt := now.Add(30 * 24 * time.Hour)
	for i, kpB64 := range toInsert {
		if _, err := base64.StdEncoding.DecodeString(kpB64); err != nil {
			return nil, errs.ErrArgs.WrapMsg("keyPackages[" + string(rune(i+'0')) + "] must be valid base64")
		}
		kps[i] = &model.MLSKeyPackage{
			KpID:        uuid.New().String(),
			UserID:      req.UserID,
			DeviceID:    req.DeviceID,
			KeyPackage:  kpB64,
			Consumed:    false,
			CreatedAt:   now,
			ExpiresAt:   expiresAt,
		}
	}

	if err := s.db.BatchInsert(ctx, kps); err != nil {
		return nil, err
	}

	total, err := s.db.CountByDevice(ctx, req.UserID, req.DeviceID)
	if err != nil {
		return nil, err
	}

	return &pbopenmls.RefreshKeyPackagesResp{
		UploadedCount: int32(len(kps)),
		TotalCount:    total,
	}, nil
}

// ==================== Group Commit ====================

func (s *openMLSServer) SubmitCommit(ctx context.Context, req *pbopenmls.SubmitCommitReq) (*pbopenmls.SubmitCommitResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}
	if req.CommitMessage == "" {
		return nil, errs.ErrArgs.WrapMsg("commitMessage is required")
	}
	if _, err := base64.StdEncoding.DecodeString(req.CommitMessage); err != nil {
		return nil, errs.ErrArgs.WrapMsg("commitMessage must be valid base64")
	}

	// Ensure group state exists (create if first commit)
	_, err := s.db.GetState(ctx, req.GroupID)
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			if err := s.db.UpsertState(ctx, &model.MLSGroupState{
				GroupID:      req.GroupID,
				CurrentEpoch: 0,
				MemberCount:  1,
				CreatedAt:    time.Now(),
				LastCommitAt: time.Now(),
			}); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	// Optimistic concurrency: increment epoch
	newEpoch, err := s.db.IncrEpoch(ctx, req.GroupID, req.FromEpoch)
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			return nil, errs.New("epoch conflict: current epoch does not match fromEpoch").Wrap()
		}
		return nil, err
	}

	// Persist commit record
	seqNum := time.Now().UnixMilli()
	commit := &model.MLSCommit{
		ID:             uuid.New().String(),
		GroupID:        req.GroupID,
		Epoch:          newEpoch,
		SequenceNumber: seqNum,
		CommitMessage:  req.CommitMessage,
		SenderUserID:   req.SenderUserID,
		SenderDeviceID: req.SenderDeviceID,
		CreatedAt:      time.Now(),
	}
	if err := s.db.AppendCommit(ctx, commit); err != nil {
		return nil, err
	}

	return &pbopenmls.SubmitCommitResp{
		NewEpoch:       newEpoch,
		SequenceNumber: seqNum,
		BroadcastCount: 0,
	}, nil
}

func (s *openMLSServer) GetCommits(ctx context.Context, req *pbopenmls.GetCommitsReq) (*pbopenmls.GetCommitsResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
	}

	commits, err := s.db.FindSinceEpoch(ctx, req.GroupID, req.SinceEpoch, limit)
	if err != nil {
		return nil, err
	}

	state, err := s.db.GetState(ctx, req.GroupID)
	if err != nil && !errs.ErrRecordNotFound.Is(err) {
		return nil, err
	}
	var currentEpoch uint64
	if state != nil {
		currentEpoch = state.CurrentEpoch
	}

	records := make([]*pbopenmls.CommitRecord, len(commits))
	for i, c := range commits {
		records[i] = &pbopenmls.CommitRecord{
			Epoch:          c.Epoch,
			SequenceNumber: c.SequenceNumber,
			CommitMessage:  c.CommitMessage,
			SenderUserID:   c.SenderUserID,
			CreatedAt:      c.CreatedAt.Unix(),
		}
	}

	hasMore := len(commits) == limit
	return &pbopenmls.GetCommitsResp{
		GroupID:      req.GroupID,
		Commits:      records,
		CurrentEpoch: currentEpoch,
		HasMore:      hasMore,
	}, nil
}

func (s *openMLSServer) SendWelcome(ctx context.Context, req *pbopenmls.SendWelcomeReq) (*pbopenmls.SendWelcomeResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}
	if len(req.Recipients) == 0 {
		return nil, errs.ErrArgs.WrapMsg("recipients must not be empty")
	}
	// v1: record count only; Welcome is delivered by caller via msg_gateway directly
	return &pbopenmls.SendWelcomeResp{
		DeliveredCount: int32(len(req.Recipients)),
	}, nil
}

func (s *openMLSServer) GetGroupState(ctx context.Context, req *pbopenmls.GetGroupStateReq) (*pbopenmls.GetGroupStateResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}

	state, err := s.db.GetState(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	return &pbopenmls.GetGroupStateResp{
		GroupID:      state.GroupID,
		CurrentEpoch: state.CurrentEpoch,
		MemberCount:  state.MemberCount,
		LastCommitAt: state.LastCommitAt.Unix(),
		CreatedAt:    state.CreatedAt.Unix(),
	}, nil
}

func (s *openMLSServer) DeleteGroup(ctx context.Context, req *pbopenmls.DeleteGroupReq) (*pbopenmls.DeleteGroupResp, error) {
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID is required")
	}

	if err := s.db.DeleteGroup(ctx, req.GroupID); err != nil {
		return nil, err
	}
	if err := s.db.DeleteByGroupID(ctx, req.GroupID); err != nil {
		return nil, err
	}

	return &pbopenmls.DeleteGroupResp{}, nil
}

// ==================== Credential ====================

func (s *openMLSServer) IssueCredential(ctx context.Context, req *pbopenmls.IssueCredentialReq) (*pbopenmls.IssueCredentialResp, error) {
	if s.signingKey == nil {
		return nil, errs.New("credential issuing is not configured (signingKey is empty)").Wrap()
	}

	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}
	if req.UserID != opUserID {
		return nil, errs.ErrNoPermission.WrapMsg("userID mismatch with token")
	}
	if req.UserID == "" || req.DeviceID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID and deviceID are required")
	}
	if req.LeafPublicKey == "" {
		return nil, errs.ErrArgs.WrapMsg("leafPublicKey is required")
	}
	if _, err := base64.StdEncoding.DecodeString(req.LeafPublicKey); err != nil {
		return nil, errs.ErrArgs.WrapMsg("leafPublicKey must be valid base64")
	}

	now := time.Now()
	expiresAt := now.Add(30 * 24 * time.Hour)

	identity := req.UserID + ":" + req.DeviceID + ":" + req.Platform
	payload := credentialPayload{
		Identity:   identity,
		LeafPubKey: req.LeafPublicKey,
		IssuedAt:   now.Unix(),
		ExpiresAt:  expiresAt.Unix(),
		Issuer:     s.config.RpcConfig.SigningKey.Issuer,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, errs.New("marshal credential payload").Wrap()
	}

	sig := ed25519.Sign(s.signingKey, payloadBytes)
	envelope := credentialEnvelope{
		Payload: base64.StdEncoding.EncodeToString(payloadBytes),
		Sig:     base64.StdEncoding.EncodeToString(sig),
	}
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		return nil, errs.New("marshal credential envelope").Wrap()
	}
	credentialB64 := base64.StdEncoding.EncodeToString(envelopeBytes)

	return &pbopenmls.IssueCredentialResp{
		Credential:     credentialB64,
		CredentialType: "basic",
		IssuedAt:       now.Unix(),
		ExpiresAt:      expiresAt.Unix(),
		Issuer:         s.config.RpcConfig.SigningKey.Issuer,
	}, nil
}

func (s *openMLSServer) VerifyCredential(ctx context.Context, req *pbopenmls.VerifyCredentialReq) (*pbopenmls.VerifyCredentialResp, error) {
	if s.signingKey == nil {
		return nil, errs.New("credential verification is not configured (signingKey is empty)").Wrap()
	}
	if req.Credential == "" {
		return nil, errs.ErrArgs.WrapMsg("credential is required")
	}

	envelopeBytes, err := base64.StdEncoding.DecodeString(req.Credential)
	if err != nil {
		return &pbopenmls.VerifyCredentialResp{Valid: false}, nil
	}

	var envelope credentialEnvelope
	if err := json.Unmarshal(envelopeBytes, &envelope); err != nil {
		return &pbopenmls.VerifyCredentialResp{Valid: false}, nil
	}

	payloadBytes, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil {
		return &pbopenmls.VerifyCredentialResp{Valid: false}, nil
	}
	sig, err := base64.StdEncoding.DecodeString(envelope.Sig)
	if err != nil {
		return &pbopenmls.VerifyCredentialResp{Valid: false}, nil
	}

	pubKey := s.signingKey.Public().(ed25519.PublicKey)
	if !ed25519.Verify(pubKey, payloadBytes, sig) {
		return &pbopenmls.VerifyCredentialResp{Valid: false}, nil
	}

	var payload credentialPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return &pbopenmls.VerifyCredentialResp{Valid: false}, nil
	}

	if time.Now().Unix() > payload.ExpiresAt {
		return &pbopenmls.VerifyCredentialResp{Valid: false}, nil
	}

	// Parse identity: userID:deviceID:platform
	var userID, deviceID string
	parts := splitIdentity(payload.Identity)
	if len(parts) >= 2 {
		userID = parts[0]
		deviceID = parts[1]
	}

	return &pbopenmls.VerifyCredentialResp{
		Valid:     true,
		UserID:    userID,
		DeviceID:  deviceID,
		ExpiresAt: payload.ExpiresAt,
	}, nil
}

func (s *openMLSServer) GetRootPublicKey(ctx context.Context, req *pbopenmls.GetRootPublicKeyReq) (*pbopenmls.GetRootPublicKeyResp, error) {
	if s.rootPubKey == "" {
		return nil, errs.New("signing key not configured").Wrap()
	}
	keyID := s.config.RpcConfig.SigningKey.KeyID
	if keyID == "" {
		keyID = "v1"
	}
	return &pbopenmls.GetRootPublicKeyResp{
		PublicKey: s.rootPubKey,
		KeyID:     keyID,
		Algorithm: "Ed25519",
	}, nil
}

// splitIdentity splits "userID:deviceID:platform" by the first two colons.
func splitIdentity(identity string) []string {
	var parts []string
	start := 0
	colons := 0
	for i := 0; i < len(identity) && colons < 2; i++ {
		if identity[i] == ':' {
			parts = append(parts, identity[start:i])
			start = i + 1
			colons++
		}
	}
	parts = append(parts, identity[start:])
	return parts
}
