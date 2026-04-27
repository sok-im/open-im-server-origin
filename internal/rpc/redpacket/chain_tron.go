package redpacket

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	pbredpacket "github.com/openimsdk/protocol/redpacket"
)

type tronClient struct {
	nodeURL         string
	contractAddr    string
	signerKey       *ecdsa.PrivateKey
	configAdminKey  *ecdsa.PrivateKey
	contractABI     abi.ABI
}

func newTronClient(cfg config.RedPacketTRON) *tronClient {
	contractABI, _ := abi.JSON(strings.NewReader(string(redpacketABI)))
	tc := &tronClient{
		nodeURL:      strings.TrimRight(cfg.NodeURL, "/"),
		contractAddr: cfg.ContractAddress,
		contractABI:  contractABI,
	}
	if cfg.SignerPrivateKey != "" {
		key, err := crypto.HexToECDSA(strings.TrimPrefix(cfg.SignerPrivateKey, "0x"))
		if err == nil {
			tc.signerKey = key
		}
	}
	if cfg.ConfigAdminPrivateKey != "" {
		key, err := crypto.HexToECDSA(strings.TrimPrefix(cfg.ConfigAdminPrivateKey, "0x"))
		if err == nil {
			tc.configAdminKey = key
		}
	}
	return tc
}

// SignClaim signs a claim for TRON. To stay byte-identical with the EVM
// contract, the digest is fetched from getSignMessage() via TRON
// /wallet/triggerconstantcontract (mirrors what we do for ETH), then signed
// raw with the configured signer key.
func (t *tronClient) SignClaim(ctx context.Context, packetID, claimer, authNonce, randomSeed string, deadline int64) (string, error) {
	if t.signerKey == nil {
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
	claimerAddr := common.HexToAddress(tronAddrToHex(claimer))

	// ABI-encode the call: getSignMessage(packetId, claimer, authNonce, randomSeed, deadline)
	callData, err := t.contractABI.Pack(
		"getSignMessage",
		packetIDBig, claimerAddr, authNonceBig, randomSeedBig, big.NewInt(deadline),
	)
	if err != nil {
		return "", fmt.Errorf("pack getSignMessage: %w", err)
	}
	// TRON triggerconstantcontract takes the parameter blob without the 4-byte selector.
	if len(callData) < 4 {
		return "", fmt.Errorf("encoded call data too short")
	}
	paramHex := hex.EncodeToString(callData[4:])

	// owner_address can be any T-address; use the signer's derived TRON address.
	ownerAddr := tronHexToBase58(crypto.PubkeyToAddress(t.signerKey.PublicKey).Hex())

	resp, err := t.post("/wallet/triggerconstantcontract", map[string]any{
		"owner_address":     ownerAddr,
		"contract_address":  t.contractAddr,
		"function_selector": "getSignMessage" + getMethodSig(t.contractABI, "getSignMessage"),
		"parameter":         paramHex,
		"visible":           true,
	})
	if err != nil {
		return "", fmt.Errorf("triggerconstantcontract: %w", err)
	}
	if msg, ok := resp["result"].(map[string]any); ok {
		if msg["result"] != true {
			rawMsg, _ := msg["message"].(string)
			if rawMsg != "" {
				if b, err := hex.DecodeString(rawMsg); err == nil {
					return "", fmt.Errorf("getSignMessage failed: %s", string(b))
				}
				return "", fmt.Errorf("getSignMessage failed: %s", rawMsg)
			}
		}
	}
	results, _ := resp["constant_result"].([]any)
	if len(results) == 0 {
		return "", fmt.Errorf("getSignMessage returned no result")
	}
	hexResult, _ := results[0].(string)
	digestBytes, err := hex.DecodeString(hexResult)
	if err != nil {
		return "", fmt.Errorf("decode digest hex: %w", err)
	}
	if len(digestBytes) != 32 {
		return "", fmt.Errorf("digest length unexpected: %d", len(digestBytes))
	}

	sig, err := crypto.Sign(digestBytes, t.signerKey)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	sig[64] += 27
	return "0x" + hex.EncodeToString(sig), nil
}

// SendAdminTx sends a config admin transaction on TRON.
func (t *tronClient) SendAdminTx(ctx context.Context, method string, args ...any) (string, error) {
	if t.configAdminKey == nil {
		return "", fmt.Errorf("config admin private key not configured")
	}

	// ABI-encode the function parameters.
	callData, err := packAdminArgs(t.contractABI, method, args...)
	if err != nil {
		return "", fmt.Errorf("pack %s: %w", method, err)
	}
	// Remove the 4-byte selector; TRON triggersmartcontract takes selector-less params.
	paramHex := hex.EncodeToString(callData[4:])

	ownerAddr := tronHexToBase58(crypto.PubkeyToAddress(t.configAdminKey.PublicKey).Hex())

	// Build unsigned transaction.
	triggerReq := map[string]any{
		"owner_address":     ownerAddr,
		"contract_address":  t.contractAddr,
		"function_selector": method + getMethodSig(t.contractABI, method),
		"parameter":         paramHex,
		"fee_limit":         100_000_000,
		"call_value":        0,
	}
	triggerResp, err := t.post("/wallet/triggersmartcontract", triggerReq)
	if err != nil {
		return "", fmt.Errorf("triggersmartcontract: %w", err)
	}
	txJSON, ok := triggerResp["transaction"]
	if !ok {
		return "", fmt.Errorf("no transaction in triggersmartcontract response")
	}

	// Sign the transaction.
	txMap, ok := txJSON.(map[string]any)
	if !ok {
		return "", fmt.Errorf("invalid transaction format")
	}
	txid, _ := txMap["txID"].(string)
	txidBytes, err := hex.DecodeString(txid)
	if err != nil {
		return "", fmt.Errorf("decode txid: %w", err)
	}
	sig, err := crypto.Sign(txidBytes, t.configAdminKey)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	sig[64] += 27
	txMap["signature"] = []string{hex.EncodeToString(sig)}

	// Broadcast.
	broadcastResp, err := t.post("/wallet/broadcasttransaction", txMap)
	if err != nil {
		return "", fmt.Errorf("broadcast: %w", err)
	}
	if result, ok := broadcastResp["result"].(bool); !ok || !result {
		msg, _ := broadcastResp["message"].(string)
		return "", fmt.Errorf("broadcast failed: %s", msg)
	}
	return txid, nil
}

// ParseTxEvents decodes redpacket events from a TRON transaction receipt.
func (t *tronClient) ParseTxEvents(ctx context.Context, txHash string) ([]*pbredpacket.TxEvent, error) {
	resp, err := t.get("/v1/transactions/" + txHash + "/events")
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}
	dataArr, ok := resp["data"].([]any)
	if !ok {
		return nil, nil
	}

	var events []*pbredpacket.TxEvent
	for _, item := range dataArr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		eventName, _ := m["event_name"].(string)
		result, _ := m["result"].(map[string]any)
		data, _ := json.Marshal(result)
		events = append(events, &pbredpacket.TxEvent{Name: eventName, Data: data})
	}
	return events, nil
}

