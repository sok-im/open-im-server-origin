package crypto

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	pbcrypto "github.com/openimsdk/protocol/crypto"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/discovery"
	"google.golang.org/grpc"
)

// Config aggregates all configuration needed by the Crypto service.
type Config struct {
	RpcConfig     config.Crypto
	MongodbConfig config.Mongo
	Share         config.Share
	Discovery     config.Discovery
}

type cryptoServer struct {
	pbcrypto.UnimplementedCryptoServiceServer
	config        *Config
	db            controller.CryptoDatabase
	virgilPrivKey ed25519.PrivateKey
	groupClient   *rpcli.GroupClient
	msgClient     *rpcli.MsgClient
	userClient    *rpcli.UserClient
}

// Start initialises the Crypto gRPC service and registers it with the gRPC server.
func Start(ctx context.Context, cfg *Config, client discovery.SvcDiscoveryRegistry, server *grpc.Server) error {
	mgocli, err := mongoutil.NewMongoDB(ctx, cfg.MongodbConfig.Build())
	if err != nil {
		return err
	}

	cryptoDB, err := mgo.NewCryptoMongo(mgocli.GetDB())
	if err != nil {
		return err
	}

	groupConn, err := client.GetConn(ctx, cfg.Share.RpcRegisterName.Group)
	if err != nil {
		return err
	}

	msgConn, err := client.GetConn(ctx, cfg.Share.RpcRegisterName.Msg)
	if err != nil {
		return err
	}

	userConn, err := client.GetConn(ctx, cfg.Share.RpcRegisterName.User)
	if err != nil {
		return err
	}

	virgilPrivKey, err := parseVirgilPrivateKey(cfg.RpcConfig.Virgil)
	if err != nil {
		return fmt.Errorf("virgil key init: %w", err)
	}

	s := &cryptoServer{
		config:        cfg,
		db:            controller.NewCryptoDatabase(cryptoDB),
		virgilPrivKey: virgilPrivKey,
		groupClient:   rpcli.NewGroupClient(groupConn),
		msgClient:     rpcli.NewMsgClient(msgConn),
		userClient:    rpcli.NewUserClient(userConn),
	}

	pbcrypto.RegisterCryptoServiceServer(server, s)
	return nil
}

func parseVirgilPrivateKey(cfg config.VirgilConfig) (ed25519.PrivateKey, error) {
	if cfg.APIKey == "" || cfg.APIKeyID == "" || cfg.AppID == "" {
		return nil, fmt.Errorf("virgil configuration is incomplete (appID/apiKeyID/apiKey required)")
	}
	keyBytes, err := base64.StdEncoding.DecodeString(cfg.APIKey)
	if err != nil {
		return nil, fmt.Errorf("decode virgil API key: %w", err)
	}
	switch len(keyBytes) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(keyBytes), nil
	case ed25519.PrivateKeySize:
		return keyBytes, nil
	default:
		return nil, fmt.Errorf("invalid virgil API key length: %d (expected %d or %d)", len(keyBytes), ed25519.SeedSize, ed25519.PrivateKeySize)
	}
}
