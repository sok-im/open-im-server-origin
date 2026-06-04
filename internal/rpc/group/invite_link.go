package group

import (
	"context"
	"math/rand"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/callbackstruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/constant"
	pbgroup "github.com/openimsdk/protocol/group"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
)

const inviteLinkChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// genLinkID 生成 8 位随机字母数字短码。
func genLinkID() string {
	b := make([]byte, 8)
	for i := range b {
		b[i] = inviteLinkChars[rand.Intn(len(inviteLinkChars))]
	}
	return string(b)
}

// inviteLinkToProto 将 model 转换为 proto 消息。
func inviteLinkToProto(m *model.GroupInviteLink) *pbgroup.GroupInviteLinkInfo {
	return &pbgroup.GroupInviteLinkInfo{
		LinkID:      m.LinkID,
		GroupID:     m.GroupID,
		CreatorID:   m.CreatorID,
		ExpireAt:    m.ExpireAt,
		MaxUseCount: m.MaxUseCount,
		UsedCount:   m.UsedCount,
		Revoked:     m.Revoked,
		CreatedAt:   m.CreatedAt.UnixMilli(),
	}
}

// isGroupInviteLinkEnabled 判断群是否已开启邀请链接功能。
func isGroupInviteLinkEnabled(group *model.Group) bool {
	return group.EnableInviteLink == model.GroupEnableInviteLinkOn
}

// isLinkValid 判断链接是否仍然有效。
func isLinkValid(link *model.GroupInviteLink) bool {
	if link.Revoked {
		return false
	}
	if link.ExpireAt != 0 && time.Now().UnixMilli() > link.ExpireAt {
		return false
	}
	if link.MaxUseCount != 0 && link.UsedCount >= link.MaxUseCount {
		return false
	}
	return true
}

// CreateGroupInviteLink 为指定群生成一条邀请链接（仅群主/管理员可创建）。
func (s *groupServer) CreateGroupInviteLink(ctx context.Context, req *pbgroup.CreateGroupInviteLinkReq) (*pbgroup.CreateGroupInviteLinkResp, error) {
	if err := s.CheckGroupAdmin(ctx, req.GroupID); err != nil {
		return nil, err
	}

	group, err := s.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if !isGroupInviteLinkEnabled(group) {
		return nil, errs.ErrNoPermission.WrapMsg("group invite link is disabled")
	}

	cfg := s.config.RpcConfig.InviteLink

	// 计算过期时间：请求值 > 0 则使用请求值，否则使用配置默认值；0 表示永不过期。
	expireSeconds := req.ExpireSeconds
	if expireSeconds == 0 && cfg.DefaultExpireSeconds > 0 {
		expireSeconds = int64(cfg.DefaultExpireSeconds)
	}
	if cfg.MaxExpireSeconds > 0 && expireSeconds > int64(cfg.MaxExpireSeconds) {
		return nil, errs.ErrArgs.WrapMsg("expireSeconds exceeds maximum allowed", "max", cfg.MaxExpireSeconds)
	}

	var expireAt int64
	if expireSeconds > 0 {
		expireAt = time.Now().Add(time.Duration(expireSeconds) * time.Second).UnixMilli()
	}

	// 计算最大使用次数。
	maxUseCount := req.MaxUseCount
	if maxUseCount == 0 && cfg.DefaultMaxUseCount > 0 {
		maxUseCount = int32(cfg.DefaultMaxUseCount)
	}
	if cfg.MaxUseCountCap > 0 && maxUseCount > int32(cfg.MaxUseCountCap) {
		return nil, errs.ErrArgs.WrapMsg("maxUseCount exceeds maximum allowed", "max", cfg.MaxUseCountCap)
	}

	link := &model.GroupInviteLink{
		LinkID:      genLinkID(),
		GroupID:     req.GroupID,
		CreatorID:   mcontext.GetOpUserID(ctx),
		ExpireAt:    expireAt,
		MaxUseCount: maxUseCount,
		UsedCount:   0,
		Revoked:     false,
		CreatedAt:   time.Now(),
	}

	if err := s.inviteLinkDB.Create(ctx, link); err != nil {
		return nil, err
	}

	return &pbgroup.CreateGroupInviteLinkResp{
		Link: inviteLinkToProto(link),
	}, nil
}

// GetGroupInviteLink 查询邀请链接信息及群预览（不需要身份验证，允许未登录用户预览）。
func (s *groupServer) GetGroupInviteLink(ctx context.Context, req *pbgroup.GetGroupInviteLinkReq) (*pbgroup.GetGroupInviteLinkResp, error) {
	link, err := s.inviteLinkDB.GetByLinkID(ctx, req.LinkID)
	if err != nil {
		return nil, err
	}

	groupInfos, err := s.getGroupsInfo(ctx, []string{link.GroupID})
	if err != nil {
		return nil, err
	}
	valid := isLinkValid(link)
	if len(groupInfos) > 0 && groupInfos[0].GetEnableInviteLink() != model.GroupEnableInviteLinkOn {
		valid = false
	}
	resp := &pbgroup.GetGroupInviteLinkResp{
		Link:  inviteLinkToProto(link),
		Valid: valid,
	}
	if len(groupInfos) > 0 {
		resp.GroupInfo = groupInfos[0]
	}
	return resp, nil
}

