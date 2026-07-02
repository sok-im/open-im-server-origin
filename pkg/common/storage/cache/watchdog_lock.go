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
	"time"
)

// WatchdogLock is a minimal Redis-backed distributed lock used to elect a
// single leader among multiple replicas of a service (e.g. the rtc RPC's
// background call-watchdog scan). It is intentionally narrow in scope: one
// named lock, held by one caller-generated token at a time, with explicit
// renew/release rather than a general-purpose mutex library.
type WatchdogLock interface {
	// TryAcquire attempts to become the leader by setting the lock key to token
	// only if it does not already exist (SET NX). Returns true if acquired.
	TryAcquire(ctx context.Context, token string, ttl time.Duration) (bool, error)

	// Renew extends the lock TTL, but only if the caller still owns it (the
	// stored value equals token). Returns false if the lock was lost (expired
	// and taken over by another replica, or never held).
	Renew(ctx context.Context, token string, ttl time.Duration) (bool, error)

	// Release drops the lock, but only if the caller still owns it. It is safe
	// to call even if the lock was already lost.
	Release(ctx context.Context, token string) error
}
