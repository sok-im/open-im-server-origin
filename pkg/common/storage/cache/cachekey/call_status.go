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

package cachekey

import "time"

const (
	// CallStatusKey is the Redis key prefix for per-user audio/video call status.
	// Full key: CALL_STATUS:{userID}
	CallStatusKey = "CALL_STATUS:"

	// CallStatusExpire is the TTL applied to every call-status entry.
	// It acts as a safety net: if a crash or network partition prevents the
	// normal deletion path from running, the key self-expires within this window
	// so users are not permanently stuck in a "connecting" or "in-call" state.
	CallStatusExpire = 5 * time.Minute
)

// GetCallStatusKey returns the Redis key for a given user's call status.
func GetCallStatusKey(userID string) string {
	return CallStatusKey + userID
}
