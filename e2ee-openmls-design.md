# OpenMLS 端到端加密详细方案设计

> **文档版本**：v1.0  
> **日期**：2026-05-30  
> **项目**：OpenIM Flutter Enterprise  
> **协议基础**：[MLS RFC 9420](https://www.rfc-editor.org/rfc/rfc9420)  
> **参考实现**：[OpenMLS](https://github.com/openmls/openmls)（Rust，经第三方安全审计）

---

## 目录

1. [背景与目标](#1-背景与目标)
2. [MLS 协议核心概念](#2-mls-协议核心概念)
3. [整体架构](#3-整体架构)
4. [时序图](#4-时序图)
   - 4.1 [设备注册与 KeyPackage 上传](#41-设备注册与-keypackage-上传)
   - 4.2 [1:1 会话建立（首次握手）](#42-11-会话建立首次握手)
   - 4.3 [消息加密发送](#43-消息加密发送)
   - 4.4 [消息接收与解密](#44-消息接收与解密)
   - 4.5 [群组创建与成员初始化](#45-群组创建与成员初始化)
   - 4.6 [群成员加入（Add）](#46-群成员加入add)
   - 4.7 [群成员移除（Remove）](#47-群成员移除remove)
   - 4.8 [密钥轮换（PCS Update）](#48-密钥轮换pcs-update)
   - 4.9 [新设备登录同步](#49-新设备登录同步)
5. [后端接口详细说明](#5-后端接口详细说明)
   - 5.1 [MLS Delivery Service（新增）](#51-mls-delivery-service新增)
   - 5.2 [Auth Server 扩展接口](#52-auth-server-扩展接口)
   - 5.3 [现有接口变更说明](#53-现有接口变更说明)
6. [数据结构定义](#6-数据结构定义)
7. [客户端集成方案](#7-客户端集成方案)
8. [密钥存储设计](#8-密钥存储设计)
9. [ContentType 约定](#9-contenttype-约定)
10. [安全属性分析](#10-安全属性分析)
11. [实施路线图](#11-实施路线图)
12. [风险与缓解措施](#12-风险与缓解措施)

---

## 1. 背景与目标

### 1.1 现状

当前 OpenIM Flutter Enterprise 消息传输：

- 消息体**明文存储于服务器**，服务器可读取全部内容
- `openim-sdk-core/internal/crypto/` 存在 Virgil E3Kit 相关钩子，但**未在 Flutter 层激活**
- `initSDK` 中 `isNeedEncryption: false`，传输层加密亦未开启
- 消息结构中已有 `IsEncryption` / `InEncryptStatus` 字段，为后续加密预留

### 1.2 目标

| 目标 | 说明 |
|------|------|
| **端到端加密** | 服务器仅转发密文，无法获知消息内容 |
| **前向保密（FS）** | 历史密钥泄露不影响过去消息的安全性 |
| **后向安全（PCS）** | 私钥泄露后，经过密钥轮换恢复安全 |
| **群组扩展性** | 群组密钥协商复杂度 O(log N)，支持大群 |
| **多设备支持** | 同一用户多设备均可解密，独立叶节点 |
| **最小服务器改动** | msg_gateway、OpenIM REST 几乎不变 |

### 1.3 选型理由：MLS vs 其他方案

| 方案 | 群组复杂度 | 前向保密 | 后向安全 | 多设备 | 标准化 |
|------|-----------|---------|---------|-------|-------|
| **MLS (RFC 9420)** | O(log N) | ✅ | ✅ | ✅ 原生 | IETF 标准 |
| Signal Protocol | O(N) | ✅ | ✅ | ⚠️ 需额外 | 事实标准 |
| Virgil E3Kit | O(N) | ⚠️ 部分 | ❌ | ✅ | 私有 |
| 自研 AES-GCM | O(1) | ❌ | ❌ | 需自实现 | 无 |

---

## 2. MLS 协议核心概念

```
KeyPackage       —— 设备公钥包（HPKE init_key + Ed25519 leaf_key + Credential）
Credential       —— 将 userID/deviceID 与签名公钥绑定的服务器颁发证书
Ratchet Tree     —— 二叉树结构，叶节点为每个成员设备，内节点存路径密钥
Epoch            —— 每次成员变更（Add/Remove/Update）后推进，派生全新对称密钥
Proposal         —— 成员变更意向（Add/Remove/Update），需 Commit 才生效
Commit           —— 确认并应用若干 Proposal，推进 Epoch，广播给所有成员
Welcome          —— Commit 后向新成员发送的加密初始化包，含 ratchet tree 快照
MLSMessage       —— 加密应用消息（PrivateMessage），携带 epoch + 密文
HPKE             —— Hybrid Public Key Encryption，基于 X25519-HKDF-SHA256 + AES-128-GCM
```

### 密钥派生链

```
init_secret
    │  (KDF + commit_secret)
    ▼
epoch_secret
    ├─► sender_data_secret   (加密发送者身份，PrivateMessage 中隐藏真实发送者)
    ├─► encryption_secret    (消息内容加密根密钥)
    │       └─► per-leaf ratchet → per-message key+nonce (用后即删)
    ├─► exporter_secret      (外部导出，如媒体加密)
    ├─► authentication_secret
    └─► membership_key       (验证成员资格)
```

---

## 3. 整体架构

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          Flutter App Layer                               │
│                                                                          │
│  ┌──────────────┐    ┌─────────────────────┐    ┌──────────────────┐   │
│  │  ChatLogic   │───▶│   MLSController     │───▶│  MLSKeyStore     │   │
│  │ (chat_logic) │    │ (Dart FFI wrapper)  │    │(secure_storage)  │   │
│  └──────┬───────┘    └──────────┬──────────┘    └──────────────────┘   │
│         │                       │ FFI / MethodChannel                   │
└─────────┼───────────────────────┼─────────────────────────────────────-┘
          │                       │
┌─────────┼───────────────────────┼──────────────────────────────────────┐
│         │   Go SDK Core (openim-sdk-core)                               │
│  ┌──────▼───────┐    ┌──────────▼──────────┐    ┌──────────────────┐  │
│  │MessageManager│    │  MLS Session Mgr    │    │  MLSKeyStore     │  │
│  │ (send/recv)  │    │  (internal/mls/)    │    │  (SQLite 加密)   │  │
│  └──────┬───────┘    └──────────┬──────────┘    └──────────────────┘  │
│         │                       │ CGO → Rust OpenMLS                   │
└─────────┼───────────────────────┼──────────────────────────────────────┘
          │ WebSocket             │ HTTPS
┌─────────▼───────────────────────▼──────────────────────────────────────┐
│                         Server Layer                                     │
│                                                                          │
│  ┌───────────────┐  ┌──────────────────┐  ┌──────────────────────────┐ │
│  │  msg_gateway  │  │  MLS DS (新增)   │  │  Auth Server (扩展)      │ │
│  │ (不变，仅转发)│  │  KeyPkg + Commit │  │  Credential 颁发         │ │
│  └───────────────┘  └──────────────────┘  └──────────────────────────┘ │
│                                                                          │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │         OpenIM REST API（用户/群组/好友 CRUD，不变）                │  │
│  └───────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────┘
```

**核心原则：**
- `msg_gateway` 只看到 base64 密文 blob，零感知消息内容
- MLS DS 是唯一新增服务，负责 KeyPackage 分发和 Commit 有序广播
- 现有 OpenIM REST、WebSocket 协议字段**不变**，仅 `MsgData.content` 改为密文

---

## 4. 时序图

### 4.1 设备注册与 KeyPackage 上传

```mermaid
sequenceDiagram
    participant App as Flutter App
    participant SDK as Go SDK Core
    participant Auth as Auth Server
    participant DS as MLS DS

    App->>SDK: initSDK() + login(userID, token)
    SDK->>SDK: 检查本地是否已有 KeyPackage 私钥
    alt 首次登录或密钥不存在
        SDK->>SDK: generate init_key (X25519 HPKE)
        SDK->>SDK: generate leaf_key (Ed25519 签名密钥对)
        SDK->>Auth: POST /crypto/credential\n{userID, deviceID, leaf_pub_key}
        Auth->>Auth: 验证 token，签发 MLS Credential\n(userID + deviceID + leaf_pub_key + 有效期)
        Auth-->>SDK: {credential: base64(signed_credential)}
        SDK->>SDK: 组装 KeyPackage\n{init_key, leaf_key, credential, ciphersuite, extensions}
        SDK->>SDK: 存储 (init_key_priv, leaf_key_priv) 到加密 KeyStore
        SDK->>DS: POST /mls/key_packages/upload\n{userID, deviceID, key_package: TLS序列化}
        DS->>DS: 验证 Credential 签名\n存储 KeyPackage（与 userID 关联）
        DS-->>SDK: {status: "ok", kp_id: "uuid"}
        SDK->>SDK: 存储 kp_id 到本地
    end
    SDK-->>App: 登录成功，E2EE 就绪
```

**说明：**
- 每个设备每次登录后检查 KeyPackage 是否仍有效（未被消费），若已被消费则重新上传
- Credential 有效期建议 30 天，到期前 App 自动续签
- 每个用户应上传至少 `max_devices * 5` 份 KeyPackage（防止消费耗尽）

---

### 4.2 1:1 会话建立（首次握手）

```mermaid
sequenceDiagram
    participant Alice as Alice (发起方)
    participant DS as MLS DS
    participant GW as msg_gateway
    participant Bob as Bob (接收方)

    Note over Alice,Bob: Alice 首次向 Bob 发消息，需建立 MLS Group

    Alice->>DS: GET /mls/key_packages/{bobUserID}
    DS-->>Alice: [{kp_device1, kp_device2, ...}]\n(每个 KP 一次性消费)
    DS->>DS: 标记这些 KP 为 "consumed"

    Alice->>Alice: MlsGroup::new(alice_credential)\n创建单成员 MLS Group (groupID = conversationID)
    Alice->>Alice: group.add_members([bob_kp_d1, bob_kp_d2])\n生成 (Commit, Welcome_for_bob_d1, Welcome_for_bob_d2)
    Alice->>Alice: 推进到 Epoch 1（自己应用 Commit）

    Alice->>DS: POST /mls/groups/{groupID}/commit\n{commit_msg, epoch: 0}
    DS-->>Alice: {status: "ok", new_epoch: 1}

    Alice->>GW: POST sendMsg\n{contentType: MLS_HANDSHAKE, content: Welcome_for_bob_d1,\n recvID: bob_d1_userID}
    Alice->>GW: POST sendMsg\n{contentType: MLS_HANDSHAKE, content: Welcome_for_bob_d2,\n recvID: bob_d2_userID}

    GW-->>Bob: 推送 MLS_HANDSHAKE 消息 (Welcome)
    Bob->>Bob: MlsGroup::new_from_welcome(welcome)\n初始化本地 MLS Group，进入 Epoch 1
    Bob->>Bob: 存储 GroupState 到加密 SQLite

    Note over Alice,Bob: 握手完成，双方均处于 Epoch 1，可互相加密通信
```

---

### 4.3 消息加密发送

```mermaid
sequenceDiagram
    participant UI as ChatLogic (Flutter)
    participant MLS as MLSController
    participant SDK as Go SDK / MessageManager
    participant GW as msg_gateway
    participant DB as 本地 SQLite

    UI->>UI: 用户输入消息，触发 _sendMessage()
    UI->>MLS: encrypt(conversationID, plaintext_bytes)
    MLS->>MLS: 查找本地 GroupState (by conversationID)
    MLS->>MLS: group.create_message(plaintext)\n→ PrivateMessage {epoch, sender_data_ciphertext,\n   content_ciphertext, auth_tag}
    MLS->>MLS: 序列化 MLSMessage → base64
    MLS-->>UI: encrypted_content (base64)

    UI->>SDK: sendMessage({contentType: 1001, content: encrypted_content, ...})
    SDK->>DB: INSERT 本地消息记录（密文，status=sending）
    SDK->>GW: WebSocket SendMsg\n{MsgData: {contentType:1001, content: base64_ciphertext}}

    GW->>GW: 验证 token，转发（不解密）
    GW-->>SDK: SendMsgResp {serverMsgID, sendTime}
    SDK->>DB: UPDATE status=succeeded, 填充 serverMsgID/sendTime
    SDK-->>UI: onMsgSendSuccess callback
    UI->>UI: 更新消息气泡状态（已发送）
```

**要点：**
- `contentType = 1001 (ENCRYPTED)` 标识这是 MLS 加密消息
- `content` 字段存储 base64 编码的 TLS 序列化 `MLSMessage`
- 服务器、中间节点对密文完全透明，仅做路由转发

---

### 4.4 消息接收与解密

```mermaid
sequenceDiagram
    participant GW as msg_gateway
    participant SDK as Go SDK Core
    participant MLS as MLSController (Dart)
    participant UI as ChatLogic (Flutter)
    participant DB as 本地 SQLite

    GW->>SDK: OnRecvNewMessage\n{contentType:1001, content: base64_ciphertext}
    SDK->>SDK: 检测 contentType == ENCRYPTED (1001)
    SDK->>SDK: base64 decode → MLSMessage bytes

    SDK->>MLS: FFI: mls_decrypt(groupID, mls_message_bytes)
    MLS->>MLS: group.process_message(mls_msg)\n验证: epoch 匹配, 成员签名, MAC
    MLS->>MLS: 提取 ApplicationMessage {plaintext, sender_leaf_index}
    MLS->>MLS: 通过 leaf_index → Credential → userID 得到真实发送者
    MLS-->>SDK: {plaintext_bytes, verified_sender_userID}

    SDK->>SDK: 解析明文为实际消息内容（Text/Image/File...）
    SDK->>DB: INSERT 解密后的消息（存明文，sender 已验证）
    SDK->>SDK: 触发 OnRecvNewMessage callback（传明文 Message）
    SDK-->>UI: recvNewMessageSubject.add(message)
    UI->>UI: messageList 更新，滚动到最新
    UI->>UI: 渲染消息气泡（显示明文）
```

**异常处理：**

| 情况 | 处理方式 |
|------|---------|
| epoch 不匹配（落后） | 向 DS 拉取缺失 Commit，重放后重新解密 |
| epoch 不匹配（超前） | 缓存消息，等待 Commit 追上后解密 |
| 成员签名验证失败 | 丢弃消息，记录安全日志，上报服务器 |
| GroupState 不存在 | 向 DS 请求 Welcome 重新初始化（设备恢复场景） |

---

### 4.5 群组创建与成员初始化

```mermaid
sequenceDiagram
    participant Creator as 群主 (Creator)
    participant DS as MLS DS
    participant GW as msg_gateway
    participant M1 as 成员1
    participant M2 as 成员2

    Note over Creator: 在 OpenIM 侧群组已创建，获得 groupID

    Creator->>DS: GET /mls/key_packages/{member1_userID}
    Creator->>DS: GET /mls/key_packages/{member2_userID}
    DS-->>Creator: [kp_m1_d1, kp_m1_d2, kp_m2_d1, ...]

    Creator->>Creator: MlsGroup::new(groupID, creator_credential)
    Creator->>Creator: group.add_members([kp_m1_d1, kp_m1_d2, kp_m2_d1])\n→ (Commit, Welcome_m1, Welcome_m2)
    Creator->>Creator: 应用 Commit，推进到 Epoch 1

    Creator->>DS: POST /mls/groups/{groupID}/commit\n{commit_msg, from_epoch:0}
    DS-->>Creator: {status:"ok", epoch:1}

    Creator->>GW: sendMsg MLS_HANDSHAKE(Welcome_m1) → member1
    Creator->>GW: sendMsg MLS_HANDSHAKE(Welcome_m2) → member2

    par 并行处理
        GW-->>M1: Welcome
        M1->>M1: new_from_welcome() → Epoch 1
        M1->>M1: 存储 GroupState
    and
        GW-->>M2: Welcome
        M2->>M2: new_from_welcome() → Epoch 1
        M2->>M2: 存储 GroupState
    end

    Note over Creator,M2: 所有成员均在 Epoch 1，群组 E2EE 就绪
```

---

### 4.6 群成员加入（Add）

```mermaid
sequenceDiagram
    participant Admin as 群主/管理员
    participant DS as MLS DS
    participant GW as msg_gateway
    participant Existing as 现有成员 (已在群)
    participant NewMember as 新成员

    Admin->>DS: GET /mls/key_packages/{newMember_userID}
    DS-->>Admin: [kp_new_d1, kp_new_d2]

    Admin->>Admin: group.propose_add(kp_new_d1) → Add Proposal
    Admin->>Admin: group.commit([add_proposal]) → (Commit, Welcome_for_new)
    Admin->>Admin: 应用 Commit，推进到 Epoch N+1

    Admin->>DS: POST /mls/groups/{groupID}/commit\n{commit, from_epoch: N}
    DS->>DS: 验证 epoch 连续性，广播给现有成员
    DS-->>Admin: {epoch: N+1}

    DS-->>Existing: 推送 Commit 消息（走 GW）
    Existing->>Existing: group.process_message(commit)\n推进到 Epoch N+1（派生新密钥）
    Existing->>Existing: 删除 Epoch N 的密钥材料 ✓前向保密

    Admin->>GW: sendMsg MLS_HANDSHAKE(Welcome) → newMember
    GW-->>NewMember: Welcome
    NewMember->>NewMember: new_from_welcome() → Epoch N+1
    Note right of NewMember: 新成员无法解密 Epoch 0..N 的消息（前向保密）
```

---

### 4.7 群成员移除（Remove）

```mermaid
sequenceDiagram
    participant Admin as 群主/管理员
    participant DS as MLS DS
    participant Remaining as 剩余成员
    participant Removed as 被移除成员

    Admin->>Admin: group.propose_remove(removed_leaf_index) → Remove Proposal
    Admin->>Admin: group.commit([remove_proposal]) → Commit
    Admin->>Admin: 应用 Commit，推进到 Epoch N+1
    Note right of Admin: Epoch N+1 密钥由新的 ratchet tree 派生\n被移除成员无对应叶节点

    Admin->>DS: POST /mls/groups/{groupID}/commit\n{commit, from_epoch: N}
    DS-->>Admin: {epoch: N+1}

    DS-->>Remaining: 推送 Commit（走 GW）
    Remaining->>Remaining: group.process_message(commit)\n推进到 Epoch N+1
    Remaining->>Remaining: 删除 Epoch N 密钥 ✓

    Note over Removed: 被移除成员不会收到 Epoch N+1 的 Commit
    Note over Removed: 即使截获密文，无 Epoch N+1 密钥，无法解密 ✓后向安全
```

---

### 4.8 密钥轮换（PCS Update）

```mermaid
sequenceDiagram
    participant Member as 任意成员 (自身)
    participant DS as MLS DS
    participant Others as 其他成员

    Note over Member: 触发条件：每 50 条消息 / 每 24 小时 / 安全事件

    Member->>Member: group.propose_self_update() → Update Proposal\n（生成新的 leaf_key 和 path_secrets）
    Member->>Member: group.commit([update_proposal]) → Commit
    Member->>Member: 应用 Commit，推进到 Epoch N+1
    Member->>Member: 安全删除旧 leaf_key_priv ✓PCS恢复

    Member->>DS: POST /mls/groups/{groupID}/commit\n{commit, from_epoch: N}
    DS-->>Member: {epoch: N+1}

    DS-->>Others: 推送 Commit
    Others->>Others: group.process_message(commit)\n推进 Epoch，更新该成员路径密钥
    Others->>Others: 删除 Epoch N 密钥材料

    Note over Member,Others: 即使 Member 旧私钥被盗，\nEpoch N+1 后的消息对攻击者不可解 ✓PCS
```

---

### 4.9 新设备登录同步

```mermaid
sequenceDiagram
    participant NewDevice as 新设备 (同一用户)
    participant Auth as Auth Server
    participant DS as MLS DS
    participant GW as msg_gateway
    participant ExistingDevice as 已登录设备

    NewDevice->>NewDevice: generate new init_key + leaf_key
    NewDevice->>Auth: POST /crypto/credential {userID, new_device_id, leaf_pub}
    Auth-->>NewDevice: {credential}
    NewDevice->>DS: POST /mls/key_packages/upload
    DS-->>NewDevice: {kp_id}

    Note over NewDevice: 通知已登录设备将新设备 Add 到所有群组

    NewDevice->>GW: sendMsg(contentType: MLS_NEW_DEVICE_REQUEST)\n告知现有设备"新设备已上传 KP"

    ExistingDevice->>DS: GET /mls/key_packages/{userID} (获取新设备 KP)
    loop 每个群组
        ExistingDevice->>ExistingDevice: group.add_members([new_device_kp])
        ExistingDevice->>DS: POST /mls/groups/{gid}/commit
        ExistingDevice->>GW: sendMsg MLS_HANDSHAKE(Welcome) → newDevice
    end

    NewDevice-->>NewDevice: 收到各群 Welcome，逐一 new_from_welcome()
    Note right of NewDevice: 历史消息不可解密（前向保密，符合安全预期）\n此后新消息均可解密
```

---

## 5. 后端接口详细说明

### 5.1 MLS Delivery Service（新增）

MLS DS 是独立部署的轻量微服务，负责：KeyPackage 存储分发、Commit 有序广播、Welcome 路由。

**服务基础路径**：`/mls`（独立微服务或挂载于现有 chat API 服务下）

---

#### `POST /mls/key_packages/upload`

**功能**：设备上传自己的 KeyPackage，供他人发起加密会话时消费。

**请求头**：
```
Authorization: Bearer {imToken}
Content-Type: application/json
```

**请求体**：
```json
{
  "user_id": "user123",
  "device_id": "device_abc",
  "platform": "iOS",
  "key_package": "base64(TLS序列化的 KeyPackage)",
  "ciphersuite": "MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519",
  "credential_type": "basic",
  "expires_at": 1780000000
}
```

**字段说明**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| user_id | string | 是 | 用户 ID，与 token 中一致 |
| device_id | string | 是 | 设备唯一标识（安装 UUID 或平台设备指纹） |
| platform | string | 是 | iOS / Android / Web |
| key_package | string | 是 | TLS 序列化的 KeyPackage，base64 编码 |
| ciphersuite | string | 是 | 固定值，双方必须一致 |
| credential_type | string | 否 | basic（默认）或 x509 |
| expires_at | int64 | 否 | Unix 时间戳，KP 过期时间；不填默认 30 天 |

**响应**：
```json
{
  "code": 0,
  "msg": "ok",
  "data": {
    "kp_id": "kp_uuid_xxx",
    "total_count": 5
  }
}
```

**错误码**：

| code | 含义 |
|------|------|
| 4001 | Credential 签名验证失败 |
| 4002 | KeyPackage 格式错误 |
| 4003 | 超出单设备最大 KP 数量限制（默认 20） |

---

#### `GET /mls/key_packages/{user_id}`

**功能**：获取指定用户所有在线设备的 KeyPackage（每个 KP **一次性消费**）。发起加密会话时调用。

**请求头**：
```
Authorization: Bearer {imToken}
```

**查询参数**：

| 参数 | 类型 | 说明 |
|------|------|------|
| exclude_device_id | string | 排除自己的设备（可选） |
| count_per_device | int | 每设备取几份，默认 1 |

**响应**：
```json
{
  "code": 0,
  "data": {
    "user_id": "user456",
    "key_packages": [
      {
        "kp_id": "kp_uuid_1",
        "device_id": "device_ios",
        "platform": "iOS",
        "key_package": "base64(...)",
        "consumed": false
      },
      {
        "kp_id": "kp_uuid_2",
        "device_id": "device_android",
        "platform": "Android",
        "key_package": "base64(...)",
        "consumed": false
      }
    ]
  }
}
```

**注意**：
- 此接口调用后，DS 将对应 KP 标记为 `consumed`，不会再返回给其他请求
- 若用户 KP 耗尽（`key_packages` 为空），调用方应通过其他渠道（如系统消息）通知对方补充 KP
- DS 应在 KP 剩余数量低于阈值（如 3 份/设备）时，主动推送通知给对应用户补充

---

#### `GET /mls/key_packages/count/{user_id}`

**功能**：查询指定用户各设备的 KeyPackage 剩余数量（不消费）。

**响应**：
```json
{
  "code": 0,
  "data": {
    "user_id": "user123",
    "devices": [
      {"device_id": "device_ios", "available_count": 8},
      {"device_id": "device_android", "available_count": 3}
    ],
    "total_available": 11
  }
}
```

---

#### `POST /mls/key_packages/refresh`

**功能**：批量补充 KeyPackage（当剩余量低于阈值时客户端主动调用）。与 `upload` 语义相同，单次可上传多份。

**请求体**：
```json
{
  "user_id": "user123",
  "device_id": "device_ios",
  "key_packages": [
    "base64(kp1)",
    "base64(kp2)",
    "base64(kp3)"
  ]
}
```

---

#### `POST /mls/groups/{group_id}/commit`

**功能**：提交 MLS Commit 消息，DS 负责有序广播给群内所有成员。这是 MLS 成员变更（Add/Remove/Update）的核心接口。

**请求体**：
```json
{
  "group_id": "group_xxx",
  "sender_user_id": "user123",
  "sender_device_id": "device_ios",
  "from_epoch": 5,
  "commit_message": "base64(TLS序列化的 MLSMessage(Commit))",
  "welcome_messages": [
    {
      "recipient_user_id": "user456",
      "recipient_device_id": "device_android",
      "welcome_message": "base64(TLS序列化的 Welcome)"
    }
  ]
}
```

**字段说明**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| group_id | string | 是 | MLS Group ID，与 OpenIM conversationID 对应 |
| sender_user_id | string | 是 | 发起 Commit 的用户 |
| from_epoch | uint64 | 是 | 发送时客户端认为的当前 epoch；DS 以此做乐观并发控制 |
| commit_message | string | 是 | TLS 序列化的 Commit MLSMessage，base64 |
| welcome_messages | array | 否 | 仅在有新成员时携带（Add Proposal 对应的 Welcome） |

**响应**：
```json
{
  "code": 0,
  "data": {
    "new_epoch": 6,
    "sequence_number": 42,
    "broadcast_count": 5
  }
}
```

**错误码**：

| code | 含义 | 客户端处理 |
|------|------|---------|
| 4010 | epoch 冲突（已有更新的 Commit） | 拉取最新 epoch 后重试 |
| 4011 | 发送者不在群组中 | 重新获取 Welcome 初始化 |
| 4012 | Commit 验证失败 | 放弃，上报错误 |

**有序性保证**：
- DS 对每个 `group_id` 维护单写锁，同时只有一个 Commit 被处理
- `from_epoch` 必须等于 DS 当前存储的 epoch，否则拒绝并返回 4010
- Commit 写入 Redis Stream，支持成员断线重连后按序拉取

---

#### `GET /mls/groups/{group_id}/commits`

**功能**：成员重连后，拉取本地 epoch 之后的全部 Commit（用于追赶状态）。

**查询参数**：

| 参数 | 类型 | 说明 |
|------|------|------|
| since_epoch | uint64 | 从此 epoch 之后（不含）开始返回 |
| limit | int | 最多返回条数，默认 50 |

**响应**：
```json
{
  "code": 0,
  "data": {
    "group_id": "group_xxx",
    "commits": [
      {
        "epoch": 6,
        "sequence_number": 42,
        "commit_message": "base64(...)",
        "sender_user_id": "user123",
        "created_at": 1748573100
      }
    ],
    "current_epoch": 6,
    "has_more": false
  }
}
```

---

#### `POST /mls/groups/{group_id}/welcome`

**功能**：向新成员/新设备发送 Welcome（可独立于 Commit 调用，用于补发）。

**请求体**：
```json
{
  "group_id": "group_xxx",
  "sender_user_id": "user123",
  "recipients": [
    {
      "user_id": "user789",
      "device_id": "device_new",
      "welcome_message": "base64(Welcome)"
    }
  ]
}
```

DS 通过 msg_gateway 向各接收方推送 `MLS_HANDSHAKE` 类型消息。

---

#### `GET /mls/groups/{group_id}/state`

**功能**：查询群组当前 MLS 状态（调试/恢复用）。

**响应**：
```json
{
  "code": 0,
  "data": {
    "group_id": "group_xxx",
    "current_epoch": 8,
    "member_count": 12,
    "last_commit_at": 1748573100,
    "created_at": 1748560000
  }
}
```

---

#### `DELETE /mls/groups/{group_id}`

**功能**：群组解散时清理 MLS DS 侧数据（由服务端群组解散事件触发，非客户端调用）。

---

### 5.2 Auth Server 扩展接口

#### `POST /crypto/credential`

**功能**：Auth Server 为设备颁发 MLS Credential。Credential 将 `userID + deviceID` 与设备签名公钥绑定，其他成员通过 Credential 验证消息发送者身份。

**请求头**：
```
Authorization: Bearer {chatToken}
Content-Type: application/json
```

**请求体**：
```json
{
  "user_id": "user123",
  "device_id": "device_abc_uuid",
  "platform": "iOS",
  "leaf_public_key": "base64(Ed25519公钥，32字节)",
  "client_version": "3.8.3"
}
```

**响应**：
```json
{
  "code": 0,
  "data": {
    "credential": "base64(BasicCredential TLS序列化)",
    "credential_type": "basic",
    "issued_at": 1748573100,
    "expires_at": 1751165100,
    "issuer": "openim-auth.example.com"
  }
}
```

**Credential 内容结构**（TLS 编码前的明文）：
```
BasicCredential {
  identity: bytes  // userID:deviceID:platform
}
// 由 Auth Server 私钥（Ed25519）对整个 KeyPackage 的 tbs 部分签名
```

**注意**：
- Auth Server 需维护一个 Ed25519 根签名密钥对
- 公钥需在 `/.well-known/mls-credentials` 端点公开，客户端初始化时拉取并固定（Certificate Pinning）
- Credential 有效期 30 天，客户端提前 7 天续签

---

#### `GET /crypto/credential/verify`

**功能**：验证 Credential 有效性（供其他客户端或 DS 调用，也可由客户端本地验证）。

**查询参数**：
```
credential=base64(credential)
```

**响应**：
```json
{
  "code": 0,
  "data": {
    "valid": true,
    "user_id": "user123",
    "device_id": "device_abc_uuid",
    "expires_at": 1751165100
  }
}
```

---

#### `GET /crypto/root_public_key`

**功能**：返回 Auth Server 用于签发 Credential 的根公钥（客户端启动时拉取，用于本地验证 Credential）。

**响应**：
```json
{
  "code": 0,
  "data": {
    "public_key": "base64(Ed25519根公钥，32字节)",
    "key_id": "v1",
    "algorithm": "Ed25519"
  }
}
```

---

### 5.3 现有接口变更说明

#### `POST /msg/send_msg`（msg_gateway）

**变更**：无变更。`MsgData.content` 字段由明文改为 base64(MLSMessage)，服务器不感知变化。

**受影响字段**：

| 字段 | 变更前 | 变更后 |
|------|-------|-------|
| `content_type` | 101 (Text) | 1001 (ENCRYPTED) 或原值（旧客户端兼容） |
| `content` | 明文 JSON 字符串 | base64(TLS序列化的 MLSMessage PrivateMessage) |
| `is_send_msg` | 不变 | 不变 |

#### `POST /msg/get_history_message`（msg_gateway）

**变更**：无变更。历史消息以密文返回，由客户端本地解密后展示。

**注意**：若用户 GroupState 不存在（如重装），历史密文消息将无法解密，这是 E2EE 的预期行为。建议 UI 提示"无法解密的加密消息"。

---

## 6. 数据结构定义

### 6.1 MLSMessage Wrapper（客户端封装层）

```json
{
  "v": 1,
  "cs": "MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519",
  "gid": "conversationID_xxx",
  "epoch": 5,
  "mls_msg": "base64(TLS序列化的 MLSMessage)"
}
```

此 JSON 整体 base64 后作为 `MsgData.content` 值。

### 6.2 本地 GroupState 存储结构（SQLite）

```sql
CREATE TABLE mls_group_state (
  group_id        TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL,
  epoch           INTEGER NOT NULL DEFAULT 0,
  group_state     BLOB NOT NULL,  -- OpenMLS 序列化的 MlsGroup，AES-256-GCM 加密
  member_count    INTEGER NOT NULL DEFAULT 1,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  last_commit_seq INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE mls_pending_proposals (
  id              TEXT PRIMARY KEY,
  group_id        TEXT NOT NULL,
  proposal        BLOB NOT NULL,
  proposal_ref    TEXT NOT NULL,
  created_at      INTEGER NOT NULL
);

CREATE TABLE mls_message_cache (
  msg_client_id   TEXT PRIMARY KEY,
  group_id        TEXT NOT NULL,
  epoch           INTEGER NOT NULL,
  mls_ciphertext  BLOB NOT NULL,
  cached_at       INTEGER NOT NULL
  -- 缓存超前 epoch 的消息，等 Commit 到达后重新解密
);
```

### 6.3 本地 KeyStore 结构（flutter_secure_storage）

```
key: "mls_init_key_{device_id}"      value: base64(X25519私钥, 32字节)
key: "mls_leaf_key_priv_{device_id}" value: base64(Ed25519私钥, 32字节)
key: "mls_leaf_key_pub_{device_id}"  value: base64(Ed25519公钥, 32字节)
key: "mls_credential_{device_id}"    value: base64(TLS序列化的BasicCredential)
key: "mls_root_pub_key"              value: base64(Auth Server根公钥)
key: "mls_db_encryption_key"         value: base64(AES-256-GCM密钥, 32字节，用于加密GroupState数据库)
```

所有 key 存储于：
- **iOS**：Keychain，`kSecAttrAccessibleWhenUnlockedThisDeviceOnly`
- **Android**：Android Keystore + EncryptedSharedPreferences

### 6.4 消息 ContentType 扩展

```go
// 在 sdk_struct/sdk_struct.go 中扩展
const (
    // 现有
    ContentTypeText  = 101
    // 新增
    ContentTypeEncrypted    = 1001  // MLS PrivateMessage（加密应用消息）
    ContentTypeMlsHandshake = 1002  // MLS 握手消息（Welcome / Commit / Proposal）
    // DS 内部使用，不经 msg_gateway
    ContentTypeMlsKeyPackage = 1003
)
```

---

## 7. 客户端集成方案

### 7.1 改动文件清单

#### Flutter 层

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `lib/core/controller/im_controller.dart` | 修改 | `login()` 成功后调用 `MLSController.initialize()` 上传 KeyPackage |
| `lib/core/im_callback.dart` | 修改 | `onRecvNewMessage` 中路由 `MLS_HANDSHAKE` 消息到 `MLSController` |
| `lib/pages/chat/chat_logic.dart` | 修改 | `_sendMessage()` 前插入加密；接收时解密 |
| `openim_common/lib/src/apis.dart` | 新增 | `MLSApi` 类：KP 上传/获取、Commit、Credential |
| `openim_common/lib/src/urls.dart` | 新增 | MLS DS 端点路径常量 |
| `lib/core/mls_controller.dart` | 新增 | Dart FFI 封装，GroupState 缓存，Epoch 追踪 |
| `lib/core/mls_key_store.dart` | 新增 | `flutter_secure_storage` 封装，私钥生命周期管理 |

#### Go SDK Core 层

| 模块 | 改动 | 说明 |
|------|------|------|
| `internal/crypto/crypto.go` | 扩展 | 新增 UploadKeyPackage / FetchKeyPackages / PostCommit / GetCommits / PostWelcome |
| `sdk_struct/sdk_struct.go` | 扩展 | 新增 ContentType 常量 ENCRYPTED=1001, MLS_HANDSHAKE=1002 |
| `internal/mls/`（新增） | 新增 | MLS 会话管理 + CGO 桥接 OpenMLS Rust FFI |
| `internal/conversation_msg/api.go` | 不变 | 发送路径不变，内容已是加密 blob |

### 7.2 加密拦截点（伪代码）

```dart
// lib/pages/chat/chat_logic.dart

Future<void> _sendMessage(Message message) async {
  // 加密拦截
  if (MLSController.isE2EEEnabled(conversationID)) {
    final plaintext = jsonEncode(message.toSendPayload());
    final encrypted = await MLSController.encrypt(
      groupId: conversationID,
      plaintext: utf8.encode(plaintext),
    );
    message = message.copyWith(
      contentType: ContentType.encrypted,
      content: base64Encode(encrypted),
    );
  }

  // 原有发送逻辑（不变）
  await messageManager.sendMessage(
    message: message,
    recvID: recvID,
    groupID: groupID,
  );
}
```

```dart
// lib/core/im_callback.dart

void onRecvNewMessage(Message message) {
  if (message.contentType == ContentType.mlsHandshake) {
    // MLS 握手消息（Welcome/Commit），不显示在 UI
    MLSController.processHandshake(message);
    return;
  }

  if (message.contentType == ContentType.encrypted) {
    MLSController.decrypt(
      groupId: message.conversationID,
      ciphertext: base64Decode(message.content!),
    ).then((plaintext) {
      final decrypted = message.copyWith(
        contentType: _parsePlaintextContentType(plaintext),
        content: utf8.decode(plaintext.data),
        senderID: plaintext.verifiedSenderID, // 经 MLS 验证的发送者
      );
      recvNewMessageSubject.add(decrypted);
    }).catchError((e) {
      // 解密失败：显示"无法解密的消息"
      recvNewMessageSubject.add(message.copyWith(
        contentType: ContentType.cannotDecrypt,
      ));
    });
    return;
  }

  recvNewMessageSubject.add(message);
}
```

---

## 8. 密钥存储设计

### 8.1 密钥分层

```
Level 0: 设备硬件安全（Keychain / Android Keystore）
    └── mls_db_encryption_key（加密 GroupState 数据库的 AES-256-GCM 主密钥）
    └── mls_leaf_key_priv（Ed25519 私钥，消息签名）
    └── mls_init_key（X25519 私钥，一次性，用后删除）

Level 1: 加密 SQLite 数据库（用 L0 密钥加密）
    └── GroupState（OpenMLS 序列化，含 epoch 密钥树）
    └── Pending Proposals
    └── Message Cache（超前 epoch 的缓存密文）

Level 2: 内存（仅运行时）
    └── 当前 epoch 的 per-message key+nonce（用后立即归零）
```

### 8.2 密钥生命周期

| 密钥 | 生成时机 | 删除时机 | 存储位置 |
|------|---------|---------|---------|
| `init_key_priv` | 首次登录 / KP 刷新 | KP 被消费（对方建立会话）后立即删除 | Keychain |
| `leaf_key_priv` | 首次登录 / Update Commit | 下一次 Update Commit 生效后删除旧密钥 | Keychain |
| `epoch_secret` | Commit 推进 | 下一个 Commit 生效后，立即安全擦除 | 内存 → GroupState |
| `per-message key` | 每条消息加/解密 | 加/解密完成后立即归零 | 内存 |
| `GroupState` | 群组初始化 | 退群 / 群解散 / 用户登出 | 加密 SQLite |

---

## 9. ContentType 约定

| ContentType | 整数值 | 名称 | 处理方式 | 是否显示在 UI |
|-------------|--------|------|---------|-------------|
| 现有文本 | 101 | Text | 现有逻辑（向后兼容） | 是 |
| 现有图片 | 102 | Picture | 现有逻辑 | 是 |
| **加密消息** | **1001** | **ENCRYPTED** | 触发 `mls_decrypt()`，显示解密后内容 | 是（解密后） |
| **MLS 握手** | **1002** | **MLS_HANDSHAKE** | 路由到 `MLSController.processHandshake()`，状态机处理 | **否** |
| 无法解密 | 1099 | CANNOT_DECRYPT | 显示"消息无法解密，请升级客户端"提示 | 是（提示） |

---

## 10. 安全属性分析

| 属性 | 是否满足 | 实现机制 |
|------|---------|---------|
| **机密性** | ✅ | AES-128-GCM per-message 加密，服务器零感知 |
| **完整性** | ✅ | AEAD（Authenticated Encryption）+ Ed25519 叶节点签名 |
| **发送者认证** | ✅ | 每条消息携带 sender_data（HMAC 派生），接收方通过 Credential 验证身份 |
| **前向保密（FS）** | ✅ | 每 Epoch 独立派生密钥，旧密钥 Commit 后立即擦除 |
| **后向安全（PCS）** | ✅ | 成员定期 Update Proposal，重新密钥化路径，恢复 PCS |
| **服务器盲知** | ✅ | PrivateMessage 隐藏发送者，服务器只见加密 blob |
| **群组可扩展性** | ✅ | Ratchet Tree O(log N) 成员管理 |
| **多设备** | ✅ | 每台设备为独立叶节点，并行解密 |
| **重放攻击防护** | ✅ | per-message 序号 + 独一无二 nonce（基于 generation counter） |
| **成员资格保密** | ⚠️ 部分 | 基本 MLS 不隐藏 ratchet tree，可选 Private Group State 扩展 |

---

## 11. 实施路线图

### Phase 1：基础设施（4-6 周）

| 任务 | 负责层 | 产出 |
|------|-------|------|
| 集成 OpenMLS crate，编译 Android/iOS FFI 库 | Native | `libopenmls.so` + `.xcframework` |
| 实现 Go MLS FFI 桥（CGO wrapper） | Go SDK | `internal/mls/` 模块 |
| KeyPackage 生命周期：生成/上传/消费/刷新 | Go SDK + Server | MLS DS 核心 API |
| MLS Credential 颁发（Auth Server 新增端点） | Server | `/crypto/credential` |
| 本地密钥存储（`flutter_secure_storage` 封装） | Flutter | `MLSKeyStore` |
| MLS DS 服务骨架（Go，~2000 行） | Server | MLS DS v0.1 |

### Phase 2：1:1 消息加密（3-4 周）

| 任务 | 负责层 | 产出 |
|------|-------|------|
| `MLSController`：init/add/encrypt/decrypt | Flutter Dart | `mls_controller.dart` |
| `ChatLogic` 集成：发送拦截 + 接收解密 | Flutter | `chat_logic.dart` 修改 |
| Welcome / Commit 旁路分发 | Flutter + Server | `im_callback.dart` 扩展 |
| 端到端集成测试（Alice ↔ Bob） | QA | 集成测试套件 |
| 多设备同步（新设备 Add + Welcome） | Flutter + Server | 多端 E2EE |

### Phase 3：群组加密（4-5 周）

| 任务 | 负责层 | 产出 |
|------|-------|------|
| 群组 MLS Group 初始化（批量 Add） | Flutter + Server | 群组 E2EE 初始化 |
| 成员 Add/Remove → Commit 广播 + Epoch 推进 | Flutter + Server DS | 成员变更 E2EE |
| Commit 有序性保证（Redis Stream + 版本锁） | Server DS | Commit 队列 |
| 自动 Update 策略（PCS 定时轮换） | Flutter | 后台任务 |
| 群历史消息 UI 处理（加锁图标提示） | Flutter UI | 用户体验 |

### Phase 4：安全加固与发布（2-3 周）

| 任务 | 说明 |
|------|------|
| 设备完整性检测 | 复用现有 `IntegrityReport` API，加入 MLS Credential 绑定 |
| 密钥备份（可选） | 用户密码派生 KEK 加密私钥备份（明确告知丢失不可恢复） |
| 安全审计 | 代码审计 + OpenMLS 官方 fuzz 测试集成 |
| 灰度发布 | Feature flag：`isE2EEEnabled`，按用户组逐步开启 |
| 监控与告警 | Epoch 推进延迟、KP 耗尽、Commit 冲突率 |

---

## 12. 风险与缓解措施

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| Commit 竞争导致群组 epoch 分裂 | 中 | 高 | DS 序列化写 + `from_epoch` 乐观锁；客户端重试机制 |
| KeyPackage 耗尽（批量新设备上线） | 低 | 中 | DS 低水位告警（< 3 份/设备），客户端自动后台补充 |
| 旧客户端无法显示加密消息 | 高（过渡期） | 中 | ContentType 1001 优雅降级提示"请升级"；保持 101 向后兼容 |
| GroupState 丢失（重装、迁移） | 低 | 高 | 明确产品决策：历史不可恢复（E2EE 预期行为）；可选密码备份 |
| FFI 层 Rust panic 影响 App 稳定性 | 中 | 高 | Rust `catch_unwind`；Dart Isolate 隔离；崩溃上报；降级明文模式 |
| Commit 广播延迟导致消息解密失败 | 中 | 中 | 客户端 epoch 对齐缓冲队列；用户感知延迟 < 500ms |
| 服务器私钥泄露伪造 Credential | 低 | 极高 | Auth Server 签名密钥离线保存；定期轮换；Certificate Transparency |

---

*文档由 Cursor AI 基于 openim-flutter-enterprise 代码库自动生成，请在实施前进行人工审阅。*
