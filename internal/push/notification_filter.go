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

// isTargetedGroupMemberPush reports group-session messages aimed at one member (recvID != groupID),
// such as a group call invite signaling sent only to the callee.
func isTargetedGroupMemberPush(msg *sdkws.MsgData) bool {
	if msg == nil || msg.SessionType != constant.ReadGroupChatType {
		return false
	}
	if msg.GroupID == "" || msg.RecvID == "" || msg.RecvID == msg.GroupID {
		return false
	}
	return isSignalingNotification(msg.ContentType)
}

func userNotificationEnabled(user *sdkws.UserInfo, contentType int32) bool {
	if user == nil {
		return true
	}
	var switchVal int32
	switch contentType {
	case constant.PaymentNotification,
		constant.RedPacketClaimNotification,
		constant.TransferReceiveNotification,
		constant.RedPacketExpiredNotification,
		constant.TransferExpiredNotification:
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
			log.ZInfo(ctx, "signaling offline push skipped: AvNotification switch off",
				"userID", userID,
				"contentType", msg.ContentType,
				"clientMsgID", msg.ClientMsgID,
				"recvID", msg.RecvID,
				"sendID", msg.SendID,
				"avNotification", avSwitch,
			)
			continue
		}
		log.ZDebug(ctx, "offline push skipped: user notification switch off",
			"userID", userID, "contentType", msg.ContentType, "clientMsgID", msg.ClientMsgID)
	}
	return out, nil
}
