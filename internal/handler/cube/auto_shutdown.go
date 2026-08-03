package cube

import (
	"bufio"
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

type AutoShutdownRequest = CubeModel.AutoShutdownRequest
type AutoShutdownResponse = CubeModel.AutoShutdownResponse
type AutoShutdownTargetResult = CubeModel.AutoShutdownTargetResult
type AutoShutdownMountResult = CubeModel.AutoShutdownMountResult

const (
	autoShutdownLocalHeader    = "X-Cube-Auto-Shutdown-Local"
	autoShutdownRequestTimeout = 3 * time.Minute
	autoShutdownCommandTimeout = 30 * time.Second
	autoShutdownFSTabPath      = "/etc/fstab"
	autoShutdownMountsPath     = "/proc/self/mounts"
)

type autoShutdownTarget struct {
	Hostname string
	Target   string
}

type autoShutdownFSTabEntry struct {
	Source     string
	MountPoint string
}

var autoShutdownProtectedMounts = map[string]struct{}{
	"/":         {},
	"/boot":     {},
	"/boot/efi": {},
	"/usr":      {},
	"/var":      {},
	"/var/log":  {},
}

// AutoShutdown godoc
//
//	@Summary		Auto Shutdown
//	@Description	전체 호스트 정상 종료 절차를 수행합니다. 사용 가능한 action: check_mount, stop_scvms, shutdown_hosts. SSH 대신 ablecube API fan-out을 사용합니다.
//	@Tags			Cube-AutoShutdown
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.AutoShutdownRequest	true	"auto shutdown request"
//	@Success		200	{object}	CubeModel.AutoShutdownResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/auto-shutdown [post]
func AutoShutdown(context *gin.Context) {
	var req AutoShutdownRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := normalizeAutoShutdownRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	if isAutoShutdownLocalRequest(context) {
		resp := runAutoShutdownLocal(req, nil)
		context.JSON(statusCodeFromAutoShutdownResponse(resp), resp)
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

	resp := runAutoShutdown(req, cfg)
	context.JSON(statusCodeFromAutoShutdownResponse(resp), resp)
}

func normalizeAutoShutdownRequest(req *AutoShutdownRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "check_mount", "check-mount":
		req.Action = "check_mount"
	case "stop_scvms", "stop-scvms":
		req.Action = "stop_scvms"
	case "shutdown_hosts", "shutdown-hosts":
		req.Action = "shutdown_hosts"
	default:
		return fmt.Errorf("unsupported action")
	}
	return nil
}

func isAutoShutdownLocalRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(autoShutdownLocalHeader)), "1")
}

func runAutoShutdown(req AutoShutdownRequest, cfg *CubeModel.ClusterConfigSection) AutoShutdownResponse {
	targets := orderAutoShutdownTargets(buildAutoShutdownTargets(cfg))
	if len(targets) == 0 {
		return autoShutdownError(req, autoShutdownRetName(req.Action), "hosts[].ablecube required", nil)
	}

	switch req.Action {
	case "check_mount", "shutdown_hosts":
		return runAutoShutdownFanout(req, targets)
	case "stop_scvms":
		return runAutoShutdownStopSCVMs(req, cfg, targets)
	default:
		return autoShutdownError(req, autoShutdownRetName(req.Action), "unsupported action", nil)
	}
}

func runAutoShutdownFanout(req AutoShutdownRequest, targets []autoShutdownTarget) AutoShutdownResponse {
	client := &http.Client{Timeout: autoShutdownRequestTimeout}
	results := make([]AutoShutdownTargetResult, 0, len(targets))
	for _, target := range targets {
		var resp AutoShutdownResponse
		var err error
		if isLocalTarget(target.Target) || isGFSManageLocalHostname(target.Hostname) {
			resp = runAutoShutdownLocal(req, nil)
		} else {
			resp, err = callAutoShutdownRemote(client, target, req)
		}
		if err != nil {
			results = append(results, autoShutdownTargetError(req, target, autoShutdownRetName(req.Action), err.Error()))
			continue
		}
		results = append(results, autoShutdownResultFromResponse(req, target, resp))
	}
	return autoShutdownResponseFromResults(req, autoShutdownRetName(req.Action), results)
}

func runAutoShutdownStopSCVMs(req AutoShutdownRequest, cfg *CubeModel.ClusterConfigSection, targets []autoShutdownTarget) AutoShutdownResponse {
	scvmReq := SCVMUpdateRequest{Action: "stop"}
	results := make([]AutoShutdownTargetResult, 0, len(targets))
	for _, target := range targets {
		var resp SCVMUpdateResponse
		var err error
		if isLocalTarget(target.Target) || isGFSManageLocalHostname(target.Hostname) {
			resp = runSCVMUpdateLocal(cfg, scvmReq)
		} else {
			resp, err = callSCVMUpdateRemote(target.Target, scvmReq)
		}
		if err != nil {
			results = append(results, autoShutdownTargetError(req, target, "Storage Center VM Stop", err.Error()))
			continue
		}
		results = append(results, AutoShutdownTargetResult{
			Hostname: target.Hostname,
			Target:   firstNonEmpty(resp.Target, target.Target),
			Code:     resp.Code,
			Val:      resp.Val,
			RetName:  firstNonEmpty(resp.RetName, "Storage Center VM Stop"),
			Message:  resp.Message,
			Action:   req.Action,
		})
	}
	return autoShutdownResponseFromResults(req, "Storage Center VM Stop", results)
}

