# Welcome Service Notification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After a successful `UserRegister`, asynchronously send one multilingual welcome `ServiceNotification` card from `service_notification_bot`, with YAML-configurable copy.

**Architecture:** Pure helpers normalize language and pick templates. `user` RPC loads `welcome_service_notification.yml`, persists `language` on register, then async-calls `msg.SendMsg` with `NotificationChatType` + contentType `2002`, matching existing `/msg/send_service_notification`. Send failures never roll back registration.

**Tech Stack:** Go, existing OpenIM `user`/`msg` RPC, `config` YAML (`mapstructure`), `apistruct.ServiceNotificationContent`, `idutil.GetMsgIDByMD5`.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-22-welcome-service-notification-design.md`
- Trigger: only after successful `UserRegister` (not login)
- SendID: `service_notification_bot` (must already be a notification account)
- Empty/unknown language → `defaultLanguage: en`
- Message: `sessionType=4` (`NotificationChatType`), `contentType=2002` (`ServiceNotification`), `subType=2`
- Offline push: disabled in v1
- Send failure: warn log only; no retry / no backfill in v1
- Do not modify `msggateway` or push business logic
- Reuse contentType 2002; do not add a new protocol contentType

## File Map

| File | Responsibility |
|---|---|
| `config/welcome_service_notification.yml` | Default enable/sendUserID/templates |
| `pkg/common/config/config.go` | `WelcomeServiceNotification` struct + filename const |
| `pkg/common/config/env.go` | Env prefix map entry for the new YAML |
| `pkg/common/cmd/constant.go` | Filename const + `ConfigEnvPrefixMap` list |
| `pkg/common/cmd/user.go` | Load YAML into `user.Config` |
| `internal/rpc/user/user.go` | `Config` field; persist `Language`; call sender after create |
| `internal/rpc/user/welcome_notify.go` | Normalize/pick/clientMsgID + async send |
| `internal/rpc/user/welcome_notify_test.go` | Unit tests for helpers + enable gate |
| `config/README_zh_CN.md` / `config/README.md` | One-line doc of the new file (optional, same commit as YAML) |

---

### Task 1: Language normalize + template pick (TDD)

**Files:**
- Create: `internal/rpc/user/welcome_notify.go`
- Create: `internal/rpc/user/welcome_notify_test.go`

**Interfaces:**
- Consumes: none
- Produces:
  - `func NormalizeWelcomeLanguage(lang string) string`
  - `func PickWelcomeTemplate(lang string, defaultLanguage string, templates map[string]WelcomeTemplate) (key string, tmpl WelcomeTemplate, ok bool)`
  - `func WelcomeClientMsgID(userID string) string`
  - type `WelcomeTemplate struct { Title string; Content string }`

- [ ] **Step 1: Write the failing tests**

Create `internal/rpc/user/welcome_notify_test.go`:

```go
package user

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeWelcomeLanguage(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"zh", "zh-CN"},
		{"zh-CN", "zh-CN"},
		{"zh_CN", "zh-CN"},
		{"ZH-cn", "zh-CN"},
		{"zh-Hans", "zh-CN"},
		{"zh-TW", "zh-TW"},
		{"zh_TW", "zh-TW"},
		{"zh-HK", "zh-TW"},
		{"zh-Hant", "zh-TW"},
		{"en", "en"},
		{"en-US", "en"},
		{"en_GB", "en"},
		{"fr-FR", "fr-fr"}, // unknown: lowercased form; picker falls back
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			assert.Equal(t, c.want, NormalizeWelcomeLanguage(c.in))
		})
	}
}

func TestPickWelcomeTemplate(t *testing.T) {
	templates := map[string]WelcomeTemplate{
		"zh-CN": {Title: "欢迎来到 SOK", Content: "简中正文"},
		"zh-TW": {Title: "歡迎來到 SOK", Content: "繁中正文"},
		"en":    {Title: "Welcome to SOK", Content: "EN body"},
	}

	key, tmpl, ok := PickWelcomeTemplate("zh-HK", "en", templates)
	assert.True(t, ok)
	assert.Equal(t, "zh-TW", key)
	assert.Equal(t, "繁中正文", tmpl.Content)

	key, tmpl, ok = PickWelcomeTemplate("", "en", templates)
	assert.True(t, ok)
	assert.Equal(t, "en", key)
	assert.Equal(t, "EN body", tmpl.Content)

	key, tmpl, ok = PickWelcomeTemplate("fr", "en", templates)
	assert.True(t, ok)
	assert.Equal(t, "en", key)

	_, _, ok = PickWelcomeTemplate("fr", "de", templates)
	assert.False(t, ok)
}

