package group

import (
	"testing"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/stretchr/testify/require"
)

func TestCollectGroupPermissionChangedFields(t *testing.T) {
	before := &model.Group{
		AllowSendMsg:   0,
		AllowAddMember: 0,
		AllowPinMsg:    0,
		AllowBurn:      0,
	}
	after := &model.Group{
		AllowSendMsg:   1,
		AllowAddMember: 1,
		AllowPinMsg:    0,
		AllowBurn:      1,
	}
	requested := map[string]bool{
		GroupPermFieldAllowSendMsg:    true,
		GroupPermFieldAllowAddMember:  true,
		GroupPermFieldAllowPinMsg:     true,
		GroupPermFieldAllowMemberBurn: true,
	}
	got := CollectGroupPermissionChangedFields(before, after, requested)
	require.Equal(t, map[string]int32{
		GroupPermFieldAllowSendMsg:    1,
		GroupPermFieldAllowAddMember:  1,
		GroupPermFieldAllowMemberBurn: 1,
	}, got)
}

func TestCollectGroupPermissionChangedFields_SameValueSkipped(t *testing.T) {
	g := &model.Group{AllowSendMsg: 1}
	requested := map[string]bool{GroupPermFieldAllowSendMsg: true}
	got := CollectGroupPermissionChangedFields(g, g, requested)
	require.Empty(t, got)
}

func TestCollectGroupPermissionChangedFields_UnrequestedIgnored(t *testing.T) {
	before := &model.Group{AllowPinMsg: 0}
	after := &model.Group{AllowPinMsg: 1}
	got := CollectGroupPermissionChangedFields(before, after, map[string]bool{})
	require.Empty(t, got)
}

func TestCollectGroupPermissionChangedFields_SingleField(t *testing.T) {
	before := &model.Group{AllowPinMsg: 0, AllowSendMsg: 1}
	after := &model.Group{AllowPinMsg: 1, AllowSendMsg: 1}
	requested := map[string]bool{GroupPermFieldAllowPinMsg: true}
	got := CollectGroupPermissionChangedFields(before, after, requested)
	require.Equal(t, map[string]int32{GroupPermFieldAllowPinMsg: 1}, got)
}
