# Payment Notification Mongo Persistence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist payment notification payloads to Mongo on `send_payment_notification` (without changing send behavior), and add admin-only `get_payment_notifications` filtered by `sendUserID`.

**Architecture:** Add `payment_notification` collection via model + database interface + mgo (API 直连，无 controller/RPC). `SendPaymentNotification` inserts one doc after BindJSON, then calls existing `sendNotificationChatMsg` unchanged. New HTTP handler queries by `send_user_id` with pagination.

**Tech Stack:** Go, Gin, MongoDB (`mongoutil` / `pagination`), existing OpenIM API patterns.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-27-payment-notification-mongo-design.md`
- `SendPaymentNotification`：**只增加 Mongo 写入**；请求/响应/`sendNotificationChatMsg` 行为不动
- `content.recvUserID` 保持 `string`（不改为数组）
- 收发权威源：顶层 `req.sendUserID` / `req.recvUserID`；content 内收发仅入库
- 写失败 → 不发通知；发失败 → 不回滚 Mongo
- 查询：**仅**按顶层语义 `send_user_id`；仅 admin；无唯一索引 / 无幂等
- Collection 名：`payment_notification`；索引：`{send_user_id: 1, create_time: -1}`
- 不新增 controller / RPC / Cache

## File Map

| File | Responsibility |
|---|---|
| `pkg/common/storage/model/payment_notification.go` (new) | BSON document + SecondaryAction |
| `pkg/common/storage/database/name.go` | `PaymentNotificationName` |
| `pkg/common/storage/database/payment_notification.go` (new) | `Create` / `FindPage` interface |
| `pkg/common/storage/database/mgo/payment_notification.go` (new) | Mongo impl + index |
| `internal/api/payment_notification_build.go` (new) | Pure mapper req+content → model |
| `internal/api/payment_notification_build_test.go` (new) | Mapper unit tests |
| `pkg/apistruct/msg.go` | Get req/resp DTOs（不改 Content 类型） |
| `internal/api/msg.go` | Inject DB；Send 写库；Get handler |
| `internal/api/router.go` | NewMongo + inject + route |

---

### Task 1: Model + database interface + collection name

**Files:**
- Create: `pkg/common/storage/model/payment_notification.go`
- Create: `pkg/common/storage/database/payment_notification.go`
- Modify: `pkg/common/storage/database/name.go`

**Interfaces:**
- Produces: `model.PaymentNotification`, `model.PaymentNotificationAction`
- Produces: `database.PaymentNotification` with:
  - `Create(ctx context.Context, n *model.PaymentNotification) error`
  - `FindPage(ctx context.Context, sendUserID string, pagination pagination.Pagination) (total int64, list []*model.PaymentNotification, err error)`
- Produces: `database.PaymentNotificationName = "payment_notification"`

- [ ] **Step 1: Add collection name constant**

In `pkg/common/storage/database/name.go`, append:

```go
	PaymentNotificationName = "payment_notification"
```

- [ ] **Step 2: Add model**

Create `pkg/common/storage/model/payment_notification.go`:

```go
package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type PaymentNotificationAction struct {
	Text string `bson:"text"`
	URL  string `bson:"url"`
}

type PaymentNotification struct {
	ID                primitive.ObjectID         `bson:"_id,omitempty"`
	SendUserID        string                     `bson:"send_user_id"`
	RecvUserID        string                     `bson:"recv_user_id"`
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
```

- [ ] **Step 3: Add database interface**

Create `pkg/common/storage/database/payment_notification.go`:

```go
package database

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/pagination"
)

type PaymentNotification interface {
	Create(ctx context.Context, n *model.PaymentNotification) error
	FindPage(ctx context.Context, sendUserID string, pagination pagination.Pagination) (total int64, list []*model.PaymentNotification, err error)
}
```

- [ ] **Step 4: Compile-check packages**

Run: `go build ./pkg/common/storage/model/ ./pkg/common/storage/database/`

Expected: exit 0

- [ ] **Step 5: Commit**

```bash
git add pkg/common/storage/model/payment_notification.go \
  pkg/common/storage/database/payment_notification.go \
  pkg/common/storage/database/name.go
git commit -m "$(cat <<'EOF'
feat(storage): add payment_notification model and DB interface

