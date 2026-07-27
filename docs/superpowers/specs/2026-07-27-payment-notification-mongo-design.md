# SendPaymentNotification 支付信息落 Mongo 设计文档

## 背景与目标

`POST /msg/send_payment_notification` 当前只通过 `sendNotificationChatMsg` 下发钱包通知卡片，支付信息不单独持久化，无法按发送者/接收者回溯历史。

目标：

1. 发送支付通知前，将支付信息写入 Mongo（权威源）
2. 提供 HTTP 查询接口，按发送者、接收者过滤并分页

## 现状关键事实

- API：`internal/api/msg.go` → `SendPaymentNotification` → `sendNotificationChatMsg` → msg RPC `SendMsg`
- Content：`apistruct.PaymentNotificationContent`，其中 **`RecvUserID` 为 `[]string`**，`SendUserID` 为 string
- 请求顶层另有 `sendUserID` / `recvUserID`（string）；本设计以 **Content.RecvUserID[]** 为接收人列表权威来源
- `api/router.go` 已持有 Mongo 连接，并存在 API 直连存储层先例（如 `user_global_black`）
- 尚无 `payment_notification` collection / model / 查询接口

## 设计决策（已确认）

| 决策点 | 结论 |
|--------|------|
| 目的 | 可按发送者/接收者查历史（不只审计） |
| 本轮范围 | 写入 + HTTP 查询一起做 |
| 写/发顺序 | 先写 Mongo，成功后再发通知；写失败整次请求失败 |
| 实现路径 | API 直连 Mongo 存储层（方案 1），不新增 RPC |
| 接收人来源 | `content.recvUserID[]`（必填、非空）；对数组内每人各写一条、各发一条 |
| 查询主键 | `send_user_id`（请求顶层 `sendUserID`）+ `recv_user_id`（数组中单个元素，文档内为 string） |
| 多条语义 | 同一接收人可累积多条支付通知；无唯一约束 |
| 幂等 | 本轮不做 orderNo/bizID 唯一约束；重复调用可产生多条 |
| 发通知失败 | Mongo 已写入不回滚（可「有库无通知」） |
| 查询鉴权 | 仅 admin |
| Cache | 无；Mongo 为权威源 |

## 架构与数据流

### 写路径（按接收人展开）

```
POST /msg/send_payment_notification
  1. BindJSON + 校验
  2. recvIDs = content.RecvUserID；为空 → ErrArgs
     （同请求内去重，保序）
  3. 校验 sendUserID 为通知账号（GetNotificationByID）
  4. 为每个 recvID 组装一条 model.PaymentNotification
     → InsertMany 一次写入 —— 失败 → GinError，不发通知
  5. 对每个 recvID 调用 SendMsg(SendID=sendUserID, RecvID=recvID)
     —— 单个失败记入 failedIDs，不中断其余（对齐 BatchSendServiceNotification）
  6. GinSuccess（含成功结果 / failedIDs）
```

说明：

- 顶层请求的 `recvUserID`（string）**废弃不用**；以 `content.recvUserID[]` 为准
- 同一 `recvUserID` 在不同请求中可多次出现 → 多条历史记录（允许）

### 读路径

```
POST /msg/get_payment_notifications
  1. BindJSON；sendUserID、recvUserID 至少一个非空
  2. CheckAdmin
  3. FindPage：按 send_user_id / recv_user_id（string 等值）过滤，create_time DESC
  4. 返回 total + notifications
```

## 数据模型

**Collection**：`payment_notification`（常量 `database.PaymentNotificationName`）

每条文档对应 **一个接收人**（由 `content.recvUserID[]` 展开）。

