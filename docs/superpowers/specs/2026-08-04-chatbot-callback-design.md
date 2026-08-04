# 客服机器人（AI 智能客服）设计文档

## 背景与目标

需要在 OpenIM 中增加「客服机器人」能力：用户可以像和普通用户聊天一样，和一个客服机器人对话，由 AI 生成回答。

核心定位：**AI 智能客服**，且 AI 推理（LLM 调用、知识库检索、回答生成）在**外部 AI 后端**完成。OpenIM 只负责：

1. 提供一个机器人 IM 账号身份
2. 把「用户发给机器人的消息」异步回调给外部 AI 后端
3. 机器人的回复走现有发消息接口发回给用户

设计遵循仓库分层原则：**不把 LLM 逻辑塞进 IM**、同步路径只做「校验 → 持久化 → 发事件 → ACK」、回调走异步且失败不拖垮聊天主链路。

## 设计决策（已确认）

| 决策点 | 结论 |
|--------|------|
| 机器人定位 | AI 智能客服 |
| AI 推理位置 | 外部 AI 后端，通过 webhook 回调解耦 |
| 机器人身份 | **全局单个**机器人账号，配置驱动，无管理接口 |
| 触发范围 | 单聊发给机器人（`recvID==botUserID`）+ 群内 @机器人 |
| 回复方式 | 异步回调；AI 后端调用现有发消息接口以机器人身份回复 |
| 账号创建 | 服务启动时按配置**自动创建**（幂等） |
| 触发机制 | 方案 A：在 `rpc/msg` 发送路径内做专用机器人回调 |

### 方案选型（为何选 A）

- **方案 A（选定）**：在 `sendMsgSingleChat` / `sendMsgGroupChat` 的 `MsgToMQ` 成功后，判定是否命中机器人，命中则异步 POST。精准、最小侵入、复用现有异步 webhook 设施，单聊与群 @ 都能覆盖。
- 方案 B（复用现有 `afterSendSingleMsg`，外部自过滤）：OpenIM 零改动，但所有单聊消息全量外发、噪声大，且无法处理群内 @机器人。否决。
- 方案 C（独立机器人 RPC 服务消费 Kafka）：与发送路径解耦但引入常驻服务，对「单个客服机器人」是过度设计。否决。

## 架构与数据流

```
用户 --(单聊发给bot / 群内@bot)--> msggateway --> rpc/msg SendMsg
                                                     |
                                                     |-- MsgToMQ(Kafka)  [主链路照常，先 ACK]
                                                     |
                                                     `-- 命中机器人? --是--> 异步 POST 回调 --> 外部 AI 后端(LLM/知识库)
                                                                                                    |
用户 <-- 正常消息链路(持久化/推送/多端同步) <-- OpenIM /msg/send_msg <-- 以机器人身份回复 --------------'
```

关键点：

- 回调在 `MsgToMQ` **成功之后**触发（消息已确保入队），异步执行，**失败不影响用户发消息**。
- 机器人回复是普通消息，走完整持久化/推送链路，用户多端同步、离线推送自动生效。
- **防回环**：机器人自己发出的消息（`sendID==botUserID`）绝不触发回调；机器人回复的 `recvID=用户`，天然不满足「recvID==bot」，不会二次触发。

## 配置

配置放在 `share.yml`（`config.Share`），因为需要被两个服务读取：`openim-rpc-user`（启动自动建号）与 `openim-rpc-msg`（触发回调），二者都已加载 share.yml。

新增 `config.Chatbot` 结构体并挂到 `Share`：

```yaml
# share.yml 新增
chatbot:
  enable: true                                    # 总开关
  userID: "openIMChatbot"                          # 机器人账号 userID
  nickname: "在线客服"
  faceURL: ""
  callbackURL: "http://your-ai-backend/callback"   # 为空则回退到 webhooks.yml 的全局 url
  timeout: 5                                        # 回调超时(秒)
  deniedTypes: [1201]                              # 不回调的 contentType（typing 已内置忽略）
```

```go
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

## 机器人账号自动创建

在 `internal/rpc/user/user.go` 的 `Start` 中，仿照 `IMAdminUserID` 的写法，把机器人追加进 `users` 初始化列表，`AppMangerLevel = AppRobotAdmin(4)`（复用协议中预留但从未使用的常量）：

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

`InitOnce` 幂等，重启不会重复建号。机器人是真实用户账号：用户可加好友、单聊；也可被邀请进群（进群操作由管理端/运维完成，属现有能力）。

用 `AppRobotAdmin` 而非普通用户：语义清晰，便于区分机器人身份，且 AI 后端以机器人身份发消息时可走 app manager 发送路径。

## 触发逻辑与回调载荷

### 触发点

在 `internal/rpc/msg/send.go` 两处 `MsgToMQ` 成功之后各加一行调用：

