package cube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"ablecloud.io/ablestack-api/internal/service/clusterconfig"
	"github.com/gin-gonic/gin"
)

type SecurityPatchRequest = CubeModel.SecurityPatchRequest
type SecurityPatchResponse = CubeModel.SecurityPatchResponse
type SecurityPatchSummary = CubeModel.SecurityPatchSummary
type SecurityPatchTargetResult = CubeModel.SecurityPatchTargetResult
type SecurityPatchValue = CubeModel.SecurityPatchValue

const (
	securityPatchMaxRetries      = 3
	securityPatchRetryDelaySec   = 2
	securityPatchSuccessPattern  = "Permissions have been updated."
	securityPatchDefaultRetName  = "Security Update"
	securityPatchScriptPath      = "/usr/local/sbin/security_patch.sh"
	securityPatchCommandTimeout  = 30 * time.Minute
	securityPatchSSHConnectTO    = "10"
	securityPatchAblestackJSONPy = "python/ablestack_json/ablestackJson.py"
)

// SecurityPatch godoc
//
//	@Summary		Security Patch
//	@Description	cluster.json 대상에 security_patch.sh를 로컬/SSH로 실행하거나 security_patch.status 값을 업데이트합니다.
//	@Tags			CUBE - Security
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.SecurityPatchRequest	true	"security patch request"
//	@Success		200	{object}	CubeModel.SecurityPatchResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/security/patch [post]
func SecurityPatch(context *gin.Context) {
	var req SecurityPatchRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := normalizeSecurityPatchRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	cfg, err := loadSecurityPatchClusterConfig(req.JSONPath)
	if err != nil {
		resp := securityPatchError(req.RetName, err.Error())
		context.JSON(statusCodeFromSecurityPatchResponse(resp), resp)
		return
	}

	resp := runSecurityPatch(req, cfg)
	context.JSON(statusCodeFromSecurityPatchResponse(resp), resp)
}

