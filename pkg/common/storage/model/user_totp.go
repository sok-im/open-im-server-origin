package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// UserTotp stores a user's TOTP binding. One document per user.
type UserTotp struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`
	UserID     string             `bson:"user_id"`
	Secret     string             `bson:"secret"`  // AES-256-GCM encrypted Base32 secret
	Enabled    bool               `bson:"enabled"` // always true while bound
	BoundAt    int64              `bson:"bound_at"`    // Unix seconds
	CreateTime time.Time          `bson:"create_time"`
	UpdateTime time.Time          `bson:"update_time"`
}

// UserTotpRecovery holds one one-time recovery code for a user.
// Eight documents are inserted per binding. Each is invalidated once used.
type UserTotpRecovery struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`
	UserID     string             `bson:"user_id"`
	CodeHash   string             `bson:"code_hash"` // bcrypt hash of the plaintext code
	Used       bool               `bson:"used"`
	UsedAt     int64              `bson:"used_at,omitempty"` // Unix seconds; omitted when unused
	CreateTime time.Time          `bson:"create_time"`
}
