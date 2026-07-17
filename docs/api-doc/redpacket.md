# 红包（RedPacket）API 接口文档

**Base URL:** `/redpacket`
**协议:** HTTP POST，`Content-Type: application/json`
**认证:** 请求头携带 `token: <JWT令牌>`（标注“需要登录”的接口必须提供；`/redpacket/admin/*` 需要管理员令牌）

> **统一响应结构**
>
> ```json
> {
>   "errCode": 0,
>   "errMsg": "ok",
>   "errDlt": "",
>   "data": { }
> }
> ```
>
> `errCode` 为 `0` 表示成功，非 0 表示错误，`errDlt` 为错误详情。以下各接口的“响应体”仅展开 `data` 字段内容。

> **多链路由约定（chainKey / chainType）**
>
> 绝大多数接口都接受 `chainKey` 与 `chainType` 两个可选字段用于定位目标链：
>
> - `chainKey`：链实例的唯一标识键，服务端按它精确路由到对应链运行时（优先级最高）。
> - `chainType`：链类型（`EVM` / `TRON`）。当 `chainKey` 为空时，按 `chainType` 匹配；若同类型配置了多条链则会因歧义报错。
> - **二者至少提供其一**；同时提供时会校验 `chainKey` 与 `chainType` 是否一致。
>
> **例外:** 退款相关接口（`request_refund` / `refund_callback` / `get_refund`）**只接受 `chainKey` 且为必填**，不再接受 `chainType`——避免冗余字段带来歧义。

---

## 接口总览

| # | 接口 | 路径 | 登录 | 说明 |
|---|------|------|:----:|------|
| 1 | 创建红包订单 | `POST /redpacket/create_order` | ✅ | 生成待上链订单，返回 bizID |
| 2 | 红包创建回调 | `POST /redpacket/created_callback` | ✅ | 回传创建交易 hash，激活红包 |
| 3 | 查询红包详情 | `POST /redpacket/detail` | — | 红包信息 + 领取记录 |
| 4 | 申请领取签名 | `POST /redpacket/issue_claim_sign` | ✅ | 领取前获取服务端授权签名 |
| 5 | 提交领取结果 | `POST /redpacket/claim_result` | ✅ | 回传领取交易 hash，确认领取 |
| 6 | ~~申请退款（后端代发）~~ | ~~`POST /redpacket/request_refund`~~ | — | **暂不开放**（路由已隐藏），改用 #7 |
| 7 | 退款回调（用户自退） | `POST /redpacket/refund_callback` | ✅ | 回传用户自发的退款交易 hash |
| 8 | 查询退款记录 | `POST /redpacket/get_refund` | — | 查询某红包的退款结果 |
| 9 | 发起钱包绑定挑战 | `POST /redpacket/wallet_bind/challenge` | ✅ | 生成待签名消息 |
| 10 | 确认钱包绑定 | `POST /redpacket/wallet_bind/confirm` | — | 提交签名完成绑定 |
| 11 | 查询钱包绑定 | `POST /redpacket/wallet_bind/detail` | ✅ | 查询当前用户钱包绑定 |
| 12 | 设置签名者 | `POST /redpacket/admin/set_signer` | 🔒 | 管理员：设置合约签名者 |
| 13 | 设置代币白名单 | `POST /redpacket/admin/set_token` | 🔒 | 管理员：配置可用代币 |
| 14 | 设置过期时长 | `POST /redpacket/admin/set_expiry` | 🔒 | 管理员：设置默认过期时长 |
| 15 | 允许所有代币 | `POST /redpacket/admin/set_allow_all_tokens` | 🔒 | 管理员：开关全代币放行 |
| 16 | 原生代币开关 | `POST /redpacket/admin/set_native_token_enabled` | 🔒 | 管理员：开关原生代币 |
| 17 | 解析交易事件 | `POST /redpacket/admin/parse_tx_events` | 🔒 | 管理员：解析任意 tx 的链上事件 |

> 🔒 = 需要管理员令牌（`authverify.CheckAdmin`）。

---

## 1. 创建红包订单

**POST** `/redpacket/create_order` — 需要登录。

