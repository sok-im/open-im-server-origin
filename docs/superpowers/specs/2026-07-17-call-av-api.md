# 音视频通话接口 — 请求与响应

> 日期：2026-07-17  
> 依据：`protocol/rtc/rtc.proto`、`protocol/openmls/openmls.proto`、`internal/api/router.go`  
> 兼容策略：仅当邀请 `customData.e2ee.required=true` 时走强制 E2EE；未声明则行为与旧版一致

## 0. 通用约定

| 项 | 说明 |
|---|---|
| Base | HTTP API 前缀以部署为准，下文路径相对 API Gateway |
| 鉴权 | 需登录 Token；`opUserID` 取自 JWT |
| Content-Type | `application/json` |
| 统一响应壳 | `{ "errCode": 0, "errMsg": "", "errDlt": "", "data": { ... } }`；业务字段在 `data` 内 |
| `userID`（E2EE） | 请求体 `userID` 必须等于当前登录用户，否则发 Token 返回 **1834** |
| `conversationID` | 以服务端回传为准：单聊 `si_<sorted_a>_<sorted_b>`，群聊 = `groupID` |

---

## 1. HTTP 路由一览

### 1.1 RTC 信令 `/rtc`

| Method | Path | 说明 |
|---|---|---|
| POST | `/rtc/signal_message_assemble` | 统一信令入口（invite/accept/join/cancel/…） |
| POST | `/rtc/signal_get_token_by_room_id` | 按 roomID 取 LiveKit Token（续期） |
| POST | `/rtc/signal_get_room_by_group_id` | 查群进行中通话 |
| POST | `/rtc/signal_get_rooms` | 批量查房间 |
| POST | `/rtc/get_signal_invitation_info` | 按 roomID 查邀请 |
| POST | `/rtc/get_signal_invitation_info_start_app` | App 启动恢复待接听邀请 |
| POST | `/rtc/is_call_ended_by_room_id` | 通话是否已结束 |
| POST | `/rtc/signal_send_custom_signal` | 自定义信令（E2EE 换钥控制面） |
| POST | `/rtc/signal_notify_group_call_ended` | 群通话结束通知 |
| POST | `/rtc/get_signal_invitation_records` | 历史通话记录（含文件类） |
| POST | `/rtc/delete_signal_records` | 删除历史记录 |

### 1.2 OpenMLS（通话 E2EE 依赖）`/openmls/v1`

| Method | Path | 说明 |
|---|---|---|
| POST | `/openmls/v1/key_packages/upload` | 上传 KeyPackage |
| POST | `/openmls/v1/key_packages/fetch` | 拉取并消费 KeyPackage |
| POST | `/openmls/v1/key_packages/count` | 统计未消费 KP |
| POST | `/openmls/v1/key_packages/refresh` | 刷新 KP |
| POST | `/openmls/v1/groups/commit` | SubmitCommit（原子 CAS） |
| POST | `/openmls/v1/groups/commits` | 追赶 Commit 历史（需群成员） |
| POST | `/openmls/v1/groups/welcome` | 投递 Welcome |
| POST | `/openmls/v1/groups/state` | 查 MLS 组状态（需群成员） |
| POST | `/openmls/v1/groups/delete` | 删除 MLS 组 |

---

## 2. 公共结构

### 2.1 `E2EECapability`

强制 E2EE 房间发 Token 前校验；缺能力 / 无 FrameCryptor → **1830**；scheme/version 不匹配或缺省不可解析 → **1833**。

