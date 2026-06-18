# Group Chat Read Receipt Design

**Date:** 2026-06-19  
**Status:** Approved

---

## Goal

Enable group chat read receipts with "any-one-reader" semantics: once any group member reads a message, the message sender sees it as "已读". All members can also see the read status when they fetch messages.

---

## Background

Currently OpenIM supports:
- **Single chat**: Full read receipt — sender sees `OnRecvC2CReadReceipt` when peer reads. Messages get `is_read=true` in MongoDB and `HasReadTime` in `AttachedInfo`.
- **Group chat**: Only `hasReadSeq` per user (for unread count). No per-message `is_read` marking, no notification to sender.

The `em.go` already has an empty `OnRecvGroupReadReceipt` implementation on `emptyAdvancedMsgListener` but the `OnAdvancedMsgListener` interface does not include it yet, and the server/SDK paths are not wired up.

---

## Requirements

1. **New API** `POST /msg/mark_group_msgs_as_read`: a group member submits the seqs they have read.
2. **Idempotent**: only the *first* reader triggers the receipt notification to the sender. Subsequent readers are silently ignored.
3. **Sender notification**: the message sender receives `OnRecvGroupReadReceipt` with the list of their message IDs that were read.
4. **All-member visibility**: when any member fetches messages, the `is_read` field reflects whether the message has been read by anyone. (Free because `is_read` lives in the MongoDB msg document shared by all members.)
5. **Excluded**: messages sent by the reader themselves are never counted as "read by someone else".

---

## Data Flow

```
Member B opens group, views msgs seq=[1,2,3]
  │
  ▼
SDK markGroupMsgsAsRead2Server(conversationID, seqs=[1,2,3])
  → POST /msg/mark_group_msgs_as_read {conversationID, seqs, userID:"B"}
  │
  ▼
Server (MarkGroupMsgsAsRead handler):
  1. Validate conversation is ReadGroupChatType
  2. GetMsgBySeqs → {seq, send_id, is_read, clientMsgID} per seq
  3. Filter: is_read==false AND send_id != "B"
  4. MongoDB BulkWrite: set is_read=true where is_read != true (CAS-safe)
  5. Group filtered msgs by send_id:
       A: clientMsgIDs=[msg1,msg3]  → HasReadReceipt to A
       C: clientMsgIDs=[msg2]       → HasReadReceipt to C
  6. Notification: sendID=B, recvID=sender, contentType=HasReadReceipt,
                   sessionType=ReadGroupChatType
  │
  ▼
Member A's SDK receives HasReadReceipt (sessionType=ReadGroupChatType):
  doReadDrawing:
    MarkAsReadUserID("B") != loginUserID("A") && ConversationType==ReadGroupChatType
    → update local messages IsRead=true
    → OnRecvGroupReadReceipt([{GroupID, UserID:"B", MsgIDList:[...], SessionType, ReadTime}])
  │
  ▼
Other members: fetch messages → is_read=true already in MongoDB
```

---

## API

```
POST /msg/mark_group_msgs_as_read
{ "conversationID": "sg_xxx", "seqs": [1,2,3], "userID": "B" }
→ {}
```

Errors: 400 if seqs empty or conversation not ReadGroupChatType.

---

## Proto Changes (apply to both copies)

Files: `protocol/msg/msg.proto` AND `openim-sdk-core/protocol/msg/msg.proto`

```protobuf
message MarkGroupMsgsAsReadReq {
  string conversationID = 1;
  repeated int64 seqs   = 2;
  string userID         = 3;
}
message MarkGroupMsgsAsReadResp {}

// in service msg {}
rpc MarkGroupMsgsAsRead(MarkGroupMsgsAsReadReq) returns (MarkGroupMsgsAsReadResp);
```

The generated `.pb.go` and `_grpc.pb.go` files are edited manually (no protoc toolchain step needed).

---

## Server Files

| File | Change |
|------|--------|
| `protocol/msg/msg.proto` | Add req/resp messages + RPC |
| `protocol/msg/msg.pb.go` | Manual: add structs |
| `protocol/msg/msg_grpc.pb.go` | Manual: add client/server stubs |
| `protocol/msg/msg.go` | Add Check() for MarkGroupMsgsAsReadReq |
| `pkg/common/storage/database/msg.go` | Add MarkGroupChatMsgsAsRead to interface |
| `pkg/common/storage/database/mgo/msg.go` | Implement it |
| `pkg/common/storage/controller/msg.go` | Wire it up |
| `internal/rpc/msg/as_read.go` | Add MarkGroupMsgsAsRead handler |
| `internal/api/msg.go` | Add HTTP handler |
| `internal/api/router.go` | Register route |

---

## SDK Files

| File | Change |
|------|--------|
| `openim-sdk-core/protocol/msg/msg.proto` | Same as server proto |
| `openim-sdk-core/protocol/msg/msg.pb.go` | Manual: add structs |
| `openim-sdk-core/protocol/msg/msg_grpc.pb.go` | Manual: add stubs |
| `openim-sdk-core/protocol/msg/msg.go` | Add Check() |
| `openim-sdk-core/open_im_sdk_callback/callback_client.go` | Add OnRecvGroupReadReceipt to OnAdvancedMsgListener |
| `openim-sdk-core/pkg/api/api.go` | Add MarkGroupMsgsAsRead endpoint |
| `openim-sdk-core/internal/conversation_msg/server_api.go` | Add markGroupMsgsAsRead2Server |
| `openim-sdk-core/internal/conversation_msg/read_drawing.go` | Add group branch in doReadDrawing; call new API in markConversationMessageAsRead |

---

## Key Implementation Details

### Idempotency

1. `GetMsgBySeqs` → get `{send_id, is_read, clientMsgID}` per seq
2. Local filter: `is_read==false && send_id != readerUserID`
3. BulkWrite with extra filter `is_read != true` (CAS-safe, no double-write)
4. Send receipt only for step-2 set (slight over-notify on race, deduped by SDK local `IsRead` check)

### Notification (server)

```go
for senderID, seqsForSender := range senderSeqsMap {
    m.notificationSender.NotificationWithSessionType(ctx,
        req.UserID, senderID,
        constant.HasReadReceipt,
        constant.ReadGroupChatType,
        &sdkws.MarkAsReadTips{
            MarkAsReadUserID: req.UserID,
            ConversationID:   req.ConversationID,
            Seqs:             seqsForSender,
        })
}
```

### SDK doReadDrawing group branch

```go
} else if conversation.ConversationType == constant.ReadGroupChatType {
    var successMsgIDs []string
    for _, message := range messages {
        if message.IsRead {
            continue
        }
        message.IsRead = true
        if err = c.db.UpdateMessage(ctx, tips.ConversationID, message); err == nil {
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

### SDK markConversationMessageAsRead group path

```go
case constant.ReadGroupChatType, constant.NotificationChatType:
    msgs, err := c.db.GetUnreadMessage(ctx, conversationID)
    if err != nil {
        return err
    }
    _, seqs := c.getAsReadMsgMapAndList(ctx, msgs)
    if len(seqs) > 0 {
        if err := c.markGroupMsgsAsRead2Server(ctx, conversationID, seqs); err != nil {
            log.ZWarn(ctx, "markGroupMsgsAsRead2Server err", err)
        }
    }
    // keep existing: update hasReadSeq for unread count
    if err := c.markConversationAsReadServer(ctx, conversationID, maxSeq, nil); err != nil {
        return err
    }
```

---

## Out of Scope

- "Read by N members" count per message
- List of all members who read a specific message
- Persisting reader identity server-side (only first-read boolean stored)
- Single chat logic — unchanged
