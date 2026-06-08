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

package user

import (
	"context"
	"sort"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/protocol/constant"
	pbuser "github.com/openimsdk/protocol/user"
	"github.com/openimsdk/tools/errs"
)

const onlineUserAreaCodeBatchSize = 500

func (s *userServer) UserRegisterCount(ctx context.Context, req *pbuser.UserRegisterCountReq) (*pbuser.UserRegisterCountResp, error) {
	if req.Start > req.End {
		return nil, errs.ErrArgs.WrapMsg("start > end")
	}
	total, err := s.db.CountTotal(ctx, nil)
	if err != nil {
		return nil, err
	}
	start := time.UnixMilli(req.Start)
	before, err := s.db.CountTotal(ctx, &start)
	if err != nil {
		return nil, err
	}
	count, err := s.db.CountRangeEverydayTotal(ctx, start, time.UnixMilli(req.End))
	if err != nil {
		return nil, err
	}
	return &pbuser.UserRegisterCountResp{Total: total, Before: before, Count: count}, nil
}

func (s *userServer) GetOnlineUserCount(ctx context.Context, req *pbuser.GetOnlineUserCountReq) (*pbuser.GetOnlineUserCountResp, error) {
	if err := authverify.CheckAdmin(ctx, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	filterArea := req.GetAreaCode()
	areaCounts := make(map[string]int64)
	var total int64

	cursor := uint64(0)
	for {
		onlineResp, err := s.GetAllOnlineUsers(ctx, &pbuser.GetAllOnlineUsersReq{Cursor: cursor})
		if err != nil {
			return nil, err
		}

		onlineUserIDs := make([]string, 0, len(onlineResp.StatusList))
		for _, status := range onlineResp.StatusList {
			if status.Status == constant.Online {
				onlineUserIDs = append(onlineUserIDs, status.UserID)
			}
		}

		for i := 0; i < len(onlineUserIDs); i += onlineUserAreaCodeBatchSize {
			end := i + onlineUserAreaCodeBatchSize
			if end > len(onlineUserIDs) {
				end = len(onlineUserIDs)
			}
			batch := onlineUserIDs[i:end]
			users, err := s.db.Find(ctx, batch)
			if err != nil {
				return nil, err
			}
			userAreaMap := make(map[string]string, len(users))
			for _, u := range users {
				userAreaMap[u.UserID] = u.AreaCode
			}
			for _, userID := range batch {
				area := userAreaMap[userID]
				if filterArea != "" {
					if area == filterArea {
						total++
					}
					continue
				}
				areaCounts[area]++
				total++
			}
		}

		if onlineResp.NextCursor == 0 {
			break
		}
		cursor = onlineResp.NextCursor
	}

	if filterArea != "" {
		return &pbuser.GetOnlineUserCountResp{
			OnlineUserCount: total,
			//AreaCode:        filterArea,
		}, nil
	}

	areaCountList := make([]*pbuser.OnlineUserAreaCount, 0, len(areaCounts))
	for area, count := range areaCounts {
		areaCountList = append(areaCountList, &pbuser.OnlineUserAreaCount{
			AreaCode:        area,
			OnlineUserCount: count,
		})
	}
	sort.Slice(areaCountList, func(i, j int) bool {
		return areaCountList[i].AreaCode < areaCountList[j].AreaCode
	})

	return &pbuser.GetOnlineUserCountResp{
		OnlineUserCount: total,
		//AreaCounts:      areaCountList,
	}, nil
}
