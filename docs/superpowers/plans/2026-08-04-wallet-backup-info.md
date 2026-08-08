# Wallet Backup Info Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `POST /wallet/set_backup_info` 与 `POST /wallet/get_backup_info`，将钱包备份元数据按 uid Upsert 到 Mongo（同 uid 只保留最新一条），且仅允许用户读写自己的 uid。

**Architecture:** API 直连 Mongo（对齐 `phone/set_sn_info`）：model + database 接口 + mgo Upsert/GetByUID；`internal/api/wallet_backup.go` 做参数校验与 `opUserID == uid` 鉴权；`router.go` 注册 `/wallet` 路由组。不新增 RPC / proto / cache。

**Tech Stack:** Go, Gin, MongoDB (`mongoutil` / mongo-driver), OpenIM `errs` / `apiresp` / `mcontext`

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-04-wallet-backup-info-design.md`
- 鉴权：仅 `opUserID == uid`（op 为空也拒绝）
- 同 uid：Upsert 覆盖；`uid` 唯一索引
- get 无记录：成功返回空数据（`backupTime=0, fileSize=0, name=""`，uid 回填请求值）
- Collection：`wallet_backup_info`；字段单位：`backupTime` 秒、`fileSize` 字节
- 不新增 controller / RPC / Cache / proto
- JSON 字段名：`uid` / `backupTime` / `fileSize` / `name`

## File Map

| File | Responsibility |
|---|---|
| `pkg/common/storage/model/wallet_backup.go` (new) | BSON document |
| `pkg/common/storage/database/name.go` | `WalletBackupInfoName` |
| `pkg/common/storage/database/wallet_backup.go` (new) | `Upsert` / `GetByUID` 接口 |
| `pkg/common/storage/database/mgo/wallet_backup.go` (new) | Mongo 实现 + uid 唯一索引 |
| `internal/api/wallet_backup.go` (new) | HTTP handlers + 校验/鉴权 helper |
| `internal/api/wallet_backup_test.go` (new) | 校验与鉴权单元测试 |
| `internal/api/router.go` | NewMongo + inject + `/wallet` routes |

---

### Task 1: Model + database interface + collection name

**Files:**
- Create: `pkg/common/storage/model/wallet_backup.go`
- Create: `pkg/common/storage/database/wallet_backup.go`
- Modify: `pkg/common/storage/database/name.go`

**Interfaces:**
- Produces: `model.WalletBackupInfo`
- Produces: `database.WalletBackupInfo` with:
  - `Upsert(ctx context.Context, info *model.WalletBackupInfo) error`
  - `GetByUID(ctx context.Context, uid string) (*model.WalletBackupInfo, error)` — 无记录返回 `(nil, nil)`
- Produces: `database.WalletBackupInfoName = "wallet_backup_info"`

- [ ] **Step 1: Add collection name constant**

In `pkg/common/storage/database/name.go`, append inside the existing `const (` block:

```go
	WalletBackupInfoName = "wallet_backup_info"
```

- [ ] **Step 2: Add model**

Create `pkg/common/storage/model/wallet_backup.go`:

```go
package model

// WalletBackupInfo 钱包备份元数据（每 uid 一条，覆盖写）
type WalletBackupInfo struct {
	UID        string `bson:"uid"`
	BackupTime int64  `bson:"backup_time"` // 秒
	FileSize   int64  `bson:"file_size"`   // 字节
	Name       string `bson:"name"`
	UpdateTime int64  `bson:"update_time"` // 服务端写入 UnixMilli
}
```

- [ ] **Step 3: Add database interface**

Create `pkg/common/storage/database/wallet_backup.go`:

```go
package database

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

type WalletBackupInfo interface {
	Upsert(ctx context.Context, info *model.WalletBackupInfo) error
	// GetByUID 按 uid 查询；无记录时返回 (nil, nil)
	GetByUID(ctx context.Context, uid string) (*model.WalletBackupInfo, error)
}
```

- [ ] **Step 4: Commit**

```bash
git add pkg/common/storage/model/wallet_backup.go \
  pkg/common/storage/database/wallet_backup.go \
  pkg/common/storage/database/name.go
git commit -m "$(cat <<'EOF'
feat(wallet): add wallet backup info model and database interface

EOF
)"
```

---

### Task 2: Mongo Upsert / GetByUID

**Files:**
- Create: `pkg/common/storage/database/mgo/wallet_backup.go`

**Interfaces:**
- Consumes: `model.WalletBackupInfo`, `database.WalletBackupInfo`, `database.WalletBackupInfoName`
- Produces: `mgo.NewWalletBackupInfoMongo(db *mongo.Database) (database.WalletBackupInfo, error)`

- [ ] **Step 1: Implement mgo layer**

Create `pkg/common/storage/database/mgo/wallet_backup.go`:

```go
package mgo

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func NewWalletBackupInfoMongo(db *mongo.Database) (database.WalletBackupInfo, error) {
	coll := db.Collection(database.WalletBackupInfoName)
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "uid", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &walletBackupInfoMgo{coll: coll}, nil
}

