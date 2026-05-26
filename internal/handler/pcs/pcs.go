package pcs

import (
	PCS "ablecloud.io/ablestack-api/internal/model/pcs"
	"github.com/gin-gonic/gin"
	"net/http"
)

// GetStatus godoc
//
//	@Summary		Show Status of PCS
//	@Description	PCS 상태값을 보여줍니다.
//	@Tags			API, PCS
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Success		200	{object}	PCS.TypePCSStatus
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		404	{object}	HTTP404NotFound
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/pcs [get]
func GetStatus(ctx *gin.Context) {
	ctx.IndentedJSON(http.StatusOK, PCS.Status())
}

// GetResource godoc
//
//	@Summary		Show Status of PCS
//	@Description	PCS 상태값을 보여줍니다.
//	@Tags			API, PCS
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Success		200	{object}	PCS.TypePCSResources
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		404	{object}	HTTP404NotFound
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/pcs/resources [get]
func GetResource(ctx *gin.Context) {
	ctx.IndentedJSON(http.StatusOK, PCS.GetResource())
}

func Monitor() {
	PCS.UpdateStatus()
}
