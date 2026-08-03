package cube

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type GlueClusterUpdateRequest = CubeModel.GlueClusterUpdateRequest
type GlueClusterUpdateResponse = CubeModel.GlueClusterUpdateResponse

const (
	glueClusterUpdateCommandTimeout = 10 * time.Second
	glueClusterUpdateSuccessVal     = "success"
	glueClusterMaintenanceOnRetName = "Maintenance Mode On"
	glueClusterOffRetName           = "Maintenance Mode Off"
)

var glueClusterMaintenanceFlags = []string{"noout", "nobackfill", "norecover"}

// UpdateGlueCluster godoc
//
//	@Summary		Glue Cluster Update
//	@Description	스토리지 클러스터 유지보수 모드를 설정하거나 해제합니다.
//	@Tags			Cube-GlueCluster
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.GlueClusterUpdateRequest	true	"glue cluster update request"
//	@Success		200	{object}	CubeModel.GlueClusterUpdateResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/gluecluster/update [post]
func UpdateGlueCluster(context *gin.Context) {
	var req GlueClusterUpdateRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := normalizeGlueClusterUpdateRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	resp := runGlueClusterUpdate(req)
	context.JSON(statusCodeFromGlueClusterUpdateResponse(resp), resp)
}

func normalizeGlueClusterUpdateRequest(req *GlueClusterUpdateRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}

	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "set_noout":
		req.Action = "set_noout"
	case "unset_noout":
		req.Action = "unset_noout"
	default:
		return fmt.Errorf("unsupported action")
	}
	return nil
}

func runGlueClusterUpdate(req GlueClusterUpdateRequest) GlueClusterUpdateResponse {
	action := "set"
	retName := glueClusterMaintenanceOnRetName
	if req.Action == "unset_noout" {
		action = "unset"
		retName = glueClusterOffRetName
	}

	for _, flag := range glueClusterMaintenanceFlags {
		if err := runGlueClusterMaintenanceCommand(action, flag); err != nil {
			val := fmt.Sprintf("%s %s ERROR", action, flag)
			return GlueClusterUpdateResponse{
				Code:    http.StatusInternalServerError,
				Val:     val,
				RetName: retName,
				Message: err.Error(),
				Action:  req.Action,
			}
		}
	}

	return GlueClusterUpdateResponse{
		Code:    http.StatusOK,
		Val:     glueClusterUpdateSuccessVal,
		RetName: retName,
		Message: glueClusterUpdateSuccessVal,
		Action:  req.Action,
	}
}

func runGlueClusterMaintenanceCommand(action string, flag string) error {
	output, timedOut, err := runCommandOutputWithEnv(
		"ceph",
		glueClusterUpdateCommandTimeout,
		glueClusterUpdateCommandEnv(),
		"osd",
		action,
		flag,
	)
	command := fmt.Sprintf("ceph osd %s %s", action, flag)
	if timedOut {
		return fmt.Errorf("%s timed out after %s", command, glueClusterUpdateCommandTimeout)
	}
	if err != nil {
		message := strings.TrimSpace(output)
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("%s failed: %s", command, message)
	}
	return nil
}

func glueClusterUpdateCommandEnv() []string {
	return append(os.Environ(), "LANG=en_US.utf-8", "LANGUAGE=en")
}

func statusCodeFromGlueClusterUpdateResponse(resp GlueClusterUpdateResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
