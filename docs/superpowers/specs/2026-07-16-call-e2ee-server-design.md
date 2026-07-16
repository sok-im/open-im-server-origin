# 音视频通话 E2EE 服务端设计

> 日期：2026-07-16  
> 状态：待实现  
> 依据：`音视频通话E2EE-服务端接口与实施方案.md`、`音视频通话端到端加密（E2EE）实施计划.md` 第 5 章  
> 仓库范围：本 OpenIM Server（`internal/rpc/rtc`、`internal/rpc/openmls`、`protocol`、存储层）

## 1. 目标与兼容策略

**目标：** 在服务端落地通话 E2EE 控制面能力：描述符透传、`conversationID` 规范化、能力声明与 Token 门禁、自定义信令桥接强化、OpenMLS Commit CAS、群成员移除时 LiveKit 踢人；**不**接触媒体密钥。

**兼容策略（已确认：方案 A — 按描述符门禁）：**

- `customData` 为空或不含 `e2ee.required=true` → 走现有非 E2EE 路径，行为与现网一致。
- 仅当邀请落库后 `e2ee.required=true` → 启用能力/版本门禁、短 TTL JWT、JWT 元数据 `e2ee=true`、强化成员校验。
- 服务端**绝不**把 E2EE 房间静默降级为非 E2EE；旧客户端进不了 E2EE 房间，但仍可发起普通通话。

**安全红线：**

1. 不生成、保存、下发 `K_media` 或可派生材料。
2. 不解密 `mlsMessage`，只透明转发。
3. 日志/JWT/Webhook/推送不得出现密钥、密钥承诺值、MLS 密文明文。
4. E2EE 房间禁用服务端录制/Egress/转码/转写/混流/SIP（建房/发 Token 时写入非秘密元数据；LiveKit 侧策略配合）。

## 2. 架构与职责

| 组件 | 职责 |
|---|---|
| `internal/rpc/rtc` | 邀请/接听/加入/取 Token/取房间/启动恢复；描述符落库与回传；Token 门禁；自定义信令校验与转发；群通话踢人 |
| `internal/rpc/openmls` | `SubmitCommit` CAS + 幂等 + 4010；Commit 历史 |
| `internal/rpc/group` | 踢人/退群后已有 MLS Remove trigger；本期补「有进行中群通话 → RemoveParticipant」联动 |
| LiveKit | 仅转发密文帧；JWT 短 TTL；`RemoveParticipant` |

实现路径：**增量挂接现有 `signal.go` / openmls**（不新建独立 E2EE 微服务）。

## 3. 数据流

### 3.1 写路径（邀请）

1. 客户端 `customData`（含 `e2ee` JSON）原样写入 `SignalInvitation.CustomData`。
2. 服务端计算规范化 `conversationID`：
   - 单聊：复用 `msgprocessor` / `conversationutil` 的 `si_` 排序规则；
   - 群聊：`groupID`。
3. 解析一次 `e2ee.required`（及可选 `call_id`）写入扩展字段，便于门禁。
4. 幂等：同一 `roomID` 重复创建返回已存描述符，不改写 `roundID`/`generation`。
5. 响应显式回传 `conversationID`；E2EE 房间在需要发 Token 时走门禁逻辑。

### 3.2 读路径

Accept / Join / GetTokenByRoomID / GetRoomByGroupID / GetInvitationInfoStartApp / 离线推送恢复：

- 从 DB 原样回传 `e2ee`（从 `customData` 中抽出 `e2ee` 对象序列化，或约定回传与写入一致的描述符 JSON 字符串）。
- 回传规范化 `conversationID`。

### 3.3 Token 门禁（仅 `e2ee_required=true`）

按序校验：

1. `e2eeCapability` 存在，且 `schemes` 含允许方案、`version` ≥ 最低版本、`frameCryptor=true`。
2. 单聊：请求者 ∈ `{inviter} ∪ invitees`。
3. 群聊：实时查群成员表/RPC（权威源），禁止仅用本地 cache 做最终放行。
4. JWT：`SetValidFor` ≤ 5 分钟；可写非秘密 metadata `e2ee=true`；禁止密钥字段。
5. 续期：现有 `GetTokenByRoomID` 即续期路径。

失败返回 §6 错误码，附 `minClientVersion` / `requiredSchemes`（如适用）。

## 4. 存储模型

### 4.1 `SignalInvitation` 扩展

| 字段 | 类型 | 说明 |
|---|---|---|
| `custom_data` | string | 已有；完整不透明 JSON |
| `conversation_id` | string | 新增；规范化会话 ID |
| `e2ee_required` | bool | 新增；门禁开关 |
| `call_id` | string | 新增可选；排查/幂等 |

