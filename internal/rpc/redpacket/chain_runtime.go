package redpacket

import (
	"crypto/ecdsa"
	"fmt"
	"sort"
	"strings"

	"github.com/openimsdk/open-im-server/v3/internal/rpc/redpacket/chain"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/errs"
)

type chainRuntime struct {
	ChainKey        string
	ChainType       string
	ChainID         int64
	ContractAddress string
	SignerKey       *ecdsa.PrivateKey
	EVMClient       *chain.ChainClient
	TronClient      *chain.TronClient
}

func applyRuntimeDefaults(runtime *chainRuntime, chainID int64, contractAddress string) (int64, string) {
	if runtime == nil {
		return chainID, contractAddress
	}
	if chainID == 0 {
		chainID = runtime.ChainID
	}
	if strings.TrimSpace(contractAddress) == "" {
		contractAddress = runtime.ContractAddress
	}
	return chainID, contractAddress
}

func resolveBindingChainType(_ string, chainType string) (string, error) {
	return normalizeChainType(chainType)
}

func (s *redPacketServer) runtimeFromPacket(rp *model.RedPacket) (*chainRuntime, error) {
	if rp == nil {
		return nil, errs.ErrArgs.WrapMsg("red packet is nil")
	}
	return s.resolveRuntime(rp.ChainKey, rp.ChainType, rp.ChainID)
}

func (s *redPacketServer) resolveRuntime(chainKey, chainType string, chainID int64) (*chainRuntime, error) {
	chainKey = strings.TrimSpace(chainKey)
	if chainKey != "" {
		runtime, ok := s.chainRuntimes[chainKey]
		if !ok {
			return nil, errs.ErrArgs.WrapMsg("unsupported chain_key: " + chainKey)
		}
		if chainType != "" {
			normalizedChainType, err := normalizeChainType(chainType)
			if err != nil {
				return nil, err
			}
			if runtime.ChainType != normalizedChainType {
				return nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("chain_key %s does not match chain_type %s", chainKey, normalizedChainType))
			}
		}
		if chainID != 0 && runtime.ChainID != 0 && runtime.ChainID != chainID {
			return nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("chain_key %s does not match chain_id %d", chainKey, chainID))
		}
		return runtime, nil
	}

	if chainType == "" {
		return nil, errs.ErrArgs.WrapMsg("chain_key is required")
	}
	normalizedChainType, err := normalizeChainType(chainType)
	if err != nil {
		return nil, err
	}
	var matched []*chainRuntime
	for _, runtime := range s.chainRuntimes {
		if runtime == nil || runtime.ChainType != normalizedChainType {
			continue
		}
		if chainID != 0 && runtime.ChainID != chainID {
			continue
		}
		matched = append(matched, runtime)
	}
	switch len(matched) {
	case 0:
		return nil, errs.ErrArgs.WrapMsg("no chain runtime matches request")
	case 1:
		return matched[0], nil
	default:
		keys := make([]string, 0, len(matched))
		for _, runtime := range matched {
			keys = append(keys, runtime.ChainKey)
		}
		sort.Strings(keys)
		return nil, errs.ErrArgs.WrapMsg("chain_key is required when multiple runtimes share the same chain_type", "availableChainKeys", strings.Join(keys, ","))
	}
}

func buildRuntimeConfigs(cfg config.RedPacket) map[string]config.RedPacketChainRuntime {
	if len(cfg.Chains) > 0 {
		return cfg.Chains
	}

	runtimes := make(map[string]config.RedPacketChainRuntime)
	if strings.TrimSpace(cfg.Chain.RPCURL) != "" || strings.TrimSpace(cfg.Chain.ContractAddress) != "" {
		runtimes["evm-default"] = config.RedPacketChainRuntime{
			ChainType:             "EVM",
			ChainID:               cfg.Chain.ChainID,
			RPCURL:                cfg.Chain.RPCURL,
			ContractAddress:       cfg.Chain.ContractAddress,
			SignerPrivateKey:      cfg.Chain.SignerPrivateKey,
			ConfigAdminPrivateKey: cfg.Chain.ConfigAdminPrivateKey,
		}
	}
	if strings.TrimSpace(cfg.Tron.FullNodeURL) != "" || strings.TrimSpace(cfg.Tron.ContractBase58) != "" {
		runtimes["tron-default"] = config.RedPacketChainRuntime{
			ChainType:      "TRON",
			ChainID:        0,
			FullNodeURL:    cfg.Tron.FullNodeURL,
			ContractBase58: cfg.Tron.ContractBase58,
			OwnerBase58:    cfg.Tron.OwnerBase58,
			PrivateKeyHex:  cfg.Tron.PrivateKeyHex,
			FeeLimit:       cfg.Tron.FeeLimit,
		}
	}
	return runtimes
}