func runAutoShutdownLocal(req AutoShutdownRequest, cfg *CubeModel.ClusterConfigSection) AutoShutdownResponse {
	target := resolveLocalSCVMUpdateTarget(cfg)
	switch req.Action {
	case "check_mount":
		mounts, err := checkAutoShutdownMountsLocal()
		if err != nil {
			return autoShutdownLocalError(req, target, "Umount Volume", err.Error(), mounts)
		}
		return AutoShutdownResponse{
			Code:    http.StatusOK,
			Val:     true,
			RetName: "Umount Volume",
			Message: "ok",
			Action:  req.Action,
			Target:  target,
			Mounts:  mounts,
		}
	case "stop_scvms":
		return autoShutdownResponseFromSCVM(req, runSCVMUpdateLocal(cfg, SCVMUpdateRequest{Action: "stop"}))
	case "shutdown_hosts":
		if err := shutdownAutoShutdownHostLocal(); err != nil {
			return autoShutdownLocalError(req, target, "Hosts Shutdown", err.Error(), nil)
		}
		return AutoShutdownResponse{
			Code:    http.StatusOK,
			Val:     true,
			RetName: "Hosts Shutdown",
			Message: "ok",
			Action:  req.Action,
			Target:  target,
		}
	default:
		return autoShutdownLocalError(req, target, autoShutdownRetName(req.Action), "unsupported action", nil)
	}
}

func checkAutoShutdownMountsLocal() ([]AutoShutdownMountResult, error) {
	entries, err := readAutoShutdownUUIDFSTabEntries(autoShutdownFSTabPath)
	if err != nil {
		return nil, err
	}
	mounted, err := readAutoShutdownMountedPoints(autoShutdownMountsPath)
	if err != nil {
		return nil, err
	}

	results := make([]AutoShutdownMountResult, 0, len(entries))
	for _, entry := range entries {
		result := AutoShutdownMountResult{
			Source:     entry.Source,
			MountPoint: entry.MountPoint,
			Status:     "not-mounted",
			Message:    "ok",
		}
		if _, protected := autoShutdownProtectedMounts[entry.MountPoint]; protected {
			result.Status = "skipped"
			result.Message = "protected system mount"
			results = append(results, result)
			continue
		}
		if !mounted[entry.MountPoint] {
			results = append(results, result)
			continue
		}
		if _, err := runAutoShutdownCommand(autoShutdownCommandTimeout, "umount", entry.MountPoint); err != nil {
			result.Status = "failed"
			result.Message = err.Error()
			results = append(results, result)
			return results, err
		}
		result.Status = "unmounted"
		results = append(results, result)
	}
	return results, nil
}

