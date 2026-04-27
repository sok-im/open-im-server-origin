package redpacket

import (
	"context"
	"crypto/ecdsa"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	pbredpacket "github.com/openimsdk/protocol/redpacket"
)

//go:embed abi/RedPacket.json
var redpacketABI []byte

type ethClient struct {
	client          *ethclient.Client
	contractAddr    common.Address
	signerKey       *ecdsa.PrivateKey
	configAdminKey  *ecdsa.PrivateKey
	chainID         *big.Int
	contractABI     abi.ABI
}

func newEthClient(cfg config.RedPacketETH) (*ethClient, error) {
	client, err := ethclient.Dial(cfg.RPCURL)
	if err != nil {
		return nil, fmt.Errorf("ethclient dial: %w", err)
	}

	contractABI, err := abi.JSON(strings.NewReader(string(redpacketABI)))
	if err != nil {
		return nil, fmt.Errorf("parse abi: %w", err)
	}

	ec := &ethClient{
		client:       client,
		contractAddr: common.HexToAddress(cfg.ContractAddress),
		chainID:      big.NewInt(cfg.ChainID),
		contractABI:  contractABI,
	}

	if cfg.SignerPrivateKey != "" {
		key, err := crypto.HexToECDSA(strings.TrimPrefix(cfg.SignerPrivateKey, "0x"))
		if err != nil {
			return nil, fmt.Errorf("parse signer private key: %w", err)
		}
		ec.signerKey = key
	}

	if cfg.ConfigAdminPrivateKey != "" {
		key, err := crypto.HexToECDSA(strings.TrimPrefix(cfg.ConfigAdminPrivateKey, "0x"))
		if err != nil {
			return nil, fmt.Errorf("parse config admin private key: %w", err)
		}
		ec.configAdminKey = key
	}

	return ec, nil
}

// SignClaim generates a backend signature for a red packet claim.
// It calls getSignMessage on-chain to get the digest, then signs it with the signer key.
func (e *ethClient) SignClaim(ctx context.Context, packetID, claimer, authNonce, randomSeed string, deadline int64) (string, error) {
	if e.signerKey == nil {
		return "", fmt.Errorf("signer private key not configured")
	}

	packetIDBig, ok := new(big.Int).SetString(packetID, 10)
	if !ok {
		return "", fmt.Errorf("invalid packetId: %s", packetID)
	}
	authNonceBig, ok := new(big.Int).SetString(authNonce, 10)
	if !ok {
		return "", fmt.Errorf("invalid authNonce: %s", authNonce)
	}
	randomSeedBig, ok := new(big.Int).SetString(randomSeed, 10)
	if !ok {
		return "", fmt.Errorf("invalid randomSeed: %s", randomSeed)
	}
	deadlineBig := big.NewInt(deadline)
	claimerAddr := common.HexToAddress(claimer)

	// Call getSignMessage on-chain to get the digest.
	callData, err := e.contractABI.Pack("getSignMessage", packetIDBig, claimerAddr, authNonceBig, randomSeedBig, deadlineBig)
	if err != nil {
		return "", fmt.Errorf("pack getSignMessage: %w", err)
	}
	result, err := e.client.CallContract(ctx, ethereum.CallMsg{
		To:   &e.contractAddr,
		Data: callData,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("getSignMessage call: %w", err)
	}

	var digest [32]byte
	copy(digest[:], result)

	// Raw ECDSA sign (no personal_sign prefix).
	sig, err := crypto.Sign(digest[:], e.signerKey)
	if err != nil {
		return "", fmt.Errorf("sign digest: %w", err)
	}
	// EVM contracts expect v = 27 or 28.
	sig[64] += 27
	return "0x" + hex.EncodeToString(sig), nil
}

// SendAdminTx sends a config-admin transaction to the contract.
func (e *ethClient) SendAdminTx(ctx context.Context, method string, args ...any) (string, error) {
	if e.configAdminKey == nil {
		return "", fmt.Errorf("config admin private key not configured")
	}

	packedArgs, err := packAdminArgs(e.contractABI, method, args...)
	if err != nil {
		return "", fmt.Errorf("pack args for %s: %w", method, err)
	}

	nonce, err := e.client.PendingNonceAt(ctx, crypto.PubkeyToAddress(e.configAdminKey.PublicKey))
	if err != nil {
		return "", fmt.Errorf("get nonce: %w", err)
	}
	gasPrice, err := e.client.SuggestGasPrice(ctx)
	if err != nil {
		return "", fmt.Errorf("get gas price: %w", err)
	}

	msg := ethereum.CallMsg{
		From: crypto.PubkeyToAddress(e.configAdminKey.PublicKey),
		To:   &e.contractAddr,
		Data: packedArgs,
	}
	gasLimit, err := e.client.EstimateGas(ctx, msg)
	if err != nil {
		gasLimit = 200000
	}

	tx := types.NewTransaction(nonce, e.contractAddr, big.NewInt(0), gasLimit, gasPrice, packedArgs)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(e.chainID), e.configAdminKey)
	if err != nil {
		return "", fmt.Errorf("sign tx: %w", err)
	}
	if err := e.client.SendTransaction(ctx, signedTx); err != nil {
		return "", fmt.Errorf("send tx: %w", err)
	}
	return signedTx.Hash().Hex(), nil
}

