package push

import (
	"context"
	"encoding/json"
	"math/rand"
	"strconv"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/IBM/sarama"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/kafka"
	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/open-im-server/v3/pkg/rpccache"
	"github.com/openimsdk/open-im-server/v3/pkg/util/conversationutil"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/msggateway"
	pbpush "github.com/openimsdk/protocol/push"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/timeutil"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

type ConsumerHandler struct {
	pushConsumerGroup      *kafka.MConsumerGroup
	offlinePusher          offlinepush.OfflinePusher
	onlinePusher           OnlinePusher
	pushDatabase           controller.PushDatabase
	groupMuteDB            controller.GroupMuteDatabase
	onlineCache            *rpccache.OnlineCache
	groupLocalCache        *rpccache.GroupLocalCache
	conversationLocalCache *rpccache.ConversationLocalCache
	userLocalCache         *rpccache.UserLocalCache
	webhookClient          *webhook.Client
	config                 *Config
	userClient             *rpcli.UserClient
	groupClient            *rpcli.GroupClient
	msgClient              *rpcli.MsgClient
	conversationClient     *rpcli.ConversationClient
}

func NewConsumerHandler(ctx context.Context, config *Config, database controller.PushDatabase, offlinePusher offlinepush.OfflinePusher, rdb redis.UniversalClient,
	client discovery.SvcDiscoveryRegistry, groupMuteDB controller.GroupMuteDatabase) (*ConsumerHandler, error) {
	var consumerHandler ConsumerHandler
	var err error
	consumerHandler.pushConsumerGroup, err = kafka.NewMConsumerGroup(config.KafkaConfig.Build(), config.KafkaConfig.ToPushGroupID,
		[]string{config.KafkaConfig.ToPushTopic}, true)
	if err != nil {
		return nil, err
	}
	userConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.User)
	if err != nil {
		return nil, err
	}
	groupConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Group)
	if err != nil {
		return nil, err
	}
	msgConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Msg)
	if err != nil {
		return nil, err
	}
	conversationConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Conversation)
	if err != nil {
		return nil, err
	}
	consumerHandler.userClient = rpcli.NewUserClient(userConn)
	consumerHandler.groupClient = rpcli.NewGroupClient(groupConn)
	consumerHandler.msgClient = rpcli.NewMsgClient(msgConn)
	consumerHandler.conversationClient = rpcli.NewConversationClient(conversationConn)
	consumerHandler.groupMuteDB = groupMuteDB

	consumerHandler.offlinePusher = offlinePusher
	consumerHandler.onlinePusher = NewOnlinePusher(client, config)
	consumerHandler.groupLocalCache = rpccache.NewGroupLocalCache(consumerHandler.groupClient, &config.LocalCacheConfig, rdb)
	consumerHandler.conversationLocalCache = rpccache.NewConversationLocalCache(consumerHandler.conversationClient, &config.LocalCacheConfig, rdb)
	consumerHandler.userLocalCache = rpccache.NewUserLocalCache(consumerHandler.userClient, &config.LocalCacheConfig, rdb)
	consumerHandler.webhookClient = webhook.NewWebhookClient(config.WebhooksConfig.URL)
	consumerHandler.config = config
	consumerHandler.pushDatabase = database
	consumerHandler.onlineCache, err = rpccache.NewOnlineCache(consumerHandler.userClient, consumerHandler.groupLocalCache, rdb, config.RpcConfig.FullUserCache, nil)
	if err != nil {
		return nil, err
	}
	return &consumerHandler, nil
}

