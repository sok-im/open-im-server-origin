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

package database

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

type MessageReaction interface {
	// Set upserts the caller's reaction on a message, replacing any previous emoji.
	// The unique key (conversation_id, client_msg_id, user_id) guarantees one row per user per message.
	Set(ctx context.Context, conversationID, clientMsgID, userID, emoji string, now time.Time) error
	// Remove deletes the caller's reaction on a message. If emoji is not empty it is used for a
	// consistency check (only delete when the stored emoji matches). Returns whether a row was removed.
	Remove(ctx context.Context, conversationID, clientMsgID, userID, emoji string) (bool, error)
	// FindByMessage returns all reaction rows for one message ordered by create_time asc.
	FindByMessage(ctx context.Context, conversationID, clientMsgID string) ([]*model.MessageReaction, error)
	// FindByMessages returns reaction rows for multiple messages in a conversation, ordered by create_time asc.
	FindByMessages(ctx context.Context, conversationID string, clientMsgIDs []string) ([]*model.MessageReaction, error)
}
