# OpenMLS / Crypto API 接口文档

本文档描述 OpenIM API 服务中 **MLS Delivery Service** 与 **Credential** 相关 HTTP 接口。路由定义见 `internal/api/router.go`。

## 通用说明

### 基础路径

| 分组 | 路径前缀 |
|------|----------|
| KeyPackage / Group | `/openmls/v1` |
| Credential / 根公钥 | `/crypto/v1` |

> 实际访问地址为 `{API_HOST}:{API_PORT}` + 上表路径，例如 `http://127.0.0.1:10002/openmls/v1/key_packages/upload`。

### 请求方式

- 所有接口均为 **POST**
- 请求体：**JSON**（`Content-Type: application/json`）
- JSON 字段名与 protobuf 定义一致，采用 **camelCase**（如 `userID`、`deviceID`）

### 公共请求头

| Header | 必填 | 含义 | 如何设置 |
|--------|------|------|----------|
| `token` | 是 | 用户登录 Token | 调用 `/auth/user_token` 等鉴权接口获取，放入 HTTP Header |
| `operationID` | 是 | 请求追踪 ID | 客户端生成的唯一字符串（UUID 等），每次请求不同 |

### 公共响应格式

遵循 OpenIM 标准 API 响应包装：

```json
{
  "errCode": 0,
  "errMsg": "",
  "errDlt": "",
  "data": { }
}
```

- `errCode == 0` 表示成功，`data` 为各接口定义的响应体
- 非 0 时 `data` 通常为空，错误详情见 `errMsg` / `errDlt`

### 鉴权级别说明

| 级别 | 说明 |
|------|------|
| **登录用户** | Header `token` 有效即可；部分接口额外要求 body 中 `userID` / `senderUserID` 与 Token 对应用户一致 |
| **IM 管理员** | Token 对应用户须在服务端配置的 `imAdminUserID` 列表中 |
| **无额外校验** | 仅需有效 Token，不校验 body 用户字段与 Token 是否一致 |

### 二进制字段编码

以下字段在传输时使用 **标准 Base64**（RFC 4648，带 padding）编码的字符串：

- `keyPackage` / `keyPackages`
- `commitMessage` / `welcomeMessage`
- `leafPublicKey`
- `credential`

原始内容为 RFC 9420 TLS 序列化字节或 JSON 签名信封。

### 推荐客户端流程

```
1. GET 根公钥          → /crypto/v1/root_public_key
2. 申请 Credential     → /crypto/v1/credential/issue
3. 本地生成 KeyPackage（Credential 写入 leaf BasicCredential.identity）
4. 上传 KeyPackage     → /openmls/v1/key_packages/upload 或 refresh
5. 建群/加人时拉取 KP → /openmls/v1/key_packages/fetch
6. 提交 Commit         → /openmls/v1/groups/commit
7. 断线追赶            → /openmls/v1/groups/commits
```

---

## 一、KeyPackage 管理（`/openmls/v1`）

KeyPackage 为 MLS 一次性密钥包，供其他成员在 Add 提案时消费。每个设备默认最多存储 **20** 个未消费 KP（可通过 `openim-rpc-openmls.yml` 中 `maxKeyPackagesPerDevice` 配置）。

---

### 1.1 上传 KeyPackage

**`POST /openmls/v1/key_packages/upload`**

上传单个 KeyPackage 到服务端池。

**鉴权：** 登录用户；`userID` 必须与 Token 用户一致。

#### 请求体

| 字段 | 必填 | 类型 | 含义 | 如何设置 |
|------|------|------|------|----------|
| `userID` | 是 | string | 所属用户 ID | 与当前登录 Token 的 userID 相同 |
| `deviceID` | 是 | string | 设备唯一标识 | 客户端注册/生成的 device ID，同一用户下唯一 |
| `keyPackage` | 是 | string | TLS 序列化 KeyPackage 的 Base64 | 本地 MLS 库生成；若启用 Credential，须将 `IssueCredential` 返回的 `credential` 原样写入 leaf 的 BasicCredential.identity |
| `platform` | 否 | string | 平台标识 | 如 `ios` / `android` / `web`；启用 Credential 校验时以 Credential 内 identity 为准，可省略 |
| `ciphersuite` | 否 | string | 密码套件 | 如 `0x0001`；未填则不单独存储 |
| `credentialType` | 否 | string | Credential 类型 | 通常为 `basic` |
| `expiresAt` | 否 | int64 | KP 过期时间（Unix 秒） | 默认当前时间 + 30 天；>0 时覆盖默认值 |

