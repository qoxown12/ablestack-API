package dashboard

import (
	Dashboard "ablecloud.io/ablestack-api/internal/model/dashboard"
	"github.com/gin-gonic/gin"
	"net/http"
)

// GetStatus godoc
//
//	@Summary		Show StorageCenterClusterStatus of GLUE
//	@Description	GLUE의 상태값을 보여줍니다.
//	@Tags			API, Dashboard
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Success		200	{object}	Dashboard.TypeStorageCenterCluster
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		404	{object}	HTTP404NotFound
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/dashboard [get]
func GetStatus(ctx *gin.Context) {
	SCC := Dashboard.StorageCenterClusterUpdateStatus()
	CCV := Dashboard.CloudVMStatus()
	ret := map[string]interface{}{
		"StorageCenterCluster": SCC,
		"CloudCenterVM":        CCV,
	}
	ctx.IndentedJSON(http.StatusOK, ret)
}

func Monitor() {
	Dashboard.StorageCenterClusterUpdateStatus()
	Dashboard.CloudVMUpdateStatus()
}

//func MonitorDashboard() {
//	Dashboard.StorageCenterClusterUpdateStatus()
//}
