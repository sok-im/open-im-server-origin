package redpacket

import (
	"context"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/openimsdk/open-im-server/v3/internal/rpc/redpacket/chain"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	pbredpacket "github.com/openimsdk/protocol/redpacket"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/log"
	"google.golang.org/grpc"
)

type Config struct {
	RpcConfig     config.RedPacket
	MongodbConfig config.Mongo
	Share         config.Share
	Discovery     config.Discovery
}

type redPacketServer struct {
	pbredpacket.UnimplementedRedPacketServer
	config         *Config
	db             controller.RedPacketDatabase
	chainRuntimes  map[string]*chainRuntime
	groupClient    *rpcli.GroupClient
	relationClient *rpcli.RelationClient
}

func Start(ctx context.Context, conf *Config, registry discovery.SvcDiscoveryRegistry, server *grpc.Server) error {
	mgoClient, err := mongoutil.NewMongoDB(ctx, conf.MongodbConfig.Build())
	if err != nil {
		return err
	}
	db := mgoClient.GetDB()

	rpDB, err := mgo.NewRedPacketMongo(db)
	if err != nil {
		return err
	}
	claimDB, err := mgo.NewRedPacketClaimMongo(db)
	if err != nil {
		return err
	}
	claimAuthDB, err := mgo.NewRedPacketClaimAuthMongo(db)
	if err != nil {
		return err
	}
	refundDB, err := mgo.NewRedPacketRefundMongo(db)
	if err != nil {
		return err
	}
	challengeDB, err := mgo.NewWalletBindingChallengeMongo(db)
	if err != nil {
		return err
	}
	bindingDB, err := mgo.NewWalletBindingMongo(db)
	if err != nil {
		return err
	}
	auditLogDB, err := mgo.NewAdminAuditLogMongo(db)
	if err != nil {
		return err
	}

	repo := controller.NewRedPacketDatabase(rpDB, claimDB, claimAuthDB, refundDB, challengeDB, bindingDB, auditLogDB)

	runtimes := make(map[string]*chainRuntime)
	runtimeCfgs := buildRuntimeConfigs(conf.RpcConfig)
	var abiJSON []byte
	for chainKey, runtimeCfg := range runtimeCfgs {
		chainType, typeErr := normalizeChainType(runtimeCfg.ChainType)
		if typeErr != nil {
			log.ZWarn(ctx, "skip redpacket runtime with unsupported chain type", typeErr, "chainKey", chainKey)
			continue
		}
		runtime := &chainRuntime{
			ChainKey:        chainKey,
			ChainType:       chainType,
			ChainID:         runtimeCfg.ChainID,
			ContractAddress: runtimeCfg.ContractAddress,
		}
		switch chainType {
		case "EVM":
			evmClient, clientErr := chain.NewClient(
				runtimeCfg.RPCURL,
				runtimeCfg.ContractAddress,
				runtimeCfg.ChainID,
				runtimeCfg.SignerPrivateKey,
				runtimeCfg.ConfigAdminPrivateKey,
			)
			if clientErr != nil {
				log.ZWarn(ctx, "redpacket evm client init failed, continuing without it", clientErr, "chainKey", chainKey)
			} else {
				runtime.EVMClient = evmClient
				if runtime.ChainID == 0 {
					if chainValue := evmClient.ChainID(); chainValue != nil {
						runtime.ChainID = chainValue.Int64()
					}
				}
				runtime.ContractAddress = evmClient.ContractAddress().Hex()
			}
			if k := runtimeCfg.SignerPrivateKey; k != "" {
				sk, parseErr := crypto.HexToECDSA(strings.TrimPrefix(k, "0x"))
				if parseErr != nil {
					log.ZWarn(ctx, "redpacket signer private key parse failed", parseErr, "chainKey", chainKey)
				} else {
					runtime.SignerKey = sk
				}
			}
		case "TRON":
			if len(abiJSON) == 0 {
				var abiErr error
				abiJSON, abiErr = chain.ExtractABIFromEmbeddedArtifact()
				if abiErr != nil {
					log.ZWarn(ctx, "redpacket tron load abi failed", abiErr)
				}
			}
			if len(abiJSON) > 0 {
				tronClient, clientErr := chain.NewTronClient(
					runtimeCfg.FullNodeURL,
					runtimeCfg.ContractBase58,
					runtimeCfg.OwnerBase58,
					runtimeCfg.PrivateKeyHex,
					abiJSON,
					runtimeCfg.FeeLimit,
				)
				if clientErr != nil {
					log.ZWarn(ctx, "redpacket tron client init failed", clientErr, "chainKey", chainKey)
				} else {
					runtime.TronClient = tronClient
					runtime.ContractAddress = tronClient.ContractAddress()
				}
			}
		}
		runtimes[chainKey] = runtime
	}

	groupConn, err := registry.GetConn(ctx, conf.Share.RpcRegisterName.Group)
	if err != nil {
		return err
	}
	friendConn, err := registry.GetConn(ctx, conf.Share.RpcRegisterName.Friend)
	if err != nil {
		return err
	}

	srv := &redPacketServer{
		config:         conf,
		db:             repo,
		chainRuntimes:  runtimes,
		groupClient:    rpcli.NewGroupClient(groupConn),
		relationClient: rpcli.NewRelationClient(friendConn),
	}

	pbredpacket.RegisterRedPacketServer(server, srv)

	if conf.RpcConfig.Indexer.PollInterval > 0 {
		for _, runtime := range runtimes {
			if runtime == nil {
				continue
			}
			if runtime.EVMClient != nil {
				ethIndexer := chain.NewIndexer(runtime.ChainKey, runtime.EVMClient, repo, conf.RpcConfig.Indexer.PollInterval, 0, conf.RpcConfig.Indexer.MaxBlocksPerPoll)
				ethIndexer.Start(ctx)
			}
			if runtime.TronClient != nil {
				tronIndexer := chain.NewTronIndexer(runtime.ChainKey, runtime.TronClient, repo, conf.RpcConfig.Indexer.PollInterval, 0)
				tronIndexer.Start(ctx)
			}
		}
	} else {
		log.ZInfo(ctx, "redpacket indexer disabled by config", "pollInterval", conf.RpcConfig.Indexer.PollInterval)
	}

	return nil
}
