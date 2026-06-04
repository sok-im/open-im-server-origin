package convert

import (
	"context"
	"testing"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/sdkws"
)

func TestBlackDB2Pb_missingUser(t *testing.T) {
	ctx := context.Background()
	blackDBs := []*model.Black{{
		OwnerUserID:  "owner",
		BlockUserID:  "missing-user",
		CreateTime:   time.Unix(1, 0),
		AddSource:    1,
		OperatorUserID: "owner",
	}}
	getUsers := func(ctx context.Context, userIDs []string) (map[string]*sdkws.UserInfo, error) {
		return map[string]*sdkws.UserInfo{}, nil
	}

	got, err := BlackDB2Pb(ctx, blackDBs, getUsers)
	if err != nil {
		t.Fatalf("BlackDB2Pb: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	if got[0].BlackUserInfo != nil {
		t.Fatal("BlackUserInfo: want nil for missing user")
	}
}
