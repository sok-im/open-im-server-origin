package msg

import (
	"testing"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
)

func TestChatbotHit(t *testing.T) {
	base := config.Chatbot{Enable: true, UserID: "bot"}
	cases := []struct {
		name string
		cfg  config.Chatbot
		msg  *sdkws.MsgData
		want bool
	}{
		{"single hit", base, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "u", RecvID: "bot", ContentType: constant.Text}, true},
		{"single miss other recv", base, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "u", RecvID: "other", ContentType: constant.Text}, false},
		{"loop bot sender", base, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "bot", RecvID: "u", ContentType: constant.Text}, false},
		{"disabled", config.Chatbot{Enable: false, UserID: "bot"}, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "u", RecvID: "bot", ContentType: constant.Text}, false},
		{"empty userID", config.Chatbot{Enable: true, UserID: ""}, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "u", RecvID: "", ContentType: constant.Text}, false},
		{"typing ignored", base, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "u", RecvID: "bot", ContentType: constant.Typing}, false},
		{"denied type", config.Chatbot{Enable: true, UserID: "bot", DeniedTypes: []int32{1201}}, &sdkws.MsgData{SessionType: constant.SingleChatType, SendID: "u", RecvID: "bot", ContentType: 1201}, false},
		{"group at hit", base, &sdkws.MsgData{SessionType: constant.ReadGroupChatType, SendID: "u", GroupID: "g", AtUserIDList: []string{"bot"}, ContentType: constant.AtText}, true},
		{"group no at", base, &sdkws.MsgData{SessionType: constant.ReadGroupChatType, SendID: "u", GroupID: "g", AtUserIDList: []string{"other"}, ContentType: constant.AtText}, false},
		{"group loop bot sender", base, &sdkws.MsgData{SessionType: constant.ReadGroupChatType, SendID: "bot", GroupID: "g", AtUserIDList: []string{"bot"}, ContentType: constant.AtText}, false},
		{"unknown session", base, &sdkws.MsgData{SessionType: constant.NotificationChatType, SendID: "u", RecvID: "bot", ContentType: constant.Text}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chatbotHit(c.cfg, c.msg); got != c.want {
				t.Fatalf("chatbotHit() = %v, want %v", got, c.want)
			}
		})
	}
}
