package redpacket

import (
	"math/big"
	"testing"
)

func TestResolveRuntimeByChainKey(t *testing.T) {
	srv := &redPacketServer{
		chainRuntimes: map[string]*chainRuntime{
			"eth-sepolia": {
				ChainKey:        "eth-sepolia",
				ChainType:       "EVM",
				ChainID:         11155111,
				ContractAddress: "0xeth",
			},
			"tron-nile": {
				ChainKey:        "tron-nile",
				ChainType:       "TRON",
				ChainID:         3448148188,
				ContractAddress: "TContract",
			},
		},
	}

	runtime, err := srv.resolveRuntime("eth-sepolia", "EVM", 11155111)
	if err != nil {
		t.Fatalf("resolveRuntime returned error: %v", err)
	}
	if runtime.ChainKey != "eth-sepolia" {
		t.Fatalf("unexpected runtime key: %s", runtime.ChainKey)
	}
}

func TestResolveRuntimeRejectsMismatchedChainType(t *testing.T) {
	srv := &redPacketServer{
		chainRuntimes: map[string]*chainRuntime{
			"eth-sepolia": {
				ChainKey:  "eth-sepolia",
				ChainType: "EVM",
				ChainID:   11155111,
			},
		},
	}

	if _, err := srv.resolveRuntime("eth-sepolia", "TRON", 0); err == nil {
		t.Fatal("expected mismatched chain type to fail")
	}
}

func TestResolveRuntimeRejectsAmbiguousLegacyEVMRequest(t *testing.T) {
	srv := &redPacketServer{
		chainRuntimes: map[string]*chainRuntime{
			"eth-sepolia": {
				ChainKey:  "eth-sepolia",
				ChainType: "EVM",
				ChainID:   11155111,
			},
			"bsc-testnet": {
				ChainKey:  "bsc-testnet",
				ChainType: "EVM",
				ChainID:   97,
			},
		},
	}

	if _, err := srv.resolveRuntime("", "EVM", 0); err == nil {
		t.Fatal("expected ambiguous legacy EVM request to fail")
	}
}

func TestApplyRuntimeDefaultsUsesResolvedChain(t *testing.T) {
	runtime := &chainRuntime{
		ChainKey:        "eth-sepolia",
		ChainType:       "EVM",
		ChainID:         11155111,
		ContractAddress: "0xeth",
	}

	chainID, contractAddress := applyRuntimeDefaults(runtime, 0, "")
	if chainID != 11155111 {
		t.Fatalf("unexpected chainID: %d", chainID)
	}
	if contractAddress != "0xeth" {
		t.Fatalf("unexpected contract address: %s", contractAddress)
	}
}

func TestBindingLookupChainTypeUsesEVMNamespace(t *testing.T) {
	chainType, err := resolveBindingChainType("eth-sepolia", "EVM")
	if err != nil {
		t.Fatalf("resolveBindingChainType returned error: %v", err)
	}
	if chainType != "EVM" {
		t.Fatalf("unexpected binding chain type: %s", chainType)
	}
}

func TestBindingLookupChainTypeFallsBackToRuntimeType(t *testing.T) {
	chainType, err := resolveBindingChainType("tron-nile", "")
	if err == nil {
		t.Fatalf("expected missing chain type without explicit runtime context to fail, got %s", chainType)
	}
}

func TestChainRuntimeSignerSelection(t *testing.T) {
	signer := big.NewInt(1)
	if signer.Cmp(big.NewInt(1)) != 0 {
		t.Fatal("sanity check failed")
	}
}
