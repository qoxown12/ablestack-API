package cube

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type CCVMCloudInitCreateRequest = CubeModel.CCVMCloudInitCreateRequest

const (
	ccvmCloudInitISOPath        = "/var/lib/libvirt/images/ccvm-cloudinit.iso"
	ccvmCloudInitHostname       = "ccvm"
	ccvmCloudInitSuccessMessage = "ccvm cloudinit iso create success"
	ccvmCloudInitCopyTimeout    = 2 * time.Minute
	ccvmCloudInitCopyRetries    = 3
)

// CreateCCVMCloudInit godoc
//
//	@Summary		Create CCVM Cloud-Init ISO
//	@Description	cluster.json과 선택 입력된 service network 정보를 기준으로 CCVM cloud-init ISO를 생성하고 대상 ablecube host에 복사합니다.
//	@Tags			Cube-CCVM
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.CCVMCloudInitCreateRequest	false	"ccvm cloud-init generate request"
//	@Success		200	{object}	CubeModel.GenCloudInitResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/cloudinit/ccvm/generate [post]
func CreateCCVMCloudInit(context *gin.Context) {
	started := time.Now()
	clientIP, method, path := ccvmCloudInitRequestInfo(context)
	appendAbleStackAPILog("ccvm_cloudinit", "event=start client=%q method=%s path=%s", clientIP, method, path)

	var req CCVMCloudInitCreateRequest
	if context.Request.Body != nil && context.Request.ContentLength != 0 {
		if err := context.ShouldBindJSON(&req); err != nil {
			appendAbleStackAPILog("ccvm_cloudinit", "event=invalid_request client=%q error=%q elapsed=%s", clientIP, err.Error(), time.Since(started))
			context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
				ErrCode: http.StatusBadRequest,
				Message: "invalid request",
			})
			return
		}
	}
	if err := normalizeCCVMCloudInitCreateRequest(&req); err != nil {
		appendAbleStackAPILog("ccvm_cloudinit", "event=bad_request client=%q error=%q elapsed=%s", clientIP, err.Error(), time.Since(started))
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	cfg, err := loadClusterConfigSection()
	if err != nil {
		appendAbleStackAPILog("ccvm_cloudinit", "event=cluster_config_failed client=%q error=%q elapsed=%s", clientIP, err.Error(), time.Since(started))
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}

	genReq, err := buildCreateCCVMCloudInitRequest(cfg, req)
	if err != nil {
		appendAbleStackAPILog("ccvm_cloudinit", "event=build_request_failed client=%q error=%q elapsed=%s", clientIP, err.Error(), time.Since(started))
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	targets, targetField := ccvmCloudInitCopyTargets(cfg)
	appendAbleStackAPILog(
		"ccvm_cloudinit",
		"event=targets client=%q cluster_type=%q iscsi_storage=%q target_field=%s target_count=%d targets=%q",
		clientIP,
		strings.TrimSpace(cfg.Type),
		strings.TrimSpace(cfg.IscsiStorage),
		targetField,
		len(targets),
		formatHosts(targets),
	)
	if len(targets) == 0 {
		appendAbleStackAPILog("ccvm_cloudinit", "event=no_targets client=%q target_field=%s elapsed=%s", clientIP, targetField, time.Since(started))
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: targetField + " required",
		})
		return
	}

	resp := runGenCloudInit(genReq, cfg)
	if resp.Code != http.StatusOK {
		appendAbleStackAPILog("ccvm_cloudinit", "event=generate_failed client=%q code=%d message=%q elapsed=%s", clientIP, resp.Code, resp.Message, time.Since(started))
		context.JSON(statusCodeFromGenCloudInitResponse(resp), resp)
		return
	}

	if err := copyCCVMCloudInitISOToTargets(targets); err != nil {
		appendAbleStackAPILog("ccvm_cloudinit", "event=copy_failed client=%q target_field=%s target_count=%d error=%q elapsed=%s", clientIP, targetField, len(targets), err.Error(), time.Since(started))
		resp.Code = http.StatusInternalServerError
		resp.Message = err.Error()
		context.JSON(http.StatusInternalServerError, resp)
		return
	}

	resp.Message = ccvmCloudInitSuccessMessage
	appendAbleStackAPILog("ccvm_cloudinit", "event=success client=%q iso_path=%q target_field=%s target_count=%d elapsed=%s", clientIP, ccvmCloudInitISOPath, targetField, len(targets), time.Since(started))
	context.JSON(http.StatusOK, resp)
}

func ccvmCloudInitRequestInfo(context *gin.Context) (string, string, string) {
	if context == nil || context.Request == nil {
		return "", "", ""
	}
	path := context.FullPath()
	if path == "" && context.Request.URL != nil {
		path = context.Request.URL.Path
	}
	return context.ClientIP(), context.Request.Method, path
}

