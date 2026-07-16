// Copyright © 2024 OpenIM. All rights reserved.
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

package rtc

import (
	"context"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	cacheredis "github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/redis"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/protocol/rtc"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/redisutil"
	"github.com/openimsdk/tools/discovery"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
)

// Config aggregates all configuration needed by the RTC service.
type Config struct {
	RpcConfig          config.Rtc
	MongodbConfig      config.Mongo
	RedisConfig        config.Redis
	Share              config.Share
	Discovery          config.Discovery
	NotificationConfig config.Notification
}

type rtcServer struct {
	rtc.UnimplementedRtcServiceServer
	config             *Config
	db                 controller.RtcDatabase
	globalBlackDB      controller.UserGlobalBlackDatabase
	userDB             database.User
	roomClient         *lksdk.RoomServiceClient
	msgClient          *rpcli.MsgClient
	userClient         *rpcli.UserClient
	groupClient        *rpcli.GroupClient
	relationClient     *rpcli.RelationClient
	rdb                redis.UniversalClient
	tokenExpiry        time.Duration
	e2eeTokenExpiry    time.Duration
	e2eeAllowedSchemes []string
	e2eeMinVersion     int
	callStatusCache    cache.CallStatusCache
}

// Start initialises the RTC gRPC service and registers it with the gRPC server.
func Start(ctx context.Context, cfg *Config, client discovery.SvcDiscoveryRegistry, server *grpc.Server) error {
	mgocli, err := mongoutil.NewMongoDB(ctx, cfg.MongodbConfig.Build())
	if err != nil {
		return err
	}

	rdb, err := redisutil.NewRedisClient(ctx, cfg.RedisConfig.Build())
	if err != nil {
		return err
	}

	signalDB, err := mgo.NewSignalMongo(mgocli.GetDB())
	if err != nil {
		return err
	}

	globalBlackMgo, err := mgo.NewUserGlobalBlackMongo(mgocli.GetDB())
	if err != nil {
		return err
	}
	userMgo, err := mgo.NewUserMongo(mgocli.GetDB())
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

	friendConn, err := client.GetConn(ctx, cfg.Share.RpcRegisterName.Friend)
	if err != nil {
		return err
	}

	groupConn, err := client.GetConn(ctx, cfg.Share.RpcRegisterName.Group)
	if err != nil {
		return err
	}

	lk := cfg.RpcConfig.LiveKit
	roomClient := lksdk.NewRoomServiceClient(lk.InternalAddress, lk.APIKey, lk.APISecret)

	tokenExpiry := time.Duration(lk.TokenExpiry) * time.Second
	if tokenExpiry <= 0 {
		tokenExpiry = time.Hour
	}
	e2eeTokenExpiry := time.Duration(lk.E2EETokenExpiry) * time.Second
	if e2eeTokenExpiry <= 0 || e2eeTokenExpiry > 5*time.Minute {
		e2eeTokenExpiry = 5 * time.Minute
	}
	allowedSchemes := cfg.RpcConfig.E2EE.AllowedSchemes
	if len(allowedSchemes) == 0 {
		allowedSchemes = []string{"mls-exporter-livekit-v1"}
	}
	e2eeMinVersion := cfg.RpcConfig.E2EE.MinVersion
	if e2eeMinVersion <= 0 {
		e2eeMinVersion = 1
	}

	callStatusTTL := time.Duration(cfg.RpcConfig.CallStatusTTL) * time.Second
	if callStatusTTL <= 0 {
		callStatusTTL = 10 * time.Second
	}

	s := &rtcServer{
		config:             cfg,
		db:                 controller.NewRtcDatabase(signalDB),
		globalBlackDB:      controller.NewUserGlobalBlackDatabase(globalBlackMgo),
		userDB:             userMgo,
		roomClient:         roomClient,
		msgClient:          rpcli.NewMsgClient(msgConn),
		userClient:         rpcli.NewUserClient(userConn),
		groupClient:        rpcli.NewGroupClient(groupConn),
		relationClient:     rpcli.NewRelationClient(friendConn),
		rdb:                rdb,
		tokenExpiry:        tokenExpiry,
		e2eeTokenExpiry:    e2eeTokenExpiry,
		e2eeAllowedSchemes: allowedSchemes,
		e2eeMinVersion:     e2eeMinVersion,
		callStatusCache:    cacheredis.NewCallStatusCache(rdb, callStatusTTL),
	}

	rtc.RegisterRtcServiceServer(server, s)
	return nil
}
