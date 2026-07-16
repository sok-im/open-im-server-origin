# 音视频通话 E2EE — 客户端改造方案

> 日期：2026-07-16  
> 状态：待客户端实施  
> 依据：`音视频通话端到端加密（E2EE）实施计划.md`、服务端已落地契约（OpenIM Server `lintao1`）  
> 兼容策略：**方案 A** — 仅当邀请 `e2ee.required=true` 时走强制 E2EE；旧客户端仍可发非 E2EE 通话

---

## 0. 与服务端对齐结论

服务端已具备：

| 能力 | 服务端状态 | 客户端必须配合 |
|---|---|---|
| `customData.e2ee` 透传 + 响应 `conversationID` / `e2ee` | 已落地 | 邀请写入描述符；接听/加入/取 Token/恢复时解析服务端回传 |
| `e2eeCapability` 门禁 | 已落地 | Invite/Accept/Join/GetToken 必带能力；处理 1830–1834 |
| 自定义信令转发（16KB / 限流 / messageID 去重） | 已落地 | SDK 贯通 Send + OnReceive；控制消息走 MLS 密文 |
| `openMLSSubmitCommit` CAS + 4010 | 已落地 | Commit：先 DS 再 merge；冲突追赶后重生 Commit |
| 踢人 `RemoveParticipant` + 短 TTL JWT | 已落地 | E2EE 房间短周期续 Token；踢出/退群后触发换钥 |

服务端**不**做：派生/下发 `K_media`、解密 `mlsMessage`、静默降级。

---

## 1. 目标与红线

**目标：** 复用本端 OpenMLS 会话，导出每通电话独立的 32 字节 `K_media`，交给 LiveKit FrameCryptor；OpenIM/LiveKit 只见密文帧与非秘密控制面。

**红线：**

1. 禁止静默降级到非 E2EE（E2EE 房间失败 → 结束通话 + 升级/安全提示）。
2. `K_media`、导出器秘密、密钥承诺值、MLS 控制明文不得进日志 / `localEx` / 推送 / 通话记录。
3. 收到 `key_activate` 之前不得 `room.connect` / 发布轨道。
4. `conversationID` 以服务端回传为准，禁止只凭对端 ID 猜测。

---

## 2. 分层职责（客户端）

| 层 | 模块（建议路径） | 职责 |
|---|---|---|
| SDK 桥 | `flutter_openim_sdk` + Android/iOS + Go Core | 信令字段、`SendCustomSignal`、`OnReceiveCustomSignal`、OpenMLS DS 响应 |
| MLS | `lib/core/e2ee/mls_controller.dart` 等 | Commit 原子性、exportSecret、严格模式控制消息加解密 |
| 通话 E2EE | `lib/im/Call/e2ee/*` | 描述符、协调器状态机、换钥 |
| LiveKit | `lib/im/Call/engine/livekit_*.dart` | KeyProvider + FrameCryptor + VP8 |
| 业务入口 | `call_controller.dart` 等 | 邀请/接听/加入/恢复接线，失败关闭 |

---

## 3. 与服务端契约（客户端必改调用）

### 3.1 邀请 `customData`

当前空字符串改为：

```json
{
  "e2ee": {
    "required": true,
    "scheme": "mls-exporter-livekit-v1",
    "version": 1,
    "conversationID": "<服务端回传或本地预填后以服务端为准>",
    "callID": "<uuid>",
    "roomID": "<邀请后以服务端 roomID 为准>",
    "roundID": "<uuid>",
    "targetEpoch": <本端 MLS epoch>,
    "generation": 0,
    "keyIndex": 0,
    "contextHash": "<base64url>",
    "expiresAt": <ms>
  }
}
```

- 主叫：生成 `callID`/`roundID`/`generation`/`keyIndex`/`contextHash`/`targetEpoch`。
- `roomID`：以 Invite 响应为准回写描述符（若先本地占位，须在收到响应后对齐）。
- `conversationID`：以响应字段为准覆盖本地猜测。

