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
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/tools/errs"
	"github.com/redis/go-redis/v9"
)

// renewLockScript extends the TTL of key only if its value still equals the
// caller's token, so a replica can never renew a lock it no longer owns
// (e.g. after its own TTL already expired and another replica took over).
const renewLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	redis.call("PEXPIRE", KEYS[1], ARGV[2])
	return 1
end
return 0
`

// releaseLockScript deletes key only if its value still equals the caller's
// token, preventing a delayed release from a former leader from deleting a
// newer leader's lock.
const releaseLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`

// NewWatchdogLock returns a Redis-backed WatchdogLock bound to a single named key.
func NewWatchdogLock(rdb redis.UniversalClient, key string) cache.WatchdogLock {
	return &watchdogLock{rdb: rdb, key: key}
}

type watchdogLock struct {
	rdb redis.UniversalClient
	key string
}

func (l *watchdogLock) TryAcquire(ctx context.Context, token string, ttl time.Duration) (bool, error) {
	ok, err := l.rdb.SetNX(ctx, l.key, token, ttl).Result()
	if err != nil {
		return false, errs.WrapMsg(err, "WatchdogLock.TryAcquire redis SETNX failed", "key", l.key)
	}
	return ok, nil
}

func (l *watchdogLock) Renew(ctx context.Context, token string, ttl time.Duration) (bool, error) {
	res, err := l.rdb.Eval(ctx, renewLockScript, []string{l.key}, token, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, errs.WrapMsg(err, "WatchdogLock.Renew redis EVAL failed", "key", l.key)
	}
	return res == 1, nil
}

func (l *watchdogLock) Release(ctx context.Context, token string) error {
	if err := l.rdb.Eval(ctx, releaseLockScript, []string{l.key}, token).Err(); err != nil {
		return errs.WrapMsg(err, "WatchdogLock.Release redis EVAL failed", "key", l.key)
	}
	return nil
}
