# Call E2EE Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement OpenIM server control-plane for call E2EE (descriptor passthrough, conversationID, token gating, custom-signal hardening, Commit CAS, LiveKit kick on member remove) while keeping non-E2EE calls unchanged.

**Architecture:** Incremental hooks into existing `internal/rpc/rtc` and `internal/rpc/openmls`. E2EE gates activate only when invitation `e2ee.required=true`. Media keys never touch the server. Protocol fields already drafted in `protocol/` submodule.

**Tech Stack:** Go, gRPC/protobuf, MongoDB, Redis, LiveKit server SDK, existing OpenIM errs/RPC patterns.

## Global Constraints

- Compatibility mode A: gate only when `e2ee.required=true`; empty/`required=false` keeps legacy behavior.
- Never generate, store, or log `K_media`, exporter secrets, key commitments, or MLS plaintext.
- Do not decrypt `mlsMessage`; transparent forward only.
- Never silently downgrade an E2EE room to non-E2EE.
- Allowed scheme default: `mls-exporter-livekit-v1`; min version `1`; require `frameCryptor=true`.
- E2EE JWT TTL ≤ 5 minutes; metadata may include only non-secret `e2ee=true`.
- Spec: `docs/superpowers/specs/2026-07-16-call-e2ee-server-design.md`.

## File Map

| File | Responsibility |
|---|---|
| `protocol/rtc/rtc.proto` (+ generated) | Already has E2EE fields; finalize if gaps; add kick RPC if needed |
| `protocol/openmls/openmls.proto` (+ generated) | Commit CAS response fields (already drafted) |
| `pkg/common/servererrs/code.go`, `predefine.go` | E2EE + 4010 error codes |
| `pkg/common/storage/model/signal.go` | Invitation E2EE columns |
| `pkg/common/storage/model/openmls.go` | Commit idempotency fields |
| `pkg/common/storage/database/openmls.go` + `mgo/openmls.go` + controller | FindByIdempotencyKey / indexes |
| `internal/rpc/rtc/e2ee.go` (new) | Parse descriptor, capability check, conversationID, extract e2ee JSON |
| `internal/rpc/rtc/signal.go` | Wire invite/accept/join/token/room/startApp responses + gating |
| `internal/rpc/rtc/custom_signal.go` (new) | Size/rate/dedupe/member checks for custom signal |
| `internal/rpc/rtc/server.go` | Redis client wiring, E2EE token TTL config |
| `config/openim-rpc-rtc.yml` | `e2eeTokenExpiry`, allowed schemes |
| `internal/rpc/openmls/service.go` | SubmitCommit idempotency + 4010 contract |
| `internal/rpc/group/group.go` | After kick/quit, call RTC remove participants |
| `pkg/rpcli/rtc.go` | Client for remove-participants RPC |
| `internal/rpc/rtc/e2ee_test.go`, `custom_signal_test.go`, `internal/rpc/openmls/*_test.go` | Unit tests |

---

### Task 1: Finalize protocol submodule + error codes

**Files:**
- Modify: `protocol/rtc/rtc.proto` (only if kick RPC missing)
- Modify: `protocol/openmls/openmls.proto` (verify SubmitCommitResp fields)
- Regenerate: `protocol/rtc/rtc.pb.go`, `protocol/openmls/openmls.pb.go` (use repo’s usual `make` / protoc flow)
- Modify: `pkg/common/servererrs/code.go`
- Modify: `pkg/common/servererrs/predefine.go`
- Test: `go test ./pkg/common/servererrs/...`

**Interfaces:**
- Produces: error vars `ErrCallE2EERequiredUnsupported` (1830) … `ErrCallE2EETokenDenied` (1834), `ErrMLSEpochConflict` (4010)

- [ ] **Step 1: Add error codes**

In `pkg/common/servererrs/code.go` under RTC section:

