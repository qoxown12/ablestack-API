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

type MultipathSyncRequest = CubeModel.MultipathSyncRequest
type MultipathSyncResponse = CubeModel.MultipathSyncResponse
type MultipathSyncTargetResult = CubeModel.MultipathSyncTargetResult
type MultipathSyncStepResult = CubeModel.MultipathSyncStepResult

const (
	multipathSyncLocalHeader = "X-Cube-Multipath-Sync-Local"
	multipathSyncRemoteTO    = 5 * time.Minute
	multipathSyncCommandTO   = 2 * time.Minute
	multipathSyncShortTO     = 30 * time.Second
	multipathConfigDir       = "/etc/multipath"
	multipathBindingsPath    = "/etc/multipath/bindings"
	multipathWWIDsPath       = "/etc/multipath/wwids"
)

// MultipathSync godoc
//
//	@Summary		Multipath Sync
//	@Description	cluster.json hosts[].ablecube 대상 API를 호출해 SCSI rescan 또는 multipath bindings/wwids 동기화를 수행합니다. SSH/SCP는 사용하지 않습니다.
//	@Tags			Cube-Multipath
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.MultipathSyncRequest	false	"multipath sync request"
//	@Success		200	{object}	CubeModel.MultipathSyncResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/multipath/sync [post]
func MultipathSync(context *gin.Context) {
	req := MultipathSyncRequest{Action: "sync"}
	if context.Request.Body != nil && context.Request.ContentLength != 0 {
		if err := context.ShouldBindJSON(&req); err != nil {
			context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
				ErrCode: http.StatusBadRequest,
				Message: "invalid request",
			})
			return
		}
	}
	if err := normalizeMultipathSyncRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	if isMultipathSyncLocalRequest(context) {
		resp := runMultipathSyncLocal(req, "local", "")
		context.JSON(statusCodeFromMultipathSyncResponse(resp), resp)
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

	resp := runMultipathSync(req, cfg)
	context.JSON(statusCodeFromMultipathSyncResponse(resp), resp)
}

func normalizeMultipathSyncRequest(req *MultipathSyncRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "", "sync":
		req.Action = "sync"
	case "rescan", "scan":
		req.Action = "rescan"
	default:
		return fmt.Errorf("unsupported action")
	}
	req.Targets = normalizeStringSlice(req.Targets)
	req.TargetHostnames = normalizeStringSlice(req.TargetHostnames)
	return nil
}

func isMultipathSyncLocalRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(multipathSyncLocalHeader)), "1")
}

func runMultipathSync(req MultipathSyncRequest, cfg *CubeModel.ClusterConfigSection) MultipathSyncResponse {
	targets := buildMultipathSyncTargets(cfg, req)
	if len(targets) == 0 {
		return multipathSyncError(req, "fanout", "hosts[].ablecube required", nil, nil)
	}

	if req.Action == "sync" {
		source, err := loadMultipathSyncSourceFiles()
		if err != nil {
			return multipathSyncError(req, "local", err.Error(), nil, nil)
		}
		req.Bindings = source.Bindings
		req.WWIDs = source.WWIDs
		req.SourceProvided = true
	}

	client := &http.Client{Timeout: multipathSyncRemoteTO}
	results := make([]MultipathSyncTargetResult, 0, len(targets))
	for _, target := range targets {
		results = append(results, runMultipathSyncOnTarget(client, target, req, cfg))
	}
	if err := firstMultipathSyncResultError(results); err != nil {
		return multipathSyncError(req, "fanout", err.Error(), nil, results)
	}
	return MultipathSyncResponse{
		Code:    http.StatusOK,
		Message: "ok",
		Action:  req.Action,
		Target:  "fanout",
		Results: results,
	}
}

func buildMultipathSyncTargets(cfg *CubeModel.ClusterConfigSection, req MultipathSyncRequest) []gfsManageTarget {
	if cfg == nil {
		return nil
	}
	if len(req.Targets) > 0 {
		return multipathSyncTargetsFromIPs(cfg, req.Targets)
	}
	if len(req.TargetHostnames) > 0 {
		return multipathSyncTargetsFromHostnames(cfg, req.TargetHostnames)
	}
	return buildGFSManageTargets(cfg)
}

func multipathSyncTargetsFromIPs(cfg *CubeModel.ClusterConfigSection, targets []string) []gfsManageTarget {
	hostnames := map[string]string{}
	for _, host := range cfg.Hosts {
		target := strings.TrimSpace(host.Ablecube)
		if target != "" {
			hostnames[target] = strings.TrimSpace(host.Hostname)
		}
	}
	out := make([]gfsManageTarget, 0, len(targets))
	for _, target := range normalizeStringSlice(targets) {
		out = append(out, gfsManageTarget{Hostname: hostnames[target], Target: target})
	}
	return out
}

