# Grafana 分模块看板设计

**日期**: 2026-07-20  
**状态**: 待实现  
**方案**: 生成器产出独立 JSON（方案 3）

## 背景与目标

当前 `OpenIM API / RPC SLI` 看板聚合了全部 API/RPC SLI，适合总览，但不利于按模块/服务下钻排障。

**目标**：

- 保留现有总览看板（API-SLI、MessagePipeline、Middleware、CronTask、Traces）
- 新增两类独立看板：
  - **业务域**：按 API `module` / RPC `name` 过滤
  - **服务进程**：按 Prometheus `job` 过滤，并叠加进程专属指标
- 全量一次覆盖所有模块（用户确认）
- 通过生成器维护，避免手写 30+ 份重复 JSON

## 非目标

- 不修改业务代码或 Prometheus 指标名
- 不删除/替换现有总览看板
- 不使用 Grafana 变量做「单看板多模块」伪拆分

## 看板清单

### 保留总览（不动）

| 文件 | uid | 说明 |
|---|---|---|
| `API-SLI.json` | `openim-sli` | API/RPC 全局 SLI |
| `MessagePipeline.json` | `openim-pipeline` | 消息链路 |
| `Middleware.json` | `openim-middleware` | 中间件 |
| `CronTask.json` | `openim-crontask` | 定时任务 |
| `Traces.json` | `openim-traces` | 链路追踪 |

### 业务域看板（16 张）

Grafana folder：`OpenIM`（文件名前缀 `Domain-` 便于浏览）

| id | title | apiModule | rpcName | 备注 |
|---|---|---|---|---|
| auth | Auth | auth | auth | |
| user | User | user | user | |
| friend | Friend | friend | friend | |
| group | Group | group | group | |
| conversation | Conversation | conversation | conversation | |
| msg | Message | msg | msg | |
| rtc | RTC | rtc | rtc | |
| third | Third | third | third | |
| object | Object | object | — | 仅 API（对象存储） |
| crypto | Crypto | crypto/v1 | crypto | API module 含版本路径 |
| openmls | OpenMLS | openmls/v1 | openMLS | rpcName 与 share.yml 一致 |
| redpacket | RedPacket | redpacket | redPacket | |
| captcha | Captcha | — | captcha | 仅 RPC |
| totp | TOTP | — | totp | 仅 RPC |
| virgil | Virgil Security | — | virgilSecurity | 仅 RPC |
| statistics | Statistics | statistics | — | 仅 API |

uid 规则：`openim-domain-<id>`  
title 规则：`OpenIM Domain / <title>`

### 服务进程看板（19 张）

| id | title | prometheus job | kind | 专属指标 |
|---|---|---|---|---|
| api | API | openimserver-openim-api | api | 全量 API SLI |
| msggateway | MsgGateway | openimserver-openim-msggateway | gateway | online_user_num, ws_* |
| msgtransfer | MsgTransfer | openimserver-openim-msgtransfer | transfer | msg_insert_*, seq_set_failed |
| push | Push | openimserver-openim-push | push | msg_offline_push_failed, msg_long_time_push |
| crontask | CronTask | openimserver-openim-crontask | cron | cron_task_* |
| rpc-auth | rpc-auth | openimserver-openim-rpc-auth | rpc | user_login_total |
| rpc-user | rpc-user | openimserver-openim-rpc-user | rpc | user_register_total |
| rpc-friend | rpc-friend | openimserver-openim-rpc-friend | rpc | — |
| rpc-group | rpc-group | openimserver-openim-rpc-group | rpc | — |
| rpc-conversation | rpc-conversation | openimserver-openim-rpc-conversation | rpc | — |
| rpc-msg | rpc-msg | openimserver-openim-rpc-msg | rpc | single/group chat process |
| rpc-third | rpc-third | openimserver-openim-rpc-third | rpc | — |
| rpc-rtc | rpc-rtc | openimserver-openim-rpc-rtc | rpc | — |
| rpc-crypto | rpc-crypto | openimserver-openim-rpc-crypto | rpc | — |
| rpc-openmls | rpc-openmls | openimserver-openim-rpc-openmls | rpc | — |
| rpc-virgilsecurity | rpc-virgilsecurity | openimserver-openim-rpc-virgilsecurity | rpc | — |
| rpc-captcha | rpc-captcha | openimserver-openim-rpc-captcha | rpc | — |
| rpc-totp | rpc-totp | openimserver-openim-rpc-totp | rpc | — |
| rpc-redpacket | rpc-redpacket | openimserver-openim-rpc-redpacket | rpc | — |

uid 规则：`openim-svc-<id>`  
title 规则：`OpenIM Service / <title>`

## 面板模板

### 业务域看板（统一 8 面板）

当 `apiModule` 非空时生成 API 区块；当 `rpcName` 非空时生成 RPC 区块。

**API 区块**（4 面板）：

