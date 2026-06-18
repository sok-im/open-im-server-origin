# Group Chat Read Receipt Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add "any-one-reader marks message as read" group chat read receipts, notifying message senders via `OnRecvGroupReadReceipt` callback.

**Architecture:** New `MarkGroupMsgsAsRead` gRPC/HTTP endpoint; server fetches messages, filters unread ones, atomically sets `is_read=true` in MongoDB, then fans out `HasReadReceipt` notifications per sender. SDK `doReadDrawing` handles `ReadGroupChatType` receipts and fires `OnRecvGroupReadReceipt`.

**Tech Stack:** Go, MongoDB (BulkWrite), gRPC, protocol buffers (manually-edited generated files), openim-sdk-core SDK

## Global Constraints

- Both proto copies must stay in sync: `protocol/msg/msg.proto` and `openim-sdk-core/protocol/msg/msg.proto`
- Both `.pb.go` and `_grpc.pb.go` generated files are edited manually (no protoc toolchain)
- Never modify single-chat read receipt logic (`MarkMsgsAsRead`, `OnRecvC2CReadReceipt`)
- The `MarkGroupMsgsAsReadReq.Seqs` field must use field number `2` and `UserID` field number `3` (consistent with `MarkMsgsAsReadReq` pattern)

---

### Task 1: Proto structs + Check() — both copies

**Files:**
- Modify: `protocol/msg/msg.proto`
- Modify: `protocol/msg/msg.pb.go`
- Modify: `protocol/msg/msg_grpc.pb.go`
- Modify: `protocol/msg/msg.go`
- Modify: `openim-sdk-core/protocol/msg/msg.proto`
- Modify: `openim-sdk-core/protocol/msg/msg.pb.go`
- Modify: `openim-sdk-core/protocol/msg/msg_grpc.pb.go`
- Modify: `openim-sdk-core/protocol/msg/msg.go`

**Interfaces:**
- Produces: `MarkGroupMsgsAsReadReq{ConversationID string, Seqs []int64, UserID string}`, `MarkGroupMsgsAsReadResp{}`, gRPC `MsgClient.MarkGroupMsgsAsRead`, `MsgServer.MarkGroupMsgsAsRead`

- [ ] **Step 1: Add proto message definitions**

In `protocol/msg/msg.proto`, add after the `MarkMsgsAsReadResp {}` block (around line 95):

```protobuf
message MarkGroupMsgsAsReadReq {
  string conversationID = 1;
  repeated int64 seqs   = 2;
  string userID         = 3;
}

message MarkGroupMsgsAsReadResp {}
```

In the `service msg {}` block (around line 480), add after `MarkConversationAsRead`:
```protobuf
  rpc MarkGroupMsgsAsRead(MarkGroupMsgsAsReadReq) returns (MarkGroupMsgsAsReadResp);
```

Apply the identical changes to `openim-sdk-core/protocol/msg/msg.proto`.

- [ ] **Step 2: Add pb.go structs (server copy)**

In `protocol/msg/msg.pb.go`, find the `MarkMsgsAsReadResp` struct block. After it, add:

```go
type MarkGroupMsgsAsReadReq struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	ConversationID string `protobuf:"bytes,1,opt,name=conversationID,proto3" json:"conversationID,omitempty"`
	Seqs          []int64 `protobuf:"varint,2,rep,packed,name=seqs,proto3" json:"seqs,omitempty"`
	UserID        string  `protobuf:"bytes,3,opt,name=userID,proto3" json:"userID,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *MarkGroupMsgsAsReadReq) Reset()         { *x = MarkGroupMsgsAsReadReq{} }
func (x *MarkGroupMsgsAsReadReq) String() string  { return protoimpl.X.MessageStringOf(x) }
func (*MarkGroupMsgsAsReadReq) ProtoMessage()      {}
func (x *MarkGroupMsgsAsReadReq) GetConversationID() string {
	if x != nil { return x.ConversationID }
	return ""
}
func (x *MarkGroupMsgsAsReadReq) GetSeqs() []int64 {
	if x != nil { return x.Seqs }
	return nil
}
func (x *MarkGroupMsgsAsReadReq) GetUserID() string {
	if x != nil { return x.UserID }
	return ""
}