func normalizeSecurityPatchRequest(req *SecurityPatchRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	req.JSONPath = strings.TrimSpace(req.JSONPath)
	if req.JSONPath == "" {
		req.JSONPath = resolveClusterJSONPath()
	}
	if !filepath.IsAbs(req.JSONPath) {
		return fmt.Errorf("json must be an absolute path")
	}
	req.SSHUser = strings.TrimSpace(req.SSHUser)
	if req.SSHUser == "" {
		req.SSHUser = "root"
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	if err := checkSecurityPatchPort(req.NewPort); err != nil {
		return err
	}
	if err := checkSecurityPatchPort(&req.SSHPort); err != nil {
		return err
	}
	if req.PortChange && req.NewPort == nil {
		return fmt.Errorf("port_change requires new_port")
	}
	if req.CephSSHChange && req.NewPort == nil {
		return fmt.Errorf("ceph_ssh_change requires new_port")
	}
	req.RetName = strings.TrimSpace(req.RetName)
	if req.RetName == "" {
		req.RetName = securityPatchDefaultRetName
	}
	if len(req.Targets) == 0 {
		req.Targets = []string{"all"}
	}
	normalizedTargets := make([]string, 0, len(req.Targets))
	for _, target := range req.Targets {
		target = strings.ToLower(strings.TrimSpace(target))
		switch target {
		case "ccvm", "ablecube", "scvm", "all":
			normalizedTargets = append(normalizedTargets, target)
		case "":
			continue
		default:
			return fmt.Errorf("invalid target: %s", target)
		}
	}
	if len(normalizedTargets) == 0 {
		normalizedTargets = []string{"all"}
	}
	req.Targets = normalizedTargets
	return nil
}

func runSecurityPatch(req SecurityPatchRequest, cfg *CubeModel.ClusterConfigSection) SecurityPatchResponse {
	clusterType := strings.ToLower(strings.TrimSpace(cfg.Type))

	if req.UpdateJSONFile {
		if err := updateSecurityPatchStatus(req, cfg); err != nil {
			return securityPatchError(req.RetName+" Error", "security patch error: "+err.Error())
		}
		return SecurityPatchResponse{
			Code:    http.StatusOK,
			RetName: req.RetName,
			Message: "ok",
			Val: SecurityPatchValue{
				Summary: SecurityPatchSummary{
					Message:     "security_patch.status updated to true for all ablecube hosts",
					JSON:        resolveSecurityPatchStatusJSONPath(),
					Val:         "security_patch.status = true",
					ClusterType: clusterType,
				},
			},
		}
	}

	if req.CephSSHChange {
		result := runSecurityPatchLocal(req.NewPort, req.DryRun, clusterType, req.PortChange, true)
		result.IP = "127.0.0.1"
		result.IsLocal = true
		success := 0
		if result.OK {
			success = 1
		}
		code := http.StatusOK
		if success != 1 {
			code = http.StatusMultiStatus
		}
		return securityPatchResultsResponse(req, code, SecurityPatchSummary{
			RequestedNewPort: req.NewPort,
			SSHUser:          req.SSHUser,
			Total:            1,
			Success:          success,
			Failed:           1 - success,
			DryRun:           req.DryRun,
			MaxRetries:       securityPatchMaxRetries,
			RetryDelaySec:    securityPatchRetryDelaySec,
			SuccessPattern:   securityPatchSuccessPattern,
			ClusterType:      clusterType,
			CephSSHChange:    true,
		}, []SecurityPatchTargetResult{result})
	}

	if req.AddHost {
		results := []SecurityPatchTargetResult{
			runSecurityPatchLocal(req.NewPort, req.DryRun, clusterType, req.PortChange, false),
		}
		results[0].IP = "127.0.0.1"
		results[0].IsLocal = true
		if clusterType == "ablestack-hci" {
			results = append(results, runSecurityPatchRemote("scvm", req.SSHUser, req.SSHPort, req.NewPort, req.DryRun, clusterType, req.PortChange))
		}
		success := countSecurityPatchSuccess(results)
		code := http.StatusOK
		if success != len(results) {
			code = http.StatusMultiStatus
		}
		connectPort := req.SSHPort
		return securityPatchResultsResponse(req, code, SecurityPatchSummary{
			RequestedNewPort: req.NewPort,
			ConnectPort:      &connectPort,
			SSHUser:          req.SSHUser,
			Total:            len(results),
			Success:          success,
			Failed:           len(results) - success,
			DryRun:           req.DryRun,
			MaxRetries:       securityPatchMaxRetries,
			RetryDelaySec:    securityPatchRetryDelaySec,
			SuccessPattern:   securityPatchSuccessPattern,
			ClusterType:      clusterType,
			Alone:            true,
			ScvmIncluded:     clusterType == "ablestack-hci",
		}, results)
	}

	targets := gatherSecurityPatchTargets(cfg, req.Targets)
	if len(targets) == 0 {
		return securityPatchResultsResponse(req, http.StatusOK, SecurityPatchSummary{
			Message: "no targets",
			JSON:    req.JSONPath,
			DryRun:  req.DryRun,
		}, []SecurityPatchTargetResult{})
	}

	localIPs := getSecurityPatchLocalIPv4s()
	results := make([]SecurityPatchTargetResult, 0, len(targets))
	for _, target := range targets {
		if _, ok := localIPs[target]; ok {
			res := runSecurityPatchLocal(req.NewPort, req.DryRun, clusterType, req.PortChange, false)
			res.IP = target
			res.IsLocal = true
			results = append(results, res)
			continue
		}
		results = append(results, runSecurityPatchRemote(target, req.SSHUser, req.SSHPort, req.NewPort, req.DryRun, clusterType, req.PortChange))
	}

	success := countSecurityPatchSuccess(results)
	code := http.StatusOK
	if success != len(results) {
		code = http.StatusMultiStatus
	}
	connectPort := req.SSHPort
	return securityPatchResultsResponse(req, code, SecurityPatchSummary{
		RequestedNewPort: req.NewPort,
		ConnectPort:      &connectPort,
		SSHUser:          req.SSHUser,
		Total:            len(results),
		Success:          success,
		Failed:           len(results) - success,
		DryRun:           req.DryRun,
		MaxRetries:       securityPatchMaxRetries,
		RetryDelaySec:    securityPatchRetryDelaySec,
		SuccessPattern:   securityPatchSuccessPattern,
		ClusterType:      clusterType,
	}, results)
}

func loadSecurityPatchClusterConfig(jsonPath string) (*CubeModel.ClusterConfigSection, error) {
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, err
	}
	root := map[string]any{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w", err)
	}
	root = clusterconfig.NormalizeClusterJSON(root)
	rawCfg, ok := root["clusterConfig"]
	if !ok {
		return nil, fmt.Errorf("clusterConfig not found")
	}
	cfgRaw, err := json.Marshal(rawCfg)
	if err != nil {
		return nil, err
	}
	var cfg CubeModel.ClusterConfigSection
	if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func gatherSecurityPatchTargets(cfg *CubeModel.ClusterConfigSection, kinds []string) []string {
	wantAll := false
	wants := map[string]bool{}
	for _, kind := range kinds {
		if kind == "all" {
			wantAll = true
		}
		wants[kind] = true
	}
	targets := map[string]struct{}{}
	if wantAll || wants["ablecube"] {
		for _, host := range cfg.Hosts {
			if ip := strings.TrimSpace(host.Ablecube); ip != "" {
				targets[ip] = struct{}{}
			}
		}
	}
	if wantAll || wants["scvm"] {
		for _, host := range cfg.Hosts {
			if ip := strings.TrimSpace(host.Scvm); ip != "" {
				targets[ip] = struct{}{}
			}
		}
	}
	if wantAll || wants["ccvm"] {
		if ip := strings.TrimSpace(cfg.CCVM.IP); ip != "" {
			targets[ip] = struct{}{}
		}
	}
	out := make([]string, 0, len(targets))
	for target := range targets {
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool {
		left, lErr := netip.ParseAddr(out[i])
		right, rErr := netip.ParseAddr(out[j])
		if lErr == nil && rErr == nil {
			return left.Less(right)
		}
		return out[i] < out[j]
	})
	return out
}

func runSecurityPatchRemote(ip string, user string, connectPort int, newPort *int, dryRun bool, clusterType string, portChange bool) SecurityPatchTargetResult {
	remote := fmt.Sprintf("%s@%s", user, ip)
	remoteCmd := buildSecurityPatchCommandString(newPort, portChange, false)
	sshCmd := []string{
		"/usr/bin/ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=" + securityPatchSSHConnectTO,
		"-p", fmt.Sprintf("%d", connectPort),
		remote,
		remoteCmd,
	}

	result := SecurityPatchTargetResult{
		IP:             ip,
		ConnectPort:    &connectPort,
		ChangeTo:       newPort,
		SuccessPattern: securityPatchSuccessPattern,
		ClusterType:    clusterType,
	}
	if dryRun {
		result.OK = true
		result.RC = 0
		result.DryRunCmd = joinSecurityPatchCommand(sshCmd)
		result.RetriesPlanned = securityPatchMaxRetries
		result.RetryDelaySec = securityPatchRetryDelaySec
		return result
	}

	for attempt := 1; attempt <= securityPatchMaxRetries; attempt++ {
		rc, stdout, stderr := runSecurityPatchCommand(sshCmd[0], sshCmd[1:]...)
		result.RC = rc
		result.Stderr = strings.TrimSpace(stderr)
		result.Attempts = attempt
		if rc == 0 || strings.Contains(stdout, securityPatchSuccessPattern) {
			result.OK = true
			result.SuccessAttempt = &attempt
			break
		}
		if attempt < securityPatchMaxRetries {
			time.Sleep(time.Duration(securityPatchRetryDelaySec) * time.Second)
		}
	}
	return result
}

func runSecurityPatchLocal(newPort *int, dryRun bool, clusterType string, portChange bool, cephSSHChange bool) SecurityPatchTargetResult {
	cmd := buildSecurityPatchCommandArgs(newPort, portChange, cephSSHChange)
	result := SecurityPatchTargetResult{
		IP:             "127.0.0.1",
		ChangeTo:       newPort,
		SuccessPattern: securityPatchSuccessPattern,
		ClusterType:    clusterType,
		IsLocal:        true,
	}
	if dryRun {
		result.OK = true
		result.RC = 0
		result.DryRunCmd = joinSecurityPatchCommand(cmd)
		result.RetriesPlanned = securityPatchMaxRetries
		result.RetryDelaySec = securityPatchRetryDelaySec
		return result
	}

	for attempt := 1; attempt <= securityPatchMaxRetries; attempt++ {
		rc, stdout, stderr := runSecurityPatchCommand(cmd[0], cmd[1:]...)
		result.RC = rc
		result.Stderr = strings.TrimSpace(stderr)
		result.Attempts = attempt
		if rc == 0 && strings.Contains(stdout, securityPatchSuccessPattern) {
			result.OK = true
			result.SuccessAttempt = &attempt
			break
		}
		if attempt < securityPatchMaxRetries {
			time.Sleep(time.Duration(securityPatchRetryDelaySec) * time.Second)
		}
	}
	return result
}

func buildSecurityPatchCommandArgs(newPort *int, portChange bool, cephSSHChange bool) []string {
	cmd := []string{resolveSecurityPatchScriptPath()}
	if newPort != nil {
		cmd = append(cmd, "-P", fmt.Sprintf("%d", *newPort))
	}
	if portChange {
		cmd = append(cmd, "--port-change")
	}
	if cephSSHChange {
		cmd = append(cmd, "--ceph-ssh-change")
	}
	return cmd
}

func buildSecurityPatchCommandString(newPort *int, portChange bool, cephSSHChange bool) string {
	return joinSecurityPatchCommand(buildSecurityPatchCommandArgs(newPort, portChange, cephSSHChange))
}

func resolveSecurityPatchScriptPath() string {
	if path := strings.TrimSpace(os.Getenv("ABLESTACK_SECURITY_PATCH_SCRIPT")); path != "" {
		return path
	}
	return securityPatchScriptPath
}

func runSecurityPatchCommand(command string, args ...string) (int, string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), securityPatchCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return 124, stdout.String(), firstNonEmpty(stderr.String(), "command timed out")
	}
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), stdout.String(), stderr.String()
	}
	if os.IsNotExist(err) {
		return 127, stdout.String(), err.Error()
	}
	return 1, stdout.String(), err.Error()
}