```go
	CallE2EERequiredUnsupportedError     = 1830
	CallE2EEConversationNotReadyError    = 1831
	CallE2EEGroupMembershipInvalidError  = 1832
	CallE2EEProtocolVersionMismatchError = 1833
	CallE2EETokenDeniedError             = 1834

	MLSEpochConflictError = 4010
```

In `predefine.go`:

```go
	ErrCallE2EERequiredUnsupported     = errs.NewCodeError(CallE2EERequiredUnsupportedError, "CALL_E2EE_REQUIRED_UNSUPPORTED")
	ErrCallE2EEConversationNotReady    = errs.NewCodeError(CallE2EEConversationNotReadyError, "CALL_E2EE_CONVERSATION_NOT_READY")
	ErrCallE2EEGroupMembershipInvalid  = errs.NewCodeError(CallE2EEGroupMembershipInvalidError, "CALL_E2EE_GROUP_MEMBERSHIP_INVALID")
	ErrCallE2EEProtocolVersionMismatch = errs.NewCodeError(CallE2EEProtocolVersionMismatchError, "CALL_E2EE_PROTOCOL_VERSION_MISMATCH")
	ErrCallE2EETokenDenied             = errs.NewCodeError(CallE2EETokenDeniedError, "CALL_E2EE_TOKEN_DENIED")
	ErrMLSEpochConflict                = errs.NewCodeError(MLSEpochConflictError, "epoch conflict")
```

- [ ] **Step 2: Verify proto completeness**

Confirm `protocol/rtc/rtc.proto` already has `E2EECapability`, invite/accept/join/token `e2eeCapability`, and response fields `conversationID` / `e2ee`. If `SignalRemoveParticipants` is missing, add:

```protobuf
message SignalRemoveParticipantsReq {
  string groupID = 1;
  repeated string userIDs = 2;
}
message SignalRemoveParticipantsResp {}
```

And register RPC on `RtcService`. Regenerate pb.go via project script (e.g. `make proto` or existing protocol Makefile).

- [ ] **Step 3: Commit protocol + errs**

```bash
cd protocol && git add -A && git commit -m "feat(protocol): call E2EE fields and remove-participants RPC"
cd .. && git add protocol pkg/common/servererrs && git commit -m "feat: add call E2EE error codes and bump protocol"
```

---

### Task 2: Invitation + MLSCommit storage fields

**Files:**
- Modify: `pkg/common/storage/model/signal.go`
- Modify: `pkg/common/storage/model/openmls.go`
- Modify: `pkg/common/storage/database/openmls.go`
- Modify: `pkg/common/storage/database/mgo/openmls.go`
- Modify: `pkg/common/storage/controller/openmls.go`
- Test: compile `go build ./pkg/common/storage/...`

**Interfaces:**
- Produces: `SignalInvitation.{ConversationID,E2EERequired,CallID}`; `MLSCommit.{FromEpoch,CommitHash,IdempotencyKey}`; `FindByIdempotencyKey(ctx, key) (*MLSCommit, error)`

- [ ] **Step 1: Extend models**

`signal.go`:

```go
	ConversationID string `bson:"conversation_id,omitempty"`
	E2EERequired   bool   `bson:"e2ee_required,omitempty"`
	CallID         string `bson:"call_id,omitempty"`
```

`openmls.go` `MLSCommit`:

```go
	FromEpoch      uint64 `bson:"from_epoch"`
	CommitHash     string `bson:"commit_hash,omitempty"`
	IdempotencyKey string `bson:"idempotency_key,omitempty"`
```

- [ ] **Step 2: Add indexes + FindByIdempotencyKey**

In `NewMLSCommitMongo` indexes, add unique partial index on `idempotency_key` where non-empty. Keep existing `(group_id, epoch)` unique.

```go
FindByIdempotencyKey(ctx context.Context, key string) (*model.MLSCommit, error)
```

Wire through database interface and controller.

- [ ] **Step 3: Commit**

