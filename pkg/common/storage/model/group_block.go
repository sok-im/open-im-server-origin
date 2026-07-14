package model

import "time"

// GroupBlock is per-user block of group chat message and notification push (online + offline).
// OwnerUserID must be a group member.
type GroupBlock struct {
	OwnerUserID string    `bson:"owner_user_id"`
	GroupID     string    `bson:"group_id"`
	CreateTime  time.Time `bson:"create_time"`
}
