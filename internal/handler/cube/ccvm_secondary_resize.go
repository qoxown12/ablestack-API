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

type CCVMSecondaryResizeRequest = CubeModel.CCVMSecondaryResizeRequest
type CCVMSecondaryResizeResponse = CubeModel.CCVMSecondaryResizeResponse

const (
	ccvmSecondaryResizeLocalHeader = "X-Cube-CCVM-Secondary-Resize-Local"
	ccvmSecondaryResizeModeHeader  = "X-Cube-CCVM-Secondary-Resize-Mode"
	ccvmSecondaryResizeGuestHeader = "X-Cube-CCVM-Secondary-Resize-Guest"
	ccvmSecondaryResizeModePing    = "ping"
	ccvmSecondaryResizeModeGrow    = "grow"
	ccvmSecondaryResizeRetName     = "CCVM Secondary Resize"
	ccvmSecondaryResizeSuccess     = "CCVM secondary filesystem expansion success."
	ccvmSecondaryResizeMinGiB      = 1
	ccvmSecondaryResizeMaxGiB      = 500
	ccvmSecondaryResizeLimitGiB    = 2000
	ccvmSecondaryResizeTimeout     = 10 * time.Minute
	ccvmSecondaryResizeShortTO     = 10 * time.Second
	ccvmSecondaryResizeRequestTO   = 12 * time.Minute
	ccvmSecondaryResizePoll        = 5 * time.Second
	ccvmSecondaryVMImagePath       = "/mnt/glue-gfs/ccvm.qcow2"
	ccvmSecondaryStandaloneImage   = "/mnt/glue/ccvm.qcow2"
	ccvmSecondaryRBDImage          = "rbd/ccvm"
)

// CCVMSecondaryResize godoc
//
//	@Summary		CCVM Secondary Resize
//	@Description	CCVM secondary 용량을 추가합니다. SSH 대신 qemu-guest-agent를 사용하며, PCS 제어는 pcs.go helper를 사용합니다.
//	@Tags			CUBE - CCVM
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.CCVMSecondaryResizeRequest	true	"ccvm secondary resize request"
//	@Success		200	{object}	CubeModel.CCVMSecondaryResizeResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/ccvm/secondary/resize [post]
func CCVMSecondaryResize(context *gin.Context) {
	var req CCVMSecondaryResizeRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if isCCVMSecondaryResizeLocalRequest(context) {
		resp := runCCVMSecondaryResizeLocalMode(
			req,
			strings.TrimSpace(context.GetHeader(ccvmSecondaryResizeModeHeader)),
			isCCVMSecondaryResizeGuestRequest(context),
		)
		context.JSON(statusCodeFromCCVMSecondaryResizeResponse(resp), resp)
		return
	}

	if err := normalizeCCVMSecondaryResizeRequest(&req); err != nil {
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

	resp := runCCVMSecondaryResize(req, cfg)
	context.JSON(statusCodeFromCCVMSecondaryResizeResponse(resp), resp)
}

func normalizeCCVMSecondaryResizeRequest(req *CCVMSecondaryResizeRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	if req.AddSize < ccvmSecondaryResizeMinGiB || req.AddSize > ccvmSecondaryResizeMaxGiB {
		return fmt.Errorf("please enter additional capacity size between 1 and 500 GiB")
	}
	return nil
}

func isCCVMSecondaryResizeLocalRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(ccvmSecondaryResizeLocalHeader)), "1")
}

func isCCVMSecondaryResizeGuestRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(ccvmSecondaryResizeGuestHeader)), "1")
}

func runCCVMSecondaryResize(req CCVMSecondaryResizeRequest, cfg *CubeModel.ClusterConfigSection) CCVMSecondaryResizeResponse {
	if cfg == nil {
		return ccvmSecondaryResizeError("", "", "clusterConfig not found")
	}

	osType := strings.ToLower(strings.TrimSpace(cfg.Type))
	switch osType {
	case "ablestack-hci", "ablestack-hci-filesystem":
		return runCCVMSecondaryResizeRBD(req, cfg, osType)
	case "ablestack-vm":
		return runCCVMSecondaryResizeVM(req, cfg, osType)
	case "ablestack-standalone":
		return runCCVMSecondaryResizeStandalone(req, osType)
	default:
		return ccvmSecondaryResizeError(osType, "", "unsupported cluster type")
	}
}