```bash
git add pkg/common/storage && git commit -m "feat(storage): E2EE invitation fields and MLS commit idempotency"
```

---

### Task 3: E2EE helper package (parse, conversationID, capability)

**Files:**
- Create: `internal/rpc/rtc/e2ee.go`
- Create: `internal/rpc/rtc/e2ee_test.go`

**Interfaces:**
- Produces:
  - `parseE2EEFromCustomData(customData string) (required bool, callID string, e2eeJSON string, err error)`
  - `normalizeCallConversationID(groupID, inviterID string, inviteeIDs []string) string`
  - `checkE2EECapability(cap *rtc.E2EECapability, allowedSchemes []string, minVersion int) error`

- [ ] **Step 1: Write failing tests**

```go
func TestNormalizeCallConversationID_Single(t *testing.T) {
	id := normalizeCallConversationID("", "b", []string{"a"})
	if id != "si_a_b" {
		t.Fatalf("got %s", id)
	}
}

func TestParseE2EE_Required(t *testing.T) {
	raw := `{"e2ee":{"required":true,"callID":"c1","scheme":"mls-exporter-livekit-v1","version":1}}`
	req, callID, e2eeJSON, err := parseE2EEFromCustomData(raw)
	if err != nil || !req || callID != "c1" || e2eeJSON == "" {
		t.Fatalf("parse failed: %v %v %v %q", err, req, callID, e2eeJSON)
	}
}

func TestCheckE2EECapability_RejectMissing(t *testing.T) {
	err := checkE2EECapability(nil, []string{"mls-exporter-livekit-v1"}, 1)
	if err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Run tests (expect fail)**

```bash
go test ./internal/rpc/rtc/ -run 'TestNormalize|TestParseE2EE|TestCheckE2EE' -count=1
```

Expected: FAIL undefined / not found.

- [ ] **Step 3: Implement helpers**

Use `github.com/openimsdk/open-im-server/v3/pkg/util/conversationutil.GenConversationIDForSingle` for 1:1; group → `groupID`.

`parseE2EEFromCustomData`: unmarshal outer JSON, read `e2ee` object; `e2eeJSON` = `json.Marshal` of that object only (byte-stable for response field). Empty customData → `required=false`, no error.

`checkE2EECapability`: require non-nil, `frameCryptor`, schemes intersection with allow-list, and if client sends version via scheme string only use allow-list (descriptor version checked separately from invitation customData when gating). Map failures to `ErrCallE2EERequiredUnsupported` or `ErrCallE2EEProtocolVersionMismatch`.

Also add:

```go
func e2eeRequiredFromInvitation(inv *model.SignalInvitation) bool {
	return inv != nil && inv.E2EERequired
}
```

- [ ] **Step 4: Re-run tests (expect pass) + commit**

```bash
go test ./internal/rpc/rtc/ -run 'TestNormalize|TestParseE2EE|TestCheckE2EE' -count=1
git add internal/rpc/rtc/e2ee.go internal/rpc/rtc/e2ee_test.go
git commit -m "feat(rtc): E2EE descriptor parse and capability helpers"
```

---

### Task 4: Wire invite/accept/join/room/startApp descriptor + conversationID

**Files:**
- Modify: `internal/rpc/rtc/signal.go` (`invitationToModel`, `modelToInvitationInfo`, `handleInvite`, `handleInviteInGroup`, `handleAccept`, `handleJoin`, `SignalGetRoomByGroupID`, `GetSignalInvitationInfoStartApp`, `getTokenByRoomID` responses)
- Test: `go test ./internal/rpc/rtc/ -count=1` (existing + new)

**Interfaces:**
- Consumes: Task 3 helpers
- Produces: responses with `ConversationID` and `E2Ee` filled; DB rows with E2EE columns

- [ ] **Step 1: Update model conversion**

In `invitationToModel`, after building `m`:

```go
	convID := normalizeCallConversationID(inv.GroupID, inv.InviterUserID, inv.InviteeUserIDList)
	required, callID, _, _ := parseE2EEFromCustomData(inv.CustomData)
	m.ConversationID = convID
	m.E2EERequired = required
	m.CallID = callID
	if inv.ConversationID == "" {
		inv.ConversationID = convID
	}
