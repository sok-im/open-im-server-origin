# 客服机器人外部 AI 后端 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现独立 FastAPI 服务：接收 OpenIM 机器人回调 → LlamaIndex+Chroma RAG → OpenAI 兼容 LLM → 以机器人身份调用 `/msg/send_msg` 回写。

**Architecture:** 服务落在本仓库 `services/chatbot-ai/`（不进入 `openim-rpc-*`）。回调快速 200 + BackgroundTasks；幂等按 `clientMsgID`；知识权威源为 `knowledge/`，Chroma 可全量重建。

**Tech Stack:** Python 3.11+、FastAPI、uvicorn、LlamaIndex、Chroma、httpx、pytest。

## Global Constraints

- 路径根：`services/chatbot-ai/`（相对仓库根 `open-im-server-origin`）。
- RAG：LlamaIndex + Chroma，collection 名 `faq`；不上 LangChain/Agent/多轮 Memory。
- 单轮：仅当前用户问题 + 检索片段。
- 回调路径：`POST /callbackAfterSendMsgToBotCommand`（与 OpenIM `callbackURL`+command 拼接一致）。
- 回写：`POST {OPENIM_API}/msg/send_msg`，admin token，`sendID=BOT_USER_ID`；文本 `contentType=101`，`content` 为 `{"content":"..."}` 映射到 API 的 `content` map。
- OpenIM HTTP API `SendMsgReq`：`recvID` + 内嵌字段 `sendID/groupID/sessionType/contentType/content`（见 `pkg/apistruct/manage.go`）。
- 幂等 key：`processed:{clientMsgID}`；memory 或 redis；成功 TTL 24h，失败短 TTL 5min。
- 测试：`cd services/chatbot-ai && python -m pytest -q`；不强制联调真实 LLM/OpenIM（用 mock）。
- YAGNI：无增量 ingest、无 PDF、无 Rerank、无流式 IM。

---

## File Structure

```
services/chatbot-ai/
  app/
    __init__.py
    main.py                 # FastAPI app + lifespan + routes
    config.py               # pydantic-settings / env
    callback.py             # 回调解析、过滤、调度后台任务
    openim_client.py        # get_admin_token + send_text
    idempotency.py          # try_acquire / mark 接口
    rag/
      __init__.py
      index.py              # chroma + VectorStoreIndex 加载/重建
      ingest.py             # SimpleDirectoryReader → SentenceSplitter → index
      query.py              # RetrieverQueryEngine + 系统提示
  knowledge/sample_faq.md   # 样例 FAQ
  tests/
    test_idempotency.py
    test_openim_client.py
    test_callback_filter.py
    test_query_prompt.py    # 可选：系统提示/空检索兜底纯逻辑
  requirements.txt
  .env.example
  README.md
  data/chroma/              # gitignore
```

---

### Task 1: 脚手架与配置

**Files:**
- Create: `services/chatbot-ai/requirements.txt`
- Create: `services/chatbot-ai/.env.example`
- Create: `services/chatbot-ai/app/__init__.py`
- Create: `services/chatbot-ai/app/config.py`
- Create: `services/chatbot-ai/knowledge/sample_faq.md`
- Create: `services/chatbot-ai/.gitignore`

**Interfaces:**
- Produces: `Settings`（pydantic BaseSettings）字段与 Global Constraints 配置表一致；`get_settings()` 缓存单例。

- [ ] **Step 1: 写 requirements.txt**

```text
fastapi>=0.110
uvicorn[standard]>=0.27
httpx>=0.27
pydantic-settings>=2.2
llama-index-core>=0.11
llama-index-embeddings-openai>=0.2
llama-index-llms-openai-like>=0.2
llama-index-vector-stores-chroma>=0.2
chromadb>=0.5
pytest>=8.0
pytest-asyncio>=0.23
respx>=0.21
```

（若 `llama-index-llms-openai-like` 包名随版本变化，以实现时 PyPI 实际名为准，保持 OpenAI 兼容 `api_base`。）

- [ ] **Step 2: 写 config.py**

```python
from functools import lru_cache
from pydantic_settings import BaseSettings, SettingsConfigDict

class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    openim_api: str = "http://127.0.0.1:10002"
    openim_admin_user: str = "imAdmin"
    openim_admin_secret: str = "openIM123"
    openim_admin_token: str = ""  # 非空则跳过登录
    bot_user_id: str = "openIMChatbot"
    bot_nickname: str = "在线客服"

    llm_base_url: str = "https://api.openai.com/v1"
    llm_api_key: str = ""
    llm_model: str = "gpt-4o-mini"

    embed_base_url: str = ""
    embed_api_key: str = ""
    embed_model: str = "text-embedding-3-small"

    chroma_path: str = "./data/chroma"
    knowledge_dir: str = "./knowledge"
    similarity_top_k: int = 4
    admin_reindex_token: str = "change-me"
    idempotency_backend: str = "memory"  # memory | redis
    redis_url: str = "redis://127.0.0.1:6379/0"

    fallback_no_hit: str = "暂未查到相关说明，请换个问法或联系人工客服。"
    fallback_busy: str = "客服繁忙，请稍后再试。"

@lru_cache
def get_settings() -> Settings:
    return Settings()
```

