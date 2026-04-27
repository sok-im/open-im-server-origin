package redpacket

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	pbredpacket "github.com/openimsdk/protocol/redpacket"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/grpc"
)

// Config holds all configuration for the redpacket service.
type Config struct {
	RpcConfig     config.RedPacket
	MongodbConfig config.Mongo
	Share         config.Share
	Discovery     config.Discovery
}

// server is the gRPC redpacket server.
type server struct {
	pbredpacket.UnimplementedRedPacketServer
	conf   config.RedPacket
	db     *mongo.Database
	eth    *ethClient
	tron   *tronClient
}

func Start(ctx context.Context, cfg *Config, _ discovery.SvcDiscoveryRegistry, grpcServer *grpc.Server) error {
	mongoClient, err := mongoutil.NewMongoDB(ctx, cfg.MongodbConfig.Build())
	if err != nil {
		log.ZError(ctx, "redpacket connect mongodb failed", err)
		return err
	}
	db := mongoClient.GetDB()

	// Create indexes for redpacket collection.
	// biz_id index is unique but partial — chain-indexer-created records have no
	// biz_id and would otherwise collide on the empty/null value.
	rpColl := db.Collection(collRedPacket)
	if _, err := rpColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "biz_id", Value: 1}},
			Options: options.Index().
				SetUnique(true).
				SetPartialFilterExpression(bson.M{"biz_id": bson.M{"$type": "string", "$gt": ""}}),
		},
		{Keys: bson.D{{Key: "packet_id", Value: 1}}},
	}); err != nil {
		log.ZError(ctx, "redpacket create indexes failed", err)
		return err
	}

	// Create indexes for claims collection.
	claimColl := db.Collection(collClaims)
	if _, err := claimColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "packet_id", Value: 1}, {Key: "claimer_wallet", Value: 1}, {Key: "auth_nonce", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "packet_id", Value: 1}}},
	}); err != nil {
		log.ZError(ctx, "redpacket create claim indexes failed", err)
		return err
	}

	s := &server{
		conf: cfg.RpcConfig,
		db:   db,
	}

	// Initialise ETH client if configured.
	if cfg.RpcConfig.ETH.RPCURL != "" && cfg.RpcConfig.ETH.ContractAddress != "" {
		ec, err := newEthClient(cfg.RpcConfig.ETH)
		if err != nil {
			log.ZError(ctx, "redpacket init eth client failed", err)
			return err
		}
		s.eth = ec

		// Start ETH indexer in background.
		go func() {
			idx := newEthIndexer(ec, db, cfg.RpcConfig.ETH.StartBlock)
			idx.Run(ctx)
		}()
	}

	// Initialise TRON client if configured.
	if cfg.RpcConfig.TRON.NodeURL != "" && cfg.RpcConfig.TRON.ContractAddress != "" {
		tc := newTronClient(cfg.RpcConfig.TRON)
		s.tron = tc

		// Start TRON indexer in background.
		go func() {
			idx := newTronIndexer(tc, db, cfg.RpcConfig.TRON.StartBlock)
			idx.Run(ctx)
		}()
	}

	pbredpacket.RegisterRedPacketServer(grpcServer, s)
	return nil
}

// ─── User RPCs ───────────────────────────────────────────────────────────────