EOF
)"
```

---

### Task 2: Mongo implementation (Create + FindPage)

**Files:**
- Create: `pkg/common/storage/database/mgo/payment_notification.go`

**Interfaces:**
- Consumes: `database.PaymentNotification`, `database.PaymentNotificationName`, `model.PaymentNotification`
- Produces: `mgo.NewPaymentNotificationMongo(db *mongo.Database) (database.PaymentNotification, error)`

- [ ] **Step 1: Implement mgo**

Create `pkg/common/storage/database/mgo/payment_notification.go`:

```go
package mgo

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/pagination"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func NewPaymentNotificationMongo(db *mongo.Database) (database.PaymentNotification, error) {
	coll := db.Collection(database.PaymentNotificationName)
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "send_user_id", Value: 1},
			{Key: "create_time", Value: -1},
		},
	})
	if err != nil {
		return nil, err
	}
	return &PaymentNotificationMgo{coll: coll}, nil
}

type PaymentNotificationMgo struct {
	coll *mongo.Collection
}

func (p *PaymentNotificationMgo) Create(ctx context.Context, n *model.PaymentNotification) error {
	if n.CreateTime.IsZero() {
		n.CreateTime = time.Now()
	}
	return mongoutil.InsertOne(ctx, p.coll, n)
}

func (p *PaymentNotificationMgo) FindPage(ctx context.Context, sendUserID string, pagination pagination.Pagination) (int64, []*model.PaymentNotification, error) {
	filter := bson.M{"send_user_id": sendUserID}
	return mongoutil.FindPage[*model.PaymentNotification](ctx, p.coll, filter, pagination,
		options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}}))
}
```

- [ ] **Step 2: Compile-check**

Run: `go build ./pkg/common/storage/database/mgo/`

Expected: exit 0

- [ ] **Step 3: Commit**

```bash
git add pkg/common/storage/database/mgo/payment_notification.go
git commit -m "$(cat <<'EOF'
feat(storage): implement payment_notification Mongo Create/FindPage

EOF
)"
```

---

### Task 3: Pure mapper + unit tests

**Files:**
- Create: `internal/api/payment_notification_build.go`
- Create: `internal/api/payment_notification_build_test.go`

**Interfaces:**
- Consumes: `apistruct.PaymentNotificationContent`, `model.PaymentNotification`
- Produces: `buildPaymentNotification(sendUserID, recvUserID string, content apistruct.PaymentNotificationContent) *model.PaymentNotification`

- [ ] **Step 1: Write failing tests**

Create `internal/api/payment_notification_build_test.go`:

```go
package api

