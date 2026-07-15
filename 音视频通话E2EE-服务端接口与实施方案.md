# 音视频通话 E2EE 服务端接口与实施方案

> 本文档基于《音视频通话端到端加密（E2EE）实施计划.md》第 5 章「OpenIMSDK 与服务端契约」整理，并补充第 1、3、8 章相关约束，面向 OpenIM 信令服务、Token 服务、MLS Commit 分发服务（DS）与 LiveKit 服务端的实现团队。

---

## 0. 服务端职责与安全红线

### 0.1 职责边界

| 组件 | 职责 | 禁止事项 |
|---|---|---|
| OpenIM 服务端 | 房间成员授权、Token 门禁、控制消息转发、Commit 分发服务（DS） | 不解密 MLS 控制消息，不保存媒体密钥 |
| LiveKit SFU | 转发已加密编码帧 | 不录制、转码、转写或解密 E2EE 房间 |

### 0.2 服务端安全红线（强制）

1. 服务端**绝不生成、保存、下发**媒体密钥 `K_media`，也不得保存任何可派生 `K_media` 的材料（MLS 导出器秘密值、签名私钥等）。
2. 服务端**不解密** `mlsMessage`（MLS 应用密文），只做透明转发。
3. JWT、房间元数据、Webhook、日志、分析、崩溃报告中**均不得出现**密钥、密钥承诺值、MLS 密文明文。
4. E2EE 为**强制模式**，服务端不得提供任何静默降级到非 E2EE 通话的路径。
5. E2EE 房间**禁用**服务端录制、Egress、转码、转写、审核、混流、SIP/PSTN、服务端 AI 媒体处理与服务端代理。

---

## 1. 接口总览

| 编号 | 接口/能力 | 类型 | 核心变更 | 幂等 |
|---|---|---|---|---|
| A1 | `signalingInvite` | 信令 | 透传并回存 `e2ee` 描述符 + `e2eeCapability`，返回规范化 `conversationID` | 依赖 `callID`/`roomID` |
| A2 | `signalingInviteInGroup` | 信令 | 同上（群聊） | 同上 |
| A3 | `signalingAccept` | 信令 | 校验并回传描述符 + 上报能力 | 同上 |
| A4 | `signalingJoin` | 信令 | 群通话加入，校验成员身份并回传描述符 | 同上 |
| A5 | `signalingGetTokenByRoomID` | Token | 能力/版本/成员门禁，发放带 `e2ee=true` 元数据的 JWT | 幂等发放 |
| A6 | `signalingGetRoomByGroupID` | 信令 | 回传房间当前描述符 | 只读 |
| A7 | `signalingGetInvitationInfoStartApp` | 信令 | 应用启动恢复，回传完整描述符 | 只读 |
| A8 | 离线推送恢复 | 推送 | 推送载荷携带非秘密描述符 | 只读 |
| B1 | `signalingSendCustomSignal` | 自定义信令 | 新增发送链路，透传不透明 `customInfo` | 按 `messageID` 去重 |
| B2 | `onReceiveCustomSignal` | 自定义信令 | 新增接收回调，透传发送者信息 | — |
| C1 | `openMLSSubmitCommit` | DS | 改为原子「比较并追加」，返回 4010 冲突契约 | 按 `idempotencyKey` |
| D1 | LiveKit 服务端策略 | SFU | 禁用录制/转码等，撤权与踢人 | — |

---

## 2. E2EE 描述符与能力声明（数据结构）

### 2.1 `e2ee` 初始描述符

单聊与群聊邀请把当前空 `customData` 替换为：

```json
{
  "e2ee": {
    "required": true,
    "scheme": "mls-exporter-livekit-v1",
    "version": 1,
    "conversationID": "si_user_a_user_b",
    "callID": "call_uuid",
    "roomID": "room_uuid",
    "roundID": "round_uuid",
    "targetEpoch": 42,
    "generation": 0,
    "keyIndex": 0,
    "contextHash": "base64url_sha256",
    "expiresAt": 1780000000000
  }
}
```

字段说明：

