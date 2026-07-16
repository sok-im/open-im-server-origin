#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is not running. Start Docker Desktop first."
  exit 1
fi

echo "Starting monitoring and tracing stack..."
docker compose --profile m up -d \
  prometheus grafana alertmanager node-exporter redis-exporter mongodb-exporter kafka-exporter \
  tempo otel-collector loki promtail

echo
echo "Monitoring stack started:"
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
echo "Covered services: openim-api, openim-rpc-*, openim-msgtransfer, openim-msggateway, openim-push, openim-crontask"
echo "Ensure OpenIM services are running with prometheus.enable=true (see config/openim-api.yml)."
echo "Prometheus discovers targets via http://127.0.0.1:10002/prometheus_discovery/*"
echo "Grafana dashboards: Demo / Middleware / Traces / API-SLI / MessagePipeline / CronTask"
echo "Logs path for Promtail: ./logs and ./_output/logs (config/log.yml storageLocation=./logs/)"
echo "Self-monitor jobs: prometheus / tempo / loki / otel-collector"
echo
echo "Production Grafana hardening (recommended):"
echo "  export GF_AUTH_ANONYMOUS_ENABLED=false"
echo "  export GF_SECURITY_ADMIN_PASSWORD='<strong-password>'"
echo "Retention defaults: Prometheus 15d/10GB, Loki/Tempo 168h (7d)"
