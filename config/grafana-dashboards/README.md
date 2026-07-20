# Grafana dashboard generator

Generates per-module domain/service dashboards from `modules.yaml`.

```bash
# from repo root
go test ./config/grafana-dashboards/ -v
go run ./config/grafana-dashboards/
```

Output:

- `config/grafana-template/domain/*.json`
- `config/grafana-template/service/*.json`

Edit `modules.yaml`, re-run the generator, and commit the generated JSON.
