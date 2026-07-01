// Copyright © 2023 OpenIM. All rights reserved.

package convert

import (
	"strings"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/relation"
	"github.com/openimsdk/protocol/sdkws"
)

// FriendAliasInfo holds per-friend display alias fields owned by the viewer.
type FriendAliasInfo struct {
	Remark          string
	FriendFirstName string
	FriendLastName  string
}

// DisplayNicknameForFriend 按统一优先级返回展示名：remark > 好友备注名 firstName+lastName > 用户资料 firstName+lastName > nickname。
func DisplayNicknameForFriend(remark, friendFirstName, friendLastName string, user *sdkws.UserInfo) string {
	if strings.TrimSpace(remark) != "" {
		return remark
	}
	if name := BuildFullName(friendFirstName, friendLastName); name != "" {
		return name
	}
	return MemberDisplayNickname(user)
}

// DisplayNickname 无好友别名时按用户资料解析展示名；完整规则请使用 DisplayNicknameForFriend。
func DisplayNickname(remark string, user *sdkws.UserInfo) string {
	return DisplayNicknameForFriend(remark, "", "", user)
}

// FriendAliasMapFromFriendModels 从好友记录构建 friendUserID -> 别名信息映射。
func FriendAliasMapFromFriendModels(friends []*model.Friend) map[string]FriendAliasInfo {
	m := make(map[string]FriendAliasInfo, len(friends))
	for _, f := range friends {
		if f == nil {
			continue
		}
		m[f.FriendUserID] = FriendAliasInfo{
			Remark:          f.Remark,
			FriendFirstName: f.FriendFirstName,
			FriendLastName:  f.FriendLastName,
		}
	}
	return m
}

// FriendAliasMapFromFriendInfos 从好友信息构建 friendUserID -> 别名信息映射。
func FriendAliasMapFromFriendInfos(friends []*relation.FriendInfoOnly) map[string]FriendAliasInfo {
	m := make(map[string]FriendAliasInfo, len(friends))
	for _, f := range friends {
		if f == nil {
			continue
		}
		m[f.FriendUserID] = FriendAliasInfo{
			Remark:          f.Remark,
			FriendFirstName: f.FirstName,
			FriendLastName:  f.LastName,
		}
	}
	return m
}

// RemarkMapFromFriendModels 从好友记录构建 friendUserID -> remark 映射。
func RemarkMapFromFriendModels(friends []*model.Friend) map[string]string {
	m := make(map[string]string, len(friends))
	for _, f := range friends {
		if f != nil && f.Remark != "" {
			m[f.FriendUserID] = f.Remark
		}
	}
	return m
}

// RemarkMapFromFriendInfos 从好友信息构建 friendUserID -> remark 映射。
func RemarkMapFromFriendInfos(friends []*relation.FriendInfoOnly) map[string]string {
	m := make(map[string]string, len(friends))
	for _, f := range friends {
		if f != nil && f.Remark != "" {
			m[f.FriendUserID] = f.Remark
		}
	}
	return m
}

// DisplayNicknameForUser 根据别名映射与用户资料解析展示名。
func DisplayNicknameForUser(userID string, users map[string]*sdkws.UserInfo, aliasMap map[string]FriendAliasInfo) string {
	if userID == "" {
		return ""
	}
	alias := aliasMap[userID]
	return DisplayNicknameForFriend(alias.Remark, alias.FriendFirstName, alias.FriendLastName, users[userID])
}
