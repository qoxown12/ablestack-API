package cube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type CloudInitStatusRequest = CubeModel.CloudInitStatusRequest
type CloudInitStatusResponse = CubeModel.CloudInitStatusResponse
type CloudInitStatusResult = CubeModel.CloudInitStatusResult

const (
	cloudInitStatusLocalHeader = "X-Cube-CloudInit-Local"
	cloudInitStatusRetName     = "CloudInit Status"
	cloudInitStatusTimeout     = 20 * time.Second
	cloudInitPingTimeout       = 5 * time.Second
)

type cloudInitTarget struct {
	Role     string
	Hostname string
	Target   string
}

// CloudInitStatus godoc
//
//	@Summary		Cloud-Init Status
//	@Description	SCVM/CCVM API를 호출해 대상 VM의 cloud-init status 또는 API 기반 ping 상태를 확인합니다. target은 ccvm, scvm 또는 직접 IP를 사용할 수 있습니다.
//	@Tags			CUBE - CloudInit
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.CloudInitStatusRequest	true	"cloud-init status request"
//	@Success		200	{object}	CubeModel.CloudInitStatusResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/cloudinit/status [post]
func CloudInitStatus(context *gin.Context) {
	var req CloudInitStatusRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := normalizeCloudInitStatusRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	if isCloudInitStatusLocalRequest(context) {
		resp := cloudInitStatusResponse(req.Action, []CloudInitStatusResult{
			runCloudInitStatusLocal(req, cloudInitTarget{Target: firstNonEmpty(req.Target, "local")}),
		})
		context.JSON(statusCodeFromCloudInitStatusResponse(resp), resp)
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

	targets, err := resolveCloudInitTargets(req, cfg)
	if err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	if len(targets) == 0 {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "no targets to apply",
		})
		return
	}

	resp := runCloudInitStatusTargets(req, targets)
	context.JSON(statusCodeFromCloudInitStatusResponse(resp), resp)
}

func normalizeCloudInitStatusRequest(req *CloudInitStatusRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "status":
		req.Action = "status"
	case "ping":
		req.Action = "ping"
	default:
		return fmt.Errorf("unsupported action")
	}
	req.Target = strings.TrimSpace(req.Target)
	req.TargetHostname = strings.TrimSpace(req.TargetHostname)
	if req.Target == "" {
		return fmt.Errorf("target required")
	}
	return nil
}

func isCloudInitStatusLocalRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(cloudInitStatusLocalHeader)), "1")
}

func resolveCloudInitTargets(req CloudInitStatusRequest, cfg *CubeModel.ClusterConfigSection) ([]cloudInitTarget, error) {
	target := strings.ToLower(strings.TrimSpace(req.Target))
	switch target {
	case "ccvm":
		if cfg == nil || strings.TrimSpace(cfg.CCVM.IP) == "" {
			return nil, fmt.Errorf("ccvm ip required")
		}
		return []cloudInitTarget{{Role: "ccvm", Target: strings.TrimSpace(cfg.CCVM.IP)}}, nil
	case "scvm":
		if cfg == nil {
			return nil, fmt.Errorf("clusterConfig not found")
		}
		hostnames := parseTargetHostnames(req.TargetHostname)
		return resolveCloudInitSCVMTargets(cfg, hostnames)
	default:
		return []cloudInitTarget{{Role: "custom", Target: strings.TrimSpace(req.Target)}}, nil
	}
}

func resolveCloudInitSCVMTargets(cfg *CubeModel.ClusterConfigSection, targetHostnames []string) ([]cloudInitTarget, error) {
	if cfg == nil {
		return nil, fmt.Errorf("clusterConfig not found")
	}
	if !isHCITarget(cfg.Type) {
		return nil, fmt.Errorf("unsupported cluster type")
	}

	hostnameFilter := map[string]struct{}{}
	for _, hostname := range targetHostnames {
		hostnameFilter[strings.TrimSpace(hostname)] = struct{}{}
	}

	targets := make([]cloudInitTarget, 0, len(cfg.Hosts))
	for _, host := range cfg.Hosts {
		hostname := strings.TrimSpace(host.Hostname)
		if len(hostnameFilter) > 0 {
			if _, ok := hostnameFilter[hostname]; !ok {
				continue
			}
		}
		scvmTarget := strings.TrimSpace(host.ScvmMngt)
		if scvmTarget == "" {
			continue
		}
		targets = append(targets, cloudInitTarget{
			Role:     "scvm",
			Hostname: hostname,
			Target:   scvmTarget,
		})
	}
	if len(targetHostnames) > 0 && len(targets) == 0 {
		return nil, fmt.Errorf("target_hostname not found")
	}
	return dedupeCloudInitTargets(targets), nil
}

func dedupeCloudInitTargets(targets []cloudInitTarget) []cloudInitTarget {
	seen := map[string]struct{}{}
	out := make([]cloudInitTarget, 0, len(targets))
	for _, target := range targets {
		key := fmt.Sprintf("%s|%s|%s", target.Role, target.Hostname, target.Target)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, target)
	}
	return out
}

