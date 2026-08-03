package cube

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	libvirtinfra "ablecloud.io/ablestack-api/internal/infra/libvirt"
	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type BootstrapRequest = CubeModel.BootstrapRequest
type BootstrapResponse = CubeModel.BootstrapResponse
type BootstrapScriptResult = CubeModel.BootstrapScriptResult
type BootstrapHealthResult = CubeModel.BootstrapHealthResult

const (
	bootstrapLocalHeader        = "X-Cube-Bootstrap-Local"
	bootstrapScriptPath         = "/root/bootstrap.sh"
	bootstrapGuestLogPath       = "/var/log/ablestack-bootstrap.log"
	bootstrapRemoteRequestTO    = 125 * time.Minute
	bootstrapScriptExecTO       = 120 * time.Minute
	bootstrapGuestCommandTO     = 10 * time.Second
	bootstrapGuestPollInterval  = 2 * time.Second
	bootstrapGuestReadyInterval = 5 * time.Second
)

type bootstrapScriptTarget struct {
	Role     string
	Hostname string
	Target   string
	Domain   string
	Args     []string
}

type bootstrapGuestExecResponse struct {
	Return struct {
		PID int `json:"pid"`
	} `json:"return"`
}

type bootstrapGuestExecStatusResponse struct {
	Return struct {
		Exited   bool   `json:"exited"`
		ExitCode int    `json:"exitcode"`
		OutData  string `json:"out-data"`
		ErrData  string `json:"err-data"`
	} `json:"return"`
}

// SCVMBootstrap godoc
//
//	@Summary		SCVM Bootstrap
//	@Description	대표 SCVM의 /root/bootstrap.sh를 host qemu-guest-agent로 실행한 뒤 SCVM API health 확인, 라이선스 등록/status 확인을 수행합니다. deploy_run의 scvm_bootstrap step도 같은 실행 흐름을 사용합니다.
//	@Tags			Cube-SCVM
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.BootstrapRequest	false	"scvm bootstrap request"
//	@Success		200	{object}	CubeModel.BootstrapResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/scvm/bootstrap [post]
func SCVMBootstrap(context *gin.Context) {
	runBootstrapHandler(context, licenseApplyRoleSCVM)
}

// CCVMBootstrap godoc
//
//	@Summary		CCVM Bootstrap
//	@Description	CCVM의 /root/bootstrap.sh를 host qemu-guest-agent로 실행한 뒤 CCVM API health 확인, 라이선스 등록/status 확인을 수행합니다. deploy_run의 ccvm_bootstrap step도 같은 실행 흐름을 사용합니다.
//	@Tags			Cube-CCVM
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.BootstrapRequest	false	"ccvm bootstrap request"
//	@Success		200	{object}	CubeModel.BootstrapResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/ccvm/bootstrap [post]
func CCVMBootstrap(context *gin.Context) {
	runBootstrapHandler(context, licenseApplyRoleCCVM)
}

func runBootstrapHandler(context *gin.Context, role string) {
	var req BootstrapRequest
	if context.Request != nil && context.Request.Body != nil && context.Request.ContentLength != 0 {
		if err := context.ShouldBindJSON(&req); err != nil {
			context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
				ErrCode: http.StatusBadRequest,
				Message: "invalid request",
			})
			return
		}
	}

	if isBootstrapLocalRequest(context) {
		resp := runBootstrapScriptLocalRequest(req, role)
		context.JSON(statusCodeFromBootstrapResponse(resp), resp)
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
	if role == licenseApplyRoleSCVM && !isHCITarget(cfg.Type) {
		context.JSON(http.StatusOK, BootstrapResponse{
			Code:    http.StatusOK,
			Role:    role,
			Message: "scvm_bootstrap is not required for " + strings.TrimSpace(cfg.Type),
		})
		return
	}

	resp := runBootstrapRole(req, cfg, role, context.GetHeader("Authorization"))
	context.JSON(statusCodeFromBootstrapResponse(resp), resp)
}

func bootstrapRequestFromDeployRun(req DeployRunRequest) BootstrapRequest {
	return BootstrapRequest{
		LicenseContent:  req.LicenseContent,
		Licenses:        req.Licenses,
		LicenseFilename: req.LicenseFilename,
		RunScript:       req.RunBootstrapScript,
	}
}

