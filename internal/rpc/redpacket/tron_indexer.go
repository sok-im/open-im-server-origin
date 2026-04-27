package redpacket

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/crypto"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/openimsdk/tools/log"
)

const (
	cursorIDTRON    = "tron"
	tronBlockBatch  = int64(20)
	tronIndexerDelay = 3 * time.Second
)

// tronIndexer polls TRON blocks and processes RedPacket contract events.
type tronIndexer struct {
	tron       *tronClient
	db         *mongo.Database
	startBlock int64

	// Pre-computed event topic hashes for matching.
	topicPacketCreated  string
	topicPacketClaimed  string
	topicPacketRefunded string
}

func newTronIndexer(tron *tronClient, db *mongo.Database, startBlock int64) *tronIndexer {
	idx := &tronIndexer{tron: tron, db: db, startBlock: startBlock}

	// Build topic hashes (keccak256 of event signatures).
	idx.topicPacketCreated = topicHash("PacketCreated(uint256,address,uint8,address,uint256,uint256,uint256)")
	idx.topicPacketClaimed = topicHash("PacketClaimed(uint256,address,uint256,uint256,uint256,uint256)")
	idx.topicPacketRefunded = topicHash("PacketRefunded(uint256,address,address,uint256)")
	return idx
}

func topicHash(sig string) string {
	return hex.EncodeToString(crypto.Keccak256([]byte(sig)))
}

func (idx *tronIndexer) Run(ctx context.Context) {
	log.ZInfo(ctx, "tron indexer started")
	for {
		select {
		case <-ctx.Done():
			log.ZInfo(ctx, "tron indexer stopped")
			return
		default:
		}
		if err := idx.step(ctx); err != nil {
			log.ZWarn(ctx, "tron indexer step error", err)
		}
		time.Sleep(tronIndexerDelay)
	}
}

func (idx *tronIndexer) step(ctx context.Context) error {
	from := idx.loadCursor(ctx)

	// Get current TRON block number.
	resp, err := idx.tron.post("/wallet/getnowblock", map[string]any{})
	if err != nil {
		return fmt.Errorf("getnowblock: %w", err)
	}
	blockHeader, _ := resp["block_header"].(map[string]any)
	rawData, _ := blockHeader["raw_data"].(map[string]any)
	latestF, _ := rawData["number"].(float64)
	latest := int64(latestF)
	if latest <= from {
		return nil
	}

	to := from + tronBlockBatch
	if to > latest {
		to = latest
	}

	// Advance the cursor block-by-block, only after each block has been fully
	// processed without errors. A failing block stops the loop so the same
	// range gets retried on the next tick (no event loss).
	for blockNum := from + 1; blockNum <= to; blockNum++ {
		if err := idx.processBlock(ctx, blockNum); err != nil {
			log.ZWarn(ctx, "tron indexer process block error", err, "block", blockNum)
			return err
		}
		idx.saveCursor(ctx, blockNum)
	}
	return nil
}

func (idx *tronIndexer) processBlock(ctx context.Context, blockNum int64) error {
	resp, err := idx.tron.post("/wallet/getblockbynum", map[string]any{"num": blockNum})
	if err != nil {
		return err
	}
	transactions, _ := resp["transactions"].([]any)
	for _, txItem := range transactions {
		tx, ok := txItem.(map[string]any)
		if !ok {
			continue
		}
		if err := idx.processTx(ctx, tx, blockNum); err != nil {
			return err
		}
	}
	return nil
}

func (idx *tronIndexer) processTx(ctx context.Context, tx map[string]any, blockNum int64) error {
	txID, _ := tx["txID"].(string)
	ret, _ := tx["ret"].([]any)
	if len(ret) == 0 {
		return nil
	}
	retMap, _ := ret[0].(map[string]any)
	contractRet, _ := retMap["contractRet"].(string)
	if contractRet != "SUCCESS" {
		return nil
	}

	rawData, _ := tx["raw_data"].(map[string]any)
	contracts, _ := rawData["contract"].([]any)
	for _, c := range contracts {
		cm, _ := c.(map[string]any)
		if cm["type"] != "TriggerSmartContract" {
			continue
		}
		param, _ := cm["parameter"].(map[string]any)
		value, _ := param["value"].(map[string]any)
		contractAddr, _ := value["contract_address"].(string)
		if normaliseAddr(contractAddr) != normaliseAddr(idx.tron.contractAddr) {
			continue
		}
		if err := idx.fetchAndProcessLogs(ctx, txID, blockNum); err != nil {
			return err
		}
	}
	return nil
}

