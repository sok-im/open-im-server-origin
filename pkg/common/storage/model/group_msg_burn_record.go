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

package model

// GroupMsgBurnRecord 记录群消息定时删除的截止时间。
//
// 写入时机：msgtransfer 在群消息分配 seq 后写入。
// BurnEndTime = 发送时间 + 群 MsgBurnDuration（群设置优先）；群未开启时回退为发送者用户 MsgBurnDuration。
// 删除时机：cron 发现 BurnEndTime <= now 时触发删除并同步客户端。
type GroupMsgBurnRecord struct {
	// GroupID 群组 ID
	GroupID string `bson:"group_id"`
	// Seq 消息序列号
	Seq int64 `bson:"seq"`
	// SendID 发送该条群消息的用户 ID
	SendID string `bson:"send_id"`
	// BurnEndTime 消息删除截止时间戳（毫秒）
	BurnEndTime int64 `bson:"burn_end_time"`
	// CreateTime 记录创建时间戳（毫秒）
	CreateTime int64 `bson:"create_time"`
}