func updateSecurityPatchStatus(req SecurityPatchRequest, cfg *CubeModel.ClusterConfigSection) error {
	configPath := computeSecurityPatchConfigPath(req.JSONPath)
	if req.Local {
		return runSecurityPatchStatusUpdateLocal(configPath)
	}
	for _, host := range cfg.Hosts {
		target := strings.TrimSpace(host.Ablecube)
		if target == "" {
			continue
		}
		if err := runSecurityPatchStatusUpdateRemote(configPath, target, req.SSHUser, req.SSHPort); err != nil {
			return err
		}
	}
	return nil
}

func runSecurityPatchStatusUpdateLocal(configPath string) error {
	cmd := []string{
		"python3",
		filepath.Join(configPath, securityPatchAblestackJSONPy),
		"update",
		"--depth1", "security_patch",
		"--depth2", "status",
		"--value", "true",
	}
	rc, _, stderr := runSecurityPatchCommand(cmd[0], cmd[1:]...)
	if rc != 0 {
		return fmt.Errorf("%s", firstNonEmpty(stderr, "security_patch.status update failed"))
	}
	return nil
}

func runSecurityPatchStatusUpdateRemote(configPath string, target string, user string, port int) error {
	remote := fmt.Sprintf("%s@%s", user, target)
	remoteArgs := []string{
		"python3",
		filepath.Join(configPath, securityPatchAblestackJSONPy),
		"update",
		"--depth1", "security_patch",
		"--depth2", "status",
		"--value", "true",
	}
	cmd := []string{
		"/usr/bin/ssh",
		"-o", "StrictHostKeyChecking=no",
		"-p", fmt.Sprintf("%d", port),
		remote,
		joinSecurityPatchCommand(remoteArgs),
	}
	rc, _, stderr := runSecurityPatchCommand(cmd[0], cmd[1:]...)
	if rc != 0 {
		return fmt.Errorf("%s: %s", target, firstNonEmpty(stderr, "security_patch.status update failed"))
	}
	return nil
}

