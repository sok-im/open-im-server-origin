package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	pbredpacket "github.com/openimsdk/protocol/redpacket"
	"github.com/openimsdk/tools/a2r"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/log"
)

// RedPacketApi holds the gRPC client for the redpacket service.
type RedPacketApi struct {
	Client         pbredpacket.RedPacketClient
	imAdminUserIDs []string
}

func NewRedPacketApi(client pbredpacket.RedPacketClient, imAdminUserIDs []string) *RedPacketApi {
	return &RedPacketApi{Client: client, imAdminUserIDs: imAdminUserIDs}
}

// requireAdmin verifies the current OpUserID is in the configured IM admin list.
// Returns true on success; on failure responds with 403 and returns false.
func (r *RedPacketApi) requireAdmin(ctx *gin.Context) bool {
	if err := authverify.CheckAdmin(ctx, r.imAdminUserIDs); err != nil {
		apiresp.GinError(ctx, err)
		return false
	}
	return true
}

// ─── User endpoints ────────────────────────────────────────────────────────────

func (r *RedPacketApi) CreateOrder(ctx *gin.Context) {
	req, err := a2r.ParseRequestNotCheck[pbredpacket.CreateOrderReq](ctx)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	resp, err := r.Client.CreateOrder(ctx, req)
	if err != nil {
		log.ZError(ctx, "redpacket create-order rpc failed", err)
		apiresp.GinError(ctx, err)
		return
	}
	apiresp.GinSuccess(ctx, resp)
}

func (r *RedPacketApi) CreatedCallback(ctx *gin.Context) {
	req, err := a2r.ParseRequestNotCheck[pbredpacket.CreatedCallbackReq](ctx)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	resp, err := r.Client.CreatedCallback(ctx, req)
	if err != nil {
		log.ZError(ctx, "redpacket created-callback rpc failed", err)
		apiresp.GinError(ctx, err)
		return
	}
	apiresp.GinSuccess(ctx, resp)
}

func (r *RedPacketApi) GetDetail(ctx *gin.Context) {
	packetID := ctx.Query("packet_id")
	if packetID == "" {
		apiresp.GinError(ctx, servererrs.ErrArgs.WrapMsg("packet_id is required"))
		return
	}
	resp, err := r.Client.GetDetail(ctx, &pbredpacket.GetDetailReq{PacketId: packetID})
	if err != nil {
		log.ZError(ctx, "redpacket get-detail rpc failed", err, "packetID", packetID)
		apiresp.GinError(ctx, err)
		return
	}
	apiresp.GinSuccess(ctx, resp)
}

func (r *RedPacketApi) ClaimSign(ctx *gin.Context) {
	req, err := a2r.ParseRequestNotCheck[pbredpacket.ClaimSignReq](ctx)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	resp, err := r.Client.ClaimSign(ctx, req)
	if err != nil {
		log.ZError(ctx, "redpacket claim-sign rpc failed", err)
		apiresp.GinError(ctx, err)
		return
	}
	apiresp.GinSuccess(ctx, resp)
}

func (r *RedPacketApi) ClaimResult(ctx *gin.Context) {
	req, err := a2r.ParseRequestNotCheck[pbredpacket.ClaimResultReq](ctx)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	resp, err := r.Client.ClaimResult(ctx, req)
	if err != nil {
		log.ZError(ctx, "redpacket claim-result rpc failed", err)
		apiresp.GinError(ctx, err)
		return
	}
	apiresp.GinSuccess(ctx, resp)
}

// ─── Admin endpoints (require IMAdminUserID auth) ──────────────────────────────

func (r *RedPacketApi) SetSigner(ctx *gin.Context) {
	if !r.requireAdmin(ctx) {
		return
	}
	req, err := a2r.ParseRequestNotCheck[pbredpacket.SetSignerReq](ctx)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	resp, err := r.Client.SetSigner(ctx, req)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	apiresp.GinSuccess(ctx, resp)
}

func (r *RedPacketApi) SetToken(ctx *gin.Context) {
	if !r.requireAdmin(ctx) {
		return
	}
	req, err := a2r.ParseRequestNotCheck[pbredpacket.SetTokenReq](ctx)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	resp, err := r.Client.SetToken(ctx, req)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	apiresp.GinSuccess(ctx, resp)
}

func (r *RedPacketApi) SetExpiry(ctx *gin.Context) {
	if !r.requireAdmin(ctx) {
		return
	}
	req, err := a2r.ParseRequestNotCheck[pbredpacket.SetExpiryReq](ctx)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	resp, err := r.Client.SetExpiry(ctx, req)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	apiresp.GinSuccess(ctx, resp)
}

func (r *RedPacketApi) SetAllowAllTokens(ctx *gin.Context) {
	if !r.requireAdmin(ctx) {
		return
	}
	req, err := a2r.ParseRequestNotCheck[pbredpacket.SetAllowAllTokensReq](ctx)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	resp, err := r.Client.SetAllowAllTokens(ctx, req)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	apiresp.GinSuccess(ctx, resp)
}

func (r *RedPacketApi) SetNativeToken(ctx *gin.Context) {
	if !r.requireAdmin(ctx) {
		return
	}
	req, err := a2r.ParseRequestNotCheck[pbredpacket.SetNativeTokenReq](ctx)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	resp, err := r.Client.SetNativeToken(ctx, req)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	apiresp.GinSuccess(ctx, resp)
}

func (r *RedPacketApi) ParseTxEvents(ctx *gin.Context) {
	if !r.requireAdmin(ctx) {
		return
	}
	req, err := a2r.ParseRequestNotCheck[pbredpacket.ParseTxEventsReq](ctx)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	resp, err := r.Client.ParseTxEvents(ctx, req)
	if err != nil {
		apiresp.GinError(ctx, err)
		return
	}
	type eventOut struct {
		Name string          `json:"name"`
		Data json.RawMessage `json:"data"`
	}
	out := make([]eventOut, 0, len(resp.Events))
	for _, ev := range resp.Events {
		data := ev.Data
		if !json.Valid(data) {
			data = []byte("null")
		}
		out = append(out, eventOut{Name: ev.Name, Data: json.RawMessage(data)})
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": out})
}