```

In `modelToInvitationInfo`, set `ConversationID: m.ConversationID` (fallback recompute if empty for old rows).

- [ ] **Step 2: Fill invite responses**

`handleInvite` return:

```go
	_, _, e2eeJSON, _ := parseE2EEFromCustomData(inv.CustomData)
	return &rtc.SignalInviteResp{
		// ...existing fields...
		ConversationID: inv.ConversationID,
	}, nil
```

Same for `InviteInGroup`. Persist via updated `invitationToModel`.

For Accept/Join/GetRoom/StartApp responses that have `ConversationID`/`E2Ee` fields:

```go
	_, _, e2eeJSON, _ := parseE2EEFromCustomData(dbInv.CustomData)
	resp.ConversationID = dbInv.ConversationID
	resp.E2Ee = e2eeJSON
```

Ensure `Invitation` nested objects also carry `ConversationID` via `modelToInvitationInfo`.

- [ ] **Step 3: Manual sanity — non-E2EE path unchanged**

Empty `CustomData` invite must still succeed; `E2EERequired=false`; no new errors.

- [ ] **Step 4: Commit**

```bash
git add internal/rpc/rtc/signal.go
git commit -m "feat(rtc): persist and return E2EE descriptor and conversationID"
```

---

### Task 5: Token gating + short TTL for E2EE rooms

**Files:**
- Modify: `config/openim-rpc-rtc.yml`
- Modify: RTC config struct (find LiveKit config under `pkg/common/config` or rtc local config)
- Modify: `internal/rpc/rtc/server.go`
- Modify: `internal/rpc/rtc/signal.go` (`genToken`, `getTokenByRoomID`, `ensureCallParticipant`, accept/join token paths)
- Create: `internal/rpc/rtc/e2ee_token_test.go` (table tests for gate decisions)

**Interfaces:**
- Consumes: `checkE2EECapability`, invitation flags
- Produces: gated token issuance; `genToken(roomID, userID, e2ee bool)`

- [ ] **Step 1: Config**

```yaml
liveKit:
  tokenExpiry: 3600
  e2eeTokenExpiry: 300   # seconds, ≤ 300 preferred
e2ee:
  allowedSchemes: ["mls-exporter-livekit-v1"]
  minVersion: 1
```

Wire into `rtcServer` fields: `e2eeTokenExpiry`, `e2eeAllowedSchemes`, `e2eeMinVersion`.

- [ ] **Step 2: Tighten membership for E2EE**

Replace open add-invitee behavior when E2EE required:

```go
func (s *rtcServer) ensureCallParticipant(ctx context.Context, inv *model.SignalInvitation, userID string) error {
	if userID == inv.InviterUserID || datautil.Contain(userID, inv.InviteeUserIDList...) {
		return nil
	}
	if inv.E2EERequired {
		if inv.GroupID != "" {
			// authoritative member check — prefer DB/RPC without treating cache as sole authority
			if _, err := s.groupClient.GetGroupMemberInfo(ctx, inv.GroupID, userID); err != nil {
				return servererrs.ErrCallE2EEGroupMembershipInvalid.Wrap()
			}
			return s.db.AddInvitee(ctx, inv.RoomID, userID)
		}
		return servererrs.ErrCallE2EEGroupMembershipInvalid.WrapMsg("not an invited participant")
	}
	return s.db.AddInvitee(ctx, inv.RoomID, userID)
}
```

Use the existing group client method that hits membership (if only cache helper exists, document that Kick deletes membership so subsequent Get fails — acceptable; prefer `GetGroupMembersInfo` / non-cache if available).

- [ ] **Step 3: Capability gate before token**

In `getTokenByRoomID` (and Accept/Join/Invite token issuance when `inv.E2EERequired`):

```go
	if inv.E2EERequired {
		if err := checkE2EECapability(req.E2EeCapability, s.e2eeAllowedSchemes, s.e2eeMinVersion); err != nil {
			return nil, err
		}
	}
	token, err := s.genToken(req.RoomID, req.UserID, inv.E2EERequired)
