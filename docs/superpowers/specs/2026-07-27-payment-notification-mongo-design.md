# SendPaymentNotification 支付信息落 Mongo 设计文档

## 背景与目标

`POST /msg/send_payment_notification` 当前只通过 `sendNotificationChatMsg` 下发钱包通知卡片，支付信息不单独持久化，无法按发送者回溯历史。

目标：

1. 发送支付通知前，将支付信息写入 Mongo（权威源）
2. 提供 HTTP 查询接口，**仅按 `sendUserID` 过滤并分页**（不支持按 recv 查询）

## 现状关键事实

- API：`internal/api/msg.go` → `SendPaymentNotification` → `sendNotificationChatMsg` → msg RPC `SendMsg`
- Content：`apistruct.PaymentNotificationContent`，其中 **`RecvUserID` 为 `string`（保持不变，不改为数组）**
- 请求顶层另有 `sendUserID` / `recvUserID`（string）；当前发通知使用顶层字段
- `api/router.go` 已持有 Mongo 连接，并存在 API 直连存储层先例（如 `user_global_black`）
- 尚无 `payment_notification` collection / model / 查询接口

## 设计决策（已确认）

| 决策点 | 结论 |
|--------|------|
| 目的 | 可按发送者查历史（不只审计） |
| 本轮范围 | 写入 + HTTP 查询一起做 |
| 写/发顺序 | 先写 Mongo，成功后再发通知；写失败整次请求失败 |
| 实现路径 | API 直连 Mongo 存储层（方案 1），不新增 RPC |
| 接收人类型 | `content.recvUserID` **保持 string**，不做数组展开 |
| 收发权威源 | **顶层** `req.sendUserID` / `req.recvUserID`（与现有 `sendNotificationChatMsg` 一致） |
| content 内收发 | `content.sendUserID` / `content.recvUserID` 仅作业务附加字段一并入库，不驱动发送 |
| 查询维度 | **仅** `send_user_id`（顶层 `sendUserID`）；不按 `recv_user_id` 过滤 |
| 多条语义 | 同一发送方可累积多条支付通知；无唯一约束 |
| 幂等 | 本轮不做 orderNo/bizID 唯一约束；重复调用可产生多条 |
| 发通知失败 | Mongo 已写入不回滚（可「有库无通知」） |
| 查询鉴权 | 仅 admin |
| Cache | 无；Mongo 为权威源 |

## 架构与数据流

### 写路径（单接收人）

```
POST /msg/send_payment_notification
  1. BindJSON + 校验（顶层 sendUserID、recvUserID、content 必填）
  2. 校验 sendUserID 为通知账号（GetNotificationByID）
  3. 组装一条 model.PaymentNotification → Create（InsertOne）
     —— SendUserID/RecvUserID 取自顶层 req
     —— Content 字段（含 content.sendUserID / content.recvUserID）一并落库
     —— 失败 → GinError，不发通知
  4. sendNotificationChatMsg(req.SendUserID, req.RecvUserID, ...)
     —— 失败 → GinError（库记录保留）
  5. GinSuccess（对齐现有单条发送响应）
```

说明：

- 每次请求写 **1 条** Mongo、发 **1 条** 通知
- 同一收发方在不同请求中可多次出现 → 多条历史记录（允许）
- 不强制 `req.recvUserID == content.recvUserID`

### 读路径

```
POST /msg/get_payment_notifications
  1. BindJSON；sendUserID 必填
  2. CheckAdmin
  3. FindPage：按 send_user_id 等值过滤，create_time DESC
  4. 返回 total + notifications
```

## 数据模型

**Collection**：`payment_notification`（常量 `database.PaymentNotificationName`）

每条文档对应 **一次发送请求 / 一个接收人**。

