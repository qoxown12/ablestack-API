package cube

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type GlueConfigUpdateRequest = CubeModel.GlueConfigUpdateRequest
type GlueConfigUpdateResponse = CubeModel.GlueConfigUpdateResponse
type GlueConfigUpdateTargetResult = CubeModel.GlueConfigUpdateTargetResult

const (
	glueConfigCephDir          = "/etc/ceph"
	glueConfigCephConf         = "/etc/ceph/ceph.conf"
	glueConfigCephTempConf     = "/etc/ceph/ceph_temp.conf"
	glueConfigCommandTimeout   = 30 * time.Second
	glueConfigCopyTimeout      = 3 * time.Minute
	glueConfigHealthTimeout    = 5 * time.Second
	glueConfigUpdateSuccessVal = "Glue Config All cube host and scvm update Success"
)

type glueConfigTarget struct {
	Role     string
	Hostname string
	Target   string
}

// UpdateGlueConfig godoc
//
//	@Summary		Update Glue Config
//	@Description	/etc/ceph 설정을 생성하거나 pcsCluster.hostnameN에서 가져온 뒤 cluster.json hosts[].ablecube/scvmMngt 대상으로 배포합니다. 요청 body 없이 호출합니다.
//	@Tags			Cube-GlueCluster
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	CubeModel.GlueConfigUpdateResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/glue/config/update [post]
func UpdateGlueConfig(context *gin.Context) {
	req := GlueConfigUpdateRequest{Action: "update"}
	if context.Request.ContentLength > 0 {
		if err := context.ShouldBindJSON(&req); err != nil {
			context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
				ErrCode: http.StatusBadRequest,
				Message: "invalid request",
			})
			return
		}
	}

	if err := normalizeGlueConfigUpdateRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
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

	resp := runGlueConfigUpdate(req, cfg)
	context.JSON(statusCodeFromGlueConfigUpdateResponse(resp), resp)
}

func normalizeGlueConfigUpdateRequest(req *GlueConfigUpdateRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "", "update":
		req.Action = "update"
	default:
		return fmt.Errorf("unsupported action")
	}
	return nil
}

func runGlueConfigUpdate(req GlueConfigUpdateRequest, cfg *CubeModel.ClusterConfigSection) GlueConfigUpdateResponse {
	if cfg == nil {
		return glueConfigUpdateError(req, "", "cluster config required", nil)
	}

	source, err := prepareGlueConfigSourceFiles(cfg)
	if err != nil {
		return glueConfigUpdateError(req, source, err.Error(), nil)
	}

	targets := buildGlueConfigTargets(cfg)
	if len(targets) == 0 {
		return glueConfigUpdateError(req, source, "hosts[].ablecube or hosts[].scvmMngt required", nil)
	}

	healthResults := checkGlueConfigHealthTargets(targets)
	if hasGlueConfigFailure(healthResults) {
		return glueConfigUpdateError(req, source, "health check failed", healthResults)
	}

	copyResults := copyGlueConfigToTargets(targets)
	results := append(healthResults, copyResults...)
	if hasGlueConfigFailure(copyResults) {
		return glueConfigUpdateError(req, source, "Glue Config copy Failed", results)
	}

	return GlueConfigUpdateResponse{
		Code:    http.StatusOK,
		Val:     glueConfigUpdateSuccessVal,
		Message: glueConfigUpdateSuccessVal,
		Action:  req.Action,
		Source:  source,
		Results: results,
	}
}

func prepareGlueConfigSourceFiles(cfg *CubeModel.ClusterConfigSection) (string, error) {
	generateErr := generateGlueCephConfig()
	_, fileErr := listGlueConfigFiles()
	if generateErr == nil && fileErr == nil {
		return "local", nil
	}

	source, fallbackErr := copyGlueConfigFromPCSClusterHost(cfg)
	if fallbackErr != nil {
		return source, fmt.Errorf("local ceph config prepare failed: %s; fallback failed: %s", joinGlueConfigErrors(generateErr, fileErr), fallbackErr)
	}
	if _, err := listGlueConfigFiles(); err != nil {
		return source, err
	}
	return source, nil
}