```

`genToken`:

```go
func (s *rtcServer) genToken(roomID, userID string, e2ee bool) (string, error) {
	at := auth.NewAccessToken(...)
	grant := &auth.VideoGrant{RoomJoin: true, Room: roomID}
	validFor := s.tokenExpiry
	if e2ee {
		validFor = s.e2eeTokenExpiry
		if validFor <= 0 || validFor > 5*time.Minute {
			validFor = 5 * time.Minute
		}
		at.SetMetadata(map[string]string{"e2ee": "true"}) // or LiveKit Attributes API if SetMetadata unavailable — use supported non-secret attribute only
	}
	at.SetVideoGrant(grant).SetIdentity(userID).SetValidFor(validFor)
	return at.ToJWT()
}
```

Update all `genToken` call sites to pass `e2ee` flag from invitation.

Fill `SignalGetTokenByRoomIDResp.ConversationID` / `E2Ee`.

- [ ] **Step 4: Unit-test gate matrix**

Cases: missing capability → 1830; wrong scheme → 1833; non-member E2EE → 1832; non-E2EE missing capability → OK.

- [ ] **Step 5: Commit**

```bash
git add config/openim-rpc-rtc.yml internal/rpc/rtc pkg/common/config
git commit -m "feat(rtc): E2EE token gating and short-lived JWT"
```

---

### Task 6: Harden SignalSendCustomSignal

**Files:**
- Create: `internal/rpc/rtc/custom_signal.go`
- Create: `internal/rpc/rtc/custom_signal_test.go`
- Modify: `internal/rpc/rtc/signal.go` (`SignalSendCustomSignal`)
- Modify: `internal/rpc/rtc/server.go` (inject redis)

**Interfaces:**
- Produces: validated forward with `senderUserID`, `senderPlatformID`, `serverSeq`

- [ ] **Step 1: Tests for size / messageID extract**

```go
func TestCustomInfoTooLarge(t *testing.T) {
	big := strings.Repeat("a", 16*1024+1)
	if err := validateCustomInfoSize(big); err == nil {
		t.Fatal("expected size error")
	}
}
```

- [ ] **Step 2: Implement validation + rewrite send path**

```go
const maxCustomInfoBytes = 16 * 1024

