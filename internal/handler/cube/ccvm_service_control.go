package cube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type CCVMServiceControlRequest = CubeModel.CCVMServiceControlRequest
type CCVMServiceControlResponse = CubeModel.CCVMServiceControlResponse

// CCVMServiceControl godoc
//
//	@Summary		CCVM Service Control
//	@Description	CCVM의 서비스 제어 요청을 CCVM 노드로 전달합니다.
//	@Tags			CUBE - CCVM
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.CCVMServiceControlRequest	true	"ccvm service control request"
//	@Success		200	{object}	CubeModel.CCVMServiceControlResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/ccvm/service/control [post]
func CCVMServiceControl(context *gin.Context) {
	var req CCVMServiceControlRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	action := normalizeCCVMServiceAction(req.Action)
	if action == "" {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "unsupported action",
		})
		return
	}
	if strings.TrimSpace(req.ServiceName) == "" {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "service_name required",
		})
		return
	}

	cfg, err := loadClusterConfigSection()
	if err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}
	target := strings.TrimSpace(cfg.CCVM.IP)
	if target == "" {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "ccvm ip required",
		})
		return
	}

	if isLocalTarget(target) {
		resp := runCCVMServiceAction(action, req.ServiceName)
		if resp.Code != 200 {
			context.JSON(http.StatusInternalServerError, resp)
			return
		}
		context.JSON(http.StatusOK, resp)
		return
	}

	resp, err := callCCVMServiceControl(target, CCVMServiceControlRequest{
		Action:      action,
		ServiceName: req.ServiceName,
	})
	if err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: err.Error(),
		})
		return
	}
	if resp.Code != 200 {
		context.JSON(http.StatusInternalServerError, resp)
		return
	}
	context.JSON(http.StatusOK, resp)
}

// normalizeCCVMServiceAction은 허용된 systemctl 액션만 정규화해 반환한다.
func normalizeCCVMServiceAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "start":
		return "start"
	case "restart":
		return "restart"
	case "stop":
		return "stop"
	case "status":
		return "status"
	default:
		return ""
	}
}

// runCCVMServiceAction은 현재 노드에서 systemctl 명령으로 서비스를 직접 제어한다.
func runCCVMServiceAction(action, serviceName string) CCVMServiceControlResponse {
	cmd := exec.Command("systemctl", action, serviceName)
	if err := cmd.Run(); err != nil {
		return CCVMServiceControlResponse{
			Code: 500,
			Val:  fmt.Sprintf("%s service %s control error", serviceName, action),
		}
	}
	return CCVMServiceControlResponse{
		Code: 200,
		Val:  fmt.Sprintf("%s service %s control success", serviceName, action),
	}
}

// callCCVMServiceControl은 원격 CCVM 노드의 서비스 제어 API를 호출한다.
func callCCVMServiceControl(target string, req CCVMServiceControlRequest) (CCVMServiceControlResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return CCVMServiceControlResponse{}, err
	}

	baseURL := buildTargetURL(target)
	url := fmt.Sprintf("%s/api/v1/cube/ccvm/service/control", baseURL)
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return CCVMServiceControlResponse{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return CCVMServiceControlResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return CCVMServiceControlResponse{}, fmt.Errorf("ccvm service control failed: %s", resp.Status)
	}

	var out CCVMServiceControlResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CCVMServiceControlResponse{}, err
	}
	return out, nil
}
