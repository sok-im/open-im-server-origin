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

package conversation

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
)

// checkGroupBurnPermission 校验当前操作者是否具备群会话阅后即焚设置权限。
// allowBurn=0（默认）仅群主可设置；allowBurn=1 时全员可设置。
func (c *conversationServer) checkGroupBurnPermission(ctx context.Context, groupID string) error {
	if groupID == "" {
		return nil
	}
	if authverify.IsAppManagerUid(ctx, c.config.Share.IMAdminUserID) {
		return nil
	}
	groupInfo, err := c.groupClient.GetGroupInfo(ctx, groupID)
	if err != nil {
		return err
	}
	if groupInfo.GetAllowBurn() == model.GroupAllowBurnAllMember {
		return nil
	}
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return errs.ErrNoPermission.WrapMsg("op user id empty")
	}
	member, err := c.groupClient.GetGroupMemberInfo(ctx, groupID, opUserID)
	if err != nil {
		return err
	}
	if member.RoleLevel == constant.GroupOwner {
		return nil
	}
	return errs.ErrNoPermission.WrapMsg("only group owner can set burn setting")
}

func (c *conversationServer) checkGroupBurnPermissionByConversation(ctx context.Context, conversationType int32, groupID string) error {
	if conversationType != constant.ReadGroupChatType && conversationType != constant.WriteGroupChatType {
		return nil
	}
	return c.checkGroupBurnPermission(ctx, groupID)
}