`embed_base_url` / `embed_api_key` 为空时回退到 `llm_base_url` / `llm_api_key`。

- [ ] **Step 3: sample FAQ + gitignore + .env.example**

`knowledge/sample_faq.md` 写 3～5 条中文 FAQ。`.gitignore` 含 `data/`、`.env`、`__pycache__/`、`.venv/`。`.env.example` 列出全部 Settings 字段示例值。

- [ ] **Step 4: Commit**

```bash
git add services/chatbot-ai
git commit -m "chore(chatbot-ai): scaffold config and sample knowledge"
```

---

### Task 2: 幂等（TDD）

**Files:**
- Create: `services/chatbot-ai/app/idempotency.py`
- Test: `services/chatbot-ai/tests/test_idempotency.py`

**Interfaces:**
- Produces: `class IdempotencyStore` with `try_acquire(client_msg_id: str) -> bool`、`release_short(client_msg_id: str)`（失败短锁可选，首期：acquire 成功即占位 5min，`confirm(client_msg_id)` 升为 24h）。

简化接口（按此实现）：

```python
class IdempotencyStore:
    def try_acquire(self, client_msg_id: str) -> bool: ...
    def confirm(self, client_msg_id: str) -> None: ...  # TTL 24h
```

`try_acquire`：若 key 不存在则写入短 TTL（5min）并返回 True；已存在返回 False。  
`confirm`：将 TTL 延长到 24h（或重写）。  
Memory 实现用 dict + expiry timestamp；`idempotency_backend=redis` 时用 redis `SET NX EX`。

- [ ] **Step 1: 写失败测试**

```python
import time
from app.idempotency import MemoryIdempotencyStore

def test_acquire_once():
    s = MemoryIdempotencyStore(short_ttl=60, long_ttl=86400)
    assert s.try_acquire("m1") is True
    assert s.try_acquire("m1") is False

def test_confirm_keeps_block():
    s = MemoryIdempotencyStore(short_ttl=1, long_ttl=86400)
    assert s.try_acquire("m2") is True
    s.confirm("m2")
    assert s.try_acquire("m2") is False
```

- [ ] **Step 2: Run test — expect fail（模块不存在）**

```bash
cd services/chatbot-ai && python -m pytest tests/test_idempotency.py -q
```

- [ ] **Step 3: 实现 MemoryIdempotencyStore + factory `build_idempotency(settings)`**

- [ ] **Step 4: Run test — expect pass**

- [ ] **Step 5: Commit**

```bash
git add services/chatbot-ai/app/idempotency.py services/chatbot-ai/tests/test_idempotency.py
git commit -m "feat(chatbot-ai): add clientMsgID idempotency store"
```

---

### Task 3: OpenIM HTTP 客户端（TDD）

**Files:**
- Create: `services/chatbot-ai/app/openim_client.py`
- Test: `services/chatbot-ai/tests/test_openim_client.py`

**Interfaces:**
- Produces:
  - `async def ensure_admin_token(client: httpx.AsyncClient, settings) -> str`
  - `async def send_bot_text(client, settings, *, token, text, session_type, recv_user_id="", group_id="") -> None`
- `get_admin_token`：`POST {openim_api}/auth/get_admin_token`，body `{"secret": openim_admin_secret, "userID": openim_admin_user}`（若 OpenIM 实际字段不同，以实现时对照 API 调整并更新 README）。
- `send_msg`：`POST {openim_api}/msg/send_msg`，headers `token`、`operationID`（uuid）。

发送 body 形状：

```python
{
  "recvID": recv_user_id,  # 单聊必填；群聊可空字符串视 API 要求
  "sendID": settings.bot_user_id,
  "senderNickname": settings.bot_nickname,
  "groupID": group_id,
  "senderPlatformID": 0,
  "content": {"content": text},
  "contentType": 101,
  "sessionType": session_type,  # 1 单聊 / 3 群聊
  "isOnlineOnly": False,
  "notOfflinePush": False,
  "sendTime": 0,
}
```

`send_bot_text`：失败重试最多 2 次（共 3 次尝试），指数退避 0.2s/0.5s；仍失败 raise。

- [ ] **Step 1: 用 respx mock 写测试**（token 缓存、send 成功、send 失败重试）

- [ ] **Step 2: pytest 红**

- [ ] **Step 3: 实现 openim_client.py**

- [ ] **Step 4: pytest 绿**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(chatbot-ai): OpenIM admin token and send_msg client"
```

---

### Task 4: 回调过滤纯逻辑（TDD）

**Files:**
- Create: `services/chatbot-ai/app/callback.py`（先过滤函数，路由后置）
- Test: `services/chatbot-ai/tests/test_callback_filter.py`

**Interfaces:**
- Produces: `def parse_user_question(payload: dict) -> str | None`  
  - `contentType` 非 101（Text）且非 106（AtText，若常量如此）→ None（实现时对照 OpenIM：Text=101，AtText=106）  
  - `content` 可能是 JSON 字符串 `{"content":"..."}` 或纯文本 → 抽出问题文本；空 → None

- [ ] **Step 1–4: TDD 实现 parse_user_question**

用例：纯 JSON content、空、错误类型、AtText 带 content。

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(chatbot-ai): parse OpenIM callback text content"
```

