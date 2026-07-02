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

package database

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/pagination"
)

// SignalDatabase defines storage operations for RTC signaling.
type SignalDatabase interface {
	// CreateInvitation stores a new signal invitation (called when invite is initiated).
	CreateInvitation(ctx context.Context, inv *model.SignalInvitation) error
	// GetInvitationByRoomID retrieves an invitation by roomID.
	GetInvitationByRoomID(ctx context.Context, roomID string) (*model.SignalInvitation, error)
	// GetInvitationByInviteeUserID retrieves the most recent pending invitation for a user.
	GetInvitationByInviteeUserID(ctx context.Context, userID string) (*model.SignalInvitation, error)
	// GetInvitationByUserID retrieves the most recent invitation where the user is
	// the inviter or appears in invitee_user_id_list.
	GetInvitationByUserID(ctx context.Context, userID string) (*model.SignalInvitation, error)
	// DeleteInvitation removes an invitation record when the call ends.
	DeleteInvitation(ctx context.Context, roomID string) error
	// TryDeleteInvitation atomically removes one invitation by roomID.
	// Returns true when a document was deleted (first caller wins for end-of-call dedup).
	TryDeleteInvitation(ctx context.Context, roomID string) (bool, error)
	// RemoveInvitee removes a single user from the invitee list via $pull;
	// if the list becomes empty the document is deleted automatically.
	RemoveInvitee(ctx context.Context, roomID string, userID string) error
	// PullInvitee removes a single user from the invitee list via $pull without
	// auto-deleting the invitation record when the list becomes empty.
	// Use this when the call is still ongoing (e.g. a participant leaves mid-call)
	// so the invitation survives for TryDeleteInvitation to claim when the call ends.
	PullInvitee(ctx context.Context, roomID string, userID string) error
	// AddInvitee appends a user to the invitee list (e.g. user joined without prior invite).
	AddInvitee(ctx context.Context, roomID string, userID string) error
	// SetAcceptTime records the first accept timestamp for a call (no-op if already set).
	SetAcceptTime(ctx context.Context, roomID string, acceptTime int64) error
	// GetInvitationByGroupID retrieves the active invitation for a group.
	GetInvitationByGroupID(ctx context.Context, groupID string) (*model.SignalInvitation, error)
	// GetInvitationsByRoomIDs retrieves invitations for the given room IDs.
	GetInvitationsByRoomIDs(ctx context.Context, roomIDs []string) ([]*model.SignalInvitation, error)
	// ListAllInvitations returns every current invitation, bounded by limit, for
	// the call-watchdog background scan. The active-invitation collection is
	// expected to stay small relative to the online user count.
	ListAllInvitations(ctx context.Context, limit int64) ([]*model.SignalInvitation, error)
	// GetBusyUserIDs returns the subset of userIDs that are currently involved in an active call
	// (either as inviter or as invitee in a pending invitation).
	GetBusyUserIDs(ctx context.Context, userIDs []string) ([]string, error)

	// CreateRecord stores a completed call record.
	CreateRecord(ctx context.Context, record *model.SignalRecord) error
	// SearchRecords returns paginated call history filtered by sender/receiver and time range.
	SearchRecords(ctx context.Context, sendID, recvID string, sessionType int32, startTime, endTime int64, pagination pagination.Pagination) (int64, []*model.SignalRecord, error)
	// DeleteRecords removes call history entries by their SIDs.
	DeleteRecords(ctx context.Context, sIDs []string) error
}
