package model

import "time"

// GroupInviteLink 群邀请链接记录。
// LinkID 为 8 位随机字母数字码，作为短链标识。
type GroupInviteLink struct {
	// LinkID 唯一短码，例如 "Ab3Cd8Ef"
	LinkID string `bson:"link_id"`
	// GroupID 所属群 ID
	GroupID string `bson:"group_id"`
	// CreatorID 创建者 UserID（群主或管理员）
	CreatorID string `bson:"creator_id"`
	// ExpireAt 到期时间（Unix 毫秒）；0 表示永不过期
	ExpireAt int64 `bson:"expire_at"`
	// MaxUseCount 最大使用次数；0 表示不限次数
	MaxUseCount int32 `bson:"max_use_count"`
	// UsedCount 已使用次数（原子递增）
	UsedCount int32 `bson:"used_count"`
	// Revoked 是否已被吊销
	Revoked bool `bson:"revoked"`
	// CreatedAt 创建时间
	CreatedAt time.Time `bson:"created_at"`
}
