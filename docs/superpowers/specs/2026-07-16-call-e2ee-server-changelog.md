# 音视频通话 E2EE — 服务端修改说明

> 日期：2026-07-16  
> 分支：`lintao1`  
> 协议子模块：`protocol` @ `c41ba8f`  
> 兼容策略：**方案 A** — 仅当邀请 `e2ee.required=true` 时启用门禁；无描述符的通话行为与现网一致

---

## 1. 提交一览

| Commit | 说明 |
|---|---|
| `2721db299` | 服务端设计 spec |
| `5b60b651b` | 服务端实现计划 |
| `c41ba8f`（protocol） | RTC/OpenMLS proto + 生成代码 |
| `025148069` | 错误码 1830–1834 / 4010 +  bump protocol |
| `a7daa20e5` | 邀请/Commit 存储扩展与幂等索引 |
| `6c2f74140` | E2EE helper（解析/conversationID/能力校验） |
| `adf18e092` | 信令透传、Token 门禁、自定义信令、Commit CAS、踢人联动 |

父仓相对文档基线约 **+1448 / −107**（含 docs）；核心代码主要在 `adf18e092` 等实现提交。

---

## 2. 协议层（`protocol` 子模块）

### 2.1 `rtc/rtc.proto`

- 新增 `E2EECapability`（`schemes` / `frameCryptor` / `maxKeyRingSize` / `platform` / `clientVersion`）
- `InvitationInfo.conversationID`
- 请求增加 `e2eeCapability`：Invite / InviteInGroup / Accept / Join / GetTokenByRoomID
- 响应增加 `conversationID` 和/或 `e2ee`：Invite、InviteInGroup、Accept、Join、GetRoomByGroupID、GetTokenByRoomID、StartApp
- 新增 RPC：`SignalRemoveParticipants(groupID, userIDs)`

### 2.2 `openmls/openmls.proto`

- `SubmitCommitReq`：`commitHash`、`idempotencyKey`
- `SubmitCommitResp`：`accepted`、`duplicate`、`acceptedEpoch`、`commitID`、`expectedFromEpoch`（保留原 `newEpoch` 等字段）

---

## 3. 错误码（`pkg/common/servererrs`）

| 码 | 常量 | 文案 |
|---|---|---|
| 1830 | `CALL_E2EE_REQUIRED_UNSUPPORTED` | 强制 E2EE 但客户端不支持 |
| 1831 | `CALL_E2EE_CONVERSATION_NOT_READY` | 会话/MLS 未就绪（预留） |
| 1832 | `CALL_E2EE_GROUP_MEMBERSHIP_INVALID` | 非邀请方/非群成员 |
| 1833 | `CALL_E2EE_PROTOCOL_VERSION_MISMATCH` | scheme/版本不匹配 |
| 1834 | `CALL_E2EE_TOKEN_DENIED` | Token 被拒（预留） |
| 4010 | `epoch conflict` | MLS Commit epoch 冲突 |

---

## 4. 存储层

### 4.1 `SignalInvitation`（`pkg/common/storage/model/signal.go`）

新增字段：

- `conversation_id`
- `e2ee_required`
- `call_id`

`custom_data` 仍存完整不透明 JSON（含 `e2ee` 对象）。

### 4.2 `MLSCommit`

新增：`from_epoch`、`commit_hash`、`idempotency_key`  
Mongo：`idempotency_key` 非空唯一（partial unique index）  
接口：`FindByIdempotencyKey`

---

## 5. RTC 业务（`internal/rpc/rtc`）

### 5.1 新增文件

| 文件 | 作用 |
|---|---|
| `e2ee.go` | 解析 `customData.e2ee`、规范化 `conversationID`、能力校验 |
| `custom_signal.go` | 自定义信令：成员校验、16KB、限流、messageID 去重、补发送者/serverSeq |
| `remove_participants.go` | 按 groupID 查进行中房间并 LiveKit `RemoveParticipant` |

### 5.2 `signal.go` 行为变更

