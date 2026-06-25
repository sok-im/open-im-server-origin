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

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/errs"
	"github.com/redis/go-redis/v9"
)

// NewCallStatusCache returns a Redis-backed CallStatusCache.
func NewCallStatusCache(rdb redis.UniversalClient) cache.CallStatusCache {
	return &callStatusCache{rdb: rdb, expire: cachekey.CallStatusExpire}
}

type callStatusCache struct {
	rdb    redis.UniversalClient
	expire time.Duration
}

func (c *callStatusCache) SetCallStatus(ctx context.Context, userID string, status *model.UserCallStatus) error {
	data, err := json.Marshal(status)
	if err != nil {
		return errs.WrapMsg(err, "CallStatusCache.SetCallStatus marshal failed", "userID", userID)
	}
	key := cachekey.GetCallStatusKey(userID)
	if err := c.rdb.Set(ctx, key, data, c.expire).Err(); err != nil {
		return errs.WrapMsg(err, "CallStatusCache.SetCallStatus redis SET failed", "userID", userID, "key", key)
	}
	return nil
}

func (c *callStatusCache) GetCallStatus(ctx context.Context, userID string) (*model.UserCallStatus, error) {
	key := cachekey.GetCallStatusKey(userID)
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, errs.ErrRecordNotFound.WrapMsg("call status not found", "userID", userID)
		}
		return nil, errs.WrapMsg(err, "CallStatusCache.GetCallStatus redis GET failed", "userID", userID, "key", key)
	}
	var status model.UserCallStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, errs.WrapMsg(err, "CallStatusCache.GetCallStatus unmarshal failed", "userID", userID)
	}
	return &status, nil
}

func (c *callStatusCache) DeleteCallStatus(ctx context.Context, userIDs ...string) error {
	if len(userIDs) == 0 {
		return nil
	}
	keys := make([]string, len(userIDs))
	for i, uid := range userIDs {
		keys[i] = cachekey.GetCallStatusKey(uid)
	}
	if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
		return errs.WrapMsg(err, "CallStatusCache.DeleteCallStatus redis DEL failed", "userIDs", userIDs)
	}
	return nil
}