func runBootstrapRole(req BootstrapRequest, cfg *CubeModel.ClusterConfigSection, role string, authHeader string) BootstrapResponse {
	role = normalizeBootstrapRole(role)
	if role == "" {
		return BootstrapResponse{Code: http.StatusBadRequest, Message: "unsupported bootstrap role"}
	}

	targets, err := buildLicenseApplyTargets(LicenseApplyRequest{
		Roles:           []string{role},
		TargetHostnames: req.TargetHostnames,
	}, cfg)
	if err != nil {
		return BootstrapResponse{Code: http.StatusBadRequest, Role: role, Message: err.Error()}
	}
	if len(targets) == 0 {
		return BootstrapResponse{Code: http.StatusBadRequest, Role: role, Message: role + " target not found"}
	}

	scriptResults, err := runBootstrapScripts(req, cfg, role)
	if err != nil {
		return BootstrapResponse{
			Code:    http.StatusInternalServerError,
			Role:    role,
			Script:  scriptResults,
			Message: err.Error(),
		}
	}

	readiness := make([]BootstrapHealthResult, 0, len(targets))
	for _, target := range targets {
		health, err := waitDeployRunAPIHealth(target.Target)
		result := bootstrapHealthResult(target, health)
		readiness = append(readiness, result)
		if err != nil {
			return BootstrapResponse{
				Code:    http.StatusInternalServerError,
				Role:    role,
				Script:  scriptResults,
				Health:  readiness,
				Message: err.Error(),
			}
		}
	}

	licenseReq := LicenseApplyRequest{
		Action:          "register",
		LicenseContent:  req.LicenseContent,
		Licenses:        req.Licenses,
		Filename:        req.LicenseFilename,
		Roles:           []string{role},
		TargetHostnames: req.TargetHostnames,
	}
	applyResp := runLicenseApply(licenseReq, cfg, authHeader)
	if applyResp.Code != http.StatusOK {
		return BootstrapResponse{
			Code:         statusCodeFromLicenseApplyResponse(applyResp),
			Role:         role,
			Script:       scriptResults,
			Health:       readiness,
			LicenseApply: &applyResp,
			Message:      firstNonEmpty(applyResp.Message, role+"_bootstrap license apply failed"),
		}
	}

	statusResp := runLicenseApply(LicenseApplyRequest{
		Action:          "status",
		Roles:           []string{role},
		TargetHostnames: req.TargetHostnames,
	}, cfg, authHeader)
	if statusResp.Code != http.StatusOK {
		return BootstrapResponse{
			Code:          statusCodeFromLicenseApplyResponse(statusResp),
			Role:          role,
			Script:        scriptResults,
			Health:        readiness,
			LicenseApply:  &applyResp,
			LicenseStatus: &statusResp,
			Message:       firstNonEmpty(statusResp.Message, role+"_bootstrap license status failed"),
		}
	}

	return BootstrapResponse{
		Code:          http.StatusOK,
		Role:          role,
		Script:        scriptResults,
		Health:        readiness,
		LicenseApply:  &applyResp,
		LicenseStatus: &statusResp,
		Message:       role + "_bootstrap success",
	}
}

func normalizeBootstrapRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "scvm", "storage", "storage-vm", "storage_vm":
		return licenseApplyRoleSCVM
	case "ccvm", "cloud", "cloud-vm", "cloud_vm":
		return licenseApplyRoleCCVM
	default:
		return ""
	}
}

func isBootstrapLocalRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(bootstrapLocalHeader)), "1")
}

func runBootstrapScriptLocalRequest(req BootstrapRequest, role string) BootstrapResponse {
	role = normalizeBootstrapRole(role)
	if role == "" {
		return BootstrapResponse{Code: http.StatusBadRequest, Message: "unsupported bootstrap role"}
	}
	target := bootstrapScriptTarget{
		Role:     role,
		Hostname: firstNonEmpty(req.ScriptHostname, role),
		Target:   firstNonEmpty(req.ScriptTarget, "local"),
		Domain:   firstNonEmpty(req.ScriptDomain, defaultBootstrapScriptDomain(role)),
		Args:     normalizeStringSlice(req.ScriptArgs),
	}
	result := runBootstrapScriptLocal(target)
	resp := BootstrapResponse{
		Code:   result.Code,
		Role:   role,
		Script: []BootstrapScriptResult{result},
	}
	if result.Code == http.StatusOK {
		resp.Message = "bootstrap script success"
	} else {
		resp.Message = firstNonEmpty(result.Message, "bootstrap script failed")
	}
	return resp
}

