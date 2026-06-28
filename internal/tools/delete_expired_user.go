package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
)

const deleteExpiredUserBatchLimit = 100

// chatHTTPClient 带超时，防止 chat 服务无响应时 cron worker 永久挂起。
var chatHTTPClient = &http.Client{Timeout: 3 * time.Second}

// deleteExpiredOfflineUsers 是 cron "@every 1m" 触发的入口。
// 批量查询离线时长超过 delete_account_interval 的用户并依次调用 chat /account/del 删除。
func (c *cronServer) deleteExpiredOfflineUsers() {
	now := time.Now()
	operationID := fmt.Sprintf("cron_del_expired_user_%d_%d", os.Getpid(), now.UnixMilli())
	ctx := mcontext.SetOperationID(c.ctx, operationID)
	log.ZInfo(ctx, "deleteExpiredOfflineUsers: start", "time", now)

	users, err := c.userOfflineRecordDB.FindExpiredUsers(ctx, now, deleteExpiredUserBatchLimit)
	if err != nil {
		log.ZError(ctx, "deleteExpiredOfflineUsers: FindExpiredUsers failed", err)
		return
	}
	if len(users) == 0 {
		log.ZDebug(ctx, "deleteExpiredOfflineUsers: no expired users found")
		return
	}
	log.ZInfo(ctx, "deleteExpiredOfflineUsers: found expired users", "count", len(users))

	adminToken, err := c.fetchChatAdminToken(ctx)
	if err != nil {
		log.ZError(ctx, "deleteExpiredOfflineUsers: fetchChatAdminToken failed", err)
		return
	}

	for i, u := range users {
		subCtx := mcontext.SetOperationID(c.ctx, fmt.Sprintf("%s_%d", operationID, i))
		if c.deleteExpiredUser(subCtx, adminToken, u.UserID) {
			log.ZInfo(subCtx, "tom deleteExpiredUser: success", "userID", u.UserID)
		} else {
			log.ZError(subCtx, "tom deleteExpiredUser: failed", nil, "userID", u.UserID)
		}
	}

	log.ZInfo(ctx, "deleteExpiredOfflineUsers: done", "count", len(users), "elapsed", time.Since(now))
}

// deleteExpiredUser 通过 chat HTTP API POST /account/del 删除单个过期用户。
// chat 服务端会处理：强制登出、删除好友/群组关系、清理 chat 账号数据等。
// adminToken 为当次批次开始时通过 chat-admin-api /account/login 获取的 chat adminToken。
// 返回 true 表示 chat 业务成功且已清理 user_offline_record。
func (c *cronServer) deleteExpiredUser(ctx context.Context, adminToken, userID string) bool {
	log.ZInfo(ctx, "tom deleteExpiredUser: start", "userID", userID)

	operationID := mcontext.GetOperationID(ctx)

	body, _ := json.Marshal(map[string]any{"userIDs": []string{userID}})
	url := c.chatAPIAddress + "/account/del"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.ZError(ctx, "tom deleteExpiredUser: build request failed", err, "userID", userID)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("token", adminToken)
	req.Header.Set("operationID", operationID)

	resp, err := chatHTTPClient.Do(req)
	if err != nil {
		log.ZError(ctx, "tom deleteExpiredUser: HTTP call failed", err, "userID", userID, "url", url)
		return false
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.ZError(ctx, "tom deleteExpiredUser: read response body failed", err, "userID", userID)
		return false
	}

	if resp.StatusCode != http.StatusOK {
		log.ZError(ctx, "tom deleteExpiredUser: chat API returned error",
			fmt.Errorf("status %d", resp.StatusCode),
			"userID", userID, "response", string(respBody))
		return false
	}

	var apiResp apiresp.ApiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		log.ZError(ctx, "tom deleteExpiredUser: decode chat response failed", err,
			"userID", userID, "response", string(respBody))
		return false
	}
	if apiResp.ErrCode != 0 {
		log.ZError(ctx, "tom deleteExpiredUser: chat API business error",
			fmt.Errorf("errCode=%d errMsg=%s", apiResp.ErrCode, apiResp.ErrMsg),
			"userID", userID, "errDlt", apiResp.ErrDlt, "response", string(respBody))
		return false
	}

	// chat /account/del 已处理好友/群组/IM用户删除；仅清理 user_offline_record 防止重复触发
	if err := c.userOfflineRecordDB.Delete(ctx, userID); err != nil {
		log.ZWarn(ctx, "tom deleteExpiredUser: Delete offline record failed", err, "userID", userID)
		return false
	}

	log.ZInfo(ctx, "tom deleteExpiredUser: done", "userID", userID)
	return true
}
