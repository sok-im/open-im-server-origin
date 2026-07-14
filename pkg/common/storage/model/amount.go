package model

import (
	"math/big"
	"strings"
)

// SubAmounts returns a-b for two base-10 integer amount strings, clamped at 0
// (a red packet balance can never be negative). Empty operands are treated as 0.
func SubAmounts(a, b string) string {
	left := new(big.Int)
	if s := strings.TrimSpace(a); s != "" {
		left.SetString(s, 10)
	}
	right := new(big.Int)
	if s := strings.TrimSpace(b); s != "" {
		right.SetString(s, 10)
	}
	diff := new(big.Int).Sub(left, right)
	if diff.Sign() < 0 {
		return "0"
	}
	return diff.String()
}

// FormatUnits converts a raw on-chain integer amount (base-10 string, in the
// token's smallest unit / "wei") into a human-readable decimal string using the
// given token decimals. Trailing zeros in the fractional part are trimmed.
//
// Examples:
//
//	FormatUnits("1500000000000000000", 18) == "1.5"
//	FormatUnits("1000000", 6)               == "1"
//	FormatUnits("123", 6)                   == "0.000123"
//	FormatUnits("0", 18)                    == "0"
//
// If raw is empty it is treated as "0". If raw is not a valid integer the raw
// string is returned unchanged so callers never lose information.
func FormatUnits(raw string, decimals int32) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "0"
	}
	n, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return raw
	}
	if decimals <= 0 {
		return n.String()
	}

	neg := n.Sign() < 0
	if neg {
		n = new(big.Int).Abs(n)
	}

	digits := n.String()
	d := int(decimals)

	var intPart, fracPart string
	if len(digits) <= d {
		intPart = "0"
		fracPart = strings.Repeat("0", d-len(digits)) + digits
	} else {
		intPart = digits[:len(digits)-d]
		fracPart = digits[len(digits)-d:]
	}

	fracPart = strings.TrimRight(fracPart, "0")
	result := intPart
	if fracPart != "" {
		result += "." + fracPart
	}
	if neg {
		result = "-" + result
	}
	return result
}