func runCCVMSecondaryResizeRBD(req CCVMSecondaryResizeRequest, cfg *CubeModel.ClusterConfigSection, osType string) CCVMSecondaryResizeResponse {
	newSizeGiB, err := ccvmSecondaryResizeNewRBDSize(req.AddSize)
	if err != nil {
		return ccvmSecondaryResizeError(osType, "", err.Error())
	}
	if newSizeGiB > ccvmSecondaryResizeLimitGiB {
		return ccvmSecondaryResizeError(osType, "", "CCVM can support capacities up to 2 TiB.")
	}

	if _, err := waitCCVMSecondaryGuestAgentOnStartedTarget(cfg, ccvmSecondaryResizeShortTO); err != nil {
		return ccvmSecondaryResizeError(osType, "", "Please check if CCVM status is running normally.")
	}
	if err := ccvmSecondaryResizeDisablePCS(cfg); err != nil {
		return ccvmSecondaryResizeError(osType, "", err.Error())
	}
	if err := ccvmSecondaryResizeWaitPCSRole(cfg, "Stopped", ccvmSecondaryResizeTimeout); err != nil {
		return ccvmSecondaryResizeError(osType, "", err.Error())
	}
	if _, err := runPCSCommand(ccvmSecondaryResizeTimeout, "rbd", "snap", "purge", ccvmSecondaryRBDImage, "--no-progress"); err != nil {
		return ccvmSecondaryResizeError(osType, "", "CCVM snapshot purge failed.")
	}
	if _, err := runPCSCommand(ccvmSecondaryResizeTimeout, "rbd", "resize", "-s", fmt.Sprintf("%dG", newSizeGiB), ccvmSecondaryRBDImage); err != nil {
		return ccvmSecondaryResizeError(osType, "", "CCVM image resize failed.")
	}
	if err := ccvmSecondaryResizeEnablePCS(cfg); err != nil {
		return ccvmSecondaryResizeError(osType, "", err.Error())
	}

	target, err := waitCCVMSecondaryGuestAgentOnStartedTarget(cfg, ccvmSecondaryResizeTimeout)
	if err != nil {
		return ccvmSecondaryResizeError(osType, "", "CCVM SSH not available after start. Please check.")
	}
	if err := ccvmSecondaryResizeGrowOnTarget(target.Target, req); err != nil {
		return ccvmSecondaryResizeError(osType, target.Target, err.Error())
	}
	return ccvmSecondaryResizeOK(osType, target.Target)
}