### 3.2 能力声明（所有准入请求）

Invite / Accept / Join / GetTokenByRoomID 增加：

```json
{
  "e2eeCapability": {
    "schemes": ["mls-exporter-livekit-v1"],
    "frameCryptor": true,
    "maxKeyRingSize": 16,
    "platform": "ios|android",
    "clientVersion": "<app version>"
  }
}
```

错误码映射（服务端 18xx）：

| 码 | 名 | UI |
|---|---|---|
| 1830 | `CALL_E2EE_REQUIRED_UNSUPPORTED` | 升级提示，无降级按钮 |
| 1831 | `CALL_E2EE_CONVERSATION_NOT_READY` | MLS/会话未就绪 |
| 1832 | `CALL_E2EE_GROUP_MEMBERSHIP_INVALID` | 无权限 |
| 1833 | `CALL_E2EE_PROTOCOL_VERSION_MISMATCH` | 升级提示；`clientVersion` 缺省也会触发 |
| 1834 | `CALL_E2EE_TOKEN_DENIED` | 无法进入加密通话；含 `userID` 与鉴权身份不一致 |

### 3.3 Token 续期

E2EE 房间 JWT TTL ≤ 5 分钟。活跃通话需周期性 `signalingGetTokenByRoomID`（建议 2–4 分钟），续期时同样带 `e2eeCapability`（含可解析的 `clientVersion`）。

请求体 `userID` 必须等于当前登录用户（服务端比对 JWT → `opUserID`）；勿代他人取 Token，否则返回 **1834**。

### 3.4 自定义信令（换钥控制面）

**Go Core（`openim-sdk-core-origin`）已落地：**

- `SignalingSendCustomSignal` 发送链路（已存在）
- `OnReceiveCustomSignal` 接收链路：`handleCustomSignalNotification` 对非 `groupCallStatus` 的 `CustomSignalNotification`(1605) 原样上抛（`internal/signaling/signaling.go`）；`OnSignalingListener` 新增 `OnReceiveCustomSignal`（`open_im_sdk_callback/callback_client.go`）；WASM/空实现同步（`wasm/event_listener/listener.go`、`open_im_sdk/em.go`）
- **Core 不解密** `mlsMessage`，转发载荷 `{roomID,senderUserID,senderPlatformID,serverSeq,customInfo}`

**仍需（客户端 App / 原生桥）：**

- Dart：`onReceiveCustomSignal` 回调贯通到 App 层
- Android/iOS 桥暴露该回调；重生成 jar/so、xcframework

载荷：`kind=call_e2ee_control`，`mlsMessage` 为 MLS 应用密文；客户端二次校验 `messageID`、`generation`、类型、`verifiedSenderID` vs 外层 `senderUserID`。

单条 ≤ 16KB；勿打明文日志。

短期兜底（仅 Core 未好时）：隐藏 `CustomMessage`，须严格过滤 UI/`localEx`。

### 3.5 OpenMLS Commit

顺序固定：

1. 生成本地 pending Commit  
2. `openMLSSubmitCommit`（带 `fromEpoch` + `idempotencyKey`）  
3. 成功 → `mergePendingCommit()`  
4. 失败 → `clearPendingCommit()`  
5. **4010** → 追赶 `GetCommits` 后**重新生成** Commit（禁止重发旧包）  
6. DS 成功但本地合并失败 → 从 Commit 历史恢复  

> 注意：服务端冲突时 `expectedFromEpoch` 目前可能在 errMsg 中（`expectedFromEpoch=N`）；客户端解析需兼容 message 与未来结构化 `data`。

---

## 4. 客户端实施任务清单（按依赖排序）

### P0 — MLS 基础安全（上线通话 E2EE 前置）