import (
	"testing"

	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPaymentNotification_MapsTopLevelAndContent(t *testing.T) {
	content := apistruct.PaymentNotificationContent{
		Title:           "充值",
		Amount:          "10.00",
		TransactionType: apistruct.PaymentTransactionTypeTransfer,
		TransactionTime: "2026-06-18 17:04:25",
		Currency:        "USDT",
		CurrencyIconURL: "https://icon",
		DetailURL:       "https://detail",
		DetailText:      "查看详情",
		SecondaryAction: &apistruct.PaymentNotificationAction{Text: "去赎回", URL: "https://redeem"},
		OrderNo:         "ord-1",
		BizID:           "biz-1",
		ChainID:         "1",
		PacketID:        "pkt-1",
		ChainKey:        "ethereum",
		SendUserID:      "biz_sender",
		RecvUserID:      "biz_receiver",
		GroupID:         "g1",
	}

	doc := buildPaymentNotification("notify_bot", "u1", content)
	require.NotNil(t, doc)
	assert.Equal(t, "notify_bot", doc.SendUserID)
	assert.Equal(t, "u1", doc.RecvUserID)
	assert.Equal(t, "充值", doc.Title)
	assert.Equal(t, "10.00", doc.Amount)
	assert.Equal(t, apistruct.PaymentTransactionTypeTransfer, doc.TransactionType)
	assert.Equal(t, "2026-06-18 17:04:25", doc.TransactionTime)
	assert.Equal(t, "USDT", doc.Currency)
	assert.Equal(t, "https://icon", doc.CurrencyIconURL)
	assert.Equal(t, "https://detail", doc.DetailURL)
	assert.Equal(t, "查看详情", doc.DetailText)
	require.NotNil(t, doc.SecondaryAction)
	assert.Equal(t, "去赎回", doc.SecondaryAction.Text)
	assert.Equal(t, "https://redeem", doc.SecondaryAction.URL)
	assert.Equal(t, "ord-1", doc.OrderNo)
	assert.Equal(t, "biz-1", doc.BizID)
	assert.Equal(t, "1", doc.ChainID)
	assert.Equal(t, "pkt-1", doc.PacketID)
	assert.Equal(t, "ethereum", doc.ChainKey)
	assert.Equal(t, "g1", doc.GroupID)
	assert.Equal(t, "biz_sender", doc.ContentSendUserID)
	assert.Equal(t, "biz_receiver", doc.ContentRecvUserID)
	assert.False(t, doc.CreateTime.IsZero())
}

func TestBuildPaymentNotification_NilSecondaryAction(t *testing.T) {
	doc := buildPaymentNotification("s", "r", apistruct.PaymentNotificationContent{
		Title: "t", Amount: "1", TransactionType: "转账",
		TransactionTime: "t", Currency: "USDT",
	})
	require.NotNil(t, doc)
	assert.Nil(t, doc.SecondaryAction)
}
```

- [ ] **Step 2: Run tests — expect FAIL**

Run: `go test ./internal/api/ -run TestBuildPaymentNotification -count=1`

Expected: FAIL (`buildPaymentNotification` undefined)

- [ ] **Step 3: Implement mapper**

Create `internal/api/payment_notification_build.go`:

```go
package api

import (
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

func buildPaymentNotification(sendUserID, recvUserID string, content apistruct.PaymentNotificationContent) *model.PaymentNotification {
	doc := &model.PaymentNotification{
		SendUserID:        sendUserID,
		RecvUserID:        recvUserID,
		Title:             content.Title,
		Amount:            content.Amount,
		TransactionType:   content.TransactionType,
		TransactionTime:   content.TransactionTime,
		Currency:          content.Currency,
		CurrencyIconURL:   content.CurrencyIconURL,
		DetailURL:         content.DetailURL,
		DetailText:        content.DetailText,
		OrderNo:           content.OrderNo,
		BizID:             content.BizID,
		ChainID:           content.ChainID,
		PacketID:          content.PacketID,
		ChainKey:          content.ChainKey,
		GroupID:           content.GroupID,
		ContentSendUserID: content.SendUserID,
		ContentRecvUserID: content.RecvUserID,
		CreateTime:        time.Now(),
	}
	if content.SecondaryAction != nil {
		doc.SecondaryAction = &model.PaymentNotificationAction{
			Text: content.SecondaryAction.Text,
			URL:  content.SecondaryAction.URL,
		}
	}
	return doc
}
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test ./internal/api/ -run TestBuildPaymentNotification -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/payment_notification_build.go internal/api/payment_notification_build_test.go
git commit -m "$(cat <<'EOF'
feat(api): add payment notification Mongo document mapper

EOF
)"
```

---

### Task 4: Wire Create into SendPaymentNotification + inject DB

**Files:**
- Modify: `internal/api/msg.go` (`MessageApi` struct, `NewMessageApi`, `SendPaymentNotification`)
- Modify: `internal/api/router.go` (init Mongo, pass into `NewMessageApi`)

**Interfaces:**
- Consumes: `database.PaymentNotification`, `buildPaymentNotification`
- Produces: `MessageApi.paymentNotificationDB` field; `NewMessageApi(..., paymentNotificationDB database.PaymentNotification)`

- [ ] **Step 1: Extend MessageApi**

In `internal/api/msg.go`, update imports to include:

```go
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
```

Update struct + constructor:

```go
type MessageApi struct {
	Client                msg.MsgClient
	userClient            *rpcli.UserClient
	relationClient        *rpcli.RelationClient
	imAdminUserID         []string
	validate              *validator.Validate
	paymentNotificationDB database.PaymentNotification
}

func NewMessageApi(client msg.MsgClient, userClient *rpcli.UserClient, relationClient *rpcli.RelationClient, imAdminUserID []string, paymentNotificationDB database.PaymentNotification) MessageApi {
	return MessageApi{
		Client:                client,
		userClient:            userClient,
		relationClient:        relationClient,
		imAdminUserID:         imAdminUserID,
		validate:              validator.New(),
		paymentNotificationDB: paymentNotificationDB,
	}
}
```

- [ ] **Step 2: Insert Create before send**

Replace `SendPaymentNotification` body **after** successful BindJSON / log, **before** `sendNotificationChatMsg`:

```go
func (m *MessageApi) SendPaymentNotification(c *gin.Context) {
	req := struct {
		SendUserID string                               `json:"sendUserID" binding:"required"`
		RecvUserID string                               `json:"recvUserID" binding:"required"`
		Content    apistruct.PaymentNotificationContent `json:"content" binding:"required"`
	}{}
	if err := c.BindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WithDetail(err.Error()).Wrap())
		return
	}

	log.ZDebug(c, "SendPaymentNotification", "req", req)

	doc := buildPaymentNotification(req.SendUserID, req.RecvUserID, req.Content)
	if err := m.paymentNotificationDB.Create(c, doc); err != nil {
		log.ZError(c, "SendPaymentNotification create mongo failed", err,
			"sendUserID", req.SendUserID, "recvUserID", req.RecvUserID,
			"orderNo", req.Content.OrderNo, "bizID", req.Content.BizID)
		apiresp.GinError(c, err)
		return
	}

	m.sendNotificationChatMsg(c, req.SendUserID, req.RecvUserID, constant.PaymentNotification, req.Content, false)
}
```

Do **not** change `sendNotificationChatMsg` or Content binding.

- [ ] **Step 3: Wire router**

In `internal/api/router.go`, after existing Mongo inits (near `friendDB`), add:

```go
	paymentNotificationDB, err := mgo.NewPaymentNotificationMongo(mgocli.GetDB())
	if err != nil {
		return nil, err
	}