type walletBackupInfoMgo struct {
	coll *mongo.Collection
}

func (w *walletBackupInfoMgo) Upsert(ctx context.Context, info *model.WalletBackupInfo) error {
	if info == nil || info.UID == "" {
		return errs.ErrArgs.WrapMsg("uid is empty")
	}
	now := time.Now().UnixMilli()
	filter := bson.M{"uid": info.UID}
	update := bson.M{
		"$set": bson.M{
			"backup_time": info.BackupTime,
			"file_size":   info.FileSize,
			"name":        info.Name,
			"update_time": now,
		},
		"$setOnInsert": bson.M{"uid": info.UID},
	}
	_, err := w.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return errs.Wrap(err)
}

func (w *walletBackupInfoMgo) GetByUID(ctx context.Context, uid string) (*model.WalletBackupInfo, error) {
	if uid == "" {
		return nil, nil
	}
	doc, err := mongoutil.FindOne[*model.WalletBackupInfo](ctx, w.coll, bson.M{"uid": uid})
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			return nil, nil
		}
		return nil, err
	}
	return doc, nil
}
```

- [ ] **Step 2: Compile check**

Run: `go build ./pkg/common/storage/database/mgo/`

Expected: success (no errors)

- [ ] **Step 3: Commit**

```bash
git add pkg/common/storage/database/mgo/wallet_backup.go
git commit -m "$(cat <<'EOF'
feat(wallet): implement wallet backup info mongo upsert

EOF
)"
```

---

### Task 3: API handlers + validation unit tests

**Files:**
- Create: `internal/api/wallet_backup.go`
- Create: `internal/api/wallet_backup_test.go`

**Interfaces:**
- Consumes: `database.WalletBackupInfo`, `model.WalletBackupInfo`
- Produces:
  - `NewWalletBackupApi(db database.WalletBackupInfo) *WalletBackupApi`
  - `(*WalletBackupApi).SetBackupInfo(c *gin.Context)`
  - `(*WalletBackupApi).GetBackupInfo(c *gin.Context)`
  - `requireSelfUID(opUserID, uid string) error`
  - `validateSetBackupInfo(uid, name string, backupTime, fileSize int64) error`

- [ ] **Step 1: Write failing unit tests**

Create `internal/api/wallet_backup_test.go`:

```go
package api

import (
	"testing"

	"github.com/openimsdk/tools/errs"
	"github.com/stretchr/testify/require"
)

func TestRequireSelfUID(t *testing.T) {
	require.True(t, errs.ErrNoPermission.Is(requireSelfUID("", "u1")))
	require.True(t, errs.ErrNoPermission.Is(requireSelfUID("u2", "u1")))
	require.NoError(t, requireSelfUID("u1", "u1"))
}

