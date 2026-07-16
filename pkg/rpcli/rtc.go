package rpcli

import (
	"context"

	"github.com/openimsdk/protocol/rtc"
	"google.golang.org/grpc"
)

func NewRtcServiceClient(cc grpc.ClientConnInterface) *RtcServiceClient {
	return &RtcServiceClient{rtc.NewRtcServiceClient(cc)}
}

type RtcServiceClient struct {
	rtc.RtcServiceClient
}

func (x *RtcServiceClient) SignalRemoveParticipants(ctx context.Context, groupID string, userIDs []string) error {
	if groupID == "" || len(userIDs) == 0 {
		return nil
	}
	_, err := x.RtcServiceClient.SignalRemoveParticipants(ctx, &rtc.SignalRemoveParticipantsReq{
		GroupID: groupID,
		UserIDs: userIDs,
	})
	return err
}
