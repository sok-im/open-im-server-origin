# chatbot-ai

OpenIM 客服机器人 AI 后端：接收 `callbackAfterSendMsgToBotCommand`，用 LlamaIndex + Chroma 做单轮 FAQ 检索回答，再经 OpenIM admin token 回写 `send_msg`。

## 启动

```bash
cd services/chatbot-ai
python -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
cp .env.example .env   # 填 LLM key、OPENIM_API、BOT_USER_ID 等
uvicorn app.main:app --host 0.0.0.0 --port 8000
```

健康检查：`GET /healthz` → `{"ok": true}`。

## 对接 OpenIM（`share.yml`）

`config/share.yml` 中：

```yaml
chatbot:
  enable: true
  userID: "openIMChatbot"          # 必须与 BOT_USER_ID 一致
  nickname: "在线客服"
  callbackURL: "http://<host>:8000"  # 本服务根地址，不要带尾部 command
  timeout: 5
```

OpenIM 会把 command 拼到 URL 后，实际请求：

`POST {callbackURL}/callbackAfterSendMsgToBotCommand`

改完后重启 `openim-rpc-user` / `openim-rpc-msg`。

### 对接要点

1. **Admin token 回写**：机器人账号不能自取用户 token。本服务用 `OPENIM_ADMIN_USER` + `OPENIM_ADMIN_SECRET`（或预设 `OPENIM_ADMIN_TOKEN`）拿 IMAdmin token，以 `sendID=BOT_USER_ID` 调 `/msg/send_msg` 发回复。Token 按 `expireTimeSeconds` 缓存并提前约 60s 刷新；`send_msg` 遇 HTTP 401 / OpenIM token errCode 时清缓存并重试一次。
2. **回调尽力一次**：OpenIM 侧异步回调可能丢弃或重放；本服务按 `clientMsgID` 幂等，同一 ID 不会二次回复。
3. **`callbackURL`**：指向本服务根地址（如 `http://127.0.0.1:8000`），无尾部 path/command。
4. **可选回调密钥**：若设置环境变量 `CALLBACK_SECRET`，则要求请求头 `X-Callback-Secret` 与之相等，否则 401；未设置时不校验（便于本地开发）。OpenIM 侧需自行在回调 HTTP 头里带上该值（或在网关注入）。

## 幂等

- Key：`clientMsgID`
- 拿到短 TTL 锁才处理；成功 `confirm` 后延长到长 TTL（约 24h）
- 失败保留短 TTL，抑制风暴重试
- **仅 `IDEMPOTENCY_BACKEND=memory` 已实现**（进程内，单实例）。`redis` 会 `NotImplementedError`，多实例部署暂不可用。

## 知识库重建

启动时空 Chroma 会自动 ingest `KNOWLEDGE_DIR`。手动全量重建：

```bash
curl -X POST -H "X-Admin-Token: change-me" http://127.0.0.1:8000/admin/reindex
```

`X-Admin-Token` 必须等于环境变量 `ADMIN_REINDEX_TOKEN`（默认 `change-me`）。

## 测试

```bash
cd services/chatbot-ai
PYTHONPATH=. python -m pytest -q
```
