// Package main generates per-module Grafana dashboards from modules.yaml.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Modules struct {
	Domains  []Domain  `yaml:"domains"`
	Services []Service `yaml:"services"`
}

type Domain struct {
	ID        string `yaml:"id"`
	Title     string `yaml:"title"`
	APIModule string `yaml:"apiModule"`
	RPCName   string `yaml:"rpcName"`
}

type Service struct {
	ID     string   `yaml:"id"`
	Title  string   `yaml:"title"`
	Job    string   `yaml:"job"`
	Kind   string   `yaml:"kind"`
	Extras []string `yaml:"extras"`
}

func loadModules(path string) (*Modules, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Modules
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func ds() map[string]any {
	return map[string]any{"type": "prometheus", "uid": "prometheus"}
}

func target(expr, legend, ref string) map[string]any {
	return map[string]any{"expr": expr, "legendFormat": legend, "refId": ref}
}

func row(id int, title string, y int) map[string]any {
	return map[string]any{
		"collapsed": false,
		"gridPos":   map[string]any{"h": 1, "w": 24, "x": 0, "y": y},
		"id":        id,
		"title":     title,
		"type":      "row",
	}
}

func timeseries(id int, title, unit string, x, y, w, h int, targets []map[string]any) map[string]any {
	p := map[string]any{
		"datasource": ds(),
		"gridPos":    map[string]any{"h": h, "w": w, "x": x, "y": y},
		"id":         id,
		"targets":    targets,
		"title":      title,
		"type":       "timeseries",
	}
	if unit != "" {
		defaults := map[string]any{"unit": unit}
		if unit == "percentunit" {
			defaults["min"] = 0
			defaults["max"] = 1
		}
		p["fieldConfig"] = map[string]any{"defaults": defaults}
	}
	return p
}

func stat(id int, title string, x, y, w, h int, expr, legend string, withUpMapping bool) map[string]any {
	p := map[string]any{
		"datasource": ds(),
		"gridPos":    map[string]any{"h": h, "w": w, "x": x, "y": y},
		"id":         id,
		"options": map[string]any{
			"colorMode": "background",
			"graphMode": "none",
			"reduceOptions": map[string]any{
				"calcs":  []string{"lastNotNull"},
				"fields": "",
				"values": false,
			},
		},
		"targets": []map[string]any{target(expr, legend, "A")},
		"title":   title,
		"type":    "stat",
	}
	if withUpMapping {
		p["fieldConfig"] = map[string]any{
			"defaults": map[string]any{
				"mappings": []map[string]any{
					{
						"type": "value",
						"options": map[string]any{
							"0": map[string]any{"color": "red", "text": "DOWN"},
							"1": map[string]any{"color": "green", "text": "UP"},
						},
					},
				},
				"thresholds": map[string]any{
					"mode": "absolute",
					"steps": []map[string]any{
						{"color": "red", "value": nil},
						{"color": "green", "value": 1},
					},
				},
			},
		}
	}
	return p
}

func gauge(id int, title string, x, y, w, h int, expr, legend string) map[string]any {
	return map[string]any{
		"datasource": ds(),
		"fieldConfig": map[string]any{
			"defaults": map[string]any{
				"unit": "percentunit",
				"min":  0,
				"max":  1,
			},
		},
		"gridPos": map[string]any{"h": h, "w": w, "x": x, "y": y},
		"id":      id,
		"options": map[string]any{
			"reduceOptions": map[string]any{
				"calcs":  []string{"lastNotNull"},
				"fields": "",
				"values": false,
			},
		},
		"targets": []map[string]any{target(expr, legend, "A")},
		"title":   title,
		"type":    "gauge",
	}
}

func baseDashboard(uid, title string, tags []string, panels []map[string]any) map[string]any {
	return map[string]any{
		"annotations":          map[string]any{"list": []any{}},
		"editable":             true,
		"fiscalYearStartMonth": 0,
		"graphTooltip":         1,
		"links":                []any{},
		"panels":               panels,
		"refresh":              "30s",
		"schemaVersion":        39,
		"tags":                 tags,
		"time":                 map[string]any{"from": "now-1h", "to": "now"},
		"timezone":             "browser",
		"title":                title,
		"uid":                  uid,
		"version":              1,
	}
}

func buildDomainDashboard(d Domain) map[string]any {
	panels := make([]map[string]any, 0, 16)
	id, y := 1, 0

	if d.APIModule != "" {
		m := d.APIModule
		panels = append(panels, row(id, "API", y))
		id++
		y++
		panels = append(panels,
			timeseries(id, "API QPS by path", "reqps", 0, y, 12, 8, []map[string]any{
				target(fmt.Sprintf(`sum by (path) (rate(api_count{module="%s"}[1m]))`, m), "{{path}}", "A"),
			}),
		)
		id++
		panels = append(panels,
			timeseries(id, "API Latency", "s", 12, y, 12, 8, []map[string]any{
				target(fmt.Sprintf(`histogram_quantile(0.50, sum by (le) (rate(api_request_duration_seconds_bucket{module="%s"}[5m])))`, m), "p50", "A"),
				target(fmt.Sprintf(`histogram_quantile(0.95, sum by (le) (rate(api_request_duration_seconds_bucket{module="%s"}[5m])))`, m), "p95", "B"),
				target(fmt.Sprintf(`histogram_quantile(0.99, sum by (le) (rate(api_request_duration_seconds_bucket{module="%s"}[5m])))`, m), "p99", "C"),
			}),
		)
		id++
		y += 8
		panels = append(panels,
			timeseries(id, "API Business Error Rate", "percentunit", 0, y, 8, 8, []map[string]any{
				target(fmt.Sprintf(`sum(rate(api_count{module="%s",code!="0"}[5m])) / clamp_min(sum(rate(api_count{module="%s"}[5m])), 0.001)`, m, m), "error rate", "A"),
			}),
		)
		id++
		panels = append(panels,
			timeseries(id, "API p95 Top Paths", "s", 8, y, 8, 8, []map[string]any{
				target(fmt.Sprintf(`topk(10, histogram_quantile(0.95, sum by (le, path) (rate(api_request_duration_seconds_bucket{module="%s"}[5m]))))`, m), "{{path}}", "A"),
			}),
		)
		id++
		panels = append(panels,
			timeseries(id, "API Errors Top Paths", "reqps", 16, y, 8, 8, []map[string]any{
				target(fmt.Sprintf(`topk(10, sum by (path, code) (rate(api_count{module="%s",code!="0"}[5m])))`, m), "{{path}} code={{code}}", "A"),
			}),
		)
		id++
		y += 8
	}

	if d.RPCName != "" {
		n := d.RPCName
		panels = append(panels, row(id, "RPC", y))
		id++
		y++
		panels = append(panels,
			timeseries(id, "RPC QPS by method", "reqps", 0, y, 12, 8, []map[string]any{
				target(fmt.Sprintf(`sum by (path) (rate(rpc_count{name="%s"}[1m]))`, n), "{{path}}", "A"),
			}),
		)
		id++
		panels = append(panels,
			timeseries(id, "RPC Latency", "s", 12, y, 12, 8, []map[string]any{
				target(fmt.Sprintf(`histogram_quantile(0.50, sum by (le) (rate(rpc_request_duration_seconds_bucket{name="%s"}[5m])))`, n), "p50", "A"),
				target(fmt.Sprintf(`histogram_quantile(0.95, sum by (le) (rate(rpc_request_duration_seconds_bucket{name="%s"}[5m])))`, n), "p95", "B"),
				target(fmt.Sprintf(`histogram_quantile(0.99, sum by (le) (rate(rpc_request_duration_seconds_bucket{name="%s"}[5m])))`, n), "p99", "C"),
			}),
		)
		id++
		y += 8
		panels = append(panels,
			timeseries(id, "RPC p95 Top Methods", "s", 0, y, 12, 8, []map[string]any{
				target(fmt.Sprintf(`topk(10, histogram_quantile(0.95, sum by (le, path) (rate(rpc_request_duration_seconds_bucket{name="%s"}[5m]))))`, n), "{{path}}", "A"),
			}),
		)
		id++
		panels = append(panels,
			timeseries(id, "RPC Errors by code", "reqps", 12, y, 12, 8, []map[string]any{
				target(fmt.Sprintf(`topk(10, sum by (path, code) (rate(rpc_count{name="%s",code!="0"}[5m])))`, n), "{{path}} code={{code}}", "A"),
			}),
		)
	}

	return baseDashboard(
		"openim-domain-"+d.ID,
		"OpenIM Domain / "+d.Title,
		[]string{"openim", "domain", d.ID},
		panels,
	)
}

func buildServiceDashboard(s Service) map[string]any {
	panels := make([]map[string]any, 0, 24)
	id, y := 1, 0
	job := s.Job

	panels = append(panels, row(id, "Process", y))
	id++
	y++
	panels = append(panels, stat(id, "Target Up", 0, y, 6, 4, fmt.Sprintf(`up{job="%s"}`, job), "up", true))
	id++
	panels = append(panels, timeseries(id, "Goroutines", "", 6, y, 6, 4, []map[string]any{
		target(fmt.Sprintf(`go_goroutines{job="%s"}`, job), "goroutines", "A"),
	}))
	id++
	panels = append(panels, timeseries(id, "Memory", "bytes", 12, y, 6, 4, []map[string]any{
		target(fmt.Sprintf(`process_resident_memory_bytes{job="%s"}`, job), "rss", "A"),
	}))
	id++
	panels = append(panels, timeseries(id, "CPU", "", 18, y, 6, 4, []map[string]any{
		target(fmt.Sprintf(`rate(process_cpu_seconds_total{job="%s"}[5m])`, job), "cpu cores", "A"),
	}))
	id++
	y += 4

	switch s.Kind {
	case "api":
		panels = append(panels, row(id, "API SLI", y))
		id++
		y++
		panels = append(panels,
			timeseries(id, "API QPS by module", "reqps", 0, y, 8, 8, []map[string]any{
				target(`sum(rate(api_count[1m]))`, "total", "A"),
				target(`sum by (module) (rate(api_count[1m]))`, "{{module}}", "B"),
			}),
		)
		id++
		panels = append(panels,
			timeseries(id, "API Latency", "s", 8, y, 8, 8, []map[string]any{
				target(`histogram_quantile(0.50, sum by (le) (rate(api_request_duration_seconds_bucket[5m])))`, "p50", "A"),
				target(`histogram_quantile(0.95, sum by (le) (rate(api_request_duration_seconds_bucket[5m])))`, "p95", "B"),
				target(`histogram_quantile(0.99, sum by (le) (rate(api_request_duration_seconds_bucket[5m])))`, "p99", "C"),
			}),
		)
		id++
		panels = append(panels,
			timeseries(id, "API Business Error Rate", "percentunit", 16, y, 8, 8, []map[string]any{
				target(`sum(rate(api_count{code!="0"}[5m])) / clamp_min(sum(rate(api_count[5m])), 0.001)`, "error rate", "A"),
			}),
		)
		id++
		y += 8

	case "gateway":
		panels = append(panels, row(id, "MsgGateway Capacity", y))
		id++
		y++
		panels = append(panels, stat(id, "Online Users", 0, y, 6, 7, `online_user_num`, "online users", false))
		id++
		panels = append(panels, timeseries(id, "WS Connections", "", 6, y, 6, 7, []map[string]any{
			target(`ws_connection_num`, "connections", "A"),
			target(`ws_max_connection_num`, "max", "B"),
		}))
		id++
		panels = append(panels, gauge(id, "WS Utilization", 12, y, 6, 7,
			`ws_connection_num / clamp_min(ws_max_connection_num, 1)`, "utilization"))
		id++
		panels = append(panels, timeseries(id, "WS Rejected by Reason", "ops", 18, y, 6, 7, []map[string]any{
			target(`sum by (reason) (rate(ws_conn_rejected_total[5m]))`, "{{reason}}", "A"),
		}))
		id++
		y += 7

	case "transfer":
		panels = append(panels, row(id, "Persist", y))
		id++
		y++
		panels = append(panels, timeseries(id, "msgtransfer Persist", "ops", 0, y, 24, 8, []map[string]any{
			target(`sum(rate(msg_insert_redis_success_total[1m]))`, "redis success", "A"),
			target(`sum(rate(msg_insert_redis_failed_total[1m]))`, "redis failed", "B"),
			target(`sum(rate(msg_insert_mongo_success_total[1m]))`, "mongo success", "C"),
			target(`sum(rate(msg_insert_mongo_failed_total[1m]))`, "mongo failed", "D"),
			target(`sum(rate(seq_set_failed_total[1m]))`, "seq failed", "E"),
		}))
		id++
		y += 8

	case "push":
		panels = append(panels, row(id, "Push", y))
		id++
		y++
		panels = append(panels, timeseries(id, "Push Failures / Slow Push", "ops", 0, y, 24, 8, []map[string]any{
			target(`sum(rate(msg_offline_push_failed_total[1m]))`, "offline push failed", "A"),
			target(`sum(rate(msg_long_time_push_total[1m]))`, "slow push >10s", "B"),
		}))
		id++
		y += 8

	case "rpc":
		panels = append(panels, row(id, "RPC SLI", y))
		id++
		y++
		panels = append(panels,
			timeseries(id, "RPC QPS by method", "reqps", 0, y, 12, 8, []map[string]any{
				target(fmt.Sprintf(`sum by (path) (rate(rpc_count{job="%s"}[1m]))`, job), "{{path}}", "A"),
			}),
		)
		id++
		panels = append(panels,
			timeseries(id, "RPC Latency", "s", 12, y, 12, 8, []map[string]any{
				target(fmt.Sprintf(`histogram_quantile(0.50, sum by (le) (rate(rpc_request_duration_seconds_bucket{job="%s"}[5m])))`, job), "p50", "A"),
				target(fmt.Sprintf(`histogram_quantile(0.95, sum by (le) (rate(rpc_request_duration_seconds_bucket{job="%s"}[5m])))`, job), "p95", "B"),
				target(fmt.Sprintf(`histogram_quantile(0.99, sum by (le) (rate(rpc_request_duration_seconds_bucket{job="%s"}[5m])))`, job), "p99", "C"),
			}),
		)
		id++
		y += 8
		panels = append(panels,
			timeseries(id, "RPC p95 Top Methods", "s", 0, y, 12, 8, []map[string]any{
				target(fmt.Sprintf(`topk(10, histogram_quantile(0.95, sum by (le, path) (rate(rpc_request_duration_seconds_bucket{job="%s"}[5m]))))`, job), "{{path}}", "A"),
			}),
		)
		id++
		panels = append(panels,
			timeseries(id, "RPC Errors by code", "reqps", 12, y, 12, 8, []map[string]any{
				target(fmt.Sprintf(`topk(10, sum by (path, code) (rate(rpc_count{job="%s",code!="0"}[5m])))`, job), "{{path}} code={{code}}", "A"),
			}),
		)
		id++
		y += 8

	case "cron":
		panels = append(panels, row(id, "Cron Tasks", y))
		id++
		y++
		panels = append(panels, timeseries(id, "Cron Runs", "ops", 0, y, 12, 8, []map[string]any{
			target(fmt.Sprintf(`sum by (task, result) (rate(cron_task_runs_total{job="%s"}[5m]))`, job), "{{task}} {{result}}", "A"),
		}))
		id++
		panels = append(panels, timeseries(id, "Cron Duration p95", "s", 12, y, 12, 8, []map[string]any{
			target(fmt.Sprintf(`histogram_quantile(0.95, sum by (le, task) (rate(cron_task_duration_seconds_bucket{job="%s"}[15m])))`, job), "{{task}} p95", "A"),
		}))
		id++
		y += 8
		panels = append(panels, timeseries(id, "Last Success Time", "dateTimeAsIso", 0, y, 24, 8, []map[string]any{
			target(fmt.Sprintf(`cron_task_last_success_unixtime{job="%s"} * 1000`, job), "{{task}}", "A"),
		}))
		id++
		y += 8
	}

	for _, extra := range s.Extras {
		switch extra {
		case "user_login":
			panels = append(panels, row(id, "Auth Extras", y))
			id++
			y++
			panels = append(panels, timeseries(id, "User Login", "ops", 0, y, 24, 8, []map[string]any{
				target(fmt.Sprintf(`sum(rate(user_login_total{job="%s"}[5m]))`, job), "login/s", "A"),
			}))
			id++
			y += 8
		case "user_register":
			panels = append(panels, row(id, "User Extras", y))
			id++
			y++
			panels = append(panels, timeseries(id, "User Register", "ops", 0, y, 24, 8, []map[string]any{
				target(fmt.Sprintf(`sum(rate(user_register_total{job="%s"}[5m]))`, job), "register/s", "A"),
			}))
			id++
			y += 8
		case "msg_process":
			panels = append(panels, row(id, "Msg Process", y))
			id++
			y++
			panels = append(panels, timeseries(id, "rpc-msg Process QPS", "ops", 0, y, 24, 8, []map[string]any{
				target(`sum(rate(single_chat_msg_process_success_total[1m]))`, "single success", "A"),
				target(`sum(rate(group_chat_msg_process_success_total[1m]))`, "group success", "B"),
				target(`sum(rate(single_chat_msg_process_failed_total[1m]))`, "single failed", "C"),
				target(`sum(rate(group_chat_msg_process_failed_total[1m]))`, "group failed", "D"),
			}))
			id++
			y += 8
		}
	}

	_ = y
	return baseDashboard(
		"openim-svc-"+s.ID,
		"OpenIM Service / "+s.Title,
		[]string{"openim", "service", s.ID},
		panels,
	)
}

func writeDashboard(path string, dash map[string]any) error {
	b, err := json.MarshalIndent(dash, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func resolvePaths() (yamlPath, outRoot string, err error) {
	candidates := []struct {
		yaml string
		out  string
	}{
		{"modules.yaml", filepath.Join("..", "grafana-template")},
		{filepath.Join("config", "grafana-dashboards", "modules.yaml"), filepath.Join("config", "grafana-template")},
	}
	for _, c := range candidates {
		if _, e := os.Stat(c.yaml); e == nil {
			return c.yaml, c.out, nil
		}
	}
	return "", "", fmt.Errorf("modules.yaml not found (run from repo root or config/grafana-dashboards)")
}

func fileNameFromTitle(prefix, title string) string {
	return prefix + strings.ReplaceAll(title, " ", "") + ".json"
}

func generateAll(yamlPath, outRoot string) (int, int, error) {
	m, err := loadModules(yamlPath)
	if err != nil {
		return 0, 0, err
	}
	domainDir := filepath.Join(outRoot, "domain")
	serviceDir := filepath.Join(outRoot, "service")
	if err := os.MkdirAll(domainDir, 0o755); err != nil {
		return 0, 0, err
	}
	if err := os.MkdirAll(serviceDir, 0o755); err != nil {
		return 0, 0, err
	}
	for _, d := range m.Domains {
		path := filepath.Join(domainDir, fileNameFromTitle("Domain-", d.Title))
		if err := writeDashboard(path, buildDomainDashboard(d)); err != nil {
			return 0, 0, err
		}
	}
	for _, s := range m.Services {
		path := filepath.Join(serviceDir, fileNameFromTitle("Service-", s.Title))
		if err := writeDashboard(path, buildServiceDashboard(s)); err != nil {
			return 0, 0, err
		}
	}
	return len(m.Domains), len(m.Services), nil
}

func main() {
	yamlPath, outRoot, err := resolvePaths()
	if err != nil {
		panic(err)
	}
	nd, ns, err := generateAll(yamlPath, outRoot)
	if err != nil {
		panic(err)
	}
	fmt.Printf("generated %d domain + %d service dashboards\n", nd, ns)
}
