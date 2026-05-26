package mold

import (
	Cube "ablecloud.io/ablestack-api/internal/model/cube"
	Mold "ablecloud.io/ablestack-api/internal/model/mold"
	"github.com/gin-gonic/gin"
	"net/http"
)

// GetStatus godoc
//
//	@Summary		Show Status of MOLD
//	@Description	MOLD의 상태값을 보여줍니다.
//	@Tags			API, Mold, MOLD
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Success		200	{object}	Mold.TypeMoldStatus
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		404	{object}	HTTP404NotFound
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/mold [get]
func GetStatus(ctx *gin.Context) {

	ctx.IndentedJSON(http.StatusOK, Mold.Status())
}

func MonitorStatus() {
	Mold.UpdateStatus()
}

func GetCCVMInfo(ctx *gin.Context) {
	ctx.IndentedJSON(http.StatusOK, Cube.GetVMStatus("ccvm"))
}
