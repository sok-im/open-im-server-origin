package model

const (
	DeviceStatusActive  = "active"
	DeviceStatusRevoked = "revoked"
)

// CryptoDevice stores an E2EE-capable device registered by a user.
// Each device maps to a unique Virgil Identity used for key management.
type CryptoDevice struct {
	DeviceID       string `bson:"device_id"`
	UserID         string `bson:"user_id"`
	Platform       string `bson:"platform"`
	DeviceModel    string `bson:"device_model"`
	AppVersion     string `bson:"app_version"`
	VirgilIdentity string `bson:"virgil_identity"`
	Status         string `bson:"status"`
	LastSeenAt     int64  `bson:"last_seen_at"`
	CreateTime     int64  `bson:"create_time"`
}

// GroupKeyEvent records a group key rotation event triggered by membership
// changes or explicit admin rotation. Clients use the version to decide
// whether they need to re-fetch the group session key.
type GroupKeyEvent struct {
	EventID         string `bson:"event_id"`
	GroupID         string `bson:"group_id"`
	GroupKeyVersion int64  `bson:"group_key_version"`
	EventType       string `bson:"event_type"`
	OperatorUserID  string `bson:"operator_user_id"`
	CreateTime      int64  `bson:"create_time"`
}
