# 注册欢迎服务号通知设计

> 日期：2026-07-22  
> 状态：已确认，待实现  
> 范围：`config`、`pkg/common/config`、`pkg/common/cmd`、`internal/rpc/user`（`UserRegister` + 异步发送）；复用现有 `ServiceNotification`（contentType=2002）与通知会话（sessionType=4）

## 1. 目标

用户**注册成功后仅一次**，由官方服务号 `service_notification_bot` 向该用户发送一条**服务号卡片**欢迎通知。文案按用户语言选择，支持简体中文、繁体中文、英文；文案与开关通过 YAML 配置。

### 1.1 已确认决策

| 项 | 决策 |
|---|---|
| 触发时机 | 仅 `UserRegister` 成功后发一次（不在登录时发） |
| 空 / 未知语言 | 回落英文（`defaultLanguage: en`） |
| 文案配置 | YAML（`config/welcome_service_notification.yml`） |
| 消息形态 | 现有服务号卡片 `ServiceNotification`（title + content） |
| 标题 | 短标题按语言配置（如「欢迎来到 SOK」） |
| 发件人 | `service_notification_bot`（须为已存在的 notification 账号） |
| 离线推送 | v1 默认关闭（与现有单发 `SendServiceNotification` 一致） |
| 发送失败 | 不回滚注册、v1 不重试、不补发 |

### 1.2 非目标（v1）

- 登录触发或登录补发
- 运营后台热改文案 / DB 存模板
- 发送失败入队重试、DB「已发送」标记
- 欢迎卡离线推送
- `detailURL` / 「查看详情」跳转

## 2. 现状

- 用户模型已有 `language` 字段（`User.Language` / `UserInfo.language`），可通过 `UpdateUserInfoEx` 更新。
- **`UserRegister` 当前未把 `language` 写入 Mongo**，实现时必须一并落库，否则语言选择永远走默认英文。
- 已有服务号投递能力：`POST /msg/send_service_notification` → `NotificationChatType` + `ServiceNotification`（2002），内容结构为 `apistruct.ServiceNotificationContent`。
- `user` RPC 已持有 `msgClient`，具备调 `msg.SendMsg` 的能力。

## 3. 方案选择（已确认）

| 方案 | 结论 |
|---|---|
| `UserRegister` 落库成功后异步 `msg.SendMsg` | **采用**：改动面小，注册与发送解耦，复用现有服务号通道 |
| `AfterUserRegister` webhook → 外部服务再调发送 API | 拒绝：多一跳、运维面大 |
| 注册写待发标记 + worker/cron | 拒绝：欢迎通知过重 |

## 4. 写路径与职责

```
业务注册方 ──UserRegister──► user RPC
                              │
                              ├─ 校验 / 已有 userID 幂等
                              ├─ 落库 User（含 language）
                              ├─ AfterUserRegister webhook（保持）
                              ├─ 返回成功 ACK
                              └─ 异步（不阻塞 ACK）：
                                   enable? → 按 language 选模板
                                   → msg.SendMsg(
                                       sendID=service_notification_bot,
                                       recvID=新用户,
                                       sessionType=NotificationChat,
                                       contentType=ServiceNotification,
                                       content={title, content, subType}
                                     )
```

| 组件 | 做什么 | 不做什么 |
|---|---|---|
| `user` RPC | 注册落库；读欢迎 YAML；语言归一化；异步调 `msg.SendMsg` | 不做厂商推送；不进 `msggateway` |
| `msg` | 通知会话写路径（seq / 持久化 / 在线投递） | 不感知欢迎文案 |
| `push` | 仅当 options 要求离线推送时参与 | 欢迎卡 v1 不启用离线推送 |

**同步边界**：欢迎通知失败**不阻塞、不回滚**注册；仅 warn 日志（带 `userID` / `clientMsgID` / `language`）。

## 5. 配置

### 5.1 文件

`config/welcome_service_notification.yml`，由 `user` RPC 启动时加载（挂到 `user.Config`，在 `pkg/common/cmd/user.go` 的 configMap 注册）。

### 5.2 结构与默认值

```yaml
enable: true
sendUserID: service_notification_bot
defaultLanguage: en
subType: 2                   # ServiceNotificationSubTypeAccount
templates:
  zh-CN:
    title: 欢迎来到 SOK
    content: 欢迎来到 SOK！端到端加密，无人可读。我是官方服务号，负责推送通知与安全提醒。请放心，这里只有你和TA。
  zh-TW:
    title: 歡迎來到 SOK
    content: 歡迎來到 SOK！端對端加密，無人可讀。我是官方服務號，負責推送通知與安全提醒。請放心，這裡只有你和TA。
  en:
    title: Welcome to SOK
    content: Welcome to SOK! End-to-end encrypted — unread by anyone else. I'm the official service account for notifications and security alerts. Rest assured, it's just you and them here.
```