- `sendMsgSingleChat`：在 `webhookAfterSendSingleMsg` 之后调 `m.webhookAfterSendMsgToChatBot(ctx, req)`
- `sendMsgGroupChat`：在 `webhookAfterSendGroupMsg` 之后调同一方法

### 命中判定

新文件 `internal/rpc/msg/chatbot.go`，判定逻辑为纯函数，便于单测：

```go
func chatbotHit(cfg config.Chatbot, msg *sdkws.MsgData) bool {
    if !cfg.Enable || cfg.UserID == "" { return false }
    if msg.SendID == cfg.UserID { return false }              // 防回环：机器人自己发的
    if msg.ContentType == constant.Typing { return false }     // 忽略正在输入
    if datautil.Contain(msg.ContentType, cfg.DeniedTypes...) { return false }
    switch msg.SessionType {
    case constant.SingleChatType:
        return msg.RecvID == cfg.UserID                          // 单聊发给机器人
    case constant.ReadGroupChatType:
        return datautil.Contain(cfg.UserID, msg.AtUserIDList...)  // 群内 @机器人
    default:
        return false
    }
}
```

命中后用**独立 webhook client**（`callbackURL` 非空时用它，否则回退全局 URL）异步 POST。`msgServer` 增加 `chatbotWebhook *webhook.Client` 字段并在 `Start` 中初始化。

### 回调载荷

新增 `pkg/callbackstruct` 结构 `CallbackAfterSendMsgToBotReq/Resp` 与 command 常量 `CallbackAfterSendMsgToBotCommand`，复用现有 `toCommonCallback` 再补充路由字段：

```json
{
  "callbackCommand": "callbackAfterSendMsgToBotCommand",
  "sendID": "user_123",
  "recvID": "openIMChatbot",
  "groupID": "",
  "sessionType": 1,
  "contentType": 101,
  "content": "你们几点上班？",
  "clientMsgID": "...",
  "serverMsgID": "...",
  "seq": 42,
  "senderNickname": "张三",
  "atUserIDList": ["openIMChatbot"],
  "sendTime": 1730000000000,
  "operationID": "..."
}
```

单聊时 `recvID`=机器人、`groupID`=空；群聊时 `groupID`=群ID、`recvID`=空。AI 后端据 `sessionType` 决定回复目标：单聊回 `recvID=原 sendID`，群聊回 `groupID`。

### AI 后端回复（复用现有接口，无需新增）

AI 后端以 app manager token 调用现有 `POST /msg/send_msg`：`sendID=机器人userID`，单聊 `recvID=用户`、群聊 `groupID=群`。这条回复走正常链路，也会经过 `chatbotHit`，但因 `sendID==botUserID` 被直接跳过，不回环。

## 错误处理与容错

- 回调用 `AsyncPost`：机器人/AI 后端不可达、超时、5xx 都**不影响用户发消息**（主链路已在 `MsgToMQ` 成功后 ACK）。
- 失败仅打 `log.ZWarn`，带 `sendID/recvID/groupID/clientMsgID/seq/operationID`，符合「关键 ID 入日志」约定。
- **至少一次语义**：网络抖动可能重复回调，AI 后端应按 `clientMsgID` 幂等去重（属对方职责，写入对接文档）。
- 群聊未 @机器人、机器人非群成员：不触发（基于 @列表判定，与成员关系解耦）。

## 可观测性

复用现有 webhook 的 prometheus 计数；本轮不新增指标（YAGNI）。后续如需可加 `ChatbotCallbackCounter`。

## 测试

- 单测 `chatbotHit`（`internal/rpc/msg/chatbot_test.go`）：单聊命中/不命中、群 @命中/未 @、防回环（`sendID==bot`）、`enable=false`、typing/`deniedTypes` 过滤、未知 sessionType。
- 复用现有 webhook client（已被其他回调覆盖），不重复测网络层。

## 改动清单（最小侵入）

| 文件 | 改动 |
|------|------|
| `pkg/common/config/config.go` | 新增 `Chatbot` struct，挂到 `Share` |
| `config/share.yml` | 新增 `chatbot:` 配置段 |
| `internal/rpc/user/user.go` | `Start` 里按配置自动建机器人账号（`AppRobotAdmin`）|
| `pkg/callbackstruct/*`（新增或复用 msg.go）| `CallbackAfterSendMsgToBotReq/Resp` + command 常量 |
| `internal/rpc/msg/server.go` | `msgServer` 加 `chatbotWebhook` 字段并初始化 |
| `internal/rpc/msg/chatbot.go`（新）| `chatbotHit` 判定 + `webhookAfterSendMsgToChatBot` 触发 |
| `internal/rpc/msg/send.go` | 单聊/群聊各加一行触发调用 |
| `internal/rpc/msg/chatbot_test.go`（新）| `chatbotHit` 单测 |

## 明确不做（YAGNI）

- 多机器人管理接口、机器人 RPC 服务
- 群内非 @ 的全量消息转发
- LLM / 知识库逻辑（在外部 AI 后端）
- 同步等待回复
- 会话转人工坐席
