# SendPaymentNotification 支付信息落 Mongo 设计文档

## 背景与目标

`POST /msg/send_payment_notification` 当前只通过 `sendNotificationChatMsg` 下发钱包通知卡片，支付信息不单独持久化。

目标：

1. 在现有发送流程上，**仅增加**将支付信息写入 Mongo
2. `SendPaymentNotification` 的请求结构、校验、发通知、响应等**其他逻辑保持不动**
3. 本轮一并提供 HTTP 查询接口，**仅按 `sendUserID` 过滤并分页**

## 现状关键事实

- API：`internal/api/msg.go` → `SendPaymentNotification` → `sendNotificationChatMsg` → msg RPC `SendMsg`
- 现有请求：顶层 `sendUserID` / `recvUserID` + `content`（`PaymentNotificationContent`，`RecvUserID` 为 string）
- `api/router.go` 已持有 Mongo 连接，并存在 API 直连存储层先例（如 `user_global_black`）
- 尚无 `payment_notification` collection / model / 查询接口

## 设计决策（已确认）

| 决策点 | 结论 |
|--------|------|
| 本轮范围 | **写入 + 按 sendUserID 查询一起做** |
| Send 改动范围 | **只增加 Mongo 写入**；BindJSON、鉴权/通知账号校验、`sendNotificationChatMsg`、响应结构均不改 |
| 写/发顺序 | 先写 Mongo，成功后再走现有 `sendNotificationChatMsg`；写失败整次请求失败（不发通知） |
| 实现路径 | API 直连 Mongo 存储层，不新增 RPC |
| 收发字段 | 与现有一致：顶层 `req.sendUserID` / `req.recvUserID` 驱动发送；content 字段一并入库 |
| content.recvUserID | 保持 string，不改为数组 |
| 查询维度 | **仅** `send_user_id`（顶层 `sendUserID`）；不按 recv 过滤 |
| 查询鉴权 | 仅 admin |
| 多条语义 | 同一发送方可累积多条；无唯一约束 |
| 幂等 | 本轮不做 |
| 发通知失败 | Mongo 已写入不回滚 |
| Cache | 无；Mongo 为权威源 |

## 架构与数据流

### 写路径（最小改动）

```
POST /msg/send_payment_notification   // 行为相对现状：仅多一步落库
  1. BindJSON（现有：sendUserID / recvUserID / content）—— 不动
  2. 组装 model.PaymentNotification → Create（InsertOne）  【新增】
     —— SendUserID/RecvUserID 取自顶层 req
     —— Content 全量字段落库
     —— 失败 → GinError，return（不调用后续发送）
  3. sendNotificationChatMsg(req.SendUserID, req.RecvUserID, PaymentNotification, content, false)
     —— 与现有完全一致（内部含通知账号校验、SendMsg、GinSuccess/GinError）
```

说明：

- **不**改请求 JSON 契约、**不**改响应、**不**抽离/重写 `sendNotificationChatMsg`
- 写库插在 `BindJSON` 成功之后、`sendNotificationChatMsg` 之前
- 同一收发方可多次调用 → 多条历史（允许）

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

```go
type PaymentNotification struct {
	ID                primitive.ObjectID         `bson:"_id,omitempty"`
	SendUserID        string                     `bson:"send_user_id"` // 顶层 sendUserID；查询键
	RecvUserID        string                     `bson:"recv_user_id"` // 顶层 recvUserID；仅存储
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
	ContentSendUserID string                     `bson:"content_send_user_id,omitempty"`
	ContentRecvUserID string                     `bson:"content_recv_user_id,omitempty"`
	CreateTime        time.Time                  `bson:"create_time"`
}

type PaymentNotificationAction struct {
	Text string `bson:"text"`
	URL  string `bson:"url"`
}
```

**索引**：`{send_user_id: 1, create_time: -1}`（无唯一索引）

## 组件与改动清单

1. **model**：`pkg/common/storage/model/payment_notification.go`
2. **database 接口**：`Create` + `FindPage(ctx, sendUserID, pagination)`
3. **mgo 实现** + `database/name.go` 常量 `PaymentNotificationName`
4. **MessageApi**：注入 DB；`SendPaymentNotification` 在现有调用前插入 Create；新增 `GetPaymentNotifications`
5. **router**：初始化 Mongo、注入 MessageApi、注册 `POST /msg/get_payment_notifications`
6. **apistruct**：查询 req/resp（`sendUserID` + pagination）

不新增 controller 层；不修改 `sendNotificationChatMsg` / `PaymentNotificationContent` 类型定义。

## HTTP 契约

### Send（仅内部多一步写库；对外契约不变）

`POST /msg/send_payment_notification` — 请求/响应与现状相同：

```json
{
  "sendUserID": "notification_account",
  "recvUserID": "u1",
  "content": { "...": "PaymentNotificationContent 不变" }
}
```

### Get（新增）

`POST /msg/get_payment_notifications`

```json
{
  "sendUserID": "required",
  "pagination": { "pageNumber": 1, "showNumber": 20 }
}
```

响应：`{ "total", "notifications": [...] }`，`createTime` 为 Unix 毫秒。仅 admin。

## 错误与容错

| 场景 | 行为 |
|------|------|
| BindJSON 失败 | 与现有一致 |
| Create 失败 | GinError；**不**调用 `sendNotificationChatMsg` |
| 发送失败 | 与现有一致（库记录保留） |
| 查询缺 sendUserID | `ErrArgs` |
| 非 admin 查询 | `ErrNoPermission` |

日志关键字段：`sendUserID`、`recvUserID`、`orderNo`、`bizID`。

## 非目标（本轮不做）

- 改 `SendPaymentNotification` 的请求/响应结构或发送行为
- `content.recvUserID` 改为数组 / fan-out
- 按 `recvUserID` 查询
- 幂等 / 唯一索引 / 发失败回滚
- RPC / Cache

## 测试建议

- 成功路径：Mongo 多 1 条，通知行为与改前一致
- Create 失败：无 SendMsg，接口报错
- 按 sendUserID 分页返回该发送方全部记录（含不同 recv）
- 缺 sendUserID / 非 admin 查询失败
