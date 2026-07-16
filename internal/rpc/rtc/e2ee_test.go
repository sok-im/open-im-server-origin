package rtc

import (
	"testing"

	pbrtc "github.com/openimsdk/protocol/rtc"
)

func TestNormalizeCallConversationIDSingle(t *testing.T) {
	if got := normalizeCallConversationID("", "b", []string{"a"}); got != "si_a_b" {
		t.Fatalf("conversation ID = %q, want si_a_b", got)
	}
}

func TestNormalizeCallConversationIDGroup(t *testing.T) {
	if got := normalizeCallConversationID("group-1", "b", []string{"a"}); got != "group-1" {
		t.Fatalf("conversation ID = %q, want group-1", got)
	}
}

func TestParseE2EERequired(t *testing.T) {
	required, callID, raw, err := parseE2EEFromCustomData(`{"e2ee":{"required":true,"callID":"call-1","scheme":"mls-exporter-livekit-v1","version":1}}`)
	if err != nil || !required || callID != "call-1" || raw == "" {
		t.Fatalf("parse = (%v, %q, %q, %v)", required, callID, raw, err)
	}
}

func TestCheckE2EECapability(t *testing.T) {
	if err := checkE2EECapability(nil, []string{"mls-exporter-livekit-v1"}, 1); err == nil {
		t.Fatal("expected missing capability error")
	}
	if err := checkE2EECapability(&pbrtc.E2EECapability{
		Schemes:       []string{"mls-exporter-livekit-v1"},
		FrameCryptor:  true,
		ClientVersion: "1",
	}, []string{"mls-exporter-livekit-v1"}, 1); err != nil {
		t.Fatalf("capability rejected: %v", err)
	}
}
