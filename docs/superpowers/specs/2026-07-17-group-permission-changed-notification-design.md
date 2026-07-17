# 群权限变更通知设计

> 日期：2026-07-17  
> 状态：已确认，待实现  
> 范围：`protocol`、`internal/rpc/group`、`pkg/notification`、`config`、`openim-sdk-core`（submodule）

## 1. 目标

当以下四个群权限设置**实际发生变化**后，向群内成员发送专用通知，并在 SDK 触发 `onGroupPermissionChanged`：

| API 字段名 | 服务端字段 | 入口 |
|---|---|---|
| `allowSendMsg` | `AllowSendMsg` | `SetSendMessageSetting` / `SetGroupInfo` / `SetGroupInfoEx` |
| `allowAddMember` | `AllowAddMember` | `SetInviteSetting` → `SetGroupInfoEx`；或 `SetGroupInfo` / `SetGroupInfoEx` |
| `allowPinMsg` | `AllowPinMsg` | `SetPinSetting` → `SetGroupInfoEx`；或 `SetGroupInfo` / `SetGroupInfoEx` |
| `allowMemberBurn` | `AllowBurn` | `SetBurnSetting` → `SetGroupInfoEx`；或 `SetGroupInfo` / `SetGroupInfoEx` |

**不在本期范围：** `AllowEditGroupInfo`、`EnableInviteLink`、`NeedVerification`、`MsgBurnDuration` 等其它群设置；它们继续走现有通知。

## 2. 现状与问题

- `SetGroupInfo` / `SetGroupInfoEx`：权限字段写入后会设 `normalFlag`，最终发 `GroupInfoSetNotification`（1502），SDK 走通用 `OnGroupInfoChanged`。
- `SetSendMessageSetting`：更新 `allow_send_msg` 后发 `GroupMutedNotification` / `GroupCancelMutedNotification`（1514/1515），语义是「全员禁言」，与「仅管理员可发消息」权限设置不对齐，且不会触发权限专用回调。
- 客户端需要专用 `onGroupPermissionChanged`，不能仅靠复用 1502。

## 3. 方案选择（已确认）

| 方案 | 结论 |
|---|---|
| 新增统一 `GroupPermissionChangedNotification` | **采用**：一个 contentType、一次合并发送、`changedFields` 标明变更项 |
| 四个独立通知 | 拒绝：协议与处理器重复，批量改权限会产生多条消息 |
| 仅复用 `GroupInfoSetNotification` | 拒绝：无法提供专用 listener 回调 |

## 4. 协议

### 4.1 contentType

在 `protocol/constant/constant.go` 增加：

```go
GroupPermissionChangedNotification = 1530
```

（当前群通知段已用到 1529；1530 为空闲号。）

### 4.2 Tips

在 `protocol/sdkws/sdkws.proto` 增加：

```protobuf
// OnGroupPermissionChanged()
message GroupPermissionChangedTips {
  GroupMemberFullInfo opUser = 1;
  GroupInfo group = 2;
  // changedFields: API 语义名，如 allowSendMsg / allowAddMember / allowPinMsg / allowMemberBurn
  repeated string changedFields = 3;
  int64 operationTime = 4;
  uint64 groupMemberVersion = 5;
  string groupMemberVersionID = 6;
}
```

`changedFields` 固定使用 API 名（与 HTTP JSON 一致）；服务端内部 `AllowBurn` 映射为 `allowMemberBurn`。

### 4.3 通知配置

与 `groupBurnDurationSet` 同级：

- `config/notification.yml`：`groupPermissionChanged`
- `pkg/common/config`：`Notification.GroupPermissionChanged`
- `pkg/notification/msg.go`：contentType → config / sessionType（`ReadGroupChatType`）

推荐默认：

```yaml
groupPermissionChanged:
  isSendMsg: true
  reliabilityLevel: 2
  unreadCount: false
  offlinePush:
    enable: false
    title: group permission updated
    desc: group permission updated
    ext: group permission updated
```

## 5. 服务端写路径

### 5.1 变更检测

更新 DB **之前**读取当前群信息，更新成功后再次 `TakeGroup`，对四个字段做前后对比：

