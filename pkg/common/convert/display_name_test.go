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

func TestDisplayNicknameForFriend(t *testing.T) {
	u := &sdkws.UserInfo{Nickname: "nick", FirstName: "A", LastName: "B"}
	if got := DisplayNicknameForFriend("备注", "C", "D", u); got != "备注" {
		t.Fatalf("remark: got %q", got)
	}
	if got := DisplayNicknameForFriend("", "C", "D", u); got != "C D" {
		t.Fatalf("friend alias: got %q", got)
	}
	if got := DisplayNicknameForFriend("", "", "", u); got != "A B" {
		t.Fatalf("profile name: got %q", got)
	}
}