| 面板 | PromQL |
|---|---|
| API QPS by path | `sum by (path) (rate(api_count{module="<apiModule>"}[1m]))` |
| API Latency p50/p95/p99 | `histogram_quantile(0.XX, sum by (le) (rate(api_request_duration_seconds_bucket{module="<apiModule>"}[5m])))` |
| API Business Error Rate | `sum(rate(api_count{module="<apiModule>",code!="0"}[5m])) / clamp_min(sum(rate(api_count{module="<apiModule>"}[5m])), 0.001)` |
| API p95 Top Paths | `topk(10, histogram_quantile(0.95, sum by (le, path) (rate(api_request_duration_seconds_bucket{module="<apiModule>"}[5m]))))` |
| API Errors Top Paths | `topk(10, sum by (path, code) (rate(api_count{module="<apiModule>",code!="0"}[5m])))` |

**RPC 区块**（4 面板）：

| 面板 | PromQL |
|---|---|
| RPC QPS by method | `sum by (path) (rate(rpc_count{name="<rpcName>"}[1m]))` |
| RPC Latency p50/p95/p99 | `histogram_quantile(0.XX, sum by (le) (rate(rpc_request_duration_seconds_bucket{name="<rpcName>"}[5m])))` |
| RPC p95 Top Methods | `topk(10, histogram_quantile(0.95, sum by (le, path) (rate(rpc_request_duration_seconds_bucket{name="<rpcName>"}[5m]))))` |
| RPC Errors by code | `topk(10, sum by (path, code) (rate(rpc_count{name="<rpcName>",code!="0"}[5m])))` |

### 服务进程看板

**通用层**（所有服务，4 面板）：

| 面板 | PromQL |
|---|---|
| Target Up | `up{job="<job>"}` |
| Goroutines | `go_goroutines{job="<job>"}` |
| Memory | `process_resident_memory_bytes{job="<job>"}` |
| CPU | `rate(process_cpu_seconds_total{job="<job>"}[5m])` |

**按 kind 追加**：

| kind | 追加面板 |
|---|---|
| api | 全局 API QPS / Latency / Error Rate（不限 module） |
| gateway | online_user_num, ws_connection_num, ws utilization, ws_conn_rejected |
| transfer | msg_insert_redis/mongo success/fail, seq_set_failed |
| push | msg_offline_push_failed, msg_long_time_push |
| rpc | 该 job 上 rpc_count QPS / Latency / Top Methods |
| cron | cron_task_runs_total, cron_task_duration_seconds, cron_task_last_success_unixtime |

**rpc 专属追加**（按服务 id）：

| 服务 id | 追加面板 |
|---|---|
| rpc-auth | `rate(user_login_total{job="<job>"}[5m])` |
| rpc-user | `rate(user_register_total{job="<job>"}[5m])` |
| rpc-msg | single/group chat process success/fail |

多实例 job（msgtransfer、push）使用 `sum(...)` 聚合。

## 目录与生成器

### 源文件

```
config/grafana-dashboards/
  modules.yaml          # 模块清单（domains + services）
  generate.go           # 生成器（推荐 Go，与仓库一致）
```

### 生成物

```
config/grafana-template/
  domain/
    Domain-Auth.json
    ...
  service/
    Service-API.json
    ...
```

生成命令（Makefile target）：

```makefile
gen-grafana-dashboards:
	go run ./config/grafana-dashboards/generate.go
```

生成物提交进 git；部署机无需运行生成器。

### modules.yaml 结构

```yaml
domains:
  - id: auth
    title: Auth
    apiModule: auth
    rpcName: auth

services:
  - id: api
    title: API
    job: openimserver-openim-api
    kind: api
    rpcName: null
    extras: []
```

`extras` 可选，用于 rpc-auth / rpc-user / rpc-msg 等专属面板。

## Grafana Provisioning

当前 `config/grafana/provisioning/dashboards/default.yml`：

```yaml
options:
  path: /etc/grafana/dashboards
```

Grafana file provider **支持子目录递归**。生成物放入 `domain/`、`service/` 子目录即可，无需改 provisioning。

docker-compose volume 已挂载：

```yaml
- ./config/grafana-template:/etc/grafana/dashboards
```

## JSON 规范

与现有看板保持一致：

- `schemaVersion`: 39
- `datasource`: `{ "type": "prometheus", "uid": "prometheus" }`
- `refresh`: `30s`
- `time`: `{ "from": "now-1h", "to": "now" }`
- `tags`: `["openim", "domain"|"service", "<id>"]`
- stat 面板需包含 `options.reduceOptions`（参考 Middleware.json），避免 Grafana 11 渲染异常

## 验证计划

1. 本地运行 `make gen-grafana-dashboards`，确认生成 35 张 JSON
2. 在 Prometheus `19091` 上 spot-check 各看板 PromQL 有 series
3. 重启/等待 Grafana provisioning（30s interval），确认 OpenIM folder 出现新看板
4. 抽查：Domain/Auth、Service/msggateway、Service/rpc-msg 有曲线

## 后续可选

- 在总览 API-SLI 看板加 `links` 跳转到各 domain 看板
- 总览面板 drilldown（需 Grafana 10+ dashboard links + 变量）