// ParseTxEvents decodes redpacket events from a transaction receipt.
func (e *ethClient) ParseTxEvents(ctx context.Context, txHash string) ([]*pbredpacket.TxEvent, error) {
	hash := common.HexToHash(txHash)
	receipt, err := e.client.TransactionReceipt(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("get receipt: %w", err)
	}
	return parseLogsToTxEvents(e.contractABI, receipt.Logs, e.contractAddr)
}

// packAdminArgs ABI-encodes function call data with type coercion for string/bool/any args.
func packAdminArgs(contractABI abi.ABI, method string, args ...any) ([]byte, error) {
	m, ok := contractABI.Methods[method]
	if !ok {
		return nil, fmt.Errorf("method %s not found in ABI", method)
	}
	converted, err := convertArgs(m.Inputs, args)
	if err != nil {
		return nil, err
	}
	return contractABI.Pack(method, converted...)
}

// convertArgs coerces string/any args to the types expected by the ABI method inputs.
func convertArgs(inputs abi.Arguments, args []any) ([]any, error) {
	if len(inputs) != len(args) {
		return nil, fmt.Errorf("arg count mismatch: expected %d, got %d", len(inputs), len(args))
	}
	out := make([]any, len(args))
	for i, input := range inputs {
		switch input.Type.String() {
		case "address":
			switch v := args[i].(type) {
			case string:
				out[i] = common.HexToAddress(v)
			case common.Address:
				out[i] = v
			default:
				return nil, fmt.Errorf("arg %d: cannot convert %T to address", i, args[i])
			}
		case "uint256":
			switch v := args[i].(type) {
			case string:
				n, ok := new(big.Int).SetString(v, 10)
				if !ok {
					return nil, fmt.Errorf("arg %d: cannot parse %q as uint256", i, v)
				}
				out[i] = n
			case int64:
				out[i] = big.NewInt(v)
			case *big.Int:
				out[i] = v
			default:
				return nil, fmt.Errorf("arg %d: cannot convert %T to uint256", i, args[i])
			}
		case "bool":
			switch v := args[i].(type) {
			case bool:
				out[i] = v
			default:
				return nil, fmt.Errorf("arg %d: cannot convert %T to bool", i, args[i])
			}
		default:
			out[i] = args[i]
		}
	}
	return out, nil
}

// parseLogsToTxEvents decodes EVM event logs into TxEvent protos.
func parseLogsToTxEvents(contractABI abi.ABI, logs []*types.Log, contractAddr common.Address) ([]*pbredpacket.TxEvent, error) {
	var events []*pbredpacket.TxEvent
	for _, l := range logs {
		if l.Address != contractAddr || len(l.Topics) == 0 {
			continue
		}
		event, err := contractABI.EventByID(l.Topics[0])
		if err != nil {
			continue
		}
		data, err := encodeEventData(contractABI, event.Name, l)
		if err != nil {
			continue
		}
		events = append(events, &pbredpacket.TxEvent{Name: event.Name, Data: data})
	}
	return events, nil
}

// encodeEventData unpacks a log and JSON-encodes the named fields.
// All values are normalised to JSON strings so big numbers don't lose
// precision when consumed by JS clients (which would otherwise overflow
// at 2^53). Address fields are EIP-55 checksummed.
func encodeEventData(contractABI abi.ABI, eventName string, l *types.Log) ([]byte, error) {
	vals := make(map[string]string)
	event, ok := contractABI.Events[eventName]
	if !ok {
		return json.Marshal(vals)
	}

	// Unpack non-indexed fields from Data.
	if len(l.Data) > 0 {
		nonIndexed := make(map[string]any)
		if err := contractABI.UnpackIntoMap(nonIndexed, eventName, l.Data); err == nil {
			for k, v := range nonIndexed {
				vals[k] = formatEventValue(v)
			}
		}
	}

	// Decode indexed topics (skip topic[0] which is the event sig).
	topicIdx := 1
	for _, input := range event.Inputs {
		if !input.Indexed {
			continue
		}
		if topicIdx >= len(l.Topics) {
			break
		}
		topic := l.Topics[topicIdx]
		topicIdx++
		switch input.Type.String() {
		case "address":
			vals[input.Name] = common.HexToAddress(topic.Hex()).Hex()
		case "uint256", "uint128", "uint64", "uint32", "uint16", "uint8",
			"int256", "int128", "int64", "int32", "int16", "int8":
			vals[input.Name] = new(big.Int).SetBytes(topic.Bytes()).String()
		default:
			vals[input.Name] = topic.Hex()
		}
	}
	return json.Marshal(vals)
}

// formatEventValue converts an ABI-decoded value into a stable JSON-string
// representation. Big numbers stay strings to preserve precision.
func formatEventValue(v any) string {
	switch x := v.(type) {
	case common.Address:
		return x.Hex()
	case *big.Int:
		if x == nil {
			return "0"
		}
		return x.String()
	case []byte:
		return "0x" + hex.EncodeToString(x)
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}
