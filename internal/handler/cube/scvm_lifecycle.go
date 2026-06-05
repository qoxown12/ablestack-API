package cube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type SCVMUpdateRequest = CubeModel.SCVMUpdateRequest
type SCVMUpdateResponse = CubeModel.SCVMUpdateResponse

const (
	scvmUpdateLocalHeader      = "X-Cube-SCVM-Update-Local"
	scvmUpdateRequestTimeout   = 5 * time.Minute
	scvmUpdateCommandTimeout   = 2 * time.Minute
	scvmDeleteCommandTimeout   = 30 * time.Second
	scvmResourceCommandTimeout = 30 * time.Second
	scvmUpdateSuccessStatus    = "ok"
	scvmSetupCommandTimeout    = 5 * time.Minute
	scvmSetupTemplatePath      = "/var/lib/libvirt/images/ablestack-template-back.qcow2"
	scvmSetupImagePath         = "/var/lib/libvirt/images/scvm.qcow2"
	scvmSetupSuccessMessage    = "storage center setup success"
	scvmResetSuccessMessage    = "storage center reset success"
)

// SCVMLifecycle godoc
//
//	@Summary		SCVM Lifecycle
//	@Description	Storage Center VM lifecycle 작업을 수행합니다. 사용 가능한 action: setup, reset, start, stop, delete, resource. resource action은 cpu 또는 memory 값을 함께 전달합니다.
//	@Tags			Cube-SCVM
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.SCVMUpdateRequest	true	"scvm lifecycle request"
//	@Success		200	{object}	CubeModel.SCVMUpdateResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/scvm/lifecycle [post]
func SCVMLifecycle(context *gin.Context) {
	var req SCVMUpdateRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}
	handleSCVMLifecycleRequest(context, req)
}

func handleSCVMLifecycleRequest(context *gin.Context, req SCVMUpdateRequest) {
	if err := normalizeSCVMUpdateRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	cfg, cfgErr := loadClusterConfigSection()
	if strings.TrimSpace(req.TargetHostname) != "" && cfgErr != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}

	if isSCVMUpdateLocalRequest(context) {
		resp := runSCVMUpdateLocal(cfg, req)
		context.JSON(statusCodeFromSCVMUpdateResponse(resp), resp)
		return
	}

	target := resolveSCVMUpdateTarget(cfg, req)
	if strings.TrimSpace(req.TargetHostname) != "" && strings.TrimSpace(target) == "" {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "target_hostname not found",
		})
		return
	}
	if strings.TrimSpace(target) != "" && !isLocalTarget(target) {
		resp, err := callSCVMUpdateRemote(target, req)
		if err != nil {
			resp = scvmUpdateError(req, target, scvmUpdateRetName(req.Action), err.Error())
		}
		context.JSON(statusCodeFromSCVMUpdateResponse(resp), resp)
		return
	}

	resp := runSCVMUpdateLocal(cfg, req)
	context.JSON(statusCodeFromSCVMUpdateResponse(resp), resp)
}

func normalizeSCVMUpdateRequest(req *SCVMUpdateRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}

	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "start":
		req.Action = "start"
	case "stop":
		req.Action = "stop"
	case "delete":
		req.Action = "delete"
	case "resource":
		req.Action = "resource"
	case "setup":
		req.Action = "setup"
	case "reset":
		req.Action = "reset"
	default:
		return fmt.Errorf("unsupported action")
	}

	req.Target = strings.TrimSpace(req.Target)
	req.TargetHostname = strings.TrimSpace(req.TargetHostname)
	if req.Action == "resource" && req.CPU <= 0 && req.Memory <= 0 {
		return fmt.Errorf("cpu or memory required")
	}
	if req.CPU < 0 {
		return fmt.Errorf("invalid cpu")
	}
	if req.Memory < 0 {
		return fmt.Errorf("invalid memory")
	}
	return nil
}

func isSCVMUpdateLocalRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(scvmUpdateLocalHeader)), "1")
}