| 字段 | 类型 | 含义 | 服务端处理 |
|---|---|---|---|
| `required` | bool | 是否强制 E2EE | 校验：`true` 时执行门禁 |
| `scheme` | string | 加密方案标识 | 与服务端允许列表比对 |
| `version` | int | 协议版本 | 与最低版本比对 |
| `conversationID` | string | 会话 ID（单聊 `si_...`，群聊 groupID） | **必须显式回传** |
| `callID` | string | 本次通话 ID | 原样保存 |
| `roomID` | string | LiveKit 房间 ID | 原样保存，作为路由键 |
| `roundID` | string | 本轮协商随机 ID | 原样保存，防重放 |
| `targetEpoch` | int | 目标 MLS epoch | 原样保存 |
| `generation` | int | 密钥代次（单调递增） | 原样保存 |
| `keyIndex` | int | 密钥环索引 | 原样保存 |
| `contextHash` | string | 上下文 SHA256（base64url） | 原样保存 |
| `expiresAt` | int(ms) | 描述符过期时间戳 | 可用于清理，不做密钥判断 |

**关键约束：** 该字段**不得包含** `K_media`、MLS 导出器秘密值、签名私钥或任何可用于恢复密钥的材料。服务端只做**原样保存与返回**，不解析语义、不校验密码学正确性（该责任在客户端）。

### 2.2 `e2eeCapability` 能力声明

邀请、接听、加入和 Token 请求增加：

```json
{
  "e2eeCapability": {
    "schemes": ["mls-exporter-livekit-v1"],
    "frameCryptor": true,
    "maxKeyRingSize": 16,
    "platform": "ios",
    "clientVersion": "1.0.0+6"
  }
}
```

| 字段 | 类型 | 含义 |
|---|---|---|
| `schemes` | string[] | 客户端支持的加密方案列表 |
| `frameCryptor` | bool | 是否支持帧级加解密 |
| `maxKeyRingSize` | int | 密钥环最大容量 |
| `platform` | string | 平台标识（ios/android） |
| `clientVersion` | string | 客户端版本 |

---

## 3. 信令接口详细契约（A1–A8）

### 3.1 描述符透传要求（A1–A8 通用）

以下接口**必须原样保存并返回描述符**，且响应必须显式返回规范化 `conversationID`：

- `signalingInvite`（单聊邀请）
- `signalingInviteInGroup`（群聊邀请）
- `signalingAccept`（接听）
- `signalingJoin`（群通话加入）
- `signalingGetTokenByRoomID`（按房间取 Token）
- `signalingGetRoomByGroupID`（按群取房间）
- `signalingGetInvitationInfoStartApp`（应用启动恢复）
- 离线推送恢复

**写路径要求：**

1. 邀请创建时把 `e2ee` 描述符与 `e2eeCapability` 与房间元数据一起持久化到房间/邀请记录（作为不透明 blob，**不入敏感字段索引**）。
2. 幂等：相同 `callID`/`roomID` 的重复邀请必须返回同一描述符，不得生成新 `roundID`/`generation`。
3. 读路径：接听、加入、取房间、启动恢复、推送恢复必须回传**与写入完全一致**的描述符字节。

### 3.2 `conversationID` 规范化

- 单聊：返回稳定的 `si_...` 形式 `conversationID`（如 `si_user_a_user_b`），由用户对按固定规则排序生成，**服务端负责返回，客户端不得只凭对端 ID 猜测**。
- 群聊：当前可继续使用 `groupID`，但**仍须在协议中显式返回** `conversationID` 字段。

### 3.3 请求/响应示例（以 `signalingAccept` 为例）

请求：

```json
{
  "operationID": "op_uuid",
  "invitation": {
    "roomID": "room_uuid",
    "inviterUserID": "user_a",
    "inviteeUserIDList": ["user_b"],
    "customData": "{\"e2ee\":{...}}"
  },
  "e2eeCapability": {
    "schemes": ["mls-exporter-livekit-v1"],
    "frameCryptor": true,
    "maxKeyRingSize": 16,
    "platform": "ios",
    "clientVersion": "1.0.0+6"
  }
}
```

成功响应：

```json
{
  "errCode": 0,
  "data": {
    "roomID": "room_uuid",
    "conversationID": "si_user_a_user_b",
    "e2ee": { "...": "原样回传描述符" }
  }
}
```

---

## 4. 能力声明与 Token 门禁（A5）

### 4.1 门禁规则（`signalingGetTokenByRoomID`）

服务端在发放 LiveKit JWT 前，必须按顺序校验：