func runCloudInitStatusTargets(req CloudInitStatusRequest, targets []cloudInitTarget) CloudInitStatusResponse {
	client := &http.Client{Timeout: cloudInitStatusTimeout}
	results := make([]CloudInitStatusResult, 0, len(targets))
	for _, target := range targets {
		if isLocalTarget(target.Target) {
			results = append(results, runCloudInitStatusLocal(req, target))
			continue
		}

		result, err := callCloudInitStatusRemote(client, target, req)
		if err != nil {
			result = CloudInitStatusResult{
				Role:     target.Role,
				Hostname: target.Hostname,
				Target:   target.Target,
				Code:     http.StatusInternalServerError,
				Message:  err.Error(),
			}
		}
		results = append(results, result)
	}
	return cloudInitStatusResponse(req.Action, results)
}

func runCloudInitStatusLocal(req CloudInitStatusRequest, target cloudInitTarget) CloudInitStatusResult {
	result := CloudInitStatusResult{
		Role:     target.Role,
		Hostname: target.Hostname,
		Target:   firstNonEmpty(target.Target, req.Target, "local"),
		Code:     http.StatusOK,
		Message:  "ok",
	}

	switch req.Action {
	case "status":
		lines, err := readLocalCloudInitStatus()
		if err != nil {
			result.Code = http.StatusInternalServerError
			result.Message = err.Error()
			return result
		}
		result.Val = lines
	case "ping":
		pingTarget := firstNonEmpty(target.Target, req.Target, "local")
		if pingTarget != "local" {
			if err := pingCloudInitTarget(pingTarget); err != nil {
				result.Code = http.StatusInternalServerError
				result.Message = err.Error()
				return result
			}
		}
		result.Val = map[string]string{"host": pingTarget, "ping": "OK"}
	}
	return result
}

func readLocalCloudInitStatus() ([]string, error) {
	command := "/usr/bin/cloud-init"
	if _, err := os.Stat(command); err != nil {
		command = "cloud-init"
	}
	out, timedOut, err := runCommandOutputWithEnv(command, cloudInitStatusTimeout, cloudInitCommandEnv(), "status")
	if timedOut {
		return nil, fmt.Errorf("cloud-init status timed out after %s", cloudInitStatusTimeout)
	}
	if err != nil {
		return nil, fmt.Errorf("cloud-init status failed: %s", firstNonEmpty(strings.TrimSpace(out), err.Error()))
	}
	return splitLines(out), nil
}

func pingCloudInitTarget(target string) error {
	out, timedOut, err := runCommandOutputWithEnv("ping", cloudInitPingTimeout, cloudInitCommandEnv(), "-c", "1", target)
	if timedOut {
		return fmt.Errorf("ping %s timed out after %s", target, cloudInitPingTimeout)
	}
	if err != nil {
		return fmt.Errorf("ping %s failed: %s", target, firstNonEmpty(strings.TrimSpace(out), err.Error()))
	}
	return nil
}

func callCloudInitStatusRemote(client *http.Client, target cloudInitTarget, req CloudInitStatusRequest) (CloudInitStatusResult, error) {
	localReq := CloudInitStatusRequest{
		Action: req.Action,
		Target: target.Target,
	}
	body, err := json.Marshal(localReq)
	if err != nil {
		return CloudInitStatusResult{}, err
	}

	url := fmt.Sprintf("%s/api/v1/cube/cloudinit/status", buildTargetURL(target.Target))
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return CloudInitStatusResult{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(cloudInitStatusLocalHeader, "1")

	resp, err := client.Do(httpReq)
	if err != nil {
		return CloudInitStatusResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return CloudInitStatusResult{}, fmt.Errorf("cloud-init status failed: %s", resp.Status)
	}

	var out CloudInitStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CloudInitStatusResult{}, err
	}
	if len(out.Results) == 0 {
		return CloudInitStatusResult{}, fmt.Errorf("cloud-init status empty from %s", target.Target)
	}

	result := out.Results[0]
	result.Role = firstNonEmpty(result.Role, target.Role)
	result.Hostname = firstNonEmpty(result.Hostname, target.Hostname)
	result.Target = firstNonEmpty(result.Target, target.Target)
	return result, nil
}

func cloudInitStatusResponse(action string, results []CloudInitStatusResult) CloudInitStatusResponse {
	hasFail := false
	for _, result := range results {
		if result.Code != http.StatusOK {
			hasFail = true
			break
		}
	}

	code := http.StatusOK
	message := "cloud-init status check success"
	if hasFail {
		code = http.StatusInternalServerError
		message = "cloud-init status check failed"
	}
	return CloudInitStatusResponse{
		Code:    code,
		Val:     results,
		RetName: cloudInitStatusRetName,
		Message: message,
		Action:  action,
		Results: results,
	}
}

func statusCodeFromCloudInitStatusResponse(resp CloudInitStatusResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func cloudInitCommandEnv() []string {
	return append([]string{"LANG=en_US.utf-8", "LANGUAGE=en"}, os.Environ()...)
}