func statusCodeFromSCVMUpdateResponse(resp SCVMUpdateResponse) int {
	if resp.Code == 200 {
		return http.StatusOK
	}
	if resp.Code == 400 {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func resolveSCVMUpdateTarget(cfg *CubeModel.ClusterConfigSection, req SCVMUpdateRequest) string {
	if strings.TrimSpace(req.Target) != "" {
		return strings.TrimSpace(req.Target)
	}
	if cfg == nil || strings.TrimSpace(req.TargetHostname) == "" {
		return ""
	}
	targetHostname := strings.ToLower(strings.TrimSpace(req.TargetHostname))
	for _, host := range cfg.Hosts {
		if strings.ToLower(strings.TrimSpace(host.Hostname)) == targetHostname {
			return strings.TrimSpace(host.Ablecube)
		}
	}
	return ""
}

func runSCVMUpdateLocal(cfg *CubeModel.ClusterConfigSection, req SCVMUpdateRequest) SCVMUpdateResponse {
	target := resolveLocalSCVMUpdateTarget(cfg)
	var err error

	switch req.Action {
	case "start":
		err = startSCVMLocal()
	case "stop":
		err = stopSCVMLocal()
	case "delete":
		err = deleteSCVMLocal()
	case "resource":
		err = updateSCVMResourceLocal(req.CPU, req.Memory)
	case "setup":
		err = setupSCVMLocal()
	case "reset":
		err = resetSCVMLocal()
	default:
		err = fmt.Errorf("unsupported action")
	}
	if err != nil {
		return scvmUpdateError(req, target, scvmUpdateRetName(req.Action), err.Error())
	}

	clearSCVMStatusCache()
	return SCVMUpdateResponse{
		Code:    200,
		Val:     scvmLifecycleSuccessValue(req.Action),
		RetName: scvmUpdateRetName(req.Action),
		Message: scvmLifecycleSuccessMessage(req.Action),
		Target:  target,
		Action:  req.Action,
	}
}

func startSCVMLocal() error {
	state, exists, err := readSCVMDomainState()
	if err == nil && exists && strings.EqualFold(state, "running") {
		return nil
	}

	if _, err := runSCVMUpdateCommand(scvmUpdateCommandTimeout, "virsh", "start", scvmDomainName); err != nil {
		return err
	}
	return waitForSCVMState(scvmUpdateCommandTimeout, "running")
}

func stopSCVMLocal() error {
	state, exists, err := readSCVMDomainState()
	if err == nil && exists && strings.EqualFold(state, "shut off") {
		return nil
	}
	if !exists {
		return fmt.Errorf("scvm domain not found")
	}

	if _, err := runSCVMUpdateCommand(scvmUpdateCommandTimeout, "virsh", "shutdown", scvmDomainName); err != nil {
		return err
	}
	return waitForSCVMState(scvmUpdateCommandTimeout, "shut off")
}

func deleteSCVMLocal() error {
	_, _ = runSCVMUpdateCommand(scvmDeleteCommandTimeout, "virsh", "destroy", scvmDomainName)
	if _, err := runSCVMUpdateCommand(scvmDeleteCommandTimeout, "virsh", "undefine", "--nvram", scvmDomainName); err != nil {
		if !isSCVMDomainNotFoundError(err) {
			return err
		}
	}
	if err := waitForSCVMDomainGone(scvmUpdateCommandTimeout); err != nil {
		return err
	}
	return removeCephConfigContents()
}

func updateSCVMResourceLocal(cpu int, memoryGiB int) error {
	if cpu > 0 {
		if _, err := runSCVMUpdateCommand(
			scvmResourceCommandTimeout,
			"virt-xml",
			scvmDomainName,
			"--edit",
			"--vcpus",
			fmt.Sprintf("maxvcpus=%d", cpu),
		); err != nil {
			return err
		}
	}

	if memoryGiB > 0 {
		memoryMiB := memoryGiB * 1024
		if _, err := runSCVMUpdateCommand(
			scvmResourceCommandTimeout,
			"virt-xml",
			scvmDomainName,
			"--edit",
			"--memory",
			fmt.Sprintf("%d,maxmemory=%d", memoryMiB, memoryMiB),
		); err != nil {
			return err
		}
	}
	return nil
}

func setupSCVMLocal() error {
	state, exists, err := readSCVMDomainState()
	if err != nil && !isSCVMDomainNotFoundError(err) {
		return err
	}
	if err == nil && exists && strings.EqualFold(state, "running") {
		_, err := runSCVMUpdateCommand(scvmSetupCommandTimeout, "virsh", "autostart", scvmDomainName)
		return err
	}

	if err := copySCVMLifecycleFile(scvmSetupTemplatePath, scvmSetupImagePath, 0o666); err != nil {
		return fmt.Errorf("prepare scvm image: %w", err)
	}

	xmlPath := filepath.Join(resolveAbleStackVMConfigDir("scvm"), "scvm.xml")
	if err := requireRegularFile(xmlPath, "scvm xml not found"); err != nil {
		return err
	}
	if _, err := runSCVMUpdateCommand(scvmSetupCommandTimeout, "virsh", "define", xmlPath); err != nil {
		return err
	}

	state, exists, err = readSCVMDomainState()
	if err != nil && !isSCVMDomainNotFoundError(err) {
		return err
	}
	if !exists || !strings.EqualFold(state, "running") {
		if _, err := runSCVMUpdateCommand(scvmSetupCommandTimeout, "virsh", "start", scvmDomainName); err != nil {
			return err
		}
		if err := waitForSCVMState(scvmSetupCommandTimeout, "running"); err != nil {
			return err
		}
	}

	_, err = runSCVMUpdateCommand(scvmSetupCommandTimeout, "virsh", "autostart", scvmDomainName)
	return err
}

func resetSCVMLocal() error {
	scvmConfigDir := resolveAbleStackVMConfigDir("scvm")
	if err := os.MkdirAll(scvmConfigDir, 0o755); err != nil {
		return err
	}

	_, _ = runSCVMUpdateCommand(scvmDeleteCommandTimeout, "virsh", "destroy", scvmDomainName)
	if _, err := runSCVMUpdateCommand(scvmDeleteCommandTimeout, "virsh", "undefine", scvmDomainName, "--keep-nvram"); err != nil {
		if !isSCVMDomainNotFoundError(err) {
			return err
		}
	}
	if err := os.Remove(scvmSetupImagePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := removeCephConfigContents(); err != nil {
		return err
	}
	return removeDirContents(scvmConfigDir)
}

func copySCVMLifecycleFile(src string, dst string, mode os.FileMode) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	info, err := source.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	temp := dst + ".tmp"
	target, err := os.OpenFile(temp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(target, source); err != nil {
		_ = target.Close()
		_ = os.Remove(temp)
		return err
	}
	if err = target.Close(); err != nil {
		_ = os.Remove(temp)
		return err
	}
	if err = os.Chmod(temp, mode); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return os.Rename(temp, dst)
}

func copySCVMLifecycleDir(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}

	return filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)

		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(link, target)
		case entry.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode().IsRegular():
			return copySCVMLifecycleFile(path, target, info.Mode().Perm())
		default:
			return nil
		}
	})
}

func removeDirContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func waitForSCVMState(timeout time.Duration, desired string) error {
	deadline := time.Now().Add(timeout)
	for {
		state, exists, err := readSCVMDomainState()
		if err == nil && exists && strings.EqualFold(state, desired) {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("wait scvm %s timed out: %w", desired, err)
			}
			if !exists {
				return fmt.Errorf("wait scvm %s timed out: domain not found", desired)
			}
			return fmt.Errorf("wait scvm %s timed out: current state=%s", desired, state)
		}
		time.Sleep(time.Second)
	}
}

func waitForSCVMDomainGone(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, exists, err := readSCVMDomainState()
		if !exists || isSCVMDomainNotFoundError(err) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait scvm delete timed out")
		}
		time.Sleep(time.Second)
	}
}

func readSCVMDomainState() (string, bool, error) {
	out, err := runSCVMUpdateCommand(scvmDeleteCommandTimeout, "virsh", "domstate", scvmDomainName)
	state := strings.TrimSpace(out)
	if err != nil {
		if isSCVMDomainNotFoundOutput(state) || isSCVMDomainNotFoundError(err) {
			return "", false, nil
		}
		return state, false, err
	}
	return state, true, nil
}

func runSCVMUpdateCommand(timeout time.Duration, command string, args ...string) (string, error) {
	env := scvmUpdateCommandEnv()
	if command == "virsh" {
		env = virshEnv()
	}
	out, timedOut, err := runCommandOutputWithEnv(command, timeout, env, args...)
	if timedOut {
		return out, fmt.Errorf("%s %s timed out after %s", command, strings.Join(args, " "), timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("%s %s failed: %s", command, strings.Join(args, " "), msg)
	}
	return out, nil
}

func scvmUpdateCommandEnv() []string {
	return append([]string{"LANG=en_US.utf-8", "LANGUAGE=en"}, os.Environ()...)
}

func removeCephConfigContents() error {
	const cephConfigDir = "/etc/ceph"
	entries, err := os.ReadDir(cephConfigDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(cephConfigDir + "/" + entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func isSCVMDomainNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return isSCVMDomainNotFoundOutput(err.Error())
}

func isSCVMDomainNotFoundOutput(output string) bool {
	output = strings.ToLower(strings.TrimSpace(output))
	return strings.Contains(output, "domain not found") ||
		strings.Contains(output, "failed to get domain") ||
		strings.Contains(output, "no domain")
}

func clearSCVMStatusCache() {
	scvmStatusCache.mu.Lock()
	defer scvmStatusCache.mu.Unlock()
	scvmStatusCache.expires = time.Time{}
	scvmStatusCache.data = SCVMStatusDetail{}
}

func resolveLocalSCVMUpdateTarget(cfg *CubeModel.ClusterConfigSection) string {
	if cfg != nil {
		for _, host := range cfg.Hosts {
			target := strings.TrimSpace(host.Ablecube)
			if target == "" {
				continue
			}
			if isLocalTarget(target) {
				return target
			}
		}
	}
	name, err := os.Hostname()
	if err == nil && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return "local"
}

func callSCVMUpdateRemote(target string, req SCVMUpdateRequest) (SCVMUpdateResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return SCVMUpdateResponse{}, err
	}

	baseURL := buildTargetURL(target)
	url := fmt.Sprintf("%s/api/v1/cube/scvm/lifecycle", baseURL)
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return SCVMUpdateResponse{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(scvmUpdateLocalHeader, "1")

	client := &http.Client{Timeout: scvmUpdateRequestTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return SCVMUpdateResponse{}, err
	}
	defer resp.Body.Close()

	var out SCVMUpdateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return SCVMUpdateResponse{}, err
	}
	if out.Code == 0 {
		out.Code = resp.StatusCode
	}
	if strings.TrimSpace(out.Target) == "" {
		out.Target = target
	}
	if strings.TrimSpace(out.Action) == "" {
		out.Action = req.Action
	}
	return out, nil
}

func scvmUpdateRetName(action string) string {
	switch action {
	case "start":
		return "Storage Center VM Start"
	case "stop":
		return "Storage Center VM Stop"
	case "delete":
		return "Storage Center VM Delete"
	case "resource":
		return "Storage Center VM UPDATE"
	case "setup":
		return "Storage Center VM Setup"
	case "reset":
		return "Storage Center VM Reset"
	default:
		return "Storage Center VM"
	}
}

func scvmLifecycleSuccessValue(action string) any {
	switch action {
	case "setup":
		return scvmSetupSuccessMessage
	case "reset":
		return scvmResetSuccessMessage
	default:
		return true
	}
}

func scvmLifecycleSuccessMessage(action string) string {
	switch action {
	case "setup":
		return scvmSetupSuccessMessage
	case "reset":
		return scvmResetSuccessMessage
	default:
		return scvmUpdateSuccessStatus
	}
}

func scvmUpdateError(req SCVMUpdateRequest, target string, retName string, message string) SCVMUpdateResponse {
	return SCVMUpdateResponse{
		Code:    500,
		Val:     false,
		RetName: retName,
		Message: message,
		Target:  target,
		Action:  req.Action,
	}
}