---

### Task 5: RAG ingest + query

**Files:**
- Create: `services/chatbot-ai/app/rag/index.py`
- Create: `services/chatbot-ai/app/rag/ingest.py`
- Create: `services/chatbot-ai/app/rag/query.py`
- Create: `services/chatbot-ai/app/rag/__init__.py`

**Interfaces:**
- `build_embed_model(settings)` / `build_llm(settings)`：OpenAI-compatible（`api_base`=`*_base_url`）
- `get_or_create_index(settings, *, force_rebuild: bool=False) -> VectorStoreIndex`
- `ingest_knowledge(settings) -> int` 返回 node 数
- `answer_question(settings, index, question: str) -> str`  
  - Retriever `similarity_top_k=settings.similarity_top_k`  
  - 系统提示：仅依据资料；无依据返回 `settings.fallback_no_hit`  
  - LLM 异常 → 返回 `settings.fallback_busy`

实现要点：

```python
# ingest: SimpleDirectoryReader(knowledge_dir) → SentenceSplitter(512, 64)
# → VectorStoreIndex.from_documents(..., storage_context with ChromaVectorStore)
# force_rebuild: 删除 chroma collection 或清空 persist 目录后重建
```

- [ ] **Step 1: 实现 index/ingest/query**（可用环境变量跳过真实 embedding 的单测；本任务以手动 `python -c` 或可选 integration mark 验证）

最小可测：对 `answer_question` 抽纯函数 `compose_fallback(retrieved_empty: bool, llm_error: bool, settings) -> str | None` 单测兜底分支；完整 RAG 联调放 README。

- [ ] **Step 2: 兜底逻辑单测通过**

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(chatbot-ai): LlamaIndex Chroma ingest and query"
```

---

### Task 6: FastAPI 路由接线 + README

**Files:**
- Create: `services/chatbot-ai/app/main.py`
- Modify: `services/chatbot-ai/app/callback.py`（加上 process 流水线）
- Create: `services/chatbot-ai/README.md`

**Interfaces:**
- Lifespan：加载 settings → 构建 idempotency → `get_or_create_index`（空则 ingest）
- `POST /callbackAfterSendMsgToBotCommand`：解析 body → `try_acquire` → BackgroundTasks 跑 pipeline → 立即 `{"actionCode":0}`
- Pipeline：`parse_user_question` → `answer_question` → `send_bot_text`（单聊 recv=sendID；群聊 groupID）→ `confirm`
- `POST /admin/reindex`：Header `X-Admin-Token` == `admin_reindex_token` → `force_rebuild`
- `GET /healthz` → `{"ok": true}`

Pipeline 伪代码：

```python
async def handle_bot_message(payload, app_state):
    cid = payload.get("clientMsgID") or ""
    if not cid or not app_state.idem.try_acquire(cid):
        return
    try:
        q = parse_user_question(payload)
        if not q:
            return
        text = answer_question(app_state.settings, app_state.index, q)
        st = int(payload.get("sessionType") or 0)
        async with httpx.AsyncClient(timeout=30) as client:
            token = await ensure_admin_token(client, app_state.settings)
            if st == 1:
                await send_bot_text(..., session_type=1, recv_user_id=payload["sendID"])
            elif st == 3:
                await send_bot_text(..., session_type=3, group_id=payload["groupID"])
        app_state.idem.confirm(cid)
    except Exception:
        log.exception("bot pipeline failed")
```

README 必写：如何配 `share.yml` chatbot.callbackURL、admin token 语义、幂等、启动命令 `uvicorn app.main:app --host 0.0.0.0 --port 8000`、reindex。

- [ ] **Step 1: 实现 main + pipeline**

- [ ] **Step 2: `python -m pytest -q` 全绿**

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(chatbot-ai): FastAPI callback pipeline and README"
```

---

## 手动联调（实现完成后）

1. 填 `.env`（LLM key、OPENIM_API、BOT_USER_ID）
2. `uvicorn app.main:app --port 8000`
3. `share.yml`：`chatbot.enable=true`，`callbackURL: http://<host>:8000`，`userID` 与 `BOT_USER_ID` 一致；重启 user/msg
4. 用户单聊机器人发 FAQ 相关问题 → 收到回复
5. 同一 `clientMsgID` 重放回调 → 不二次回复
6. `curl -X POST -H "X-Admin-Token: ..." http://127.0.0.1:8000/admin/reindex`

## Spec 覆盖自检

- LlamaIndex + Chroma + 单轮 → Task 5 ✅
- 回调快速 ACK + 后台处理 → Task 6 ✅
- 幂等 clientMsgID → Task 2 ✅
- OpenIM send_msg / admin token → Task 3 ✅
- ingest 启动空库 + reindex → Task 5/6 ✅
- 兜底文案 → Task 5/6 ✅
- README 对接三条 → Task 6 ✅
