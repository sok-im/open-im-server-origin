// Copyright © 2024 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/webhook"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/rtc"
	"github.com/openimsdk/tools/a2r"
	"github.com/openimsdk/tools/log"
)

type RtcApi struct {
	Client  rtc.RtcServiceClient
	liveKit config.LiveKit
}

func NewRtcApi(client rtc.RtcServiceClient, liveKit config.LiveKit) RtcApi {
	return RtcApi{Client: client, liveKit: liveKit}
}

func (o *RtcApi) SignalMessageAssemble(c *gin.Context) {
	a2r.Call(c, rtc.RtcServiceClient.SignalMessageAssemble, o.Client)
}

func (o *RtcApi) SignalGetRoomByGroupID(c *gin.Context) {
	a2r.Call(c, rtc.RtcServiceClient.SignalGetRoomByGroupID, o.Client)
}

func (o *RtcApi) SignalGetTokenByRoomID(c *gin.Context) {
	a2r.Call(c, rtc.RtcServiceClient.SignalGetTokenByRoomID, o.Client)
}

func (o *RtcApi) SignalGetRooms(c *gin.Context) {
	a2r.Call(c, rtc.RtcServiceClient.SignalGetRooms, o.Client)
}

func (o *RtcApi) GetSignalInvitationInfo(c *gin.Context) {
	a2r.Call(c, rtc.RtcServiceClient.GetSignalInvitationInfo, o.Client)
}

func (o *RtcApi) GetSignalInvitationInfoStartApp(c *gin.Context) {
	a2r.Call(c, rtc.RtcServiceClient.GetSignalInvitationInfoStartApp, o.Client)
}

func (o *RtcApi) SignalSendCustomSignal(c *gin.Context) {
	a2r.Call(c, rtc.RtcServiceClient.SignalSendCustomSignal, o.Client)
}

func (o *RtcApi) SignalNotifyGroupCallEnded(c *gin.Context) {
	a2r.Call(c, rtc.RtcServiceClient.SignalNotifyGroupCallEnded, o.Client)
}

func (o *RtcApi) GetSignalInvitationRecords(c *gin.Context) {
	a2r.Call(c, rtc.RtcServiceClient.GetSignalInvitationRecords, o.Client)
}

func (o *RtcApi) DeleteSignalRecords(c *gin.Context) {
	a2r.Call(c, rtc.RtcServiceClient.DeleteSignalRecords, o.Client)
}

// LiveKitWebhook receives LiveKit server webhook events (room_finished,
// participant_left, etc.) and forwards room_id/event to the rtc RPC's
// NotifyRoomEvent so the call watchdog can re-check that room immediately
// instead of waiting for its next periodic scan tick. This is a raw HTTP
// endpoint (not a2r.Call) because LiveKit signs the request body itself
// rather than sending a proto request our standard request wrapper expects.
// Enable it by pointing the LiveKit server's webhook_url at this route AND
// setting rtc.watchdog.webhookEnabled: true (config/openim-rpc-rtc.yml).
func (o *RtcApi) LiveKitWebhook(c *gin.Context) {
	event, err := webhook.ReceiveWebhookEvent(c.Request, auth.NewSimpleKeyProvider(o.liveKit.APIKey, o.liveKit.APISecret))
	if err != nil {
		log.ZWarn(c.Request.Context(), "LiveKitWebhook: verify/parse failed", err)
		c.Status(http.StatusUnauthorized)
		return
	}
	roomID := ""
	if event.GetRoom() != nil {
		roomID = event.GetRoom().GetName()
	}
	if roomID != "" {
		if _, err := o.Client.NotifyRoomEvent(c.Request.Context(), &rtc.NotifyRoomEventReq{
			RoomID:    roomID,
			EventType: event.GetEvent(),
		}); err != nil {
			log.ZWarn(c.Request.Context(), "LiveKitWebhook: NotifyRoomEvent failed", err, "roomID", roomID, "event", event.GetEvent())
		}
	}
	c.Status(http.StatusOK)
}