func readAutoShutdownUUIDFSTabEntries(path string) ([]autoShutdownFSTabEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	entries := make([]autoShutdownFSTabEntry, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "UUID") {
			continue
		}
		entries = append(entries, autoShutdownFSTabEntry{
			Source:     fields[0],
			MountPoint: unescapeAutoShutdownMountPath(fields[1]),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func readAutoShutdownMountedPoints(path string) (map[string]bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	mounted := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		mounted[unescapeAutoShutdownMountPath(fields[1])] = true
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return mounted, nil
}

func shutdownAutoShutdownHostLocal() error {
	_, err := runAutoShutdownCommand(autoShutdownCommandTimeout, "shutdown", "-h", "-t", "5")
	return err
}

func runAutoShutdownCommand(timeout time.Duration, command string, args ...string) (string, error) {
	out, timedOut, err := runCommandOutputWithEnv(command, timeout, autoShutdownCommandEnv(), args...)
	if timedOut {
		return out, fmt.Errorf("%s %s timed out after %s", command, strings.Join(args, " "), timeout)
	}
	if err != nil {
		return out, fmt.Errorf("%s %s failed: %s", command, strings.Join(args, " "), firstNonEmpty(strings.TrimSpace(out), err.Error()))
	}
	return out, nil
}

func autoShutdownCommandEnv() []string {
	return append([]string{"LANG=en_US.utf-8", "LANGUAGE=en"}, os.Environ()...)
}

func buildAutoShutdownTargets(cfg *CubeModel.ClusterConfigSection) []autoShutdownTarget {
	if cfg == nil {
		return nil
	}
	seen := map[string]struct{}{}
	targets := make([]autoShutdownTarget, 0, len(cfg.Hosts))
	for _, host := range cfg.Hosts {
		target := strings.TrimSpace(host.Ablecube)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, autoShutdownTarget{
			Hostname: strings.TrimSpace(host.Hostname),
			Target:   target,
		})
	}
	return targets
}

func orderAutoShutdownTargets(targets []autoShutdownTarget) []autoShutdownTarget {
	remoteTargets := make([]autoShutdownTarget, 0, len(targets))
	localTargets := make([]autoShutdownTarget, 0, 1)
	for _, target := range targets {
		if isLocalTarget(target.Target) || isGFSManageLocalHostname(target.Hostname) {
			localTargets = append(localTargets, target)
			continue
		}
		remoteTargets = append(remoteTargets, target)
	}
	return append(remoteTargets, localTargets...)
}

func callAutoShutdownRemote(client *http.Client, target autoShutdownTarget, req AutoShutdownRequest) (AutoShutdownResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return AutoShutdownResponse{}, err
	}

	url := fmt.Sprintf("%s/api/v1/cube/auto-shutdown", buildTargetURL(target.Target))
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return AutoShutdownResponse{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(autoShutdownLocalHeader, "1")

	resp, err := client.Do(httpReq)
	if err != nil {
		return AutoShutdownResponse{}, err
	}
	defer resp.Body.Close()

	var out AutoShutdownResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return AutoShutdownResponse{}, err
	}
	if out.Code == 0 {
		out.Code = resp.StatusCode
	}
	if out.Code >= http.StatusBadRequest {
		return out, fmt.Errorf("%s", firstNonEmpty(out.Message, resp.Status))
	}
	return out, nil
}

func autoShutdownResultFromResponse(req AutoShutdownRequest, target autoShutdownTarget, resp AutoShutdownResponse) AutoShutdownTargetResult {
	return AutoShutdownTargetResult{
		Hostname: target.Hostname,
		Target:   firstNonEmpty(resp.Target, target.Target),
		Code:     resp.Code,
		Val:      resp.Val,
		RetName:  firstNonEmpty(resp.RetName, autoShutdownRetName(req.Action)),
		Message:  resp.Message,
		Action:   req.Action,
		Mounts:   resp.Mounts,
	}
}

func autoShutdownResponseFromSCVM(req AutoShutdownRequest, resp SCVMUpdateResponse) AutoShutdownResponse {
	return AutoShutdownResponse{
		Code:    resp.Code,
		Val:     resp.Val,
		RetName: resp.RetName,
		Message: resp.Message,
		Action:  req.Action,
		Target:  resp.Target,
	}
}

func autoShutdownResponseFromResults(req AutoShutdownRequest, retName string, results []AutoShutdownTargetResult) AutoShutdownResponse {
	code := http.StatusOK
	val := true
	message := "ok"
	for _, result := range results {
		if result.Code != http.StatusOK {
			code = http.StatusInternalServerError
			val = false
			message = firstNonEmpty(result.Message, "failed")
			break
		}
	}
	return AutoShutdownResponse{
		Code:    code,
		Val:     val,
		RetName: retName,
		Message: message,
		Action:  req.Action,
		Results: results,
	}
}

func autoShutdownTargetError(req AutoShutdownRequest, target autoShutdownTarget, retName string, message string) AutoShutdownTargetResult {
	return AutoShutdownTargetResult{
		Hostname: target.Hostname,
		Target:   target.Target,
		Code:     http.StatusInternalServerError,
		Val:      false,
		RetName:  retName,
		Message:  message,
		Action:   req.Action,
	}
}

func autoShutdownLocalError(req AutoShutdownRequest, target string, retName string, message string, mounts []AutoShutdownMountResult) AutoShutdownResponse {
	return AutoShutdownResponse{
		Code:    http.StatusInternalServerError,
		Val:     false,
		RetName: retName,
		Message: message,
		Action:  req.Action,
		Target:  target,
		Mounts:  mounts,
	}
}

func autoShutdownError(req AutoShutdownRequest, retName string, message string, results []AutoShutdownTargetResult) AutoShutdownResponse {
	return AutoShutdownResponse{
		Code:    http.StatusInternalServerError,
		Val:     false,
		RetName: retName,
		Message: message,
		Action:  req.Action,
		Results: results,
	}
}

func autoShutdownRetName(action string) string {
	switch action {
	case "check_mount":
		return "Umount Volume"
	case "stop_scvms":
		return "Storage Center VM Stop"
	case "shutdown_hosts":
		return "Hosts Shutdown"
	default:
		return "Auto Shutdown"
	}
}

func statusCodeFromAutoShutdownResponse(resp AutoShutdownResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func unescapeAutoShutdownMountPath(value string) string {
	return strings.ReplaceAll(value, `\040`, " ")
}
