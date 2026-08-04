# 客服机器人外部 AI 后端设计文档

## 背景与目标

OpenIM 侧「客服机器人回调」已定（见 `2026-08-04-chatbot-callback-design.md`）：命中机器人后异步 POST 外部服务；回复走现有 `/msg/send_msg`。

本设计定义**外部 AI 后端**：接收回调 → RAG 检索 → LLM 生成 → 以机器人身份回写 OpenIM。

目标：

1. 独立 Python 服务，不进入 OpenIM 进程
2. 小规模 FAQ/文档（几百～几千 chunk）单租户知识库
3. 与 OpenIM 回调契约对齐（幂等、快速 ACK、不拖垮 IM）

## 设计决策（已确认）

| 决策点 | 结论 |
|--------|------|
| 知识规模 | 小规模 FAQ/文档，单租户 |
| RAG 框架 | **方案 B：LlamaIndex**（不上 LangChain / Agent） |
| 向量库 | **Chroma**（本地持久化目录） |
| LLM | 可插拔，默认 OpenAI 兼容 API |
| 技术栈 | Python + FastAPI |
| 对话上下文 | **无状态单轮**（仅当前消息 + 检索片段） |
| 知识权威源 | 磁盘 `knowledge/`；Chroma 可重建 |

### 为何选 LlamaIndex 而不是自写薄层 / LangChain

- 方案 A（自写薄 RAG）更轻，但团队选择方案 B，用 LlamaIndex 承担 ingest / index / retrieve / synthesize。
- LangChain 偏 Agent/链式编排，本场景是检索问答，不采用。
- OpenIM 回调、幂等、回写仍由自写代码完成，不交给框架。

### 为何选 Chroma

- 规模匹配、LlamaIndex 集成成熟、无独立向量服务、目录挂 volume 即可。
- 不上 Qdrant/Milvus/Weaviate（运维重）；LanceDB 为备选，本设计定死 Chroma。

## 架构与数据流

```
OpenIM rpc/msg
  └─ POST {callbackURL}/callbackAfterSendMsgToBotCommand
        │
        ▼
   FastAPI（立即 200）
        │
        ├─ clientMsgID 幂等（memory/Redis TTL）
        ├─ LlamaIndex：embed → Chroma retrieve top-k
        ├─ LlamaIndex：OpenAI 兼容 LLM 合成回答
        └─ HTTP → OpenIM /msg/send_msg（admin token + sendID=bot）
```

进程边界：

- 配置驱动：OpenIM API、admin 凭证、机器人 userID、LLM/Embedding、Chroma/知识目录
- 失败隔离：回调快速 ACK；检索/LLM/回写失败只影响机器人回复
- 索引非权威源：换 embedding 模型或切块策略 → 全量重建 Chroma

## LlamaIndex 组件与知识入库

### 索引管线

| 步骤 | 组件 | 约定 |
|------|------|------|
| 读文档 | `SimpleDirectoryReader` | `KNOWLEDGE_DIR`，`.md` / `.txt` |
| 切块 | `SentenceSplitter` | `chunk_size≈512`，`chunk_overlap≈64` |
| Embedding | OpenAI 兼容 Embedding | `EMBED_*` 配置 |
| 向量存储 | `ChromaVectorStore` | `CHROMA_PATH`，collection=`faq` |
| 索引 | `VectorStoreIndex` | 启动加载；空则全量 ingest |

### 入库触发

1. **启动时**：collection 为空 → 自动 ingest；非空 → 只加载
2. **管理接口**：`POST /admin/reindex`（`ADMIN_REINDEX_TOKEN`）→ 清空并重建

首期不做增量/按文件 hash 差量；小规模全量重建即可。

### 在线查询（单轮）

用户问题 → Retriever（`similarity_top_k=3~5`）→ 系统提示约束「仅依据资料、无依据则说不知道」→ LLM 生成 → 文本。

使用 `RetrieverQueryEngine`（或等价 retrieve + synthesize）。不上 Agent、不上多轮 Memory。