func (s *server) CreateOrder(ctx context.Context, req *pbredpacket.CreateOrderReq) (*pbredpacket.CreateOrderResp, error) {
	if req.CreatorWallet == "" || req.TotalAmount == "" {
		return nil, servererrs.ErrArgs.WrapMsg("creator_wallet and total_amount are required")
	}
	now := time.Now()
	doc := redPacketDoc{
		BizID:         uuid.NewString(),
		CreatorUserID: req.CreatorUserId,
		CreatorWallet: req.CreatorWallet,
		PacketType:    int(req.PacketType),
		Token:         req.Token,
		TotalAmount:   req.TotalAmount,
		TotalShares:   int(req.TotalShares),
		ExpiryAt:      req.ExpiryAt,
		Chain:         req.Chain,
		Status:        statusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, err := s.db.Collection(collRedPacket).InsertOne(ctx, doc); err != nil {
		log.ZError(ctx, "redpacket insert failed", err)
		return nil, servererrs.ErrDatabase.WrapMsg("create order failed")
	}
	return &pbredpacket.CreateOrderResp{BizId: doc.BizID}, nil
}

func (s *server) CreatedCallback(ctx context.Context, req *pbredpacket.CreatedCallbackReq) (*pbredpacket.CreatedCallbackResp, error) {
	if req.BizId == "" || req.TxHash == "" || req.PacketId == "" {
		return nil, servererrs.ErrArgs.WrapMsg("biz_id, tx_hash and packet_id are required")
	}
	res, err := s.db.Collection(collRedPacket).UpdateOne(ctx,
		bson.M{"biz_id": req.BizId, "status": statusPending},
		bson.M{"$set": bson.M{
			"packet_id":  req.PacketId,
			"tx_hash":    req.TxHash,
			"status":     statusActive,
			"updated_at": time.Now(),
		}},
	)
	if err != nil {
		log.ZError(ctx, "redpacket callback update failed", err)
		return nil, servererrs.ErrDatabase.WrapMsg("created callback failed")
	}
	if res.MatchedCount == 0 {
		return nil, servererrs.ErrRecordNotFound.WrapMsg("biz_id not found or already confirmed", "biz_id", req.BizId)
	}
	return &pbredpacket.CreatedCallbackResp{Ok: true}, nil
}

func (s *server) GetDetail(ctx context.Context, req *pbredpacket.GetDetailReq) (*pbredpacket.GetDetailResp, error) {
	if req.PacketId == "" {
		return nil, servererrs.ErrArgs.WrapMsg("packet_id is required")
	}
	var doc redPacketDoc
	if err := s.db.Collection(collRedPacket).FindOne(ctx, bson.M{"packet_id": req.PacketId}).Decode(&doc); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, servererrs.ErrRecordNotFound.WrapMsg("packet not found", "packet_id", req.PacketId)
		}
		return nil, servererrs.ErrDatabase.WrapMsg("get detail query failed")
	}

	cursor, err := s.db.Collection(collClaims).Find(ctx, bson.M{"packet_id": req.PacketId})
	if err != nil {
		return nil, servererrs.ErrDatabase.WrapMsg("get claims failed")
	}
	defer cursor.Close(ctx)

	var claimDocs []claimDoc
	if err := cursor.All(ctx, &claimDocs); err != nil {
		return nil, servererrs.ErrDatabase.WrapMsg("decode claims failed")
	}

	resp := &pbredpacket.GetDetailResp{
		BizRecord: redPacketDocToPb(&doc),
		Claims:    make([]*pbredpacket.ClaimRecord, 0, len(claimDocs)),
	}
	for i := range claimDocs {
		resp.Claims = append(resp.Claims, claimDocToPb(&claimDocs[i]))
	}
	return resp, nil
}