创建一条待上链的红包订单，返回业务 ID（bizID）供后续链上交易关联。

### 请求体

```json
{
  "chainType": "EVM",
  "chainID": 1,
  "contractAddress": "0xAbCd...",
  "creatorWallet": "0x1234...",
  "groupID": "group_001",
  "scopeType": "GROUP",
  "receiverUserID": "",
  "receiverUserIDs": [],
  "packetType": 0,
  "token": "0x0000000000000000000000000000000000000000",
  "totalAmount": "1000000000000000000",
  "totalShares": 10,
  "expiryAt": 1800000000,
  "remark": "新年快乐",
  "transactionType": "RED_PACKET",
  "chainKey": ""
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `chainType` | string | 条件必填 | 链类型 `EVM` / `TRON`；与 `chainKey` 至少提供其一 |
| `chainKey` | string | 条件必填 | 链标识键；与 `chainType` 至少提供其一 |
| `chainID` | int64 | 可选 | 链 ID；为 0 时从链客户端自动获取 |
| `contractAddress` | string | 可选 | 合约地址；为空时从链客户端自动获取 |
| `creatorWallet` | string | **必填** | 创建者钱包地址 |
| `scopeType` | string | 可选 | `GROUP` / `DIRECT` / `PUBLIC`，默认 `PUBLIC` |
| `groupID` | string | **GROUP 时必填** | 群组 ID（`scopeType=GROUP` 时必须提供） |
| `receiverUserID` | string | **transfer 时必填** | 接收者用户 ID（`packetType=2` 且 `scopeType=DIRECT` 时必填） |
| `receiverUserIDs` | []string | **DIRECT + 均分/随机 时必填** | 多接收者用户 ID 列表（`scopeType=DIRECT` 且 `packetType=0/1` 时使用） |
| `packetType` | int32 | **必填** | `0`=均分，`1`=随机，`2`=转账 |
| `token` | string | 可选 | ERC20 代币合约地址；为空表示原生代币 |
| `totalAmount` | string | **必填** | 总金额（最小单位整数字符串，如 wei），须为正整数 |
| `totalShares` | int32 | **必填（0/1）** | 红包份数；均分/随机 >0 且 ≤10000；转账固定为 1 |
| `expiryAt` | int64 | 可选 | 过期时间（Unix 秒）；0 表示不过期；须为将来时间 |
| `remark` | string | 可选 | 备注 |
| `transactionType` | string | 可选 | 业务类型：`RED_PACKET`（默认）/ `TRANSFER`，用于区分 DIRECT 场景下的个人转账与个人红包 |

**packetType 规则说明：**

- `0`（均分）：`scopeType` 必须为 `GROUP`，`totalAmount` 必须能被 `totalShares` 整除
- `1`（随机）：`scopeType` 必须为 `GROUP`，`totalAmount` ≥ `totalShares`
- `2`（转账）：`scopeType` 必须为 `DIRECT`，`totalShares` 必须为 1，`receiverUserID` 必填，且不能转给自己

### 响应体（data）

```json
{ "bizID": "550e8400-e29b-41d4-a716-446655440000" }
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `bizID` | string | 业务订单 ID，链上交易完成后用于提交回调 |

---

## 2. 红包创建回调（链上确认）

**POST** `/redpacket/created_callback` — 需要登录。仅创建者可调用。

链上创建交易广播后，由创建者提交交易哈希；服务端解析回执确认后将红包状态置为 `ACTIVE`。

### 请求体

```json
{
  "bizID": "550e8400-e29b-41d4-a716-446655440000",
  "txHash": "0xabc123...",
  "packetID": "",
  "groupID": "",
  "scopeType": "",
  "receiverUserID": "",
  "receiverUserIDs": [],
  "chainKey": ""
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `bizID` | string | **必填** | 创建订单时返回的业务 ID |
| `txHash` | string | **必填** | 创建交易哈希 |
| `packetID` | string | 条件必填 | 链上红包 ID；链客户端离线时必须手动提供 |
| `groupID` | string | 可选 | 覆盖订单的群组 ID（不填则继承） |
| `scopeType` | string | 可选 | 覆盖订单的范围类型 |
| `receiverUserID` | string | 可选 | 覆盖单一接收者 |
| `receiverUserIDs` | []string | 可选 | 覆盖多接收者列表 |
| `chainKey` | string | 可选 | 链标识键 |

### 响应体（data）

```json
{}
```

---

## 3. 查询红包详情

**POST** `/redpacket/detail`

### 请求体

```json
{ "packetID": "12345", "chainType": "EVM", "chainKey": "" }
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `packetID` | string | **必填** | 链上红包 ID |
| `chainType` / `chainKey` | string | 条件必填 | 至少提供其一 |

