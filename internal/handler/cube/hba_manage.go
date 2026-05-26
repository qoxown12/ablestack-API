package cube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type HBAManageRequest = CubeModel.HBAManageRequest
type HBAManageResponse = CubeModel.HBAManageResponse
type HBAWWNResult = CubeModel.HBAWWNResult

const (
	hbaManageLocalHeader = "X-Cube-HBA-Manage-Local"
	hbaManageRequestTO   = 2 * time.Minute
	hbaManageCommandTO   = 30 * time.Second
)

// HBAManage godoc
//
//	@Summary		HBA Manage
//	@Description	cluster.json hosts[].ablecube 대상 API를 호출해 호스트별 HBA WWN을 조회합니다. SSH는 사용하지 않습니다.
//	@Tags			CUBE - HBA
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.HBAManageRequest	true	"hba manage request"
//	@Success		200	{object}	CubeModel.HBAManageResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/hba/manage [post]
func HBAManage(context *gin.Context) {
	var req HBAManageRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}
	if err := normalizeHBAManageRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	if strings.EqualFold(strings.TrimSpace(context.GetHeader(hbaManageLocalHeader)), "1") {
		result := localHBAWWNResult("", "")
		context.JSON(http.StatusOK, HBAManageResponse{Code: http.StatusOK, Val: []HBAWWNResult{result}, Message: "ok", Action: req.Action})
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

	resp := runHBAManage(req, cfg)
	context.JSON(statusCodeFromHBAManageResponse(resp), resp)
}

func normalizeHBAManageRequest(req *HBAManageRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "list-hba-wwn", "list":
		req.Action = "list-hba-wwn"
	default:
		return fmt.Errorf("unsupported action")
	}
	return nil
}

func runHBAManage(req HBAManageRequest, cfg *CubeModel.ClusterConfigSection) HBAManageResponse {
	targets := buildGFSManageTargets(cfg)
	if len(targets) == 0 {
		return HBAManageResponse{Code: http.StatusInternalServerError, Message: "hosts[].ablecube required", Action: req.Action}
	}
	client := &http.Client{Timeout: hbaManageRequestTO}
	results := make([]HBAWWNResult, 0, len(targets))
	for _, target := range targets {
		if target.Target == "" || isLocalTarget(target.Target) || isGFSManageLocalHostname(target.Hostname) {
			results = append(results, localHBAWWNResult(target.Hostname, target.Target))
			continue
		}
		result, err := callHBAManageRemote(client, target, req)
		if err != nil {
			result = HBAWWNResult{Hostname: target.Hostname, Target: target.Target, WWN: []string{}, Error: err.Error()}
		}
		results = append(results, result)
	}
	code := http.StatusOK
	for _, result := range results {
		if result.Error != "" {
			code = http.StatusInternalServerError
			break
		}
	}
	return HBAManageResponse{Code: code, Val: results, Message: "ok", Action: req.Action}
}

func callHBAManageRemote(client *http.Client, target gfsManageTarget, req HBAManageRequest) (HBAWWNResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return HBAWWNResult{}, err
	}
	url := fmt.Sprintf("%s/api/v1/cube/hba/manage", buildTargetURL(target.Target))
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return HBAWWNResult{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(hbaManageLocalHeader, "1")

	resp, err := client.Do(httpReq)
	if err != nil {
		return HBAWWNResult{}, err
	}
	defer resp.Body.Close()

	var out HBAManageResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return HBAWWNResult{}, err
	}
	if len(out.Val) == 0 {
		return HBAWWNResult{}, fmt.Errorf("empty hba response")
	}
	result := out.Val[0]
	result.Hostname = firstNonEmpty(result.Hostname, target.Hostname)
	result.Target = firstNonEmpty(result.Target, target.Target)
	return result, nil
}

func localHBAWWNResult(hostname string, target string) HBAWWNResult {
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	result := HBAWWNResult{
		Hostname: hostname,
		Target:   target,
		WWN:      []string{},
	}
	out, _, err := runCommandOutputWithEnv("lspci", hbaManageCommandTO, gfsManageCommandEnv())
	if err != nil || !strings.Contains(strings.ToLower(out), "fibre") {
		return result
	}
	paths, err := filepath.Glob("/sys/class/fc_host/host*/port_name")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			result.Error = err.Error()
			continue
		}
		value := strings.TrimSpace(string(raw))
		if value != "" {
			result.WWN = append(result.WWN, value)
		}
	}
	result.WWN = normalizeStringSlice(result.WWN)
	return result
}

func statusCodeFromHBAManageResponse(resp HBAManageResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