#### 响应 `data`

| 字段 | 类型 | 含义 |
|------|------|------|
| `kpID` | string | 服务端分配的唯一 KeyPackage ID |
| `totalCount` | int32 | 该设备当前未消费 KP 总数 |

#### 示例

```json
// Request
{
  "userID": "user_001",
  "deviceID": "device_abc",
  "platform": "ios",
  "keyPackage": "AAECAw...",
  "ciphersuite": "0x0001",
  "credentialType": "basic"
}

// Response data
{
  "kpID": "550e8400-e29b-41d4-a716-446655440000",
  "totalCount": 3
}
```

#### 常见错误

- 单设备 KP 数量达到上限（默认 20）
- KeyPackage 内嵌 Credential 签名校验失败或与 `userID`/`deviceID` 不匹配
- `userID` 与 Token 不一致

---

### 1.2 拉取 KeyPackage

**`POST /openmls/v1/key_packages/fetch`**

按用户拉取可用 KeyPackage 并**标记为已消费**（一次性使用）。用于加人/建群时获取目标用户的 KP。

**鉴权：** 登录用户（匿名拉取禁止）。

#### 请求体

| 字段 | 必填 | 类型 | 含义 | 如何设置 |
|------|------|------|------|----------|
| `userID` | 是 | string | 目标用户 ID | 需要获取 KP 的用户 |
| `excludeDeviceID` | 否 | string | 排除的设备 ID | 拉取时跳过该设备（如排除本机） |
| `countPerDevice` | 否 | int32 | 每个设备最多返回数量 | 默认 `1`；≤0 时按 1 处理 |

#### 响应 `data`

| 字段 | 类型 | 含义 |
|------|------|------|
| `userID` | string | 请求的目标用户 ID |
| `keyPackages` | array | KeyPackage 列表，见下表 |

**`keyPackages[]` 元素：**

| 字段 | 类型 | 含义 |
|------|------|------|
| `kpID` | string | KeyPackage ID |
| `deviceID` | string | 来源设备 ID |
| `platform` | string | 平台 |
| `keyPackage` | string | Base64 KeyPackage |
| `consumed` | bool | 是否已消费（返回时通常为 `true`） |

---

### 1.3 查询 KeyPackage 余量

**`POST /openmls/v1/key_packages/count`**

查询指定用户各设备可用（未消费）KeyPackage 数量。

**鉴权：** 登录用户。

#### 请求体

| 字段 | 必填 | 类型 | 含义 | 如何设置 |
|------|------|------|------|----------|
| `userID` | 是 | string | 目标用户 ID | 要统计的用户 |

#### 响应 `data`

| 字段 | 类型 | 含义 |
|------|------|------|
| `userID` | string | 用户 ID |
| `devices` | array | 各设备统计 |
| `totalAvailable` | int32 | 全部设备可用 KP 总和 |

**`devices[]` 元素：**

| 字段 | 类型 | 含义 |
|------|------|------|
| `deviceID` | string | 设备 ID |
| `availableCount` | int32 | 该设备可用 KP 数 |

---

### 1.4 批量刷新 KeyPackage

**`POST /openmls/v1/key_packages/refresh`**

批量上传 KeyPackage，用于设备 KP 池补货。

**鉴权：** 登录用户；`userID` 必须与 Token 用户一致。

#### 请求体