```

Change:

```go
	m := NewMessageApi(msg.NewMsgClient(msgConn), rpcli.NewUserClient(userConn), rpcli.NewRelationClient(friendConn), config.Share.IMAdminUserID)
```

to:

```go
	m := NewMessageApi(msg.NewMsgClient(msgConn), rpcli.NewUserClient(userConn), rpcli.NewRelationClient(friendConn), config.Share.IMAdminUserID, paymentNotificationDB)
```

If any other `NewMessageApi` call sites exist, update them the same way.

- [ ] **Step 4: Find and fix compile breaks**

Run: `rg -n "NewMessageApi\(" -g '*.go'`

Update every call site to pass `paymentNotificationDB` (or `nil` only in tests that do not hit SendPaymentNotification — prefer real mock/stub if needed).

Run: `go build ./internal/api/`

Expected: exit 0

- [ ] **Step 5: Re-run mapper tests**

Run: `go test ./internal/api/ -run TestBuildPaymentNotification -count=1`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/msg.go internal/api/router.go
git commit -m "$(cat <<'EOF'
feat(api): persist payment notification to Mongo before send

EOF
)"
```

---

### Task 5: GetPaymentNotifications HTTP API

**Files:**
- Modify: `pkg/apistruct/msg.go` (append Get DTOs at end of payment-related types)
- Modify: `internal/api/msg.go` (add handler)
- Modify: `internal/api/router.go` (register route)

**Interfaces:**
- Consumes: `MessageApi.paymentNotificationDB.FindPage`, `authverify.CheckAdmin`
- Produces: `POST /msg/get_payment_notifications`

- [ ] **Step 1: Add apistruct DTOs**

In `pkg/apistruct/msg.go`, after `PaymentNotificationContent`, add:

```go
type GetPaymentNotificationsReq struct {
	SendUserID string                   `json:"sendUserID" binding:"required"`
	Pagination *sdkws.RequestPagination `json:"pagination" binding:"required"`
}

type PaymentNotificationItem struct {
	ID                string                     `json:"id"`
	SendUserID        string                     `json:"sendUserID"`
	RecvUserID        string                     `json:"recvUserID"`
	Title             string                     `json:"title"`
	Amount            string                     `json:"amount"`
	TransactionType   string                     `json:"transactionType"`
	TransactionTime   string                     `json:"transactionTime"`
	Currency          string                     `json:"currency"`
	CurrencyIconURL   string                     `json:"currencyIconURL,omitempty"`
	DetailURL         string                     `json:"detailURL,omitempty"`
	DetailText        string                     `json:"detailText,omitempty"`
	SecondaryAction   *PaymentNotificationAction `json:"secondaryAction,omitempty"`
	OrderNo           string                     `json:"orderNo,omitempty"`
	BizID             string                     `json:"bizID,omitempty"`
	ChainID           string                     `json:"chainId,omitempty"`
	PacketID          string                     `json:"packetId,omitempty"`
	ChainKey          string                     `json:"chainKey,omitempty"`
	GroupID           string                     `json:"groupID,omitempty"`
	ContentSendUserID string                     `json:"contentSendUserID,omitempty"`
	ContentRecvUserID string                     `json:"contentRecvUserID,omitempty"`
	CreateTime        int64                      `json:"createTime"` // Unix ms
}

type GetPaymentNotificationsResp struct {
	Total         int64                      `json:"total"`
	Notifications []*PaymentNotificationItem `json:"notifications"`
}
```

