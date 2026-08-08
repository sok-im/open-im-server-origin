package prommetrics

import (
	"context"
	"net"
	"strconv"
	"time"

	gp "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const rpcPath = commonPath

var (
	grpcMetrics *gp.ServerMetrics
	rpcCounter  = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rpc_count",
			Help: "Total number of RPC calls",
		},
		[]string{"name", "path", "code"},
	)
	rpcDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rpc_request_duration_seconds",
			Help:    "RPC request latency in seconds",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"name", "path", "code"},
	)
)

func RpcInit(cs []prometheus.Collector, listener net.Listener) error {
	reg := prometheus.NewRegistry()
	cs = append(append(
		baseCollector,
		rpcCounter,
		rpcDuration,
	), cs...)
	return Init(reg, listener, rpcPath, promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		Registry:          reg,
		EnableOpenMetrics: true,
	}), cs...)
}

func RPCCall(name string, path string, code int) {
	rpcCounter.With(prometheus.Labels{"name": name, "path": path, "code": strconv.Itoa(code)}).Inc()
}

func RPCObserve(name, path string, code int, duration time.Duration) {
	RPCObserveCtx(context.Background(), name, path, code, duration)
}

func RPCObserveCtx(ctx context.Context, name, path string, code int, duration time.Duration) {
	labels := prometheus.Labels{"name": name, "path": path, "code": strconv.Itoa(code)}
	rpcCounter.With(labels).Inc()
	observeWithExemplar(ctx, rpcDuration.With(labels), duration.Seconds())
}

func GetGrpcServerMetrics() *gp.ServerMetrics {
	if grpcMetrics == nil {
		grpcMetrics = gp.NewServerMetrics()
		grpcMetrics.EnableHandlingTimeHistogram()
	}
	return grpcMetrics
}

func GetGrpcCusMetrics(registerName string, share *config.Share) []prometheus.Collector {
	switch registerName {
	case share.RpcRegisterName.MessageGateway:
		return []prometheus.Collector{
			OnlineUserGauge,
			OnlineUserConnGauge,
			WSMaxConnGauge,
			WSConnRejectedCounter,
		}
	case share.RpcRegisterName.Msg:
		return []prometheus.Collector{
			SingleChatMsgProcessSuccessCounter,
			SingleChatMsgProcessFailedCounter,
			GroupChatMsgProcessSuccessCounter,
			GroupChatMsgProcessFailedCounter,
		}
	case share.RpcRegisterName.Push:
		return []prometheus.Collector{
			MsgOfflinePushFailedCounter,
			MsgLoneTimePushCounter,
		}
	case share.RpcRegisterName.Auth:
		return []prometheus.Collector{UserLoginCounter}
	case share.RpcRegisterName.User:
		return []prometheus.Collector{UserRegisterCounter}
	default:
		return nil
	}
}