- `enable: false`：注册成功也不发。
- 卡片字段：仅 `title` + `content`；v1 不配 `detailURL`。
- `subType` 默认 `2`（账号通知）。

### 5.3 语言归一化

注册请求中的 `UserInfo.language` 先写入用户文档，再按下表映射到模板 key：

| 输入示例 | 模板 key |
|---|---|
| `zh-CN` / `zh` / `zh_CN` / `zh-Hans` | `zh-CN` |
| `zh-TW` / `zh-HK` / `zh_TW` / `zh-Hant` | `zh-TW` |
| `en` / `en-US` / `en_US` / `en-GB` | `en` |
| 空、未知、或模板缺失 | `defaultLanguage`（`en`） |

归一化规则实现为纯函数，便于单测；比较前做 trim，大小写不敏感（如 `ZH-cn` → `zh-CN`）。

## 6. 消息构造

对齐现有 `MessageApi.buildNotificationChatSendMsgReq` / `SendServiceNotification`：

| 字段 | 值 |
|---|---|
| `SendID` | 配置 `sendUserID`（`service_notification_bot`） |
| `RecvID` | 新注册 `userID` |
| `SessionType` | `NotificationChatType`（4） |
| `ContentType` | `ServiceNotification`（2002） |
| `MsgFrom` | `SysMsgType` |
| `Content` | `NotificationElem.Detail` = JSON(`ServiceNotificationContent`) |
| `ClientMsgID` | `MD5("welcome_service_notification:" + userID)` |
| `Options` | `IsSendMsg=true`，可靠性与现有单发服务号一致；离线推送关闭 |
| `OfflinePushInfo` | v1 不传 / 为空 |

发送前校验 `sendUserID` 为 notification 账号（复用现有 `GetNotificationByID` 语义）；失败则 warn 并跳过。

## 7. 幂等与失败语义

**主保障**：同一 `userID` 注册只成功一次（再次注册 → `ErrRegisteredAlready`），欢迎发送只在成功路径触发。

**辅保障**：稳定 `clientMsgID`（见上），便于日志关联与潜在去重。

| 场景 | 行为 |
|---|---|
| `enable=false` | 不发，注册成功 |
| `sendUserID` 非法 / 不存在 | warn，注册成功 |
| `msg.SendMsg` 失败 | warn，**不重试**，注册成功 |
| 落库后、发送前进程崩溃 | 可能收不到欢迎卡；**不补发** |
| 批量注册多用户 | 每个成功落库用户各发一条 |

异步：落库成功后对每个用户异步发送；ctx 携带 `operationID` / `userID`，与注册 RPC 解耦。

## 8. 代码落点（实现指引）

1. **配置**：新增 YAML + `pkg/common/config` 结构体；`cmd/user` 注册文件名常量。
2. **注册落库**：`UserRegister` 写入 `user.Language`。
3. **语言工具**：如 `pkg/.../welcomenotify` 或 `internal/rpc/user` 内 `normalizeLanguage` + `pickTemplate`。
4. **发送**：`UserRegister` 成功后调用 `welcomeSender.SendAsync(...)`，内部组 `SendMsgReq` 调 `msgClient.SendMsg`。
5. **不改**：`msggateway`、`push` 业务逻辑、协议 contentType（复用 2002）。

## 9. 测试要点

### 9.1 单元

- 语言归一化：`zh`→`zh-CN`，`zh-HK`→`zh-TW`，`en-US`→`en`，空/未知→`en`
- 模板选择与缺模板回落
- `enable=false` 不调用 `SendMsg`
- 同一 `userID` 的 `clientMsgID` 稳定

### 9.2 集成 / 手工

1. `language=zh-CN` 注册 → 服务号会话出现简中欢迎卡  
2. `zh-TW` / `en` → 对应文案  
3. 不传 `language` → 英文  
4. 同一 `userID` 再次注册失败，且无第二条欢迎卡  
5. `enable=false` → 无欢迎卡，注册成功  
6. 客户端：`sessionType=4`，卡片 `title` / `content` / `subType=2` 可渲染  

### 9.3 不测（v1）

离线推送、登录补发、发送失败自动重试。

## 10. 运维前置

- 环境中已创建 notification 账号 `service_notification_bot`（昵称/头像由运营配置）。
- 部署包含 `welcome_service_notification.yml`；改文案需发配置并重启 `user` RPC（或沿用仓库既有配置热加载机制，若无则重启）。