Add import in `pkg/apistruct/msg.go` if missing:

```go
import "github.com/openimsdk/protocol/sdkws"
```

(If `apistruct` currently has no import block for sdkws, add a proper import section; keep existing license header.)

- [ ] **Step 2: Implement Get handler**

In `internal/api/msg.go`, add:

```go
func (m *MessageApi) GetPaymentNotifications(c *gin.Context) {
	var req apistruct.GetPaymentNotificationsReq
	if err := c.BindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WithDetail(err.Error()).Wrap())
		return
	}
	if err := authverify.CheckAdmin(c, m.imAdminUserID); err != nil {
		apiresp.GinError(c, err)
		return
	}
	total, list, err := m.paymentNotificationDB.FindPage(c, req.SendUserID, req.Pagination)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	items := make([]*apistruct.PaymentNotificationItem, 0, len(list))
	for _, n := range list {
		item := &apistruct.PaymentNotificationItem{
			ID:                n.ID.Hex(),
			SendUserID:        n.SendUserID,
			RecvUserID:        n.RecvUserID,
			Title:             n.Title,
			Amount:            n.Amount,
			TransactionType:   n.TransactionType,
			TransactionTime:   n.TransactionTime,
			Currency:          n.Currency,
			CurrencyIconURL:   n.CurrencyIconURL,
			DetailURL:         n.DetailURL,
			DetailText:        n.DetailText,
			OrderNo:           n.OrderNo,
			BizID:             n.BizID,
			ChainID:           n.ChainID,
			PacketID:          n.PacketID,
			ChainKey:          n.ChainKey,
			GroupID:           n.GroupID,
			ContentSendUserID: n.ContentSendUserID,
			ContentRecvUserID: n.ContentRecvUserID,
			CreateTime:        n.CreateTime.UnixMilli(),
		}
		if n.SecondaryAction != nil {
			item.SecondaryAction = &apistruct.PaymentNotificationAction{
				Text: n.SecondaryAction.Text,
				URL:  n.SecondaryAction.URL,
			}
		}
		items = append(items, item)
	}
	apiresp.GinSuccess(c, &apistruct.GetPaymentNotificationsResp{
		Total:         total,
		Notifications: items,
	})
}
```

Ensure `authverify` is already imported in `msg.go` (it is).

- [ ] **Step 3: Register route**

In `internal/api/router.go`, immediately after:

```go
		msgGroup.POST("/send_payment_notification", m.SendPaymentNotification)
```

add:

```go
		msgGroup.POST("/get_payment_notifications", m.GetPaymentNotifications)
```

- [ ] **Step 4: Build + test**

Run:

```bash
go build ./internal/api/ ./pkg/apistruct/
go test ./internal/api/ -run TestBuildPaymentNotification -count=1
```

Expected: both succeed

- [ ] **Step 5: Commit**

```bash
git add pkg/apistruct/msg.go internal/api/msg.go internal/api/router.go
git commit -m "$(cat <<'EOF'
feat(api): add get_payment_notifications by sendUserID

EOF
)"
```

---

## Spec Coverage Checklist

| Spec requirement | Task |
|---|---|
| Model + collection + index `{send_user_id, create_time}` | 1, 2 |
| Create before send; send path otherwise unchanged | 4 |
| Top-level send/recv for query keys / send; content fields stored | 3, 4 |
| `content.recvUserID` remains string | (no change; covered by mapper tests) |
| Get by sendUserID only + admin + pagination | 5 |
| No controller/RPC/幂等/recv query | (explicit non-goals; not in tasks) |

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-27-payment-notification-mongo.md`. Two execution options:

**1. Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks

**2. Inline Execution** — execute tasks in this session with checkpoints

Which approach?