func (s *rtcServer) SignalSendCustomSignal(ctx context.Context, req *rtc.SignalSendCustomSignalReq) (*rtc.SignalSendCustomSignalResp, error) {
	if len(req.CustomInfo) > maxCustomInfoBytes {
		return nil, errs.ErrArgs.WrapMsg("customInfo exceeds 16KB")
	}
	inv, err := s.db.GetInvitationByRoomID(ctx, req.RoomID)
	if err != nil {
		return nil, errs.WrapMsg(err, "room not found or ended")
	}
	opUserID := mcontext.GetOpUserID(ctx)
	if err := s.ensureCustomSignalSender(ctx, inv, opUserID); err != nil {
		return nil, err
	}
	if err := s.rateLimitCustomSignal(ctx, inv.RoomID, opUserID); err != nil {
		return nil, err
	}
	msgID := extractCustomMessageID(req.CustomInfo)
	if msgID != "" {
		ok, err := s.markCustomSignalOnce(ctx, inv.RoomID, msgID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return &rtc.SignalSendCustomSignalResp{}, nil // duplicate
		}
	}
	serverSeq := time.Now().UnixMilli() // or redis INCR rtc:custom_seq:{roomID}
	content, err := json.Marshal(map[string]any{
		"roomID":           req.RoomID,
		"senderUserID":     opUserID,
		"senderPlatformID": mcontext.GetOpUserPlatform(ctx), // use existing helper if named differently
		"serverSeq":        serverSeq,
		"customInfo":       json.RawMessage(req.CustomInfo), // if CustomInfo is JSON string, nest carefully to avoid double-encoding
	})
	// ... forward to recipients (existing loop) ...
}
```

Rate limit: Redis sliding window / token bucket key `rtc:cs:rl:{roomID}:{userID}` — 10/s burst 20.

Dedupe: `SET rtc:custom_signal:{roomID}:{messageID} 1 NX EX <invitation TTL seconds>`.

**Do not log `customInfo` body.**

- [ ] **Step 3: Commit**

```bash
git add internal/rpc/rtc/
git commit -m "feat(rtc): harden custom signal auth, size, rate limit, dedupe"
```

---

### Task 7: SubmitCommit CAS + idempotency + 4010

**Files:**
- Modify: `internal/rpc/openmls/service.go` (`SubmitCommit`)
- Create/Modify: `internal/rpc/openmls/submit_commit_test.go`
- Consumes: `FindByIdempotencyKey`, `ErrMLSEpochConflict`

**Interfaces:**
- Produces: resp with `Accepted`, `Duplicate`, `AcceptedEpoch`, `CommitID`, `ExpectedFromEpoch`; legacy `NewEpoch` still set on success

- [ ] **Step 1: Write tests (table)**

1. First commit fromEpoch=0 → acceptedEpoch=1, accepted=true  
2. Replay same idempotencyKey → duplicate=true, same commitID, epoch unchanged  
3. Stale fromEpoch → error code 4010 with expectedFromEpoch  

Prefer interface-fake DB if full mongo unavailable.

- [ ] **Step 2: Implement SubmitCommit contract**

Pseudo-order:

```go
	if req.IdempotencyKey != "" {
		if prev, err := s.db.FindByIdempotencyKey(ctx, req.IdempotencyKey); err == nil && prev != nil {
			return &pbopenmls.SubmitCommitResp{
				Accepted: true, Duplicate: true,
				AcceptedEpoch: prev.Epoch, CommitID: prev.ID,
				NewEpoch: prev.Epoch, SequenceNumber: prev.SequenceNumber,
			}, nil
		}
	}
	newEpoch, err := s.db.IncrEpoch(ctx, req.GroupID, req.FromEpoch)
	if err != nil && errs.ErrRecordNotFound.Is(err) {
		st, _ := s.db.GetState(ctx, req.GroupID)
		var expected uint64
		if st != nil {
			expected = st.CurrentEpoch
		}
		return nil, servererrs.ErrMLSEpochConflict.WrapMsg("epoch conflict").
			// if WrapMsg can't attach data, return custom error implementing CodeError with expected in message
			// OR return resp-style via gRPC status details — match how other OpenIM business codes return.
	}
	// append commit with FromEpoch, CommitHash, IdempotencyKey
	return &pbopenmls.SubmitCommitResp{
		Accepted: true, Duplicate: false,
		AcceptedEpoch: newEpoch, CommitID: commit.ID,
		NewEpoch: newEpoch, SequenceNumber: seqNum,
		BroadcastCount: broadcastCount,
	}, nil