func (s *server) ClaimSign(ctx context.Context, req *pbredpacket.ClaimSignReq) (*pbredpacket.ClaimSignResp, error) {
	if req.PacketId == "" || req.Claimer == "" {
		return nil, servererrs.ErrArgs.WrapMsg("packet_id and claimer are required")
	}

	// Check packet exists and is active.
	var doc redPacketDoc
	if err := s.db.Collection(collRedPacket).FindOne(ctx, bson.M{"packet_id": req.PacketId, "status": statusActive}).Decode(&doc); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, servererrs.ErrRecordNotFound.WrapMsg("packet not found or not active", "packet_id", req.PacketId)
		}
		return nil, servererrs.ErrDatabase.WrapMsg("claim sign query failed")
	}

	// Check if already claimed by this claimer.
	count, err := s.db.Collection(collClaims).CountDocuments(ctx, bson.M{
		"packet_id":      req.PacketId,
		"claimer_wallet": req.Claimer,
		"status":         statusConfirmed,
	})
	if err != nil {
		return nil, servererrs.ErrDatabase.WrapMsg("check claim failed")
	}
	if count > 0 {
		return nil, servererrs.ErrNoPermission.WrapMsg("already claimed")
	}

	// Generate authNonce and deadline.
	nonceBig, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		return nil, servererrs.ErrInternalServer.WrapMsg("generate nonce failed")
	}
	authNonce := nonceBig.String()

	// Parse randomSeed (generate if empty/zero).
	randomSeed := req.RandomSeed
	if randomSeed == "" || randomSeed == "0" {
		seed, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
		if err != nil {
			return nil, servererrs.ErrInternalServer.WrapMsg("generate random seed failed")
		}
		randomSeed = seed.String()
	}

	deadline := time.Now().Add(10 * time.Minute).Unix()

	// Sign using ETH or TRON client based on the packet's chain. Both paths
	// fetch the digest from the contract on-chain to stay byte-identical with
	// the contract's getSignMessage implementation.
	var sig string
	switch doc.Chain {
	case "tron":
		if s.tron == nil {
			return nil, servererrs.ErrInternalServer.WrapMsg("TRON client is not configured")
		}
		sig, err = s.tron.SignClaim(ctx, req.PacketId, req.Claimer, authNonce, randomSeed, deadline)
	default: // "eth" or empty
		if s.eth == nil {
			return nil, servererrs.ErrInternalServer.WrapMsg("ETH client is not configured")
		}
		sig, err = s.eth.SignClaim(ctx, req.PacketId, req.Claimer, authNonce, randomSeed, deadline)
	}
	if err != nil {
		log.ZError(ctx, "redpacket sign claim failed", err)
		return nil, servererrs.ErrInternalServer.WrapMsg("failed to issue claim signature: " + err.Error())
	}

	// Persist claim auth record.
	now := time.Now()
	authDoc := claimAuthDoc{
		PacketID:     req.PacketId,
		ClaimerWallet: req.Claimer,
		UserID:       req.UserId,
		AuthNonce:    authNonce,
		RandomSeed:   randomSeed,
		Deadline:     deadline,
		Signature:    sig,
		CreatedAt:    now,
	}
	if _, err := s.db.Collection(collClaimAuth).InsertOne(ctx, authDoc); err != nil {
		log.ZError(ctx, "redpacket insert claim auth failed", err)
		// Non-fatal: signature is already issued.
	}

	return &pbredpacket.ClaimSignResp{
		AuthNonce:  authNonce,
		Deadline:   deadline,
		Signature:  sig,
		RandomSeed: randomSeed,
	}, nil
}

func (s *server) ClaimResult(ctx context.Context, req *pbredpacket.ClaimResultReq) (*pbredpacket.ClaimResultResp, error) {
	if req.PacketId == "" || req.TxHash == "" {
		return nil, servererrs.ErrArgs.WrapMsg("packet_id and tx_hash are required")
	}
	now := time.Now()
	filter := bson.M{"packet_id": req.PacketId, "claimer_wallet": req.ClaimerWallet, "auth_nonce": req.AuthNonce}
	// claim_tx_hash is always updated (we want the latest hash the client reported).
	// status is intentionally NOT in $set: when the indexer has already observed
	// the on-chain PacketClaimed event and set status to CONFIRMED, we must not
	// regress it to PENDING. status only gets set on the first insert.
	update := bson.M{
		"$set": bson.M{
			"claim_tx_hash": req.TxHash,
			"updated_at":    now,
		},
		"$setOnInsert": bson.M{
			"status":     statusPending,
			"created_at": now,
		},
	}
	opts := options.Update().SetUpsert(true)
	if _, err := s.db.Collection(collClaims).UpdateOne(ctx, filter, update, opts); err != nil {
		log.ZError(ctx, "redpacket claim result upsert failed", err)
		return nil, servererrs.ErrDatabase.WrapMsg("claim result failed")
	}
	return &pbredpacket.ClaimResultResp{Ok: true}, nil
}

