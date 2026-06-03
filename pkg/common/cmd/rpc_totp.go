package cmd

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/internal/rpc/totp"
	"github.com/openimsdk/open-im-server/v3/pkg/common/startrpc"
	"github.com/openimsdk/open-im-server/v3/version"
	"github.com/openimsdk/tools/system/program"
	"github.com/spf13/cobra"
)

type TotpRpcCmd struct {
	*RootCmd
	ctx        context.Context
	configMap  map[string]any
	totpConfig *totp.Config
}

func NewTotpRpcCmd() *TotpRpcCmd {
	var totpConfig totp.Config
	ret := &TotpRpcCmd{totpConfig: &totpConfig}
	ret.configMap = map[string]any{
		OpenIMRPCTotpCfgFileName: &totpConfig.RpcConfig,
		MongodbConfigFileName:    &totpConfig.MongodbConfig,
		RedisConfigFileName:      &totpConfig.RedisConfig,
		ShareFileName:            &totpConfig.Share,
		DiscoveryConfigFilename:  &totpConfig.Discovery,
	}
	ret.RootCmd = NewRootCmd(program.GetProcessName(), WithConfigMap(ret.configMap))
	ret.ctx = context.WithValue(context.Background(), "version", version.Version)
	ret.Command.RunE = func(cmd *cobra.Command, args []string) error {
		return ret.runE()
	}
	return ret
}

func (c *TotpRpcCmd) Exec() error {
	return c.Execute()
}

func (c *TotpRpcCmd) runE() error {
	return startrpc.Start(
		c.ctx,
		&c.totpConfig.Discovery,
		&c.totpConfig.RpcConfig.Prometheus,
		c.totpConfig.RpcConfig.RPC.ListenIP,
		c.totpConfig.RpcConfig.RPC.RegisterIP,
		c.totpConfig.RpcConfig.RPC.AutoSetPorts,
		c.totpConfig.RpcConfig.RPC.Ports,
		c.Index(),
		c.totpConfig.Share.RpcRegisterName.Totp,
		&c.totpConfig.Share,
		c.totpConfig,
		nil,
		totp.Start,
	)
}