func runCCVMSecondaryResizeVM(req CCVMSecondaryResizeRequest, cfg *CubeModel.ClusterConfigSection, osType string) CCVMSecondaryResizeResponse {
	if target, err := waitCCVMSecondaryCCVMAPIOnStartedResource(cfg, ccvmSecondaryResizeShortTO); err != nil {
		appendAbleStackAPILog("ccvm_secondary_resize", "event=vm_precheck_failed target=%q error=%q", target, err.Error())
		return ccvmSecondaryResizeError(osType, "", "Please check if CCVM status is running normally.")
	} else {
		appendAbleStackAPILog("ccvm_secondary_resize", "event=vm_precheck_success target=%q", target)
	}
	if err := ccvmSecondaryResizeDisablePCS(cfg); err != nil {
		appendAbleStackAPILog("ccvm_secondary_resize", "event=vm_disable_failed error=%q", err.Error())
		return ccvmSecondaryResizeError(osType, "", err.Error())
	}
	if err := ccvmSecondaryResizeWaitPCSRole(cfg, "Stopped", ccvmSecondaryResizeTimeout); err != nil {
		appendAbleStackAPILog("ccvm_secondary_resize", "event=vm_stop_wait_failed error=%q", err.Error())
		return ccvmSecondaryResizeError(osType, "", err.Error())
	}
	if _, err := runCCVMSecondaryResizeCommand(ccvmSecondaryResizeTimeout, "qemu-img", "resize", ccvmSecondaryVMImagePath, fmt.Sprintf("+%dG", req.AddSize)); err != nil {
		appendAbleStackAPILog("ccvm_secondary_resize", "event=vm_image_resize_failed error=%q", err.Error())
		return ccvmSecondaryResizeError(osType, "", err.Error())
	}
	if err := ccvmSecondaryResizeEnablePCS(cfg); err != nil {
		appendAbleStackAPILog("ccvm_secondary_resize", "event=vm_enable_failed error=%q", err.Error())
		return ccvmSecondaryResizeError(osType, "", err.Error())
	}

	target, err := waitCCVMSecondaryCCVMAPIOnStartedResource(cfg, ccvmSecondaryResizeTimeout)
	if err != nil {
		appendAbleStackAPILog("ccvm_secondary_resize", "event=vm_start_wait_failed target=%q error=%q", target, err.Error())
		return ccvmSecondaryResizeError(osType, "", "CCVM API not available after start. Please check.")
	}
	if err := ccvmSecondaryResizeGrowOnCCVM(cfg, req); err != nil {
		appendAbleStackAPILog("ccvm_secondary_resize", "event=vm_guest_grow_failed target=%q error=%q", target, err.Error())
		return ccvmSecondaryResizeError(osType, target, err.Error())
	}
	appendAbleStackAPILog("ccvm_secondary_resize", "event=vm_success target=%q add_size=%d", target, req.AddSize)
	return ccvmSecondaryResizeOK(osType, target)
}

func runCCVMSecondaryResizeStandalone(req CCVMSecondaryResizeRequest, osType string) CCVMSecondaryResizeResponse {
	if _, err := runCCVMSecondaryResizeCommand(ccvmSecondaryResizeShortTO, "virsh", "shutdown", ccvmSnapName); err != nil {
		// shutdown은 이미 꺼진 상태에서도 실패할 수 있으므로 상태 대기에서 최종 판단한다.
		_ = err
	}
	if err := waitForCCVMState(ccvmSecondaryResizeTimeout, "shut off"); err != nil {
		return ccvmSecondaryResizeError(osType, "local", "CCVM shutdown timeout. Please check.")
	}
	if _, err := runCCVMSecondaryResizeCommand(ccvmSecondaryResizeTimeout, "qemu-img", "resize", ccvmSecondaryStandaloneImage, fmt.Sprintf("+%dG", req.AddSize)); err != nil {
		return ccvmSecondaryResizeError(osType, "local", err.Error())
	}
	if _, err := runCCVMSecondaryResizeCommand(ccvmSecondaryResizeShortTO, "virsh", "start", ccvmSnapName); err != nil {
		return ccvmSecondaryResizeError(osType, "local", err.Error())
	}
	if err := waitForCCVMState(ccvmSecondaryResizeTimeout, "running"); err != nil {
		return ccvmSecondaryResizeError(osType, "local", err.Error())
	}
	if err := waitCCVMSecondaryGuestAgentLocal(ccvmSecondaryResizeTimeout); err != nil {
		return ccvmSecondaryResizeError(osType, "local", "CCVM SSH not available after start. Please check.")
	}
	if err := ccvmSecondaryResizeGrowGuestFilesystem(); err != nil {
		return ccvmSecondaryResizeError(osType, "local", err.Error())
	}
	return ccvmSecondaryResizeOK(osType, "local")
}

func runCCVMSecondaryResizeLocalMode(req CCVMSecondaryResizeRequest, mode string, guestRequest bool) CCVMSecondaryResizeResponse {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ccvmSecondaryResizeModePing:
		if guestRequest {
			return ccvmSecondaryResizeOK("", "ccvm")
		}
		if err := ccvmSecondaryResizeGuestPing(); err != nil {
			return ccvmSecondaryResizeError("", "local", err.Error())
		}
		return ccvmSecondaryResizeOK("", "local")
	case ccvmSecondaryResizeModeGrow:
		if guestRequest {
			if err := ccvmSecondaryResizeGrowGuestFilesystemDirect(); err != nil {
				return ccvmSecondaryResizeError("", "ccvm", err.Error())
			}
			return ccvmSecondaryResizeOK("", "ccvm")
		}
		if err := ccvmSecondaryResizeGrowGuestFilesystem(); err != nil {
			return ccvmSecondaryResizeError("", "local", err.Error())
		}
		return ccvmSecondaryResizeOK("", "local")
	default:
		return CCVMSecondaryResizeResponse{
			Code:    http.StatusBadRequest,
			Val:     "unsupported local mode",
			RetName: ccvmSecondaryResizeRetName,
			Message: "unsupported local mode",
			Target:  "local",
		}
	}
}

