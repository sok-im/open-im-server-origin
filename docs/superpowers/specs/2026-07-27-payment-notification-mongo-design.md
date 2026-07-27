# SendPaymentNotification 支付信息落 Mongo 设计文档

## 背景与目标

`POST /msg/send_payment_notification` 当前只通过 `sendNotificationChatMsg` 下发钱包通知卡片，支付信息不单独持久化，无法按发送者/接收者回溯历史。

目标：

1. 发送支付通知前，将支付信息写入 Mongo（权威源）
2. 提供 HTTP 查询接口，按发送者、接收者过滤并分页

## 现状关键事实

- API：`internal/api/msg.go` → `SendPaymentNotification` → `sendNotificationChatMsg` → msg RPC `SendMsg`
- Content：`apistruct.PaymentNotificationContent`（标题、金额、类型、时间、币种、orderNo/bizID 等）
- 请求顶层另有 `sendUserID` / `recvUserID`（通知会话的发送方/接收方）；Content 内还有业务侧 `sendUserID` 与 `recvUserID[]`
- `api/router.go` 已持有 Mongo 连接，并存在 API 直连存储层先例（如 `user_global_black`）
- 尚无 `payment_notification` collection / model / 查询接口

## 设计决策（已确认）

| 决策点 | 结论 |
|--------|------|
| 目的 | 可按发送者/接收者查历史（不只审计） |
| 本轮范围 | 写入 + HTTP 查询一起做 |
| 写/发顺序 | 先写 Mongo，成功后再发通知；写失败整次请求失败 |
| 实现路径 | API 直连 Mongo 存储层（方案 1），不新增 RPC |
| 查询主键 | 请求顶层 `sendUserID` / `recvUserID` |
| 幂等 | 本轮不做 orderNo/bizID 唯一约束；重复调用可产生多条 |
| 发通知失败 | Mongo 已写入不回滚（可「有库无通知」） |
| 查询鉴权 | 仅 admin |
| Cache | 无；Mongo 为权威源 |

## 架构与数据流

### 写路径

```
POST /msg/send_payment_notification
  1. BindJSON + 校验
  2. 组装 model.PaymentNotification（顶层 send/recv + content 字段）
  3. paymentNotificationDB.Create  —— 失败 → GinError，不发通知
  4. sendNotificationChatMsg(...) —— 失败 → GinError（库中已有记录）
  5. GinSuccess
```

### 读路径

```
POST /msg/get_payment_notifications
  1. BindJSON；sendUserID、recvUserID 至少一个非空
  2. CheckAdmin
  3. paymentNotificationDB.FindPage(filter, pagination) 按 create_time DESC
  4. 返回 total + notifications
```

## 数据模型

**Collection**：`payment_notification`（常量 `database.PaymentNotificationName`）

```go
type PaymentNotification struct {
	ID                   primitive.ObjectID `bson:"_id,omitempty"`
	SendUserID           string             `bson:"send_user_id"`            // 请求顶层，查询键
	RecvUserID           string             `bson:"recv_user_id"`            // 请求顶层，查询键
	Title                string             `bson:"title"`
	Amount               string             `bson:"amount"`
	TransactionType      string             `bson:"transaction_type"`
	TransactionTime      string             `bson:"transaction_time"`
	Currency             string             `bson:"currency"`
	CurrencyIconURL      string             `bson:"currency_icon_url,omitempty"`
	DetailURL            string             `bson:"detail_url,omitempty"`
	DetailText           string             `bson:"detail_text,omitempty"`
	SecondaryAction      *PaymentNotificationAction `bson:"secondary_action,omitempty"`
	OrderNo              string             `bson:"order_no,omitempty"`
	BizID                string             `bson:"biz_id,omitempty"`
	ChainID              string             `bson:"chain_id,omitempty"`
	PacketID             string             `bson:"packet_id,omitempty"`
	ChainKey             string             `bson:"chain_key,omitempty"`
	GroupID              string             `bson:"group_id,omitempty"`
	ContentSendUserID    string             `bson:"content_send_user_id,omitempty"`
	ContentRecvUserIDs   []string           `bson:"content_recv_user_ids,omitempty"`
	CreateTime           time.Time          `bson:"create_time"`
}

type PaymentNotificationAction struct {
	Text string `bson:"text"`
	URL  string `bson:"url"`
}
```

**索引**：

- `{send_user_id: 1, create_time: -1}`
- `{recv_user_id: 1, create_time: -1}`
- `{send_user_id: 1, recv_user_id: 1, create_time: -1}`

## 组件与改动清单

1. **model**：`pkg/common/storage/model/payment_notification.go`
2. **database 接口**：`pkg/common/storage/database/payment_notification.go`
   - `Create(ctx, *model.PaymentNotification) error`
   - `FindPage(ctx, sendUserID, recvUserID string, pagination) (total int64, list []*model.PaymentNotification, err error)`
3. **mgo 实现**：`pkg/common/storage/database/mgo/payment_notification.go`（建索引 + Create + FindPage）
4. **name**：`database/name.go` 增加 `PaymentNotificationName = "payment_notification"`
5. **MessageApi**：注入 `database.PaymentNotification`；改 `SendPaymentNotification`；新增 `GetPaymentNotifications`
6. **router**：`NewPaymentNotificationMongo`，注入 `NewMessageApi`；注册 `POST /msg/get_payment_notifications`
7. **apistruct**（可选）：查询 req/resp 结构体，或放在 `msg.go` 内局部类型

不新增 controller 层（API 直连 database 接口即可，与部分现有 API 用法一致）。

## HTTP 契约

### Send（既有，行为增强）

`POST /msg/send_payment_notification`

请求不变。成功语义变为：已落库且通知发送成功。写库失败则 HTTP 错误，不发通知。

### Get（新增）

`POST /msg/get_payment_notifications`

请求：

```json
{
  "sendUserID": "optional",
  "recvUserID": "optional",
  "pagination": { "pageNumber": 1, "showNumber": 20 }
}
```

响应：

```json
{
  "total": 100,
  "notifications": [
    {
      "sendUserID": "...",
      "recvUserID": "...",
      "title": "...",
      "amount": "...",
      "transactionType": "...",
      "transactionTime": "...",
      "currency": "...",
      "orderNo": "...",
      "bizID": "...",
      "createTime": 0
    }
  ]
}
```

`createTime` 为 Unix 毫秒（与仓库其他列表接口习惯对齐，实现时与同类 API 保持一致）。

## 错误与容错

| 场景 | 行为 |
|------|------|
| 写 Mongo 失败 | 返回错误；不发通知 |
| 写成功、发通知失败 | 返回错误；库中保留记录，本轮不补偿 |
| 查询缺 send 且缺 recv | `ErrArgs` |
| 非 admin 查询 | `ErrNoPermission` |
| 查询 DB 失败 | 返回错误 |

日志关键字段：`sendUserID`、`recvUserID`、`orderNo`、`bizID`。

## 非目标（本轮不做）

- orderNo / bizID 幂等去重
- 按业务 Content 收发方查询
- RPC 封装 / 多服务复用
- Cache、搜索旁路
- 发通知失败后的 Mongo 回滚或补偿重试
- 接收人自查自己的列表（仅 admin）

## 测试建议

- 写成功后 Mongo 有文档，字段与请求一致
- 写失败时不调用 SendMsg（可用 mock / 注入失败）
- 仅 send / 仅 recv / 两者 AND 过滤正确
- 分页 total 与列表一致，按 create_time 降序
- 非 admin 查询被拒绝
- 两者皆空查询返回参数错误
