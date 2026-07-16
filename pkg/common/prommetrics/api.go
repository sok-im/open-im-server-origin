package prommetrics

import (
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	return Init(apiRegistry, listener, commonPath, promhttp.HandlerFor(apiRegistry, promhttp.HandlerOpts{}), cs...)
}

func APIModuleFromPath(path string) string {
	path = strings.TrimPrefix(path, "/")
	if path == "" || path == "<404>" {
		return "unknown"
	}
	parts := strings.Split(path, "/")
	if len(parts) >= 2 && (parts[0] == "virgil" || parts[0] == "openmls" || parts[0] == "crypto") {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

func APICall(module, path, method string, apiCode int) {
	apiCounter.WithLabelValues(module, path, method, strconv.Itoa(apiCode)).Inc()
}

func APIObserve(module, path, method string, apiCode int, duration time.Duration) {
	labels := []string{module, path, method, strconv.Itoa(apiCode)}
	apiCounter.WithLabelValues(labels...).Inc()
	apiDuration.WithLabelValues(labels...).Observe(duration.Seconds())
}

func HttpCall(module, path, method string, status int) {
	httpCounter.WithLabelValues(module, path, method, strconv.Itoa(status)).Inc()
}

//func ApiHandler() http.Handler {
//	return promhttp.InstrumentMetricHandler(
//		apiRegistry, promhttp.HandlerFor(apiRegistry, promhttp.HandlerOpts{}),
//	)
//}