func TestValidateSetBackupInfo(t *testing.T) {
	require.True(t, errs.ErrArgs.Is(validateSetBackupInfo("", "n", 1, 0)))
	require.True(t, errs.ErrArgs.Is(validateSetBackupInfo("u1", "", 1, 0)))
	require.True(t, errs.ErrArgs.Is(validateSetBackupInfo("u1", "n", 0, 0)))
	require.True(t, errs.ErrArgs.Is(validateSetBackupInfo("u1", "n", 1, -1)))
	require.NoError(t, validateSetBackupInfo("u1", "n", 1, 0))
	require.NoError(t, validateSetBackupInfo("u1", "wallet.zip", 1720000000, 1048576))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestRequireSelfUID|TestValidateSetBackupInfo' -count=1`

Expected: FAIL（`requireSelfUID` / `validateSetBackupInfo` undefined）

- [ ] **Step 3: Implement API + helpers**

Create `internal/api/wallet_backup.go`:

```go
package api

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
)

type WalletBackupApi struct {
	db database.WalletBackupInfo
}

func NewWalletBackupApi(db database.WalletBackupInfo) *WalletBackupApi {
	return &WalletBackupApi{db: db}
}

type walletSetBackupInfoReq struct {
	UID        string `json:"uid" binding:"required"`
	BackupTime int64  `json:"backupTime" binding:"required"`
	FileSize   int64  `json:"fileSize"`
	Name       string `json:"name" binding:"required"`
}

type walletGetBackupInfoReq struct {
	UID string `json:"uid" binding:"required"`
}

type walletBackupInfoResp struct {
	UID        string `json:"uid"`
	BackupTime int64  `json:"backupTime"`
	FileSize   int64  `json:"fileSize"`
	Name       string `json:"name"`
}

func requireSelfUID(opUserID, uid string) error {
	if opUserID == "" || opUserID != uid {
		return errs.ErrNoPermission.WrapMsg("only self can access wallet backup info")
	}
	return nil
}

func validateSetBackupInfo(uid, name string, backupTime, fileSize int64) error {
	if strings.TrimSpace(uid) == "" {
		return errs.ErrArgs.WrapMsg("uid is empty")
	}
	if strings.TrimSpace(name) == "" {
		return errs.ErrArgs.WrapMsg("name is empty")
	}
	if backupTime <= 0 {
		return errs.ErrArgs.WrapMsg("backupTime must be > 0")
	}
	if fileSize < 0 {
		return errs.ErrArgs.WrapMsg("fileSize must be >= 0")
	}
	return nil
}

// SetBackupInfo POST /wallet/set_backup_info
func (a *WalletBackupApi) SetBackupInfo(c *gin.Context) {
	var req walletSetBackupInfoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	uid := strings.TrimSpace(req.UID)
	name := strings.TrimSpace(req.Name)
	if err := validateSetBackupInfo(uid, name, req.BackupTime, req.FileSize); err != nil {
		apiresp.GinError(c, err)
		return
	}
	if err := requireSelfUID(mcontext.GetOpUserID(c), uid); err != nil {
		apiresp.GinError(c, err)
		return
	}
	if err := a.db.Upsert(c, &model.WalletBackupInfo{
		UID:        uid,
		BackupTime: req.BackupTime,
		FileSize:   req.FileSize,
		Name:       name,
	}); err != nil {
		log.ZError(c, "SetBackupInfo", err, "uid", uid)
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, nil)
}

// GetBackupInfo POST /wallet/get_backup_info
func (a *WalletBackupApi) GetBackupInfo(c *gin.Context) {
	var req walletGetBackupInfoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	uid := strings.TrimSpace(req.UID)
	if uid == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("uid is empty"))
		return
	}
	if err := requireSelfUID(mcontext.GetOpUserID(c), uid); err != nil {
		apiresp.GinError(c, err)
		return
	}
	info, err := a.db.GetByUID(c, uid)
	if err != nil {
		log.ZError(c, "GetBackupInfo", err, "uid", uid)
		apiresp.GinError(c, err)
		return
	}
	resp := walletBackupInfoResp{UID: uid}
	if info != nil {
		resp.BackupTime = info.BackupTime
		resp.FileSize = info.FileSize
		resp.Name = info.Name
	}
	apiresp.GinSuccess(c, resp)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestRequireSelfUID|TestValidateSetBackupInfo' -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/wallet_backup.go internal/api/wallet_backup_test.go
git commit -m "$(cat <<'EOF'
feat(wallet): add set/get wallet backup info API handlers

EOF
)"
```

---

### Task 4: Wire Mongo + routes in router

**Files:**
- Modify: `internal/api/router.go`

**Interfaces:**
- Consumes: `mgo.NewWalletBackupInfoMongo`, `NewWalletBackupApi`
- Produces: routes
  - `POST /wallet/set_backup_info`
  - `POST /wallet/get_backup_info`

- [ ] **Step 1: Init Mongo next to phoneSNDB**

In `newGinRouter`, after `phoneSNDB` init block (~line 91-94), add:

```go
	walletBackupDB, err := mgo.NewWalletBackupInfoMongo(mgocli.GetDB())
	if err != nil {
		return nil, err
	}
```

- [ ] **Step 2: Construct API + register routes**

After `phoneSN := NewPhoneSNApi(phoneSNDB)` (~line 204), add:

```go
	walletBackup := NewWalletBackupApi(walletBackupDB)
```

After the `/phone` group block (~line 493-497), add:

```go
	{
		walletGroup := r.Group("/wallet")
		walletGroup.POST("/set_backup_info", walletBackup.SetBackupInfo)
		walletGroup.POST("/get_backup_info", walletBackup.GetBackupInfo)
	}
```

- [ ] **Step 3: Compile API package**

Run: `go build ./internal/api/`

Expected: success

- [ ] **Step 4: Re-run unit tests**

Run: `go test ./internal/api/ -run 'TestRequireSelfUID|TestValidateSetBackupInfo' -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/router.go
git commit -m "$(cat <<'EOF'
feat(wallet): register wallet backup info routes

EOF
)"
```

---

### Task 5: Manual smoke checklist (no automated HTTP test)

**Files:** none (verification only)

- [ ] **Step 1: Confirm endpoints exist in router**

Run: `rg 'set_backup_info|get_backup_info|NewWalletBackup' internal/api pkg/common/storage`

Expected: matches in `router.go`, `wallet_backup.go`, `mgo/wallet_backup.go`

- [ ] **Step 2: Document manual verify steps**（实现完成后由执行者按需在本地跑 API）

1. 登录拿到 token，记 `uid=U`
2. `POST /wallet/set_backup_info` body `{"uid":"U","backupTime":1720000000,"fileSize":100,"name":"a.zip"}` → 成功
3. `POST /wallet/get_backup_info` body `{"uid":"U"}` → 返回上述字段
4. 再 set 不同 `name`/`backupTime` → get 只见最新值
5. 用 U 的 token 请求他人 uid → `ErrNoPermission`
6. get 从未 set 过的用户 → 成功且 `backupTime=0,fileSize=0,name=""`

- [ ] **Step 3: Final commit only if any leftover docs/fixes** — 若无改动则跳过
