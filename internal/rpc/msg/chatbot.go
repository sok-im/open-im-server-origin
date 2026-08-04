package msg

import (
	"context"

	cbapi "github.com/openimsdk/open-im-server/v3/pkg/callbackstruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	pbchat "github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
)

// chatbotHit 判定一条消息是否应回调给客服机器人。
// 单聊：recvID 为机器人；群聊：AtUserIDList 含机器人。
// 机器人自己发出的消息（防回环）、typing、deniedTypes 一律不命中。
func chatbotHit(cfg config.Chatbot, msg *sdkws.MsgData) bool {
	if !cfg.Enable || cfg.UserID == "" {
		return false
	}
	if msg.SendID == cfg.UserID {
		return false
	}
	if msg.ContentType == constant.Typing {
		return false
	}
	if datautil.Contain(msg.ContentType, cfg.DeniedTypes...) {
		return false
	}
	switch msg.SessionType {
	case constant.SingleChatType:
		return msg.RecvID == cfg.UserID
	case constant.ReadGroupChatType:
		return datautil.Contain(cfg.UserID, msg.AtUserIDList...)
	default:
		return false
	}
}

func (m *msgServer) webhookAfterSendMsgToChatBot(ctx context.Context, req *pbchat.SendMsgReq) {
	cfg := m.config.Share.Chatbot
	if !chatbotHit(cfg, req.MsgData) {
		return
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5
	}
	cbReq := &cbapi.CallbackAfterSendMsgToBotReq{
		CommonCallbackReq: toCommonCallback(ctx, req, cbapi.CallbackAfterSendMsgToBotCommand),
	}
	switch req.MsgData.SessionType {
	case constant.SingleChatType:
		cbReq.RecvID = req.MsgData.RecvID
	case constant.ReadGroupChatType:
		cbReq.GroupID = req.MsgData.GroupID
	}
	log.ZDebug(ctx, "webhookAfterSendMsgToChatBot", "sendID", req.MsgData.SendID,
		"recvID", cbReq.RecvID, "groupID", cbReq.GroupID, "clientMsgID", req.MsgData.ClientMsgID, "seq", req.MsgData.Seq)
	m.chatbotWebhook.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq,
		&cbapi.CallbackAfterSendMsgToBotResp{}, &config.AfterConfig{Enable: true, Timeout: timeout})
}
