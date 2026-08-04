package model

// WalletBackupInfo 钱包备份元数据（每 uid 一条，覆盖写）
type WalletBackupInfo struct {
	UID        string `bson:"uid"`
	BackupTime int64  `bson:"backup_time"` // 秒
	FileSize   int64  `bson:"file_size"`   // 字节
	Name       string `bson:"name"`
	UpdateTime int64  `bson:"update_time"` // 服务端写入 UnixMilli
}
