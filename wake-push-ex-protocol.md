# SOK Wake Push `ex` 协议（后端集成说明）

> Signal 式隐私离线推送：服务端 **不得** 在 push 载荷中携带消息明文；`title`/`desc` 仅为占位符，真实通知内容由客户端 NSE（iOS）或 PushDecryptService（Android）本地解密后展示。

客户端协议定义见：`local_plugin/openim_common/lib/src/push/wake_push_protocol.dart`

---

## 1. 整体流程

```
发消息客户端
  └─ SendMessage.offlinePushInfo { title, desc, ex, ... }
       └─ OpenIM 离线推送模块（须透传，禁止重写 title/desc）
            ├─ iOS APNs：mutable-content=1 + ex
            └─ Android EngageLab：data 透传 sok_wake_push / ex
                 └─ 接收端 Native 解密 → 展示真实 title/body
```

---

## 2. OpenIM `offlinePushInfo` 字段

客户端通过 SDK `SendMessage` 传入，后端 **原样转发** 至推送网关：

| 字段 | 类型 | 说明 |
|------|------|------|
| `title` | string | 占位标题，固定 `"SOK"` |
| `desc` | string | 占位正文，固定 `"你收到了一条新消息"` |
| `ex` | string | **核心**：JSON 字符串，见下文 schema |
| `iOSPushSound` | string | 默认 `"default"`；来电为 `"call.caf"`（可选） |
| `iOSBadgeCount` | bool | 是否增加角标，通常 `true` |

### 后端必须遵守

1. **透传** `ex` 完整字符串，不做截断、转义破坏或二次序列化。
2. **禁止** 从 OpenIM 消息 DB 读取明文填充 `title`/`desc`。
3. **禁止** 在服务端解密 `ex` 内的 `mls_msg`。
4. iOS 推送必须设置 `mutable-content: 1`，否则 Notification Service Extension 不会运行。

---

## 3. `ex` JSON Schema

`ex` 为 UTF-8 JSON 字符串（非 Base64 包裹）。当前 `schemaVersion = 1`。

### 3.1 公共字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `schemaVersion` | int | 是 | 固定 `1` |
| `pushType` | string | 是 | 见下表 |
| `conversationID` | string | 是* | OpenIM 会话 ID，用于点击跳转 |
| `clientMsgID` | string | 是* | 客户端消息 ID，用于 dedupe |

\* 通话推送 `pushType=call` 时 `conversationID`/`clientMsgID` 可为空。

### 3.2 `pushType` 枚举

| 值 | 含义 |
|----|------|
| `e2ee_message` | E2EE（MLS）加密消息 |
| `plain_message` | 非 E2EE 消息 |
| `call` | 音视频通话邀请 |

---

## 4. 各类型载荷示例

### 4.1 E2EE 消息（`e2ee_message`）

```json
{
  "schemaVersion": 1,
  "pushType": "e2ee_message",
  "conversationID": "si_8249848931_7055180231",
  "clientMsgID": "9c1c257f11d83e3313e80ce3f2bc966c",
  "serverMsgID": "optional_server_id",
  "sourceID": "7055180231",
  "sessionType": 1,
  "senderNickname": "张三",
  "mls_msg": "<Base64 MLS 密文>",
  "epoch": 5,
  "groupIdBytes": "<Base64 群组 ID 字节，可选>"
}
```

| 字段 | 说明 |
|------|------|
| `mls_msg` | **必填**。MLS 加密消息 blob，Base64 编码；NSE 本地解密 |
| `epoch` | 可选。MLS epoch，辅助解密 |
| `groupIdBytes` | 可选。群组 ID 原始字节 Base64；缺省时 NSE 从本地映射表查找 |
| `sessionType` | `1`=单聊，`3`=群聊（与 OpenIM 一致） |
| `sourceID` | 单聊为对方 userID，群聊为 groupID |

### 4.2 非 E2EE 消息（`plain_message`）

```json
{
  "schemaVersion": 1,
  "pushType": "plain_message",
  "conversationID": "si_8249848931_7055180231",
  "clientMsgID": "abc123def456",
  "sourceID": "7055180231",
  "sessionType": 1,
  "senderNickname": "张三",
  "contentType": 101,
  "previewBody": "你好"
}
```

| 字段 | 说明 |
|------|------|
| `contentType` | OpenIM 消息类型，如 `101`=文本 |
| `previewBody` | 客户端生成的预览文案（如 `"[图片]"`），**非服务端 DB 明文** |

### 4.3 通话邀请（`call`）

