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

package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MessageReaction 表情反应记录：一条消息 + 一个用户 只保留一行（换 emoji 即 UPDATE，取消即 DELETE）。
// 反应是消息的附属元数据，不修改原消息内容体。
type MessageReaction struct {
	ID             primitive.ObjectID `bson:"_id"`
	ConversationID string             `bson:"conversation_id"`
	ClientMsgID    string             `bson:"client_msg_id"`
	UserID         string             `bson:"user_id"`
	Emoji          string             `bson:"emoji"`
	CreateTime     time.Time          `bson:"create_time"`
	UpdateTime     time.Time          `bson:"update_time"`
}
