package group

import (
	pbgroup "github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/sdkws"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

const (
	GroupPermFieldAllowSendMsg    = "allowSendMsg"
	GroupPermFieldAllowAddMember  = "allowAddMember"
	GroupPermFieldAllowPinMsg     = "allowPinMsg"
	GroupPermFieldAllowMemberBurn = "allowMemberBurn"
)

// CollectGroupPermissionChangedFields returns a map of API field name → new value
// for permission fields that were requested and actually changed.
// Stable insert order: allowSendMsg, allowAddMember, allowPinMsg, allowMemberBurn.
func CollectGroupPermissionChangedFields(before, after *model.Group, requested map[string]bool) map[string]int32 {
	if before == nil || after == nil || len(requested) == 0 {
		return nil
	}
	out := make(map[string]int32)
	type pair struct {
		name string
		old  int32
		new  int32
	}
	checks := []pair{
		{GroupPermFieldAllowSendMsg, before.AllowSendMsg, after.AllowSendMsg},
		{GroupPermFieldAllowAddMember, before.AllowAddMember, after.AllowAddMember},
		{GroupPermFieldAllowPinMsg, before.AllowPinMsg, after.AllowPinMsg},
		{GroupPermFieldAllowMemberBurn, before.AllowBurn, after.AllowBurn},
	}
	for _, c := range checks {
		if !requested[c.name] {
			continue
		}
		if c.old != c.new {
			out[c.name] = c.new
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func permissionRequestedFromGroupInfoForSet(g *sdkws.GroupInfoForSet) map[string]bool {
	m := make(map[string]bool)
	if g == nil {
		return m
	}
	if g.AllowSendMsg != nil {
		m[GroupPermFieldAllowSendMsg] = true
	}
	if g.AllowAddMember != nil {
		m[GroupPermFieldAllowAddMember] = true
	}
	if g.AllowPinMsg != nil {
		m[GroupPermFieldAllowPinMsg] = true
	}
	if g.AllowBurn != nil {
		m[GroupPermFieldAllowMemberBurn] = true
	}
	return m
}

func permissionRequestedFromSetGroupInfoEx(req *pbgroup.SetGroupInfoExReq) map[string]bool {
	m := make(map[string]bool)
	if req == nil {
		return m
	}
	if req.AllowSendMsg != nil {
		m[GroupPermFieldAllowSendMsg] = true
	}
	if req.AllowAddMember != nil {
		m[GroupPermFieldAllowAddMember] = true
	}
	if req.AllowPinMsg != nil {
		m[GroupPermFieldAllowPinMsg] = true
	}
	if req.AllowBurn != nil {
		m[GroupPermFieldAllowMemberBurn] = true
	}
	return m
}
