package api

import (
	"github.com/gin-gonic/gin"
	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
	"github.com/openimsdk/open-im-server/v3/pkg/linkpreview"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
)

type LinkPreviewApi struct{}

func NewLinkPreviewApi() *LinkPreviewApi {
	return &LinkPreviewApi{}
}

func (l *LinkPreviewApi) Preview(c *gin.Context) {
	var req apistruct.LinkPreviewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}

	resp, err := linkpreview.Fetch(c.Request.Context(), req.URL)
	if err != nil {
		log.ZWarn(c, "link preview fetch failed", err, "url", req.URL)
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}
