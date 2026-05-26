package cube

import (
	"net/http"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type GlueClusterStatusResponse = CubeModel.GlueClusterStatusResponse

// GetGlueClusterStatus godoc
//
//	@Summary		Glue Cluster Status
//	@Description	스토리지 클러스터 상태 상세 정보를 조회합니다.
//	@Tags			CUBE - Glue Cluster
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Success		200	{object}	CubeModel.GlueClusterStatusResponse
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/gluecluster/status [get]
func GetGlueClusterStatus(context *gin.Context) {
	resp, err := CubeModel.GlueClusterStatusDetail()
	if err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: err.Error(),
		})
		return
	}
	context.IndentedJSON(http.StatusOK, resp)
}
