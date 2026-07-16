package rtc

import (
	"strings"
	"testing"
)

func TestValidateCustomInfoSize(t *testing.T) {
	if err := validateCustomInfoSize(strings.Repeat("a", maxCustomInfoBytes)); err != nil {
		t.Fatalf("exact limit should pass: %v", err)
	}
	if err := validateCustomInfoSize(strings.Repeat("a", maxCustomInfoBytes+1)); err == nil {
		t.Fatal("expected size error")
	}
}

func TestExtractCustomMessageID(t *testing.T) {
	if got := extractCustomMessageID(`{"messageID":"m1","kind":"call_e2ee_control"}`); got != "m1" {
		t.Fatalf("got %q", got)
	}
	if got := extractCustomMessageID(`not-json`); got != "" {
		t.Fatalf("got %q", got)
	}
}