### 响应体（data）

```json
{
  "record": {
    "bizID": "550e8400-...",
    "chainType": "EVM",
    "packetID": "12345",
    "chainID": 1,
    "contractAddress": "0xAbCd...",
    "creatorUserID": "user_001",
    "creatorWallet": "0x1234...",
    "groupID": "group_001",
    "scopeType": "GROUP",
    "receiverUserID": "",
    "receiverUserIDs": [],
    "packetType": 0,
    "token": "0x0000000000000000000000000000000000000000",
    "totalAmount": "1000000000000000000",
    "totalShares": 10,
    "claimedAmount": "300000000000000000",
    "claimedShares": 3,
    "expiryAt": 1800000000,
    "txHash": "0xabc123...",
    "status": "ACTIVE",
    "createdAt": 1715500000,
    "updatedAt": 1715500100,
    "chainKey": "evm-1",
    "transactionType": "RED_PACKET",
    "decimals": 18,
    "totalAmountDisplay": "1",
    "claimedAmountDisplay": "0.3",
    "remainingAmount": "700000000000000000",
    "remainingAmountDisplay": "0.7",
    "remainingShares": 7
  },
  "claims": [
    {
      "packetID": "12345",
      "userID": "user_002",
      "claimerWallet": "0x5678...",
      "authNonce": "1715500050000000000",
      "claimTxHash": "0xdef456...",
      "claimedAmount": "100000000000000000",
      "blockNumber": 19000000,
      "status": "CONFIRMED",
      "createdAt": 1715500050,
      "updatedAt": 1715500060,
      "claimedAmountDisplay": "0.1"
    }
  ]
}
```

**record 关键字段：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | string | `PENDING` / `ACTIVE` / `COMPLETED` / `REFUNDED` / `EXPIRED` |
| `totalAmount` / `claimedAmount` / `remainingAmount` | string | 金额（最小单位） |
| `*Display` | string | 对应金额的人类可读值（按 `decimals` 换算） |
| `totalShares` / `claimedShares` / `remainingShares` | int32 | 总 / 已领 / 剩余份数 |
| `decimals` | int32 | 代币精度 |

---

## 4. 申请领取签名

**POST** `/redpacket/issue_claim_sign` — 需要登录。

领取红包前先获取服务端授权签名用于链上验证。会校验领取资格（群成员/好友关系、是否已领取、红包状态等）。

### 请求体

