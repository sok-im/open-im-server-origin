// Copyright © 2026 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package virgilsecurity

import (
	"context"
	"fmt"
	"time"

	"github.com/VirgilSecurity/virgil-sdk-go/cryptoimpl"
	"github.com/VirgilSecurity/virgil-sdk-go/sdk"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/redis"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	pbvirgil "github.com/openimsdk/protocol/virgilsecurity"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/redisutil"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/s3"
	"github.com/openimsdk/tools/s3/aws"
	"github.com/openimsdk/tools/s3/cos"
	"github.com/openimsdk/tools/s3/disable"
	"github.com/openimsdk/tools/s3/kodo"
	"github.com/openimsdk/tools/s3/minio"
	"github.com/openimsdk/tools/s3/oss"
	"google.golang.org/grpc"
)

const (
	defaultVirgilJWTTTL = 10 * time.Minute
	maxVirgilJWTTTL     = 60 * time.Minute
	minVirgilJWTTTL     = 1 * time.Minute
	defaultUploadTTL    = 30 * time.Minute
	defaultDownloadTTL  = 24 * time.Hour
)

// Config 聚合 openim-rpc-virgilsecurity 需要的全部配置块。
type Config struct {
	RpcConfig     config.VirgilSecurity
	RedisConfig   config.Redis
	MongodbConfig config.Mongo
	MinioConfig   config.Minio
	Share         config.Share
	Discovery     config.Discovery
}

type virgilSecurityServer struct {
	pbvirgil.UnimplementedVirgilSecurityServiceServer
	config       *Config
	db           controller.VirgilSecurityDatabase
	jwtGenerator *sdk.JwtGenerator
	s3           s3.Interface
}

// Start 初始化 virgilsecurity gRPC 服务并完成注册。
func Start(ctx context.Context, cfg *Config, _ discovery.SvcDiscoveryRegistry, server *grpc.Server) error {
	mgocli, err := mongoutil.NewMongoDB(ctx, cfg.MongodbConfig.Build())
	if err != nil {
		return err
	}
	db := mgocli.GetDB()

	deviceDB, err := mgo.NewVirgilDeviceMongo(db)
	if err != nil {
		return err
	}
	versionDB, err := mgo.NewVirgilUserDeviceVersionMongo(db)
	if err != nil {
		return err
	}
	eventDB, err := mgo.NewVirgilDeviceEventMongo(db)
	if err != nil {
		return err
	}
	convDB, err := mgo.NewVirgilConversationMongo(db)
	if err != nil {
		return err
	}
	fileRefDB, err := mgo.NewVirgilFileRefMongo(db)
	if err != nil {
		return err
	}

	vsDB := controller.NewVirgilSecurityDatabase(deviceDB, versionDB, eventDB, convDB, fileRefDB, mgocli.GetTx())

	var jwtGen *sdk.JwtGenerator
	vc := cfg.RpcConfig.Virgil
	if vc.AppID != "" && vc.AppKey != "" && vc.AppKeyID != "" {
		virgilCrypto := cryptoimpl.NewVirgilCrypto()
		privateKey, err := virgilCrypto.ImportPrivateKey([]byte(vc.AppKey), "")
		if err != nil {
			return fmt.Errorf("import virgil app key: %w", err)
		}
		jwtGen = sdk.NewJwtGenerator(
			privateKey,
			vc.AppKeyID,
			cryptoimpl.NewVirgilAccessTokenSigner(),
			vc.AppID,
			maxVirgilJWTTTL,
		)
	}

	s3iface, err := buildS3(ctx, cfg)
	if err != nil {
		return err
	}

	pbvirgil.RegisterVirgilSecurityServiceServer(server, &virgilSecurityServer{
		config:       cfg,
		db:           vsDB,
		jwtGenerator: jwtGen,
		s3:           s3iface,
	})
	return nil
}

// buildS3 根据 RpcConfig.Object.Enable 选择对应的 S3 后端。
// "" 时返回 disable 实现，避免在未启用 E2EE 文件能力时也强依赖对象存储。
func buildS3(ctx context.Context, cfg *Config) (s3.Interface, error) {
	enable := cfg.RpcConfig.Object.Enable
	switch enable {
	case "":
		return disable.NewDisable(), nil
	case "minio":
		rdb, err := redisutil.NewRedisClient(ctx, cfg.RedisConfig.Build())
		if err != nil {
			return nil, err
		}
		return minio.NewMinio(ctx, redis.NewMinioCache(rdb), *cfg.MinioConfig.Build())
	case "cos":
		return cos.NewCos(*cfg.RpcConfig.Object.Cos.Build())
	case "oss":
		return oss.NewOSS(*cfg.RpcConfig.Object.Oss.Build())
	case "kodo":
		return kodo.NewKodo(*cfg.RpcConfig.Object.Kodo.Build())
	case "aws":
		return aws.NewAws(*cfg.RpcConfig.Object.Aws.Build())
	default:
		return nil, fmt.Errorf("invalid object enable: %s", enable)
	}
}