func (c *ConsumerHandler) handleMs2PsChat(ctx context.Context, msg []byte) {
	msgFromMQ := pbpush.PushMsgReq{}
	if err := proto.Unmarshal(msg, &msgFromMQ); err != nil {
		log.ZError(ctx, "push Unmarshal msg err", err, "msg", string(msg))
		return
	}

	sec := msgFromMQ.MsgData.SendTime / 1000
	nowSec := timeutil.GetCurrentTimestampBySecond()

	if nowSec-sec > 10 {
		prommetrics.MsgLoneTimePushCounter.Inc()
		log.ZWarn(ctx, "it’s been a while since the message was sent", nil, "msg", msgFromMQ.String(), "sec", sec, "nowSec", nowSec, "nowSec-sec", nowSec-sec)
	}
	var err error

	switch msgFromMQ.MsgData.SessionType {
	case constant.ReadGroupChatType:
		err = c.Push2Group(ctx, msgFromMQ.MsgData.GroupID, msgFromMQ.MsgData)
	default:
		var pushUserIDList []string
		isSenderSync := datautil.GetSwitchFromOptions(msgFromMQ.MsgData.Options, constant.IsSenderSync)
		if !isSenderSync || msgFromMQ.MsgData.SendID == msgFromMQ.MsgData.RecvID {
			pushUserIDList = append(pushUserIDList, msgFromMQ.MsgData.RecvID)
		} else {
			pushUserIDList = append(pushUserIDList, msgFromMQ.MsgData.RecvID, msgFromMQ.MsgData.SendID)
		}
		err = c.Push2User(ctx, pushUserIDList, msgFromMQ.MsgData)
	}
	if err != nil {
		log.ZWarn(ctx, "push failed", err, "msg", msgFromMQ.String())
	}
}

func (*ConsumerHandler) Setup(sarama.ConsumerGroupSession) error { return nil }