func multipathSyncTargetsFromHostnames(cfg *CubeModel.ClusterConfigSection, hostnames []string) []gfsManageTarget {
	wanted := map[string]struct{}{}
	for _, hostname := range normalizeStringSlice(hostnames) {
		wanted[strings.ToLower(hostname)] = struct{}{}
	}
	out := make([]gfsManageTarget, 0, len(wanted))
	for _, host := range cfg.Hosts {
		hostname := strings.TrimSpace(host.Hostname)
		target := strings.TrimSpace(host.Ablecube)
		if hostname == "" || target == "" {
			continue
		}
		if _, ok := wanted[strings.ToLower(hostname)]; ok {
			out = append(out, gfsManageTarget{Hostname: hostname, Target: target})
		}
	}
	return out
}

func runMultipathSyncOnTarget(client *http.Client, target gfsManageTarget, req MultipathSyncRequest, cfg *CubeModel.ClusterConfigSection) MultipathSyncTargetResult {
	if target.Target == "" || isLocalTarget(target.Target) || isGFSManageLocalHostname(target.Hostname) {
		resp := runMultipathSyncLocal(req, firstNonEmpty(target.Target, "local"), target.Hostname)
		return multipathSyncTargetResult(target, resp)
	}
	resp, err := callMultipathSyncRemote(client, target, req)
	if err != nil {
		return MultipathSyncTargetResult{
			Hostname: target.Hostname,
			Target:   target.Target,
			Code:     http.StatusInternalServerError,
			Message:  err.Error(),
		}
	}
	return multipathSyncTargetResult(target, resp)
}

func callMultipathSyncRemote(client *http.Client, target gfsManageTarget, req MultipathSyncRequest) (MultipathSyncResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return MultipathSyncResponse{}, err
	}
	url := fmt.Sprintf("%s/api/v1/cube/multipath/sync", buildTargetURL(target.Target))
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return MultipathSyncResponse{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(multipathSyncLocalHeader, "1")

	resp, err := client.Do(httpReq)
	if err != nil {
		return MultipathSyncResponse{}, err
	}
	defer resp.Body.Close()

	var out MultipathSyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		if resp.StatusCode >= 300 {
			return MultipathSyncResponse{}, fmt.Errorf("multipath sync failed: %s", resp.Status)
		}
		return MultipathSyncResponse{}, err
	}
	if out.Code == 0 {
		out.Code = resp.StatusCode
	}
	if strings.TrimSpace(out.Target) == "" || strings.EqualFold(strings.TrimSpace(out.Target), "local") {
		out.Target = target.Target
	}
	if strings.TrimSpace(out.Action) == "" {
		out.Action = req.Action
	}
	return out, nil
}

func runMultipathSyncLocal(req MultipathSyncRequest, target string, hostname string) MultipathSyncResponse {
	steps := make([]MultipathSyncStepResult, 0, 8)
	addStep := func(name string, output string, critical bool, err error) bool {
		step := multipathSyncStep(name, output, err)
		steps = append(steps, step)
		return !critical || err == nil
	}

	if !addStep("rescan_scsi", "", true, rescanMultipathSCSIHosts()) {
		return multipathSyncLocalError(req, target, steps)
	}
	if req.Action == "rescan" {
		return multipathSyncLocalOK(req, target, steps)
	}

	if !req.SourceProvided {
		steps = append(steps, multipathSyncStep("source_files", "", fmt.Errorf("multipath source bindings/wwids were not provided")))
		return multipathSyncLocalError(req, target, steps)
	}
	addCommandStep := func(name string, critical bool, command string, args ...string) bool {
		out, err := runMultipathSyncCommand(command, args...)
		return addStep(name, out, critical, err)
	}
	if !addCommandStep("mpathconf_enable", true, "mpathconf", "--enable") {
		return multipathSyncLocalError(req, target, steps)
	}
	if !addCommandStep("multipathd_enable", true, "systemctl", "enable", "--now", "multipathd") {
		return multipathSyncLocalError(req, target, steps)
	}
	time.Sleep(time.Second)
	addCommandStep("multipathd_socket_stop", false, "systemctl", "stop", "multipathd.socket")
	addCommandStep("multipath_flush", false, "multipath", "-F")
	if !addStep("write_source_files", "", true, writeMultipathSyncSourceFiles(req)) {
		return multipathSyncLocalError(req, target, steps)
	}
	if !addCommandStep("multipathd_restart", true, "systemctl", "restart", "multipathd") {
		return multipathSyncLocalError(req, target, steps)
	}
	return multipathSyncLocalOK(req, target, steps)
}