func TestWelcomeClientMsgIDStable(t *testing.T) {
	a := WelcomeClientMsgID("userA")
	b := WelcomeClientMsgID("userA")
	c := WelcomeClientMsgID("userB")
	assert.NotEmpty(t, a)
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
}
```

Note: `NormalizeWelcomeLanguage` must **not** map unknown codes to `en`; only `PickWelcomeTemplate` falls back via `defaultLanguage`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rpc/user/ -run 'TestNormalizeWelcomeLanguage|TestPickWelcomeTemplate|TestWelcomeClientMsgIDStable' -count=1`

Expected: FAIL (undefined functions / types)

- [ ] **Step 3: Minimal implementation**

Create `internal/rpc/user/welcome_notify.go`:

```go
package user

import (
	"strings"

	"github.com/openimsdk/tools/utils/idutil"
)

type WelcomeTemplate struct {
	Title   string
	Content string
}

func NormalizeWelcomeLanguage(lang string) string {
	s := strings.TrimSpace(lang)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "_", "-")
	lower := strings.ToLower(s)

	switch lower {
	case "zh", "zh-cn", "zh-hans":
		return "zh-CN"
	case "zh-tw", "zh-hk", "zh-hant":
		return "zh-TW"
	case "en", "en-us", "en-gb":
		return "en"
	default:
		return lower
	}
}

func PickWelcomeTemplate(lang, defaultLanguage string, templates map[string]WelcomeTemplate) (string, WelcomeTemplate, bool) {
	if templates == nil {
		return "", WelcomeTemplate{}, false
	}
	try := func(key string) (string, WelcomeTemplate, bool) {
		if key == "" {
			return "", WelcomeTemplate{}, false
		}
		if t, ok := templates[key]; ok && t.Title != "" && t.Content != "" {
			return key, t, true
		}
		return "", WelcomeTemplate{}, false
	}
	if key, t, ok := try(NormalizeWelcomeLanguage(lang)); ok {
		return key, t, true
	}
	defKey := NormalizeWelcomeLanguage(defaultLanguage)
	if defKey == "" {
		defKey = defaultLanguage
	}
	if key, t, ok := try(defKey); ok {
		return key, t, true
	}
	if key, t, ok := try("en"); ok {
		return key, t, true
	}
	return "", WelcomeTemplate{}, false
}

func WelcomeClientMsgID(userID string) string {
	return idutil.GetMsgIDByMD5("welcome_service_notification:" + userID)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rpc/user/ -run 'TestNormalizeWelcomeLanguage|TestPickWelcomeTemplate|TestWelcomeClientMsgIDStable' -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rpc/user/welcome_notify.go internal/rpc/user/welcome_notify_test.go
git commit -m "feat(user): add welcome language normalize and template pick helpers"
```

---

### Task 2: YAML config + load into user RPC

**Files:**
- Create: `config/welcome_service_notification.yml`
- Modify: `pkg/common/config/config.go` (add structs + `WelcomeServiceNotificationFileName` near other filename consts)
- Modify: `pkg/common/config/env.go` (append filename to `fileNames`)
- Modify: `pkg/common/cmd/constant.go` (add `WelcomeServiceNotificationFileName` var + init assignment + append to `fileNames`)
- Modify: `pkg/common/cmd/user.go` (configMap entry)
- Modify: `internal/rpc/user/user.go` (`Config` struct field)
- Modify: `config/README_zh_CN.md` and `config/README.md` (one row each)

**Interfaces:**
- Consumes: none
- Produces:
  - `config.WelcomeServiceNotification` with `Enable`, `SendUserID`, `DefaultLanguage`, `SubType`, `Templates`
  - `config.WelcomeServiceNotificationTemplate` with `Title`, `Content`
  - Loaded into `user.Config.WelcomeServiceNotificationConfig`

- [ ] **Step 1: Add YAML file**

Create `config/welcome_service_notification.yml`:

```yaml
# Welcome service-account card sent once after UserRegister.
enable: true
sendUserID: service_notification_bot
defaultLanguage: en
subType: 2 # ServiceNotificationSubTypeAccount
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

- [ ] **Step 2: Add config structs**

In `pkg/common/config/config.go`, near other config types (e.g. after `CallRingtoneDefaults`):

```go
type WelcomeServiceNotificationTemplate struct {
	Title   string `mapstructure:"title"`
	Content string `mapstructure:"content"`
}

