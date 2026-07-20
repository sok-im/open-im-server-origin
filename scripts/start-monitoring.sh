#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

MONITORING_SERVICES=(
  prometheus grafana alertmanager node-exporter redis-exporter mongodb-exporter kafka-exporter
  tempo otel-collector loki promtail
)

usage() {
  cat <<EOF
Usage: $(basename "$0") [start|restart]

  start    Start monitoring stack (default)
  restart  Restart monitoring stack
EOF
}

check_docker() {
  if ! docker info >/dev/null 2>&1; then
    echo "Docker daemon is not running. Start Docker Desktop first."
    exit 1
  fi
}

start_stack() {
  echo "Starting monitoring and tracing stack..."
  docker compose --profile m up -d "${MONITORING_SERVICES[@]}"
}

restart_stack() {
  echo "Restarting monitoring and tracing stack..."
  docker compose --profile m restart "${MONITORING_SERVICES[@]}"
}

print_info() {
  echo
  echo "Monitoring stack endpoints:"
  echo "  Grafana:         http://127.0.0.1:${GRAFANA_PORT:-13000}"
  echo "  Prometheus:      http://127.0.0.1:${PROMETHEUS_PORT:-19091}"
  echo "  Alertmanager:    http://127.0.0.1:${ALERTMANAGER_PORT:-19093}"
  echo "  Tempo:           http://127.0.0.1:${TEMPO_PORT:-3200}"
  echo "  Loki:            http://127.0.0.1:${LOKI_PORT:-3100}"
  echo "  OTel Collector:  grpc://127.0.0.1:${OTEL_COLLECTOR_GRPC_PORT:-4317}"
  echo
  echo "Enable tracing before starting OpenIM services:"
  echo "  export OPENIM_TRACE_ENABLED=true"
  echo "  export OTEL_EXPORTER_OTLP_ENDPOINT=127.0.0.1:4317"
  echo "  # local/dev: export OPENIM_TRACE_SAMPLE_RATIO=1"
  echo "  # production: export OPENIM_ENV=production   # defaults sample ratio to 0.1"
  echo "  # or explicit: export OPENIM_TRACE_SAMPLE_RATIO=0.1"
  echo
  echo "Covered services: openim-api, openim-rpc-*, openim-msgtransfer, openim-msggateway, openim-push, openim-crontask, openimchat-*"
  echo "Ensure OpenIM/Chat services are running with prometheus.enable=true."
  echo "Prometheus discovers OpenIM targets via http://127.0.0.1:10002/prometheus_discovery/*"
  echo "Prometheus discovers Chat targets via static jobs (see config/prometheus.yml openimchat-*)"
  echo "Grafana dashboards: Demo / Middleware / Traces / API-SLI / Chat-Service-SLI / MessagePipeline / CronTask / domain/* / service/*"
  echo "Logs path for Promtail: ./logs and ./_output/logs (config/log.yml storageLocation=./logs/)"
  echo "Self-monitor jobs: prometheus / tempo / loki / otel-collector"
  echo
  echo "Production Grafana hardening (recommended):"
  echo "  export GF_AUTH_ANONYMOUS_ENABLED=false"
  echo "  export GF_SECURITY_ADMIN_PASSWORD='<strong-password>'"
  echo "Retention defaults: Prometheus 15d/10GB, Loki/Tempo 168h (7d)"
}

cmd="${1:-start}"
check_docker

case "$cmd" in
  start)
    start_stack
    print_info
    ;;
  restart)
    restart_stack
    print_info
    ;;
  -h | --help | help)
    usage
    ;;
  *)
    echo "Unknown command: $cmd"
    usage
    exit 1
    ;;
esac