```

**Important:** On conflict, clients expect `errCode=4010` and `data.expectedFromEpoch`. Follow existing OpenIM pattern for structured error payloads (check how `ErrAllUserBusy` returns). If only message string is possible, include `expectedFromEpoch=%d` and still set code 4010; prefer structured `errs` detail if project supports it.

- [ ] **Step 3: Commit**

```bash
git add internal/rpc/openmls pkg/common/storage
git commit -m "feat(openmls): SubmitCommit idempotency and 4010 epoch conflict"
```

---

### Task 8: Group member remove → LiveKit RemoveParticipant

**Files:**
- Modify: `protocol/rtc/rtc.proto` (if not done in Task 1)
- Modify: `internal/rpc/rtc/signal.go` — implement `SignalRemoveParticipants`
- Modify: `pkg/rpcli/rtc.go` — client wrapper
- Modify: `internal/rpc/group/group.go` — after kick/quit, async call RTC
- Modify: `internal/rpc/group` server init — inject RTC client conn

**Interfaces:**
- Produces: `SignalRemoveParticipants(groupID, userIDs)` → for each user, if invitation exists for group, `roomClient.RemoveParticipant`

- [ ] **Step 1: Implement RTC handler**

```go
func (s *rtcServer) SignalRemoveParticipants(ctx context.Context, req *rtc.SignalRemoveParticipantsReq) (*rtc.SignalRemoveParticipantsResp, error) {
	inv, err := s.db.GetInvitationByGroupID(ctx, req.GroupID)
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			return &rtc.SignalRemoveParticipantsResp{}, nil
		}
		return nil, err
	}
	for _, uid := range req.UserIDs {
		_, err := s.roomClient.RemoveParticipant(ctx, &livekit.RoomParticipantIdentity{
			Room: inv.RoomID, Identity: uid,
		})
		if err != nil {
			log.ZWarn(ctx, "RemoveParticipant failed", err, "roomID", inv.RoomID, "userID", uid)
		}
	}
	return &rtc.SignalRemoveParticipantsResp{}, nil
}
```

- [ ] **Step 2: Call from KickGroupMember / QuitGroup**

After existing `RemoveMemberTrigger`:

```go
	go s.rtcClient.SignalRemoveParticipants(context.WithoutCancel(ctx), req.GroupID, req.KickedUserIDs)
```

Failures must not fail kick/quit (log only).

- [ ] **Step 3: Commit**

```bash
git add internal/rpc/rtc internal/rpc/group pkg/rpcli protocol
git commit -m "feat: kick/quit removes LiveKit participants for active group calls"
```

---

### Task 9: Regression + acceptance checklist

**Files:**
- Touch only tests / small fixes discovered

- [ ] **Step 1: Run focused packages**

```bash
go test ./internal/rpc/rtc/ ./internal/rpc/openmls/ ./pkg/common/servererrs/ -count=1
```

Expected: PASS.

- [ ] **Step 2: Manual acceptance against spec §8**

| Case | Expected |
|---|---|
| Invite without e2ee | Same as today; long TTL token OK |
| Invite with `e2ee.required=true`, token w/o capability | 1830 |
| E2EE getToken non-member | 1832 |
| Accept returns conversationID + e2ee JSON | Matches stored customData.e2ee |
| Custom signal >16KB | Reject |
| Duplicate messageID | No second forward |
| SubmitCommit conflict | 4010 |
| Idempotent SubmitCommit | duplicate=true |
| Kick during group call | RemoveParticipant attempted |

- [ ] **Step 3: Grep for secret leakage**

```bash
rg -n "K_media|keyCommitment|SetMetadata.*key" internal/rpc/rtc internal/rpc/openmls || true
```

Ensure no media key paths.

- [ ] **Step 4: Final commit if fixes needed**

```bash
git add -A && git commit -m "test: call E2EE server acceptance fixes"
```

---

## Self-Review (plan vs spec)

| Spec item | Task |
|---|---|
| Descriptor passthrough + conversationID | 3, 4 |
| Capability + token gate + short TTL | 5 |
| Custom signal auth/size/rate/dedupe | 6 |
| SubmitCommit CAS / 4010 / idempotency | 2, 7 |
| RemoveParticipant on kick/quit | 8 |
| Compatibility A (non-E2EE unchanged) | 4, 5, 9 |
| No K_media | Global + 9 |
| Error codes 1830–1834, 4010 | 1 |

No TBD placeholders. Types align across tasks (`E2EERequired`, `parseE2EEFromCustomData`, `ErrMLSEpochConflict`).
