package push

import (
	"testing"

	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
)

func TestIsTargetedGroupMemberPush(t *testing.T) {
	signalingType := int32(constant.SignalingNotificationBegin)

	tests := []struct {
		name string
		msg  *sdkws.MsgData
		want bool
	}{
		{
			name: "group invite signaling to callee",
			msg: &sdkws.MsgData{
				SessionType: constant.ReadGroupChatType,
				GroupID:     "g1",
				RecvID:      "userB",
				ContentType: signalingType,
			},
			want: true,
		},
		{
			name: "group broadcast notification",
			msg: &sdkws.MsgData{
				SessionType: constant.ReadGroupChatType,
				GroupID:     "g1",
				RecvID:      "g1",
				ContentType: constant.GroupCallStartedNotification,
			},
			want: false,
		},
		{
			name: "single chat signaling",
			msg: &sdkws.MsgData{
				SessionType: constant.SingleChatType,
				RecvID:      "userB",
				ContentType: signalingType,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTargetedGroupMemberPush(tt.msg); got != tt.want {
				t.Fatalf("isTargetedGroupMemberPush() = %v, want %v", got, tt.want)
			}
		})
	}
}
