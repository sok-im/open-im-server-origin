package model

import "testing"

func TestFormatUnits(t *testing.T) {
	cases := []struct {
		raw      string
		decimals int32
		want     string
	}{
		{"1500000000000000000", 18, "1.5"},
		{"1000000000000000000", 18, "1"},
		{"1000000", 6, "1"},
		{"1500000", 6, "1.5"},
		{"123", 6, "0.000123"},
		{"0", 18, "0"},
		{"", 18, "0"},
		{"1", 0, "1"},
		{"250000", 6, "0.25"},
		{"999", 18, "0.000000000000000999"},
		{"1234567", 6, "1.234567"},
		{"-1500000", 6, "-1.5"},
		{"not-a-number", 18, "not-a-number"},
	}
	for _, c := range cases {
		if got := FormatUnits(c.raw, c.decimals); got != c.want {
			t.Errorf("FormatUnits(%q, %d) = %q, want %q", c.raw, c.decimals, got, c.want)
		}
	}
}

func TestSubAmounts(t *testing.T) {
	cases := []struct {
		a, b, want string
	}{
		{"1000000000000000000", "400000000000000000", "600000000000000000"},
		{"1000000", "1000000", "0"},
		{"5", "9", "0"}, // clamped, never negative
		{"", "0", "0"},
		{"1000000", "", "1000000"},
	}
	for _, c := range cases {
		if got := SubAmounts(c.a, c.b); got != c.want {
			t.Errorf("SubAmounts(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}
