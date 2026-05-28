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

package cmd

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/internal/rpc/virgilsecurity"
	"github.com/openimsdk/open-im-server/v3/pkg/common/startrpc"
	"github.com/openimsdk/open-im-server/v3/version"
	"github.com/openimsdk/tools/system/program"
	"github.com/spf13/cobra"
)

type VirgilSecurityRpcCmd struct {
	*RootCmd
	ctx       context.Context
	configMap map[string]any
	cfg       *virgilsecurity.Config
}

func NewVirgilSecurityRpcCmd() *VirgilSecurityRpcCmd {
	var cfg virgilsecurity.Config
	ret := &VirgilSecurityRpcCmd{cfg: &cfg}
	ret.configMap = map[string]any{
		OpenIMRPCVirgilSecurityCfgFileName: &cfg.RpcConfig,
		RedisConfigFileName:                &cfg.RedisConfig,
		MongodbConfigFileName:              &cfg.MongodbConfig,
		MinioConfigFileName:                &cfg.MinioConfig,
		ShareFileName:                      &cfg.Share,
		DiscoveryConfigFilename:            &cfg.Discovery,
	}
	ret.RootCmd = NewRootCmd(program.GetProcessName(), WithConfigMap(ret.configMap))
	ret.ctx = context.WithValue(context.Background(), "version", version.Version)
	ret.Command.RunE = func(cmd *cobra.Command, args []string) error {
		return ret.runE()
	}
	return ret
}

func (c *VirgilSecurityRpcCmd) Exec() error {
	return c.Execute()
}

func (c *VirgilSecurityRpcCmd) runE() error {
	return startrpc.Start(c.ctx,
		&c.cfg.Discovery,
		&c.cfg.RpcConfig.Prometheus,
		c.cfg.RpcConfig.RPC.ListenIP,
		c.cfg.RpcConfig.RPC.RegisterIP,
		c.cfg.RpcConfig.RPC.AutoSetPorts,
		c.cfg.RpcConfig.RPC.Ports,
		c.Index(),
		c.cfg.Share.RpcRegisterName.VirgilSecurity,
		&c.cfg.Share,
		c.cfg,
		nil,
		virgilsecurity.Start,
	)
}