| 字段 | 必填 | 类型 | 含义 | 如何设置 |
|------|------|------|------|----------|
| `userID` | 是 | string | 用户 ID | 与 Token 用户一致 |
| `deviceID` | 是 | string | 设备 ID | 本机设备 ID |
| `keyPackages` | 是 | string[] | Base64 KeyPackage 数组 | 至少 1 个；超出设备上限时只插入剩余配额 |

#### 响应 `data`

| 字段 | 类型 | 含义 |
|------|------|------|
| `uploadedCount` | int32 | 本次实际上传数量 |
| `totalCount` | int32 | 该设备当前 KP 总数 |

---

## 二、MLS Group / Commit（`/openmls/v1`）

MLS 群组状态与 Commit 历史由 Delivery Service 持久化；Commit 会通过 IM 消息通道广播给 OpenIM 群成员（`groupID` 需对应真实 OpenIM 群组 ID）。

---

### 2.1 提交 Commit

**`POST /openmls/v1/groups/commit`**

提交 MLS Commit，递增 epoch，并可选随 Commit 投递 Welcome 给新成员。

**鉴权：** 登录用户；`senderUserID` 须与 Token 用户一致，或为 IM 管理员。

#### 请求体

| 字段 | 必填 | 类型 | 含义 | 如何设置 |
|------|------|------|------|----------|
| `groupID` | 是 | string | MLS / OpenIM 群组 ID | 与 OpenIM 群 `groupID` 对齐以便广播 |
| `senderUserID` | 是 | string | Commit 发送者用户 ID | 当前操作用户 |
| `senderDeviceID` | 是 | string | 发送者设备 ID | 本机 device ID |
| `commitMessage` | 是 | string | TLS 序列化 MLS Commit 的 Base64 | 本地 MLS 库生成的 Commit 消息 |
| `fromEpoch` | 否 | uint64 | 客户端认为的当前 epoch | 乐观锁：须与服务端 epoch 一致，否则返回 epoch conflict |
| `welcomeMessages` | 否 | array | 随 Commit 一并发送的 Welcome | Add 成员时使用，见下表 |

**`welcomeMessages[]` 元素：**

| 字段 | 必填 | 类型 | 含义 | 如何设置 |
|------|------|------|------|----------|
| `recipientUserID` | 是* | string | 新成员用户 ID | 被添加的用户 |
| `recipientDeviceID` | 否 | string | 新成员设备 ID | 仅日志用途；投递按用户单聊通道 |
| `welcomeMessage` | 是* | string | Welcome 消息 Base64 | 本地 MLS 库为该设备生成的 Welcome |

> \* 数组元素中 `recipientUserID` 或 `welcomeMessage` 为空时，该条会被跳过。

#### 响应 `data`

| 字段 | 类型 | 含义 |
|------|------|------|
| `newEpoch` | uint64 | 提交后的新 epoch |
| `sequenceNumber` | int64 | Commit 序列号（当前等于 newEpoch） |
| `broadcastCount` | int32 | 通过群消息广播的成员数；非 OpenIM 群时为 0 |

#### 说明

- 首次 Commit 会自动创建群组状态（epoch 从 0 递增）
- Commit 以 Custom 消息（`extension=mls_handshake`）经通知通道推送，不计入聊天记录
- 若 `groupID` 无法解析为 OpenIM 群，仍持久化 Commit，客户端需轮询 `GetCommits`

---

### 2.2 拉取 Commit 历史

**`POST /openmls/v1/groups/commits`**

按 epoch 增量拉取 Commit，用于断线重连后追赶状态。

**鉴权：** 登录用户。

#### 请求体

| 字段 | 必填 | 类型 | 含义 | 如何设置 |
|------|------|------|------|----------|
| `groupID` | 是 | string | 群组 ID | |
| `sinceEpoch` | 否 | uint64 | 起始 epoch（不含） | 只返回 epoch > sinceEpoch 的记录；默认 0 |
| `limit` | 否 | int32 | 最大返回条数 | 默认 `50`；≤0 时按 50 |

#### 响应 `data`