func runBootstrapScripts(req BootstrapRequest, cfg *CubeModel.ClusterConfigSection, role string) ([]BootstrapScriptResult, error) {
	if !shouldRunBootstrapScript(req) {
		return nil, nil
	}

	targets, err := buildBootstrapScriptTargets(req, cfg, role)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("%s bootstrap script target not found", role)
	}

	results := make([]BootstrapScriptResult, 0, len(targets))
	for _, target := range targets {
		var result BootstrapScriptResult
		if isBootstrapScriptLocalTarget(target) {
			result = runBootstrapScriptLocal(target)
		} else {
			result = callBootstrapScriptRemote(target)
		}
		results = append(results, result)
		if result.Code != http.StatusOK {
			return results, fmt.Errorf("%s bootstrap script failed on %s: %s", role, firstNonEmpty(target.Hostname, target.Target), firstNonEmpty(result.Message, "unknown error"))
		}
	}
	return results, nil
}

func shouldRunBootstrapScript(req BootstrapRequest) bool {
	if req.RunScript == nil {
		return true
	}
	return *req.RunScript
}

func buildBootstrapScriptTargets(req BootstrapRequest, cfg *CubeModel.ClusterConfigSection, role string) ([]bootstrapScriptTarget, error) {
	switch role {
	case licenseApplyRoleSCVM:
		target, err := buildSCVMBootstrapScriptTarget(req, cfg)
		if err != nil {
			return nil, err
		}
		return []bootstrapScriptTarget{target}, nil
	case licenseApplyRoleCCVM:
		target, err := buildCCVMBootstrapScriptTarget(req, cfg)
		if err != nil {
			return nil, err
		}
		return []bootstrapScriptTarget{target}, nil
	default:
		return nil, fmt.Errorf("unsupported bootstrap role")
	}
}

func buildSCVMBootstrapScriptTarget(req BootstrapRequest, cfg *CubeModel.ClusterConfigSection) (bootstrapScriptTarget, error) {
	if cfg == nil {
		return bootstrapScriptTarget{}, fmt.Errorf("clusterConfig required")
	}
	filter := licenseApplyHostnameFilter(req.TargetHostnames)
	for i := range cfg.Hosts {
		host := &cfg.Hosts[i]
		hostname := licenseApplySCVMHostname(host)
		if !licenseApplyTargetMatchesFilter(licenseApplyRoleSCVM, hostname, host, filter) {
			continue
		}
		target := strings.TrimSpace(host.Ablecube)
		if target == "" {
			return bootstrapScriptTarget{}, fmt.Errorf("%s hosts[].ablecube required", firstNonEmpty(host.Hostname, hostname))
		}
		return bootstrapScriptTarget{
			Role:     licenseApplyRoleSCVM,
			Hostname: hostname,
			Target:   target,
			Domain:   scvmDomainName,
		}, nil
	}
	if len(filter) > 0 {
		return bootstrapScriptTarget{}, fmt.Errorf("target_hostname not found")
	}
	return bootstrapScriptTarget{}, fmt.Errorf("scvm host not found")
}

func buildCCVMBootstrapScriptTarget(req BootstrapRequest, cfg *CubeModel.ClusterConfigSection) (bootstrapScriptTarget, error) {
	if cfg == nil {
		return bootstrapScriptTarget{}, fmt.Errorf("clusterConfig required")
	}
	filter := licenseApplyHostnameFilter(req.TargetHostnames)
	if len(filter) > 0 && !licenseApplyTargetMatchesFilter(licenseApplyRoleCCVM, licenseApplyRoleCCVM, nil, filter) {
		return bootstrapScriptTarget{}, fmt.Errorf("target_hostname not found")
	}
	if len(cfg.PCSCluster.HostnameList()) > 0 {
		startedTarget, err := waitCCVMSecondaryGuestAgentOnStartedTarget(cfg, deployRunBootstrapReadyTO)
		if err != nil {
			return bootstrapScriptTarget{}, err
		}
		return bootstrapScriptTarget{
			Role:     licenseApplyRoleCCVM,
			Hostname: firstNonEmpty(startedTarget.Hostname, startedTarget.PCSIP, licenseApplyRoleCCVM),
			Target:   firstNonEmpty(startedTarget.Target, "local"),
			Domain:   ccvmSnapName,
			Args:     ccvmBootstrapScriptArgs(),
		}, nil
	}
	execTarget, ok := selectPCSExecutionTarget(cfg)
	if !ok {
		return bootstrapScriptTarget{}, fmt.Errorf("ccvm execution target not found")
	}
	return bootstrapScriptTarget{
		Role:     licenseApplyRoleCCVM,
		Hostname: firstNonEmpty(execTarget.Hostname, execTarget.PCSHost, licenseApplyRoleCCVM),
		Target:   firstNonEmpty(execTarget.Target, "local"),
		Domain:   ccvmSnapName,
		Args:     ccvmBootstrapScriptArgs(),
	}, nil
}

