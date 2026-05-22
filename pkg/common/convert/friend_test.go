package convert

import (
	"context"
	"testing"
	"time"

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

	got, err := FriendsDB2Pb(ctx, friendsDB, getUsers)
	if err != nil {
		t.Fatalf("FriendsDB2Pb: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len: got %d, want 0 for missing user", len(got))
	}
}