- 值未变 → 不加入 `changedFields`
- `changedFields` 为空 → **不发送**权限变更通知
- 请求未携带某字段 → 该字段不参与对比

### 5.2 发送时机与去重

在 DB 更新成功、拿到最新 `GroupInfo` 后发送一条通知：

| 入口 | 行为 |
|---|---|
| `SetSendMessageSetting` | 值变化时发 `GroupPermissionChangedNotification`；**不再**发 `GroupMuted` / `GroupCancelMuted` |
| `SetGroupInfo` / `SetGroupInfoEx` | 权限实际变化 → 发权限通知；若同请求还更新了其它「普通」字段（introduction/ex/lookMemberInfo 等）→ 另发 `GroupInfoSetNotification`；群名/公告/头像/入群审核/阅后即焚时长仍走各自专用通知 |

实现要点：

- 从 `UpdateGroupInfoExMap` / `SetGroupInfo` 的 `normalFlag` 计数中**剥离**四个权限字段，避免权限变更再触发一次通用 `GroupInfoSetNotification`。
- 权限通知 payload 中的 `group` 为更新后的完整 `GroupInfo`（含全部权限当前值）。
- 一次请求多字段变化 → **一条**通知，`changedFields` 含所有实际变化名。

### 5.3 通知发送器

在 `internal/rpc/group/notification.go` 增加 `GroupPermissionChangedNotification`，风格对齐 `GroupNeedVerificationSetNotification`：

1. `fillOpUser`
2. `setVersion`（group member version）
3. `Notification(..., 1530, tips)`

### 5.4 失败边界

- DB 更新失败：不发通知，接口返回错误。
- DB 成功、通知发送失败：沿用现有异步通知语义，**不回滚** DB；日志带 `groupID`、`opUserID`、`changedFields`。
- 群已解散：现有校验拦截，不发通知。

## 6. SDK（`openim-sdk-core` submodule）

1. 初始化 submodule：`git submodule update --init openim-sdk-core`（必要时同步最新 `protocol` 依赖）。
2. 同步常量 `1530` 与 `GroupPermissionChangedTips`（与 protocol 对齐）。
3. 群通知分发中识别 `GroupPermissionChangedNotification`：
   - 用 tips 中的 `group` 更新本地群信息缓存；
   - 再调用 listener：`OnGroupPermissionChanged(tips)` / 绑定层 `onGroupPermissionChanged`。
4. 旧版 SDK：未知 contentType 忽略，不影响收发主链路。

## 7. 数据流（摘要）

```
客户端 SetXxxSetting / SetGroupInfo(Ex)
  → API → group RPC
  → 鉴权（仅群主/管理员可改权限）
  → TakeGroup（旧值）
  → UpdateGroup（权威源）
  → TakeGroup（新值）
  → diff 四个权限 → changedFields
  → [changedFields 非空] Notification 1530 → msgtransfer → 群成员在线/可靠投递
  → SDK 更新本地 GroupInfo → onGroupPermissionChanged
```

权威源仍是 DB 中的群文档；通知与本地 cache 均为衍生数据。

## 8. 测试计划

| 用例 | 期望 |
|---|---|
| 单独改 `allowSendMsg` / `allowAddMember` / `allowPinMsg` / `allowMemberBurn` | 各发一条 1530，`changedFields` 仅含对应名 |
| 一次 `SetGroupInfoEx` 改多个权限 | 一条 1530，`changedFields` 含多项 |
| 请求值与 DB 相同 | 不发 1530 |
| 同时改 introduction + allowPinMsg | 一条 1530 + 一条 1502 |
| `SetSendMessageSetting` | 发 1530，不发 1514/1515 |
| SDK | 解码 tips、更新缓存后触发 `OnGroupPermissionChanged` |

## 9. 非目标 / 明确不做

- 不为每个权限新增独立 contentType。
- 不把权限变更塞进 `msggateway` 业务逻辑。
- 不在同步写路径做离线推送业务（配置默认 `offlinePush.enable: false`）。
- 本期不改 `AllowEditGroupInfo` / 邀请链接等其它设置的通知语义。
