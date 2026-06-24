package push

import (
	"context"

	"github.com/IBM/sarama"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/kafka"
	"github.com/openimsdk/open-im-server/v3/pkg/rpccache"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/protocol/constant"
	pbpush "github.com/openimsdk/protocol/push"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

type OfflinePushConsumerHandler struct {
	OfflinePushConsumerGroup *kafka.MConsumerGroup
	offlinePusher            offlinepush.OfflinePusher
	userLocalCache           *rpccache.UserLocalCache
}

func NewOfflinePushConsumerHandler(ctx context.Context, config *Config, offlinePusher offlinepush.OfflinePusher, client discovery.SvcDiscoveryRegistry, rdb redis.UniversalClient) (*OfflinePushConsumerHandler, error) {
	var offlinePushConsumerHandler OfflinePushConsumerHandler
	var err error
	offlinePushConsumerHandler.offlinePusher = offlinePusher
	offlinePushConsumerHandler.OfflinePushConsumerGroup, err = kafka.NewMConsumerGroup(config.KafkaConfig.Build(), config.KafkaConfig.ToOfflineGroupID,
		[]string{config.KafkaConfig.ToOfflinePushTopic}, true)
	if err != nil {
		return nil, err
	}
	userConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.User)
	if err != nil {
		return nil, err
	}
	offlinePushConsumerHandler.userLocalCache = rpccache.NewUserLocalCache(rpcli.NewUserClient(userConn), &config.LocalCacheConfig, rdb)
	return &offlinePushConsumerHandler, nil
}

func (*OfflinePushConsumerHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (*OfflinePushConsumerHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }
func (o *OfflinePushConsumerHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		ctx := o.OfflinePushConsumerGroup.GetContextFromMsg(msg)
		o.handleMsg2OfflinePush(ctx, msg.Value)
		sess.MarkMessage(msg, "")
	}
	return nil
}

func (o *OfflinePushConsumerHandler) handleMsg2OfflinePush(ctx context.Context, msg []byte) {
	offlinePushMsg := pbpush.PushMsgReq{}
	if err := proto.Unmarshal(msg, &offlinePushMsg); err != nil {
		log.ZError(ctx, "offline push Unmarshal msg err", err, "msg", string(msg))
		return
	}
	if offlinePushMsg.MsgData == nil || offlinePushMsg.UserIDs == nil {
		log.ZError(ctx, "offline push msg is empty", errs.New("offlinePushMsg is empty"), "userIDs", offlinePushMsg.UserIDs, "msg", offlinePushMsg.MsgData)
		return
	}
	if offlinePushMsg.MsgData.Status == constant.MsgStatusSending {
		offlinePushMsg.MsgData.Status = constant.MsgStatusSendSuccess
	}
	log.ZInfo(ctx, "receive to OfflinePush MQ", "userIDs", offlinePushMsg.UserIDs, "msg", offlinePushMsg.MsgData)

	userIDs, err := filterOfflinePushByNotificationSwitch(ctx, o.userLocalCache, offlinePushMsg.MsgData, offlinePushMsg.UserIDs)
	if err != nil {
		log.ZWarn(ctx, "filter offline push by notification switch failed", err, "msg", offlinePushMsg.String())
		return
	}
	if len(userIDs) == 0 {
		log.ZDebug(ctx, "offline push skipped: all users disabled notification switch", "clientMsgID", offlinePushMsg.MsgData.ClientMsgID, "contentType", offlinePushMsg.MsgData.ContentType)
		return
	}

	err = o.offlinePushMsg(ctx, offlinePushMsg.MsgData, userIDs)
	if err != nil {
		log.ZWarn(ctx, "offline push failed", err, "msg", offlinePushMsg.String())
	}
}

func (o *OfflinePushConsumerHandler) offlinePushMsg(ctx context.Context, msg *sdkws.MsgData, offlinePushUserIDs []string) error {
	title, content, opts, err := offlinepush.GetOfflinePushInfos(msg)
	if err != nil {
		return err
	}
	log.ZInfo(ctx, "lintao offlinePushMsg calling pusher", "userIDs", offlinePushUserIDs, "title", title, "clientMsgID", msg.ClientMsgID)
	err = o.offlinePusher.Push(ctx, offlinePushUserIDs, title, content, opts)
	if err != nil {
		prommetrics.MsgOfflinePushFailedCounter.Inc()
		return err
	}
	return nil
}
