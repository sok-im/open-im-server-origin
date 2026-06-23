package convert

import (
	"context"
	"testing"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/sdkws"
)

func TestFriendsDB2Pb_missingUser(t *testing.T) {
	ctx := context.Background()
	friendsDB := []*model.Friend{{
		OwnerUserID:  "owner",
		FriendUserID: "missing-user",
		Remark:       "备注",
		CreateTime:   time.Unix(1, 0),
	}}
	getUsers := func(ctx context.Context, userIDs []string) (map[string]*sdkws.UserInfo, error) {
		return map[string]*sdkws.UserInfo{}, nil
	}
	defaults := config.DeactivatedUserDefaults{
		Nickname: "Deactivated user",
		FaceURL:  "http://13.215.203.29:10002/object/6794065114/mmexport1782219627453.jpg",
	}

	got, err := FriendsDB2Pb(ctx, friendsDB, getUsers, defaults)
	if err != nil {
		t.Fatalf("FriendsDB2Pb: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1 for missing user", len(got))
	}
	if got[0].FriendUser.Nickname != defaults.Nickname {
		t.Fatalf("nickname: got %q, want %q", got[0].FriendUser.Nickname, defaults.Nickname)
	}
	if got[0].FriendUser.FaceURL != defaults.FaceURL {
		t.Fatalf("faceURL: got %q, want %q", got[0].FriendUser.FaceURL, defaults.FaceURL)
	}
	if got[0].FriendUser.FirstName != defaults.Nickname {
		t.Fatalf("firstName: got %q, want %q", got[0].FriendUser.FirstName, defaults.Nickname)
	}
}

func TestFriendOnlyDB2PbOnly_missingUser(t *testing.T) {
	friendsDB := []*model.Friend{{
		OwnerUserID:  "owner",
		FriendUserID: "missing-user",
	}}
	defaults := config.DeactivatedUserDefaults{
		Nickname: "Deactivated user",
		FaceURL:  "http://example.com/avatar.jpg",
	}

	got := FriendOnlyDB2PbOnly(friendsDB, map[string]*sdkws.UserInfo{}, defaults)
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	if got[0].FirstName != defaults.Nickname {
		t.Fatalf("firstName: got %q, want %q", got[0].FirstName, defaults.Nickname)
	}
}
