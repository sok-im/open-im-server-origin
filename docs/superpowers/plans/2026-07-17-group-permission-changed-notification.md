# Group Permission Changed Notification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When `allowSendMsg` / `allowAddMember` / `allowPinMsg` / `allowMemberBurn` actually change, broadcast `GroupPermissionChangedNotification` (1530) so SDK fires `OnGroupPermissionChanged`, without breaking existing mute / group-info notifications.

**Architecture:** Add one contentType + Tips in `protocol`. Pure helper diffs requested permission fields before/after DB update. `NotificationSender` broadcasts 1530 with full latest `GroupInfo` + `changedFields`. Wire into `SetSendMessageSetting` (keep 1514/1515), `SetGroupInfo`, and `SetGroupInfoEx` (strip permissions from `normalFlag`/`num` so they do not also fire 1502). Mirror handler + listener in `openim-sdk-core`.

**Tech Stack:** Go, protobuf/`mage` in `protocol` submodule, OpenIM group RPC, `openim-sdk-core` group notification dispatch.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-17-group-permission-changed-notification-design.md`
- contentType: `1530` = `GroupPermissionChangedNotification`
- `changedFields` API names only: `allowSendMsg`, `allowAddMember`, `allowPinMsg`, `allowMemberBurn` (map DB `AllowBurn` → `allowMemberBurn`)
- Send 1530 only when values actually change; same-value request → no 1530
- `SetSendMessageSetting`: keep 1514/1515 **and** add 1530
- `SetGroupInfo` / `SetGroupInfoEx`: permission changes → 1530 only (not 1514/1515); other normal fields may still send 1502
- Do not change notifications for `AllowEditGroupInfo` / `EnableInviteLink` / `NeedVerification` / `MsgBurnDuration`
- Notification config: `isSendMsg: true`, `reliabilityLevel: 2`, `unreadCount: false`, `offlinePush.enable: false`
- `go.mod` uses `replace github.com/openimsdk/protocol => ./protocol`

## File Map

| File | Responsibility |
|---|---|
| `protocol/constant/constant.go` | Add `1530` |
| `protocol/sdkws/sdkws.proto` (+ generated `sdkws.pb.go`) | `GroupPermissionChangedTips` |
| `config/notification.yml` | `groupPermissionChanged` defaults |
| `pkg/common/config/config.go` | `Notification.GroupPermissionChanged` |
| `pkg/notification/msg.go` | Map 1530 → config + `ReadGroupChatType` |
| `internal/rpc/group/permission_notify.go` (new) | Field-name constants + `CollectGroupPermissionChangedFields` |
| `internal/rpc/group/permission_notify_test.go` (new) | Unit tests for diff helper |
| `internal/rpc/group/db_map.go` | Stop setting `normalFlag` for the four permission fields |
| `internal/rpc/group/db_map_test.go` (new) | Assert permission-only update → `normalFlag=false` |
| `internal/rpc/group/notification.go` | `GroupPermissionChangedNotification` sender |
| `internal/rpc/group/group.go` | Wire `SetSendMessageSetting` / `SetGroupInfo` / `SetGroupInfoEx` |
| `openim-sdk-core/protocol/...` | Sync constant + Tips (copy/regenerate from protocol) |
| `openim-sdk-core/pkg/constant/constant.go` | Add `1530` if SDK keeps a local mirror |
| `openim-sdk-core/open_im_sdk_callback/callback_client.go` | `OnGroupPermissionChanged` |
| `openim-sdk-core/open_im_sdk/em.go` | empty listener stub |
| `openim-sdk-core/internal/group/notification.go` | Decode 1530, sync local group, fire listener |
| `openim-sdk-core/test/listener.go` (+ wasm / integration stubs) | Implement new listener method |

---

### Task 1: Protocol — constant + Tips + regenerate

**Files:**
- Modify: `protocol/constant/constant.go`
- Modify: `protocol/sdkws/sdkws.proto`
- Regenerate: `protocol/sdkws/sdkws.pb.go` (via protocol submodule `mage` / protoc flow per `protocol/mage-README.md`)

**Interfaces:**
- Produces: `constant.GroupPermissionChangedNotification = 1530`
- Produces: `sdkws.GroupPermissionChangedTips` with fields `OpUser`, `Group`, `ChangedFields []string`, `OperationTime int64`, `GroupMemberVersion uint64`, `GroupMemberVersionID string`

- [ ] **Step 1: Add contentType constant**

In `protocol/constant/constant.go`, after `GroupE2EENotification = 1529`:

```go
	GroupPermissionChangedNotification = 1530
