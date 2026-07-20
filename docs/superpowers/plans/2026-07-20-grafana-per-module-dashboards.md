# Grafana Per-Module Dashboards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Generate 35 independent Grafana dashboards (16 domain + 19 service) from a YAML inventory via a Go generator, while keeping existing overview dashboards unchanged.

**Architecture:** A single `modules.yaml` lists domains and services. `generate.go` builds Grafana JSON (schemaVersion 39, prometheus datasource uid `prometheus`) into `config/grafana-template/domain/` and `service/`. Generated JSON is committed; Grafana file provisioning recursively loads the template directory.

**Tech Stack:** Go 1.21+, `gopkg.in/yaml.v3`, Grafana 11 JSON dashboards, Prometheus PromQL

**Spec:** `docs/superpowers/specs/2026-07-20-grafana-per-module-dashboards-design.md`

## Global Constraints

- Do not modify business/metrics code under `pkg/common/prommetrics/` or RPC services
- Do not delete or rewrite existing overview dashboards: `API-SLI.json`, `MessagePipeline.json`, `Middleware.json`, `CronTask.json`, `Traces.json`, `Demo.json`
- Datasource must be `{ "type": "prometheus", "uid": "prometheus" }`
- uid: `openim-domain-<id>` / `openim-svc-<id>`; title: `OpenIM Domain / <title>` / `OpenIM Service / <title>`
- tags: `["openim", "domain"|"service", "<id>"]`
- refresh `30s`, time `now-1h`→`now`, schemaVersion `39`
- Stat panels MUST include `options.reduceOptions` (Grafana 11)
- API `module` / RPC `name` values must match live labels (e.g. `crypto/v1`, `openMLS`, `redPacket`, `virgilSecurity`)
- Multi-instance jobs (`msgtransfer`, `push`) use `sum(...)` aggregation where rates are shown

---

## File Structure

| Path | Responsibility |
|---|---|
| `config/grafana-dashboards/modules.yaml` | Inventory of domains + services |
| `config/grafana-dashboards/generate.go` | Generator: load YAML → emit Grafana JSON |
| `config/grafana-dashboards/generate_test.go` | Unit tests for panel/filter/count behavior |
| `config/grafana-dashboards/README.md` | How to regenerate |
| `config/grafana-template/domain/*.json` | Generated domain dashboards (committed) |
| `config/grafana-template/service/*.json` | Generated service dashboards (committed) |

No Makefile in this repo — document `go run` in README.

---

### Task 1: modules.yaml inventory

**Files:**
- Create: `config/grafana-dashboards/modules.yaml`

**Interfaces:**
- Produces: YAML schema consumed by generator:
  - `domains[]`: `{id, title, apiModule?, rpcName?}` (empty string = absent)
  - `services[]`: `{id, title, job, kind, extras[]}` where `kind` ∈ `api|gateway|transfer|push|rpc|cron` and `extras` ∈ `user_login|user_register|msg_process`

- [ ] **Step 1: Create modules.yaml with full inventory**