不对描述符内部字段建二级索引。

### 4.2 自定义信令去重

- Redis：`rtc:custom_signal:{roomID}:{messageID}`，`SET NX` + TTL（跟随 invitation 生命周期）。
- 不建独立 Mongo 表（可丢、可重建）。

### 4.3 `MLSCommit` 扩展

| 字段 | 说明 |
|---|---|
| `from_epoch` | 源 epoch |
| `commit_hash` | 去重/校验 |
| `idempotency_key` | 幂等键 |

唯一约束：`(group_id, from_epoch)`；`idempotency_key`（非空）唯一。

## 5. 接口变更要点

### 5.1 信令（A1–A8）

协议字段（`protocol/rtc`）已部分就绪：`E2EECapability`、`conversationID`、响应 `e2ee`。

业务落地：

- Invite / InviteInGroup：落库扩展字段；响应填 `conversationID`。
- Accept / Join / GetToken / GetRoom / StartApp：回传 `conversationID` + `e2ee`。
- GetToken：E2EE 房间执行门禁 + 短 TTL。

### 5.2 自定义信令（B1）

强化现有 `SignalSendCustomSignal`：

| 校验 | 规则 | 失败 |
|---|---|---|
| 房间 | invitation 存在且未结束 | 拒绝 |
| 成员 | 发送者合法（单聊名单 / 群成员权威查询） | 拒绝 |
| 大小 | `customInfo` ≤ 16KB | 拒绝 |
| 限流 | 每用户每房间 10/s，突发 20 | 拒绝/限流 |
| 去重 | `messageID` Redis NX | 静默成功、不转发 |
| 透传 | 不解密；补 `senderUserID`、`senderPlatformID`、`serverSeq` | — |

非 E2EE 自定义信令只要通过成员/大小/限流仍可转发（兼容）。

### 5.3 `SubmitCommit`（C1）

1. `idempotencyKey` 命中 → `accepted=true, duplicate=true`，回传原 `acceptedEpoch`/`commitID`，不推进 epoch。
2. `fromEpoch == current` → CAS +1，落库，`accepted=true, acceptedEpoch=fromEpoch+1`。
3. 否则 → 业务错误码 **4010**，`expectedFromEpoch=current`，`accepted=false`。
4. 保留旧字段 `newEpoch` / `sequenceNumber`，兼容老客户端。

### 5.4 群成员移除联动

已有：`KickGroupMember` / `QuitGroup` → `openMLSClient.RemoveMemberTrigger`。

本期补：若 `GetInvitationByGroupID` 命中进行中房间 → LiveKit `RemoveParticipant`。

不实现 JWT 黑名单；后续准入靠成员校验 + 短 TTL。

## 6. 错误码

建议放在通话错误段（18xx）：

| 码 | 常量名 |
|---|---|
| 1830 | `CALL_E2EE_REQUIRED_UNSUPPORTED` |
| 1831 | `CALL_E2EE_CONVERSATION_NOT_READY` |
| 1832 | `CALL_E2EE_GROUP_MEMBERSHIP_INVALID` |
| 1833 | `CALL_E2EE_PROTOCOL_VERSION_MISMATCH` |
| 1834 | `CALL_E2EE_TOKEN_DENIED` |
| 4010 | OpenMLS epoch conflict（文档指定） |

允许列表（可配置，默认）：`scheme=mls-exporter-livekit-v1`，`version>=1`，`frameCryptor=true`。

## 7. 明确不在本期

- Flutter / OpenIMSDK / gomobile 桥接（客户端任务）。
- 服务端生成或分发媒体密钥。
- 全量强制所有通话 E2EE。
- JWT 黑名单。
- 独立 E2EE 微服务。

## 8. 验收清单

- [ ] 无 `e2ee.required` 的通话与现网一致。
- [ ] E2EE 房间：无能力/低版本拒 Token；描述符原样回传；`conversationID` 规范化。
- [ ] 自定义信令：授权、16KB、限流、去重生效；密文不入日志。
- [ ] `SubmitCommit`：幂等 + 4010 CAS；旧响应字段仍填充。
- [ ] 踢人：`RemoveParticipant` + 后续拒发 Token。
- [ ] 审计：无 `K_media` 及相关秘密材料泄漏路径。

## 9. 参考

- `音视频通话E2EE-服务端接口与实施方案.md`
- `音视频通话端到端加密（E2EE）实施计划.md` §5、任务 6
- 现有实现：`internal/rpc/rtc/signal.go`、`internal/rpc/openmls/service.go`
