// Copyright © 2024 OpenIM. All rights reserved.
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

package cache

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

// CallStatusCache manages per-user audio/video call state in Redis.
//
// State machine (per user):
//
//	Invite sent / received  →  SetCallStatus(CallStatusConnecting)
//	Invitee accepts         →  SetCallStatus(CallStatusInCall)   for both parties
//	Call ends (any path)    →  DeleteCallStatus
type CallStatusCache interface {
	// SetCallStatus writes or overwrites the call status for a single user.
	// The entry is automatically expired after cachekey.CallStatusExpire.
	SetCallStatus(ctx context.Context, userID string, status *model.UserCallStatus) error

	// GetCallStatus retrieves the current call status for a user.
	// Returns errs.ErrRecordNotFound if no entry exists.
	GetCallStatus(ctx context.Context, userID string) (*model.UserCallStatus, error)

	// DeleteCallStatus removes call-status entries for one or more users.
	// Missing keys are silently ignored.
	DeleteCallStatus(ctx context.Context, userIDs ...string) error
}