type WelcomeServiceNotification struct {
	Enable          bool                                          `mapstructure:"enable"`
	SendUserID      string                                        `mapstructure:"sendUserID"`
	DefaultLanguage string                                        `mapstructure:"defaultLanguage"`
	SubType         int32                                         `mapstructure:"subType"`
	Templates       map[string]WelcomeServiceNotificationTemplate `mapstructure:"templates"`
}
```

Add filename constant in the same `const` block that defines `OpenIMRPCUserCfgFileName`:

```go
WelcomeServiceNotificationFileName = "welcome_service_notification.yml"
```

- [ ] **Step 3: Wire env prefix + cmd constant + user configMap**

1. `pkg/common/config/env.go`: append `WelcomeServiceNotificationFileName` to `fileNames`.
2. `pkg/common/cmd/constant.go`: declare `WelcomeServiceNotificationFileName string`, set it to `"welcome_service_notification.yml"` in `init`, append to `fileNames`.
3. `pkg/common/cmd/user.go` configMap:

```go
WelcomeServiceNotificationFileName: &userConfig.WelcomeServiceNotificationConfig,
```

4. `internal/rpc/user/user.go` Config:

```go
type Config struct {
	RpcConfig                        config.User
	RedisConfig                      config.Redis
	MongodbConfig                    config.Mongo
	KafkaConfig                      config.Kafka
	NotificationConfig               config.Notification
	Share                            config.Share
	WebhooksConfig                   config.Webhooks
	LocalCacheConfig                 config.LocalCache
	Discovery                        config.Discovery
	WelcomeServiceNotificationConfig config.WelcomeServiceNotification
}
```

5. README tables: add one line describing the new file.

- [ ] **Step 4: Verify config loads**

Run:

```bash
go test ./pkg/common/config/ -count=1
go build -o /dev/null ./pkg/common/cmd/
```

Expected: PASS / build OK.

- [ ] **Step 5: Commit**

```bash
git add config/welcome_service_notification.yml \
  pkg/common/config/config.go pkg/common/config/env.go \
  pkg/common/cmd/constant.go pkg/common/cmd/user.go \
  internal/rpc/user/user.go \
  config/README.md config/README_zh_CN.md
git commit -m "feat(config): add welcome_service_notification.yml and load in user RPC"
```

---

### Task 3: Async welcome sender

**Files:**
- Modify: `internal/rpc/user/welcome_notify.go` (add sender)
- Modify: `internal/rpc/user/welcome_notify_test.go` (enable gate + build req fields)
- Modify: `internal/rpc/user/user.go` (`Start`: construct sender; store on `userServer`)

**Interfaces:**
- Consumes: `config.WelcomeServiceNotification`, msg send func, `GetUserByID`
- Produces:
  - `type welcomeSender struct { ... }`
  - `func newWelcomeSender(...) *welcomeSender`
  - `func (w *welcomeSender) SendAfterRegister(ctx context.Context, userID, language string)` — non-blocking; safe if `w == nil`
  - `func buildWelcomeSendMsgReq(cfg config.WelcomeServiceNotification, recvUserID, language string) (*msg.SendMsgReq, error)`

- [ ] **Step 1: Write failing tests for enable gate and req shape**

Append to `welcome_notify_test.go`:

```go
func TestWelcomeSenderDisabledDoesNotSend(t *testing.T) {
	called := false
	w := &welcomeSender{
		cfg: config.WelcomeServiceNotification{Enable: false},
		sendMsg: func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error) {
			called = true
			return &msg.SendMsgResp{}, nil
		},
	}
	w.sendSync(context.Background(), "u1", "zh-CN")
	assert.False(t, called)
}

func TestBuildWelcomeSendMsgReqFields(t *testing.T) {
	cfg := config.WelcomeServiceNotification{
		Enable:          true,
		SendUserID:      "service_notification_bot",
		DefaultLanguage: "en",
		SubType:         2,
		Templates: map[string]config.WelcomeServiceNotificationTemplate{
			"en": {Title: "Welcome to SOK", Content: "EN body"},
		},
	}
	req, err := buildWelcomeSendMsgReq(cfg, "u1", "en")
	assert.NoError(t, err)
	assert.Equal(t, "service_notification_bot", req.MsgData.SendID)
	assert.Equal(t, "u1", req.MsgData.RecvID)
	assert.Equal(t, int32(constant.NotificationChatType), req.MsgData.SessionType)
	assert.Equal(t, int32(constant.ServiceNotification), req.MsgData.ContentType)
	assert.Equal(t, int32(constant.SysMsgType), req.MsgData.MsgFrom)
	assert.Equal(t, WelcomeClientMsgID("u1"), req.MsgData.ClientMsgID)
	assert.NotEmpty(t, req.MsgData.Content)
}
```

Add required imports: `context`, `config`, `msg`, `constant`.

- [ ] **Step 2: Run tests — expect FAIL**

Run: `go test ./internal/rpc/user/ -run 'TestWelcomeSenderDisabledDoesNotSend|TestBuildWelcomeSendMsgReqFields' -count=1`

Expected: FAIL (missing types/funcs)

- [ ] **Step 3: Implement sender**

Extend `welcome_notify.go` with (full imports as needed):

```go
type welcomeSender struct {
	cfg     config.WelcomeServiceNotification
	getUser func(ctx context.Context, userID string) (*tablerelation.User, error)
	sendMsg func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error)
}

