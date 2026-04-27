package redpacket

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/openimsdk/tools/log"
)

const (
	cursorIDETH  = "eth"
	indexerDelay = 5 * time.Second
	blockBatch   = int64(100)
)

type ethIndexer struct {
	eth        *ethClient
	db         *mongo.Database
	startBlock int64
}

func newEthIndexer(eth *ethClient, db *mongo.Database, startBlock int64) *ethIndexer {
	return &ethIndexer{eth: eth, db: db, startBlock: startBlock}
}

func (idx *ethIndexer) Run(ctx context.Context) {
	log.ZInfo(ctx, "eth indexer started")
	for {
		select {
		case <-ctx.Done():
			log.ZInfo(ctx, "eth indexer stopped")
			return
		default:
		}
		if err := idx.step(ctx); err != nil {
			log.ZWarn(ctx, "eth indexer step error", err)
		}
		time.Sleep(indexerDelay)
	}
}

func (idx *ethIndexer) step(ctx context.Context) error {
	from := idx.loadCursor(ctx)
	latest, err := idx.eth.client.BlockNumber(ctx)
	if err != nil {
		return err
	}
	if int64(latest) <= from {
		return nil
	}
	to := from + blockBatch
	if to > int64(latest) {
		to = int64(latest)
	}

	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(from + 1),
		ToBlock:   big.NewInt(to),
		Addresses: []common.Address{idx.eth.contractAddr},
	}
	logs, err := idx.eth.client.FilterLogs(ctx, query)
	if err != nil {
		return err
	}
	// Only advance the cursor when the entire batch was processed successfully.
	// Any per-log failure (DB outage, ABI mismatch, etc.) keeps the cursor in
	// place so the events get re-scanned on the next tick.
	hadError := false
	for i := range logs {
		if err := idx.processLog(ctx, &logs[i]); err != nil {
			hadError = true
			log.ZWarn(ctx, "eth indexer process log error", err, "txHash", logs[i].TxHash.Hex())
		}
	}
	if hadError {
		return fmt.Errorf("eth indexer batch had errors, cursor not advanced (block %d→%d)", from+1, to)
	}
	idx.saveCursor(ctx, to)
	return nil
}

func (idx *ethIndexer) processLog(ctx context.Context, l *types.Log) error {
	if len(l.Topics) == 0 {
		return nil
	}
	event, err := idx.eth.contractABI.EventByID(l.Topics[0])
	if err != nil {
		return nil // unknown event, skip
	}

	switch event.Name {
	case "PacketCreated":
		return idx.handlePacketCreated(ctx, l)
	case "PacketClaimed":
		return idx.handlePacketClaimed(ctx, l)
	case "PacketRefunded":
		return idx.handlePacketRefunded(ctx, l)
	}
	return nil
}

func (idx *ethIndexer) handlePacketCreated(ctx context.Context, l *types.Log) error {
	if len(l.Topics) < 3 {
		return fmt.Errorf("PacketCreated: not enough topics")
	}
	packetID := new(big.Int).SetBytes(l.Topics[1].Bytes()).String()
	creator := common.HexToAddress(l.Topics[2].Hex()).Hex()

	// Unpack non-indexed fields.
	vals := make(map[string]any)
	if len(l.Data) > 0 {
		_ = idx.eth.contractABI.UnpackIntoMap(vals, "PacketCreated", l.Data)
	}
	token := ""
	if t, ok := vals["token"].(common.Address); ok {
		token = t.Hex()
	}
	totalAmount := ""
	if ta, ok := vals["totalAmount"].(*big.Int); ok {
		totalAmount = ta.String()
	}
	totalShares := int32(0)
	if ts, ok := vals["totalShares"].(*big.Int); ok {
		totalShares = int32(ts.Int64())
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
			"chain":            "eth",
			"status":           statusActive,
			"contract_address": idx.eth.contractAddr.Hex(),
			"updated_at":       now,
		},
		"$setOnInsert": bson.M{"created_at": now},
	}
	_, err := idx.db.Collection(collRedPacket).UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (idx *ethIndexer) handlePacketClaimed(ctx context.Context, l *types.Log) error {
	if len(l.Topics) < 3 {
		return fmt.Errorf("PacketClaimed: not enough topics")
	}
	packetID := new(big.Int).SetBytes(l.Topics[1].Bytes()).String()
	claimer := common.HexToAddress(l.Topics[2].Hex()).Hex()

	vals := make(map[string]any)
	if len(l.Data) > 0 {
		_ = idx.eth.contractABI.UnpackIntoMap(vals, "PacketClaimed", l.Data)
	}
	authNonce := ""
	if an, ok := vals["authNonce"].(*big.Int); ok {
		authNonce = an.String()
	}
	claimedAmount := ""
	if ca, ok := vals["claimedAmount"].(*big.Int); ok {
		claimedAmount = ca.String()
	}

	now := time.Now()
	filter := bson.M{"packet_id": packetID, "claimer_wallet": claimer, "auth_nonce": authNonce}
	update := bson.M{
		"$set": bson.M{
			"claim_tx_hash":  l.TxHash.Hex(),
			"claimed_amount": claimedAmount,
			"block_number":   int64(l.BlockNumber),
			"status":         statusConfirmed,
			"updated_at":     now,
		},
		"$setOnInsert": bson.M{"created_at": now},
	}
	_, err := idx.db.Collection(collClaims).UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (idx *ethIndexer) handlePacketRefunded(ctx context.Context, l *types.Log) error {
	if len(l.Topics) < 2 {
		return fmt.Errorf("PacketRefunded: not enough topics")
	}
	packetID := new(big.Int).SetBytes(l.Topics[1].Bytes()).String()

	_, err := idx.db.Collection(collRedPacket).UpdateOne(ctx,
		bson.M{"packet_id": packetID},
		bson.M{"$set": bson.M{"status": statusRefunded, "updated_at": time.Now()}},
	)
	return err
}

func (idx *ethIndexer) loadCursor(ctx context.Context) int64 {
	var doc cursorDoc
	err := idx.db.Collection(collCursor).FindOne(ctx, bson.M{"_id": cursorIDETH}).Decode(&doc)
	if err != nil {
		return idx.startBlock
	}
	return doc.LastBlock
}

func (idx *ethIndexer) saveCursor(ctx context.Context, block int64) {
	opts := options.Update().SetUpsert(true)
	_, _ = idx.db.Collection(collCursor).UpdateOne(ctx,
		bson.M{"_id": cursorIDETH},
		bson.M{"$set": bson.M{"last_block": block}},
		opts,
	)
}