func (*ConsumerHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (c *ConsumerHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	c.onlineCache.Lock.Lock()
	for c.onlineCache.CurrentPhase.Load() < rpccache.DoSubscribeOver {
		c.onlineCache.Cond.Wait()
	}
	c.onlineCache.Lock.Unlock()
	ctx := mcontext.SetOperationID(context.TODO(), strconv.FormatInt(time.Now().UnixNano()+int64(rand.Uint32()), 10))
	log.ZDebug(ctx, "ConsumeClaim begin consume messages")

	for msg := range claim.Messages() {
		ctx := c.pushConsumerGroup.GetContextFromMsg(msg)
		ctx = mcontext.WithOpUserIDContext(ctx, c.config.Share.IMAdminUserID[0])
		c.handleMs2PsChat(ctx, msg.Value)
		sess.MarkMessage(msg, "")
	}
	return nil
}

// Push2User Suitable for two types of conversations, one is SingleChatType and the other is NotificationChatType.
func (c *ConsumerHandler) Push2User(ctx context.Context, userIDs []string, msg *sdkws.MsgData) (err error) {
	log.ZInfo(ctx, "Get msg from msg_transfer And push msg", "userIDs", userIDs, "msg", msg.String())
	defer func(duration time.Time) {
		t := time.Since(duration)
		log.ZInfo(ctx, "Get msg from msg_transfer And push msg end", "msg", msg.String(), "time cost", t)
	}(time.Now())
	if err := c.webhookBeforeOnlinePush(ctx, &c.config.WebhooksConfig.BeforeOnlinePush, userIDs, msg); err != nil {
		return err
	}

	wsResults, err := c.GetConnsAndOnlinePush(ctx, msg, userIDs)
	if err != nil {
		return err
	}

	log.ZDebug(ctx, "single and notification push result", "result", wsResults, "msg", msg, "push_to_userID", userIDs)
	if isSignalingNotification(msg.ContentType) {
		log.ZInfo(ctx, "lintao signaling push online check",
			"recvID", msg.RecvID,
			"sendID", msg.SendID,
			"clientMsgID", msg.ClientMsgID,
			"signalingPayloadType", msgprocessor.SignalingPayloadTypeName(msg.Content),
			"offlinePushOption", datautil.GetSwitchFromOptions(msg.Options, constant.IsOfflinePush),
			"offlinePushTitle", offlinePushSummary(msg).title,
			"wsResults", wsResults,
		)
	}
	log.ZInfo(ctx, "single and notification push end")

	if !c.shouldPushOffline(ctx, msg) {
		return nil
	}
	if isSignalingNotification(msg.ContentType) {
		log.ZInfo(ctx, "lintao signaling offline push start",
			"recvID", msg.RecvID,
			"sendID", msg.SendID,
			"clientMsgID", msg.ClientMsgID,
			"sessionType", msg.SessionType,
			"groupID", msg.GroupID,
			"offlinePushTitle", offlinePushSummary(msg).title,
			"offlinePushExLen", offlinePushSummary(msg).exLen,
		)
	}
	log.ZInfo(ctx, "pushOffline start", "userIDs", userIDs, "msg", msg.String())

	for _, v := range wsResults {
		//message sender do not need offline push
		if msg.SendID == v.UserID {
			continue
		}
		//receiver online push success
		if v.OnlinePush {
			if isSignalingNotification(msg.ContentType) {
				log.ZInfo(ctx, "lintao signaling offline push skipped: receiver online",
					"userID", v.UserID,
					"clientMsgID", msg.ClientMsgID,
					"recvID", msg.RecvID,
					"sendID", msg.SendID,
				)
			} else {
				log.ZDebug(ctx, "lintao offline push skipped: receiver already received via online push", "userID", v.UserID, "clientMsgID", msg.ClientMsgID)
			}
			return nil
		}
	}
	needOfflinePushUserID := []string{msg.RecvID}
	var offlinePushUserID []string

	//receiver offline push
	if err = c.webhookBeforeOfflinePush(ctx, &c.config.WebhooksConfig.BeforeOfflinePush, needOfflinePushUserID, msg, &offlinePushUserID); err != nil {
		return err
	}

	if len(offlinePushUserID) > 0 {
		needOfflinePushUserID = offlinePushUserID
	}
	needOfflinePushUserID, err = filterOfflinePushByNotificationSwitch(ctx, c.userLocalCache, msg, needOfflinePushUserID)
	if err != nil {
		return err
	}
	if len(needOfflinePushUserID) == 0 {
		if isSignalingNotification(msg.ContentType) {
			log.ZInfo(ctx, "lintao signaling offline push skipped: no eligible users after filter",
				"clientMsgID", msg.ClientMsgID,
				"recvID", msg.RecvID,
				"sendID", msg.SendID,
				"contentType", msg.ContentType,
			)
		} else {
			log.ZDebug(ctx, "lintao offline push skipped: all users disabled notification switch", "clientMsgID", msg.ClientMsgID, "contentType", msg.ContentType)
		}
		return nil
	}
	err = c.offlinePushMsg(ctx, msg, needOfflinePushUserID)
	if err != nil {
		log.ZWarn(ctx, "lintao offlinePushMsg failed", err, "needOfflinePushUserID length", len(needOfflinePushUserID), "msg", msg)
		return nil
	}

	return nil
}

func (c *ConsumerHandler) shouldPushOffline(ctx context.Context, msg *sdkws.MsgData) bool {
	isOfflinePush := datautil.GetSwitchFromOptions(msg.Options, constant.IsOfflinePush)
	if !isOfflinePush {
		if isSignalingNotification(msg.ContentType) {
			log.ZInfo(ctx, "lintao signaling offline push skipped: IsOfflinePush option false",
				"clientMsgID", msg.ClientMsgID,
				"recvID", msg.RecvID,
				"sendID", msg.SendID,
				"contentType", msg.ContentType,
			)
		} else {
			log.ZDebug(ctx, "lintao offline push skipped: IsOfflinePush option not set", "clientMsgID", msg.ClientMsgID, "contentType", msg.ContentType)
		}
		return false
	}
	switch msg.ContentType {
	case constant.RoomParticipantsConnectedNotification:
		log.ZDebug(ctx, "offline push skipped: RoomParticipantsConnectedNotification", "clientMsgID", msg.ClientMsgID)
		return false
	case constant.RoomParticipantsDisconnectedNotification:
		log.ZDebug(ctx, "offline push skipped: RoomParticipantsDisconnectedNotification", "clientMsgID", msg.ClientMsgID)
		return false
	}
	if isSignalingNotification(msg.ContentType) && !msgprocessor.IsOfflinePushSignalingContent(msg.Content) {
		log.ZInfo(ctx, "lintao signaling offline push skipped: non-offline-push signaling",
			"clientMsgID", msg.ClientMsgID,
			"recvID", msg.RecvID,
			"sendID", msg.SendID,
			"contentType", msg.ContentType,
			"signalingPayloadType", msgprocessor.SignalingPayloadTypeName(msg.Content),
			"offlinePushTitle", offlinePushSummary(msg).title,
			"hasOfflinePushInfo", msg.OfflinePushInfo != nil,
		)
		return false
	}
	if isSignalingNotification(msg.ContentType) {
		log.ZInfo(ctx, "lintao signaling offline push allowed",
			"clientMsgID", msg.ClientMsgID,
			"recvID", msg.RecvID,
			"sendID", msg.SendID,
			"signalingPayloadType", msgprocessor.SignalingPayloadTypeName(msg.Content),
			"offlinePushTitle", offlinePushSummary(msg).title,
		)
	}
	return true
}

func (c *ConsumerHandler) GetConnsAndOnlinePush(ctx context.Context, msg *sdkws.MsgData, pushToUserIDs []string) ([]*msggateway.SingleMsgToUserResults, error) {
	if msg != nil && msg.Status == constant.MsgStatusSending {
		msg.Status = constant.MsgStatusSendSuccess
	}
	onlineUserIDs, offlineUserIDs, err := c.onlineCache.GetUsersOnline(ctx, pushToUserIDs)
	if err != nil {
		return nil, err
	}

	log.ZDebug(ctx, "GetConnsAndOnlinePush online cache", "sendID", msg.SendID, "recvID", msg.RecvID, "groupID", msg.GroupID, "sessionType", msg.SessionType, "clientMsgID", msg.ClientMsgID, "serverMsgID", msg.ServerMsgID, "offlineUserIDs", offlineUserIDs, "onlineUserIDs", onlineUserIDs)
	var result []*msggateway.SingleMsgToUserResults
	if len(onlineUserIDs) > 0 {
		var err error
		result, err = c.onlinePusher.GetConnsAndOnlinePush(ctx, msg, onlineUserIDs)
		if err != nil {
			return nil, err
		}
	}
	for _, userID := range offlineUserIDs {
		result = append(result, &msggateway.SingleMsgToUserResults{
			UserID: userID,
		})
	}
	return result, nil
}

func (c *ConsumerHandler) Push2Group(ctx context.Context, groupID string, msg *sdkws.MsgData) (err error) {
	log.ZInfo(ctx, "Get group msg from msg_transfer and push msg", "msg", msg.String(), "groupID", groupID)
	defer func(duration time.Time) {
		t := time.Since(duration)
		log.ZInfo(ctx, "Get group msg from msg_transfer and push msg end", "msg", msg.String(), "groupID", groupID, "time cost", t)
	}(time.Now())
	var pushToUserIDs []string
	if err = c.webhookBeforeGroupOnlinePush(ctx, &c.config.WebhooksConfig.BeforeGroupOnlinePush, groupID, msg,
		&pushToUserIDs); err != nil {
		return err
	}

	err = c.groupMessagesHandler(ctx, groupID, &pushToUserIDs, msg)
	if err != nil {
		return err
	}

	wsResults, err := c.GetConnsAndOnlinePush(ctx, msg, pushToUserIDs)
	if err != nil {
		return err
	}

	log.ZDebug(ctx, "group push result", "result", wsResults, "msg", msg)
	log.ZInfo(ctx, "online group push end")

	if !c.shouldPushOffline(ctx, msg) {
		return nil
	}
	needOfflinePushUserIDs := c.onlinePusher.GetOnlinePushFailedUserIDs(ctx, msg, wsResults, &pushToUserIDs)
	//filter some user, like don not disturb or don't need offline push etc.
	needOfflinePushUserIDs, err = c.filterGroupMessageOfflinePush(ctx, groupID, msg, needOfflinePushUserIDs)
	if err != nil {
		return err
	}
	log.ZInfo(ctx, "filterGroupMessageOfflinePush end")

	// Use offline push messaging
	if len(needOfflinePushUserIDs) > 0 {
		c.asyncOfflinePush(ctx, needOfflinePushUserIDs, msg)
	}

	return nil
}

func (c *ConsumerHandler) asyncOfflinePush(ctx context.Context, needOfflinePushUserIDs []string, msg *sdkws.MsgData) {
	var offlinePushUserIDs []string
	err := c.webhookBeforeOfflinePush(ctx, &c.config.WebhooksConfig.BeforeOfflinePush, needOfflinePushUserIDs, msg, &offlinePushUserIDs)
	if err != nil {
		log.ZWarn(ctx, "webhookBeforeOfflinePush failed", err, "msg", msg)
		return
	}

	if len(offlinePushUserIDs) > 0 {
		needOfflinePushUserIDs = offlinePushUserIDs
	}
	if err := c.pushDatabase.MsgToOfflinePushMQ(ctx, conversationutil.GenConversationUniqueKeyForSingle(msg.SendID, msg.RecvID), needOfflinePushUserIDs, msg); err != nil {
		log.ZWarn(ctx, "Msg To OfflinePush MQ error", err, "needOfflinePushUserIDs length",
			len(needOfflinePushUserIDs), "msg", msg)
		prommetrics.GroupChatMsgProcessFailedCounter.Inc()
		return
	}
}

func (c *ConsumerHandler) groupMessagesHandler(ctx context.Context, groupID string, pushToUserIDs *[]string, msg *sdkws.MsgData) (err error) {
	if len(*pushToUserIDs) == 0 {
		*pushToUserIDs, err = c.groupLocalCache.GetGroupMemberIDs(ctx, groupID)
		if err != nil {
			return err
		}
		switch msg.ContentType {
		case constant.MemberQuitNotification:
			var tips sdkws.MemberQuitTips
			if unmarshalNotificationElem(msg.Content, &tips) != nil {
				return err
			}
			if err = c.DeleteMemberAndSetConversationSeq(ctx, groupID, []string{tips.QuitUser.UserID}); err != nil {
				log.ZError(ctx, "MemberQuitNotification DeleteMemberAndSetConversationSeq", err, "groupID", groupID, "userID", tips.QuitUser.UserID)
			}
			*pushToUserIDs = append(*pushToUserIDs, tips.QuitUser.UserID)
		case constant.MemberKickedNotification:
			var tips sdkws.MemberKickedTips
			if unmarshalNotificationElem(msg.Content, &tips) != nil {
				return err
			}
			kickedUsers := datautil.Slice(tips.KickedUserList, func(e *sdkws.GroupMemberFullInfo) string { return e.UserID })
			if err = c.DeleteMemberAndSetConversationSeq(ctx, groupID, kickedUsers); err != nil {
				log.ZError(ctx, "MemberKickedNotification DeleteMemberAndSetConversationSeq", err, "groupID", groupID, "userIDs", kickedUsers)
			}

			*pushToUserIDs = append(*pushToUserIDs, kickedUsers...)
		case constant.GroupDismissedNotification:
			if msgprocessor.IsNotification(msgprocessor.GetConversationIDByMsg(msg)) {
				var tips sdkws.GroupDismissedTips
				if unmarshalNotificationElem(msg.Content, &tips) != nil {
					return err
				}
				log.ZDebug(ctx, "GroupDismissedNotificationInfo****", "groupID", groupID, "num", len(*pushToUserIDs), "list", pushToUserIDs)
				if len(c.config.Share.IMAdminUserID) > 0 {
					ctx = mcontext.WithOpUserIDContext(ctx, c.config.Share.IMAdminUserID[0])
				}
				defer func(groupID string) {
					if err := c.groupClient.DismissGroup(ctx, groupID, true); err != nil {
						log.ZError(ctx, "DismissGroup Notification clear members", err, "groupID", groupID)
					}
				}(groupID)
			}
		}
	}
	return err
}

func (c *ConsumerHandler) offlinePushMsg(ctx context.Context, msg *sdkws.MsgData, offlinePushUserIDs []string) error {
	title, content, opts, err := offlinepush.GetOfflinePushInfos(msg)
	if err != nil {
		log.ZError(ctx, "getOfflinePushInfos failed", err, "msg", msg)
		return err
	}
	exLen := 0
	wakePush := false
	if opts != nil {
		exLen = len(opts.Ex)
		wakePush = opts.IsWakePush()
	}
	if isSignalingNotification(msg.ContentType) {
		log.ZInfo(ctx, "signaling offlinePushMsg calling pusher",
			"userIDs", offlinePushUserIDs,
			"title", title,
			"content", content,
			"clientMsgID", msg.ClientMsgID,
			"recvID", msg.RecvID,
			"sendID", msg.SendID,
			"exLen", exLen,
			"wakePush", wakePush,
		)
	} else {
		log.ZInfo(ctx, "offlinePushMsg calling pusher", "userIDs", offlinePushUserIDs, "title", title, "clientMsgID", msg.ClientMsgID)
	}
	err = c.offlinePusher.Push(ctx, offlinePushUserIDs, title, content, opts)
	if err != nil {
		if isSignalingNotification(msg.ContentType) {
			log.ZWarn(ctx, "signaling offlinePushMsg failed", err,
				"userIDs", offlinePushUserIDs,
				"clientMsgID", msg.ClientMsgID,
				"recvID", msg.RecvID,
				"sendID", msg.SendID,
			)
		}
		prommetrics.MsgOfflinePushFailedCounter.Inc()
		return err
	}
	if isSignalingNotification(msg.ContentType) {
		log.ZInfo(ctx, "signaling offlinePushMsg success",
			"userIDs", offlinePushUserIDs,
			"clientMsgID", msg.ClientMsgID,
			"recvID", msg.RecvID,
			"sendID", msg.SendID,
		)
	}
	return nil
}

type offlinePushLogSummary struct {
	title string
	exLen int
}

func offlinePushSummary(msg *sdkws.MsgData) offlinePushLogSummary {
	if msg == nil || msg.OfflinePushInfo == nil {
		return offlinePushLogSummary{}
	}
	return offlinePushLogSummary{
		title: msg.OfflinePushInfo.Title,
		exLen: len(msg.OfflinePushInfo.Ex),
	}
}

func (c *ConsumerHandler) filterGroupMessageOfflinePush(ctx context.Context, groupID string, msg *sdkws.MsgData,
	offlinePushUserIDs []string) (userIDs []string, err error) {
	needOfflinePushUserIDs, err := c.conversationClient.GetConversationOfflinePushUserIDs(ctx, conversationutil.GenGroupConversationID(groupID), offlinePushUserIDs)
	if err != nil {
		return nil, err
	}
	if len(needOfflinePushUserIDs) == 0 {
		return needOfflinePushUserIDs, nil
	}
	if c.groupMuteDB == nil {
		return filterOfflinePushByNotificationSwitch(ctx, c.userLocalCache, msg, needOfflinePushUserIDs)
	}
	muted, err := c.groupMuteDB.ListActiveMutedUserIDs(ctx, groupID, needOfflinePushUserIDs)
	if err != nil {
		return nil, err
	}
	if len(muted) == 0 {
		return filterOfflinePushByNotificationSwitch(ctx, c.userLocalCache, msg, needOfflinePushUserIDs)
	}
	mutedSet := make(map[string]struct{}, len(muted))
	for _, u := range muted {
		mutedSet[u] = struct{}{}
	}
	out := make([]string, 0, len(needOfflinePushUserIDs))
	for _, u := range needOfflinePushUserIDs {
		if _, ok := mutedSet[u]; !ok {
			out = append(out, u)
		}
	}
	return filterOfflinePushByNotificationSwitch(ctx, c.userLocalCache, msg, out)
}

func (c *ConsumerHandler) DeleteMemberAndSetConversationSeq(ctx context.Context, groupID string, userIDs []string) error {
	conversationID := msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, groupID)
	maxSeq, err := c.msgClient.GetConversationMaxSeq(ctx, conversationID)
	if err != nil {
		return err
	}
	return c.conversationClient.SetConversationMaxSeq(ctx, conversationID, userIDs, maxSeq)
}

func unmarshalNotificationElem(bytes []byte, t any) error {
	var notification sdkws.NotificationElem
	if err := json.Unmarshal(bytes, &notification); err != nil {
		return err
	}
	return json.Unmarshal([]byte(notification.Detail), t)
}