```json
{
  "schemes": ["mls-exporter-livekit-v1"],
  "frameCryptor": true,
  "maxKeyRingSize": 16,
  "platform": "ios",
  "clientVersion": "1.0.0"
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| schemes | string[] | 是 | 客户端支持的 E2EE 方案；须与服务端 allow-list 有交集 |
| frameCryptor | bool | 是 | 必须为 `true` |
| maxKeyRingSize | int32 | 否 | 密钥环容量提示 |
| platform | string | 否 | `ios` / `android` / … |
| clientVersion | string | 条件 | 服务端 `e2ee.minVersion≥1` 时必须可解析（如 `"1"` / `"1.2.0"`） |

### 2.2 `InvitationInfo`

```json
{
  "inviterUserID": "u1",
  "inviteeUserIDList": ["u2"],
  "customData": "{\"e2ee\":{...}}",
  "groupID": "",
  "roomID": "",
  "timeout": 30,
  "mediaType": "video",
  "platformID": 1,
  "sessionType": 1,
  "initiateTime": 0,
  "busyLineUserIDList": [],
  "callerRingtoneURL": "",
  "notAllowUserIDList": [],
  "conversationID": ""
}
```

| 字段 | 说明 |
|---|---|
| customData | 建议 JSON 字符串；E2EE 时内嵌 `e2ee` 描述符（见 §2.3） |
| roomID | 邀请时可由服务端生成；后续以服务端回传为准 |
| conversationID | **响应侧**由服务端规范化回传；请求侧可预填，以响应为准 |
| mediaType | `audio` / `video` |
| sessionType | 单聊 / 群聊（与 OpenIM 会话类型一致） |

### 2.3 `customData.e2ee` 描述符（客户端写入，服务端透传）

服务端只解析非秘密字段 `required` / `callID` 用于门禁与存储；其余原样保存在 `customData`，并在 Accept/Join/GetToken/GetRoom 等响应的 `e2ee` 字段原样回传。

```json
{
  "e2ee": {
    "required": true,
    "scheme": "mls-exporter-livekit-v1",
    "version": 1,
    "conversationID": "si_a_b",
    "callID": "<uuid>",
    "roomID": "<服务端 roomID>",
    "roundID": "<uuid>",
    "targetEpoch": 3,
    "generation": 0,
    "keyIndex": 0,
    "contextHash": "<base64url>",
    "expiresAt": 1720000000000
  }
}
```

**红线：** 描述符与请求/响应中**不得**出现 `K_media`、exporter secret、MLS 控制明文。

### 2.4 LiveKit Token attributes（E2EE）

E2EE 房间签发的 JWT `attributes`（非秘密）：

| Key | 说明 |
|---|---|
| `e2ee` | `"true"` |
| `e2eeRoomID` | 房间 ID |
| `e2eeConversationID` | 规范化会话 ID |
| `e2eeCallID` | 通话 ID |
| `e2eeScheme` | 协商命中的 scheme |
| `e2eeVersion` | 客户端声明的 `clientVersion` |

TTL ≤ 5 分钟；活跃通话建议 2–4 分钟续期。客户端连房前应核对 attributes 与本地会话一致。

---

## 3. 统一信令入口

### `POST /rtc/signal_message_assemble`

请求体用 `signalReq` 的 **oneof** 选择一种操作；响应在 `signalResp` 对应分支。

```json
{
  "signalReq": {
    "invite": { "...": "见 §3.1" }
  }
}
```

| oneof 字段 | 操作 |
|---|---|
| invite | 1v1 邀请 |
| inviteInGroup | 群邀请 |
| cancel | 取消 |
| accept | 接听 |
| hungUp | 挂断 |
| reject | 拒绝 |
| getTokenByRoomID | 取 Token（也可走独立 HTTP） |
| timeout | 超时未接 |
| join | 群成员中途加入 |
| heartbeat | 通话心跳（刷新忙线状态） |

---

### 3.1 邀请（1v1）— `invite`

**请求 `SignalInviteReq`**

```json
{
  "invitation": {
    "inviterUserID": "u1",
    "inviteeUserIDList": ["u2"],
    "customData": "{\"e2ee\":{\"required\":true,\"callID\":\"...\",\"scheme\":\"mls-exporter-livekit-v1\",\"version\":1}}",
    "timeout": 30,
    "mediaType": "video",
    "platformID": 1,
    "sessionType": 1
  },
  "offlinePushInfo": { "title": "来电", "desc": "视频通话", "ex": "" },
  "participant": {},
  "userID": "u1",
  "e2eeCapability": {
    "schemes": ["mls-exporter-livekit-v1"],
    "frameCryptor": true,
    "clientVersion": "1.0.0",
    "platform": "android"
  }
}
```

**响应 `SignalInviteResp`**

```json
{
  "token": "<livekit-jwt>",
  "roomID": "r_xxx",
  "liveURL": "wss://livekit.example",
  "busyLineUserIDList": [],
  "calleeRingtoneURL": "",
  "notAllowUserIDList": [],
  "callerRingtoneURL": "",
  "conversationID": "si_u1_u2"
}
```

| 响应字段 | 说明 |
|---|---|
| token | LiveKit JWT；E2EE 时已做能力门禁与身份校验 |
| roomID | 服务端房间 ID，后续一律以此为准 |
| liveURL | LiveKit 地址 |
| conversationID | 规范化会话 ID |
| busyLineUserIDList | 忙线用户 |
| notAllowUserIDList | 因接听设置不可邀请的用户 |

---

### 3.2 群邀请 — `inviteInGroup`

**请求** 同 3.1，但 `invitation.groupID` 必填，`inviteeUserIDList` 为被邀请成员。

**响应 `SignalInviteInGroupResp`**

```json
{
  "token": "<jwt>",
  "roomID": "r_xxx",
  "liveURL": "wss://...",
  "busyLineUserIDList": [],
  "calleeRingtoneURL": "",
  "notAllowUserIDList": [],
  "conversationID": "<groupID>"
}
```

---

### 3.3 接听 — `accept`

**请求 `SignalAcceptReq`**

```json
{
  "invitation": {
    "roomID": "r_xxx",
    "inviterUserID": "u1",
    "inviteeUserIDList": ["u2"],
    "groupID": "",
    "mediaType": "video",
    "sessionType": 1
  },
  "offlinePushInfo": {},
  "participant": {},
  "opUserPlatformID": 2,
  "userID": "u2",
  "e2eeCapability": {
    "schemes": ["mls-exporter-livekit-v1"],
    "frameCryptor": true,
    "clientVersion": "1.0.0"
  }
}
```

**响应 `SignalAcceptResp`**

```json
{
  "token": "<jwt>",
  "roomID": "r_xxx",
  "liveURL": "wss://...",
  "conversationID": "si_u1_u2",
  "e2ee": "{\"required\":true,\"callID\":\"...\",\"scheme\":\"mls-exporter-livekit-v1\",\"version\":1}"
}
```

| 字段 | 说明 |
|---|---|
| e2ee | 邀请时保存的 E2EE 描述符 JSON **原样**回传（字符串） |
| conversationID | 规范化会话 ID |

---

### 3.4 群加入 — `join`

**请求 `SignalJoinReq`**

```json
{
  "invitation": {
    "roomID": "r_xxx",
    "groupID": "g1",
    "mediaType": "video",
    "sessionType": 3
  },
  "participant": {},
  "opUserPlatformID": 1,
  "userID": "u3",
  "e2eeCapability": {
    "schemes": ["mls-exporter-livekit-v1"],
    "frameCryptor": true,
    "clientVersion": "1.0.0"
  }
}
```

**响应 `SignalJoinResp`**

```json
{
  "token": "<jwt>",
  "roomID": "r_xxx",
  "liveURL": "wss://...",
  "participant": [],
  "inCall": true,
  "conversationID": "g1",
  "e2ee": "{...}"
}
```

---

### 3.5 取消 / 拒绝 / 挂断 / 超时 / 心跳

| 操作 | 请求要点 | 响应 |
|---|---|---|
| cancel | `invitation` + `userID` | 空对象 |
| reject | `invitation` + `userID` + `opUserPlatformID` | 空对象 |
| hungUp | `invitation` + `userID`；可选 `callDuration`（秒，1v1） | 空对象 |
| timeout | 主叫超时未接：`invitation` + `userID` | 空对象 |
| heartbeat | `{ "userID", "roomID" }` | 空对象 |

---

## 4. 独立 HTTP 接口

### 4.1 按房间取 Token（续期）

`POST /rtc/signal_get_token_by_room_id`

**请求**

```json
{
  "roomID": "r_xxx",
  "userID": "u1",
  "participant": {},
  "e2eeCapability": {
    "schemes": ["mls-exporter-livekit-v1"],
    "frameCryptor": true,
    "clientVersion": "1.0.0"
  }
}
```

**响应**

```json
{
  "token": "<jwt>",
  "liveURL": "wss://...",
  "conversationID": "si_u1_u2",
  "e2ee": "{...}"
}
```

**E2EE 门禁：** 房间 `e2ee.required=true` 时校验身份（`userID==opUserID`）、成员资格、`e2eeCapability`；失败见 §7。

---

### 4.2 按群查进行中房间

`POST /rtc/signal_get_room_by_group_id`

**请求：** `{ "groupID": "g1" }`

**响应**

```json
{
  "invitation": { "...": "InvitationInfo，含 customData / conversationID" },
  "participant": [],
  "roomID": "r_xxx",
  "inCall": true,
  "conversationID": "g1",
  "e2ee": "{...}"
}
```

无进行中通话时：`inCall=false`，其余可空。

---

### 4.3 批量查房间

`POST /rtc/signal_get_rooms`

**请求：** `{ "roomIDs": ["r1", "r2"] }`  
**响应：** `{ "roomList": [ /* SignalGetRoomByGroupIDResp */ ] }`

---

### 4.4 查邀请 / 启动恢复 / 是否结束

| Path | 请求 | 响应要点 |
|---|---|---|
| `/rtc/get_signal_invitation_info` | `{ "roomID" }` | `invitationInfo` + `offlinePushInfo` |
| `/rtc/get_signal_invitation_info_start_app` | `{ "userID" }` | `invitation` + `offlinePushInfo` + **`conversationID`** |
| `/rtc/is_call_ended_by_room_id` | `{ "roomID" }` | `{ "isEnded": true/false }` |

---

### 4.5 自定义信令（换钥控制面）

`POST /rtc/signal_send_custom_signal`

**请求**

```json
{
  "roomID": "r_xxx",
  "customInfo": "{\"messageID\":\"msg-1\",\"kind\":\"call_e2ee_control\",\"mlsMessage\":\"<base64>\"}"
}
```

| 约束 | 说明 |
|---|---|
| 大小 | `customInfo` ≤ **16KB** |
| 授权 | 发送者须为房间参与者（邀请方/被邀方，或当前群成员） |
| 限流 | 每用户每房间约 20 条/秒（Redis 原子计数） |
| 去重 | 若 `customInfo` JSON 含 `messageID`，同房间 6h 内重复则静默跳过 |
| 生命周期 | 房间邀请不存在/已结束 → 失败 |

**响应：** `{}`（空）

**接收侧推送（WS 通知 `CustomSignalNotification` / 1605）外层信封：**

```json
{
  "roomID": "r_xxx",
  "senderUserID": "u1",
  "senderPlatformID": 1,
  "serverSeq": 42,
  "messageID": "msg-1",
  "customInfo": "{\"messageID\":\"msg-1\",\"kind\":\"call_e2ee_control\",...}"
}
```

| 字段 | 说明 |
|---|---|
| messageID | 顶层暴露，便于去重/ack；可能为空（发送方未带） |
| customInfo | **恒为字符串**（三端类型一致）；服务端不解析、不解密内容 |
| serverSeq | 房间内单调序号 |

SDK 回调：`OnReceiveCustomSignal(整段 JSON 字符串)`。App 再校验 `verifiedSenderID` vs 外层 `senderUserID`，并解密 `mlsMessage`。

---

### 4.6 群通话结束通知

`POST /rtc/signal_notify_group_call_ended`

```json
{
  "groupID": "g1",
  "roomID": "r_xxx",
  "inviterUserID": "u1",
  "mediaType": "video",
  "durationSecs": 120,
  "endReason": "hungup"
}
```

`endReason`：`hungup` | `cancel` | `reject` | `timeout`

---

### 4.7 历史记录

| Path | 请求要点 | 响应 |
|---|---|---|
| `/rtc/get_signal_invitation_records` | 分页 + sessionType + 时间窗 | `{ total, signalRecords }` |
| `/rtc/delete_signal_records` | `{ "sIDs": ["..."] }` | `{}` |

---

## 5. OpenMLS（通话 E2EE 关键子集）

### 5.1 SubmitCommit — `POST /openmls/v1/groups/commit`

**请求**

```json
{
  "groupID": "si_u1_u2",
  "senderUserID": "u1",
  "senderDeviceID": "d1",
  "fromEpoch": 2,
  "commitMessage": "<base64>",
  "welcomeMessages": [],
  "commitHash": "<sha256-base64url>",
  "idempotencyKey": "<uuid>"
}
```

**成功响应**

```json
{
  "newEpoch": 3,
  "sequenceNumber": 3,
  "broadcastCount": 1,
  "accepted": true,
  "duplicate": false,
  "acceptedEpoch": 3,
  "commitID": "<id>",
  "expectedFromEpoch": 0
}
```

| 字段 | 说明 |
|---|---|
| accepted | `true` = 已接受（或幂等命中）；客户端可信任历史可经 GetCommits 追齐 |
| duplicate | `true` = 命中 `idempotencyKey`，未重复推进 epoch |
| acceptedEpoch | 接受后的 epoch |
| expectedFromEpoch | 仅冲突时有意义 |

**原子性：** `fromEpoch` CAS 推进 epoch 与写入 Commit 历史在同一事务内（副本集有效）。

**4010 epoch 冲突：** `errCode=4010`；可能带 `accepted=false` + `expectedFromEpoch`；若 HTTP 壳丢弃 body，从 `errMsg` 解析 `expectedFromEpoch=N`。客户端须追赶后**重新生成** Commit，禁止重发旧包。

---

### 5.2 GetCommits — `POST /openmls/v1/groups/commits`

**请求：** `{ "groupID", "sinceEpoch", "limit" }`  
**响应：** `{ "groupID", "commits": [{ "epoch", "sequenceNumber", "commitMessage", "senderUserID", "createdAt" }], "currentEpoch", "hasMore" }`

**鉴权：** 仅 IM 管理员 / 1:1 双方 / 当前群成员；被踢后立即无权限。

---

### 5.3 GetGroupState — `POST /openmls/v1/groups/state`

**请求：** `{ "groupID" }`  
**响应：** `{ "groupID", "currentEpoch", "memberCount", "lastCommitAt", "createdAt" }`  
**鉴权：** 同 GetCommits。

---

## 6. 典型调用时序（E2EE）

```text
主叫:
  Invite(customData.e2ee + e2eeCapability)
  → 用响应 conversationID / roomID / token
  → MLS 导出 K_media → key_prepare / key_ready / key_activate（CustomSignal）
  → activate 后再 LiveKit connect
  → 周期 GetTokenByRoomID 续期（带 e2eeCapability）

