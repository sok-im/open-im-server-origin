package redpacket

import "testing"

func TestNormalizeTransactionType(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"empty defaults to red packet", "", "RED_PACKET", false},
		{"whitespace defaults to red packet", "   ", "RED_PACKET", false},
		{"explicit red packet", "RED_PACKET", "RED_PACKET", false},
		{"lowercase transfer", "transfer", "TRANSFER", false},
		{"mixed case red packet", "Red_Packet", "RED_PACKET", false},
		{"invalid value", "gift", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeTransactionType(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeTransactionType(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveStoredTransactionType(t *testing.T) {
	cases := []struct {
		name      string
		scopeType string
		raw       string
		want      string
		wantErr   bool
	}{
		{"group scope stores empty", "GROUP", "TRANSFER", "", false},
		{"public scope stores empty", "PUBLIC", "anything", "", false},
		{"direct empty defaults red packet", "DIRECT", "", "RED_PACKET", false},
		{"direct transfer", "DIRECT", "transfer", "TRANSFER", false},
		{"direct invalid errors", "DIRECT", "bogus", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveStoredTransactionType(tc.scopeType, tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("resolveStoredTransactionType(%q,%q) = %q, want %q", tc.scopeType, tc.raw, got, tc.want)
			}
		})
	}
}
