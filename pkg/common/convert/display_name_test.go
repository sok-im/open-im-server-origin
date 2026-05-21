package convert

import (
	"testing"

	"github.com/openimsdk/protocol/sdkws"
)

func TestDisplayNickname(t *testing.T) {
	u := &sdkws.UserInfo{Nickname: "nick", FirstName: "A", LastName: "B"}
	if got := DisplayNickname("备注", u); got != "备注" {
		t.Fatalf("remark: got %q", got)
	}
	if got := DisplayNickname("", u); got != "A B" {
		t.Fatalf("full name: got %q", got)
	}
	u2 := &sdkws.UserInfo{Nickname: "only"}
	if got := DisplayNickname("", u2); got != "only" {
		t.Fatalf("nickname: got %q", got)
	}
}
