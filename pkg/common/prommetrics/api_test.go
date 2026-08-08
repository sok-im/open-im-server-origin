package prommetrics

import (
	"testing"
	"time"
)

func TestAPIModuleFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/user/user_register", want: "user"},
		{path: "/friend/add_friend", want: "friend"},
		{path: "/virgil/v1/cards", want: "virgil/v1"},
		{path: "/openmls/v1/key_packages", want: "openmls/v1"},
		{path: "/crypto/v1/upload", want: "crypto/v1"},
		{path: "<404>", want: "unknown"},
		{path: "<unmatched>", want: "unknown"},
		{path: "", want: "unknown"},
	}
	for _, tt := range tests {
		if got := APIModuleFromPath(tt.path); got != tt.want {
			t.Fatalf("APIModuleFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestNormalizeMetricPath(t *testing.T) {
	if got := NormalizeMetricPath("/user/get_users_info", "/user/get_users_info", 200); got != "/user/get_users_info" {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeMetricPath("", "/user/u1234567890/profile", 200); got != "<unmatched>" {
		t.Fatalf("raw path must not be used as metric label, got %q", got)
	}
	if got := NormalizeMetricPath("", "/missing", 404); got != "<404>" {
		t.Fatalf("got %q", got)
	}
}

func TestCronTaskObserve(t *testing.T) {
	CronTaskObserve("delete_msg", true, time.Second)
	CronTaskObserve("delete_msg", false, 2*time.Second)
}
