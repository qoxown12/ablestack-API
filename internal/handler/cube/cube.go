package cube

import (
	"net/http"

	utils "ablecloud.io/ablestack-api/internal/infra/utils"
	Cube "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

var _ = utils.TypeVersion{}

// Version godoc
//
//	@Summary		Show Versions of CUBE
//	@Description	CUBE 의 버전을 보여줍니다.
//	@Tags			CUBE - Version
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Success		200	{object}	utils.TypeVersion
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		404	{object}	HTTP404NotFound
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/version [get]
//
// Version은 현재 CUBE 버전과 디버그 여부를 반환한다.
func Version(context *gin.Context) {
	dat := Cube.Cube().GetVersion()
	// Print the output
	dat.Debug = gin.IsDebugging()
	context.IndentedJSON(http.StatusOK, dat)
} // @name Version
