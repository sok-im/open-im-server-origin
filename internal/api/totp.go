package api

import (
	"github.com/gin-gonic/gin"
	pbtotp "github.com/openimsdk/protocol/totp"
	"github.com/openimsdk/tools/a2r"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
)

type TotpApi struct {
	Client pbtotp.TotpClient
}

func NewTotpApi(client pbtotp.TotpClient) *TotpApi {
	return &TotpApi{Client: client}
}

// GetSecret generates a temporary TOTP secret and otpauth URI for the caller.
// The caller must be authenticated; userID is taken from the request token.
func (t *TotpApi) GetSecret(c *gin.Context) {
	req, err := a2r.ParseRequestNotCheck[pbtotp.GetSecretReq](c)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	req.UserID = mcontext.GetOpUserID(c)
	resp, err := t.Client.GetSecret(c, req)
	if err != nil {
		log.ZError(c, "totp get_secret rpc failed", err, "userID", req.UserID)
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

// BindTotp verifies the first dynamic code and activates TOTP for the caller.
func (t *TotpApi) BindTotp(c *gin.Context) {
	req, err := a2r.ParseRequestNotCheck[pbtotp.BindTotpReq](c)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	req.UserID = mcontext.GetOpUserID(c)
	resp, err := t.Client.BindTotp(c, req)
	if err != nil {
		log.ZError(c, "totp bind rpc failed", err, "userID", req.UserID)
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

// VerifyTotp is the second step of the MFA login flow.
// It does NOT require a login token; the caller authenticates via mfaToken.
func (t *TotpApi) VerifyTotp(c *gin.Context) {
	req, err := a2r.ParseRequestNotCheck[pbtotp.VerifyTotpReq](c)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	resp, err := t.Client.VerifyTotp(c, req)
	if err != nil {
		log.ZError(c, "totp verify rpc failed", err, "mfaToken", req.MfaToken)
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

// GetStatus returns whether the caller has TOTP bound and the remaining recovery code count.
func (t *TotpApi) GetStatus(c *gin.Context) {
	req, err := a2r.ParseRequestNotCheck[pbtotp.GetStatusReq](c)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	req.UserID = mcontext.GetOpUserID(c)
	resp, err := t.Client.GetStatus(c, req)
	if err != nil {
		log.ZError(c, "totp get_status rpc failed", err, "userID", req.UserID)
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

// UnbindTotp removes the TOTP binding after verifying the caller's current code.
func (t *TotpApi) UnbindTotp(c *gin.Context) {
	req, err := a2r.ParseRequestNotCheck[pbtotp.UnbindTotpReq](c)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	req.UserID = mcontext.GetOpUserID(c)
	resp, err := t.Client.UnbindTotp(c, req)
	if err != nil {
		log.ZError(c, "totp unbind rpc failed", err, "userID", req.UserID)
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}
