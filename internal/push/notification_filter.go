package push

import (
	"context"

	tablerelation "github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/rpccache"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"
)

func isSignalingNotification(contentType int32) bool {
	return contentType >= constant.SignalingNotificationBegin && contentType <= constant.SignalingNotificationEnd
}

func userNotificationEnabled(user *sdkws.UserInfo, contentType int32) bool {
	if user == nil {
		return true
	}
	var switchVal int32
	switch contentType {
	case constant.PaymentNotification:
		switchVal = user.SokimPaymentNotification
	case constant.ServiceNotification:
		switchVal = user.SokimServiceNotification
	default:
		if contentType >= constant.SignalingNotificationBegin && contentType <= constant.SignalingNotificationEnd {
			switchVal = user.AvNotification
		} else {
			switchVal = user.MsgNotification
		}
	}
	return tablerelation.NotificationSwitchToBool(switchVal)
}

func filterOfflinePushByNotificationSwitch(ctx context.Context, userLocalCache *rpccache.UserLocalCache, msg *sdkws.MsgData, userIDs []string) ([]string, error) {
	if len(userIDs) == 0 || userLocalCache == nil {
		return userIDs, nil
	}
	userMap, err := userLocalCache.GetUsersInfoMap(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		if userNotificationEnabled(userMap[userID], msg.ContentType) {
			out = append(out, userID)
			continue
		}
		if isSignalingNotification(msg.ContentType) {
			avSwitch := int32(0)
			if user := userMap[userID]; user != nil {
				avSwitch = user.AvNotification
			}
			log.ZInfo(ctx, "lintao signaling offline push skipped: AvNotification switch off",
				"userID", userID,
				"contentType", msg.ContentType,
				"clientMsgID", msg.ClientMsgID,
				"recvID", msg.RecvID,
				"sendID", msg.SendID,
				"avNotification", avSwitch,
			)
			continue
		}
		log.ZDebug(ctx, "lintao offline push skipped: user notification switch off",
			"userID", userID, "contentType", msg.ContentType, "clientMsgID", msg.ClientMsgID)
	}
	return out, nil
}