1. **能力/版本校验：** `e2ee.required == true` 且（`e2eeCapability` 缺失，或 `scheme`/`version` 不在服务端允许集合内、低于最低版本）时，**拒绝发放 Token**。
2. **单聊成员校验：** 1 对 1 通话**仅邀请中的两名用户**可获取 Token。
3. **群通话成员校验：** 群通话**仅当前 OpenIM 群成员**可获取 Token（实时查询成员表，不使用缓存做权威判断）。
4. **JWT 元数据：** 可携带**非秘密**的 `e2ee=true` 元数据，**不得包含密钥**。
5. **短 TTL：** LiveKit JWT 为无状态令牌，服务端无法主动吊销已签发的 JWT。因此发放时必须设置**短 TTL**（建议 `ttl` ≤ 5 分钟），使被移除成员的 Token 快速自然失效；房间内活跃客户端通过短周期重新取 Token 续期保持在线。

> 说明：JWT 的 `ttl` 只控制「加入房间」的准入有效期，不影响已建立连接的持续时长；踢出已在房间内的成员必须依赖第 7 节的 `RemoveParticipant`。

### 4.2 群成员移除的撤权机制（贴合 LiveKit 无状态 JWT）

LiveKit JWT 无状态、不可主动吊销，因此**不采用 JWT 黑名单**方案，而是用「主动踢人 + 短 TTL 自然失效 + 换钥」三重机制。群成员被移除时，服务端必须联动：

1. **主动踢出房间（实时生效，权威手段）：** 调用 LiveKit 服务端 API `RemoveParticipant(room, identity)` 立即断开该成员在房间内的连接。这是**唯一能实时切断已建立连接**的手段。
2. **短 TTL 自然失效（阻止重新加入）：** 该成员手上已签发的 JWT 因短 TTL（≤ 5 分钟）很快过期；同时服务端在成员表中移除其身份，后续 `signalingGetTokenByRoomID` 请求按 4.1 的成员校验**直接拒绝**，无法再取到新 Token 重新加入。
3. **触发换钥（媒体层失效）：** 通知剩余客户端执行换钥（`generation` 递增），旧 `K_media` 失效。即使被移除者在被踢前抓取过密文帧，也无法解出换钥后的媒体。

> 三重机制与 MLS 进入新 epoch 共同保证被移除成员无法继续访问媒体：
> - **连接层**：`RemoveParticipant` 实时断连；
> - **准入层**：成员校验拒发 + JWT 短 TTL 过期；
> - **媒体层**：换钥 + MLS 新 epoch 使旧密钥不可用。

> 权衡（tradeoff）：短 TTL 会增加 Token 续期请求 QPS（活跃参会者每 ≤5 分钟续期一次）。若并发通话量大，可将 TTL 设为 5–10 分钟并配合 `RemoveParticipant` 兜底；核心实时撤权始终依赖踢人，而非缩短 TTL。

### 4.3 错误码

```text
CALL_E2EE_REQUIRED_UNSUPPORTED        // 强制E2EE但客户端不支持
CALL_E2EE_CONVERSATION_NOT_READY      // 会话/MLS未就绪
CALL_E2EE_GROUP_MEMBERSHIP_INVALID    // 群成员身份非法
CALL_E2EE_PROTOCOL_VERSION_MISMATCH   // 协议版本不匹配
CALL_E2EE_TOKEN_DENIED                // Token发放被拒
```

错误响应示例：

```json
{
  "errCode": 4030,
  "errMsg": "CALL_E2EE_REQUIRED_UNSUPPORTED",
  "data": { "minClientVersion": "1.0.0+6", "requiredSchemes": ["mls-exporter-livekit-v1"] }
}
```

> 客户端收到门禁错误后必须展示确定的升级/安全提示，**不提供降级操作**。

---

## 5. 自定义信令桥接（B1、B2）

用于在通话过程中透明转发 MLS 加密的通话控制消息（`key_prepare`/`key_ready`/`key_activate`/`rekey`/`abort` 等），服务端只做转发，不解密。

### 5.1 发送接口 `signalingSendCustomSignal`（B1）

Dart 管理器签名：

```dart
Future<String?> signalingSendCustomSignal({
  required String roomID,
  required Map<String, dynamic> customInfo,
  String? operationID,
});
```

发送载荷：

```json
{
  "roomID": "room_uuid",
  "customInfo": {
    "kind": "call_e2ee_control",
    "version": 1,
    "conversationID": "si_user_a_user_b",
    "messageID": "message_uuid",
    "mlsEpoch": 42,
    "mlsMessage": "BASE64_MLS_APPLICATION_CIPHERTEXT"
  }
}
```

