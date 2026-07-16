#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is not running. Start Docker Desktop first."
  exit 1
fi

echo "Starting Prometheus, Grafana, Alertmanager, Node Exporter, Redis Exporter, and MongoDB Exporter..."
docker compose --profile m up -d prometheus grafana alertmanager node-exporter redis-exporter mongodb-exporter

echo
echo "Monitoring stack started:"
echo "  Grafana:      http://127.0.0.1:${GRAFANA_PORT:-13000}"
echo "  Prometheus:   http://127.0.0.1:${PROMETHEUS_PORT:-19091}"
echo "  Alertmanager: http://127.0.0.1:${ALERTMANAGER_PORT:-19093}"
echo
echo "Ensure OpenIM services are running with prometheus.enable=true (see config/openim-api.yml)."
echo "Prometheus discovers targets via http://127.0.0.1:10002/prometheus_discovery/*"
