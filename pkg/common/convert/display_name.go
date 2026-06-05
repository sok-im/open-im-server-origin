// Copyright © 2023 OpenIM. All rights reserved.

package convert

import (
	"strings"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/relation"
	"github.com/openimsdk/protocol/sdkws"
)

// DisplayNickname 按统一优先级返回展示名：remark > firstName+lastName > nickname。
func DisplayNickname(remark string, user *sdkws.UserInfo) string {
	if strings.TrimSpace(remark) != "" {
		return remark
	}
	return MemberDisplayNickname(user)
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

// DisplayNicknameForUser 根据 remark 映射与用户资料解析展示名。
func DisplayNicknameForUser(userID string, users map[string]*sdkws.UserInfo, remarkMap map[string]string) string {
	if userID == "" {
		return ""
	}
	return DisplayNickname(remarkMap[userID], users[userID])
}
