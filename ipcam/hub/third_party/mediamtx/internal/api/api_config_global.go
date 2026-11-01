package api //nolint:revive

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/conf"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/conf/jsonwrapper"
)

func (a *API) onConfigGlobalGet(ctx *gin.Context) {
	c := conf.Redact(a.Parent.APIConfigSnapshot())

	ctx.JSON(http.StatusOK, c.Global())
}

func (a *API) onConfigGlobalPatch(ctx *gin.Context) {
	var c conf.OptionalGlobal
	err := jsonwrapper.Decode(&customLimitReader{ctx.Request.Body, maxInboundConfigSize}, &c)
	if err != nil {
		a.writeError(ctx, http.StatusBadRequest, err)
		return
	}

	err = a.Parent.APIConfigGlobalPatch(c)
	if err != nil {
		a.writeError(ctx, http.StatusBadRequest, err)
		return
	}

	a.writeOK(ctx)
}