// ─── Admin RPCs ───────────────────────────────────────────────────────────────

func (s *server) SetSigner(ctx context.Context, req *pbredpacket.SetSignerReq) (*pbredpacket.TxResp, error) {
	txHash, err := s.adminTx(ctx, req.Chain, "setSigner", req.NewSigner)
	if err != nil {
		return nil, err
	}
	return &pbredpacket.TxResp{TxHash: txHash}, nil
}

func (s *server) SetToken(ctx context.Context, req *pbredpacket.SetTokenReq) (*pbredpacket.TxResp, error) {
	txHash, err := s.adminTx(ctx, req.Chain, "setAllowedToken", req.Token, req.Allowed, req.MinShareAmount)
	if err != nil {
		return nil, err
	}
	return &pbredpacket.TxResp{TxHash: txHash}, nil
}

func (s *server) SetExpiry(ctx context.Context, req *pbredpacket.SetExpiryReq) (*pbredpacket.TxResp, error) {
	txHash, err := s.adminTx(ctx, req.Chain, "setDefaultExpiryDuration", req.Duration)
	if err != nil {
		return nil, err
	}
	return &pbredpacket.TxResp{TxHash: txHash}, nil
}

func (s *server) SetAllowAllTokens(ctx context.Context, req *pbredpacket.SetAllowAllTokensReq) (*pbredpacket.TxResp, error) {
	txHash, err := s.adminTx(ctx, req.Chain, "setAllowAllTokens", req.Allow)
	if err != nil {
		return nil, err
	}
	return &pbredpacket.TxResp{TxHash: txHash}, nil
}

func (s *server) SetNativeToken(ctx context.Context, req *pbredpacket.SetNativeTokenReq) (*pbredpacket.TxResp, error) {
	txHash, err := s.adminTx(ctx, req.Chain, "setNativeTokenEnabled", req.Enabled)
	if err != nil {
		return nil, err
	}
	return &pbredpacket.TxResp{TxHash: txHash}, nil
}

func (s *server) ParseTxEvents(ctx context.Context, req *pbredpacket.ParseTxEventsReq) (*pbredpacket.ParseTxEventsResp, error) {
	switch req.Chain {
	case "tron":
		if s.tron == nil {
			return nil, servererrs.ErrInternalServer.WrapMsg("TRON client is not configured")
		}
		events, err := s.tron.ParseTxEvents(ctx, req.TxHash)
		if err != nil {
			return nil, servererrs.ErrInternalServer.WrapMsg(err.Error())
		}
		return &pbredpacket.ParseTxEventsResp{Events: events}, nil
	case "eth", "":
		if s.eth == nil {
			return nil, servererrs.ErrInternalServer.WrapMsg("ETH client is not configured")
		}
		events, err := s.eth.ParseTxEvents(ctx, req.TxHash)
		if err != nil {
			return nil, servererrs.ErrInternalServer.WrapMsg(err.Error())
		}
		return &pbredpacket.ParseTxEventsResp{Events: events}, nil
	default:
		return nil, servererrs.ErrArgs.WrapMsg(`chain must be "eth" or "tron"`)
	}
}

// adminTx dispatches an admin transaction to the appropriate chain client.
func (s *server) adminTx(ctx context.Context, chain, method string, args ...any) (string, error) {
	switch chain {
	case "tron":
		if s.tron == nil {
			return "", servererrs.ErrInternalServer.WrapMsg("TRON client is not configured")
		}
		return s.tron.SendAdminTx(ctx, method, args...)
	default:
		if s.eth == nil {
			return "", servererrs.ErrInternalServer.WrapMsg("ETH client is not configured")
		}
		return s.eth.SendAdminTx(ctx, method, args...)
	}
}