| 字段 | 类型 | 含义 |
|------|------|------|
| `groupID` | string | 群组 ID |
| `currentEpoch` | uint64 | 服务端当前 epoch |
| `hasMore` | bool | 是否还有更多（返回数 == limit 时为 true） |
| `commits` | array | Commit 记录列表 |

**`commits[]` 元素：**

| 字段 | 类型 | 含义 |
|------|------|------|
| `epoch` | uint64 | Commit 所在 epoch |
| `sequenceNumber` | int64 | 序列号 |
| `commitMessage` | string | Base64 Commit |
| `senderUserID` | string | 发送者 |
| `createdAt` | int64 | 创建时间（Unix 秒） |

---

### 2.3 发送 Welcome

**`POST /openmls/v1/groups/welcome`**

独立向新成员点对点投递 Welcome（不依赖 Commit 内嵌）。

**鉴权：** 登录用户；`senderUserID` 须与 Token 用户一致，或为 IM 管理员。

#### 请求体

| 字段 | 必填 | 类型 | 含义 | 如何设置 |
|------|------|------|------|----------|
| `groupID` | 是 | string | 关联群组 ID | 用于日志追踪 |
| `senderUserID` | 是 | string | 发送者用户 ID | Commit / Welcome 发起方 |
| `recipients` | 是 | array | 接收者列表 | 至少 1 条；结构同 `welcomeMessages` |