func ccvmBootstrapScriptArgs() []string {
	if licenseType := currentLicenseTypeValue(); licenseType != "" {
		return []string{licenseType}
	}
	return nil
}

func defaultBootstrapScriptDomain(role string) string {
	switch role {
	case licenseApplyRoleSCVM:
		return scvmDomainName
	case licenseApplyRoleCCVM:
		return ccvmSnapName
	default:
		return ""
	}
}

func isBootstrapScriptLocalTarget(target bootstrapScriptTarget) bool {
	raw := strings.TrimSpace(target.Target)
	return raw == "" || strings.EqualFold(raw, "local") || isLocalTarget(raw)
}

func callBootstrapScriptRemote(target bootstrapScriptTarget) BootstrapScriptResult {
	req := BootstrapRequest{
		ScriptDomain:   target.Domain,
		ScriptArgs:     target.Args,
		ScriptHostname: target.Hostname,
		ScriptTarget:   target.Target,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return bootstrapScriptError(target, err.Error())
	}

	url := fmt.Sprintf("%s/api/v1/cube/%s/bootstrap", buildTargetURL(target.Target), target.Role)
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return bootstrapScriptError(target, err.Error())
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(bootstrapLocalHeader, "1")

	client := &http.Client{Timeout: bootstrapRemoteRequestTO}
	resp, err := client.Do(httpReq)
	if err != nil {
		return bootstrapScriptError(target, err.Error())
	}
	defer resp.Body.Close()

	var out BootstrapResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return bootstrapScriptError(target, err.Error())
	}
	if len(out.Script) == 0 {
		return bootstrapScriptError(target, firstNonEmpty(out.Message, "empty bootstrap script response"))
	}
	result := out.Script[0]
	if strings.TrimSpace(result.Role) == "" {
		result.Role = target.Role
	}
	if strings.TrimSpace(result.Hostname) == "" {
		result.Hostname = target.Hostname
	}
	if strings.TrimSpace(result.Target) == "" || strings.EqualFold(result.Target, "local") {
		result.Target = target.Target
	}
	if strings.TrimSpace(result.Domain) == "" {
		result.Domain = target.Domain
	}
	if result.Code == 0 {
		result.Code = out.Code
	}
	if result.Code == 0 {
		result.Code = resp.StatusCode
	}
	if result.Code != http.StatusOK && strings.TrimSpace(result.Message) == "" {
		result.Message = firstNonEmpty(out.Message, resp.Status)
	}
	return result
}

func runBootstrapScriptLocal(target bootstrapScriptTarget) BootstrapScriptResult {
	target.Domain = firstNonEmpty(target.Domain, defaultBootstrapScriptDomain(target.Role))
	target.Args = normalizeStringSlice(target.Args)
	if strings.TrimSpace(target.Domain) == "" {
		return bootstrapScriptError(target, "bootstrap script domain required")
	}
	if err := waitBootstrapGuestAgent(target.Domain, deployRunBootstrapReadyTO); err != nil {
		return bootstrapScriptError(target, err.Error())
	}
	output, err := runBootstrapGuestScript(target.Domain, target.Args, bootstrapScriptExecTO)
	if err != nil {
		result := bootstrapScriptError(target, err.Error())
		result.Output = output
		return result
	}
	return BootstrapScriptResult{
		Role:     target.Role,
		Hostname: target.Hostname,
		Target:   firstNonEmpty(target.Target, "local"),
		Domain:   target.Domain,
		Code:     http.StatusOK,
		Message:  "ok",
		Output:   output,
	}
}

func waitBootstrapGuestAgent(domain string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		_, timedOut, err := libvirtinfra.RunGuestAgentCommand(domain, libvirtinfra.GuestAgentCommandRequest{Execute: "guest-ping"}, bootstrapGuestCommandTO)
		if !timedOut && err == nil {
			return nil
		}
		if timedOut {
			lastErr = fmt.Errorf("%s guest-ping timed out", domain)
		} else {
			lastErr = err
		}
		if time.Now().Add(bootstrapGuestReadyInterval).After(deadline) {
			break
		}
		time.Sleep(bootstrapGuestReadyInterval)
	}
	return fmt.Errorf("%s guest agent not ready after %s: %w", domain, timeout, lastErr)
}