```

- [ ] **Step 2: Add Tips message**

In `protocol/sdkws/sdkws.proto`, after `GroupNeedVerificationSetTips` (or near other group tips):

```protobuf
// OnGroupPermissionChanged()
// changedFields API names: allowSendMsg / allowAddMember / allowPinMsg / allowMemberBurn
message GroupPermissionChangedTips {
  GroupMemberFullInfo opUser = 1;
  GroupInfo group = 2;
  repeated string changedFields = 3;
  int64 operationTime = 4;
  uint64 groupMemberVersion = 5;
  string groupMemberVersionID = 6;
}
```

- [ ] **Step 3: Regenerate pb.go**

From `protocol/` directory, run the repo’s usual mage/protoc target for Go (see `protocol/mage-README.md`). Confirm `GroupPermissionChangedTips` appears in `sdkws.pb.go`.

- [ ] **Step 4: Compile-check server against local protocol**

```bash
cd /Users/lintao/important/ai-customer/openim/open-im-server-origin
go build ./internal/rpc/group/...
```

Expected: PASS (no callers yet; types must compile).

- [ ] **Step 5: Commit protocol submodule, then pointer in server**

```bash
cd protocol
git add constant/constant.go sdkws/sdkws.proto sdkws/sdkws.pb.go
git commit -m "feat(protocol): add GroupPermissionChangedNotification 1530"

cd ..
git add protocol
git commit -m "chore: bump protocol for group permission notification"
```

---

### Task 2: Notification config wiring

**Files:**
- Modify: `config/notification.yml`
- Modify: `pkg/common/config/config.go`
- Modify: `pkg/notification/msg.go`

**Interfaces:**
- Consumes: `constant.GroupPermissionChangedNotification`
- Produces: `config.Notification.GroupPermissionChanged` loaded from YAML key `groupPermissionChanged`

- [ ] **Step 1: Add YAML block**

In `config/notification.yml`, after `groupNeedVerificationSet` (or next to other group settings):

```yaml
groupPermissionChanged:
  isSendMsg: true
  reliabilityLevel: 2
  unreadCount: false
  offlinePush:
    enable: false
    title: group permission updated
    desc: group permission updated
    ext: group permission updated
```

- [ ] **Step 2: Add config struct field**

In `pkg/common/config/config.go` `Notification` struct, after `GroupNeedVerificationSet`:

```go
	GroupPermissionChanged NotificationConfig `mapstructure:"groupPermissionChanged"`
```

- [ ] **Step 3: Register contentType maps**

In `pkg/notification/msg.go` `newContentTypeConf`:

```go
		constant.GroupPermissionChangedNotification: conf.GroupPermissionChanged,
```

In `newSessionTypeConf`:

```go
		constant.GroupPermissionChangedNotification: constant.ReadGroupChatType,