func normalizeCCVMCloudInitCreateRequest(req *CCVMCloudInitCreateRequest) error {
	if req == nil {
		return nil
	}
	req.SNNIC = strings.TrimSpace(req.SNNIC)
	req.SNIP = strings.TrimSpace(req.SNIP)
	req.SNGW = strings.TrimSpace(req.SNGW)
	req.SNDNS = strings.TrimSpace(req.SNDNS)

	hasServiceNetwork := req.SNNIC != "" || req.SNIP != "" || req.SNPrefix != 0 || req.SNGW != "" || req.SNDNS != ""
	if !hasServiceNetwork {
		return nil
	}
	if req.SNNIC == "" || req.SNIP == "" || req.SNPrefix <= 0 {
		return fmt.Errorf("sn_nic, sn_ip and sn_prefix are required when service network is provided")
	}
	return nil
}

func buildCreateCCVMCloudInitRequest(cfg *CubeModel.ClusterConfigSection, req CCVMCloudInitCreateRequest) (GenCloudInitRequest, error) {
	if cfg == nil {
		return GenCloudInitRequest{}, fmt.Errorf("cluster config not found")
	}
	prefix, err := parseCloudInitPrefix(cfg.MngtNic.CIDR)
	if err != nil {
		return GenCloudInitRequest{}, err
	}
	ccvmIP := strings.TrimSpace(cfg.CCVM.IP)
	if ccvmIP == "" {
		return GenCloudInitRequest{}, fmt.Errorf("ccvm.ip required")
	}

	genReq := GenCloudInitRequest{
		Type:       "ccvm",
		ISOPath:    ccvmCloudInitISOPath,
		Hostname:   ccvmCloudInitHostname,
		PubKey:     cloudInitPubKeyPath,
		PrivKey:    cloudInitPrivKeyPath,
		Hosts:      cloudInitHostsPath,
		MgmtNIC:    cloudInitMgmtNIC,
		MgmtIP:     ccvmIP,
		MgmtPrefix: prefix,
		MgmtGW:     strings.TrimSpace(cfg.MngtNic.GW),
		DNS:        strings.TrimSpace(cfg.MngtNic.DNS),
		SNNIC:      req.SNNIC,
		SNIP:       req.SNIP,
		SNPrefix:   req.SNPrefix,
		SNGW:       req.SNGW,
		SNDNS:      req.SNDNS,
	}
	if err := normalizeGenCloudInitRequest(&genReq); err != nil {
		return GenCloudInitRequest{}, err
	}
	return genReq, nil
}

func ccvmCloudInitCopyTargets(cfg *CubeModel.ClusterConfigSection) ([]string, string) {
	const (
		ablecubeField   = "hosts[].ablecube"
		ablecubePnField = "hosts[].ablecubePn"
	)
	if cfg == nil {
		return nil, ablecubePnField
	}

	useAblecube := strings.EqualFold(strings.TrimSpace(cfg.Type), "ablestack-vm") &&
		!strings.EqualFold(strings.TrimSpace(cfg.IscsiStorage), "true")
	targetField := ablecubePnField
	if useAblecube {
		targetField = ablecubeField
	}

	targets := make([]string, 0, len(cfg.Hosts))
	for _, host := range cfg.Hosts {
		target := host.AblecubePn
		if useAblecube {
			target = host.Ablecube
		}
		if target = strings.TrimSpace(target); target != "" {
			targets = append(targets, target)
		}
	}
	return dedupeHosts(targets), targetField
}

func copyCCVMCloudInitISOToTargets(targets []string) error {
	for _, target := range targets {
		if isLocalTarget(target) {
			appendAbleStackAPILog("ccvm_cloudinit", "event=copy_skip_local target=%q", target)
			continue
		}
		appendAbleStackAPILog("ccvm_cloudinit", "event=copy_start target=%q iso_path=%q", target, ccvmCloudInitISOPath)
		if err := copyCCVMCloudInitISOToTarget(target); err != nil {
			appendAbleStackAPILog("ccvm_cloudinit", "event=copy_target_failed target=%q error=%q", target, err.Error())
			return err
		}
		appendAbleStackAPILog("ccvm_cloudinit", "event=copy_target_success target=%q", target)
	}
	return nil
}

func copyCCVMCloudInitISOToTarget(target string) error {
	var lastErr error
	for i := 0; i < ccvmCloudInitCopyRetries; i++ {
		out, timedOut, err := runCommandOutputWithEnv(
			"scp",
			ccvmCloudInitCopyTimeout,
			scvmUpdateCommandEnv(),
			"-q",
			ccvmCloudInitISOPath,
			fmt.Sprintf("root@%s:%s", target, ccvmCloudInitISOPath),
		)
		if !timedOut && err == nil {
			return nil
		}
		if timedOut {
			lastErr = fmt.Errorf("%s : ccvm-cloudinit.iso copy timed out after %s", target, ccvmCloudInitCopyTimeout)
		} else {
			lastErr = fmt.Errorf("%s : ccvm-cloudinit.iso copy failed: %s", target, firstNonEmpty(strings.TrimSpace(out), err.Error()))
		}
	}
	return lastErr
}
