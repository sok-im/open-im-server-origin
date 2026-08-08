# 客服机器人（AI 智能客服）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `openim-rpc-msg` 发送路径中识别「发给客服机器人的消息」（单聊 `recvID==bot` 或群内 @bot），异步回调外部 AI 后端；机器人账号由 `openim-rpc-user` 启动时自动创建。

**Architecture:** 复用现有 webhook 异步回调设施（`webhook.Client.AsyncPost`）。回调在 `MsgToMQ` 成功后触发，失败不影响主链路。机器人是一个 `AppRobotAdmin` 级别的真实 IM 账号，回复通过现有 `/msg/send_msg` 接口发回，因 `sendID==bot` 天然防回环。

**Tech Stack:** Go；OpenIM 现有 `pkg/common/webhook`、`pkg/callbackstruct`、`config.Share`、`protocol/constant`。

## Global Constraints

- 配置放在 `config/share.yml` 的 `chatbot:` 段，对应 `config.Share.Chatbot`（`config.Chatbot` struct），因 `openim-rpc-user` 与 `openim-rpc-msg` 均加载 share.yml。
- 机器人账号 `AppMangerLevel = constant.AppRobotAdmin`（值 4，`protocol/constant/constant.go:309`）。
- 回调 command 常量：`callbackAfterSendMsgToBotCommand`。
- 触发点：`internal/rpc/msg/send.go` 的 `sendMsgSingleChat` 与 `sendMsgGroupChat`，均在各自的 after-webhook 调用之后。
- 防回环：`msg.SendID == cfg.UserID` 时绝不回调。
- 忽略 `constant.Typing`（值 113）与配置的 `deniedTypes`。
- 回调用 `AsyncPost`，失败仅 `log.ZWarn`，不影响用户发消息。
- Go 命令：构建 `go build ./...`；单测 `go test ./internal/rpc/msg/ -run TestChatbotHit -v`。

---

## File Structure

- `pkg/common/config/config.go` — 新增 `Chatbot` struct，挂到 `Share`。
- `config/share.yml` — 新增 `chatbot:` 配置段。
- `pkg/callbackstruct/constant.go` — 新增 command 常量。
- `pkg/callbackstruct/message.go` — 新增 `CallbackAfterSendMsgToBotReq/Resp`。
- `internal/rpc/msg/chatbot.go`（新）— `chatbotHit` 判定 + `webhookAfterSendMsgToChatBot` 触发。
- `internal/rpc/msg/chatbot_test.go`（新）— `chatbotHit` 单测。
- `internal/rpc/msg/server.go` — `msgServer` 加 `chatbotWebhook` 字段并在 `Start` 初始化。
- `internal/rpc/msg/send.go` — 单聊/群聊各加一行触发调用。
- `internal/rpc/user/user.go` — `Start` 里按配置自动建机器人账号。

---

## Task 1: Chatbot 配置

**Files:**
- Modify: `pkg/common/config/config.go`（`Share` struct 约 `518-524`；`Chatbot` 定义紧随 `Share` 之后）
- Modify: `config/share.yml`

**Interfaces:**
- Produces: `config.Chatbot`（字段 `Enable bool`、`UserID string`、`Nickname string`、`FaceURL string`、`CallbackURL string`、`Timeout int`、`DeniedTypes []int32`）；`config.Share.Chatbot Chatbot`。

- [ ] **Step 1: 新增 `Chatbot` struct 并挂到 `Share`**

在 `pkg/common/config/config.go` 中，把 `Share` struct 改为包含 `Chatbot` 字段，并在其后新增 `Chatbot` 定义：

```go
type Share struct {
	Secret          string          `mapstructure:"secret"`
	RpcRegisterName RpcRegisterName `mapstructure:"rpcRegisterName"`
	IMAdminUserID   []string        `mapstructure:"imAdminUserID"`
	MultiLogin      MultiLogin      `mapstructure:"multiLogin"`
	RPCMaxBodySize  MaxRequestBody  `mapstructure:"rpcMaxBodySize"`
	Chatbot         Chatbot         `mapstructure:"chatbot"`
}

// Chatbot 客服机器人配置：机器人账号身份 + 回调外部 AI 后端。
// 被 openim-rpc-user（启动自动建号）与 openim-rpc-msg（消息回调）共同读取。
type Chatbot struct {
	Enable      bool    `mapstructure:"enable"`
	UserID      string  `mapstructure:"userID"`
	Nickname    string  `mapstructure:"nickname"`
	FaceURL     string  `mapstructure:"faceURL"`
	CallbackURL string  `mapstructure:"callbackURL"`
	Timeout     int     `mapstructure:"timeout"`
	DeniedTypes []int32 `mapstructure:"deniedTypes"`
}
```

- [ ] **Step 2: 在 share.yml 追加 chatbot 段**

