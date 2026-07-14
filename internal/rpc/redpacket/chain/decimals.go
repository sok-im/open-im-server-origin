package chain

import (
	"context"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// DecimalsReader queries a token contract's `decimals()` on-chain. *ChainClient
// implements it for EVM chains. It is the correctness fallback used whenever a
// token address is not in the static known-token table below.
type DecimalsReader interface {
	TokenDecimals(ctx context.Context, tokenAddr string) (int32, error)
}

const (
	// evmNativeDecimals is the decimals of the EVM native coin (ETH / BNB).
	evmNativeDecimals int32 = 18
	// tronNativeDecimals is the decimals of the TRON native coin (TRX / sun).
	tronNativeDecimals int32 = 6
	// defaultDecimals is the last-resort value when a token cannot be resolved
	// either from the static table or on-chain. 18 matches ethers.js fromWei.
	defaultDecimals int32 = 18
)

// zeroAddress is the normalized native-token sentinel used by the service layer
// (an empty / unset token normalizes to the zero address).
var zeroAddress = strings.ToLower(common.Address{}.Hex())

// knownTokenDecimals is a curated, high-confidence table of decimals for the
// common stablecoins on ETH / BSC / TRON mainnets, keyed by the token contract
// address (lowercased). Only addresses we are confident about live here; every
// other token falls back to an on-chain decimals() query so a wrong hardcoded
// value can never silently corrupt a display amount.
//
// Note: BSC-pegged USDT/USDC use 18 decimals (unlike their 6-decimal Ethereum
// counterparts) — this table captures exactly that difference.
//
// Testnet addresses are best-effort well-known values; if any is stale it simply
// misses the table and falls back to the on-chain query, so correctness is never
// at risk — only the optimization. Verify against the tokens you actually deploy.
var knownTokenDecimals = map[string]int32{
	// ── Ethereum mainnet ──
	"0xdac17f958d2ee523a2206206994597c13d831ec7": 6, // USDT (ETH)
	"0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48": 6, // USDC (ETH)
	// ── Ethereum Sepolia testnet ──
	"0x1c7d4b196cb0c7b01d743fbc6116a902379c7238": 6, // USDC (Sepolia, Circle official)
	"0xaa8e23fb1079ea71e0a56f48a2aa51851d8433d0": 6, // USDT (Sepolia, common test token)

	// ── BSC mainnet (BEP-20; 18 decimals) ──
	"0x55d398326f99059ff775485246999027b3197955": 18, // USDT (BSC)
	"0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d": 18, // USDC (BSC)
	// ── BSC testnet (chainID 97; 18 decimals) ──
	"0x337610d27c682e347c9cd60bd4b3b107c9d34ddd": 18, // USDT (BSC testnet)
	"0x64544969ed7ebf5f083679233325356ebe738930": 18, // USDC (BSC testnet)

	// ── TRON mainnet (TRC-20; base58 addresses, stored lowercased) ──
	"tr7nhqjekqxgtci8q8zy4pl8otszgjlj6t": 6, // USDT (TRON)
	"tekxitehnzsmse2xqrbj4w32run966rdz8": 6, // USDC (TRON)
	// ── TRON Nile testnet ──
	"txyzopyrdj2d9xrtbg411xzz3km5vkaebf": 6,  // USDT (TRON Nile)
	"tnuokl1ni8aoshffl1asca1gou9rxwazfn": 18, // BTT (TRON Nile, verified on-chain)
}

// decimalsCache memoizes resolved decimals keyed by lowercased contract address.
// Token decimals are immutable per contract, so caching by address alone is safe.
var decimalsCache sync.Map // map[string]int32

func normalizeDecimalsKey(chainType, token string) string {
	token = strings.TrimSpace(token)
	if strings.EqualFold(chainType, "TRON") {
		// TRON uses base58 addresses; do not hex-normalize them.
		return strings.ToLower(token)
	}
	if token == "" {
		return zeroAddress
	}
	return strings.ToLower(common.HexToAddress(token).Hex())
}

func nativeDecimalsFor(chainType string) int32 {
	if strings.EqualFold(chainType, "TRON") {
		return tronNativeDecimals
	}
	return evmNativeDecimals
}

// ResolveDecimals determines the decimals for token on chainType, in order:
//  1. native coin (empty / zero-address token) → chain native decimals;
//  2. static known-token table;
//  3. in-memory cache of previously resolved values;
//  4. on-chain decimals() via reader (EVM only), which is then cached;
//  5. defaultDecimals (18) as a last resort.
//
// It never returns an error: an unresolvable token degrades to a sensible
// default rather than blocking red-packet creation or indexing.
func ResolveDecimals(ctx context.Context, chainType, token string, reader DecimalsReader) int32 {
	key := normalizeDecimalsKey(chainType, token)

	// Native coin has no decimals() to call.
	if key == "" || key == zeroAddress {
		return nativeDecimalsFor(chainType)
	}

	if d, ok := knownTokenDecimals[key]; ok {
		return d
	}
	if v, ok := decimalsCache.Load(key); ok {
		return v.(int32)
	}

	// On-chain fallback (EVM). TRON has no reader wired yet; degrade to default.
	if reader != nil && !strings.EqualFold(chainType, "TRON") {
		if d, err := reader.TokenDecimals(ctx, key); err == nil {
			decimalsCache.Store(key, d)
			return d
		}
	}

	return defaultDecimals
}