func resolveSecurityPatchStatusJSONPath() string {
	return resolveAbleStackPropertyFile("ablestack.json")
}

func computeSecurityPatchConfigPath(jsonPath string) string {
	clean := filepath.Clean(jsonPath)
	dir := filepath.Dir(clean)
	if filepath.Base(dir) == "properties" {
		return filepath.Dir(dir)
	}
	return resolveAbleStackConfigPath()
}

func getSecurityPatchLocalIPv4s() map[string]struct{} {
	out := map[string]struct{}{"127.0.0.1": {}}
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			if ipv4 := ip.To4(); ipv4 != nil {
				out[ipv4.String()] = struct{}{}
			}
		}
	}
	if host, err := os.Hostname(); err == nil {
		if ip, err := net.LookupHost(host); err == nil {
			for _, item := range ip {
				parsed := net.ParseIP(item)
				if parsed != nil && parsed.To4() != nil {
					out[parsed.String()] = struct{}{}
				}
			}
		}
	}
	return out
}

func checkSecurityPatchPort(port *int) error {
	if port == nil {
		return nil
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("invalid port: %d", *port)
	}
	return nil
}

func countSecurityPatchSuccess(results []SecurityPatchTargetResult) int {
	count := 0
	for _, result := range results {
		if result.OK {
			count++
		}
	}
	return count
}

func securityPatchResultsResponse(req SecurityPatchRequest, code int, summary SecurityPatchSummary, results []SecurityPatchTargetResult) SecurityPatchResponse {
	return SecurityPatchResponse{
		Code:    code,
		RetName: req.RetName,
		Message: "ok",
		Val: SecurityPatchValue{
			Summary: summary,
			Targets: results,
		},
	}
}

func securityPatchError(retName string, message string) SecurityPatchResponse {
	return SecurityPatchResponse{
		Code:    http.StatusInternalServerError,
		Val:     message,
		RetName: retName,
		Message: message,
	}
}

func statusCodeFromSecurityPatchResponse(resp SecurityPatchResponse) int {
	switch resp.Code {
	case http.StatusOK:
		return http.StatusOK
	case http.StatusMultiStatus:
		return http.StatusMultiStatus
	case http.StatusBadRequest:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func joinSecurityPatchCommand(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, shellQuoteSecurityPatch(part))
	}
	return strings.Join(quoted, " ")
}

func shellQuoteSecurityPatch(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$`!#&;|*?()[]{}<>") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
