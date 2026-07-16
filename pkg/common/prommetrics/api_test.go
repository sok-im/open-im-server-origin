package prommetrics

import "testing"

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
		{path: "", want: "unknown"},
	}
	for _, tt := range tests {
		if got := APIModuleFromPath(tt.path); got != tt.want {
			t.Fatalf("APIModuleFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