### 5.2 接收回调 `onReceiveCustomSignal`（B2）

Dart 监听器签名：

```dart
Function(String data)? onReceiveCustomSignal;
```

接收载荷（服务端补充发送者信息与服务端序列号）：

```json
{
  "roomID": "room_uuid",
  "senderUserID": "user_a",
  "senderPlatformID": 2,
  "serverSeq": 103,
  "customInfo": {
    "kind": "call_e2ee_control",
    "version": 1,
    "conversationID": "si_user_a_user_b",
    "messageID": "message_uuid",
    "mlsEpoch": 42,
    "mlsMessage": "BASE64_MLS_APPLICATION_CIPHERTEXT"
  }
}
```

字段要求：完整保留 `senderUserID`、`senderPlatformID`、`roomID`、`serverSeq`、`messageID` 与不透明 `customInfo`；`mlsMessage` 原样透传。

### 5.3 服务端校验规则（写路径）

| 校验项 | 规则 | 失败处理 |
|---|---|---|
| 发送者成员身份 | 必须是当前房间合法成员 | 拒绝，不转发 |
| 房间生命周期 | 房间必须存在且未结束 | 拒绝，不转发 |
| 载荷大小 | 单条 `customInfo` ≤ 16 KB | 拒绝 |
| 速率限制 | 每用户每房间 10 条/秒，突发 20 条 | 限流丢弃/拒绝 |
| `messageID` 去重 | 服务端按 `messageID` 去重 | 丢弃重复 |
| `mlsMessage` | 服务端**不解密** | 透明转发 |

> 未知或超长的自定义信令必须被策略拒绝，不进入应用回调。客户端侧还会二次校验 `messageID`、`generation`、控制消息类型与已验证发送者。

### 5.4 桥接链路改造清单

- **OpenIM Go Core：** 新增或恢复 `OnReceiveCustomSignal`，处理 `CustomSignalNotification`；重新生成 gomobile 产物（Android jar/so、iOS xcframework）。
- **Android：** `SignalingManager.java` 发送方法、`OnSignalingListener.java` 接收回调。
- **iOS：** `SignalingManager.swift` 发送方法、`SignalingListener` 接收回调（私有头文件已导出 `Open_im_sdkSignalingSendCustomSignal`）。
- **Dart：** `im_signaling_manager.dart`、`signaling_listener.dart`、`im_manager.dart` 事件分发。

### 5.5 短期兼容路径（可选降级实现）

若私有 OpenIMCore 暂时无法补充接收回调，短期可用 OpenIM 隐藏 `CustomMessage` 承载 MLS 密文，但必须满足：

- 使用严格模式通话控制 API；
- 从界面/会话列表/通知中过滤；
- 禁用 `localEx` 明文缓存；
- 不把原始密钥放进消息。

---

## 6. 原子化 OpenMLS Commit 契约（C1）

`openMLSSubmitCommit` 调整为原子的「比较并追加（compare-and-append）」操作，保证本端 epoch 前进与 DS 状态一致。

### 6.1 请求

```json
{
  "groupID": "group_123",
  "fromEpoch": 41,
  "commitMessage": "BASE64",
  "commitHash": "SHA256_BASE64URL",
  "idempotencyKey": "UUID",
  "welcomeMessages": []
}
```

| 字段 | 类型 | 含义 |
|---|---|---|
| `groupID` | string | MLS 群 ID |
| `fromEpoch` | int | 客户端提交时的源 epoch |
| `commitMessage` | string(base64) | Commit 密文 |
| `commitHash` | string | Commit 哈希（去重/校验） |
| `idempotencyKey` | string(UUID) | 幂等键 |
| `welcomeMessages` | array | 随 Commit 分发的 Welcome |

### 6.2 成功响应

```json
{
  "accepted": true,
  "duplicate": false,
  "fromEpoch": 41,
  "acceptedEpoch": 42,
  "commitID": "commit_uuid"
}
```

- 当 `idempotencyKey` 命中历史记录时返回 `duplicate: true`，并回传原始 `acceptedEpoch`/`commitID`。

### 6.3 冲突响应（epoch 冲突）

```json
{
  "errCode": 4010,
  "errMsg": "epoch conflict",
  "data": { "expectedFromEpoch": 43 }
}
```

### 6.4 服务端语义要求

