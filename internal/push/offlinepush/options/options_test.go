package options

import (
	"strings"
	"testing"
)

func TestAPNsCollapseID_PrefersClientMsgID(t *testing.T) {
	opts := &Opts{Signal: &Signal{ClientMsgID: "client-1", ServerMsgID: "server-1"}}
	if got := opts.APNsCollapseID(); got != "client-1" {
		t.Fatalf("APNsCollapseID() = %q, want client-1", got)
	}
}

func TestAPNsCollapseID_FallsBackToServerMsgID(t *testing.T) {
	opts := &Opts{Signal: &Signal{ServerMsgID: "server-1"}}
	if got := opts.APNsCollapseID(); got != "server-1" {
		t.Fatalf("APNsCollapseID() = %q, want server-1", got)
	}
}

func TestAPNsCollapseID_TruncatesTo64Bytes(t *testing.T) {
	longID := strings.Repeat("a", 80)
	opts := &Opts{Signal: &Signal{ClientMsgID: longID}}
	got := opts.APNsCollapseID()
	if len(got) != apnsCollapseIDMaxLen {
		t.Fatalf("APNsCollapseID() len = %d, want %d", len(got), apnsCollapseIDMaxLen)
	}
}