// JoinGroupByInviteLink 用户通过群分享/邀请链接入群（需要已登录）。
// 是否需审批仅由群的 needVerification 决定：Directly(2) 直接入群，0/1 创建入群申请。
func (s *groupServer) JoinGroupByInviteLink(ctx context.Context, req *pbgroup.JoinGroupByInviteLinkReq) (*pbgroup.JoinGroupByInviteLinkResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)

	link, err := s.inviteLinkDB.GetByLinkID(ctx, req.LinkID)
	if err != nil {
		return nil, err
	}
	if !isLinkValid(link) {
		return nil, errs.ErrArgs.WrapMsg("invite link is invalid (expired, revoked, or usage limit reached)")
	}

	group, err := s.db.TakeGroup(ctx, link.GroupID)
	if err != nil {
		return nil, err
	}
	if group.Status == constant.GroupStatusDismissed {
		return nil, servererrs.ErrDismissedAlready.Wrap()
	}
	if !isGroupInviteLinkEnabled(group) {
		return nil, errs.ErrNoPermission.WrapMsg("group invite link is disabled")
	}

	// 校验调用者是否已经是群成员。
	_, memberErr := s.db.TakeGroupMember(ctx, link.GroupID, opUserID)
	if memberErr == nil {
		return nil, errs.ErrArgs.WrapMsg("already a group member")
	}
	if !s.IsNotFound(memberErr) && errs.Unwrap(memberErr) != errs.ErrRecordNotFound {
		return nil, memberErr
	}

	// 在真正入群/创建申请之前先递增使用次数，防止并发超限。
	if err := s.inviteLinkDB.IncrUsedCount(ctx, link.LinkID); err != nil {
		return nil, err
	}

	joinReq := &pbgroup.JoinGroupReq{
		GroupID:       link.GroupID,
		ReqMessage:    req.ReqMessage,
		JoinSource:    constant.JoinByInviteLink,
		InviterUserID: opUserID,
	}
	reqCall := &callbackstruct.CallbackJoinGroupReq{
		GroupID:    joinReq.GroupID,
		GroupType:  string(group.GroupType),
		ApplyID:    joinReq.InviterUserID,
		ReqMessage: joinReq.ReqMessage,
		Ex:         joinReq.Ex,
	}
	if err := s.webhookBeforeApplyJoinGroup(ctx, &s.config.WebhooksConfig.BeforeApplyJoinGroup, reqCall); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}
	if group.NeedVerification == constant.Directly {
		if err := s.joinGroupDirectly(ctx, group, joinReq); err != nil {
			return nil, err
		}
	} else {
		if err := s.createJoinGroupApplication(ctx, joinReq); err != nil {
			return nil, err
		}
	}

	return &pbgroup.JoinGroupByInviteLinkResp{}, nil
}

// RevokeGroupInviteLink 吊销指定邀请链接（仅群主/管理员可操作）。
func (s *groupServer) RevokeGroupInviteLink(ctx context.Context, req *pbgroup.RevokeGroupInviteLinkReq) (*pbgroup.RevokeGroupInviteLinkResp, error) {
	if err := s.CheckGroupAdmin(ctx, req.GroupID); err != nil {
		return nil, err
	}

	link, err := s.inviteLinkDB.GetByLinkID(ctx, req.LinkID)
	if err != nil {
		return nil, err
	}
	if link.GroupID != req.GroupID {
		return nil, errs.ErrNoPermission.WrapMsg("link does not belong to this group")
	}

	if err := s.inviteLinkDB.Revoke(ctx, req.LinkID); err != nil {
		return nil, err
	}

	return &pbgroup.RevokeGroupInviteLinkResp{}, nil
}

// ListGroupInviteLinks 分页查询群内所有邀请链接（仅群主/管理员可查看）。
func (s *groupServer) ListGroupInviteLinks(ctx context.Context, req *pbgroup.ListGroupInviteLinksReq) (*pbgroup.ListGroupInviteLinksResp, error) {
	if !authverify.IsAppManagerUid(ctx, s.config.Share.IMAdminUserID) {
		if err := s.CheckGroupAdmin(ctx, req.GroupID); err != nil {
			return nil, err
		}
	}

	total, links, err := s.inviteLinkDB.ListByGroupID(ctx, req.GroupID, req.Pagination)
	if err != nil {
		return nil, err
	}

	protoLinks := make([]*pbgroup.GroupInviteLinkInfo, 0, len(links))
	for _, l := range links {
		protoLinks = append(protoLinks, inviteLinkToProto(l))
	}

	return &pbgroup.ListGroupInviteLinksResp{
		Total: uint32(total),
		Links: protoLinks,
	}, nil
}