type MarkGroupMsgsAsReadResp struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *MarkGroupMsgsAsReadResp) Reset()        { *x = MarkGroupMsgsAsReadResp{} }
func (x *MarkGroupMsgsAsReadResp) String() string { return protoimpl.X.MessageStringOf(x) }
func (*MarkGroupMsgsAsReadResp) ProtoMessage()     {}
```

Apply the identical changes to `openim-sdk-core/protocol/msg/msg.pb.go`.

- [ ] **Step 3: Add grpc.pb.go entries (server copy)**

In `protocol/msg/msg_grpc.pb.go`:

**3a. Add constant** (in the `const (...)` block, after `Msg_MarkConversationAsRead_FullMethodName`):
```go
Msg_MarkGroupMsgsAsRead_FullMethodName = "/openim.msg.msg/MarkGroupMsgsAsRead"
```

**3b. Add to `MsgClient` interface** (after `MarkConversationAsRead` line):
```go
MarkGroupMsgsAsRead(ctx context.Context, in *MarkGroupMsgsAsReadReq, opts ...grpc.CallOption) (*MarkGroupMsgsAsReadResp, error)
```

**3c. Add `msgClient` method** (after `func (c *msgClient) MarkConversationAsRead`):
```go
func (c *msgClient) MarkGroupMsgsAsRead(ctx context.Context, in *MarkGroupMsgsAsReadReq, opts ...grpc.CallOption) (*MarkGroupMsgsAsReadResp, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(MarkGroupMsgsAsReadResp)
	err := c.cc.Invoke(ctx, Msg_MarkGroupMsgsAsRead_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}
```

**3d. Add to `MsgServer` interface** (after `MarkConversationAsRead` line):
```go
MarkGroupMsgsAsRead(context.Context, *MarkGroupMsgsAsReadReq) (*MarkGroupMsgsAsReadResp, error)
```

**3e. Add `UnimplementedMsgServer` method** (after the existing `MarkConversationAsRead` unimplemented):
```go
func (UnimplementedMsgServer) MarkGroupMsgsAsRead(context.Context, *MarkGroupMsgsAsReadReq) (*MarkGroupMsgsAsReadResp, error) {
	return nil, status.Error(codes.Unimplemented, "method MarkGroupMsgsAsRead not implemented")
}
```

**3f. Add handler function** (after `_Msg_MarkConversationAsRead_Handler`):
```go
func _Msg_MarkGroupMsgsAsRead_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MarkGroupMsgsAsReadReq)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).MarkGroupMsgsAsRead(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Msg_MarkGroupMsgsAsRead_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).MarkGroupMsgsAsRead(ctx, req.(*MarkGroupMsgsAsReadReq))
	}
	return interceptor(ctx, in, info, handler)
}
```

**3g. Register in service descriptor** (in the `Methods` slice, after the `MarkConversationAsRead` entry):
```go
{
	MethodName: "MarkGroupMsgsAsRead",
	Handler:    _Msg_MarkGroupMsgsAsRead_Handler,
},
```

Apply the identical 3a–3g changes to `openim-sdk-core/protocol/msg/msg_grpc.pb.go`.

- [ ] **Step 4: Add Check() methods (both copies)**

In `protocol/msg/msg.go`, add after the `MarkMsgsAsReadReq.Check()` method:
```go
func (x *MarkGroupMsgsAsReadReq) Check() error {
	if x.ConversationID == "" {
		return errors.New("conversationID is empty")
	}
	if len(x.Seqs) == 0 {
		return errors.New("seqs is empty")
	}
	if x.UserID == "" {
		return errors.New("userID is empty")
	}
	for _, seq := range x.Seqs {
		if seq == 0 {
			return errors.New("seqs has 0 value is invalid")
		}
	}
	return nil
}
```

Apply the identical change to `openim-sdk-core/protocol/msg/msg.go`.

- [ ] **Step 5: Build-check both copies**

```bash
cd /path/to/repo
go build ./protocol/msg/...
go build ./openim-sdk-core/protocol/msg/...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add protocol/msg/ openim-sdk-core/protocol/msg/
git commit -m "feat: add MarkGroupMsgsAsRead proto + generated stubs"
```

---

### Task 2: Server — MongoDB implementation

**Files:**
- Modify: `pkg/common/storage/database/msg.go`
- Modify: `pkg/common/storage/database/mgo/msg.go`
- Modify: `pkg/common/storage/controller/msg.go`

**Interfaces:**
- Consumes: `GetMsgBySeqs(ctx, userID, conversationID, seqs)` (already exists, returns `minSeq, maxSeq, []*sdkws.MsgData, err`)
- Produces: `CommonMsgDatabase.MarkGroupChatMsgsAsRead(ctx, readerUserID, conversationID string, seqs []int64) (map[string][]int64, error)` — returns `senderID → []seq` for msgs that were first-marked-read

- [ ] **Step 1: Add interface method**

In `pkg/common/storage/database/msg.go`, add to the `CommonMsgDatabase` interface (after `MarkSingleChatMsgsAsRead`):
```go
// MarkGroupChatMsgsAsRead marks group messages as read (first reader wins).
// Returns a map of senderUserID -> []seq for messages that were newly marked read.
// Messages already read or sent by readerUserID are skipped.
MarkGroupChatMsgsAsRead(ctx context.Context, readerUserID, conversationID string, seqs []int64) (map[string][]int64, error)
```

- [ ] **Step 2: Add to MsgDocDatabase interface**

In `pkg/common/storage/database/msg.go`, find the `MsgDocDatabase` interface (the one with `MarkSingleChatMsgsAsRead(ctx, userID, docID string, indexes []int64)`). Add after it:
```go
// MarkGroupChatMsgsAsReadByIndex marks msgs in a single doc as read where is_read != true and send_id != readerUserID.
MarkGroupChatMsgsAsReadByIndex(ctx context.Context, readerUserID, docID string, indexes []int64) (int64, error)
```

- [ ] **Step 3: Implement MsgDocDatabase method in mgo**

In `pkg/common/storage/database/mgo/msg.go`, add after `MarkSingleChatMsgsAsRead`:
```go
func (m *MsgMgo) MarkGroupChatMsgsAsReadByIndex(ctx context.Context, readerUserID, docID string, indexes []int64) (int64, error) {
	var updates []mongo.WriteModel
	for _, index := range indexes {
		filter := bson.M{
			"doc_id": docID,
			fmt.Sprintf("msgs.%d.is_read", index): bson.M{"$ne": true},
			fmt.Sprintf("msgs.%d.msg.send_id", index): bson.M{"$ne": readerUserID},
		}
		update := bson.M{
			"$set": bson.M{
				fmt.Sprintf("msgs.%d.is_read", index): true,
			},
		}
		updates = append(updates, mongo.NewUpdateManyModel().SetFilter(filter).SetUpdate(update))
	}
	if len(updates) == 0 {
		return 0, nil
	}
	res, err := m.coll.BulkWrite(ctx, updates)
	if err != nil {
		return 0, errs.WrapMsg(err, fmt.Sprintf("docID is %s, indexes is %v", docID, indexes))
	}
	return res.ModifiedCount, nil
}
```

- [ ] **Step 4: Implement CommonMsgDatabase.MarkGroupChatMsgsAsRead in controller**

In `pkg/common/storage/controller/msg.go`, add after `MarkSingleChatMsgsAsRead`:
```go
func (db *commonMsgDatabase) MarkGroupChatMsgsAsRead(ctx context.Context, readerUserID, conversationID string, seqs []int64) (map[string][]int64, error) {
	// Step 1: fetch messages to get send_id and current is_read state
	_, _, msgs, err := db.getMsgBySeqs(ctx, readerUserID, conversationID, seqs)
	if err != nil {
		return nil, err
	}
	// Step 2: filter to unread messages not sent by reader
	type msgInfo struct {
		seq      int64
		senderID string
	}
	var candidates []msgInfo
	for _, msg := range msgs {
		if msg == nil || msg.IsRead || msg.SendID == readerUserID {
			continue
		}
		candidates = append(candidates, msgInfo{seq: msg.Seq, senderID: msg.SendID})
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	// Step 3: collect seqs for BulkWrite
	candidateSeqs := make([]int64, 0, len(candidates))
	for _, c := range candidates {
		candidateSeqs = append(candidateSeqs, c.seq)
	}
	// Step 4: BulkWrite with is_read != true filter (CAS-safe)
	for docID, seqsInDoc := range db.msgTable.GetDocIDSeqsMap(conversationID, candidateSeqs) {
		var indexes []int64
		for _, seq := range seqsInDoc {
			indexes = append(indexes, db.msgTable.GetMsgIndex(seq))
		}
		if _, err := db.msgDocDatabase.MarkGroupChatMsgsAsReadByIndex(ctx, readerUserID, docID, indexes); err != nil {
			log.ZError(ctx, "MarkGroupChatMsgsAsReadByIndex", err, "docID", docID, "indexes", indexes)
			return nil, err
		}
	}
	// Step 5: group candidates by sender (use candidates list — slight over-notify on race, acceptable)
	senderSeqMap := make(map[string][]int64)
	for _, c := range candidates {
		senderSeqMap[c.senderID] = append(senderSeqMap[c.senderID], c.seq)
	}
	// Invalidate cache for updated seqs
	_ = db.msgCache.DelMessageBySeqs(ctx, conversationID, candidateSeqs)
	return senderSeqMap, nil
}
```

- [ ] **Step 5: Build-check server storage layer**

```bash
cd /path/to/repo
go build ./pkg/common/storage/...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add pkg/common/storage/
git commit -m "feat: implement MarkGroupChatMsgsAsRead in storage layer"
```

---

### Task 3: Server — gRPC handler + HTTP endpoint

**Files:**
- Modify: `internal/rpc/msg/as_read.go`
- Modify: `internal/api/msg.go`
- Modify: `internal/api/router.go`

**Interfaces:**
- Consumes: `CommonMsgDatabase.MarkGroupChatMsgsAsRead(ctx, readerUserID, conversationID, seqs) (map[string][]int64, error)`
- Consumes: `ConversationLocalCache.GetConversation(ctx, userID, conversationID)`
- Consumes: `notificationSender.NotificationWithSessionType(ctx, sendID, recvID, contentType, sessionType, tips)`
- Produces: `msgServer.MarkGroupMsgsAsRead(ctx, *msg.MarkGroupMsgsAsReadReq) (*msg.MarkGroupMsgsAsReadResp, error)`

- [ ] **Step 1: Add gRPC handler in as_read.go**

In `internal/rpc/msg/as_read.go`, add at the end of the file:
```go
func (m *msgServer) MarkGroupMsgsAsRead(ctx context.Context, req *msg.MarkGroupMsgsAsReadReq) (*msg.MarkGroupMsgsAsReadResp, error) {
	conversation, err := m.ConversationLocalCache.GetConversation(ctx, req.UserID, req.ConversationID)
	if err != nil {
		return nil, err
	}
	if conversation.ConversationType != constant.ReadGroupChatType {
		return nil, errs.ErrArgs.WrapMsg("conversation is not a group chat")
	}
	senderSeqMap, err := m.MsgDatabase.MarkGroupChatMsgsAsRead(ctx, req.UserID, req.ConversationID, req.Seqs)
	if err != nil {
		return nil, err
	}
	for senderID, seqs := range senderSeqMap {
		// Use SingleChatType so the notification routes as a p2p message directly to senderID.
		// ReadGroupChatType would set GroupID=recvID which is wrong here.
		m.sendMarkAsReadNotification(ctx, req.ConversationID, constant.SingleChatType, req.UserID, senderID, seqs, 0)
	}
	return &msg.MarkGroupMsgsAsReadResp{}, nil
}
```

- [ ] **Step 2: Add HTTP handler in msg.go**

In `internal/api/msg.go`, add after `MarkConversationAsRead`:
```go
func (m *MessageApi) MarkGroupMsgsAsRead(c *gin.Context) {
	a2r.Call(c, msg.MsgClient.MarkGroupMsgsAsRead, m.Client)
}
```

- [ ] **Step 3: Register route in router.go**

In `internal/api/router.go`, find:
```go
msgGroup.POST("/mark_conversation_as_read", m.MarkConversationAsRead)
```
Add after it:
```go
msgGroup.POST("/mark_group_msgs_as_read", m.MarkGroupMsgsAsRead)
```

- [ ] **Step 4: Build-check server**

```bash
cd /path/to/repo
go build ./internal/...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/rpc/msg/as_read.go internal/api/msg.go internal/api/router.go
git commit -m "feat: add MarkGroupMsgsAsRead gRPC handler and HTTP route"
```

---

### Task 4: SDK — callback interface + API endpoint

**Files:**
- Modify: `openim-sdk-core/open_im_sdk_callback/callback_client.go`
- Modify: `openim-sdk-core/pkg/api/api.go`
- Modify: `openim-sdk-core/internal/conversation_msg/server_api.go`

**Interfaces:**
- Produces: `OnAdvancedMsgListener.OnRecvGroupReadReceipt(groupMsgReceiptList string)`
- Produces: `api.MarkGroupMsgsAsRead` (API endpoint)
- Produces: `Conversation.markGroupMsgsAsRead2Server(ctx, conversationID string, seqs []int64) error`

- [ ] **Step 1: Add OnRecvGroupReadReceipt to OnAdvancedMsgListener interface**

In `openim-sdk-core/open_im_sdk_callback/callback_client.go`, find:
```go
type OnAdvancedMsgListener interface {
	OnRecvNewMessage(message string)
	OnRecvC2CReadReceipt(msgReceiptList string)
	OnNewRecvMessageRevoked(messageRevoked string)
	OnRecvOfflineNewMessage(message string)
	OnMsgDeleted(message string)
	OnRecvOnlineOnlyMessage(message string)
}
```
Change to:
```go
type OnAdvancedMsgListener interface {
	OnRecvNewMessage(message string)
	OnRecvC2CReadReceipt(msgReceiptList string)
	OnRecvGroupReadReceipt(groupMsgReceiptList string)
	OnNewRecvMessageRevoked(messageRevoked string)
	OnRecvOfflineNewMessage(message string)
	OnMsgDeleted(message string)
	OnRecvOnlineOnlyMessage(message string)
}
```

Note: `emptyAdvancedMsgListener` in `open_im_sdk/em.go` already implements `OnRecvGroupReadReceipt` — no change needed there.

- [ ] **Step 2: Add API endpoint**

In `openim-sdk-core/pkg/api/api.go`, find the `var (` block containing `MarkMsgsAsRead`. Add:
```go
MarkGroupMsgsAsRead = newApi[msg.MarkGroupMsgsAsReadReq, msg.MarkGroupMsgsAsReadResp]("/msg/mark_group_msgs_as_read")
```

- [ ] **Step 3: Add server_api helper**

In `openim-sdk-core/internal/conversation_msg/server_api.go`, add after `markMsgAsRead2Server`:
```go
func (c *Conversation) markGroupMsgsAsRead2Server(ctx context.Context, conversationID string, seqs []int64) error {
	req := &pbMsg.MarkGroupMsgsAsReadReq{
		UserID:         c.loginUserID,
		ConversationID: conversationID,
		Seqs:           seqs,
	}
	return api.MarkGroupMsgsAsRead.Execute(ctx, req)
}
```

- [ ] **Step 4: Build-check SDK**

```bash
cd /path/to/repo
go build ./openim-sdk-core/...
```

Expected: any implementations of `OnAdvancedMsgListener` that don't include `OnRecvGroupReadReceipt` will fail to compile. Fix any found in test/integration files by adding the empty method.

- [ ] **Step 5: Fix test/integration listener implementations**

Check for compile errors. Any struct implementing `OnAdvancedMsgListener` in test files needs:
```go
func (l *YourListener) OnRecvGroupReadReceipt(groupMsgReceiptList string) {}
```

Files to check:
- `openim-sdk-core/test/listener.go`
- `openim-sdk-core/integration_test/internal/pkg/sdk_user_simulator/listener.go`
- `openim-sdk-core/msgtest/sdk_user_simulator/listener.go`
- `openim-sdk-core/wasm/event_listener/listener.go` (already has it — skip)

- [ ] **Step 6: Commit**

```bash
git add openim-sdk-core/open_im_sdk_callback/ openim-sdk-core/pkg/api/api.go \
        openim-sdk-core/internal/conversation_msg/server_api.go \
        openim-sdk-core/test/ openim-sdk-core/integration_test/ openim-sdk-core/msgtest/
git commit -m "feat: add OnRecvGroupReadReceipt to SDK interface and MarkGroupMsgsAsRead API"
```

---

### Task 5: SDK — doReadDrawing group branch + markConversationMessageAsRead

**Files:**
- Modify: `openim-sdk-core/internal/conversation_msg/read_drawing.go`

**Interfaces:**
- Consumes: `c.db.GetMessagesBySeqs(ctx, conversationID, seqs) ([]*model_struct.LocalChatLog, error)` — already used in single-chat branch
- Consumes: `c.db.UpdateMessage(ctx, conversationID, message)` — already used in single-chat branch
- Consumes: `c.msgListener().OnRecvGroupReadReceipt(string)` — newly added
- Consumes: `c.markGroupMsgsAsRead2Server(ctx, conversationID, seqs)` — from Task 4
- Consumes: `c.getAsReadMsgMapAndList(ctx, msgs)` — returns `([]clientMsgID, []seq)`

- [ ] **Step 1: Add group branch in doReadDrawing**

In `openim-sdk-core/internal/conversation_msg/read_drawing.go`, find the `doReadDrawing` function.
The current code has this structure inside the `if tips.MarkAsReadUserID != c.loginUserID` block:

```go
if conversation.ConversationType == constant.SingleChatType {
    // ... single chat logic ...
    c.msgListener().OnRecvC2CReadReceipt(utils.StructToJsonString(messageReceiptResp))
}
```

Add an `else if` branch immediately after the closing `}` of the `SingleChatType` block:

```go
} else if conversation.ConversationType == constant.ReadGroupChatType {
    var successMsgIDs []string
    for _, message := range messages {
        if message.IsRead {
            continue
        }
        message.IsRead = true
        if err = c.db.UpdateMessage(ctx, tips.ConversationID, message); err != nil {
            log.ZWarn(ctx, "UpdateMessage err", err, "conversationID", tips.ConversationID, "message", message)
        } else {
            successMsgIDs = append(successMsgIDs, message.ClientMsgID)
        }
    }
    if len(successMsgIDs) > 0 {
        receipt := []*sdk_struct.MessageReceipt{{
            GroupID:     conversation.GroupID,
            UserID:      tips.MarkAsReadUserID,
            MsgIDList:   successMsgIDs,
            SessionType: conversation.ConversationType,
            ReadTime:    msg.SendTime,
        }}
        c.msgListener().OnRecvGroupReadReceipt(utils.StructToJsonString(receipt))
    }
}
```

- [ ] **Step 2: Verify the messages fetch for group path**

In `doReadDrawing`, the block starting at `if tips.MarkAsReadUserID != c.loginUserID` currently contains:

```go
if len(tips.Seqs) == 0 {
    return errs.New("tips Seqs is empty").Wrap()
}
messages, err := c.db.GetMessagesBySeqs(ctx, tips.ConversationID, tips.Seqs)
```

This fetch already happens before the `if conversation.ConversationType == constant.SingleChatType` check, so the `messages` variable is available for the new group branch. No change needed.

- [ ] **Step 3: Update markConversationMessageAsRead group path**

In the same file, find the `markConversationMessageAsRead` function, specifically this block:

```go
case constant.ReadGroupChatType, constant.NotificationChatType:
    log.ZDebug(ctx, "markConversationMessageAsRead", "conversationID", conversationID, "peerUserMaxSeq", peerUserMaxSeq, "maxSeq", maxSeq)
    if err := c.markConversationAsReadServer(ctx, conversationID, maxSeq, nil); err != nil {
        return err
    }