```

- [ ] **Step 4: Compile**

```bash
go build ./pkg/notification/... ./pkg/common/config/...
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add config/notification.yml pkg/common/config/config.go pkg/notification/msg.go
git commit -m "feat: wire GroupPermissionChanged notification config"
```

---

### Task 3: Permission diff helper + db_map normalFlag split (TDD)

**Files:**
- Create: `internal/rpc/group/permission_notify.go`
- Create: `internal/rpc/group/permission_notify_test.go`
- Modify: `internal/rpc/group/db_map.go`
- Create: `internal/rpc/group/db_map_test.go`

**Interfaces:**
- Produces:
  - `const GroupPermFieldAllowSendMsg = "allowSendMsg"` (and siblings)
  - `func CollectGroupPermissionChangedFields(before, after *model.Group, requested map[string]bool) []string`
  - `UpdateGroupInfoExMap` no longer sets `normalFlag` for the four permission fields

- [ ] **Step 1: Write failing tests for CollectGroupPermissionChangedFields**

Create `internal/rpc/group/permission_notify_test.go`:

```go
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
	require.Equal(t, []string{
		GroupPermFieldAllowSendMsg,
		GroupPermFieldAllowAddMember,
		GroupPermFieldAllowMemberBurn,
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
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
go test ./internal/rpc/group/ -run CollectGroupPermissionChangedFields -v
```

Expected: FAIL (undefined symbols)

- [ ] **Step 3: Implement helper**

Create `internal/rpc/group/permission_notify.go`:

```go
package group

import "github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"

const (
	GroupPermFieldAllowSendMsg    = "allowSendMsg"
	GroupPermFieldAllowAddMember  = "allowAddMember"
	GroupPermFieldAllowPinMsg     = "allowPinMsg"
	GroupPermFieldAllowMemberBurn = "allowMemberBurn"
)

// CollectGroupPermissionChangedFields returns API field names whose values
// actually changed among the requested permission fields.
// Order is stable: allowSendMsg, allowAddMember, allowPinMsg, allowMemberBurn.
func CollectGroupPermissionChangedFields(before, after *model.Group, requested map[string]bool) []string {
	if before == nil || after == nil || len(requested) == 0 {
		return nil
	}
	var out []string
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
			out = append(out, c.name)
		}
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
```

Add imports: `pbgroup "github.com/openimsdk/protocol/group"`, `"github.com/openimsdk/protocol/sdkws"`.

- [ ] **Step 4: Re-run helper tests — expect PASS**

```bash
go test ./internal/rpc/group/ -run CollectGroupPermissionChangedFields -v
```

Expected: PASS

- [ ] **Step 5: Write db_map test then fix normalFlag**

Create `internal/rpc/group/db_map_test.go`:

```go
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
```

Run:

```bash
go test ./internal/rpc/group/ -run UpdateGroupInfoExMap -v
```

Expected: first test FAIL (`normalFlag` still true).

In `db_map.go` `UpdateGroupInfoExMap`, for `AllowSendMsg` / `AllowPinMsg` / `AllowAddMember` / `AllowBurn` blocks: **keep writing to `m`**, but **remove** `normalFlag = true` from those four blocks only. Leave `AllowEditGroupInfo` / `EnableInviteLink` as-is (still set `normalFlag`).

- [ ] **Step 6: Re-run db_map tests — expect PASS**

```bash
go test ./internal/rpc/group/ -run 'CollectGroupPermission|UpdateGroupInfoExMap' -v
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/rpc/group/permission_notify.go internal/rpc/group/permission_notify_test.go internal/rpc/group/db_map.go internal/rpc/group/db_map_test.go
git commit -m "feat(group): diff helpers and stop permission fields setting normalFlag"
```

---

### Task 4: NotificationSender for 1530

**Files:**
- Modify: `internal/rpc/group/notification.go`

**Interfaces:**
- Consumes: `sdkws.GroupPermissionChangedTips`, `constant.GroupPermissionChangedNotification`
- Produces: `func (g *NotificationSender) GroupPermissionChangedNotification(ctx context.Context, tips *sdkws.GroupPermissionChangedTips)`

- [ ] **Step 1: Add sender method**

Place near `GroupNeedVerificationSetNotification` in `notification.go`:

```go
// GroupPermissionChangedNotification broadcasts GroupPermissionChangedNotification (1530)
// when allowSendMsg / allowAddMember / allowPinMsg / allowMemberBurn actually change.
func (g *NotificationSender) GroupPermissionChangedNotification(ctx context.Context, tips *sdkws.GroupPermissionChangedTips) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if tips == nil || tips.Group == nil || len(tips.ChangedFields) == 0 {
		return
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	if tips.OperationTime == 0 {
		tips.OperationTime = time.Now().UnixMilli()
	}
	log.ZInfo(ctx, "GroupPermissionChangedNotification",
		"groupID", tips.Group.GroupID,
		"opUserID", mcontext.GetOpUserID(ctx),
		"changedFields", tips.ChangedFields,
	)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupPermissionChangedNotification, tips)
}
```

Ensure imports include `"time"` if not already present.

- [ ] **Step 2: Compile**

```bash
go build ./internal/rpc/group/...
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/rpc/group/notification.go
git commit -m "feat(group): add GroupPermissionChangedNotification sender"
```

---

### Task 5: Wire SetSendMessageSetting / SetGroupInfo / SetGroupInfoEx

**Files:**
- Modify: `internal/rpc/group/group.go` (`SetGroupInfo`, `SetGroupInfoEx`, `SetSendMessageSetting`)

**Interfaces:**
- Consumes: `CollectGroupPermissionChangedFields`, `permissionRequestedFrom*`, `GroupPermissionChangedNotification`
- Produces: RPC behavior per spec §5.2

- [ ] **Step 1: Wire `SetSendMessageSetting`**

After successful `UpdateGroup` and **before/after** existing mute notifications (order: mute first then permission is fine), add:

```go
	// existing mute / cancel-mute kept for client UI compatibility
	if req.AllowSendMsg == model.GroupPermAdminOnly {
		s.notification.GroupMutedNotification(ctx, req.GroupID)
	} else {
		s.notification.GroupCancelMutedNotification(ctx, req.GroupID)
	}

	groupAfter, err := s.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	count, err := s.db.FindGroupMemberNum(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	owner, err := s.db.TakeGroupOwner(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, owner); err != nil {
		return nil, err
	}
	s.notification.GroupPermissionChangedNotification(ctx, &sdkws.GroupPermissionChangedTips{
		Group:         s.groupDB2PB(groupAfter, owner.UserID, count),
		ChangedFields: []string{GroupPermFieldAllowSendMsg},
		OpUser:        &sdkws.GroupMemberFullInfo{},
	})
```

Note: early-return when `group.AllowSendMsg == req.AllowSendMsg` already prevents no-op notifications. Reuse the pre-update `group` variable if preferred instead of a second TakeGroup — either way `ChangedFields` is fixed to `allowSendMsg`.

Prefer minimizing DB reads: build tips from already-known `req.GroupID` + updated allowSendMsg by calling existing `getGroupInfo` path inside the notification sender is OK; simplest is TakeGroup once after update for tips.Group (full GroupInfo).

- [ ] **Step 2: Wire `SetGroupInfo`**

Before `UpdateGroup`, keep `group` as `before`. After update + re-fetch `group` as `after`:

```go
	requestedPerm := permissionRequestedFromGroupInfoForSet(req.GroupInfoForSet)
	changedFields := CollectGroupPermissionChangedFields(before, group /*after*/, requestedPerm)
	if len(changedFields) > 0 {
		s.notification.GroupPermissionChangedNotification(ctx, &sdkws.GroupPermissionChangedTips{
			Group:         tips.Group,
			ChangedFields: changedFields,
			OpUser:        tips.OpUser,
		})
	}
```

Adjust the existing `num` logic so permission keys do not trigger 1502:

```go
	num := len(update)
	for _, k := range []string{"allow_send_msg", "allow_pin_msg", "allow_add_member", "allow_burn"} {
		if _, ok := update[k]; ok {
			num--
		}
	}
```

Keep existing announcement / name / faceURL / needVerification handling. Final `if num > 0 { GroupInfoSetNotification }` must not fire for permission-only updates.

Save `before := group` before update (copy pointer from first `TakeGroup`).

- [ ] **Step 3: Wire `SetGroupInfoEx`**

Same pattern:

```go
	before := group // from first TakeGroup
	// ... UpdateGroupInfoExMap + UpdateGroup + TakeGroup after ...
	requestedPerm := permissionRequestedFromSetGroupInfoEx(req)
	changedFields := CollectGroupPermissionChangedFields(before, group, requestedPerm)
	if len(changedFields) > 0 {
		s.notification.GroupPermissionChangedNotification(ctx, &sdkws.GroupPermissionChangedTips{
			Group:         tips.Group,
			ChangedFields: changedFields,
			OpUser:        tips.OpUser,
		})
	}
	// existing notificationFlag / groupNameFlag / FaceURL / NeedVerification / MsgBurnDuration / normalFlag
```

Because Task 3 removed permission contribution to `normalFlag`, permission-only updates no longer send 1502. Mixed introduction + permission → 1530 + 1502.

- [ ] **Step 4: Compile + unit tests**

```bash
go test ./internal/rpc/group/ -count=1
go build ./internal/rpc/group/...
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rpc/group/group.go
git commit -m "feat(group): emit GroupPermissionChanged on permission setting updates"
```

---

### Task 6: SDK — listener + dispatch

**Files:**
- Sync/copy protocol into `openim-sdk-core/protocol/` (constant + `GroupPermissionChangedTips` in sdkws)
- Modify: `openim-sdk-core/pkg/constant/constant.go` (if still mirroring numbers)
- Modify: `openim-sdk-core/open_im_sdk_callback/callback_client.go`
- Modify: `openim-sdk-core/open_im_sdk/em.go`
- Modify: `openim-sdk-core/internal/group/notification.go`
- Modify: `openim-sdk-core/test/listener.go`
- Modify: `openim-sdk-core/wasm/event_listener/listener.go`
- Modify: `openim-sdk-core/integration_test/internal/pkg/sdk_user_simulator/listener.go`
- Modify: `openim-sdk-core/msgtest/sdk_user_simulator/listener.go`

**Interfaces:**
- Produces: `OnGroupListener.OnGroupPermissionChanged(groupPermissionChangedTips string)`
- Dispatch: contentType `1530` → update local group via `onlineSyncGroupAndMember`, then fire listener

- [ ] **Step 1: Sync protocol artifacts into SDK**

Copy/regenerate so SDK has:
- `GroupPermissionChangedNotification = 1530` in `openim-sdk-core/protocol/constant/constant.go`
- `GroupPermissionChangedTips` in `openim-sdk-core/protocol/sdkws/`
- Mirror in `openim-sdk-core/pkg/constant/constant.go`:

```go
	GroupPermissionChangedNotification = 1530
```

- [ ] **Step 2: Extend OnGroupListener**

In `open_im_sdk_callback/callback_client.go`, after `OnGroupNeedVerificationSet`:

```go
	// OnGroupPermissionChanged changedFields: allowSendMsg / allowAddMember / allowPinMsg / allowMemberBurn
	OnGroupPermissionChanged(groupPermissionChangedTips string)
```

- [ ] **Step 3: empty + test stubs**

In `open_im_sdk/em.go`:

```go
func (e *emptyGroupListener) OnGroupPermissionChanged(groupPermissionChangedTips string) {
	log.ZWarn(e.ctx, "GroupListener is not implemented", nil, "groupPermissionChangedTips", groupPermissionChangedTips)
}
```

In `test/listener.go` (and wasm / integration / msgtest stubs): log the tips JSON the same way as `OnGroupNeedVerificationSet`.

In `wasm/event_listener/listener.go`, follow the same bridge pattern as `OnGroupNeedVerificationSet` (callback name `onGroupPermissionChanged`).

- [ ] **Step 4: Dispatch notification**

In `openim-sdk-core/internal/group/notification.go`, before `default:`, add:

```go
		case constant.GroupPermissionChangedNotification: // 1530
			var detail sdkws.GroupPermissionChangedTips
			if err := utils.UnmarshalNotificationElem(msg.Content, &detail); err != nil {
				return err
			}
			if detail.Group == nil {
				return errs.New("group is nil in GroupPermissionChangedTips").Wrap()
			}
			if err := g.onlineSyncGroupAndMember(ctx, detail.Group.GroupID, nil, nil,
				nil, detail.Group, groupSortIDUnchanged, detail.GroupMemberVersion, detail.GroupMemberVersionID); err != nil {
				return err
			}
			g.listener().OnGroupPermissionChanged(utils.StructToJsonString(detail))
			return nil
```

Order matches spec: sync local GroupInfo first, then callback.

- [ ] **Step 5: Build SDK packages that compile**

```bash
cd openim-sdk-core
go build ./internal/group/... ./open_im_sdk/... ./open_im_sdk_callback/...
```

Expected: PASS (fix any remaining stub implementers that fail to compile).

- [ ] **Step 6: Commit inside SDK submodule, then server pointer if needed**

```bash
cd openim-sdk-core
git add -A
git commit -m "feat(sdk): OnGroupPermissionChanged for notification 1530"

cd ..
# only if submodule SHA should be recorded in this repo:
git add openim-sdk-core
git commit -m "chore: bump openim-sdk-core for group permission listener"
```

---

### Task 7: Manual verification checklist

**Files:** none (manual / staging)

- [ ] **Step 1: Server smoke (staging or local compose)**

For a test group with ≥2 members online:

1. `POST /group/set_send_message_setting` with `allowSendMsg=1` → expect **1514 + 1530**; tips.changedFields=`["allowSendMsg"]`
2. Same request again → **no** new 1530 (and no mute notify)
3. `POST /group/set_invite_setting` `allowAddMember=1` → **1530 only**, fields=`["allowAddMember"]`
4. `POST /group/set_pin_setting` → 1530 with `allowPinMsg`
5. `POST /group/set_burn_setting` → 1530 with `allowMemberBurn`
6. `set_group_info_ex` with both `introduction` + `allowPinMsg` change → **1530 + 1502**
7. Confirm `set_group_info_ex` allowSendMsg change does **not** send 1514/1515

- [ ] **Step 2: SDK smoke**

Receive 1530 → local group permission fields updated → `OnGroupPermissionChanged` fired with JSON containing `group` + `changedFields`.

- [ ] **Step 3: Final commit only if verification notes belong in docs** (optional; skip if nothing to commit)

---

## Self-Review (plan vs spec)

| Spec requirement | Task |
|---|---|
| contentType 1530 + Tips | Task 1 |
| notification.yml + config + session map | Task 2 |
| actual-value diff; API field names; AllowBurn→allowMemberBurn | Task 3 |
| NotificationSender | Task 4 |
| SetSendMessageSetting keeps 1514/1515 + 1530 | Task 5 |
| SetGroupInfo / Ex send 1530; strip from normal/num | Task 3 + 5 |
| SDK OnGroupPermissionChanged after cache sync | Task 6 |
| Manual cases from spec §8 | Task 7 |
| Out of scope: AllowEditGroupInfo / invite link / etc. | Not tasked |

No intentional placeholders left. Helper signatures are consistent across Tasks 3–5.
