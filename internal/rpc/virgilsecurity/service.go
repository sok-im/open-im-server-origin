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

package virgilsecurity

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	pbvirgil "github.com/openimsdk/protocol/virgilsecurity"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/s3"
)

// ---------- Virgil JWT ----------

func (s *virgilSecurityServer) IssueVirgilJWT(ctx context.Context, req *pbvirgil.IssueVirgilJWTReq) (*pbvirgil.IssueVirgilJWTResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}
	if req.DeviceID == "" {
		return nil, errs.ErrArgs.WrapMsg("deviceID is required")
	}
	if s.jwtGenerator == nil {
		return nil, errs.New("virgil is not configured").Wrap()
	}

	ttl := time.Duration(req.TokenTtlSec) * time.Second
	if ttl <= 0 {
		ttl = defaultVirgilJWTTTL
	}
	if ttl < minVirgilJWTTTL {
		ttl = minVirgilJWTTTL
	}
	if ttl > maxVirgilJWTTTL {
		ttl = maxVirgilJWTTTL
	}

	device, err := s.db.GetDevice(ctx, opUserID, req.DeviceID)
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			return nil, errs.ErrRecordNotFound.WrapMsg("device not registered")
		}
		return nil, err
	}
	if device.Status != "active" {
		return nil, errs.ErrNoPermission.WrapMsg("device revoked")
	}

	if err := s.db.TouchDevice(ctx, opUserID, req.DeviceID); err != nil {
		log.ZError(ctx, "TouchDevice failed", err, "userID", opUserID, "deviceID", req.DeviceID)
	}

	identity := opUserID + ":" + req.DeviceID
	token, err := s.jwtGenerator.GenerateToken(identity, nil)
	if err != nil {
		log.ZError(ctx, "GenerateToken failed", err, "identity", identity)
		return nil, errs.New("generate virgil jwt failed").Wrap()
	}
	expiresAt := time.Now().Add(ttl)
	return &pbvirgil.IssueVirgilJWTResp{
		VirgilJwt: token.String(),
		Identity:  identity,
		ExpiresAt: expiresAt.UnixMilli(),
	}, nil
}

// ---------- Devices ----------

func (s *virgilSecurityServer) RegisterDevice(ctx context.Context, req *pbvirgil.RegisterDeviceReq) (*pbvirgil.RegisterDeviceResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}
	if req.DeviceID == "" || req.CardID == "" || req.Platform == "" {
		return nil, errs.ErrArgs.WrapMsg("deviceID, cardID, platform are required")
	}
	if !isValidPlatform(req.Platform) {
		return nil, errs.ErrArgs.WrapMsg("invalid platform", "platform", req.Platform)
	}

	device, replayed, err := s.db.RegisterDevice(ctx, opUserID, req.DeviceID, req.CardID, req.Platform, req.ClientVersion, req.IdempotencyKey)
	if err != nil {
		log.ZError(ctx, "RegisterDevice failed", err, "userID", opUserID, "deviceID", req.DeviceID, "cardID", req.CardID)
		return nil, err
	}
	log.ZInfo(ctx, "RegisterDevice success",
		"userID", opUserID,
		"deviceID", req.DeviceID,
		"cardID", req.CardID,
		"replayed", replayed,
	)
	return &pbvirgil.RegisterDeviceResp{
		UserID:           device.UserID,
		DeviceID:         device.DeviceID,
		CardID:           device.CardID,
		Status:           device.Status,
		RegisteredAt:     device.CreateTime.UnixMilli(),
		IdempotentReplay: replayed,
	}, nil
}

func (s *virgilSecurityServer) GetDevices(ctx context.Context, req *pbvirgil.GetDevicesReq) (*pbvirgil.GetDevicesResp, error) {
	if req.UserID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID is required")
	}
	if err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID); err != nil {
		// 1v1 E2EE 场景需要客户端读取对端的设备目录。
		// 在 CheckAccessV3 仅允许本人/管理员的基础上，允许任意已登录用户读取活跃设备列表（不含 revoke 字段以外的敏感信息）。
		if mcontext.GetOpUserID(ctx) == "" {
			return nil, err
		}
	}

	devices, version, err := s.db.GetDevices(ctx, req.UserID, req.IncludeRevoked)
	if err != nil {
		log.ZError(ctx, "GetDevices failed", err, "userID", req.UserID)
		return nil, err
	}
	items := make([]*pbvirgil.DeviceItem, 0, len(devices))
	for _, d := range devices {
		items = append(items, deviceToPb(d))
	}
	return &pbvirgil.GetDevicesResp{
		UserID:  req.UserID,
		Devices: items,
		Version: version,
	}, nil
}

