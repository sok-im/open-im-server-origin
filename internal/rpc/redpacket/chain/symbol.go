package chain

import (
	"context"
	"strings"
	"sync"
)

// SymbolReader queries a token contract's `symbol()` on-chain. *ChainClient
// implements it for EVM chains. It is the correctness fallback used whenever a
// token address is not in the static known-token table below. It mirrors
// DecimalsReader.
type SymbolReader interface {
	TokenSymbol(ctx context.Context, tokenAddr string) (string, error)
}

const (
	// evmNativeSymbol is the generic fallback ticker for an EVM native coin,
	// used only when a runtime does not configure its own nativeSymbol. The
	// service models all EVM chains under a single "EVM" chainType (see
	// chain_runtime.go), so the correct per-chain symbol (BNB, POL, …) must come
	// from config; ETH is merely the last-resort default.
	evmNativeSymbol = "ETH"
	// tronNativeSymbol is the generic fallback ticker for the TRON native coin.
	tronNativeSymbol = "TRX"
	// defaultSymbol is the last-resort value when a token symbol cannot be
	// resolved either from the static table or on-chain. Empty means "unknown";
	// the frontend already derives a display symbol from the token address.
	defaultSymbol = ""
)

// knownTokenSymbols is the symbol counterpart of knownTokenDecimals, keyed by
// the lowercased token contract address. It covers exactly the same curated set
// of tokens; every other token falls back to an on-chain symbol() query so a
// wrong hardcoded value can never silently corrupt a display symbol.
var knownTokenSymbols = map[string]string{
	// ── Ethereum mainnet ──
	"0xdac17f958d2ee523a2206206994597c13d831ec7": "USDT", // USDT (ETH)
	"0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48": "USDC", // USDC (ETH)
	// ── Ethereum Sepolia testnet ──
	"0x1c7d4b196cb0c7b01d743fbc6116a902379c7238": "USDC", // USDC (Sepolia, Circle official)
	"0xaa8e23fb1079ea71e0a56f48a2aa51851d8433d0": "USDT", // USDT (Sepolia, common test token)

	// ── BSC mainnet (BEP-20) ──
	"0x55d398326f99059ff775485246999027b3197955": "USDT", // USDT (BSC)
	"0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d": "USDC", // USDC (BSC)
	// ── BSC testnet (chainID 97) ──
	"0x337610d27c682e347c9cd60bd4b3b107c9d34ddd": "USDT", // USDT (BSC testnet)
	"0x64544969ed7ebf5f083679233325356ebe738930": "USDC", // USDC (BSC testnet)

	// ── TRON mainnet (TRC-20; base58 addresses, stored lowercased) ──
	"tr7nhqjekqxgtci8q8zy4pl8otszgjlj6t": "USDT", // USDT (TRON)
	"tekxitehnzsmse2xqrbj4w32run966rdz8": "USDC", // USDC (TRON)
	// ── TRON Nile testnet ──
	"txyzopyrdj2d9xrtbg411xzz3km5vkaebf": "USDT", // USDT (TRON Nile)
	"tnuokl1ni8aoshffl1asca1gou9rxwazfn": "BTT",  // BTT (TRON Nile)
}

// symbolCache memoizes resolved symbols keyed by lowercased contract address.
// Token symbols are immutable per contract, so caching by address alone is safe.
var symbolCache sync.Map // map[string]string

// nativeSymbolFor returns the native-coin ticker, preferring the per-runtime
// configured value and falling back to a generic default when it is empty.
func nativeSymbolFor(chainType, configured string) string {
	if s := strings.TrimSpace(configured); s != "" {
		return s
	}
	if strings.EqualFold(chainType, "TRON") {
		return tronNativeSymbol
	}
	return evmNativeSymbol
}

// ResolveSymbol determines the symbol for token on chainType, in order:
//  1. native coin (empty / zero-address token) → configured native symbol,
//     falling back to a generic per-chainType default;
//  2. static known-token table;
//  3. in-memory cache of previously resolved values;
//  4. on-chain symbol() via reader (EVM only), which is then cached;
//  5. defaultSymbol ("") as a last resort.
//
// nativeSymbol is the runtime-configured ticker of the chain's native coin (may
// be empty). It never returns an error: an unresolvable token degrades to an
// empty symbol rather than blocking red-packet creation or indexing. It mirrors
// ResolveDecimals (reusing normalizeDecimalsKey since the key rules are the
// same — a contract address per chain).
func ResolveSymbol(ctx context.Context, chainType, token, nativeSymbol string, reader SymbolReader) string {
	key := normalizeDecimalsKey(chainType, token)

	// Native coin has no symbol() to call.
	if key == "" || key == zeroAddress {
		return nativeSymbolFor(chainType, nativeSymbol)
	}

	if s, ok := knownTokenSymbols[key]; ok {
		return s
	}
	if v, ok := symbolCache.Load(key); ok {
		return v.(string)
	}

	// On-chain fallback (EVM). TRON has no reader wired yet; degrade to default.
	if reader != nil && !strings.EqualFold(chainType, "TRON") {
		if s, err := reader.TokenSymbol(ctx, key); err == nil && s != "" {
			symbolCache.Store(key, s)
			return s
		}
	}

	return defaultSymbol
}
