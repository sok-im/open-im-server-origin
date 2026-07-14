package chain

import (
	"context"
	"errors"
	"testing"
)

type stubReader struct {
	value  int32
	err    error
	calls  int
	lastIn string
}

func (s *stubReader) TokenDecimals(_ context.Context, tokenAddr string) (int32, error) {
	s.calls++
	s.lastIn = tokenAddr
	return s.value, s.err
}

func TestResolveDecimals_Native(t *testing.T) {
	if got := ResolveDecimals(context.Background(), "EVM", "", nil); got != evmNativeDecimals {
		t.Errorf("EVM native = %d, want %d", got, evmNativeDecimals)
	}
	zero := "0x0000000000000000000000000000000000000000"
	if got := ResolveDecimals(context.Background(), "EVM", zero, nil); got != evmNativeDecimals {
		t.Errorf("EVM zero addr = %d, want %d", got, evmNativeDecimals)
	}
	if got := ResolveDecimals(context.Background(), "TRON", "", nil); got != tronNativeDecimals {
		t.Errorf("TRON native = %d, want %d", got, tronNativeDecimals)
	}
}

func TestResolveDecimals_KnownTable(t *testing.T) {
	// Ethereum USDT is 6 decimals; upper-case input must still hit the table.
	got := ResolveDecimals(context.Background(), "EVM", "0xdAC17F958D2ee523a2206206994597C13D831ec7", nil)
	if got != 6 {
		t.Errorf("USDT(ETH) = %d, want 6", got)
	}
	// BSC-pegged USDT is 18 decimals.
	got = ResolveDecimals(context.Background(), "EVM", "0x55d398326f99059fF775485246999027B3197955", nil)
	if got != 18 {
		t.Errorf("USDT(BSC) = %d, want 18", got)
	}
}

func TestResolveDecimals_OnChainFallbackAndCache(t *testing.T) {
	decimalsCache.Range(func(k, _ any) bool { decimalsCache.Delete(k); return true })
	reader := &stubReader{value: 8}
	token := "0x1111111111111111111111111111111111111111"

	if got := ResolveDecimals(context.Background(), "EVM", token, reader); got != 8 {
		t.Fatalf("first resolve = %d, want 8", got)
	}
	if reader.calls != 1 {
		t.Fatalf("expected 1 on-chain call, got %d", reader.calls)
	}
	// Second call must be served from cache, not the reader.
	if got := ResolveDecimals(context.Background(), "EVM", token, reader); got != 8 {
		t.Fatalf("cached resolve = %d, want 8", got)
	}
	if reader.calls != 1 {
		t.Fatalf("expected cache hit (still 1 call), got %d calls", reader.calls)
	}
}

func TestResolveDecimals_DefaultOnError(t *testing.T) {
	reader := &stubReader{err: errors.New("rpc down")}
	token := "0x2222222222222222222222222222222222222222"
	if got := ResolveDecimals(context.Background(), "EVM", token, reader); got != defaultDecimals {
		t.Errorf("resolve on error = %d, want %d", got, defaultDecimals)
	}
	// TRON has no on-chain reader wired; unknown token degrades to default.
	if got := ResolveDecimals(context.Background(), "TRON", "TUnknownTokenAddress0000000000000", reader); got != defaultDecimals {
		t.Errorf("TRON unknown = %d, want %d", got, defaultDecimals)
	}
}