func waitCCVMSecondaryCCVMAPIOnStartedResource(cfg *CubeModel.ClusterConfigSection, timeout time.Duration) (string, error) {
	target := ""
	if cfg != nil {
		target = strings.TrimSpace(cfg.CCVM.IP)
	}
	if target == "" {
		return "", fmt.Errorf("ccvm.ip required")
	}

	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		status, err := ccvmSecondaryResizePCSStatus(cfg)
		if err != nil {
			lastErr = err
		} else if !strings.EqualFold(status.Role, "Started") || strings.TrimSpace(status.Started) == "" {
			lastErr = fmt.Errorf("cloudcenter_res is not started: role=%s started=%s", status.Role, status.Started)
		} else if err := ccvmSecondaryResizePingCCVMAPI(target); err != nil {
			lastErr = err
		} else {
			return target, nil
		}

		if time.Now().After(deadline) {
			if lastErr != nil {
				return target, lastErr
			}
			return target, fmt.Errorf("ccvm api not available")
		}
		time.Sleep(ccvmSecondaryResizePoll)
	}
}

func ccvmSecondaryResizePingCCVMAPI(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("ccvm.ip required")
	}
	if isLocalTarget(target) {
		_, err := collectCCVMLocalStatus()
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	_, err := callCCVMStatus(client, target)
	return err
}

func ccvmSecondaryResizeGrowOnCCVM(cfg *CubeModel.ClusterConfigSection, req CCVMSecondaryResizeRequest) error {
	target := ""
	if cfg != nil {
		target = strings.TrimSpace(cfg.CCVM.IP)
	}
	if target == "" {
		return fmt.Errorf("ccvm.ip required")
	}
	if isLocalTarget(target) {
		return ccvmSecondaryResizeGrowGuestFilesystemDirect()
	}
	resp, err := callCCVMSecondaryResizeGuestRemote(target, req, ccvmSecondaryResizeModeGrow)
	if err != nil {
		return err
	}
	if resp.Code != http.StatusOK {
		return fmt.Errorf("%s", firstNonEmpty(resp.Message, resp.Val))
	}
	return nil
}

