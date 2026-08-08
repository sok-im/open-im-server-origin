package prommetrics

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/trace"
)

var (
	apiCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "api_count",
			Help: "Total number of API calls",
		},
		[]string{"module", "path", "method", "code"},
	)
	httpCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_count",
			Help: "Total number of HTTP calls",
		},
		[]string{"module", "path", "method", "status"},
	)
	apiDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "api_request_duration_seconds",
			Help:    "API request latency in seconds",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"module", "path", "method", "code"},
	)
)

func ApiInit(listener net.Listener) error {
	apiRegistry := prometheus.NewRegistry()
	cs := append(
		baseCollector,
		apiCounter,
		httpCounter,
		apiDuration,
	)
	return Init(apiRegistry, listener, commonPath, promhttp.HandlerFor(apiRegistry, promhttp.HandlerOpts{
		Registry:          apiRegistry,
		EnableOpenMetrics: true,
	}), cs...)
}

func APIModuleFromPath(path string) string {
	path = strings.TrimPrefix(path, "/")
	if path == "" || path == "<404>" || path == "<unmatched>" {
		return "unknown"
	}
	parts := strings.Split(path, "/")
	if len(parts) >= 2 && (parts[0] == "virgil" || parts[0] == "openmls" || parts[0] == "crypto") {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// NormalizeMetricPath prefers Gin route templates to avoid high-cardinality raw URL paths.
func NormalizeMetricPath(fullPath, rawPath string, status int) string {
	if status == 404 {
		return "<404>"
	}
	if fullPath != "" && fullPath != "/" {
		return fullPath
	}
	// Never emit raw request paths (may contain userIDs / tokens) into Prometheus labels.
	_ = rawPath
	return "<unmatched>"
}

func APICall(module, path, method string, apiCode int) {
	apiCounter.WithLabelValues(module, path, method, strconv.Itoa(apiCode)).Inc()
}

func APIObserve(module, path, method string, apiCode int, duration time.Duration) {
	APIObserveCtx(context.Background(), module, path, method, apiCode, duration)
}

func APIObserveCtx(ctx context.Context, module, path, method string, apiCode int, duration time.Duration) {
	labels := []string{module, path, method, strconv.Itoa(apiCode)}
	apiCounter.WithLabelValues(labels...).Inc()
	observeWithExemplar(ctx, apiDuration.WithLabelValues(labels...), duration.Seconds())
}

func HttpCall(module, path, method string, status int) {
	httpCounter.WithLabelValues(module, path, method, strconv.Itoa(status)).Inc()
}

func observeWithExemplar(ctx context.Context, observer prometheus.Observer, value float64) {
	if eo, ok := observer.(prometheus.ExemplarObserver); ok {
		if sc := trace.SpanFromContext(ctx).SpanContext(); sc.IsValid() && sc.IsSampled() {
			eo.ObserveWithExemplar(value, prometheus.Labels{
				"trace_id": sc.TraceID().String(),
			})
			return
		}
	}
	observer.Observe(value)
}