```json
{
  "schemaVersion": 1,
  "pushType": "call",
  "roomID": "room_abc123",
  "sessionType": 1
}
```

通话 push 的 `title`/`desc` 由信令接口单独指定（如「通话邀请」），`ex` 供点击时识别 roomID。

---

## 5. iOS APNs 载荷格式

EngageLab / OpenIM push 模块下发 APNs 时建议结构：

```json
{
  "aps": {
    "alert": {
      "title": "SOK",
      "body": "你收到了一条新消息"
    },
    "mutable-content": 1,
    "content-available": 1,
    "sound": "default",
    "badge": 1
  },
  "ex": "{\"schemaVersion\":1,\"pushType\":\"e2ee_message\",...}"
}
```

### 关键要求

| 项 | 要求 |
|----|------|
| `mutable-content` | **必须为 `1`**，触发 Notification Service Extension |
| `content-available` | 建议 `1`，支持后台唤醒 |
| `ex` | 放在 payload 根级，NSE 从 `userInfo` 读取 |
| `alert.title/body` | 使用客户端传入的占位符，NSE 会改写为解密后的真实内容 |

### 载荷大小

APNs 限制约 **4KB**。E2EE 的 `mls_msg` 较大时需监控；若超限应与服务端协商压缩或分片策略（当前客户端假设单次推送可携带完整 `mls_msg`）。

---

## 6. Android EngageLab 透传格式

Android 无 NSE，通过 **透传 data / custom message** 触发 `SokPushUserReceiver`：

```json
{
  "sok_wake_push": "<ex JSON 字符串>",
  "extras": {
    "ex": "<ex JSON 字符串，与 sok_wake_push 二选一或同时存在>"
  }
}
```

| 项 | 要求 |
|----|------|
| 消息类型 | 使用 **透传 / custom message**，不要仅发 notification-only |
| 字段名 | 优先 `sok_wake_push`；兼容 `extras.ex` |
| title/content | 可为占位；真实内容由 `PushDecryptService` 本地构建 notification |

---

## 7. 完整 `offlinePushInfo` 示例（E2EE）

客户端 `SendMessage` 请求片段：

```json
{
  "offlinePushInfo": {
    "title": "SOK",
    "desc": "你收到了一条新消息",
    "ex": "{\"schemaVersion\":1,\"pushType\":\"e2ee_message\",\"conversationID\":\"si_xxx\",\"clientMsgID\":\"abc\",\"mls_msg\":\"...\",\"epoch\":5}",
    "iOSPushSound": "default",
    "iOSBadgeCount": true
  }
}
```

后端推送网关应 **逐字段透传**，不做内容替换。

---

## 8. 降级与 Fallback

以下情况客户端展示占位文案（`SOK` / `你收到了一条新消息`）：

- NSE 执行超时（约 30 秒）
- MLS DB 未同步到 App Group
- `mls_msg` 解密失败
- `ex` 解析失败或字段缺失

后端 **无需** 为此做特殊处理；保证 `ex` 透传即可。

---

## 9. 联调检查清单

- [ ] 发送普通文本：APNs/Android push 中 `title`/`desc` 为占位符，非消息明文
- [ ] 发送 E2EE 消息：`ex` 含非空 `mls_msg`，iOS payload 含 `mutable-content: 1`
- [ ] iOS 杀进程收离线 push：通知栏显示解密后的发送者昵称与消息摘要
- [ ] Android 杀进程收离线 push：`SokPushUserReceiver` 触发，通知栏显示解密内容
- [ ] 点击通知：根据 `conversationID` / `pushType=call` 正确跳转
- [ ] 转发 / 红包 / 名片分享消息：同样携带 wake `ex`，非旧版明文 push

---

## 10. 参考代码位置（客户端）

| 模块 | 路径 |
|------|------|
| 协议定义 | `local_plugin/openim_common/lib/src/push/wake_push_protocol.dart` |
| 构建 offlinePushInfo | `local_plugin/openim_common/lib/src/utils/offline_push_util.dart` |
| iOS NSE | `ios/NotificationService/NotificationService.swift` |
| Android 解密 | `android/.../push/PushDecryptService.kt` |
| MLS DB 共享 | `lib/core/push/push_shared_store.dart` |

---

## 11. 版本演进

| schemaVersion | 变更 |
|---------------|------|
| `1` | 初始版本：`e2ee_message` / `plain_message` / `call` |

后续版本新增字段时须保持向后兼容（旧客户端忽略未知字段；新字段尽量 optional）。