func generateGlueCephConfig() error {
	if err := os.MkdirAll(glueConfigCephDir, 0755); err != nil {
		return err
	}

	stdout, stderr, timedOut, err := runGlueConfigStdout("ceph", glueConfigCommandTimeout, "config", "generate-minimal-conf")
	if timedOut {
		return fmt.Errorf("ceph config generate-minimal-conf timed out after %s", glueConfigCommandTimeout)
	}
	if err != nil {
		return fmt.Errorf("ceph config generate-minimal-conf failed: %s", firstNonEmpty(strings.TrimSpace(stderr), err.Error()))
	}
	if strings.TrimSpace(stdout) == "" {
		return fmt.Errorf("ceph config generate-minimal-conf returned empty output")
	}
	if err := os.WriteFile(glueConfigCephTempConf, []byte(stdout), 0644); err != nil {
		return err
	}
	return os.Rename(glueConfigCephTempConf, glueConfigCephConf)
}

func copyGlueConfigFromPCSClusterHost(cfg *CubeModel.ClusterConfigSection) (string, error) {
	pcsHosts := cfg.PCSCluster.HostnameList()
	if len(pcsHosts) == 0 {
		return "", fmt.Errorf("pcsCluster hostname required")
	}
	if err := os.MkdirAll(glueConfigCephDir, 0755); err != nil {
		return "", err
	}

	var lastSource string
	var lastErr error
	for _, pcsHost := range pcsHosts {
		sourceTarget := pcsHost
		if host, ok := findPCSClusterHost(cfg, pcsHost); ok {
			sourceTarget = firstNonEmpty(host.Ablecube, host.AblecubePn, host.Hostname, pcsHost)
		}
		source := fmt.Sprintf("pcsCluster:%s", sourceTarget)
		lastSource = source
		if isLocalTarget(sourceTarget) || isGFSManageLocalHostname(sourceTarget) {
			lastErr = fmt.Errorf("fallback source points to local host")
			continue
		}

		remotePath := fmt.Sprintf("root@%s:/etc/ceph/*", sourceTarget)
		out, timedOut, err := runCommandOutputWithEnv(
			"scp",
			glueConfigCopyTimeout,
			glueConfigCommandEnv(),
			"-q",
			"-o",
			"StrictHostKeyChecking=no",
			"-o",
			"ConnectTimeout=5",
			remotePath,
			glueConfigCephDir+"/",
		)
		if timedOut {
			lastErr = fmt.Errorf("scp from %s timed out after %s", sourceTarget, glueConfigCopyTimeout)
			continue
		}
		if err != nil {
			lastErr = fmt.Errorf("scp from %s failed: %s", sourceTarget, firstNonEmpty(strings.TrimSpace(out), err.Error()))
			continue
		}
		return source, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("pcsCluster host not found")
	}
	return lastSource, lastErr
}

func listGlueConfigFiles() ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(glueConfigCephDir, "*"))
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		files = append(files, path)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("%s files not found", glueConfigCephDir)
	}
	return files, nil
}

func buildGlueConfigTargets(cfg *CubeModel.ClusterConfigSection) []glueConfigTarget {
	if cfg == nil {
		return nil
	}
	targets := make([]glueConfigTarget, 0, len(cfg.Hosts)*2)
	seen := map[string]struct{}{}
	add := func(role string, hostname string, target string) {
		target = strings.TrimSpace(target)
		if target == "" {
			return
		}
		key := role + "|" + target
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, glueConfigTarget{
			Role:     role,
			Hostname: strings.TrimSpace(hostname),
			Target:   target,
		})
	}
	for _, host := range cfg.Hosts {
		add("ablecube", host.Hostname, host.Ablecube)
		add("scvm", host.Hostname, firstNonEmpty(host.ScvmMngt, host.Scvm))
	}
	return targets
}