- `invitationToModel` / `modelToInvitationInfo`：落库并回传 E2EE 扩展字段与 `conversationID`
- Invite / InviteInGroup：写库前解析描述符；响应带 `conversationID`；E2EE 房间发 Token 前校验能力
- Accept / Join / GetToken / GetRoom / StartApp：回传 `conversationID` + `e2ee`（从 `customData` 抽出）
- `genToken(roomID, userID, e2ee)`：E2EE 时 TTL≤5min，JWT Attributes `e2ee=true`
- `ensureCallParticipant`：E2EE 单聊禁止任意加人；群聊用 `GetGroupMemberInfo` 权威校验后再 AddInvitee；非 E2EE 保持原「可 AddInvitee」行为

### 5.3 `server.go` / 配置

- 注入 Redis（自定义信令限流/去重）
- 配置项：
  - `liveKit.e2eeTokenExpiry`（默认 300）
  - `e2ee.allowedSchemes`（默认 `mls-exporter-livekit-v1`）
  - `e2ee.minVersion`（默认 1）
- 文件：`config/openim-rpc-rtc.yml`、`pkg/common/config/config.go`

### 5.4 自定义信令规则

- 房间必须存在
- 发送者 ∈ 邀请名单，或为群成员
- `customInfo` ≤ 16KB
- Redis：约 20 条/秒/用户/房间 突发上限；`(roomID, messageID)` SET NX 去重
- 转发载荷补：`senderUserID`、`senderPlatformID`、`serverSeq`；**不解密** `mlsMessage`，不打密文日志

---

## 6. OpenMLS DS（`internal/rpc/openmls/service.go`）

`SubmitCommit`：

1. `idempotencyKey` 命中 → `accepted=true, duplicate=true`，不推进 epoch  
2. `IncrEpoch(fromEpoch)` 成功 → 落库（含 from/hash/idempotency）→ `accepted=true, acceptedEpoch=newEpoch`  
3. epoch 不匹配 → 错误码 **4010**，文案含 `expectedFromEpoch=N`（响应体也填了 `expectedFromEpoch`，但 gRPC 带 err 时客户端可能只看到错误）

旧字段 `newEpoch` / `sequenceNumber` 成功时仍填充。

---

## 7. 群服务联动（`internal/rpc/group`）

- 注入 `rtcClient`（`RpcRegisterName.Rtc`）
- `KickGroupMember` / `QuitGroup`：在原有 `RemoveMemberTrigger` 之后异步调用 `SignalRemoveParticipants`
- 踢人失败只打日志，不回滚踢群/退群

`pkg/rpcli/rtc.go`：新增 `SignalRemoveParticipants` 封装。

---

## 8. 兼容性与安全边界

**兼容：**

- 无 `e2ee` 或 `required!=true` → 不校验能力、长 TTL Token、原成员扩展逻辑
- 有 `required=true` → 能力门禁 + 短 TTL + 严格成员校验

**不做 / 不存：**

- 不生成、不存储、不转发 `K_media` 或可派生材料
- 不解密 MLS 控制密文
- 不提供 E2EE→非 E2EE 静默降级
- LiveKit Egress/录制禁用依赖 SFU 侧配置（服务端仅 JWT 标记 `e2ee=true`）

---

## 9. 测试与验证

已跑通：

- `go test ./internal/rpc/rtc ./internal/rpc/openmls`
- E2EE helper / custom signal 单测
- 顺带修复 `offline_push_test` 对 `FriendAliasInfo` 的签名适配（与展示名逻辑对齐）

未纳本变更范围：依赖真实 Mongo 的 `mgo` 集成测试（环境不可达）。

---

## 10. 客户端对接要点（摘要）

1. Invite 写入 `customData.e2ee`；Invite/Accept/Join/GetToken 带 `e2eeCapability`  
2. 使用响应 `conversationID` / `e2ee`，勿猜会话 ID  
3. E2EE 通话短周期续 Token（≤5min）  
4. 换钥走 `SignalSendCustomSignal`；监听接收回调中的透传密文  
5. Commit：先 DS 再 merge；4010 追赶后重生 Commit  

详见：`docs/superpowers/specs/2026-07-16-call-e2ee-client-plan.md`

---

## 11. 文档产物

| 路径 | 内容 |
|---|---|
| `docs/superpowers/specs/2026-07-16-call-e2ee-server-design.md` | 设计 |
| `docs/superpowers/plans/2026-07-16-call-e2ee-server.md` | 实现计划 |
| `docs/superpowers/specs/2026-07-16-call-e2ee-client-plan.md` | 客户端改造方案 |
| `音视频通话E2EE-服务端接口与实施方案.md` | 原始接口契约 |