**`recipients[]` 元素：** 同 [2.1 welcomeMessages](#21-提交-commit)

#### 响应 `data`

| 字段 | 类型 | 含义 |
|------|------|------|
| `deliveredCount` | int32 | 成功投递数量（部分失败仍返回成功，失败条目写日志） |

---

### 2.4 查询群组状态

**`POST /openmls/v1/groups/state`**

查询 MLS 群当前 epoch 与成员数等元数据。

**鉴权：** 登录用户。

#### 请求体

| 字段 | 必填 | 类型 | 含义 | 如何设置 |
|------|------|------|------|----------|
| `groupID` | 是 | string | 群组 ID | |

#### 响应 `data`

| 字段 | 类型 | 含义 |
|------|------|------|
| `groupID` | string | 群组 ID |
| `currentEpoch` | uint64 | 当前 epoch |
| `memberCount` | int32 | 成员数（SubmitCommit 时从 OpenIM 群同步） |
| `lastCommitAt` | int64 | 最后一次 Commit 时间（Unix 秒） |
| `createdAt` | int64 | 群 MLS 状态创建时间（Unix 秒） |

---

### 2.5 删除群组 MLS 状态

**`POST /openmls/v1/groups/delete`**

删除群组的 MLS 状态与全部 Commit 历史（MongoDB 事务）。

**鉴权：** **仅 IM 管理员**。

#### 请求体

| 字段 | 必填 | 类型 | 含义 | 如何设置 |
|------|------|------|------|----------|
| `groupID` | 是 | string | 要清理的群组 ID | |

#### 响应 `data`

空对象 `{}`。

---

## 三、Credential / 根公钥（`/crypto/v1`）

Credential 由服务端 Ed25519 根密钥签发，绑定 `userID:deviceID:platform` 与 leaf 公钥。需在 `openim-rpc-openmls.yml` 配置 `signingKey.privateKey`，否则 Credential 相关接口返回未配置错误。

根密钥生成示例：

```bash
openssl genpkey -algorithm ed25519 | openssl pkcs8 -topk8 -nocrypt -outform DER | base64
```

---

### 3.1 申请 Credential

**`POST /crypto/v1/credential/issue`**

为设备 leaf 密钥签发 MLS Basic Credential。

**鉴权：** 登录用户；`userID` 必须与 Token 用户一致。

#### 请求体

| 字段 | 必填 | 类型 | 含义 | 如何设置 |
|------|------|------|------|----------|
| `userID` | 是 | string | 用户 ID | 与 Token 一致 |
| `deviceID` | 是 | string | 设备 ID | |
| `leafPublicKey` | 是 | string | Leaf 签名公钥 Base64 | MLS KeyPackage leaf 节点的 signature key（32 字节 Ed25519 公钥等，依 ciphersuite） |
| `platform` | 否 | string | 平台 | 写入 identity `userID:deviceID:platform`；建议填写 |
| `clientVersion` | 否 | string | 客户端版本 | 当前未参与签发逻辑，可留空 |

#### 响应 `data`

| 字段 | 类型 | 含义 |
|------|------|------|
| `credential` | string | Base64(JSON 签名信封)，须**原样**写入 KeyPackage BasicCredential.identity |
| `credentialType` | string | 固定为 `basic` |
| `issuedAt` | int64 | 签发时间（Unix 秒） |
| `expiresAt` | int64 | 过期时间（Unix 秒，默认签发后 30 天） |
| `issuer` | string | 签发者标识（配置项 `signingKey.issuer`） |

#### Credential 信封结构（解码后）

```json
{
  "payload": "<base64(JSON)>",   // 含 identity, leaf_pub_key, issued_at, expires_at, issuer
  "sig": "<base64(Ed25519 签名)>"
}
```

---

### 3.2 验证 Credential

**`POST /crypto/v1/credential/verify`**

验证 Credential 签名与有效期（不校验与 KeyPackage leaf 的绑定）。

**鉴权：** 登录用户。

#### 请求体

| 字段 | 必填 | 类型 | 含义 | 如何设置 |
|------|------|------|------|----------|
| `credential` | 是 | string | `IssueCredential` 返回的 credential 字符串 | Base64 信封 |

#### 响应 `data`

| 字段 | 类型 | 含义 |
|------|------|------|
| `valid` | bool | 是否有效（签名正确且未过期） |
| `userID` | string | 从 identity 解析的用户 ID（valid=false 时为空） |
| `deviceID` | string | 从 identity 解析的设备 ID |
| `expiresAt` | int64 | 过期时间（Unix 秒） |

---

### 3.3 获取根公钥

**`POST /crypto/v1/root_public_key`**

获取服务端 Credential 签发根公钥，供客户端离线验签。

**鉴权：** 登录用户。

#### 请求体

无（可传空 JSON `{}`）。

#### 响应 `data`

| 字段 | 类型 | 含义 |
|------|------|------|
| `publicKey` | string | Ed25519 公钥 Base64 |
| `keyID` | string | 密钥版本 ID，默认 `v1`（配置项 `signingKey.keyID`） |
| `algorithm` | string | 固定为 `Ed25519` |

---

## 四、接口鉴权速查

| 接口 | Token | 用户字段校验 | 备注 |
|------|-------|--------------|------|
| `key_packages/upload` | 必填 | `userID` = Token 用户 | |
| `key_packages/fetch` | 必填 | — | |
| `key_packages/count` | 必填 | — | |
| `key_packages/refresh` | 必填 | `userID` = Token 用户 | |
| `groups/commit` | 必填 | `senderUserID` = Token 用户或管理员 | |
| `groups/commits` | 必填 | — | |
| `groups/welcome` | 必填 | `senderUserID` = Token 用户或管理员 | |
| `groups/state` | 必填 | — | |
| `groups/delete` | 必填 | **仅管理员** | |
| `credential/issue` | 必填 | `userID` = Token 用户 | 需配置 signingKey |
| `credential/verify` | 必填 | — | 需配置 signingKey |
| `root_public_key` | 必填 | — | 需配置 signingKey |

---

## 五、配置项参考

文件：`config/openim-rpc-openmls.yml`

| 配置 | 默认值 | 说明 |
|------|--------|------|
| `signingKey.privateKey` | 空 | Ed25519 PKCS#8 DER Base64；空则禁用 Credential |
| `signingKey.keyID` | `v1` | 根公钥版本 |
| `signingKey.issuer` | — | Credential payload 中的 issuer |
| `maxKeyPackagesPerDevice` | `20` | 每设备最大 KP 数 |

---

## 六、Proto 定义

完整 message 定义见：`protocol/openmls/openmls.proto`
