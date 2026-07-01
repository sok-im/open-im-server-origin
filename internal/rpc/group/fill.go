// Copyright © 2023 OpenIM. All rights reserved.
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

package group

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	relationtb "github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
)

func (s *groupServer) PopulateGroupMember(ctx context.Context, members ...*relationtb.GroupMember) error {
	return s.notification.PopulateGroupMember(ctx, members...)
}

func (s *groupServer) membersToPbWithDisplayNicknames(ctx context.Context, members []*relationtb.GroupMember) ([]*sdkws.GroupMemberFullInfo, error) {
	pbMembers := datautil.Slice(members, func(e *relationtb.GroupMember) *sdkws.GroupMemberFullInfo {
		return convert.Db2PbGroupMember(e)
	})
	if err := s.applyMemberDisplayNicknames(ctx, pbMembers); err != nil {
		return nil, err
	}
	return pbMembers, nil
}

// applyMemberDisplayNicknames 按当前用户视角重设群成员 Nickname：
// remark > 好友备注名 firstName+lastName > 用户资料 firstName+lastName > nickname。
func (s *groupServer) applyMemberDisplayNicknames(ctx context.Context, members []*sdkws.GroupMemberFullInfo) error {
	if len(members) == 0 {
		return nil
	}
	viewerID := mcontext.GetOpUserID(ctx)
	if viewerID == "" {
		return nil
	}
	userIDs := datautil.Slice(members, func(m *sdkws.GroupMemberFullInfo) string { return m.UserID })
	users, err := s.userClient.GetUsersInfoMap(ctx, userIDs)
	if err != nil {
		return err
	}
	friendInfos, err := s.relationClient.GetFriendsInfo(ctx, viewerID, userIDs)
	if err != nil {
		return err
	}
	aliasMap := convert.FriendAliasMapFromFriendInfos(friendInfos)
	for _, m := range members {
		alias := aliasMap[m.UserID]
		m.Nickname = convert.DisplayNicknameForFriend(alias.Remark, alias.FriendFirstName, alias.FriendLastName, users[m.UserID])
	}
	return nil
}

// friendAliasMapForUser 返回 viewerUserID 对 userIDs 的好友别名映射（非好友无条目）。
func (s *groupServer) friendAliasMapForUser(ctx context.Context, viewerUserID string, userIDs []string) (map[string]convert.FriendAliasInfo, error) {
	if viewerUserID == "" || len(userIDs) == 0 {
		return map[string]convert.FriendAliasInfo{}, nil
	}
	friendInfos, err := s.relationClient.GetFriendsInfo(ctx, viewerUserID, userIDs)
	if err != nil {
		return nil, err
	}
	return convert.FriendAliasMapFromFriendInfos(friendInfos), nil
}

// userInfoWithDisplayNickname 复制用户信息并将 Nickname 设为 remark > 好友备注名 > 用户资料名 > nickname。
func userInfoWithDisplayNickname(user *sdkws.UserInfo, aliasMap map[string]convert.FriendAliasInfo) *sdkws.UserInfo {
	if user == nil {
		return nil
	}
	if aliasMap == nil {
		aliasMap = map[string]convert.FriendAliasInfo{}
	}
	cp := *user
	alias := aliasMap[user.UserID]
	cp.Nickname = convert.DisplayNicknameForFriend(alias.Remark, alias.FriendFirstName, alias.FriendLastName, user)
	return &cp
}