```yaml
# config/grafana-dashboards/modules.yaml
domains:
  - id: auth
    title: Auth
    apiModule: auth
    rpcName: auth
  - id: user
    title: User
    apiModule: user
    rpcName: user
  - id: friend
    title: Friend
    apiModule: friend
    rpcName: friend
  - id: group
    title: Group
    apiModule: group
    rpcName: group
  - id: conversation
    title: Conversation
    apiModule: conversation
    rpcName: conversation
  - id: msg
    title: Message
    apiModule: msg
    rpcName: msg
  - id: rtc
    title: RTC
    apiModule: rtc
    rpcName: rtc
  - id: third
    title: Third
    apiModule: third
    rpcName: third
  - id: object
    title: Object
    apiModule: object
    rpcName: ""
  - id: crypto
    title: Crypto
    apiModule: crypto/v1
    rpcName: crypto
  - id: openmls
    title: OpenMLS
    apiModule: openmls/v1
    rpcName: openMLS
  - id: redpacket
    title: RedPacket
    apiModule: redpacket
    rpcName: redPacket
  - id: captcha
    title: Captcha
    apiModule: ""
    rpcName: captcha
  - id: totp
    title: TOTP
    apiModule: ""
    rpcName: totp
  - id: virgil
    title: Virgil Security
    apiModule: ""
    rpcName: virgilSecurity
  - id: statistics
    title: Statistics
    apiModule: statistics
    rpcName: ""

services:
  - id: api
    title: API
    job: openimserver-openim-api
    kind: api
    extras: []
  - id: msggateway
    title: MsgGateway
    job: openimserver-openim-msggateway
    kind: gateway
    extras: []
  - id: msgtransfer
    title: MsgTransfer
    job: openimserver-openim-msgtransfer
    kind: transfer
    extras: []
  - id: push
    title: Push
    job: openimserver-openim-push
    kind: push
    extras: []
  - id: crontask
    title: CronTask
    job: openimserver-openim-crontask
    kind: cron
    extras: []
  - id: rpc-auth
    title: rpc-auth
    job: openimserver-openim-rpc-auth
    kind: rpc
    extras: [user_login]
  - id: rpc-user
    title: rpc-user
    job: openimserver-openim-rpc-user
    kind: rpc
    extras: [user_register]
  - id: rpc-friend
    title: rpc-friend
    job: openimserver-openim-rpc-friend
    kind: rpc
    extras: []
  - id: rpc-group
    title: rpc-group
    job: openimserver-openim-rpc-group
    kind: rpc
    extras: []
  - id: rpc-conversation
    title: rpc-conversation
    job: openimserver-openim-rpc-conversation
    kind: rpc
    extras: []
  - id: rpc-msg
    title: rpc-msg
    job: openimserver-openim-rpc-msg
    kind: rpc
    extras: [msg_process]
  - id: rpc-third
    title: rpc-third
    job: openimserver-openim-rpc-third
    kind: rpc
    extras: []
  - id: rpc-rtc
    title: rpc-rtc
    job: openimserver-openim-rpc-rtc
    kind: rpc
    extras: []
  - id: rpc-crypto
    title: rpc-crypto
    job: openimserver-openim-rpc-crypto
    kind: rpc
    extras: []
  - id: rpc-openmls
    title: rpc-openmls
    job: openimserver-openim-rpc-openmls
    kind: rpc
    extras: []
  - id: rpc-virgilsecurity
    title: rpc-virgilsecurity
    job: openimserver-openim-rpc-virgilsecurity
    kind: rpc
    extras: []
  - id: rpc-captcha
    title: rpc-captcha
    job: openimserver-openim-rpc-captcha
    kind: rpc
    extras: []
  - id: rpc-totp
    title: rpc-totp
    job: openimserver-openim-rpc-totp
    kind: rpc
    extras: []
  - id: rpc-redpacket
    title: rpc-redpacket
    job: openimserver-openim-rpc-redpacket
    kind: rpc
    extras: []
```

- [ ] **Step 2: Commit**

```bash
git add config/grafana-dashboards/modules.yaml
git commit -m "$(cat <<'EOF'
chore(grafana): add modules inventory for per-module dashboards

EOF
)"
```

---

### Task 2: Generator core + domain dashboards

**Files:**
- Create: `config/grafana-dashboards/generate.go`
- Create: `config/grafana-dashboards/generate_test.go`
- Create: `config/grafana-dashboards/README.md`

**Interfaces:**
- Consumes: `modules.yaml` Domain/Service structs
- Produces:
  - `func loadModules(path string) (*Modules, error)`
  - `func buildDomainDashboard(d Domain) map[string]any`
  - `func writeDashboard(path string, dash map[string]any) error`
  - Domain panels only when `apiModule` / `rpcName` non-empty

- [ ] **Step 1: Write failing tests for domain dashboard shape**

