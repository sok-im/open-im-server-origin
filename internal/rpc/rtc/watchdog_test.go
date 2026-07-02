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

package rtc

import (
	"testing"
	"time"
)

func TestWatchdogSuspectsObserveGraceWindow(t *testing.T) {
	s := newWatchdogSuspects()

	// First observation starts the grace timer; elapsed time must be ~0, not
	// enough to satisfy any real grace period yet.
	if d := s.observe("room-1"); d != 0 {
		t.Fatalf("first observe() elapsed = %v, want 0", d)
	}

	time.Sleep(15 * time.Millisecond)

	// Second observation of the same room reports time elapsed since the
	// first sighting, not a reset.
	if d := s.observe("room-1"); d < 10*time.Millisecond {
		t.Fatalf("second observe() elapsed = %v, want >= 10ms", d)
	}
}

func TestWatchdogSuspectsClearResetsTimer(t *testing.T) {
	s := newWatchdogSuspects()

	s.observe("room-1")
	time.Sleep(15 * time.Millisecond)
	s.clear("room-1")

	// After clear, the room is no longer tracked, so the next observe starts
	// a fresh grace window instead of reporting the pre-clear elapsed time.
	if d := s.observe("room-1"); d != 0 {
		t.Fatalf("observe() after clear elapsed = %v, want 0 (fresh window)", d)
	}
}

func TestWatchdogSuspectsClearIsIdempotent(t *testing.T) {
	s := newWatchdogSuspects()
	// Clearing a room that was never observed must not panic.
	s.clear("never-seen")
}

func TestWatchdogSuspectsRetainOnlyPrunesUntracked(t *testing.T) {
	s := newWatchdogSuspects()
	s.observe("room-1")
	s.observe("room-2")
	s.observe("room-3")

	s.retainOnly(map[string]struct{}{"room-2": {}})

	// room-1 and room-3 were dropped, so they behave like never-observed
	// rooms: their next observe() starts a fresh window at 0.
	if d := s.observe("room-1"); d != 0 {
		t.Fatalf("room-1 elapsed after retainOnly = %v, want 0 (pruned)", d)
	}
	if d := s.observe("room-3"); d != 0 {
		t.Fatalf("room-3 elapsed after retainOnly = %v, want 0 (pruned)", d)
	}

	// room-2 was retained, so its original observation time survives and a
	// later observe() reports non-zero elapsed time.
	time.Sleep(15 * time.Millisecond)
	if d := s.observe("room-2"); d < 10*time.Millisecond {
		t.Fatalf("room-2 elapsed after retainOnly = %v, want >= 10ms (retained)", d)
	}
}

func TestWatchdogSuspectsConcurrentAccess(t *testing.T) {
	s := newWatchdogSuspects()
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				roomID := "room"
				s.observe(roomID)
				s.clear(roomID)
				s.retainOnly(map[string]struct{}{})
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}
