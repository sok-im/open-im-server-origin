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

package rpcli

import (
	"context"

	pbopenmls "github.com/openimsdk/protocol/openmls"
	"github.com/openimsdk/tools/log"
	"google.golang.org/grpc"
)

func NewOpenMLSClient(cc grpc.ClientConnInterface) *OpenMLSClient {
	return &OpenMLSClient{pbopenmls.NewOpenMLSServiceClient(cc)}
}

type OpenMLSClient struct {
	pbopenmls.OpenMLSServiceClient
}

// DeleteGroup purges the MLS group state and all commit history for the given
// group.  It is a fire-and-return call: errors are logged but do not propagate
// to the caller, so that group-service operations (DismissGroup) are not
// blocked by MLS state cleanup failures.
func (x *OpenMLSClient) DeleteGroup(ctx context.Context, groupID string) {
	_, err := x.OpenMLSServiceClient.DeleteGroup(ctx, &pbopenmls.DeleteGroupReq{GroupID: groupID})
	if err != nil {
		log.ZError(ctx, "OpenMLSClient.DeleteGroup failed", err, "groupID", groupID)
		return
	}
	log.ZDebug(ctx, "OpenMLSClient.DeleteGroup success", "groupID", groupID)
}

// InitGroupTrigger notifies the creator's devices to start the MLS
// group-creation flow for a newly created OpenIM group. It is a
// fire-and-return call: errors are logged but do not block the Group RPC.
func (x *OpenMLSClient) InitGroupTrigger(ctx context.Context, groupID, creatorUserID string, memberUserIDs []string) {
	_, err := x.OpenMLSServiceClient.InitGroupTrigger(ctx, &pbopenmls.InitGroupTriggerReq{
		GroupID:       groupID,
		CreatorUserID: creatorUserID,
		MemberUserIDs: memberUserIDs,
	})
	if err != nil {
		log.ZError(ctx, "OpenMLSClient.InitGroupTrigger failed", err,
			"groupID", groupID, "creatorUserID", creatorUserID)
		return
	}
	log.ZDebug(ctx, "OpenMLSClient.InitGroupTrigger success",
		"groupID", groupID, "creatorUserID", creatorUserID)
}

// AddMemberTrigger notifies the operator's devices to perform the MLS
// Add-Commit + Welcome flow after new members are invited to an OpenIM group.
// It is a fire-and-return call: errors are logged but do not block the Group RPC.
func (x *OpenMLSClient) AddMemberTrigger(ctx context.Context, groupID, operatorUserID string, newMemberUserIDs []string) {
	_, err := x.OpenMLSServiceClient.AddMemberTrigger(ctx, &pbopenmls.AddMemberTriggerReq{
		GroupID:          groupID,
		OperatorUserID:   operatorUserID,
		NewMemberUserIDs: newMemberUserIDs,
	})
	if err != nil {
		log.ZError(ctx, "OpenMLSClient.AddMemberTrigger failed", err,
			"groupID", groupID, "operatorUserID", operatorUserID)
		return
	}
	log.ZDebug(ctx, "OpenMLSClient.AddMemberTrigger success",
		"groupID", groupID, "operatorUserID", operatorUserID, "newMemberCount", len(newMemberUserIDs))
}

// RemoveMemberTrigger notifies the operator's devices to perform the MLS
// Remove-Commit flow after members are kicked from an OpenIM group, rotating
// the group epoch so removed members lose forward secrecy. It is a
// fire-and-return call: errors are logged but do not block the Group RPC.
func (x *OpenMLSClient) RemoveMemberTrigger(ctx context.Context, groupID, operatorUserID string, removedMemberUserIDs []string) {
	_, err := x.OpenMLSServiceClient.RemoveMemberTrigger(ctx, &pbopenmls.RemoveMemberTriggerReq{
		GroupID:              groupID,
		OperatorUserID:       operatorUserID,
		RemovedMemberUserIDs: removedMemberUserIDs,
	})
	if err != nil {
		log.ZError(ctx, "OpenMLSClient.RemoveMemberTrigger failed", err,
			"groupID", groupID, "operatorUserID", operatorUserID)
		return
	}
	log.ZDebug(ctx, "OpenMLSClient.RemoveMemberTrigger success",
		"groupID", groupID, "operatorUserID", operatorUserID, "removedMemberCount", len(removedMemberUserIDs))
}