```go
type PaymentNotification struct {
	ID                primitive.ObjectID         `bson:"_id,omitempty"`
	SendUserID        string                     `bson:"send_user_id"` // 顶层 sendUserID（通知账号）；查询键
	RecvUserID        string                     `bson:"recv_user_id"` // 顶层 recvUserID（通知接收方）；仅存储，不作为查询条件
	Title             string                     `bson:"title"`
	Amount            string                     `bson:"amount"`
	TransactionType   string                     `bson:"transaction_type"`
	TransactionTime   string                     `bson:"transaction_time"`
	Currency          string                     `bson:"currency"`
	CurrencyIconURL   string                     `bson:"currency_icon_url,omitempty"`
	DetailURL         string                     `bson:"detail_url,omitempty"`
	DetailText        string                     `bson:"detail_text,omitempty"`
	SecondaryAction   *PaymentNotificationAction `bson:"secondary_action,omitempty"`
	OrderNo           string                     `bson:"order_no,omitempty"`
	BizID             string                     `bson:"biz_id,omitempty"`
	ChainID           string                     `bson:"chain_id,omitempty"`
	PacketID          string                     `bson:"packet_id,omitempty"`
	ChainKey          string                     `bson:"chain_key,omitempty"`
	GroupID           string                     `bson:"group_id,omitempty"`
	ContentSendUserID string                     `bson:"content_send_user_id,omitempty"` // content.sendUserID
	ContentRecvUserID string                     `bson:"content_recv_user_id,omitempty"` // content.recvUserID
	CreateTime        time.Time                  `bson:"create_time"`
}

type PaymentNotificationAction struct {
	Text string `bson:"text"`
	URL  string `bson:"url"`
}
```

**索引**（无唯一索引；本轮查询仅按 send）：

- `{send_user_id: 1, create_time: -1}`

## 组件与改动清单

1. **model**：`pkg/common/storage/model/payment_notification.go`
2. **database 接口**：`pkg/common/storage/database/payment_notification.go`
   - `Create(ctx, *model.PaymentNotification) error`
   - `FindPage(ctx, sendUserID string, pagination) (total int64, list []*model.PaymentNotification, err error)`
3. **mgo 实现**：`pkg/common/storage/database/mgo/payment_notification.go`
4. **name**：`database/name.go` 增加 `PaymentNotificationName = "payment_notification"`
5. **MessageApi**：注入 DB；`SendPaymentNotification` 先写后发；新增 `GetPaymentNotifications`
6. **router**：初始化 Mongo、注入、注册 `POST /msg/get_payment_notifications`
7. **apistruct**：`PaymentNotificationContent.RecvUserID` **保持 `string`**；查询 req/resp（仅 sendUserID）

不新增 controller 层。

## HTTP 契约

### Send（行为变更：增加落库）

`POST /msg/send_payment_notification`

```json
{
  "sendUserID": "notification_account",
  "recvUserID": "u1",
  "content": {
    "sendUserID": "biz_sender",
    "recvUserID": "biz_receiver",
    "title": "...",
    "amount": "...",
    "transactionType": "...",
    "transactionTime": "...",
    "currency": "..."
  }
}
```

- 顶层 `sendUserID` / `recvUserID` **必填**，驱动通知会话；`sendUserID` 同时为 Mongo 查询键
- `content.recvUserID` 类型为 **string**（不改为数组），可选业务字段
- 成功：1 条 Mongo + 1 条通知；响应保持现有单条发送结构

### Get（新增）

`POST /msg/get_payment_notifications`

```json
{
  "sendUserID": "required",
  "pagination": { "pageNumber": 1, "showNumber": 20 }
}
```

仅按顶层语义的 `send_user_id` 过滤；**不接受 / 不使用 recvUserID 查询参数**。

响应：`{ "total", "notifications": [...] }`，`createTime` 为 Unix 毫秒。

## 错误与容错

| 场景 | 行为 |
|------|------|
| 顶层 send/recv 或 content 缺失 | `ErrArgs`；不写库、不发通知 |
| Create 失败 | 返回错误；不发通知 |
| SendMsg 失败 | 已写库保留；返回错误 |
| 查询缺 sendUserID | `ErrArgs` |
| 非 admin 查询 | `ErrNoPermission` |

日志关键字段：`sendUserID`、`recvUserID`、`orderNo`、`bizID`。

## 非目标（本轮不做）

- 按 `recvUserID` 查询 / 复合过滤
- `content.recvUserID` 改为数组 / 多人 fan-out
- 强制顶层与 content 收发人相等
- orderNo / bizID 幂等去重 / 唯一索引
- RPC 封装 / 多服务复用
- Cache、搜索旁路
- 发通知失败后的 Mongo 回滚
- 接收人自查（仅 admin）

## 测试建议

- 单次请求 → Mongo 1 条（send/recv 为顶层），SendMsg 1 次
- 同一 sendUserID 连续两次请求 → 查询返回 2 条（create_time DESC）
- 写库失败时不调用 SendMsg
- 缺 sendUserID 查询失败；有 sendUserID 时返回该发送方全部记录（含不同 recv）
- 分页与权限校验