| # | 内容 | 关键文件 |
|---|---|---|
| C1 | 严格模式：加密失败禁止回退明文；发送者校验 | `e2ee_adapter` / `e2ee_message_resolver` |
| C2 | Commit：DS 成功后再 merge；4010 处理；logout 重置引擎 | `mls_controller` |
| C3 | 去掉明文 Message/URL/JWT/密钥日志 | 全 E2EE/Call 路径 |

### P1 — SDK / 协议桥

> Go Core（`openim-sdk-core-origin`）侧的 C4–C6 已落地（见 §9）：protocol 已同步 E2EE 字段，`call()` 导出层对请求/响应 E2EE 字段与 SubmitCommit 新契约透明透传，`OnReceiveCustomSignal` 已贯通。**剩余为 Dart/原生桥 + 产物重生成。**

| # | 内容 | 关键文件 | Go Core | Dart/原生 |
|---|---|---|---|---|
| C4 | 信令请求/响应模型增加 `e2eeCapability`、`conversationID`、`e2ee` | `im_signaling_manager` + 原生 | ✅ 透传 | ⬜ 模型字段 |
| C5 | `SendCustomSignal` / `OnReceiveCustomSignal` 全链路 | SDK + Go Core 产物 | ✅ | ⬜ 桥+产物 |
| C6 | OpenMLS SubmitCommit 解析 `accepted/duplicate/4010` | `im_openmls_manager` | ✅ 透传+errCode | ⬜ 解析层 |

### P2 — 通话 E2EE 核心

| # | 内容 | 关键文件 |
|---|---|---|
| C7 | 模型/编解码：`CallEncryptionDescriptor`、能力、控制类型 | `lib/im/Call/e2ee/call_e2ee_*.dart` |
| C8 | `exportCallMediaKey` + 严格控制消息 API | `mls_call_control` / `mls_controller` |
| C9 | `CallE2EECoordinator`：`key_prepare/ready/activate`、epoch 屏障、换钥 | `call_e2ee_coordinator.dart` |
| C10 | LiveKit：严格 KeyProvider、`RoomOptions.encryption`、强制 VP8 | `livekit_engine.dart` |
| C11 | 接线：主叫/被叫/群加入/启动恢复；Token 带能力；activate 前不连房 | `call_controller` / `call_token_provider` |
| C12 | 运行时换钥 + 失败关闭（5s 未恢复挂断；旧钥保留 ≥30s） | coordinator + engine |

### P3 — 验证与灰度

| # | 内容 |
|---|---|
| C13 | 自动化 + 真机矩阵（1v1/群/踢人/周期换钥/弱网/进程恢复/旧客户端拒 Token） |
| C14 | Feature flag：内测账号 → 1v1 音频 → 视频 → 群通话 |

---

## 5. 关键数据流（客户端视角）

```text
主叫:
  MLS ready → 生成描述符 + capability
  → signalingInvite(customData, e2eeCapability)
  → 使用响应 conversationID/roomID
  → Coordinator: key_prepare → export → key_ready 收集
  → key_activate → LiveKit connect(加密) → 发布轨道
  → 周期 GetToken 续期；CustomSignal 换钥

被叫/加入:
  收到邀请/描述符 → 校验 required/scheme/version/过期
  → Accept/Join + capability
  → 解析响应 e2ee → epoch 追赶 → 同协调流程
  → 仅 activate 后进房

踢人/退群:
  服务端 RemoveParticipant + MLS Remove trigger
  → 剩余端 epoch 变化 → 换钥；被踢端不得再取 Token
```

密钥派生（固定）：

```text
label = "sok-im/call-media-e2ee/v1"
K_media = MLS-Exporter(label, canonicalContext, 32)
context 绑定: protocolVersion, conversationID, callID, roomID,
             roundID, targetEpoch, generation, keyIndex
```

---

## 6. 建议实施顺序（工期视角）