在 `config/share.yml` 末尾追加：

```yaml

# 客服机器人：enable 后 openim-rpc-user 启动时自动创建 userID 账号，
# 用户单聊该账号或群内 @它时，openim-rpc-msg 异步回调 callbackURL。
# callbackURL 为空则回退到 webhooks.yml 的全局 url。
chatbot:
  enable: false
  userID: "openIMChatbot"
  nickname: "在线客服"
  faceURL: ""
  callbackURL: ""
  timeout: 5
  deniedTypes: [ ]
```

- [ ] **Step 3: 构建验证**

Run: `go build ./...`
Expected: 成功，无编译错误。

- [ ] **Step 4: Commit**

```bash
git add pkg/common/config/config.go config/share.yml
git commit -m "feat(config): add chatbot config to share"
```

---

## Task 2: chatbotHit 命中判定（TDD）

**Files:**
- Create: `internal/rpc/msg/chatbot.go`
- Test: `internal/rpc/msg/chatbot_test.go`

**Interfaces:**
- Consumes: `config.Chatbot`（Task 1）；`github.com/openimsdk/protocol/sdkws`.MsgData；`github.com/openimsdk/protocol/constant`（`SingleChatType=1`、`ReadGroupChatType=3`、`Typing=113`）；`github.com/openimsdk/tools/utils/datautil`.Contain。
- Produces: `func chatbotHit(cfg config.Chatbot, msg *sdkws.MsgData) bool`。

- [ ] **Step 1: 写失败测试**

创建 `internal/rpc/msg/chatbot_test.go`：

```go
package msg

import (
	"testing"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
)

func TestChatbotHit(t *testing.T) {
	base := config.Chatbot{Enable: true, UserID: "bot"}
	cases := []struct {
		name string
		cfg  config.Chatbot
		msg  *sdkws.MsgData
		want bool
	}{
		{"single hit", base, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "u", RecvID: "bot", ContentType: constant.Text}, true},
		{"single miss other recv", base, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "u", RecvID: "other", ContentType: constant.Text}, false},
		{"loop bot sender", base, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "bot", RecvID: "u", ContentType: constant.Text}, false},
		{"disabled", config.Chatbot{Enable: false, UserID: "bot"}, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "u", RecvID: "bot", ContentType: constant.Text}, false},
		{"empty userID", config.Chatbot{Enable: true, UserID: ""}, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "u", RecvID: "", ContentType: constant.Text}, false},
		{"typing ignored", base, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "u", RecvID: "bot", ContentType: constant.Typing}, false},
		{"denied type", config.Chatbot{Enable: true, UserID: "bot", DeniedTypes: []int32{1201}}, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "u", RecvID: "bot", ContentType: 1201}, false},
		{"group at hit", base, &sdkws.MsgData{SessionType: constant.ReadGroupChatType, SendID: "u", GroupID: "g", AtUserIDList: []string{"bot"}, ContentType: constant.AtText}, true},
		{"group no at", base, &sdkws.MsgData{SessionType: constant.ReadGroupChatType, SendID: "u", GroupID: "g", AtUserIDList: []string{"other"}, ContentType: constant.AtText}, false},
		{"group loop bot sender", base, &sdkws.MsgData{SessionType: constant.ReadGroupChatType, SendID: "bot", GroupID: "g", AtUserIDList: []string{"bot"}, ContentType: constant.AtText}, false},
		{"unknown session", base, &sdkws.MsgData{SessionType: constant.NotificationChatType, SendID: "u", RecvID: "bot", ContentType: constant.Text}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chatbotHit(c.cfg, c.msg); got != c.want {
				t.Fatalf("chatbotHit() = %v, want %v", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/rpc/msg/ -run TestChatbotHit -v`
Expected: 编译失败（`undefined: chatbotHit`）。

- [ ] **Step 3: 实现 chatbotHit**

创建 `internal/rpc/msg/chatbot.go`（暂只含判定函数，触发方法在 Task 4 追加）：

```go
package msg

import (
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/utils/datautil"
)

// chatbotHit 判定一条消息是否应回调给客服机器人。
// 单聊：recvID 为机器人；群聊：AtUserIDList 含机器人。
// 机器人自己发出的消息（防回环）、typing、deniedTypes 一律不命中。
func chatbotHit(cfg config.Chatbot, msg *sdkws.MsgData) bool {
	if !cfg.Enable || cfg.UserID == "" {
		return false
	}
	if msg.SendID == cfg.UserID {
		return false
	}
	if msg.ContentType == constant.Typing {
		return false
	}
	if datautil.Contain(msg.ContentType, cfg.DeniedTypes...) {
		return false
	}
	switch msg.SessionType {
	case constant.SingleChatType:
		return msg.RecvID == cfg.UserID
	case constant.ReadGroupChatType:
		return datautil.Contain(cfg.UserID, msg.AtUserIDList...)
	default:
		return false
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/rpc/msg/ -run TestChatbotHit -v`
Expected: PASS（全部子用例通过）。