```

Replace it with:

```go
case constant.ReadGroupChatType, constant.NotificationChatType:
    log.ZDebug(ctx, "markConversationMessageAsRead", "conversationID", conversationID, "peerUserMaxSeq", peerUserMaxSeq, "maxSeq", maxSeq)
    if conversation.ConversationType == constant.ReadGroupChatType {
        unreadMsgs, err := c.db.GetUnreadMessage(ctx, conversationID)
        if err != nil {
            log.ZWarn(ctx, "GetUnreadMessage err", err, "conversationID", conversationID)
        } else {
            _, seqs := c.getAsReadMsgMapAndList(ctx, unreadMsgs)
            if len(seqs) > 0 {
                if err := c.markGroupMsgsAsRead2Server(ctx, conversationID, seqs); err != nil {
                    log.ZWarn(ctx, "markGroupMsgsAsRead2Server err", err, "conversationID", conversationID)
                }
            }
        }
    }
    if err := c.markConversationAsReadServer(ctx, conversationID, maxSeq, nil); err != nil {
        return err
    }
```

- [ ] **Step 4: Build-check SDK**

```bash
cd /path/to/repo
go build ./openim-sdk-core/...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add openim-sdk-core/internal/conversation_msg/read_drawing.go
git commit -m "feat: handle group HasReadReceipt in doReadDrawing and trigger markGroupMsgsAsRead2Server"
```

---

### Task 6: End-to-end smoke test

**Files:**
- Read: `openim-sdk-core/integration_test/` for test patterns

**Interfaces:**
- Consumes: all tasks above

- [ ] **Step 1: Manual smoke test via curl (server side)**

Start the server locally. As user A, send a group message. As user B, call:
```bash
curl -X POST http://localhost:10002/msg/mark_group_msgs_as_read \
  -H "Content-Type: application/json" \
  -H "token: <userB_token>" \
  -d '{"conversationID":"sg_<groupID>","seqs":[1],"userID":"userB"}'
