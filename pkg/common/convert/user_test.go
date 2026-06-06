// Copyright © 2023 OpenIM. All rights reserved.
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

package convert

import (
	relationtb "github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"reflect"
	"testing"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/protocol/wrapperspb"
)

func TestUsersDB2Pb(t *testing.T) {
	type args struct {
		users []*relationtb.User
	}
	tests := []struct {
		name       string
		args       args
		wantResult []*sdkws.UserInfo
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotResult := UsersDB2Pb(tt.args.users); !reflect.DeepEqual(gotResult, tt.wantResult) {
				t.Errorf("UsersDB2Pb() = %v, want %v", gotResult, tt.wantResult)
			}
		})
	}
}

func TestUserPb2DB(t *testing.T) {
	type args struct {
		user *sdkws.UserInfo
	}
	tests := []struct {
		name string
		args args
		want *relationtb.User
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UserPb2DB(tt.args.user); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UserPb2DB() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUserPb2DBMap(t *testing.T) {
	user := &sdkws.UserInfo{
		Nickname:         "TestUser",
		FaceURL:          "http://openim.io/logo.jpg",
		Ex:               "Extra Data",
		AppMangerLevel:   1,
		GlobalRecvMsgOpt: 2,
	}

	expected := map[string]any{
		"nickname":            "TestUser",
		"face_url":            "http://openim.io/logo.jpg",
		"ex":                  "Extra Data",
		"app_manager_level":   int32(1),
		"global_recv_msg_opt": int32(2),
	}

	result := UserPb2DBMap(user)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("UserPb2DBMap returned unexpected map. Got %v, want %v", result, expected)
	}
}

func TestUserPb2DBMapEx_CallRingtoneDefaults(t *testing.T) {
	defaults := &config.CallRingtoneDefaults{
		URL:    "https://example.com/default.mp3",
		Name:   "Default Ring",
		Cover:  "https://example.com/cover.jpg",
		Author: "Default Author",
	}
	user := &sdkws.UserInfoWithEx{
		CallRingtoneURL:    &wrapperspb.StringValue{Value: ""},
		CallRingtoneName:   &wrapperspb.StringValue{Value: ""},
		CallRingtoneCover:  &wrapperspb.StringValue{Value: ""},
		CallRingtoneAuthor: &wrapperspb.StringValue{Value: ""},
	}

	got := UserPb2DBMapEx(user, defaults)
	expected := map[string]any{
		"call_ringtone_url":    defaults.URL,
		"call_ringtone_name":   defaults.Name,
		"call_ringtone_cover":  defaults.Cover,
		"call_ringtone_author": defaults.Author,
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("UserPb2DBMapEx() = %v, want %v", got, expected)
	}

	customURL := "https://example.com/custom.mp3"
	userWithCustom := &sdkws.UserInfoWithEx{
		CallRingtoneURL: &wrapperspb.StringValue{Value: customURL},
	}
	gotCustom := UserPb2DBMapEx(userWithCustom, defaults)
	if gotCustom["call_ringtone_url"] != customURL {
		t.Errorf("UserPb2DBMapEx() custom url = %v, want %v", gotCustom["call_ringtone_url"], customURL)
	}
}