- [ ] **Step 5: Commit**

```bash
git add internal/rpc/msg/chatbot.go internal/rpc/msg/chatbot_test.go
git commit -m "feat(msg): add chatbot hit predicate with tests"
```

---

## Task 3: 回调结构体与 command 常量

**Files:**
- Modify: `pkg/callbackstruct/constant.go`（常量块 `17-65` 内追加）
- Modify: `pkg/callbackstruct/message.go`（文件末尾追加）

**Interfaces:**
- Produces: 常量 `callbackstruct.CallbackAfterSendMsgToBotCommand`；类型 `callbackstruct.CallbackAfterSendMsgToBotReq`（嵌入 `CommonCallbackReq`，字段 `RecvID string`、`GroupID string`）与 `callbackstruct.CallbackAfterSendMsgToBotResp`（嵌入 `CommonCallbackResp`）。

- [ ] **Step 1: 新增 command 常量**

在 `pkg/callbackstruct/constant.go` 的 `const ( ... )` 块内追加一行（放在 `CallbackAfterSendGroupMsgCommand` 之后）：

```go
	CallbackAfterSendMsgToBotCommand        = "callbackAfterSendMsgToBotCommand"
```

- [ ] **Step 2: 新增回调结构体**

在 `pkg/callbackstruct/message.go` 末尾追加：

```go
type CallbackAfterSendMsgToBotReq struct {
	CommonCallbackReq
	RecvID  string `json:"recvID"`
	GroupID string `json:"groupID"`
}

type CallbackAfterSendMsgToBotResp struct {
	CommonCallbackResp
}
```

- [ ] **Step 3: 构建验证**

Run: `go build ./...`
Expected: 成功。

- [ ] **Step 4: Commit**

```bash
git add pkg/callbackstruct/constant.go pkg/callbackstruct/message.go
git commit -m "feat(callback): add after-send-msg-to-bot callback struct"
```

---

## Task 4: msgServer 触发回调并接入发送路径

**Files:**
- Modify: `internal/rpc/msg/server.go`（`msgServer` struct `59-79`；`Start` 的 `s := &msgServer{...}` `152-168`）
- Modify: `internal/rpc/msg/chatbot.go`（追加触发方法）
- Modify: `internal/rpc/msg/send.go`（`sendMsgGroupChat` 约 `75`；`sendMsgSingleChat` 约 `192`）

**Interfaces:**
- Consumes: `chatbotHit`（Task 2）；`callbackstruct.CallbackAfterSendMsgToBot*` 与 command（Task 3）；`config.Share.Chatbot`（Task 1）；现有 `toCommonCallback`（`internal/rpc/msg/callback.go:31`）、`webhook.NewWebhookClient`、`(*webhook.Client).AsyncPost`。
- Produces: `msgServer.chatbotWebhook *webhook.Client`；`func (m *msgServer) webhookAfterSendMsgToChatBot(ctx context.Context, req *pbchat.SendMsgReq)`。

- [ ] **Step 1: msgServer 增加 chatbotWebhook 字段**

在 `internal/rpc/msg/server.go` 的 `msgServer` struct 中，`webhookClient` 字段之后追加：

```go
	chatbotWebhook         *webhook.Client
```

- [ ] **Step 2: Start 初始化 chatbotWebhook**

在 `internal/rpc/msg/server.go` 的 `Start` 函数内，`s := &msgServer{` 语句之前插入局部变量（回调 URL 为空时回退全局 webhook URL）：

```go
	chatbotURL := config.Share.Chatbot.CallbackURL
	if chatbotURL == "" {
		chatbotURL = config.WebhooksConfig.URL
	}
```

再在 `s := &msgServer{...}` 字面量中，`webhookClient:` 那一行之后追加：

```go
		chatbotWebhook:         webhook.NewWebhookClient(chatbotURL),
```

- [ ] **Step 3: 在 chatbot.go 追加触发方法**

在 `internal/rpc/msg/chatbot.go` 追加（补充 import：`context`、`config` 已在、`pbchat "github.com/openimsdk/protocol/msg"`、`cbapi "github.com/openimsdk/open-im-server/v3/pkg/callbackstruct"`、`"github.com/openimsdk/tools/log"`）：

