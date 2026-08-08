package otelx

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

func GrpcServerOptions() []grpc.ServerOption {
	if !Enabled() {
		return nil
	}
	return []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	}
}

func GrpcClientDialOptions() []grpc.DialOption {
	if !Enabled() {
		return nil
	}
	return []grpc.DialOption{
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	}
}