func runBootstrapGuestScript(domain string, args []string, timeout time.Duration) (string, error) {
	commandArgs := []string{
		"-lc",
		fmt.Sprintf("set -o pipefail; %s \"$@\" > %s 2>&1; rc=$?; tail -n 80 %s 2>/dev/null || true; exit $rc", bootstrapScriptPath, bootstrapGuestLogPath, bootstrapGuestLogPath),
		"bootstrap",
	}
	commandArgs = append(commandArgs, args...)
	req := libvirtinfra.GuestAgentCommandRequest{
		Execute: "guest-exec",
		Arguments: map[string]any{
			"path":           "/bin/bash",
			"arg":            commandArgs,
			"capture-output": true,
		},
	}
	resp, timedOut, err := libvirtinfra.RunGuestAgentCommand(domain, req, bootstrapGuestCommandTO)
	if timedOut {
		return "", fmt.Errorf("%s guest bootstrap exec timed out", domain)
	}
	if err != nil {
		return "", err
	}
	var execResp bootstrapGuestExecResponse
	if err := json.Unmarshal([]byte(resp), &execResp); err != nil {
		return "", err
	}
	if execResp.Return.PID <= 0 {
		return "", fmt.Errorf("%s guest bootstrap exec returned empty pid", domain)
	}

	deadline := time.Now().Add(timeout)
	for {
		status, err := bootstrapGuestExecStatus(domain, execResp.Return.PID)
		if err != nil {
			return "", err
		}
		if status.Return.Exited {
			output := firstNonEmpty(
				bootstrapDecodeGuestOutput(status.Return.ErrData),
				bootstrapDecodeGuestOutput(status.Return.OutData),
			)
			if status.Return.ExitCode == 0 {
				return output, nil
			}
			return output, fmt.Errorf("%s bootstrap script exited with code %d: %s", domain, status.Return.ExitCode, firstNonEmpty(output, "check "+bootstrapGuestLogPath))
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("%s bootstrap script timed out after %s", domain, timeout)
		}
		time.Sleep(bootstrapGuestPollInterval)
	}
}

func bootstrapGuestExecStatus(domain string, pid int) (bootstrapGuestExecStatusResponse, error) {
	req := libvirtinfra.GuestAgentCommandRequest{
		Execute: "guest-exec-status",
		Arguments: map[string]any{
			"pid": pid,
		},
	}
	resp, timedOut, err := libvirtinfra.RunGuestAgentCommand(domain, req, bootstrapGuestCommandTO)
	if timedOut {
		return bootstrapGuestExecStatusResponse{}, fmt.Errorf("%s guest bootstrap status timed out", domain)
	}
	if err != nil {
		return bootstrapGuestExecStatusResponse{}, err
	}
	var out bootstrapGuestExecStatusResponse
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		return bootstrapGuestExecStatusResponse{}, err
	}
	return out, nil
}

func bootstrapDecodeGuestOutput(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return value
	}
	return strings.TrimSpace(string(decoded))
}

func bootstrapScriptError(target bootstrapScriptTarget, message string) BootstrapScriptResult {
	return BootstrapScriptResult{
		Role:     target.Role,
		Hostname: target.Hostname,
		Target:   firstNonEmpty(target.Target, "local"),
		Domain:   target.Domain,
		Code:     http.StatusInternalServerError,
		Message:  strings.TrimSpace(message),
	}
}

func bootstrapHealthResult(target licenseApplyTarget, health map[string]any) BootstrapHealthResult {
	return BootstrapHealthResult{
		Role:     target.Role,
		Hostname: target.Hostname,
		Target:   firstNonEmpty(mapStringValue(health, "target"), target.Target),
		Code:     mapIntValue(health, "code"),
		Message:  mapStringValue(health, "message"),
		Attempts: mapIntValue(health, "attempts"),
	}
}

func mapStringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	switch value := values[key].(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

func mapIntValue(values map[string]any, key string) int {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func bootstrapResponseToDeployOutcome(resp BootstrapResponse) (deployRunStepOutcome, error) {
	if resp.Code == http.StatusOK {
		return deployRunSucceeded(firstNonEmpty(resp.Message, resp.Role+"_bootstrap success"), resp), nil
	}
	return deployRunStepOutcome{Output: resp}, fmt.Errorf(firstNonEmpty(resp.Message, resp.Role+"_bootstrap failed"))
}

func statusCodeFromBootstrapResponse(resp BootstrapResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