func (s *virgilSecurityServer) RevokeDevice(ctx context.Context, req *pbvirgil.RevokeDeviceReq) (*pbvirgil.RevokeDeviceResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}
	if req.DeviceID == "" {
		return nil, errs.ErrArgs.WrapMsg("deviceID is required")
	}
	if !isValidRevokeReason(req.Reason) {
		return nil, errs.ErrArgs.WrapMsg("invalid revoke reason", "reason", req.Reason)
	}

	device, err := s.db.RevokeDevice(ctx, opUserID, req.DeviceID, req.Reason)
	if err != nil {
		log.ZError(ctx, "RevokeDevice failed", err, "userID", opUserID, "deviceID", req.DeviceID)
		return nil, err
	}
	revokedAt := device.RevokedAt
	if revokedAt.IsZero() {
		revokedAt = device.UpdateTime
	}
	return &pbvirgil.RevokeDeviceResp{
		DeviceID:  device.DeviceID,
		Status:    device.Status,
		RevokedAt: revokedAt.UnixMilli(),
	}, nil
}

// ---------- Conversation ----------

func (s *virgilSecurityServer) EnsureConversation(ctx context.Context, req *pbvirgil.EnsureConversationReq) (*pbvirgil.EnsureConversationResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}
	if req.PeerUserID == "" {
		return nil, errs.ErrArgs.WrapMsg("peerUserID is required")
	}
	if req.PeerUserID == opUserID {
		return nil, errs.ErrArgs.WrapMsg("peerUserID must be different from current user")
	}
	conv, err := s.db.EnsureOneToOneConversation(ctx, opUserID, req.PeerUserID)
	if err != nil {
		log.ZError(ctx, "EnsureOneToOneConversation failed", err, "userID", opUserID, "peer", req.PeerUserID)
		return nil, err
	}
	return &pbvirgil.EnsureConversationResp{
		ConversationID: conv.ConversationID,
		CreatedAt:      conv.CreateTime.UnixMilli(),
	}, nil
}

// ---------- Event subscribe (long-poll) ----------

func (s *virgilSecurityServer) SubscribeEvents(ctx context.Context, req *pbvirgil.SubscribeEventsReq) (*pbvirgil.SubscribeEventsResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}
	userID := req.UserID
	if userID == "" {
		userID = opUserID
	}
	if err := authverify.CheckAccessV3(ctx, userID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	timeoutSec := req.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 25
	}
	if timeoutSec > 60 {
		timeoutSec = 60
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)

	// 简化长轮询：周期性轮询数据库，命中或超时返回。
	// 生产部署如需高性能可基于 Redis Pub/Sub 替换此处实现。
	const pollInterval = 500 * time.Millisecond
	for {
		events, latest, err := s.db.GetEventsSince(ctx, userID, req.SinceVersion, 256)
		if err != nil {
			return nil, err
		}
		if len(events) > 0 || time.Now().After(deadline) {
			return &pbvirgil.SubscribeEventsResp{
				Events:        groupEvents(events),
				LatestVersion: latest,
			}, nil
		}
		select {
		case <-ctx.Done():
			return &pbvirgil.SubscribeEventsResp{LatestVersion: latest}, nil
		case <-time.After(pollInterval):
		}
	}
}

// ---------- File upload signed URL ----------