```go
type PaymentNotification struct {
	ID              primitive.ObjectID             `bson:"_id,omitempty"`
	SendUserID      string                         `bson:"send_user_id"` // 请求顶层 sendUserID，查询键
	RecvUserID      string                         `bson:"recv_user_id"` // 数组展开后的单个接收人，查询键
	Title           string                         `bson:"title"`
	Amount          string                         `bson:"amount"`
	TransactionType string                         `bson:"transaction_type"`
	TransactionTime string                         `bson:"transaction_time"`
	Currency        string                         `bson:"currency"`
	CurrencyIconURL string                         `bson:"currency_icon_url,omitempty"`
	DetailURL       string                         `bson:"detail_url,omitempty"`
	DetailText      string                         `bson:"detail_text,omitempty"`
	SecondaryAction *PaymentNotificationAction     `bson:"secondary_action,omitempty"`
	OrderNo         string                         `bson:"order_no,omitempty"`
	BizID           string                         `bson:"biz_id,omitempty"`
	ChainID         string                         `bson:"chain_id,omitempty"`
	PacketID        string                         `bson:"packet_id,omitempty"`
	ChainKey        string                         `bson:"chain_key,omitempty"`
	GroupID         string                         `bson:"group_id,omitempty"`
	ContentSendUserID string                       `bson:"content_send_user_id,omitempty"` // content.sendUserID
	CreateTime      time.Time                      `bson:"create_time"`
}

type PaymentNotificationAction struct {
	Text string `bson:"text"`
	URL  string `bson:"url"`
}
```

**索引**（无唯一索引，允许同一收发方多条）：

- `{send_user_id: 1, create_time: -1}`
- `{recv_user_id: 1, create_time: -1}`
- `{send_user_id: 1, recv_user_id: 1, create_time: -1}`

## 组件与改动清单

1. **model**：`pkg/common/storage/model/payment_notification.go`
2. **database 接口**：`pkg/common/storage/database/payment_notification.go`
   - `CreateMany(ctx, []*model.PaymentNotification) error`
   - `FindPage(ctx, sendUserID, recvUserID string, pagination) (total int64, list []*model.PaymentNotification, err error)`
3. **mgo 实现**：`pkg/common/storage/database/mgo/payment_notification.go`
4. **name**：`database/name.go` 增加 `PaymentNotificationName = "payment_notification"`
5. **MessageApi**：注入 DB；改写 `SendPaymentNotification`（按数组展开）；新增 `GetPaymentNotifications`
6. **router**：初始化 Mongo、注入、注册 `POST /msg/get_payment_notifications`
7. **apistruct**：`PaymentNotificationContent.RecvUserID` 保持 `[]string`；发送请求改为依赖 content 数组；查询 req/resp

不新增 controller 层。

## HTTP 契约

### Send（行为变更）

`POST /msg/send_payment_notification`

```json
{
  "sendUserID": "notification_account",
  "content": {
    "sendUserID": "biz_sender",
    "recvUserID": ["u1", "u2"],
    "title": "...",
    "amount": "...",
    "transactionType": "...",
    "transactionTime": "...",
    "currency": "..."
  }
}
```

- `content.recvUserID` 必填且非空
- 顶层 `recvUserID` 若仍存在于旧调用方，**忽略**（实现时可从 binding 中移除）
- 成功：每个 recv 各一条 Mongo + 各一条通知；响应结构对齐批量发送（results / failedIDs）

### Get（新增）

`POST /msg/get_payment_notifications`

```json
{
  "sendUserID": "optional",
  "recvUserID": "optional",
  "pagination": { "pageNumber": 1, "showNumber": 20 }
}
```

`recvUserID` 为 **单个 string**（查该接收人的历史，含多条）。

响应：`{ "total", "notifications": [...] }`，`createTime` 为 Unix 毫秒。

## 错误与容错

| 场景 | 行为 |
|------|------|
| `content.recvUserID` 为空 | `ErrArgs`；不写库、不发通知 |
| InsertMany 失败 | 返回错误；不发通知 |
| 部分 SendMsg 失败 | 已写库保留；failedIDs 返回；其余成功继续 |
| 查询缺 send 且缺 recv | `ErrArgs` |
| 非 admin 查询 | `ErrNoPermission` |

日志关键字段：`sendUserID`、`recvUserIDs`、`orderNo`、`bizID`。

## 非目标（本轮不做）

- orderNo / bizID 幂等去重 / 唯一索引
- 一条 Mongo 存整个 recv 数组（已否定，采用按人展开）
- RPC 封装 / 多服务复用
- Cache、搜索旁路
- 发通知失败后的 Mongo 回滚
- 接收人自查（仅 admin）

## 测试建议

- `recvUserID=["u1","u2"]` → Mongo 2 条，SendMsg 2 次
- 同请求内重复 ID 去重后只写/发一次
- 同一用户连续两次请求 → 库中 2 条历史
- 写库失败时不调用 SendMsg
- 仅 send / 仅 recv / 两者 AND 过滤正确；recv 可查到多条
- 分页与权限校验