func newWelcomeSender(
	cfg config.WelcomeServiceNotification,
	getUser func(ctx context.Context, userID string) (*tablerelation.User, error),
	sendMsg func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error),
) *welcomeSender {
	return &welcomeSender{cfg: cfg, getUser: getUser, sendMsg: sendMsg}
}

func (w *welcomeSender) SendAfterRegister(ctx context.Context, userID, language string) {
	if w == nil || !w.cfg.Enable || userID == "" {
		return
	}
	opUserID := mcontext.GetOpUserID(ctx)
	operationID := mcontext.GetOperationID(ctx)
	go func() {
		asyncCtx := mcontext.SetOperationID(context.Background(), operationID)
		if opUserID != "" {
			asyncCtx = mcontext.WithOpUserIDContext(asyncCtx, opUserID)
		}
		w.sendSync(asyncCtx, userID, language)
	}()
}

func (w *welcomeSender) sendSync(ctx context.Context, userID, language string) {
	if !w.cfg.Enable {
		return
	}
	sendID := w.cfg.SendUserID
	if sendID == "" {
		log.ZWarn(ctx, "welcome notify: sendUserID empty", nil, "userID", userID)
		return
	}
	u, err := w.getUser(ctx, sendID)
	if err != nil {
		log.ZWarn(ctx, "welcome notify: get send user failed", err, "sendUserID", sendID, "userID", userID)
		return
	}
	if u.AppMangerLevel < constant.AppNotificationAdmin && u.AppMangerLevel != constant.AppAdmin {
		log.ZWarn(ctx, "welcome notify: sendUserID is not notification account", nil,
			"sendUserID", sendID, "level", u.AppMangerLevel, "userID", userID)
		return
	}

	req, err := buildWelcomeSendMsgReq(w.cfg, userID, language)
	if err != nil {
		log.ZWarn(ctx, "welcome notify: build req failed", err, "userID", userID, "language", language)
		return
	}
	if _, err := w.sendMsg(ctx, req); err != nil {
		log.ZWarn(ctx, "welcome notify: SendMsg failed", err,
			"userID", userID, "language", language, "clientMsgID", req.MsgData.ClientMsgID)
		return
	}
	log.ZInfo(ctx, "welcome notify: sent",
		"userID", userID, "language", language, "clientMsgID", req.MsgData.ClientMsgID)
}

