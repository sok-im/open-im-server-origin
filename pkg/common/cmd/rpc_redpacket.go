package cmd

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/internal/rpc/redpacket"
	"github.com/openimsdk/open-im-server/v3/pkg/common/startrpc"
	"github.com/openimsdk/open-im-server/v3/version"
	"github.com/openimsdk/tools/system/program"
	"github.com/spf13/cobra"
)

type RedPacketRpcCmd struct {
	*RootCmd
	ctx             context.Context
	configMap       map[string]any
	redpacketConfig *redpacket.Config
}

func NewRedPacketRpcCmd() *RedPacketRpcCmd {
	var redpacketConfig redpacket.Config
	ret := &RedPacketRpcCmd{redpacketConfig: &redpacketConfig}
	ret.configMap = map[string]any{
		OpenIMRPCRedPacketCfgFileName: &redpacketConfig.RpcConfig,
		MongodbConfigFileName:         &redpacketConfig.MongodbConfig,
		ShareFileName:                 &redpacketConfig.Share,
		DiscoveryConfigFilename:       &redpacketConfig.Discovery,
	}
	ret.RootCmd = NewRootCmd(program.GetProcessName(), WithConfigMap(ret.configMap))
	ret.ctx = context.WithValue(context.Background(), "version", version.Version)
	ret.Command.RunE = func(cmd *cobra.Command, args []string) error {
		return ret.runE()
	}
	return ret
}

func (c *RedPacketRpcCmd) Exec() error {
	return c.Execute()
}

func (c *RedPacketRpcCmd) runE() error {
	return startrpc.Start(c.ctx, &c.redpacketConfig.Discovery, &c.redpacketConfig.RpcConfig.Prometheus,
		c.redpacketConfig.RpcConfig.RPC.ListenIP,
		c.redpacketConfig.RpcConfig.RPC.RegisterIP,
		c.redpacketConfig.RpcConfig.RPC.AutoSetPorts,
		c.redpacketConfig.RpcConfig.RPC.Ports,
		c.Index(), c.redpacketConfig.Share.RpcRegisterName.Redpacket, &c.redpacketConfig.Share, c.redpacketConfig,
		nil,
		redpacket.Start)
}