1. **C1–C3**（不改通话 UI 也能先修 MLS 安全债）  
2. **C4–C6**（对齐已部署服务端协议；否则无法联调）  
3. **C7–C10**（可并行：模型/导出器 vs LiveKit）  
4. **C11–C12**（业务接线）  
5. **C13–C14**（联调与灰度）

联调前置：**服务端已就绪**；客户端至少完成 C4+C5+C9+C10+C11 最小闭环。

---

## 7. 验收标准（客户端）

- E2EE 通话每条发布轨道均已配置加密；无明文媒体路径。
- 能力不足/Token 拒绝：仅升级提示，无「继续普通通话」。
- 密钥从不落盘；进程恢复只恢复描述符并重新派生。
- 群成员被踢：本端断连；其余成员完成换钥。
- 现有占线/铃声/导航/重连回归不破。

---

## 8. 参考

- 总计划：`音视频通话端到端加密（E2EE）实施计划.md`（任务 1–5、7–12；任务 6 服务端已完成）
- 服务端设计：`docs/superpowers/specs/2026-07-16-call-e2ee-server-design.md`
- 服务端接口：`音视频通话E2EE-服务端接口与实施方案.md`

---

## 9. Go Core（`openim-sdk-core-origin`）落地记录（2026-07-16）

仓库：`openim-sdk-core-origin`（与本仓库同级）。标准构建与 `GOOS=js GOARCH=wasm` 构建均通过。

### 9.1 Protocol 同步（t1）

SDK 通过 `replace github.com/openimsdk/protocol => ./protocol` vendored 一份 protocol。已用服务端同版本生成物（protoc-gen-go v1.36.11）整体覆盖：

- `protocol/rtc/{rtc.proto,rtc.pb.go,rtc_grpc.pb.go}`
- `protocol/openmls/{openmls.proto,openmls.pb.go,openmls_grpc.pb.go}`

新增：`E2EECapability`、请求 `e2eeCapability`、响应 `conversationID`/`e2ee`、`SignalRemoveParticipants`，以及 SubmitCommit 的 `idempotencyKey`/`accepted`/`duplicate`/`acceptedEpoch`/`expectedFromEpoch`。

### 9.2 `OnReceiveCustomSignal`（t2，真实改动）

原 `handleCustomSignalNotification` 把非 `groupCallStatus` 的 1605 通知**直接丢弃**；现改为原样上抛：

| 文件 | 改动 |
|---|---|
| `internal/signaling/signaling.go` | 非 `groupCallStatus` 载荷 → `listener.OnReceiveCustomSignal(string(msg.Content))`，Core 不解密 |
| `open_im_sdk_callback/callback_client.go` | `OnSignalingListener` 新增 `OnReceiveCustomSignal` |
| `wasm/event_listener/listener.go` | `SignalingCallback.OnReceiveCustomSignal` |
| `open_im_sdk/em.go` | `emptySignalingListener.OnReceiveCustomSignal` 兜底 |

### 9.3 信令 / Commit 透传（t3、t4）

导出层 `call()` 反序列化 typed 请求、全量 JSON 返回 typed 响应，protocol 同步后即透传，无需额外 Go 代码：

- Invite/Accept/Join/GetToken 的 JSON 带 `e2eeCapability` → 自动进入请求；响应 `conversationID`/`e2ee` 回传 App
- SubmitCommit 成功回传 `accepted`/`duplicate`/`acceptedEpoch`；**4010** 经 `ApiPost` → `sdkerrs.New(4010,…)`，errCode 与 `expectedFromEpoch=N`（errMsg）传到 App

### 9.4 仍需（不在 Core）

- Dart/原生桥暴露 `OnReceiveCustomSignal`；重生成 `.aar`/`.so`/`xcframework`/wasm 产物
- App 层 C1–C3、C7–C14（描述符、Coordinator、导出器、LiveKit KeyProvider/FrameCryptor、短 TTL 续 Token）