```go
// config/grafana-dashboards/generate_test.go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildDomainDashboard_AuthHasAPIAndRPC(t *testing.T) {
	d := Domain{ID: "auth", Title: "Auth", APIModule: "auth", RPCName: "auth"}
	dash := buildDomainDashboard(d)
	if dash["uid"] != "openim-domain-auth" {
		t.Fatalf("uid=%v", dash["uid"])
	}
	if dash["title"] != "OpenIM Domain / Auth" {
		t.Fatalf("title=%v", dash["title"])
	}
	panels, _ := dash["panels"].([]map[string]any)
	if panels == nil {
		// also accept []any after JSON round-trip helpers
		raw, _ := json.Marshal(dash["panels"])
		var arr []map[string]any
		_ = json.Unmarshal(raw, &arr)
		panels = arr
	}
	exprs := collectExprs(panels)
	mustContain(t, exprs, `api_count{module="auth"`)
	mustContain(t, exprs, `rpc_count{name="auth"`)
}

func TestBuildDomainDashboard_ObjectAPIOnly(t *testing.T) {
	d := Domain{ID: "object", Title: "Object", APIModule: "object", RPCName: ""}
	dash := buildDomainDashboard(d)
	exprs := collectExprs(asPanelMaps(dash["panels"]))
	mustContain(t, exprs, `module="object"`)
	mustNotContain(t, exprs, `rpc_count`)
}

func TestBuildDomainDashboard_CaptchaRPCOnly(t *testing.T) {
	d := Domain{ID: "captcha", Title: "Captcha", APIModule: "", RPCName: "captcha"}
	dash := buildDomainDashboard(d)
	exprs := collectExprs(asPanelMaps(dash["panels"]))
	mustContain(t, exprs, `rpc_count{name="captcha"`)
	mustNotContain(t, exprs, `api_count`)
}

func TestLoadModulesCounts(t *testing.T) {
	path := filepath.Join("modules.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Skip("modules.yaml not next to test")
	}
	m, err := loadModules(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Domains) != 16 {
		t.Fatalf("domains=%d want 16", len(m.Domains))
	}
	if len(m.Services) != 19 {
		t.Fatalf("services=%d want 19", len(m.Services))
	}
}

// helpers: asPanelMaps, collectExprs, mustContain, mustNotContain
```

Implement the helpers in the same test file (walk `targets[].expr` recursively).

- [ ] **Step 2: Run tests — expect FAIL (undefined symbols)**

```bash
cd config/grafana-dashboards && go test -v
```

Expected: FAIL compiling (`buildDomainDashboard` undefined) or test fail.

- [ ] **Step 3: Implement generate.go — types, load, domain builder, main write**

Implement in `config/grafana-dashboards/generate.go` (package `main`):

```go
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

func loadModules(path string) (*Modules, error) { /* yaml.Unmarshal */ }

func ds() map[string]any {
	return map[string]any{"type": "prometheus", "uid": "prometheus"}
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
		p["fieldConfig"] = map[string]any{"defaults": map[string]any{"unit": unit}}
	}
	return p
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

func buildDomainDashboard(d Domain) map[string]any {
	panels := []map[string]any{}
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
	return map[string]any{
		"annotations":         map[string]any{"list": []any{}},
		"editable":            true,
		"fiscalYearStartMonth": 0,
		"graphTooltip":        1,
		"links":               []any{},
		"panels":              panels,
		"refresh":             "30s",
		"schemaVersion":       39,
		"tags":                []string{"openim", "domain", d.ID},
		"time":                map[string]any{"from": "now-1h", "to": "now"},
		"timezone":            "browser",
		"title":               "OpenIM Domain / " + d.Title,
		"uid":                 "openim-domain-" + d.ID,
		"version":             1,
	}
}

func writeDashboard(path string, dash map[string]any) error {
	b, err := json.MarshalIndent(dash, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	// Allow running from repo root or from this directory
	yamlPath := "modules.yaml"
	outRoot := filepath.Join("..", "grafana-template")
	if _, err := os.Stat(yamlPath); err != nil {
		yamlPath = filepath.Join("config", "grafana-dashboards", "modules.yaml")
		outRoot = filepath.Join("config", "grafana-template")
		_ = root
	}
	m, err := loadModules(yamlPath)
	if err != nil {
		panic(err)
	}
	domainDir := filepath.Join(outRoot, "domain")
	serviceDir := filepath.Join(outRoot, "service")
	_ = os.MkdirAll(domainDir, 0o755)
	_ = os.MkdirAll(serviceDir, 0o755)
	for _, d := range m.Domains {
		name := "Domain-" + strings.ReplaceAll(d.Title, " ", "") + ".json"
		if err := writeDashboard(filepath.Join(domainDir, name), buildDomainDashboard(d)); err != nil {
			panic(err)
		}
	}
	for _, s := range m.Services {
		name := "Service-" + strings.ReplaceAll(s.Title, " ", "") + ".json"
		if err := writeDashboard(filepath.Join(serviceDir, name), buildServiceDashboard(s)); err != nil {
			panic(err)
		}
	}
	fmt.Printf("generated %d domain + %d service dashboards\n", len(m.Domains), len(m.Services))
}
```

Note: `buildServiceDashboard` can be a stub returning empty panels for this task — Task 3 fills it. For Task 2 tests to compile, provide:

```go
func buildServiceDashboard(s Service) map[string]any {
	return map[string]any{
		"uid":   "openim-svc-" + s.ID,
		"title": "OpenIM Service / " + s.Title,
		"panels": []map[string]any{},
		"tags": []string{"openim", "service", s.ID},
		"schemaVersion": 39,
		"refresh": "30s",
		"time": map[string]any{"from": "now-1h", "to": "now"},
		"timezone": "browser",
		"version": 1,
		"editable": true,
		"annotations": map[string]any{"list": []any{}},
		"links": []any{},
		"fiscalYearStartMonth": 0,
		"graphTooltip": 1,
	}
}
```

`go.mod`: generator lives under `config/grafana-dashboards` as a standalone `main`. Prefer:

```bash
# from repo root — use module path via go run with files in subdirectory
cd config/grafana-dashboards
go mod init github.com/openimsdk/open-im-server/v3/config/grafana-dashboards
go get gopkg.in/yaml.v3
```

**Prefer not** a nested module if avoidable — instead put generator as:

```
go run ./config/grafana-dashboards/
```

from repo root with `package main` files and import `gopkg.in/yaml.v3` from root `go.mod` (already present or add via `go get`). Check root `go.mod` for yaml; if missing:

```bash
go get gopkg.in/yaml.v3
```

- [ ] **Step 4: Run domain tests — expect PASS**

```bash
cd /path/to/repo
go test ./config/grafana-dashboards/ -v
```

Expected: PASS for domain tests (service tests may come in Task 3).

- [ ] **Step 5: Add README**

```markdown
# Grafana dashboard generator

```bash
# from repo root
go run ./config/grafana-dashboards/
go test ./config/grafana-dashboards/ -v
```

Edits: change `modules.yaml` then re-run. Commit generated JSON under `config/grafana-template/domain|service/`.
```

- [ ] **Step 6: Commit**

```bash
git add config/grafana-dashboards/ go.mod go.sum
git commit -m "$(cat <<'EOF'
feat(grafana): add dashboard generator for domain panels

EOF
)"
```

---

### Task 3: Service dashboard builder + extras

**Files:**
- Modify: `config/grafana-dashboards/generate.go` (`buildServiceDashboard`)
- Modify: `config/grafana-dashboards/generate_test.go`

**Interfaces:**
- Consumes: `Service{ID,Title,Job,Kind,Extras}`
- Produces: full service dashboard JSON with common + kind-specific + extras panels
- Stat helper MUST set `options.reduceOptions.calcs = ["lastNotNull"]`

- [ ] **Step 1: Write failing service tests**

```go
func TestBuildServiceDashboard_Gateway(t *testing.T) {
	s := Service{ID: "msggateway", Title: "MsgGateway", Job: "openimserver-openim-msggateway", Kind: "gateway"}
	dash := buildServiceDashboard(s)
	if dash["uid"] != "openim-svc-msggateway" {
		t.Fatalf("uid=%v", dash["uid"])
	}
	exprs := collectExprs(asPanelMaps(dash["panels"]))
	mustContain(t, exprs, `up{job="openimserver-openim-msggateway"}`)
	mustContain(t, exprs, `online_user_num`)
	mustContain(t, exprs, `ws_connection_num`)
	mustContain(t, exprs, `ws_conn_rejected_total`)
}

func TestBuildServiceDashboard_MsgExtras(t *testing.T) {
	s := Service{ID: "rpc-msg", Title: "rpc-msg", Job: "openimserver-openim-rpc-msg", Kind: "rpc", Extras: []string{"msg_process"}}
	exprs := collectExprs(asPanelMaps(buildServiceDashboard(s)["panels"]))
	mustContain(t, exprs, `single_chat_msg_process_success_total`)
	mustContain(t, exprs, `rpc_count{job="openimserver-openim-rpc-msg"`)
}

func TestBuildServiceDashboard_TransferUsesSum(t *testing.T) {
	s := Service{ID: "msgtransfer", Title: "MsgTransfer", Job: "openimserver-openim-msgtransfer", Kind: "transfer"}
	exprs := collectExprs(asPanelMaps(buildServiceDashboard(s)["panels"]))
	mustContain(t, exprs, `sum(rate(msg_insert_redis_success_total`)
}
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
go test ./config/grafana-dashboards/ -run TestBuildServiceDashboard -v
```

- [ ] **Step 3: Implement buildServiceDashboard**

Required panels:

**Common (all kinds):**
1. Stat `Target Up`: `up{job="<job>"}` with mappings 0→DOWN / 1→UP and `options.reduceOptions`
2. Timeseries `Goroutines`: `go_goroutines{job="<job>"}`
3. Timeseries `Memory`: `process_resident_memory_bytes{job="<job>"}` unit `bytes`
4. Timeseries `CPU`: `rate(process_cpu_seconds_total{job="<job>"}[5m])`

**kind=api:** global API QPS / Latency / Error Rate (no module filter) — same exprs as overview API-SLI without module label.

**kind=gateway:**
- `online_user_num` (stat)
- `ws_connection_num` + `ws_max_connection_num` (stat)
- utilization: `ws_connection_num / clamp_min(ws_max_connection_num, 1)` (gauge or timeseries percentunit)
- `sum by (reason) (rate(ws_conn_rejected_total[5m]))`

**kind=transfer:**
```
sum(rate(msg_insert_redis_success_total[1m]))
sum(rate(msg_insert_redis_failed_total[1m]))
sum(rate(msg_insert_mongo_success_total[1m]))
sum(rate(msg_insert_mongo_failed_total[1m]))
sum(rate(seq_set_failed_total[1m]))
```
(Optionally also filter `{job="<job>"}` if series have job label — prefer with job filter when present; if counter is process-local without cross-job collision, job filter is still correct.)

**kind=push:**
```
sum(rate(msg_offline_push_failed_total[1m]))
sum(rate(msg_long_time_push_total[1m]))
```

**kind=rpc:**
```
sum by (path) (rate(rpc_count{job="<job>"}[1m]))
histogram_quantile for rpc_request_duration_seconds_bucket{job="<job>"}
topk rpc methods / errors with job filter
```

**kind=cron:**
```
sum by (task, result) (rate(cron_task_runs_total{job="<job>"}[5m]))
histogram_quantile p95 cron_task_duration_seconds_bucket{job="<job>"}
cron_task_last_success_unixtime{job="<job>"} * 1000
```

**extras:**
- `user_login` → `sum(rate(user_login_total{job="<job>"}[5m]))`
- `user_register` → `sum(rate(user_register_total{job="<job>"}[5m]))`
- `msg_process` → single/group success/fail rates (same as MessagePipeline rpc-msg panel)

Helper for stat:

```go
func stat(id int, title string, x, y, w, h int, expr, legend string) map[string]any {
	return map[string]any{
		"datasource": ds(),
		"gridPos":    map[string]any{"h": h, "w": w, "x": x, "y": y},
		"id":         id,
		"options": map[string]any{
			"colorMode": "background",
			"graphMode": "none",
			"reduceOptions": map[string]any{"calcs": []string{"lastNotNull"}, "fields": "", "values": false},
		},
		"targets": []map[string]any{target(expr, legend, "A")},
		"title":   title,
		"type":    "stat",
	}
}
```

- [ ] **Step 4: Run all generator tests — expect PASS**

```bash
go test ./config/grafana-dashboards/ -v
```

- [ ] **Step 5: Commit**

```bash
git add config/grafana-dashboards/
git commit -m "$(cat <<'EOF'
feat(grafana): implement service dashboard panel templates

EOF
)"
```

---

### Task 4: Generate and commit JSON artifacts

**Files:**
- Create: `config/grafana-template/domain/*.json` (16 files)
- Create: `config/grafana-template/service/*.json` (19 files)

- [ ] **Step 1: Run generator from repo root**

```bash
go run ./config/grafana-dashboards/
```

Expected stdout: `generated 16 domain + 19 service dashboards`

- [ ] **Step 2: Verify file counts**

```bash
ls config/grafana-template/domain | wc -l   # 16
ls config/grafana-template/service | wc -l  # 19
# overview files still present at template root
ls config/grafana-template/*.json
```

Expected overview still: `API-SLI.json`, `MessagePipeline.json`, `Middleware.json`, `CronTask.json`, `Traces.json`, `Demo.json` (and any others previously there).

- [ ] **Step 3: Spot-check JSON validity**

```bash
python3 -c '
import json,glob,sys
ok=True
for p in glob.glob("config/grafana-template/domain/*.json")+glob.glob("config/grafana-template/service/*.json"):
  d=json.load(open(p))
  assert d.get("uid") and d.get("schemaVersion")==39, p
  assert "prometheus" in str(d.get("panels"))
print("ok", 16+19)
'
```

- [ ] **Step 4: Commit generated dashboards**

```bash
git add config/grafana-template/domain config/grafana-template/service
git commit -m "$(cat <<'EOF'
chore(grafana): generate per-module domain and service dashboards

EOF
)"
```

---

### Task 5: Live verification against Prometheus/Grafana

**Files:** none (verification only)

- [ ] **Step 1: Query Prometheus via Grafana proxy (or :19091)**

```bash
BASE='http://13.215.203.29:13000/api/datasources/proxy/uid/prometheus'
# or http://13.215.203.29:19091

for q in \
  'sum(rate(api_count{module="auth"}[1m]))' \
  'sum(rate(rpc_count{name="auth"}[1m]))' \
  'online_user_num' \
  'sum(rate(rpc_count{job="openimserver-openim-rpc-msg"}[1m]))' \
  'sum(rate(single_chat_msg_process_success_total[1m]))'
do
  echo "=== $q ==="
  curl -sS -G --data-urlencode "query=$q" "$BASE/api/v1/query" | python3 -c 'import sys,json; r=json.load(sys.stdin)["data"]["result"]; print(len(r), r[:1])'
done
```

Expected: each query returns ≥1 series (or 0 only if that service truly idle — note in verification notes).

- [ ] **Step 2: Confirm Grafana loads new UIDs**

```bash
curl -sS 'http://13.215.203.29:13000/api/search?type=dash-db&query=OpenIM%20Domain' | python3 -m json.tool | head
curl -sS 'http://13.215.203.29:13000/api/dashboards/uid/openim-domain-auth' | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d["dashboard"]["title"], len(d["dashboard"]["panels"]))'
curl -sS 'http://13.215.203.29:13000/api/dashboards/uid/openim-svc-msggateway' | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d["dashboard"]["title"], len(d["dashboard"]["panels"]))'
```

If provisioning has not picked up yet (volume mount on host): restart grafana or wait `updateIntervalSeconds: 30`. If dashboards missing, check that files exist under `./config/grafana-template/domain` on the server host.

- [ ] **Step 3: Open three dashboards in browser and confirm not stuck on No data**

URLs:
- `http://13.215.203.29:13000/d/openim-domain-auth`
- `http://13.215.203.29:13000/d/openim-svc-msggateway`
- `http://13.215.203.29:13000/d/openim-svc-rpc-msg`

Wait 2–3s after load; legends should appear.

- [ ] **Step 4: Verification notes only — no code commit required unless fixes needed**

If a job filter returns empty for process-local counters (e.g. `msg_insert_*` without matching job label on series), fix generator to drop job filter for those counters and regenerate (amend Task 3/4).

---

## Spec Coverage Checklist

| Spec requirement | Task |
|---|---|
| Keep overview dashboards | Task 4 verify + Global Constraints |
| 16 domain + 19 service | Task 1 + Task 4 |
| Domain API/RPC PromQL templates | Task 2 |
| Service common + kind + extras | Task 3 |
| Generator + modules.yaml | Task 1–2 |
| Generated JSON under domain/service | Task 4 |
| Stat options.reduceOptions | Task 3 |
| Live Prometheus/Grafana verify | Task 5 |
| No metrics code changes | Global Constraints |

## Self-Review Notes

- Spec table said “API 区块（4 面板）” but listed 5 PromQLs — plan implements **5 API + 4 RPC** panels (matches PromQL table).
- Nested go.mod avoided: use root module `go run ./config/grafana-dashboards/`.
- No Makefile target (repo has no Makefile); README documents `go run`.