func multipathSyncStep(name string, output string, err error) MultipathSyncStepResult {
	step := MultipathSyncStepResult{Name: name, Status: "succeeded", Message: "ok", Output: strings.TrimSpace(output)}
	if err != nil {
		step.Status = "failed"
		step.Message = err.Error()
	}
	return step
}

func runMultipathSyncCommand(command string, args ...string) (string, error) {
	out, timedOut, err := runCommandOutputWithEnv(command, multipathSyncCommandTO, gfsManageCommandEnv(), args...)
	if timedOut {
		return out, fmt.Errorf("%s timed out", command)
	}
	if err != nil {
		return out, fmt.Errorf("%s failed: %s", command, strings.TrimSpace(firstNonEmpty(out, err.Error())))
	}
	return out, nil
}

func rescanMultipathSCSIHosts() error {
	paths, err := filepath.Glob("/sys/class/scsi_host/*/scan")
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("scsi host scan path not found")
	}
	var failures []string
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("- - -\n"), 0200); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", path, err.Error()))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

type multipathSyncSourceFiles struct {
	Bindings string
	WWIDs    string
}

func loadMultipathSyncSourceFiles() (multipathSyncSourceFiles, error) {
	bindings, err := os.ReadFile(multipathBindingsPath)
	if err != nil {
		return multipathSyncSourceFiles{}, fmt.Errorf("failed to read %s: %w", multipathBindingsPath, err)
	}
	wwids, err := os.ReadFile(multipathWWIDsPath)
	if err != nil {
		return multipathSyncSourceFiles{}, fmt.Errorf("failed to read %s: %w", multipathWWIDsPath, err)
	}
	return multipathSyncSourceFiles{Bindings: string(bindings), WWIDs: string(wwids)}, nil
}

func writeMultipathSyncSourceFiles(req MultipathSyncRequest) error {
	if err := os.MkdirAll(multipathConfigDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(multipathBindingsPath, []byte(req.Bindings), 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", multipathBindingsPath, err)
	}
	if err := os.WriteFile(multipathWWIDsPath, []byte(req.WWIDs), 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", multipathWWIDsPath, err)
	}
	return nil
}

func multipathSyncLocalOK(req MultipathSyncRequest, target string, steps []MultipathSyncStepResult) MultipathSyncResponse {
	return MultipathSyncResponse{
		Code:    http.StatusOK,
		Message: "ok",
		Action:  req.Action,
		Target:  firstNonEmpty(target, "local"),
		Steps:   steps,
	}
}

func multipathSyncLocalError(req MultipathSyncRequest, target string, steps []MultipathSyncStepResult) MultipathSyncResponse {
	return multipathSyncError(req, firstNonEmpty(target, "local"), firstFailedMultipathSyncStepMessage(steps), steps, nil)
}

func multipathSyncError(req MultipathSyncRequest, target string, message string, steps []MultipathSyncStepResult, results []MultipathSyncTargetResult) MultipathSyncResponse {
	return MultipathSyncResponse{
		Code:    http.StatusInternalServerError,
		Message: firstNonEmpty(message, "multipath sync failed"),
		Action:  req.Action,
		Target:  target,
		Steps:   steps,
		Results: results,
	}
}

func multipathSyncTargetResult(target gfsManageTarget, resp MultipathSyncResponse) MultipathSyncTargetResult {
	code := resp.Code
	if code == 0 {
		code = http.StatusOK
	}
	return MultipathSyncTargetResult{
		Hostname: firstNonEmpty(target.Hostname, resp.Target),
		Target:   firstNonEmpty(target.Target, resp.Target),
		Code:     code,
		Message:  firstNonEmpty(resp.Message, "ok"),
		Steps:    resp.Steps,
	}
}

func firstMultipathSyncResultError(results []MultipathSyncTargetResult) error {
	for _, result := range results {
		if result.Code != http.StatusOK {
			return fmt.Errorf("%s: %s", firstNonEmpty(result.Hostname, result.Target), firstNonEmpty(result.Message, "failed"))
		}
	}
	return nil
}

func firstFailedMultipathSyncStepMessage(steps []MultipathSyncStepResult) string {
	for _, step := range steps {
		if step.Status == "failed" {
			return step.Name + ": " + step.Message
		}
	}
	return "multipath sync failed"
}

func statusCodeFromMultipathSyncResponse(resp MultipathSyncResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
