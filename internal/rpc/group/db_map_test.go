package group

import (
	"context"
	"testing"

	pbgroup "github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/wrapperspb"
	"github.com/stretchr/testify/require"
)

func TestUpdateGroupInfoExMap_PermissionOnly_NoNormalFlag(t *testing.T) {
	ctx := context.Background()
	m, normalFlag, groupNameFlag, notificationFlag, err := UpdateGroupInfoExMap(ctx, &pbgroup.SetGroupInfoExReq{
		GroupID:        "g1",
		AllowSendMsg:   wrapperspb.Int32(1),
		AllowAddMember: wrapperspb.Int32(1),
		AllowPinMsg:    wrapperspb.Int32(1),
		AllowBurn:      wrapperspb.Int32(1),
	})
	require.NoError(t, err)
	require.False(t, normalFlag)
	require.False(t, groupNameFlag)
	require.False(t, notificationFlag)
	require.Contains(t, m, "allow_send_msg")
	require.Contains(t, m, "allow_add_member")
	require.Contains(t, m, "allow_pin_msg")
	require.Contains(t, m, "allow_burn")
}

func TestUpdateGroupInfoExMap_IntroductionStillNormal(t *testing.T) {
	ctx := context.Background()
	_, normalFlag, _, _, err := UpdateGroupInfoExMap(ctx, &pbgroup.SetGroupInfoExReq{
		GroupID:      "g1",
		Introduction: wrapperspb.String("hi"),
		AllowPinMsg:  wrapperspb.Int32(1),
	})
	require.NoError(t, err)
	require.True(t, normalFlag)
}
