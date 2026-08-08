package prommetrics

import (
	"net"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	cronTaskRuns = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cron_task_runs_total",
			Help: "Total cron task executions",
		},
		[]string{"task", "result"},
	)
	cronTaskDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cron_task_duration_seconds",
			Help:    "Cron task execution duration in seconds",
			Buckets: []float64{0.1, 0.5, 1, 5, 15, 30, 60, 120, 300, 600},
		},
		[]string{"task"},
	)
	cronTaskLastSuccess = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cron_task_last_success_unixtime",
			Help: "Unix timestamp of last successful cron task run",
		},
		[]string{"task"},
	)
)

func CronInit(listener net.Listener) error {
	reg := prometheus.NewRegistry()
	cs := append(
		baseCollector,
		cronTaskRuns,
		cronTaskDuration,
		cronTaskLastSuccess,
	)
	return Init(reg, listener, commonPath, promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		Registry:          reg,
		EnableOpenMetrics: true,
	}), cs...)
}

func CronTaskObserve(task string, success bool, duration time.Duration) {
	result := "success"
	if !success {
		result = "failed"
	}
	cronTaskRuns.WithLabelValues(task, result).Inc()
	cronTaskDuration.WithLabelValues(task).Observe(duration.Seconds())
	if success {
		cronTaskLastSuccess.WithLabelValues(task).Set(float64(time.Now().Unix()))
	}
}