// post sends a JSON POST to the TRON full-node HTTP API.
func (t *tronClient) post(path string, body any) (map[string]any, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := http.Post(t.nodeURL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// get sends a GET request to the TRON Grid / full-node HTTP API.
func (t *tronClient) get(path string) (map[string]any, error) {
	resp, err := http.Get(t.nodeURL + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// getMethodSig returns the ABI method signature string for function selector building.
func getMethodSig(contractABI abi.ABI, method string) string {
	m, ok := contractABI.Methods[method]
	if !ok {
		return "()"
	}
	types := make([]string, len(m.Inputs))
	for i, input := range m.Inputs {
		types[i] = input.Type.String()
	}
	return "(" + strings.Join(types, ",") + ")"
}

// tronAddrToHex converts a TRON Base58Check address to a hex address (0x-prefixed EVM form).
// For simplicity, if it already looks like hex, return as-is.
func tronAddrToHex(addr string) string {
	if strings.HasPrefix(addr, "0x") || strings.HasPrefix(addr, "0X") {
		return addr
	}
	// Decode base58check and strip the 0x41 prefix byte.
	decoded := base58Decode(addr)
	if len(decoded) < 21 {
		return addr
	}
	return "0x" + hex.EncodeToString(decoded[1:21])
}

// tronHexToBase58 converts an EVM hex address to a TRON base58check address.
func tronHexToBase58(hexAddr string) string {
	h := strings.TrimPrefix(hexAddr, "0x")
	h = strings.TrimPrefix(h, "0X")
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != 20 {
		return hexAddr
	}
	// TRON prefix byte is 0x41.
	full := append([]byte{0x41}, b...)
	return base58Encode(full)
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// sha256d returns SHA-256(SHA-256(b)). TRON's Base58Check checksum uses the
// first 4 bytes of this digest.
func sha256d(b []byte) []byte {
	h1 := sha256.Sum256(b)
	h2 := sha256.Sum256(h1[:])
	return h2[:]
}

func base58Encode(input []byte) string {
	check := sha256d(input)[:4]
	input = append(append([]byte{}, input...), check...)

	var result []byte
	x := new(big.Int).SetBytes(input)
	base := big.NewInt(58)
	mod := new(big.Int)
	zero := big.NewInt(0)
	for x.Cmp(zero) > 0 {
		x.DivMod(x, base, mod)
		result = append(result, base58Alphabet[mod.Int64()])
	}
	for _, b := range input {
		if b != 0 {
			break
		}
		result = append(result, base58Alphabet[0])
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return string(result)
}

// base58Decode performs a Base58Check decode and validates the trailing
// 4-byte SHA-256(SHA-256(payload)) checksum, returning the payload only.
// Returns nil on invalid input or checksum mismatch.
func base58Decode(input string) []byte {
	result := big.NewInt(0)
	base := big.NewInt(58)
	for _, c := range input {
		idx := strings.IndexRune(base58Alphabet, c)
		if idx < 0 {
			return nil
		}
		result.Mul(result, base)
		result.Add(result, big.NewInt(int64(idx)))
	}
	decoded := result.Bytes()
	var leading int
	for _, c := range input {
		if c != rune(base58Alphabet[0]) {
			break
		}
		leading++
	}
	out := make([]byte, leading+len(decoded))
	copy(out[leading:], decoded)
	if len(out) < 4 {
		return nil
	}
	payload, check := out[:len(out)-4], out[len(out)-4:]
	if !bytes.Equal(sha256d(payload)[:4], check) {
		return nil
	}
	return payload
}
