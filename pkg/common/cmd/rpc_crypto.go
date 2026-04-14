package cmd

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/internal/rpc/crypto"
	"github.com/openimsdk/open-im-server/v3/pkg/common/startrpc"
	"github.com/openimsdk/open-im-server/v3/version"
	"github.com/openimsdk/tools/system/program"
	"github.com/spf13/cobra"
)

type CryptoRpcCmd struct {
	*RootCmd
	ctx          context.Context
	configMap    map[string]any
	cryptoConfig *crypto.Config
}

func NewCryptoRpcCmd() *CryptoRpcCmd {
	var cryptoConfig crypto.Config
	ret := &CryptoRpcCmd{cryptoConfig: &cryptoConfig}
	ret.configMap = map[string]any{
		OpenIMRPCCryptoCfgFileName: &cryptoConfig.RpcConfig,
		MongodbConfigFileName:      &cryptoConfig.MongodbConfig,
		ShareFileName:              &cryptoConfig.Share,
		DiscoveryConfigFilename:    &cryptoConfig.Discovery,
	}
	ret.RootCmd = NewRootCmd(program.GetProcessName(), WithConfigMap(ret.configMap))
	ret.ctx = context.WithValue(context.Background(), "version", version.Version)
	ret.Command.RunE = func(cmd *cobra.Command, args []string) error {
		return ret.runE()
	}
	return ret
}

func (a *CryptoRpcCmd) Exec() error {
	return a.Execute()
}

func (a *CryptoRpcCmd) runE() error {
	return startrpc.Start(a.ctx, &a.cryptoConfig.Discovery, &a.cryptoConfig.RpcConfig.Prometheus,
		a.cryptoConfig.RpcConfig.RPC.ListenIP,
		a.cryptoConfig.RpcConfig.RPC.RegisterIP,
		a.cryptoConfig.RpcConfig.RPC.AutoSetPorts,
		a.cryptoConfig.RpcConfig.RPC.Ports,
		a.Index(),
		a.cryptoConfig.Share.RpcRegisterName.Crypto,
		&a.cryptoConfig.Share,
		a.cryptoConfig,
		nil,
		crypto.Start)
}