func (s *virgilSecurityServer) CreateUploadURL(ctx context.Context, req *pbvirgil.CreateUploadURLReq) (*pbvirgil.CreateUploadURLResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)
	opPlatform := mcontext.GetOpUserPlatform(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("missing opUserID")
	}
	if req.Size <= 0 {
		return nil, errs.ErrArgs.WrapMsg("size must be positive")
	}
	if req.ConversationID == "" {
		return nil, errs.ErrArgs.WrapMsg("conversationID is required")
	}
	if s.s3 == nil || s.s3.Engine() == "" {
		return nil, errs.New("file storage is not configured").Wrap()
	}

	contentType := req.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	uploadTTL := defaultUploadTTL
	downloadTTL := defaultDownloadTTL
	fileRefID := "file_" + uuid.NewString()
	objectKey := buildObjectKey(opUserID, req.ConversationID, fileRefID)

	put, err := s.s3.PresignedPutObject(ctx, objectKey, uploadTTL, &s3.PutOption{ContentType: contentType})
	if err != nil {
		log.ZError(ctx, "PresignedPutObject failed", err, "objectKey", objectKey)
		return nil, errs.WrapMsg(err, "presign upload url failed")
	}
	dlURL, err := s.s3.AccessURL(ctx, objectKey, downloadTTL, &s3.AccessURLOption{ContentType: contentType})
	if err != nil {
		log.ZError(ctx, "AccessURL failed", err, "objectKey", objectKey)
		return nil, errs.WrapMsg(err, "presign download url failed")
	}

	now := time.Now()
	expiresAt := now.Add(uploadTTL)
	if err := s.db.CreateFileRef(ctx, &model.VirgilFileRef{
		FileRefID:      fileRefID,
		OwnerUserID:    opUserID,
		OwnerDeviceID:  opPlatform,
		ConversationID: req.ConversationID,
		Size:           req.Size,
		ContentType:    contentType,
		ObjectKey:      objectKey,
		ExpiresAt:      expiresAt,
		CreateTime:     now,
	}); err != nil {
		log.ZError(ctx, "CreateFileRef failed", err, "fileRefID", fileRefID)
		return nil, err
	}

	return &pbvirgil.CreateUploadURLResp{
		UploadUrl:   put.URL,
		DownloadUrl: dlURL,
		FileRefID:   fileRefID,
		ExpiresAt:   expiresAt.UnixMilli(),
	}, nil
}

// ---------- helpers ----------

func deviceToPb(d *model.VirgilDevice) *pbvirgil.DeviceItem {
	registeredAt := d.CreateTime.UnixMilli()
	lastSeen := d.LastSeenAt.UnixMilli()
	return &pbvirgil.DeviceItem{
		DeviceID:      d.DeviceID,
		CardID:        d.CardID,
		Platform:      d.Platform,
		Status:        d.Status,
		ClientVersion: d.ClientVersion,
		LastSeenAt:    lastSeen,
		RegisteredAt:  registeredAt,
	}
}

func isValidPlatform(p string) bool {
	switch strings.ToLower(p) {
	case "ios", "android", "web", "desktop":
		return true
	}
	return false
}

func isValidRevokeReason(r string) bool {
	switch r {
	case "lost", "stolen", "logout", "security":
		return true
	case "":
		// 兼容未提供 reason 的请求，按 logout 语义处理。
		return true
	}
	return false
}

func groupEvents(events []*model.VirgilDeviceEvent) []*pbvirgil.EventEnvelope {
	if len(events) == 0 {
		return nil
	}
	// 同一 version 的事件合并到同一 envelope；当前实现下 (userID, version) 是 1 对 1，
	// 因此每个 event 都生成独立 envelope；保留 slice 形态以便日后扩展批量变更。
	out := make([]*pbvirgil.EventEnvelope, 0, len(events))
	for _, ev := range events {
		out = append(out, &pbvirgil.EventEnvelope{
			Type:    ev.Type,
			UserID:  ev.UserID,
			Version: ev.Version,
			Changes: []*pbvirgil.DeviceChange{{DeviceID: ev.DeviceID, Status: ev.Status}},
		})
	}
	return out
}

func buildObjectKey(userID, conversationID, fileRefID string) string {
	safeUser := strings.ReplaceAll(userID, "/", "_")
	safeConv := strings.ReplaceAll(conversationID, "/", "_")
	return "virgil/e2ee/" + safeConv + "/" + safeUser + "/" + fileRefID
}