```
Expected response: `{"errCode":0,"errMsg":"","data":{}}`

- [ ] **Step 2: Verify MongoDB is_read=true**

Connect to MongoDB and check:
```js
db.msg.find({"doc_id": /^sg_<groupID>/}).forEach(d => {
  d.msgs.forEach((m,i) => { if(m && m.msg && m.msg.seq == 1) print(i, m.is_read) })
})
```
Expected: `is_read: true` for the msg at seq=1.

- [ ] **Step 3: Verify user A receives HasReadReceipt**

User A's SDK should fire `OnRecvGroupReadReceipt`. In a test listener, log the callback payload.
Expected JSON: `[{"groupID":"<groupID>","userID":"userB","msgIDList":["<clientMsgID>"],"sessionType":3,"readTime":<ts>}]`

- [ ] **Step 4: Idempotency check**

Call the curl from Step 1 a second time (user B reads again). MongoDB should still show `is_read: true`. User A should NOT receive a second `OnRecvGroupReadReceipt` callback (because the BulkWrite filter `is_read != true` matches 0 documents, and no seqs pass the `is_read==false` filter in `MarkGroupChatMsgsAsRead`).

- [ ] **Step 5: Final commit**

```bash
git add .
git commit -m "feat: complete group chat read receipt (any-one-reader semantics)"
```