被叫:
  收到邀请 → 解析 e2ee
  → Accept(e2eeCapability)
  → 响应 e2ee / conversationID / token
  → 同协调流程后进房

群中途加入:
  Join(e2eeCapability) → e2ee / token → 追 epoch → 进房

踢人:
  服务端 RemoveParticipant + MLS Remove trigger
  → 剩余端换钥；被踢端不得再取 Token
```

---

## 7. 错误码

| errCode | 名 | 场景 |
|---|---|---|
| 1830 | `CALL_E2EE_REQUIRED_UNSUPPORTED` | 强制 E2EE 房间无能力 / 无 FrameCryptor |
| 1831 | `CALL_E2EE_CONVERSATION_NOT_READY` | MLS/会话未就绪（预留/业务侧） |
| 1832 | `CALL_E2EE_GROUP_MEMBERSHIP_INVALID` | 非成员 / 身份不符 |
| 1833 | `CALL_E2EE_PROTOCOL_VERSION_MISMATCH` | scheme 不匹配或 `clientVersion` 缺省/过低 |
| 1834 | `CALL_E2EE_TOKEN_DENIED` | `userID` ≠ `opUserID` 等 Token 拒绝 |
| 4010 | `epoch conflict` | SubmitCommit `fromEpoch` CAS 失败 |
| 其它 | Args / NoPermission / … | 参数、权限、房间不存在、限流、customInfo 超 16KB 等 |

UI 约定：1830/1833 仅升级提示，**禁止**「继续普通通话」降级按钮。

---

## 8. 与客户端文档交叉引用

- 客户端改造方案：`docs/superpowers/specs/2026-07-16-call-e2ee-client-plan.md`
- 服务端设计：`docs/superpowers/specs/2026-07-16-call-e2ee-server-design.md`
- 服务端 changelog：`docs/superpowers/specs/2026-07-16-call-e2ee-server-changelog.md`
- Proto 源：`protocol/rtc/rtc.proto`、`protocol/openmls/openmls.proto`