```go
func (m *msgServer) webhookAfterSendMsgToChatBot(ctx context.Context, req *pbchat.SendMsgReq) {
	cfg := m.config.Share.Chatbot
	if !chatbotHit(cfg, req.MsgData) {
		return
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5
	}
	cbReq := &cbapi.CallbackAfterSendMsgToBotReq{
		CommonCallbackReq: toCommonCallback(ctx, req, cbapi.CallbackAfterSendMsgToBotCommand),
	}
	switch req.MsgData.SessionType {
	case constant.SingleChatType:
		cbReq.RecvID = req.MsgData.RecvID
	case constant.ReadGroupChatType:
		cbReq.GroupID = req.MsgData.GroupID
	}
	log.ZDebug(ctx, "webhookAfterSendMsgToChatBot", "sendID", req.MsgData.SendID,
		"recvID", cbReq.RecvID, "groupID", cbReq.GroupID, "clientMsgID", req.MsgData.ClientMsgID, "seq", req.MsgData.Seq)
	m.chatbotWebhook.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq,
		&cbapi.CallbackAfterSendMsgToBotResp{}, &config.AfterConfig{Enable: true, Timeout: timeout})
}
```

- [ ] **Step 4: 接入单聊发送路径**

在 `internal/rpc/msg/send.go` 的 `sendMsgSingleChat` 中，`m.webhookAfterSendSingleMsg(...)` 之后追加：

```go
	m.webhookAfterSendMsgToChatBot(ctx, req)
```

- [ ] **Step 5: 接入群聊发送路径**

在 `internal/rpc/msg/send.go` 的 `sendMsgGroupChat` 中，`m.webhookAfterSendGroupMsg(...)` 之后追加：

```go
	m.webhookAfterSendMsgToChatBot(ctx, req)
```

- [ ] **Step 6: 构建 + 回归单测**

Run: `go build ./... && go test ./internal/rpc/msg/ -run TestChatbotHit -v`
Expected: 构建成功；`TestChatbotHit` PASS。

- [ ] **Step 7: Commit**

```bash
git add internal/rpc/msg/server.go internal/rpc/msg/chatbot.go internal/rpc/msg/send.go
git commit -m "feat(msg): trigger chatbot callback on single/group message"
```

---

## Task 5: 启动时自动创建机器人账号

**Files:**
- Modify: `internal/rpc/user/user.go`（`Start` 的 imAdmin 初始化循环 `100-104`）

**Interfaces:**
- Consumes: `config.Share.Chatbot`（Task 1）；`constant.AppRobotAdmin`（值 4）；`tablerelation.User`（现有，字段含 `UserID`、`Nickname`、`FaceURL`、`AppMangerLevel`）。

- [ ] **Step 1: 追加机器人账号到初始化列表**

在 `internal/rpc/user/user.go` 的 `Start` 中，`for _, v := range config.Share.IMAdminUserID { ... }` 循环之后追加：

```go
	if config.Share.Chatbot.Enable && config.Share.Chatbot.UserID != "" {
		users = append(users, &tablerelation.User{
			UserID:         config.Share.Chatbot.UserID,
			Nickname:       config.Share.Chatbot.Nickname,
			FaceURL:        config.Share.Chatbot.FaceURL,
			AppMangerLevel: constant.AppRobotAdmin,
		})
	}
```

- [ ] **Step 2: 构建验证**

Run: `go build ./...`
Expected: 成功。

- [ ] **Step 3: Commit**

```bash
git add internal/rpc/user/user.go
git commit -m "feat(user): auto create chatbot account on startup"
```

---

## 手动验收（实现完成后）

1. `config/share.yml` 设 `chatbot.enable: true`、`userID: openIMChatbot`、`callbackURL` 指向可接收 POST 的服务，重启 `openim-rpc-user` 与 `openim-rpc-msg`。
2. 确认用户表已存在 `openIMChatbot`（`AppMangerLevel=4`）。
3. 普通用户加机器人为好友并单聊发一条文本 → 回调服务收到 `callbackAfterSendMsgToBotCommand`，`recvID=openIMChatbot`、`sessionType=1`、`content` 为文本。
4. 群内 @机器人发消息 → 回调服务收到，`groupID` 为群 ID、`sessionType=3`。
5. 以 app manager token 调 `POST /msg/send_msg`，`sendID=openIMChatbot`、`recvID=用户` 发回复 → 用户收到，且**不产生二次回调**（`sendID==bot` 被跳过）。
6. 停掉回调服务再发消息 → 用户发送成功（仅日志 `ZWarn`），主链路不受影响。

## Spec 覆盖自检

- 配置（Chatbot + share.yml）→ Task 1 ✅
- 命中判定 / 单聊 / 群@ / 防回环 / typing / deniedTypes → Task 2 ✅
- 回调载荷 + command → Task 3 ✅
- 触发接入 + 独立 webhook client + 异步不阻塞 → Task 4 ✅
- 机器人账号自动创建（AppRobotAdmin）→ Task 5 ✅
- AI 后端复用现有 `/msg/send_msg` 回复 → 手动验收步骤 5（无需新代码）✅