func ccvmSecondaryResizeNewRBDSize(addSizeGiB int) (int64, error) {
	out, err := runCCVMSecondaryResizeCommand(ccvmSecondaryResizeShortTO, "rbd", "info", ccvmSecondaryRBDImage, "--format", "json")
	if err != nil {
		return 0, err
	}
	var info struct {
		Size int64 `json:"size"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return 0, fmt.Errorf("failed to parse rbd info: %w", err)
	}
	if info.Size <= 0 {
		return 0, fmt.Errorf("invalid rbd image size")
	}
	const gib = int64(1024 * 1024 * 1024)
	currentGiB := (info.Size + gib - 1) / gib
	return currentGiB + int64(addSizeGiB), nil
}

func ccvmSecondaryResizeDisablePCS(cfg *CubeModel.ClusterConfigSection) error {
	resp := runCCVMLifecyclePCSAction(cfg, CCVMPCSControlRequest{
		Action:   "disable",
		Resource: pcsDefaultResourceID,
	})
	if resp.Code != http.StatusOK {
		return fmt.Errorf("cloudcenter_res disable failed.")
	}
	return nil
}

func ccvmSecondaryResizeEnablePCS(cfg *CubeModel.ClusterConfigSection) error {
	resp := runCCVMLifecyclePCSAction(cfg, CCVMPCSControlRequest{
		Action:   "enable",
		Resource: pcsDefaultResourceID,
	})
	if resp.Code != http.StatusOK {
		return fmt.Errorf("cloudcenter_res enable failed.")
	}
	return nil
}

func ccvmSecondaryResizeWaitPCSRole(cfg *CubeModel.ClusterConfigSection, desiredRole string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, err := ccvmSecondaryResizePCSStatus(cfg)
		if err == nil && strings.EqualFold(status.Role, desiredRole) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("cloudcenter_res %s timeout. Please check.", strings.ToLower(desiredRole))
		}
		time.Sleep(ccvmSecondaryResizePoll)
	}
}

type ccvmSecondaryResizePCSStatusValue struct {
	Role    string
	Started string
}

func ccvmSecondaryResizePCSStatus(cfg *CubeModel.ClusterConfigSection) (ccvmSecondaryResizePCSStatusValue, error) {
	resp := runCCVMLifecyclePCSAction(cfg, CCVMPCSControlRequest{
		Action:   "status",
		Resource: pcsDefaultResourceID,
	})
	if resp.Code != http.StatusOK {
		return ccvmSecondaryResizePCSStatusValue{}, fmt.Errorf("%s", firstNonEmpty(resp.Message, fmt.Sprint(resp.Val)))
	}
	status := ccvmSecondaryResizePCSStatusFromValue(resp.Val)
	if strings.TrimSpace(status.Role) == "" {
		return ccvmSecondaryResizePCSStatusValue{}, fmt.Errorf("cloudcenter_res status parse failed")
	}
	return status, nil
}

func ccvmSecondaryResizePCSStatusFromValue(value any) ccvmSecondaryResizePCSStatusValue {
	switch typed := value.(type) {
	case CCVMPCSStatusValue:
		return ccvmSecondaryResizePCSStatusValue{Role: typed.Role, Started: typed.Started}
	case map[string]any:
		return ccvmSecondaryResizePCSStatusValue{
			Role:    ccvmSecondaryResizeMapString(typed, "role"),
			Started: ccvmSecondaryResizeMapString(typed, "started"),
		}
	case map[string]string:
		return ccvmSecondaryResizePCSStatusValue{Role: typed["role"], Started: typed["started"]}
	default:
		return ccvmSecondaryResizePCSStatusValue{}
	}
}

func ccvmSecondaryResizeMapString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func waitCCVMSecondaryGuestAgentOnStartedTarget(cfg *CubeModel.ClusterConfigSection, timeout time.Duration) (ccvmSnapPCSTarget, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		status, err := ccvmSecondaryResizePCSStatus(cfg)
		if err == nil && strings.EqualFold(status.Role, "Started") && strings.TrimSpace(status.Started) != "" {
			target, ok := resolveCCVMSnapStartedTarget(cfg, status.Started)
			if ok && strings.TrimSpace(target.Target) != "" {
				if err := ccvmSecondaryResizePingTarget(target.Target, CCVMSecondaryResizeRequest{}); err == nil {
					return target, nil
				} else {
					lastErr = err
				}
			} else {
				lastErr = fmt.Errorf("cloudcenter_res started node not found in cluster.json: %s", status.Started)
			}
		} else if err != nil {
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return ccvmSnapPCSTarget{}, lastErr
			}
			return ccvmSnapPCSTarget{}, fmt.Errorf("ccvm guest agent not available")
		}
		time.Sleep(ccvmSecondaryResizePoll)
	}
}

func waitCCVMSecondaryGuestAgentLocal(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if err := ccvmSecondaryResizeGuestPing(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return lastErr
			}
			return fmt.Errorf("ccvm guest agent not available")
		}
		time.Sleep(ccvmSecondaryResizePoll)
	}
}

func ccvmSecondaryResizePingTarget(target string, req CCVMSecondaryResizeRequest) error {
	if isLocalTarget(target) || strings.EqualFold(strings.TrimSpace(target), "local") {
		return ccvmSecondaryResizeGuestPing()
	}
	resp, err := callCCVMSecondaryResizeRemote(target, req, ccvmSecondaryResizeModePing)
	if err != nil {
		return err
	}
	if resp.Code != http.StatusOK {
		return fmt.Errorf("%s", firstNonEmpty(resp.Message, resp.Val))
	}
	return nil
}

func ccvmSecondaryResizeGrowOnTarget(target string, req CCVMSecondaryResizeRequest) error {
	if isLocalTarget(target) || strings.EqualFold(strings.TrimSpace(target), "local") {
		return ccvmSecondaryResizeGrowGuestFilesystem()
	}
	resp, err := callCCVMSecondaryResizeRemote(target, req, ccvmSecondaryResizeModeGrow)
	if err != nil {
		return err
	}
	if resp.Code != http.StatusOK {
		return fmt.Errorf("%s", firstNonEmpty(resp.Message, resp.Val))
	}
	return nil
}

func callCCVMSecondaryResizeRemote(target string, req CCVMSecondaryResizeRequest, mode string) (CCVMSecondaryResizeResponse, error) {
	return callCCVMSecondaryResizeRemoteWithGuest(target, req, mode, false)
}

func callCCVMSecondaryResizeGuestRemote(target string, req CCVMSecondaryResizeRequest, mode string) (CCVMSecondaryResizeResponse, error) {
	return callCCVMSecondaryResizeRemoteWithGuest(target, req, mode, true)
}

func callCCVMSecondaryResizeRemoteWithGuest(target string, req CCVMSecondaryResizeRequest, mode string, guest bool) (CCVMSecondaryResizeResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return CCVMSecondaryResizeResponse{}, err
	}

	url := fmt.Sprintf("%s/api/v1/cube/ccvm/secondary/resize", buildTargetURL(target))
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return CCVMSecondaryResizeResponse{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(ccvmSecondaryResizeLocalHeader, "1")
	httpReq.Header.Set(ccvmSecondaryResizeModeHeader, mode)
	if guest {
		httpReq.Header.Set(ccvmSecondaryResizeGuestHeader, "1")
	}

	client := &http.Client{Timeout: ccvmSecondaryResizeRequestTO}
	resp, err := client.Do(httpReq)
	if err != nil {
		return CCVMSecondaryResizeResponse{}, err
	}
	defer resp.Body.Close()

	var out CCVMSecondaryResizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CCVMSecondaryResizeResponse{}, err
	}
	if out.Code == 0 {
		out.Code = resp.StatusCode
	}
	if strings.TrimSpace(out.Target) == "" {
		out.Target = target
	}
	return out, nil
}

func ccvmSecondaryResizeGuestPing() error {
	_, timedOut, err := libvirtinfra.RunGuestAgentCommand(ccvmSnapName, libvirtinfra.GuestAgentCommandRequest{Execute: "guest-ping"}, ccvmSecondaryResizeShortTO)
	if timedOut {
		return fmt.Errorf("ccvm guest agent ping timed out")
	}
	if err != nil {
		return err
	}
	return nil
}

func ccvmSecondaryResizeGrowGuestFilesystem() error {
	return ccvmSecondaryResizeGuestExec(ccvmSecondaryResizeGrowGuestFilesystemCommand(), ccvmSecondaryResizeTimeout)
}

func ccvmSecondaryResizeGrowGuestFilesystemDirect() error {
	_, err := runCCVMSecondaryResizeCommand(ccvmSecondaryResizeTimeout, "/bin/sh", "-c", ccvmSecondaryResizeGrowGuestFilesystemCommand())
	return err
}

func ccvmSecondaryResizeGrowGuestFilesystemCommand() string {
	return strings.Join([]string{
		"sgdisk -e /dev/vda",
		"parted --script /dev/vda resizepart 3 100%",
		"pvresize /dev/vda3",
		"lvextend -l +100%FREE /dev/rl/nfs",
		"xfs_growfs /nfs",
	}, " && ")
}

type ccvmSecondaryGuestExecResponse struct {
	Return struct {
		PID int `json:"pid"`
	} `json:"return"`
}

type ccvmSecondaryGuestExecStatusResponse struct {
	Return struct {
		Exited   bool   `json:"exited"`
		ExitCode int    `json:"exitcode"`
		OutData  string `json:"out-data"`
		ErrData  string `json:"err-data"`
	} `json:"return"`
}

func ccvmSecondaryResizeGuestExec(command string, timeout time.Duration) error {
	req := libvirtinfra.GuestAgentCommandRequest{
		Execute: "guest-exec",
		Arguments: map[string]any{
			"path":           "/bin/sh",
			"arg":            []string{"-c", command},
			"capture-output": true,
		},
	}
	resp, timedOut, err := libvirtinfra.RunGuestAgentCommand(ccvmSnapName, req, ccvmSecondaryResizeShortTO)
	if timedOut {
		return fmt.Errorf("ccvm guest exec timed out")
	}
	if err != nil {
		return err
	}
	var execResp ccvmSecondaryGuestExecResponse
	if err := json.Unmarshal([]byte(resp), &execResp); err != nil {
		return err
	}
	if execResp.Return.PID <= 0 {
		return fmt.Errorf("ccvm guest exec returned empty pid")
	}

	deadline := time.Now().Add(timeout)
	for {
		status, err := ccvmSecondaryResizeGuestExecStatus(execResp.Return.PID)
		if err != nil {
			return err
		}
		if status.Return.Exited {
			if status.Return.ExitCode == 0 {
				return nil
			}
			message := firstNonEmpty(
				ccvmSecondaryDecodeGuestOutput(status.Return.ErrData),
				ccvmSecondaryDecodeGuestOutput(status.Return.OutData),
				fmt.Sprintf("guest command exited with code %d", status.Return.ExitCode),
			)
			return fmt.Errorf("CCVM secondary filesystem expansion failed: %s", message)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("CCVM secondary filesystem expansion timed out")
		}
		time.Sleep(time.Second)
	}
}

func ccvmSecondaryResizeGuestExecStatus(pid int) (ccvmSecondaryGuestExecStatusResponse, error) {
	req := libvirtinfra.GuestAgentCommandRequest{
		Execute: "guest-exec-status",
		Arguments: map[string]any{
			"pid": pid,
		},
	}
	resp, timedOut, err := libvirtinfra.RunGuestAgentCommand(ccvmSnapName, req, ccvmSecondaryResizeShortTO)
	if timedOut {
		return ccvmSecondaryGuestExecStatusResponse{}, fmt.Errorf("ccvm guest exec status timed out")
	}
	if err != nil {
		return ccvmSecondaryGuestExecStatusResponse{}, err
	}
	var out ccvmSecondaryGuestExecStatusResponse
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		return ccvmSecondaryGuestExecStatusResponse{}, err
	}
	return out, nil
}

func ccvmSecondaryDecodeGuestOutput(value string) string {
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

func runCCVMSecondaryResizeCommand(timeout time.Duration, command string, args ...string) (string, error) {
	env := resetCloudCenterCommandEnv()
	if command == "virsh" {
		env = virshEnv()
	}
	out, timedOut, err := runCommandOutputWithEnv(command, timeout, env, args...)
	if timedOut {
		return out, fmt.Errorf("%s %s timed out after %s", command, strings.Join(args, " "), timeout)
	}
	if err != nil {
		return out, fmt.Errorf("%s %s failed: %s", command, strings.Join(args, " "), firstNonEmpty(out, err.Error()))
	}
	return out, nil
}

func ccvmSecondaryResizeOK(osType string, target string) CCVMSecondaryResizeResponse {
	return CCVMSecondaryResizeResponse{
		Code:    http.StatusOK,
		Val:     ccvmSecondaryResizeSuccess,
		RetName: ccvmSecondaryResizeRetName,
		Message: "ok",
		Target:  target,
		OSType:  osType,
	}
}

func ccvmSecondaryResizeError(osType string, target string, message string) CCVMSecondaryResizeResponse {
	return CCVMSecondaryResizeResponse{
		Code:    http.StatusInternalServerError,
		Val:     message,
		RetName: ccvmSecondaryResizeRetName,
		Message: message,
		Target:  target,
		OSType:  osType,
	}
}

func statusCodeFromCCVMSecondaryResizeResponse(resp CCVMSecondaryResizeResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
