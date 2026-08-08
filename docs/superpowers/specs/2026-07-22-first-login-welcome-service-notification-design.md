# 首次上线欢迎服务号通知 设计文档

## 背景与目标

用户**首次登录并真正上线**（WebSocket 建立连接）后，由官方服务号向其推送一条欢迎通知；文案根据用户设置的语言选择，支持中文简体、中文繁体、英文，且文案可配置。

中文简体默认文案（产品给定）：

> 欢迎来到 SOK！端到端加密，无人可读。我是官方服务号，负责推送通知与安全提醒。请放心，这里只有你和TA。

## 现状关键事实（调研结论）

- **登录与上线分离**：拿 token 在 auth 层；真正上线是 msggateway 建立 WebSocket → `ChangeOnlineStatus` → user RPC `SetUserOnlineStatus`（`internal/rpc/user/online.go`）。「在线后」= 上线事件。
- **无「首次登录」字段**：`model.User` 只有 `CreateTime`（注册时间）。
- **服务号通道已存在**：`ContentType=ServiceNotification(2002)`，`SessionType=NotificationChatType(4)`，`Content=NotificationElem{Detail: apistruct.ServiceNotificationContent{Title, Content, SubType, ...}}`，`SendID` 需为通知账号（`AppMangerLevel>=3`）。`api/msg.go` 的 `buildNotificationChatSendMsgReq` 是参考实现。
- **用户语言字段已存在**：`model.User.Language` / `sdkws.UserInfo.language`，取值如 `zh-CN`、`en-US`；无枚举校验、无服务端 i18n。
- user RPC 已持有 `msgClient`（`rpcli.MsgClient`，内嵌 `SendMsg`）与 `db`（含 rockscache）。

## 设计决策（已确认）

| 决策点 | 结论 |
|--------|------|
| 触发钩子 | user RPC `SetUserOnlineStatus` 内 |
| 一次性标记 | user 表新增持久字段 `welcome_notification_sent`，原子 CAS 保证 exactly-once |
| 文案配置 | 新增独立配置段（`openim-rpc-user.yml` 的 `welcomeNotification`），三语各配 title+content；繁体/英文由本次起草默认值 |
| 语言映射 | 标准映射，未匹配/未设置兜底英文 |
| 存量用户 | 不特殊处理：老用户下次上线各收一次（幂等仍保证只一次）|
| 服务通知开关 | 不尊重 `SokimServiceNotification`：欢迎语为一次性 onboarding，保证必发 |

## 架构与数据流

```
msggateway WS 连上 → ChangeOnlineStatus → user RPC SetUserOnlineStatus(status.Online 非空)
  ├─ 已有：online.SetUserOnline + updateOfflineRecord
  └─ 新增：tryFirstOnlineWelcome(ctx, userID)
        1. db.GetUserByID(缓存)；WelcomeNotificationSent==true → return（快路径，避免续约打库）
        2. 原子 CAS：MarkWelcomeNotificationSent(filter: user_id=X AND !sent, set sent=true) 且失效缓存
           - matchedCount==1 才“抢到”，防并发多端上线重复
        3. 抢到者：按 user.Language 选文案 → 异步 goroutine 发服务号消息
           - 发送失败 → 回滚标记为 false，下次上线重试
```

- 发送在 detached context 的 goroutine 中进行，**不阻塞在线状态主链路**（符合“同步路径不做推送”原则）。
- `SetUserOnlineStatus` 每 ~10 分钟续约会再次调用；快路径靠缓存读命中，命中即返回，无写放大。

## 组件与改动清单

### 1. 数据模型
`pkg/common/storage/model/user.go`：`User` 结构新增

```go
// WelcomeNotificationSent 首次上线欢迎服务号是否已发送（一次性标记）
WelcomeNotificationSent bool `bson:"welcome_notification_sent"`
```

### 2. 存储层原子标记
- `pkg/common/storage/database/user.go`（interface）新增：`MarkWelcomeNotificationSent(ctx, userID) (bool, error)` 与 `SetWelcomeNotificationSent(ctx, userID, sent bool) error`（回滚用）。
- `pkg/common/storage/database/mgo/user.go` 实现：`UpdateOne(filter: user_id + welcome_notification_sent != true, $set true)`，返回 `MatchedCount==1`。
- `pkg/common/storage/controller/user.go` 透传并在成功后 `cache.DelUsersInfo(userID).ChainExecDel`（保持缓存一致）。