1. 仅当 `fromEpoch` 等于服务端当前 epoch 时才接受 Commit，接受后 epoch 前进为 `acceptedEpoch = fromEpoch + 1`（原子操作，需并发锁或 CAS）。
2. `fromEpoch` 落后时返回 `4010`，并在 `expectedFromEpoch` 告知客户端应追赶到的 epoch。
3. 按 `idempotencyKey` 幂等；重复提交返回 `duplicate: true`，**不重复推进 epoch**。
4. 保留 Commit 历史，支持客户端「DS 成功但本地合并失败」时从历史恢复。

### 6.5 客户端配套顺序（供服务端理解约束）

1. OpenMLS 生成待处理 Commit；
2. DS 原子接受 Commit；
3. DS 成功后 `mergePendingCommit()`；
4. DS 失败时 `clearPendingCommit()`；
5. 收到 `4010` 时先追赶同步，再**重新生成** Commit（禁止重发旧 Commit）；
6. DS 成功但本地合并失败时，从 DS Commit 历史恢复。

---

## 7. LiveKit 服务端策略（D1）

1. **只转发** FrameCryptor 密文，不解析媒体内容。
2. E2EE 房间**禁用** Egress、录制、转码、混流、转写、SIP/PSTN 和服务端代理。
3. JWT、房间元数据、Webhook、日志中**不得存放**媒体密钥。
4. 允许采集连接、带宽、丢包、重连指标，但不得解析媒体内容。
5. 群成员移除时执行 `RemoveParticipant` 踢人（实时断连的权威手段），并配合成员表移除后的拒发 Token、JWT 短 TTL 自然失效与客户端换钥（见 4.2）。
6. Token 发放使用短 TTL（≤ 5 分钟）以适配无状态 JWT 不可主动吊销的限制；活跃参会者短周期续期。
7. 若未来必须录制，须创建明确的**非 E2EE 房间**；**禁止**在同一房间内静默关闭 E2EE。

---

## 8. 实施方案（服务端落地步骤）

> 对应实施计划「任务 6：实现 OpenIM 服务端能力与房间契约」，在外部 OpenIM 信令服务/API 仓库实施；契约部署后回写 `im_signaling_manager.dart` 的 API 文档。

### 阶段 1：描述符透传与会话规范化

- [ ] 在邀请、接受、加入、取 Token、取房间、启动恢复、推送恢复流程中，原样保存并返回 `e2ee` 描述符（不透明 blob）。
- [ ] 为 1 对 1 与群通话返回规范化 `conversationID`。
- [ ] 保证同一 `callID`/`roomID` 的描述符幂等一致。

### 阶段 2：能力声明与 Token 门禁

- [ ] 在邀请、接受、加入、Token 请求中接收并落库 `e2eeCapability`。
- [ ] 对强制 E2EE 房间，拒绝向不支持/版本不匹配客户端发放 Token。
- [ ] 发放 Token 前校验 1 对 1 被邀请人身份或当前群成员身份。
- [ ] JWT 写入非秘密 `e2ee=true` 元数据，并设置短 TTL（≤ 5 分钟）。
- [ ] 提供 Token 续期路径，供活跃参会者短周期刷新。
- [ ] 落地 5 个错误码。

### 阶段 3：自定义信令桥接

- [ ] Go Core 新增/恢复 `OnReceiveCustomSignal` 与 `CustomSignalNotification`，重生成 Android/iOS 产物。
- [ ] 打通 Android/iOS/Dart 发送与接收链路。
- [ ] 落地授权校验、16 KB 限制、10 条/秒（突发 20）限流、`messageID` 去重、房间生命周期检查。
- [ ] 保证 `mlsMessage` 不被服务端解密、透明转发。

### 阶段 4：原子化 Commit 契约

- [ ] 实现 `openMLSSubmitCommit` 的 CAS 语义与 `4010` 冲突契约。
- [ ] 实现 `idempotencyKey` 幂等与 Commit 历史存储。

### 阶段 5：群成员移除联动与 LiveKit 策略

- [ ] 群成员移除时：从成员表移除（后续拒发 Token）、`RemoveParticipant` 实时踢人、通知剩余客户端换钥；依赖短 TTL 使旧 Token 自然失效（不做 JWT 黑名单）。
- [ ] LiveKit 房间禁用录制/Egress/转码/转写/混流/SIP。

### 阶段 6：可观测性与合规校验