```json
{
  "packetID": "12345",
  "claimer": "0x5678...",
  "randomSeed": "",
  "chainType": "EVM",
  "chainKey": ""
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `packetID` | string | **必填** | 链上红包 ID |
| `claimer` | string | **必填** | 领取者钱包地址 |
| `randomSeed` | string | 可选 | 随机种子（十进制整数字符串）；为空或 `"0"` 时服务端自动生成 |
| `chainType` / `chainKey` | string | 条件必填 | 至少提供其一 |

### 响应体（data）

```json
{
  "authNonce": "1715500050000000000",
  "deadline": 1715500350,
  "signature": "0xaabbcc...",
  "randomSeed": "8765309000000"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `authNonce` | string | 认证随机数（传给合约） |
| `deadline` | int64 | 签名过期时间（Unix 秒，约 5 分钟后） |
| `signature` | string | 服务端签名（0x 前缀，65 字节） |
| `randomSeed` | string | 使用的随机种子 |

---

## 5. 提交领取结果

**POST** `/redpacket/claim_result` — 需要登录。

链上领取交易广播后提交，服务端解析链上 `Claimed` 事件并更新领取记录。

### 请求体

```json
{
  "packetID": "12345",
  "claimer": "0x5678...",
  "txHash": "0xdef456...",
  "chainType": "EVM",
  "chainKey": ""
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `packetID` | string | **必填** | 链上红包 ID |
| `claimer` | string | **必填** | 领取者钱包地址 |
| `txHash` | string | **必填** | 领取交易哈希 |
| `chainType` / `chainKey` | string | 条件必填 | 至少提供其一 |

### 响应体（data）

```json
{}
```

---

## 6. 申请退款（后端代发）— 🚧 暂不开放

> **该接口当前已隐藏**：HTTP 路由 `/redpacket/request_refund` 已在 `internal/api/router.go` 中注释停用，handler / RPC / proto 定义均保留，恢复只需取消该行注释。退款请统一走 [#7 退款回调](#7-退款回调用户自退款)（前端自发交易 + 回传 hash）。以下内容仅作恢复后的参考。

**POST** `/redpacket/request_refund` — 需要登录。仅创建者可调用。

红包**到期后**，由后端使用管理员账户发起链上退款交易，将剩余金额退回创建者。gas 由后端承担。适用于前端不便自行发交易的场景。

### 请求体

```json
{ "packetID": "12345", "chainKey": "evm-1" }
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `packetID` | string | **必填** | 链上红包 ID（红包必须已到期） |
| `chainKey` | string | **必填** | 链标识键，唯一定位目标链 |

> 退款相关接口（`request_refund` / `refund_callback` / `get_refund`）统一以 `chainKey` 为准，**不接受 `chainType`**。

### 响应体（data）

```json
{ "txHash": "0xghi789...", "status": "REFUNDED" }
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `txHash` | string | 后端发起的退款交易哈希 |
| `status` | string | `REFUNDED`（已退款并落库）/ `PENDING`（交易已提交，回执待异步确认）/ `FAILED`（链上失败） |

---

## 7. 退款回调（用户自退款）

**POST** `/redpacket/refund_callback` — 需要登录。仅创建者可调用。

当创建者用**自己的钱包**直接调用合约 `refund`（自助退款，gas 自付）后，把退款交易哈希回传给后端。服务端解析 `PacketRefunded` 事件、落库并将红包状态置为 `REFUNDED`。本接口**不发起任何交易**，是 [`claim_result`](#5-提交领取结果) 的退款版对应接口。

> 与 `request_refund` 的区别：`request_refund` 由后端代发退款交易；`refund_callback` 由用户自发交易、仅回传 hash。两者都会正确落库并置 `REFUNDED`，前端按业务二选一即可。

### 请求体

```json
{
  "packetID": "12345",
  "txHash": "0xghi789...",
  "chainKey": "evm-1"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `packetID` | string | **必填** | 链上红包 ID |
| `txHash` | string | **必填** | 用户自发的退款交易哈希 |
| `chainKey` | string | **必填** | 链标识键，唯一定位目标链（不接受 `chainType`） |

### 响应体（data）

```json
{ "txHash": "0xghi789...", "status": "REFUNDED" }
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `txHash` | string | 回传的退款交易哈希（原样返回） |
| `status` | string | `REFUNDED`（解析成功并落库）/ `PENDING`（回执暂未解析，转异步 indexer 兜底）/ `FAILED`（链上交易失败） |

**校验与幂等：**

- 仅红包**创建者本人**可调用（与合约 `_canRefund` 一致）；
- 若红包已是 `REFUNDED` 状态，直接返回 `REFUNDED`（幂等）；
- 服务端会校验解析出的 `PacketRefunded` 事件 `packetID` 与请求一致。

---

## 8. 查询退款记录

**POST** `/redpacket/get_refund`

### 请求体

```json
{ "packetID": "12345", "chainKey": "evm-1" }
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `packetID` | string | **必填** | 链上红包 ID |
| `chainKey` | string | **必填** | 链标识键，唯一定位目标链（不接受 `chainType`） |

### 响应体（data）

```json
{
  "packetID": "12345",
  "refundTo": "0x1234...",
  "txHash": "0xghi789...",
  "amount": "700000000000000000",
  "createdAt": 1715600000,
  "chainKey": "evm-1"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `refundTo` | string | 退款目标钱包地址（合约将剩余金额退回创建者） |
| `amount` | string | 退款金额（最小单位） |
| `createdAt` | int64 | 退款记录创建时间（Unix 秒） |

---

## 9. 发起钱包绑定挑战

**POST** `/redpacket/wallet_bind/challenge` — 需要登录。

生成一条待用户签名的消息，用于将钱包地址绑定到当前用户账户（EVM 使用 EIP-4361 SIWE，TRON 使用 signMessageV2）。

### 请求体

```json
{
  "chainType": "EVM",
  "chainID": 1,
  "walletAddress": "0x5678...",
  "domain": "myapp.example.com",
  "uri": "https://myapp.example.com/wallet-bind",
  "chainKey": ""
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `chainType` | string | 条件必填 | `EVM` / `TRON`；与 `chainKey` 至少其一 |
| `walletAddress` | string | **必填** | 待绑定的钱包地址 |
| `chainID` | int64 | 可选 | 链 ID（EVM 建议提供） |
| `domain` | string | 可选 | 应用域名，默认 `redpacket` |
| `uri` | string | 可选 | 应用 URI，默认 `https://redpacket.local/wallet-bind` |

### 响应体（data）

```json
{
  "challengeID": "aaaa-bbbb-cccc-dddd",
  "userID": "user_001",
  "chainType": "EVM",
  "chainID": 1,
  "wallet": "0x5678...",
  "protocol": "siwe-eip4361",
  "signMethod": "personal_sign",
  "nonce": "xxxx-yyyy-zzzz",
  "message": "myapp.example.com wants you to sign in ...",
  "issuedAt": "2026-05-12T08:39:00Z",
  "expiresAt": "2026-05-12T08:49:00Z",
  "chainKey": "evm-1"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `challengeID` | string | 挑战 ID，确认绑定时使用 |
| `message` | string | 待签名的完整消息文本 |
| `protocol` | string | EVM: `siwe-eip4361`；TRON: `tron-signmessagev2` |
| `signMethod` | string | EVM: `personal_sign`；TRON: `signMessageV2` |
| `expiresAt` | string | 挑战过期时间（RFC3339，10 分钟有效期） |

---

## 10. 确认钱包绑定

**POST** `/redpacket/wallet_bind/confirm`

提交用户对挑战消息的签名，服务端验证后完成钱包绑定。挑战有效期 10 分钟。

### 请求体

```json
{ "challengeID": "aaaa-bbbb-cccc-dddd", "signature": "0xaabbccdd..." }
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `challengeID` | string | **必填** | 挑战 ID（来自 `/wallet_bind/challenge`） |
| `signature` | string | **必填** | 钱包签名（0x 前缀，65 字节） |

### 响应体（data）

```json
{
  "userID": "user_001",
  "chainType": "EVM",
  "chainID": 1,
  "walletAddress": "0x5678...",
  "status": "ACTIVE",
  "verifiedAt": "2026-05-12T08:42:00Z",
  "chainKey": "evm-1"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | string | 绑定状态，成功时为 `ACTIVE` |
| `verifiedAt` | string | 绑定验证时间（RFC3339） |

---

## 11. 查询钱包绑定信息

**POST** `/redpacket/wallet_bind/detail` — 需要登录。

查询当前登录用户在指定链上的活跃钱包绑定。

### 请求体

```json
{ "chainType": "EVM", "walletAddress": "0x5678...", "chainKey": "" }
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `chainType` | string | 条件必填 | `EVM` / `TRON`；与 `chainKey` 至少其一 |
| `walletAddress` | string | 可选 | 钱包地址过滤条件 |

### 响应体（data）

```json
{
  "userID": "user_001",
  "chainType": "EVM",
  "chainID": 1,
  "walletAddress": "0x5678...",
  "status": "ACTIVE",
  "challengeID": "aaaa-bbbb-cccc-dddd",
  "verifiedAt": "2026-05-12T08:42:00Z",
  "chainKey": "evm-1"
}
```

---

## 管理员接口（`/redpacket/admin/*`）

> 以下接口均需**管理员令牌**（`authverify.CheckAdmin`），并会异步写入管理审计日志。

### 12. 设置签名者

**POST** `/redpacket/admin/set_signer`

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `signerAddress` | string | **必填** | 新的合约签名者地址 |
| `chainKey` | string | 条件必填 | 目标链标识键 |

响应：`{ "message": "..." }`

### 13. 设置代币白名单

**POST** `/redpacket/admin/set_token`

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `tokenAddress` | string | **必填** | 代币合约地址 |
| `allowed` | bool | **必填** | 是否允许 |
| `minAmount` | string | 可选 | 最小金额（最小单位） |
| `chainKey` | string | 条件必填 | 目标链标识键 |

响应：`{ "message": "..." }`

### 14. 设置过期时长

**POST** `/redpacket/admin/set_expiry`

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `expirySeconds` | int64 | **必填** | 默认过期时长（秒） |
| `chainKey` | string | 条件必填 | 目标链标识键 |

响应：`{ "message": "..." }`

### 15. 允许所有代币

**POST** `/redpacket/admin/set_allow_all_tokens`

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `allowAll` | bool | **必填** | 是否放行所有代币 |
| `chainKey` | string | 条件必填 | 目标链标识键 |

响应：`{ "message": "..." }`

### 16. 原生代币开关

**POST** `/redpacket/admin/set_native_token_enabled`

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `enabled` | bool | **必填** | 是否启用原生代币 |
| `chainKey` | string | 条件必填 | 目标链标识键 |

响应：`{ "message": "..." }`

### 17. 解析交易事件

**POST** `/redpacket/admin/parse_tx_events`

通用工具：解析任意交易哈希对应的链上事件，可用于手动排障或补录（例如手动确认某笔退款/领取交易）。

### 请求体

```json
{ "chain": "EVM", "txHash": "0xabc123...", "chainKey": "" }
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `chain` | string | 条件必填 | 链类型；与 `chainKey` 至少其一 |
| `txHash` | string | **必填** | 待解析的交易哈希 |
| `chainKey` | string | 条件必填 | 目标链标识键 |

### 响应体（data）

```json
{
  "chain": "EVM",
  "txHash": "0xabc123...",
  "events": [
    { "name": "PacketCreated", "data": { "packetId": "12345", "creator": "0x1234..." } }
  ],
  "note": ""
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `events` | array | 解析出的事件列表，每项含 `name` 与 `data`（键值对） |
| `note` | string | 附加说明（如无事件/部分解析提示） |

---

## 附录：公共枚举值

| 枚举 | 可选值 | 说明 |
|------|--------|------|
| `chainType` / `chain` | `EVM` / `TRON` | 区块链类型 |
| `scopeType` | `GROUP` / `DIRECT` / `PUBLIC` | 红包范围 |
| `packetType` | `0` / `1` / `2` | 均分 / 随机 / 转账 |
| `transactionType` | `RED_PACKET` / `TRANSFER` | 业务类型（默认 `RED_PACKET`） |
| 红包 `status` | `PENDING` / `ACTIVE` / `COMPLETED` / `REFUNDED` / `EXPIRED` | 红包状态 |
| 领取 `status` | `PENDING` / `CONFIRMED` / `FAILED` | 领取记录状态 |
| 退款 `status`（回调返回） | `REFUNDED` / `PENDING` / `FAILED` | 退款处理结果 |
| 钱包绑定 `status` | `ACTIVE` | 绑定状态（查询时只返回活跃绑定） |

---

## 实现参考

- 路由：`internal/api/router.go`（`/redpacket` 分组）
- HTTP 处理：`internal/api/redpacket.go`
- Proto 定义：`protocol/redpacket/redpacket.proto`
- RPC 实现：`internal/rpc/redpacket/service.go`、`internal/rpc/redpacket/wallet.go`、`internal/rpc/redpacket/admin.go`
- 链路由：`internal/rpc/redpacket/chain_runtime.go`
- 合约：`evm-contract/contracts/RedPacketBase.sol`