### 3. 配置
- `pkg/common/config/config.go`：`type User` 新增 `WelcomeNotification WelcomeNotification`；新增结构体：

```go
type WelcomeNotification struct {
    Enable          bool                                `mapstructure:"enable"`
    SendID          string                              `mapstructure:"sendID"`
    SubType         int32                               `mapstructure:"subType"`
    DefaultLanguage string                              `mapstructure:"defaultLanguage"`
    Languages       map[string]WelcomeNotificationText  `mapstructure:"languages"`
}
type WelcomeNotificationText struct {
    Title   string `mapstructure:"title"`
    Content string `mapstructure:"content"`
}
```

- `config/openim-rpc-user.yml`：新增 `welcomeNotification` 段（含三语默认文案，见上文示例）。

### 4. 语言映射
user 包内 helper：把 `user.Language` 归一化到桶键 `zh-Hans` / `zh-Hant` / `en`：
- `zh-cn|zh-hans|zh|zh-sg` → `zh-Hans`
- `zh-tw|zh-hk|zh-mo|zh-hant` → `zh-Hant`
- 其余（含 `en-*`、空、未知）→ 取 `defaultLanguage`（默认 `en`）
- 若归一化后的桶键在 `languages` 中不存在，回退到 `defaultLanguage`；再不存在则跳过发送并告警。
- 大小写不敏感，兼容下划线（`zh_CN`）。

### 5. 发送逻辑
`internal/rpc/user/online.go`（或新增 `internal/rpc/user/welcome.go`）：
- `tryFirstOnlineWelcome(ctx, userID)`：编排上文流程。
- 构造 `msg.SendMsgReq`：`SendID=cfg.SendID`、`RecvID=userID`、`ContentType=constant.ServiceNotification`、`SessionType=constant.NotificationChatType`、`MsgFrom=constant.SysMsgType`、`Content=NotificationElem{Detail: ServiceNotificationContent{Title, Content, SubType}}`、`Options` 用 `config.GetOptionsByNotification({IsSendMsg:true, ReliabilityLevel:ReliableNotificationNoMsg}, nil)`、`ClientMsgID=idutil` 生成（保证消息侧幂等）。
- 调 `s.msgClient.SendMsg`。

## 幂等与一致性

- **exactly-once（正常路径）**：CAS 抢占 + 单赢家发送。
- **发送失败**：回滚标记 → 下次上线重试（至少一次），并发窗口内仍由 CAS 去重。
- **消息侧幂等**：`ClientMsgID` 固定为 `md5(sendID+userID+"welcome")`，即便标记回滚重试也不会在会话产生重复消息。
- **缓存**：CAS 与回滚均失效 user 缓存。

## 失败与降级

- 服务号账号未配/`enable=false`：不发送，`SetUserOnlineStatus` 正常返回。
- `SendMsg` 失败：记录 `ZWarn`，回滚标记；**绝不影响在线状态主链路**。
- 找不到用户或语言桶：告警并跳过。

## 可观测性

- 日志带 `userID`、`language`、`bucket`、`sendID`、`clientMsgID`。
- 关键分支（抢占成功/失败、发送成功/失败/回滚）均打日志。

## 不做（YAGNI）

- 不引入通用服务端 i18n 框架。
- 不改 push 模块多语言。
- 不为老用户做回填迁移（按已确认决策）。
- 不新增 HTTP API（纯内部触发）。

## 默认文案

- zh-Hans：`欢迎来到 SOK！端到端加密，无人可读。我是官方服务号，负责推送通知与安全提醒。请放心，这里只有你和TA。`
- zh-Hant：`歡迎來到 SOK！端到端加密，無人可讀。我是官方服務號，負責推送通知與安全提醒。請放心，這裡只有你和 TA。`
- en：`Welcome to SOK! End-to-end encrypted—no one else can read your chats. I'm the official service account for notifications and security alerts. Rest assured, it's just you and them here.`
