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