## OpenIM 回调、幂等与回写

### 回调入口

- `POST /callbackAfterSendMsgToBotCommand`（OpenIM 拼在 `callbackURL` 后）
- Handler 快速返回 200，业务进 BackgroundTasks / 内部队列

### 入参过滤

- 关键字段：`sendID`、`recvID`、`groupID`、`sessionType`、`content`、`contentType`、`clientMsgID`
- 非允许类型 / 空 content → 丢弃
- `clientMsgID` 已处理 → 幂等跳过

### 幂等

- Key：`processed:{clientMsgID}`，成功路径 TTL 24h
- 后端：`memory`（单实例）或 `redis`
- 拿到锁才处理；处理失败保留短 TTL（如 5min）防风暴

### 回写

- `POST {OPENIM_API}/msg/send_msg`
- 使用 **IMAdmin** token（机器人不能自取 token）
- `sendID` = `BOT_USER_ID`（与 `share.yml` chatbot.userID 一致）
- 单聊：`recvID` = 原 `sendID`；群聊：`groupID` + `sessionType=3`
- 文本：`contentType=101`，content 为 OpenIM 文本 JSON；自生成新 `clientMsgID`

### 失败兜底

| 情况 | 行为 |
|------|------|
| 检索空/低相关 | 固定兜底文案 |
| LLM 超时/5xx | 「客服繁忙，请稍后再试」+ 日志 |
| SendMsg 失败 | 重试 1～2 次；仍失败只记日志 |

## 目录结构

```
ai-customer-bot/                 # 建议独立目录或 monorepo 子目录
  app/
    main.py
    config.py
    callback.py
    openim_client.py
    rag/
      ingest.py
      query.py
      index.py
    idempotency.py
  knowledge/
  data/chroma/
  requirements.txt
  .env.example
  README.md
```

**代码落点**：本服务**不**写入 `openim-rpc-*`；可放在仓库旁目录（如 `../ai-customer-bot`）或本仓库 `services/chatbot-ai/`（实现计划阶段再定路径）。Spec 与 OpenIM 文档同放在 `docs/superpowers/specs/` 便于对照。

## 配置清单

| 变量 | 含义 |
|------|------|
| `OPENIM_API` | API 根地址 |
| `OPENIM_ADMIN_USER` / `OPENIM_ADMIN_SECRET` 或 `OPENIM_ADMIN_TOKEN` | 回写鉴权 |
| `BOT_USER_ID` | 机器人账号 |
| `LLM_BASE_URL` / `LLM_API_KEY` / `LLM_MODEL` | Chat |
| `EMBED_BASE_URL` / `EMBED_API_KEY` / `EMBED_MODEL` | Embedding |
| `CHROMA_PATH` / `KNOWLEDGE_DIR` | 默认 `./data/chroma`、`./knowledge` |
| `SIMILARITY_TOP_K` | 默认 `4` |
| `ADMIN_REINDEX_TOKEN` | reindex 鉴权 |
| `IDEMPOTENCY_BACKEND` | `memory` \| `redis` |

## 对接文档必写

1. 用 admin token + `sendID=bot` 发回复（机器人不能自取 token）
2. 回调尽力一次，可能丢弃；AI 后端按 `clientMsgID` 幂等
3. `share.yml` 的 `chatbot.callbackURL` 指向本服务根地址（无尾部 command）

## 明确不做（YAGNI）

- 多轮记忆、转人工、多租户知识库
- Agent / 工具调用、流式推送到 IM
- 增量 ingest、复杂 PDF、Rerank
- 把本服务并入 OpenIM 进程

## 与 OpenIM 侧的关系

| 侧 | 职责 |
|----|------|
| OpenIM（已实现/已设计） | 机器人账号、命中判定、异步回调 |
| 本服务 | RAG、LLM、幂等、回写 |

联调：`chatbot.enable=true`，`callbackURL` 指向本服务，重启 `openim-rpc-user` / `openim-rpc-msg` 后单聊机器人或群 @ 机器人验证。
