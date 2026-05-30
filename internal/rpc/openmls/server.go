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

package openmls

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	pbopenmls "github.com/openimsdk/protocol/openmls"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/discovery"
	"google.golang.org/grpc"
)

// Config 聚合 openim-rpc-openmls 需要的全部配置块。
type Config struct {
	RpcConfig     config.OpenMLS
	MongodbConfig config.Mongo
	Share         config.Share
	Discovery     config.Discovery
}

type openMLSServer struct {
	pbopenmls.UnimplementedOpenMLSServiceServer
	config     *Config
	db         controller.OpenMLSDatabase
	signingKey ed25519.PrivateKey // nil 表示 Credential 颁发功能未启用
	rootPubKey string             // base64(Ed25519 公钥)，用于 GetRootPublicKey 返回
}

// Start 初始化 openmls gRPC 服务并完成注册。
func Start(ctx context.Context, cfg *Config, _ discovery.SvcDiscoveryRegistry, server *grpc.Server) error {
	mgocli, err := mongoutil.NewMongoDB(ctx, cfg.MongodbConfig.Build())
	if err != nil {
		return err
	}
	db := mgocli.GetDB()

	kpDB, err := mgo.NewMLSKeyPackageMongo(db)
	if err != nil {
		return err
	}
	groupDB, err := mgo.NewMLSGroupStateMongo(db)
	if err != nil {
		return err
	}
	commitDB, err := mgo.NewMLSCommitMongo(db)
	if err != nil {
		return err
	}

	omlsDB := controller.NewOpenMLSDatabase(kpDB, groupDB, commitDB)

	var signingKey ed25519.PrivateKey
	var rootPubKeyB64 string
	if privB64 := cfg.RpcConfig.SigningKey.PrivateKey; privB64 != "" {
		privBytes, err := base64.StdEncoding.DecodeString(privB64)
		if err != nil {
			return fmt.Errorf("decode signing key: %w", err)
		}
		if len(privBytes) != ed25519.PrivateKeySize {
			return fmt.Errorf("signing key must be %d bytes, got %d", ed25519.PrivateKeySize, len(privBytes))
		}
		signingKey = ed25519.PrivateKey(privBytes)
		pubKey := signingKey.Public().(ed25519.PublicKey)
		rootPubKeyB64 = base64.StdEncoding.EncodeToString(pubKey)
	}

	pbopenmls.RegisterOpenMLSServiceServer(server, &openMLSServer{
		config:     cfg,
		db:         omlsDB,
		signingKey: signingKey,
		rootPubKey: rootPubKeyB64,
	})
	return nil
}