func checkGlueConfigHealthTargets(targets []glueConfigTarget) []GlueConfigUpdateTargetResult {
	client := &http.Client{Timeout: glueConfigHealthTimeout}
	results := make([]GlueConfigUpdateTargetResult, 0, len(targets))
	for _, target := range targets {
		result := glueConfigTargetResult("health", target, http.StatusOK, "ok")
		if !isLocalTarget(target.Target) && !isGFSManageLocalHostname(target.Hostname) {
			if err := callHealthTarget(client, target.Target); err != nil {
				result.Code = http.StatusInternalServerError
				result.Message = err.Error()
			}
		}
		results = append(results, result)
	}
	return results
}

func copyGlueConfigToTargets(targets []glueConfigTarget) []GlueConfigUpdateTargetResult {
	files, err := listGlueConfigFiles()
	if err != nil {
		results := make([]GlueConfigUpdateTargetResult, 0, len(targets))
		for _, target := range targets {
			results = append(results, glueConfigTargetResult("copy", target, http.StatusInternalServerError, err.Error()))
		}
		return results
	}

	results := make([]GlueConfigUpdateTargetResult, 0, len(targets))
	for _, target := range targets {
		if isLocalTarget(target.Target) || isGFSManageLocalHostname(target.Hostname) {
			results = append(results, glueConfigTargetResult("copy", target, http.StatusOK, "local target already updated"))
			continue
		}

		args := []string{
			"-q",
			"-o",
			"StrictHostKeyChecking=no",
			"-o",
			"ConnectTimeout=5",
		}
		args = append(args, files...)
		args = append(args, fmt.Sprintf("root@%s:/etc/ceph/", target.Target))
		out, timedOut, err := runCommandOutputWithEnv("scp", glueConfigCopyTimeout, glueConfigCommandEnv(), args...)
		if timedOut {
			results = append(results, glueConfigTargetResult("copy", target, http.StatusInternalServerError, fmt.Sprintf("scp timed out after %s", glueConfigCopyTimeout)))
			continue
		}
		if err != nil {
			results = append(results, glueConfigTargetResult("copy", target, http.StatusInternalServerError, fmt.Sprintf("scp failed: %s", firstNonEmpty(strings.TrimSpace(out), err.Error()))))
			continue
		}
		results = append(results, glueConfigTargetResult("copy", target, http.StatusOK, "ok"))
	}
	return results
}

func runGlueConfigStdout(command string, timeout time.Duration, args ...string) (string, string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = glueConfigCommandEnv()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return stdout.String(), stderr.String(), true, ctx.Err()
	}
	return stdout.String(), stderr.String(), false, err
}

func glueConfigCommandEnv() []string {
	return append([]string{"LANG=en_US.utf-8", "LANGUAGE=en"}, os.Environ()...)
}

func glueConfigTargetResult(step string, target glueConfigTarget, code int, message string) GlueConfigUpdateTargetResult {
	return GlueConfigUpdateTargetResult{
		Step:     step,
		Role:     target.Role,
		Hostname: target.Hostname,
		Target:   target.Target,
		Code:     code,
		Message:  message,
	}
}

func hasGlueConfigFailure(results []GlueConfigUpdateTargetResult) bool {
	for _, result := range results {
		if result.Code != http.StatusOK {
			return true
		}
	}
	return false
}

func joinGlueConfigErrors(errs ...error) string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		parts = append(parts, err.Error())
	}
	if len(parts) == 0 {
		return "unknown error"
	}
	return strings.Join(parts, "; ")
}

func glueConfigUpdateError(req GlueConfigUpdateRequest, source string, message string, results []GlueConfigUpdateTargetResult) GlueConfigUpdateResponse {
	return GlueConfigUpdateResponse{
		Code:    http.StatusInternalServerError,
		Val:     message,
		Message: message,
		Action:  req.Action,
		Source:  source,
		Results: results,
	}
}

func statusCodeFromGlueConfigUpdateResponse(resp GlueConfigUpdateResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