func buildWelcomeSendMsgReq(cfg config.WelcomeServiceNotification, recvUserID, language string) (*msg.SendMsgReq, error) {
	templates := make(map[string]WelcomeTemplate, len(cfg.Templates))
	for k, v := range cfg.Templates {
		templates[k] = WelcomeTemplate{Title: v.Title, Content: v.Content}
	}
	_, tmpl, ok := PickWelcomeTemplate(language, cfg.DefaultLanguage, templates)
	if !ok {
		return nil, errs.ErrArgs.WrapMsg("welcome template not found")
	}
	subType := cfg.SubType
	if subType == 0 {
		subType = apistruct.ServiceNotificationSubTypeAccount
	}
	content := apistruct.ServiceNotificationContent{
		Title:   tmpl.Title,
		Content: tmpl.Content,
		SubType: subType,
	}
	notifCfg := config.NotificationConfig{
		IsSendMsg:        true,
		ReliabilityLevel: constant.ReliableNotificationNoMsg,
	}
	opts := config.GetOptionsByNotification(notifCfg, nil)
	return &msg.SendMsgReq{
		MsgData: &sdkws.MsgData{
			SendID: cfg.SendUserID,
			RecvID: recvUserID,
			Content: []byte(jsonutil.StructToJsonString(&sdkws.NotificationElem{
				Detail: jsonutil.StructToJsonString(content),
			})),
			MsgFrom:     constant.SysMsgType,
			ContentType: constant.ServiceNotification,
			SessionType: constant.NotificationChatType,
			CreateTime:  timeutil.GetCurrentTimestampByMill(),
			ClientMsgID: WelcomeClientMsgID(recvUserID),
			Options:     opts,
		},
	}, nil
}
```

Level check must match `GetNotificationAccount` (`AppAdmin` **or** `>= AppNotificationAdmin`).

In `Start` (`user.go`), after `msgClient` is created, construct sender using the same user DB handle already built in that function (match existing variable name — typically the controller wrapping mongo user DB):

```go
welcome := newWelcomeSender(
	config.WelcomeServiceNotificationConfig,
	func(ctx context.Context, userID string) (*tablerelation.User, error) {
		return userDatabase.GetUserByID(ctx, userID) // use actual local var name from Start
	},
	msgClient.SendMsg,
)
```

Add field on `userServer`:

```go
welcomeSender *welcomeSender
```

Assign when constructing `userServer`.

- [ ] **Step 4: Run unit tests**

Run: `go test ./internal/rpc/user/ -run 'TestNormalize|TestPick|TestWelcome|TestBuildWelcome' -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rpc/user/welcome_notify.go internal/rpc/user/welcome_notify_test.go internal/rpc/user/user.go
git commit -m "feat(user): add async welcome service notification sender"
```

---

### Task 4: Wire `UserRegister` (persist language + send)

**Files:**
- Modify: `internal/rpc/user/user.go` (`UserRegister` loop ~799–824 and after `Create`)

**Interfaces:**
- Consumes: `welcomeSender.SendAfterRegister`, `user.Language` from `sdkws.UserInfo`
- Produces: registered users with `Language` stored; one async welcome per successfully created user

- [ ] **Step 1: Persist language on create**

In the `UserRegister` user construction loop, set:

```go
Language: user.Language,
```

on the `tablerelation.User` literal (alongside existing fields).

- [ ] **Step 2: Trigger welcome after successful Create**

After `s.db.Create` succeeds (Create → metrics → welcome → AfterUserRegister webhook is fine):

```go
if s.welcomeSender != nil {
	for _, u := range users {
		s.welcomeSender.SendAfterRegister(ctx, u.UserID, u.Language)
	}
}
```

- [ ] **Step 3: Compile / test user package**

Run: `go test ./internal/rpc/user/ -count=1`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/rpc/user/user.go
git commit -m "feat(user): send welcome service notification after UserRegister"
```

---

### Task 5: Manual verification checklist

**Files:** none (ops / QA)

- [ ] **Step 1: Preconditions**

1. Ensure notification account `service_notification_bot` exists (`AddNotificationAccount` / admin UI).
2. Deploy/restart `openim-rpc-user` with `welcome_service_notification.yml` present.
3. Confirm `enable: true`.

- [ ] **Step 2: Cases**

| # | Action | Expect |
|---|---|---|
| 1 | Register with `language=zh-CN` | Notification chat shows 简中 title/content |
| 2 | Register with `language=zh-TW` | 繁中文案 |
| 3 | Register with `language=en` | English copy |
| 4 | Register without `language` | English (default) |
| 5 | Re-register same `userID` | `ErrRegisteredAlready`; no second welcome |
| 6 | Set `enable: false`, restart, register | User created; no welcome card |
| 7 | Client UI | `sessionType=4`, card fields title/content/`subType=2` |

- [ ] **Step 3: No code commit** (checklist only).

---

## Spec coverage self-check

| Spec requirement | Task |
|---|---|
| Register-once trigger | Task 4 |
| Persist `language` on register | Task 4 |
| YAML templates zh-CN/zh-TW/en | Task 2 |
| Empty → en | Task 1 + Task 2 `defaultLanguage` |
| ServiceNotification card | Task 3 |
| sendUserID `service_notification_bot` | Task 2 YAML |
| Short titles per language | Task 2 YAML |
| Async; failure does not roll back | Task 3 |
| Stable clientMsgID | Task 1 |
| No offline push v1 | Task 3 (`OfflinePushInfo` omitted; offline enable false) |
| Unit tests for normalize/pick/enable | Task 1 + Task 3 |
| Manual QA | Task 5 |

## Placeholder / consistency self-check

- No TBD steps; names consistent across tasks (`NormalizeWelcomeLanguage`, `PickWelcomeTemplate`, `WelcomeClientMsgID`, `buildWelcomeSendMsgReq`, `welcomeSender`).
- Config types: `config.WelcomeServiceNotification` / `WelcomeServiceNotificationTemplate`.
- Notification-account level check matches `GetNotificationAccount`.