- [ ] 仅采集协商耗时、追赶同步次数、换钥次数、FrameCryptor 状态/错误码、失败阶段、WS 连接数、限流命中数。
- [ ] 审计日志/DB/推送/JWT/Webhook 中确认**无**密钥、密钥承诺值、MLS 明文、`Message` 明文与媒体 URL。

---

## 9. 数据模型（服务端存储）

### 9.1 房间/邀请记录扩展字段

| 字段 | 类型 | 说明 | 索引 |
|---|---|---|---|
| `room_id` | string | 主键/路由键 | 主键 |
| `conversation_id` | string | 规范化会话 ID | 二级索引 |
| `call_id` | string | 通话 ID | 二级索引 |
| `e2ee_descriptor` | blob/text | 不透明描述符 JSON | 不索引内部字段 |
| `e2ee_required` | bool | 是否强制 E2EE | — |
| `created_at` / `expires_at` | int(ms) | 生命周期 | — |

- 分片键建议：`room_id`（通话强关联房间）或 `conversation_id`。

### 9.2 自定义信令去重表

| 字段 | 类型 | 说明 |
|---|---|---|
| `room_id` | string | 房间 ID |
| `message_id` | string | 去重键（与 room_id 联合唯一） |
| `sender_user_id` | string | 发送者 |
| `server_seq` | int | 服务端序列号 |
| `created_at` | int(ms) | 用于 TTL 清理 |

- 去重窗口按通话时长设置 TTL，联合唯一键 `(room_id, message_id)`。

### 9.3 MLS Commit 历史表

| 字段 | 类型 | 说明 |
|---|---|---|
| `group_id` | string | 分片键 |
| `from_epoch` | int | 源 epoch |
| `accepted_epoch` | int | 接受后 epoch |
| `commit_id` | string | Commit ID |
| `commit_hash` | string | 去重/校验 |
| `idempotency_key` | string | 幂等键（唯一） |
| `commit_message` | blob | Commit 密文（不解密） |

- 唯一约束：`(group_id, from_epoch)` 保证同一 epoch 只接受一个 Commit；`idempotency_key` 唯一保证幂等。

---

## 10. 非功能需求与容量评估

### 10.1 SLO 建议

- 信令（邀请/接受/Token）P99 延迟 < 300ms。
- 自定义信令转发 P99 延迟 < 150ms（影响换钥收敛速度）。
- `openMLSSubmitCommit` P99 < 200ms。

### 10.2 容量估算维度

- 通话并发数 × 参会人数 → WS 连接数与 Token QPS。
- 换钥频率（每 15 分钟周期 + epoch 变化）× 参会人数 → 自定义信令 QPS。
- 群规模 × Commit 频率 → DS 写 QPS 与 Commit 历史存储增长。

### 10.3 背压与故障隔离

- 自定义信令超限触发限流（10 条/秒/用户/房间），保护 DS 与转发链路。
- 推送/搜索/审计故障不得影响通话主链路与信令转发。
- Commit 分发采用 CAS + 冲突返回，避免 epoch 分叉。

---

## 11. 服务端验收清单

- [ ] 所有列出的信令接口原样保存并回传描述符，且返回规范化 `conversationID`。
- [ ] 强制 E2EE 房间对不支持/低版本客户端拒发 Token，错误码正确。
- [ ] 1 对 1 与群通话成员门禁生效。
- [ ] 群成员移除触发「踢人（RemoveParticipant）+ 拒发新 Token/短 TTL 失效 + 换钥」三重失效。
- [ ] 自定义信令完成授权、16KB、限流、去重、房间生命周期校验，且不解密 `mlsMessage`。
- [ ] `openMLSSubmitCommit` 原子接受、`4010` 冲突、`idempotencyKey` 幂等全部正确。
- [ ] LiveKit E2EE 房间禁用录制/Egress/转码/转写/混流/SIP。
- [ ] 审计：OpenIM 日志、DB、推送载荷、JWT、Webhook、分析、崩溃报告中均无 `K_media` 及任何可派生密钥材料。
- [ ] 无任何自动或静默回退到非 E2EE 通话的服务端路径。

---

## 12. 参考

- 实施计划：`音视频通话端到端加密（E2EE）实施计划.md`（第 5 章为服务端契约主源）
- 现有信令桥接：`local_plugin/flutter_openim_sdk/lib/src/manager/im_signaling_manager.dart`
- LiveKit 加密概览：<https://docs.livekit.io/transport/encryption.md>
