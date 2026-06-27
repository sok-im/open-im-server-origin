package tools

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
)

type chatAdminLoginResp struct {
	AdminToken string `json:"adminToken"`
}

// fetchChatAdminToken 通过 chat-admin-api POST /account/login 获取 chat adminToken。
// chat /account/del 使用 chat 自己的 token 体系（与 IM auth token 不互通）。
func (c *cronServer) fetchChatAdminToken(ctx context.Context) (string, error) {
	cfg := c.config.CronTask.ChatAPI
	password := cfg.AdminPassword
	if password == "" {
		sum := md5.Sum([]byte(cfg.AdminAccount))
		password = hex.EncodeToString(sum[:])
	}

	body, _ := json.Marshal(map[string]string{
		"account":  cfg.AdminAccount,
		"password": password,
	})
	url := cfg.AdminAddress + "/account/login"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("operationID", mcontext.GetOperationID(ctx))

	resp, err := chatHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("chat admin login HTTP failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("chat admin login status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		apiresp.ApiResponse
		Data chatAdminLoginResp `json:"data"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("decode chat admin login response: %w", err)
	}
	if apiResp.ErrCode != 0 {
		return "", fmt.Errorf("chat admin login errCode=%d errMsg=%s", apiResp.ErrCode, apiResp.ErrMsg)
	}
	if apiResp.Data.AdminToken == "" {
		return "", fmt.Errorf("chat admin login: empty adminToken")
	}

	log.ZDebug(ctx, "fetchChatAdminToken: ok", "account", cfg.AdminAccount)
	return apiResp.Data.AdminToken, nil
}
