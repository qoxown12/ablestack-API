package cube

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Health godoc
//
//	@Summary		API Health
//	@Description	라이선스 등록 전 bootstrap 단계에서도 사용할 수 있는 API 서버 생존 확인입니다.
//	@Tags			Health
//	@Produce		json
//	@Success		200	{object}	ClusterHealthResponse
//	@Router			/health [get]
func Health(context *gin.Context) {
	context.JSON(http.StatusOK, ClusterHealthResponse{
		Status:  "ok",
		Code:    http.StatusOK,
		Message: "ok",
	})
}