func (idx *tronIndexer) fetchAndProcessLogs(ctx context.Context, txID string, blockNum int64) error {
	resp, err := idx.tron.get("/v1/transactions/" + txID + "/events")
	if err != nil {
		return fmt.Errorf("get events for %s: %w", txID, err)
	}
	dataArr, _ := resp["data"].([]any)
	for _, item := range dataArr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		eventName, _ := m["event_name"].(string)
		result, _ := m["result"].(map[string]any)
		var herr error
		switch eventName {
		case "PacketCreated":
			herr = idx.handlePacketCreated(ctx, txID, blockNum, result)
		case "PacketClaimed":
			herr = idx.handlePacketClaimed(ctx, txID, blockNum, result)
		case "PacketRefunded":
			herr = idx.handlePacketRefunded(ctx, txID, result)
		}
		if herr != nil {
			return herr
		}
	}
	return nil
}

func (idx *tronIndexer) handlePacketCreated(ctx context.Context, txID string, blockNum int64, result map[string]any) error {
	packetID := mapStr(result, "packetId")
	creator := tronAddrToHex(mapStr(result, "creator"))
	token := tronAddrToHex(mapStr(result, "token"))
	totalAmount := mapStr(result, "totalAmount")
	totalSharesStr := mapStr(result, "totalShares")
	totalShares := int32(0)
	if n, ok := new(big.Int).SetString(totalSharesStr, 10); ok {
		totalShares = int32(n.Int64())
	}

	now := time.Now()
	filter := bson.M{"packet_id": packetID}
	update := bson.M{
		"$set": bson.M{
			"packet_id":        packetID,
			"creator_wallet":   creator,
			"token":            token,
			"total_amount":     totalAmount,
			"total_shares":     totalShares,
			"chain":            "tron",
			"status":           statusActive,
			"contract_address": idx.tron.contractAddr,
			"tx_hash":          txID,
			"block_number":     blockNum,
			"updated_at":       now,
		},
		"$setOnInsert": bson.M{"created_at": now},
	}
	if _, err := idx.db.Collection(collRedPacket).UpdateOne(ctx, filter, update, options.Update().SetUpsert(true)); err != nil {
		return fmt.Errorf("PacketCreated upsert: %w", err)
	}
	return nil
}

func (idx *tronIndexer) handlePacketClaimed(ctx context.Context, txID string, blockNum int64, result map[string]any) error {
	packetID := mapStr(result, "packetId")
	claimer := tronAddrToHex(mapStr(result, "claimer"))
	authNonce := mapStr(result, "authNonce")
	claimedAmount := mapStr(result, "claimedAmount")

	now := time.Now()
	filter := bson.M{"packet_id": packetID, "claimer_wallet": claimer, "auth_nonce": authNonce}
	update := bson.M{
		"$set": bson.M{
			"claim_tx_hash":  txID,
			"claimed_amount": claimedAmount,
			"block_number":   blockNum,
			"status":         statusConfirmed,
			"updated_at":     now,
		},
		"$setOnInsert": bson.M{"created_at": now},
	}
	if _, err := idx.db.Collection(collClaims).UpdateOne(ctx, filter, update, options.Update().SetUpsert(true)); err != nil {
		return fmt.Errorf("PacketClaimed upsert: %w", err)
	}
	return nil
}

func (idx *tronIndexer) handlePacketRefunded(ctx context.Context, txID string, result map[string]any) error {
	packetID := mapStr(result, "packetId")
	if _, err := idx.db.Collection(collRedPacket).UpdateOne(ctx,
		bson.M{"packet_id": packetID},
		bson.M{"$set": bson.M{"status": statusRefunded, "updated_at": time.Now()}},
	); err != nil {
		return fmt.Errorf("PacketRefunded update: %w", err)
	}
	return nil
}

func (idx *tronIndexer) loadCursor(ctx context.Context) int64 {
	var doc cursorDoc
	err := idx.db.Collection(collCursor).FindOne(ctx, bson.M{"_id": cursorIDTRON}).Decode(&doc)
	if err != nil {
		return idx.startBlock
	}
	return doc.LastBlock
}

func (idx *tronIndexer) saveCursor(ctx context.Context, block int64) {
	opts := options.Update().SetUpsert(true)
	_, _ = idx.db.Collection(collCursor).UpdateOne(ctx,
		bson.M{"_id": cursorIDTRON},
		bson.M{"$set": bson.M{"last_block": block}},
		opts,
	)
}

// mapStr extracts a string value from a map, converting numeric values to string.
func mapStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%d", int64(val))
	default:
		return fmt.Sprintf("%v", val)
	}
}

// normaliseAddr strips the 0x/41 prefix and lowercases for comparison.
func normaliseAddr(addr string) string {
	addr = strings.ToLower(addr)
	addr = strings.TrimPrefix(addr, "41")
	addr = strings.TrimPrefix(addr, "0x")
	return addr
}

// Ensure abi import is used (it's needed for tronIndexer indirectly via tronClient).
var _ = abi.Arguments{}
