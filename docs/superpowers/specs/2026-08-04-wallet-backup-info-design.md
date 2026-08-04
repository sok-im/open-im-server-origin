# Wallet Backup Info 设计文档

## 背景与目标

客户端钱包备份完成后，需要把备份元数据上报服务端，便于后续查询「是否已备份 / 备份文件信息」。

目标：

1. 新增 `POST /wallet/set_backup_info`：按 uid 写入备份元数据
2. 新增 `POST /wallet/get_backup_info`：按 uid 查询最新一条
3. 同一 uid **只保留最后一条**（覆盖写）
4. 数据写入 Mongo，权威源为 Mongo

## 设计决策（已确认）

| 决策点 | 结论 |
|--------|------|
| 鉴权 | 登录用户只能读写自己的 uid（`opUserID == uid`） |
| 本轮范围 | set + get 一起做 |
| 实现路径 | API 直连 Mongo 存储层（对齐 `phone/set_sn_info`），不新增 RPC |
| 同 uid 语义 | Upsert 覆盖；uid 唯一索引保证只有一条 |
| get 无记录 | **返回成功空数据**（不报 `ErrRecordNotFound`） |
| Cache | 无；Mongo 为权威源 |
| 历史版本 | 不做；只保留最新一条 |

## 架构与数据流

### 写路径

```
POST /wallet/set_backup_info
  1. BindJSON：uid / backupTime / fileSize / name
  2. 校验：uid、name 非空；backupTime > 0；fileSize >= 0
  3. 鉴权：opUserID == uid，否则 ErrNoPermission
  4. Mongo Upsert（filter: uid）覆盖 backup_time / file_size / name / update_time
  5. GinSuccess(nil)
```

### 读路径

```
POST /wallet/get_backup_info
  1. BindJSON：uid
  2. 校验：uid 非空
  3. 鉴权：opUserID == uid，否则 ErrNoPermission
  4. Mongo FindOne by uid
  5. 无记录 → GinSuccess 空结构（uid 回填请求值，其余零值/空串）
     有记录 → GinSuccess 完整字段
```

## 接口契约

### Set

**请求**

```json
{
  "uid": "u1",
  "backupTime": 1720000000,
  "fileSize": 1048576,
  "name": "wallet-backup.zip"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| uid | string | 用户 ID，必填 |
| backupTime | int64 | 备份时间，单位秒，必填且 > 0 |
| fileSize | int64 | 文件大小，单位字节，必填且 >= 0 |
| name | string | 备份文件名称，必填 |

**响应**：标准成功，无额外 data。

### Get

**请求**

```json
{ "uid": "u1" }
```

**响应（有记录）**

```json
{
  "uid": "u1",
  "backupTime": 1720000000,
  "fileSize": 1048576,
  "name": "wallet-backup.zip"
}
```

**响应（无记录）**

```json
{
  "uid": "u1",
  "backupTime": 0,
  "fileSize": 0,
  "name": ""
}
```

## 数据模型

**Collection**：`wallet_backup_info`（常量 `database.WalletBackupInfoName`）

```go
type WalletBackupInfo struct {
	UID        string `bson:"uid"`
	BackupTime int64  `bson:"backup_time"` // 秒
	FileSize   int64  `bson:"file_size"`   // 字节
	Name       string `bson:"name"`
	UpdateTime int64  `bson:"update_time"` // 写入时服务端 UnixMilli
}
```

**索引**：`uid` 唯一索引。

**Upsert**：

```
filter: { uid }
update: {
  $set: { backup_time, file_size, name, update_time },
  $setOnInsert: { uid }
}
upsert: true
```

## 代码落点

| 层 | 文件（拟新增/修改） |
|----|---------------------|
| model | `pkg/common/storage/model/wallet_backup.go` |
| database 接口 | `pkg/common/storage/database/wallet_backup.go`（含 collection name 常量） |
| mgo 实现 | `pkg/common/storage/database/mgo/wallet_backup.go` |
| API | `internal/api/wallet_backup.go` |
| 路由 | `internal/api/router.go`：`/wallet` group 注册 set/get，初始化 Mongo |

不改 `protocol/`，不新增 RPC。

## 错误与边界

| 场景 | 行为 |
|------|------|
| 参数非法 | `ErrArgs` |
| opUserID 为空或不等于 uid | `ErrNoPermission` |
| Mongo 写/读失败 | 原样 GinError |
| get 无记录 | 成功空数据 |
| 重复 set 同一 uid | 覆盖为最新一条 |

## 非目标（本轮不做）

- 备份文件本身的上传/下载（仅存元数据）
- 备份历史列表 / 多版本
- Admin 代查他人
- Cache / 搜索旁路
- RPC / proto 变更

## 测试要点

- set：本人成功；他人 uid → 权限失败；非法参数失败
- set 两次同一 uid：get 只看到第二次的值；collection 中仅一条
- get：有记录返回字段；无记录返回空成功；他人 uid → 权限失败
