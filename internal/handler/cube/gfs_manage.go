package cube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type GFSManageRequest = CubeModel.GFSManageRequest
type GFSManageResponse = CubeModel.GFSManageResponse
type GFSManageTargetResult = CubeModel.GFSManageTargetResult
type GFSManageVolumeGroup = CubeModel.GFSManageVolumeGroup
type GFSManageStonithDevice = CubeModel.GFSManageStonithDevice

const (
	gfsManageLocalHeader      = "X-Cube-GFS-Manage-Local"
	gfsManageCommandTimeout   = 5 * time.Minute
	gfsManageRemoteTimeout    = 15 * time.Minute
	gfsManageShortTimeout     = 30 * time.Second
	gfsManageLVMConfPath      = "/etc/lvm/lvm.conf"
	gfsManageAlertLogPath     = "/var/log/pcmk_alert_file.log"
	gfsManageAlertDetailPath  = "/var/log/pcmk_alert_detail.log"
	gfsManageHCIFilesystem    = "ablestack-hci-filesystem"
	gfsManageResourceCleanup  = "resource-cleanup"
	gfsManagePrepareAlertFile = "prepare-alert-file"
)

type gfsManageTarget struct {
	Hostname string
	Target   string
}

type gfsManageVGReport struct {
	Report []struct {
		VG []struct {
			VGName string `json:"vg_name"`
		} `json:"vg"`
	} `json:"report"`
}

type gfsManageLSBLKPayload struct {
	Blockdevices []gfsManageBlockDevice `json:"blockdevices"`
}

type gfsManageBlockDevice struct {
	Name       string                 `json:"name"`
	Path       string                 `json:"path"`
	Type       string                 `json:"type"`
	Mountpoint string                 `json:"mountpoint"`
	Children   []gfsManageBlockDevice `json:"children,omitempty"`
}

type gfsManageDevicePaths struct {
	Disks      []string
	Partitions []string
	LVPaths    []string
	MapNames   []string
	BlockNames []string
}

// GFSManage godoc
//
//	@Summary		GFS Manage
//	@Description	GFS/PCS 로컬 작업을 수행하거나 cluster.json hosts[].ablecube 대상 API로 fan-out 합니다. SSH는 사용하지 않습니다.
//	@Tags			Cube-GFS
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.GFSManageRequest	true	"gfs manage request"
//	@Success		200	{object}	CubeModel.GFSManageResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/gfs/manage [post]
func GFSManage(context *gin.Context) {
	var req GFSManageRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := normalizeGFSManageRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	if isGFSManageLocalRequest(context) {
		resp := runGFSManageLocal(req, "local", nil)
		context.JSON(statusCodeFromGFSManageResponse(resp), resp)
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

	resp := runGFSManage(req, cfg)
	context.JSON(statusCodeFromGFSManageResponse(resp), resp)
}

func normalizeGFSManageRequest(req *GFSManageRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}

	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "init-pcs-cluster", "init":
		req.Action = "init-pcs-cluster"
	case "modify-lvm-conf":
		req.Action = "modify-lvm-conf"
		if req.UseLVMLockd == nil {
			value := true
			req.UseLVMLockd = &value
		}
	case "partprobe":
		req.Action = "partprobe"
	case "lvmdevices-add":
		req.Action = "lvmdevices-add"
	case gfsManageResourceCleanup:
		req.Action = gfsManageResourceCleanup
	case "check-host":
		req.Action = "check-host"
	case "check-stonith":
		req.Action = "check-stonith"
		req.Control = strings.ToLower(strings.TrimSpace(req.Control))
		if req.Control == "" {
			req.Control = "check"
		}
	case "check-ipmi":
		req.Action = "check-ipmi"
	case "set-alert":
		req.Action = "set-alert"
	case gfsManagePrepareAlertFile:
		req.Action = gfsManagePrepareAlertFile
	case "list-gfs":
		req.Action = "list-gfs"
	case "delete-gfs":
		req.Action = "delete-gfs"
	case "rescan":
		req.Action = "rescan"
	case "extend":
		req.Action = "extend"
	case "scan":
		req.Action = "scan"
	case "add-extend":
		req.Action = "add-extend"
	default:
		return fmt.Errorf("unsupported action")
	}

	req.Disk = strings.TrimSpace(req.Disk)
	req.Disks = normalizeStringSlice(append(req.Disks, splitCommaValues(req.Disk)...))
	req.VGName = strings.TrimSpace(req.VGName)
	req.LVName = strings.TrimSpace(req.LVName)
	req.GFSName = strings.TrimSpace(req.GFSName)
	req.MountPoint = strings.TrimSpace(req.MountPoint)
	req.NonStopCheck = strings.ToLower(strings.TrimSpace(req.NonStopCheck))
	req.VolumeGroups = normalizeGFSManageVolumeGroups(req.VolumeGroups, req.VGName, req.LVName)
	req.Stonith = normalizeGFSManageStonithDevices(req.Stonith)

	if req.Action == "init-pcs-cluster" && len(req.Disks) > 0 && len(req.VolumeGroups) == 0 {
		return fmt.Errorf("volume_groups or vg_name/lv_name required when disks are provided")
	}
	if req.Action == "lvmdevices-add" && len(req.Disks) == 0 {
		return fmt.Errorf("disks required")
	}
	if req.Action == "check-stonith" {
		switch req.Control {
		case "check", "enable", "disable", "security-disable", "security-enable":
		default:
			return fmt.Errorf("unsupported stonith control")
		}
	}
	if req.Action == "check-ipmi" && len(req.Stonith) == 0 {
		return fmt.Errorf("stonith required")
	}
	switch req.Action {
	case "delete-gfs":
		if req.GFSName == "" || req.VGName == "" || req.LVName == "" || len(req.Disks) == 0 {
			return fmt.Errorf("disks, gfs_name, vg_name and lv_name required")
		}
	case "rescan", "extend":
		if req.VGName == "" || req.LVName == "" || req.MountPoint == "" {
			return fmt.Errorf("vg_name, lv_name and mount_point required")
		}
	case "add-extend":
		if req.VGName == "" || req.LVName == "" || req.MountPoint == "" || req.GFSName == "" || len(req.Disks) == 0 {
			return fmt.Errorf("disks, gfs_name, vg_name, lv_name and mount_point required")
		}
	}
	return nil
}

func normalizeGFSManageVolumeGroups(values []GFSManageVolumeGroup, vgName string, lvName string) []GFSManageVolumeGroup {
	if vgName != "" || lvName != "" {
		values = append(values, GFSManageVolumeGroup{VGName: vgName, LVName: lvName})
	}
	seen := map[string]struct{}{}
	out := make([]GFSManageVolumeGroup, 0, len(values))
	for _, value := range values {
		value.VGName = strings.TrimSpace(value.VGName)
		value.LVName = strings.TrimSpace(value.LVName)
		if value.VGName == "" || value.LVName == "" {
			continue
		}
		key := value.VGName + "|" + value.LVName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeGFSManageStonithDevices(values []GFSManageStonithDevice) []GFSManageStonithDevice {
	out := make([]GFSManageStonithDevice, 0, len(values))
	for _, value := range values {
		value.IPAddr = strings.TrimSpace(value.IPAddr)
		value.IPPort = strings.TrimSpace(value.IPPort)
		value.Login = strings.TrimSpace(value.Login)
		value.Passwd = strings.TrimSpace(value.Passwd)
		if value.IPAddr == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func splitCommaValues(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	raw := strings.Split(value, ",")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func isGFSManageLocalRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(gfsManageLocalHeader)), "1")
}

func runGFSManage(req GFSManageRequest, cfg *CubeModel.ClusterConfigSection) GFSManageResponse {
	switch req.Action {
	case "init-pcs-cluster":
		target := selectGFSManageExecutionTarget(cfg)
		if target.Target == "" || target.Target == "local" || isLocalTarget(target.Target) || isGFSManageLocalHostname(target.Hostname) {
			return runGFSManageLocal(req, firstNonEmpty(target.Target, "local"), cfg)
		}
		resp, err := callGFSManageRemote(&http.Client{Timeout: gfsManageRemoteTimeout}, target, req)
		if err != nil {
			return gfsManageError(req, target.Target, err.Error(), nil)
		}
		if strings.EqualFold(resp.Target, "local") {
			resp.Target = target.Target
		}
		return resp
	case "set-alert":
		return runGFSManageSetAlert(req, cfg)
	case "delete-gfs", "extend", "add-extend":
		target := selectGFSManageExecutionTarget(cfg)
		if target.Target == "" || target.Target == "local" || isLocalTarget(target.Target) || isGFSManageLocalHostname(target.Hostname) {
			return runGFSManageLocal(req, firstNonEmpty(target.Target, "local"), cfg)
		}
		resp, err := callGFSManageRemote(&http.Client{Timeout: gfsManageRemoteTimeout}, target, req)
		if err != nil {
			return gfsManageError(req, target.Target, err.Error(), nil)
		}
		if strings.EqualFold(resp.Target, "local") {
			resp.Target = target.Target
		}
		return resp
	case "modify-lvm-conf", "partprobe", "lvmdevices-add", gfsManageResourceCleanup, gfsManagePrepareAlertFile:
		return runGFSManageFanout(req, cfg)
	case "scan", "rescan":
		return runGFSManageFanout(req, cfg)
	case "check-host":
		return gfsManageOK(req, "local", gfsManageSortedHosts(cfg), nil)
	case "check-stonith", "check-ipmi", "list-gfs":
		return runGFSManageLocal(req, "local", cfg)
	default:
		return gfsManageError(req, "local", "unsupported action", nil)
	}
}

func runGFSManageFanout(req GFSManageRequest, cfg *CubeModel.ClusterConfigSection) GFSManageResponse {
	targets := buildGFSManageTargets(cfg)
	if len(targets) == 0 {
		return gfsManageError(req, "local", "hosts[].ablecube required", nil)
	}

	client := &http.Client{Timeout: gfsManageRemoteTimeout}
	results := make([]GFSManageTargetResult, 0, len(targets))
	for _, target := range targets {
		result := runGFSManageOnTarget(client, target, req, cfg)
		results = append(results, result)
	}
	if err := firstGFSManageResultError(results); err != nil {
		return gfsManageError(req, "fanout", err.Error(), results)
	}
	return gfsManageOK(req, "fanout", "ok", results)
}

func runGFSManageSetAlert(req GFSManageRequest, cfg *CubeModel.ClusterConfigSection) GFSManageResponse {
	prepareReq := GFSManageRequest{Action: gfsManagePrepareAlertFile}
	prepareResp := runGFSManageFanout(prepareReq, cfg)
	if prepareResp.Code != http.StatusOK {
		return gfsManageError(req, "fanout", firstNonEmpty(prepareResp.Message, "alert file prepare failed"), prepareResp.Results)
	}

	target := selectGFSManageExecutionTarget(cfg)
	var setupResp GFSManageResponse
	if target.Target == "" || target.Target == "local" || isLocalTarget(target.Target) || isGFSManageLocalHostname(target.Hostname) {
		setupResp = runGFSManageLocal(req, firstNonEmpty(target.Target, "local"), cfg)
	} else {
		var err error
		setupResp, err = callGFSManageRemote(&http.Client{Timeout: gfsManageRemoteTimeout}, target, req)
		if err != nil {
			return gfsManageError(req, target.Target, err.Error(), prepareResp.Results)
		}
	}
	if setupResp.Code != http.StatusOK {
		return gfsManageError(req, firstNonEmpty(setupResp.Target, target.Target), firstNonEmpty(setupResp.Message, "pcs alert setup failed"), prepareResp.Results)
	}
	return gfsManageOK(req, firstNonEmpty(setupResp.Target, target.Target, "local"), map[string]any{
		"prepare": prepareResp.Results,
		"setup":   setupResp.Val,
	}, prepareResp.Results)
}

func runGFSManageOnTarget(client *http.Client, target gfsManageTarget, req GFSManageRequest, cfg *CubeModel.ClusterConfigSection) GFSManageTargetResult {
	if target.Target == "" || isLocalTarget(target.Target) || isGFSManageLocalHostname(target.Hostname) {
		resp := runGFSManageLocal(req, firstNonEmpty(target.Target, "local"), cfg)
		return gfsManageTargetResult(target, resp)
	}
	resp, err := callGFSManageRemote(client, target, req)
	if err != nil {
		return GFSManageTargetResult{
			Hostname: target.Hostname,
			Target:   target.Target,
			Code:     http.StatusInternalServerError,
			Message:  err.Error(),
		}
	}
	return gfsManageTargetResult(target, resp)
}

func runGFSManageLocal(req GFSManageRequest, target string, cfg *CubeModel.ClusterConfigSection) GFSManageResponse {
	switch req.Action {
	case "init-pcs-cluster":
		if cfg == nil {
			var err error
			cfg, err = loadClusterConfigSection()
			if err != nil {
				return gfsManageError(req, target, err.Error(), nil)
			}
		}
		return runGFSManageInitPCSCluster(req, cfg, target)
	case "modify-lvm-conf":
		useLVMLockd := true
		if req.UseLVMLockd != nil {
			useLVMLockd = *req.UseLVMLockd
		}
		if err := modifyGFSManageLVMConf(useLVMLockd); err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		return gfsManageOK(req, target, "Modify Lvm Conf Success", nil)
	case "partprobe":
		if err := runGFSManagePartprobe(req.Disks); err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		return gfsManageOK(req, target, "Partprobe Success", nil)
	case "lvmdevices-add":
		if cfg == nil {
			loaded, err := loadClusterConfigSection()
			if err == nil {
				cfg = loaded
			}
		}
		osType := ""
		if cfg != nil {
			osType = cfg.Type
		}
		if err := runGFSManageLVMDevicesAdd(req.Disks, osType); err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		return gfsManageOK(req, target, "Lvmdevices Add Success", nil)
	case gfsManageResourceCleanup:
		_, _ = runGFSManageCommandIgnore("pcs", "resource", "cleanup")
		return gfsManageOK(req, target, "Resource Cleanup Success", nil)
	case "check-host":
		if cfg == nil {
			var err error
			cfg, err = loadClusterConfigSection()
			if err != nil {
				return gfsManageError(req, target, err.Error(), nil)
			}
		}
		return gfsManageOK(req, target, gfsManageSortedHosts(cfg), nil)
	case "check-stonith":
		val, err := runGFSManageStonithControl(req.Control)
		if err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		return gfsManageOK(req, target, val, nil)
	case "check-ipmi":
		val, err := runGFSManageIPMICheck(req.Stonith)
		if err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		return gfsManageOK(req, target, val, nil)
	case "set-alert":
		if err := prepareGFSManageAlertFile(); err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		if err := createGFSManageAlert(); err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		return gfsManageOK(req, target, "Pcs Alert Success", nil)
	case gfsManagePrepareAlertFile:
		if err := prepareGFSManageAlertFile(); err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		return gfsManageOK(req, target, "Alert File Prepare Success", nil)
	case "list-gfs":
		vgs, err := listGFSManageVolumeGroups()
		if err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		return gfsManageOK(req, target, vgs, nil)
	case "delete-gfs":
		if cfg == nil {
			loaded, err := loadClusterConfigSection()
			if err == nil {
				cfg = loaded
			}
		}
		if err := deleteGFSManageDisk(req, cfg); err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		return gfsManageOK(req, target, "Success to gfs delete", nil)
	case "scan":
		if err := scanGFSManageSCSIHosts(); err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		return gfsManageOK(req, target, "Success to scan GFS Disk", nil)
	case "rescan":
		if err := rescanGFSManageDisk(req.VGName, req.LVName); err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		return gfsManageOK(req, target, "Success to scan GFS Disk", nil)
	case "extend":
		if cfg == nil {
			loaded, err := loadClusterConfigSection()
			if err == nil {
				cfg = loaded
			}
		}
		if err := extendGFSManageDisk(req, cfg); err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		return gfsManageOK(req, target, "Success to extend GFS Disk", nil)
	case "add-extend":
		if cfg == nil {
			loaded, err := loadClusterConfigSection()
			if err == nil {
				cfg = loaded
			}
		}
		if err := addExtendGFSManageDisk(req, cfg); err != nil {
			return gfsManageError(req, target, err.Error(), nil)
		}
		return gfsManageOK(req, target, "Success to Extend Add GFS Disk", nil)
	default:
		return gfsManageError(req, target, "unsupported action", nil)
	}
}

func runGFSManageInitPCSCluster(req GFSManageRequest, cfg *CubeModel.ClusterConfigSection, target string) GFSManageResponse {
	targets := buildGFSManageTargets(cfg)
	if len(targets) == 0 {
		return gfsManageError(req, target, "hosts[].ablecube required", nil)
	}

	if len(targets) == 1 {
		_, _ = runGFSManageCommandIgnore("pcs", "cluster", "stop")
		_, _ = runGFSManageCommandIgnore("pcs", "cluster", "destroy")
	} else {
		_, _ = runGFSManageCommandIgnore("pcs", "cluster", "stop", "--all")
		_, _ = runGFSManageCommandIgnore("pcs", "cluster", "destroy", "--all")
	}

	steps := make([]GFSManageTargetResult, 0)
	if len(req.Disks) > 0 && len(req.VolumeGroups) > 0 {
		disabled := false
		lvmReq := GFSManageRequest{Action: "modify-lvm-conf", UseLVMLockd: &disabled}
		lvmResults := runGFSManageFanout(lvmReq, cfg).Results
		steps = append(steps, lvmResults...)
		if err := firstGFSManageResultError(lvmResults); err != nil {
			return gfsManageError(req, target, err.Error(), steps)
		}

		for _, vg := range req.VolumeGroups {
			if err := cleanupGFSManageVolumeGroup(vg); err != nil {
				return gfsManageError(req, target, err.Error(), steps)
			}
		}

		for _, disk := range req.Disks {
			cleanupGFSManageDisk(disk, cfg.Type)
		}

		probeReq := GFSManageRequest{Action: "partprobe", Disks: req.Disks}
		probeResults := runGFSManageFanout(probeReq, cfg).Results
		steps = append(steps, probeResults...)
		if err := firstGFSManageResultError(probeResults); err != nil {
			return gfsManageError(req, target, err.Error(), steps)
		}

		cleanupReq := GFSManageRequest{Action: gfsManageResourceCleanup}
		cleanupResults := runGFSManageFanout(cleanupReq, cfg).Results
		steps = append(steps, cleanupResults...)
	}

	return gfsManageOK(req, target, "Init PCS Cluster Success", steps)
}

func buildGFSManageTargets(cfg *CubeModel.ClusterConfigSection) []gfsManageTarget {
	if cfg == nil {
		return nil
	}
	seen := map[string]struct{}{}
	targets := make([]gfsManageTarget, 0, len(cfg.Hosts))
	for _, host := range cfg.Hosts {
		target := strings.TrimSpace(host.Ablecube)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, gfsManageTarget{
			Hostname: strings.TrimSpace(host.Hostname),
			Target:   target,
		})
	}
	return targets
}

func selectGFSManageExecutionTarget(cfg *CubeModel.ClusterConfigSection) gfsManageTarget {
	targets := buildGFSManageTargets(cfg)
	if len(targets) == 0 {
		return gfsManageTarget{Target: "local"}
	}
	for _, target := range targets {
		if isLocalTarget(target.Target) || isGFSManageLocalHostname(target.Hostname) {
			return target
		}
	}
	client := &http.Client{Timeout: 2 * time.Second}
	for _, target := range targets {
		if err := callHealthTarget(client, target.Target); err == nil {
			return target
		}
	}
	return targets[0]
}

func isGFSManageLocalHostname(hostname string) bool {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "" {
		return false
	}
	localName, err := os.Hostname()
	if err != nil {
		return false
	}
	localName = strings.ToLower(strings.TrimSpace(localName))
	shortName := strings.SplitN(localName, ".", 2)[0]
	return hostname == localName || hostname == shortName
}

func callGFSManageRemote(client *http.Client, target gfsManageTarget, req GFSManageRequest) (GFSManageResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return GFSManageResponse{}, err
	}
	url := fmt.Sprintf("%s/api/v1/cube/gfs/manage", buildTargetURL(target.Target))
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return GFSManageResponse{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(gfsManageLocalHeader, "1")

	resp, err := client.Do(httpReq)
	if err != nil {
		return GFSManageResponse{}, err
	}
	defer resp.Body.Close()

	var out GFSManageResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		if resp.StatusCode >= 300 {
			return GFSManageResponse{}, fmt.Errorf("gfs manage failed: %s", resp.Status)
		}
		return GFSManageResponse{}, err
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

func modifyGFSManageLVMConf(useLVMLockd bool) error {
	data, err := os.ReadFile(gfsManageLVMConfPath)
	if err != nil {
		return err
	}
	content := string(data)
	if useLVMLockd {
		content = replaceGFSManageLVMConf(content, "# use_lvmlockd = 0", "use_lvmlockd = 1")
		content = replaceGFSManageLVMConf(content, "use_lvmlockd = 0", "use_lvmlockd = 1")
		content = replaceGFSManageLVMConf(content, "# use_devicesfile = 0", "use_devicesfile = 1")
		content = replaceGFSManageLVMConf(content, "use_devicesfile = 0", "use_devicesfile = 1")
	} else {
		content = replaceGFSManageLVMConf(content, "use_lvmlockd = 1", "# use_lvmlockd = 0")
		content = replaceGFSManageLVMConf(content, "use_devicesfile = 1", "use_devicesfile = 0")
	}
	if string(data) != content {
		if err := os.WriteFile(gfsManageLVMConfPath, []byte(content), 0644); err != nil {
			return err
		}
	}
	if useLVMLockd {
		_, err = runGFSManageCommand(gfsManageShortTimeout, "mpathconf", "--enable")
		return err
	}
	return nil
}

func replaceGFSManageLVMConf(content string, oldValue string, newValue string) string {
	return strings.ReplaceAll(content, oldValue, newValue)
}

func cleanupGFSManageVolumeGroup(vg GFSManageVolumeGroup) error {
	lvPath, exists, err := findGFSManageLVPath(vg.VGName, vg.LVName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	_, _ = runGFSManageCommandIgnore("vgchange", "--lock-type", "none", "--lock-opt", "force", vg.VGName, "-y")
	_, _ = runGFSManageCommandIgnore("vgchange", "-aey", vg.VGName)
	_, _ = runGFSManageCommandIgnore("lvremove", "--lockopt", "skiplv", lvPath, "-y")
	if _, err := runGFSManageCommand(gfsManageCommandTimeout, "vgremove", vg.VGName, "-y"); err != nil {
		return err
	}
	return nil
}

func findGFSManageLVPath(vgName string, lvName string) (string, bool, error) {
	if vgName == "" || lvName == "" {
		return "", false, nil
	}
	devPath := "/dev/" + vgName + "/" + lvName
	if _, err := os.Stat(devPath); err == nil {
		return devPath, true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", false, err
	}

	mapperPath := "/dev/mapper/" + strings.ReplaceAll(vgName, "-", "--") + "-" + strings.ReplaceAll(lvName, "-", "--")
	if _, err := os.Stat(mapperPath); err == nil {
		return mapperPath, true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", false, err
	}

	out, timedOut, err := runCommandOutputWithEnv("lvdisplay", gfsManageShortTimeout, gfsManageCommandEnv(), "-c")
	if timedOut {
		return "", false, fmt.Errorf("lvdisplay timed out after %s", gfsManageShortTimeout)
	}
	if err != nil && strings.TrimSpace(out) == "" {
		return "", false, nil
	}
	needle := "/" + vgName + "/" + lvName
	for _, line := range splitLines(out) {
		fields := strings.Split(line, ":")
		if len(fields) == 0 {
			continue
		}
		path := strings.TrimSpace(fields[0])
		if strings.Contains(path, needle) {
			return path, true, nil
		}
	}
	return "", false, nil
}

func cleanupGFSManageDisk(disk string, osType string) {
	disk = strings.TrimSpace(disk)
	if disk == "" {
		return
	}
	for _, partition := range gfsManagePartitionCandidates(disk, osType) {
		_, _ = runGFSManageCommandIgnore("pvremove", "-ff", "--yes", partition)
	}
	_, _ = runGFSManageCommandIgnore("parted", "-s", disk, "rm", "1")
}

func gfsManagePartitionCandidates(disk string, osType string) []string {
	disk = strings.TrimSpace(disk)
	if disk == "" {
		return nil
	}
	candidates := []string{}
	if strings.EqualFold(strings.TrimSpace(osType), gfsManageHCIFilesystem) {
		candidates = append(candidates, disk+"-part1", disk+"p1", disk+"1")
	} else if strings.Contains(disk, "dm-uuid-mpath-") {
		candidates = append(candidates, strings.Replace(disk, "dm-uuid-mpath-", "dm-uuid-part1-mpath-", 1))
	} else if strings.Contains(strings.ToLower(disk), "mpath") {
		candidates = append(candidates, disk+"1", disk+"p1")
	} else if lastGFSManageCharIsDigit(disk) {
		candidates = append(candidates, disk+"p1", disk+"-part1")
	} else {
		candidates = append(candidates, disk+"1")
	}
	return normalizeStringSlice(candidates)
}

func lastGFSManageCharIsDigit(value string) bool {
	if value == "" {
		return false
	}
	last := value[len(value)-1]
	return last >= '0' && last <= '9'
}

func runGFSManagePartprobe(disks []string) error {
	targets := normalizeStringSlice(disks)
	if len(targets) == 0 {
		targets = listGFSManageLocalDisks()
	}
	for _, disk := range targets {
		_, _ = runGFSManageCommandIgnore("partprobe", disk)
	}
	return nil
}

func runGFSManageLVMDevicesAdd(disks []string, osType string) error {
	for _, disk := range disks {
		_, _ = runGFSManageCommandIgnore("partprobe", disk)
		for _, partition := range gfsManagePartitionCandidates(disk, osType) {
			_, _ = runGFSManageCommandIgnore("lvmdevices", "--adddev", partition)
		}
	}
	return nil
}

func listGFSManageLocalDisks() []string {
	out, timedOut, err := runCommandOutputWithEnv("lsblk", gfsManageShortTimeout, gfsManageCommandEnv(), "-r", "-n", "-o", "NAME,TYPE", "-d")
	if timedOut || err != nil {
		return nil
	}
	disks := make([]string, 0)
	for _, line := range splitLines(out) {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] == "rom" {
			continue
		}
		disks = append(disks, "/dev/"+fields[0])
	}
	return disks
}

func runGFSManageStonithControl(control string) (any, error) {
	out, err := runGFSManageCommand(gfsManageShortTimeout, "pcs", "stonith", "status")
	if err != nil && control == "check" {
		return nil, err
	}
	if control == "check" {
		return strings.TrimSpace(out), nil
	}

	resourceIDs := parseGFSManageStonithResourceIDs(out)
	for _, resourceID := range resourceIDs {
		switch control {
		case "enable", "security-enable":
			if _, err := runGFSManageCommand(gfsManageShortTimeout, "pcs", "stonith", "enable", resourceID); err != nil {
				return nil, err
			}
		case "disable", "security-disable":
			if _, err := runGFSManageCommand(gfsManageShortTimeout, "pcs", "stonith", "disable", resourceID); err != nil {
				return nil, err
			}
		}
	}
	switch control {
	case "security-disable":
		if _, err := runGFSManageCommand(gfsManageShortTimeout, "pcs", "property", "set", "maintenance-mode=true"); err != nil {
			return nil, err
		}
	case "security-enable":
		if _, err := runGFSManageCommand(gfsManageShortTimeout, "pcs", "property", "set", "maintenance-mode=false"); err != nil {
			return nil, err
		}
	}
	return "Stonith Pcs Cluster Success", nil
}

func parseGFSManageStonithResourceIDs(output string) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0)
	for _, line := range splitLines(output) {
		for _, field := range strings.Fields(line) {
			field = strings.Trim(field, "*: ")
			if field == "" {
				continue
			}
			if strings.HasPrefix(field, "fence-") {
				if _, ok := seen[field]; ok {
					continue
				}
				seen[field] = struct{}{}
				ids = append(ids, field)
			}
		}
	}
	return ids
}

func runGFSManageIPMICheck(devices []GFSManageStonithDevice) (any, error) {
	type ipmiResult struct {
		IP      string `json:"ip"`
		Status  string `json:"status,omitempty"`
		Message string `json:"message,omitempty"`
	}
	results := make([]ipmiResult, 0, len(devices))
	var firstErr error
	for _, device := range devices {
		out, err := runGFSManageCommand(
			gfsManageShortTimeout,
			"ipmitool",
			"-I", "lanplus",
			"-H", device.IPAddr,
			"-U", device.Login,
			"-P", device.Passwd,
			"power", "status",
		)
		item := ipmiResult{IP: device.IPAddr}
		if err != nil {
			item.Message = err.Error()
			if firstErr == nil {
				firstErr = err
			}
		} else {
			item.Status = strings.TrimSpace(out)
		}
		results = append(results, item)
	}
	return results, firstErr
}

func prepareGFSManageAlertFile() error {
	for _, path := range []string{gfsManageAlertLogPath, gfsManageAlertDetailPath} {
		if err := prepareGFSManageAlertLogFile(path); err != nil {
			return err
		}
	}
	return nil
}

func prepareGFSManageAlertLogFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if _, err := runGFSManageCommand(gfsManageShortTimeout, "chown", "hacluster:haclient", path); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

func createGFSManageAlert() error {
	_, _ = runGFSManageCommandIgnore("pcs", "alert", "remove", "alert_file")
	if _, err := runGFSManageCommand(gfsManageShortTimeout, "pcs", "alert", "create", "id=alert_file", "description=Log events to a file.", "path="+gfsManageAlertScriptPath()); err != nil {
		return err
	}
	_, err := runGFSManageCommand(gfsManageShortTimeout, "pcs", "alert", "recipient", "add", "alert_file", "id=alert_logfile", "value="+gfsManageAlertLogPath)
	return err
}

func gfsManageAlertScriptPath() string {
	return resolveAbleStackShellFile("alert_file.sh", filepath.Join("host", "alert_file.sh"))
}

func gfsManageSortedHosts(cfg *CubeModel.ClusterConfigSection) []CubeModel.ClusterHost {
	if cfg == nil {
		return nil
	}
	hosts := append([]CubeModel.ClusterHost(nil), cfg.Hosts...)
	sort.Slice(hosts, func(i int, j int) bool {
		return strings.TrimSpace(hosts[i].Hostname) < strings.TrimSpace(hosts[j].Hostname)
	})
	return hosts
}

func listGFSManageVolumeGroups() ([]map[string]string, error) {
	out, err := runGFSManageCommand(gfsManageShortTimeout, "vgs", "-o", "vg_name", "--reportformat", "json")
	if err != nil {
		return nil, err
	}
	var report gfsManageVGReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		return nil, err
	}
	vgs := make([]map[string]string, 0)
	for _, section := range report.Report {
		for _, vg := range section.VG {
			if strings.Contains(vg.VGName, "vg_glue") {
				vgs = append(vgs, map[string]string{"vg_name": vg.VGName})
			}
		}
	}
	return vgs, nil
}

func deleteGFSManageDisk(req GFSManageRequest, cfg *CubeModel.ClusterConfigSection) error {
	_, _ = runGFSManageCommand(gfsManageCommandTimeout, "pcs", "resource", "disable", req.GFSName)
	_, _ = runGFSManageCommand(gfsManageCommandTimeout, "pcs", "resource", "disable", req.GFSName+"_res")
	time.Sleep(8 * time.Second)
	_, _ = runGFSManageCommand(gfsManageCommandTimeout, "pcs", "resource", "delete", req.GFSName, "--force")
	time.Sleep(8 * time.Second)
	_, _ = runGFSManageCommand(gfsManageCommandTimeout, "pcs", "resource", "delete", req.GFSName+"_res", "--force")
	_, _ = runGFSManageCommand(gfsManageCommandTimeout, "pcs", "resource", "cleanup")

	if err := cleanupGFSManageVolumeGroup(GFSManageVolumeGroup{VGName: req.VGName, LVName: req.LVName}); err != nil {
		return err
	}

	for _, disk := range req.Disks {
		cleanupGFSManageDisk(disk, "")
	}
	return fanoutGFSManagePartprobe(req.Disks, cfg)
}

func scanGFSManageSCSIHosts() error {
	hosts, err := filepath.Glob("/sys/class/scsi_host/*/scan")
	if err != nil {
		return err
	}
	for _, host := range hosts {
		if err := os.WriteFile(host, []byte("- - -"), 0200); err != nil {
			return err
		}
	}
	return nil
}

func rescanGFSManageDisk(vgName string, lvName string) error {
	paths, err := collectGFSManageDevicePaths(vgName, lvName, "")
	if err != nil {
		return err
	}
	for _, blockName := range paths.BlockNames {
		rescanPath := filepath.Join("/sys/block", blockName, "device", "rescan")
		_ = os.WriteFile(rescanPath, []byte("1"), 0200)
	}
	for _, mapName := range paths.MapNames {
		_, _ = runGFSManageCommand(gfsManageShortTimeout, "multipathd", "resize", "map", mapName)
	}
	return nil
}

func extendGFSManageDisk(req GFSManageRequest, cfg *CubeModel.ClusterConfigSection) error {
	if req.NonStopCheck == "true" {
		if _, err := runGFSManageCommand(gfsManageShortTimeout, "pcs", "property", "set", "maintenance-mode=true"); err != nil {
			return err
		}
		defer func() {
			_, _ = runGFSManageCommand(gfsManageShortTimeout, "pcs", "property", "set", "maintenance-mode=false")
		}()
	}

	paths, err := collectGFSManageDevicePaths(req.VGName, req.LVName, "")
	if err != nil {
		return err
	}
	for _, disk := range paths.Disks {
		_, _ = runGFSManageCommand(gfsManageCommandTimeout, "parted", "-s", disk, "resizepart", "1", "100%", "-f")
	}
	if err := fanoutGFSManagePartprobe(paths.Disks, cfg); err != nil {
		return err
	}
	for _, partition := range paths.Partitions {
		if _, err := runGFSManageCommand(gfsManageCommandTimeout, "pvresize", partition); err != nil {
			return err
		}
	}
	if _, err := runGFSManageCommand(gfsManageCommandTimeout, "lvextend", "-l", "+100%FREE", req.VGName+"/"+req.LVName); err != nil {
		return err
	}
	if _, err := runGFSManageCommand(gfsManageCommandTimeout, "gfs2_grow", req.MountPoint); err != nil {
		return err
	}
	return fanoutGFSManagePartprobe(paths.Disks, cfg)
}

func addExtendGFSManageDisk(req GFSManageRequest, cfg *CubeModel.ClusterConfigSection) error {
	if req.NonStopCheck == "true" {
		if _, err := runGFSManageCommand(gfsManageShortTimeout, "pcs", "property", "set", "maintenance-mode=true"); err != nil {
			return err
		}
		defer func() {
			_, _ = runGFSManageCommand(gfsManageShortTimeout, "pcs", "property", "set", "maintenance-mode=false")
		}()
	}

	partitions := make([]string, 0, len(req.Disks))
	osType := ""
	if cfg != nil {
		osType = cfg.Type
	}
	for _, disk := range req.Disks {
		if _, err := runGFSManageCommand(gfsManageCommandTimeout, "parted", "-s", disk, "mklabel", "gpt", "mkpart", req.GFSName, "0%", "100%", "set", "1", "lvm", "on"); err != nil {
			return err
		}
		_, _ = runGFSManageCommand(gfsManageShortTimeout, "partprobe", disk)
		partition, err := waitForGFSManagePartition(disk, osType)
		if err != nil {
			return err
		}
		if _, err := runGFSManageCommand(gfsManageCommandTimeout, "pvcreate", partition); err != nil {
			return err
		}
		partitions = append(partitions, partition)
	}

	if err := fanoutGFSManageLVMDevices(req.Disks, cfg); err != nil {
		return err
	}
	if _, err := runGFSManageCommand(gfsManageCommandTimeout, "vgextend", append([]string{req.VGName}, partitions...)...); err != nil {
		return err
	}
	if _, err := runGFSManageCommand(gfsManageCommandTimeout, "lvextend", "-l", "+100%FREE", "/dev/"+req.VGName+"/"+req.LVName); err != nil {
		return err
	}
	_, err := runGFSManageCommand(gfsManageCommandTimeout, "gfs2_grow", req.MountPoint)
	return err
}

func waitForGFSManagePartition(disk string, osType string) (string, error) {
	deadline := time.Now().Add(gfsManageShortTimeout)
	candidates := gfsManagePartitionCandidates(disk, osType)
	for {
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("partition not found for %s", disk)
		}
		time.Sleep(time.Second)
	}
}

func collectGFSManageDevicePaths(vgName string, lvName string, osType string) (gfsManageDevicePaths, error) {
	out, err := runGFSManageCommand(gfsManageShortTimeout, "lsblk", "-J", "-o", "name,path,type,mountpoint")
	if err != nil {
		return gfsManageDevicePaths{}, err
	}
	var payload gfsManageLSBLKPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		return gfsManageDevicePaths{}, err
	}

	targetName := vgName + "-" + lvName
	result := gfsManageDevicePaths{}
	var walk func(node gfsManageBlockDevice, ancestors []gfsManageBlockDevice)
	walk = func(node gfsManageBlockDevice, ancestors []gfsManageBlockDevice) {
		if node.Name == targetName {
			result.LVPaths = append(result.LVPaths, firstNonEmpty(node.Path, "/dev/mapper/"+node.Name))
			result = appendGFSManageAncestorPaths(result, ancestors, osType)
		}
		next := append(append([]gfsManageBlockDevice{}, ancestors...), node)
		for _, child := range node.Children {
			walk(child, next)
		}
	}
	for _, dev := range payload.Blockdevices {
		walk(dev, nil)
	}
	result.Disks = normalizeStringSlice(result.Disks)
	result.Partitions = normalizeStringSlice(result.Partitions)
	result.LVPaths = normalizeStringSlice(result.LVPaths)
	result.MapNames = normalizeStringSlice(result.MapNames)
	result.BlockNames = normalizeStringSlice(result.BlockNames)
	if len(result.Disks) == 0 && len(result.Partitions) == 0 {
		return result, fmt.Errorf("gfs disk not found")
	}
	return result, nil
}

func appendGFSManageAncestorPaths(result gfsManageDevicePaths, ancestors []gfsManageBlockDevice, osType string) gfsManageDevicePaths {
	if len(ancestors) == 0 {
		return result
	}
	root := ancestors[0]
	parent := ancestors[len(ancestors)-1]
	if root.Name != "" {
		result.BlockNames = append(result.BlockNames, root.Name)
	}
	if mpath, ok := nearestGFSManageMultipathAncestor(ancestors); ok {
		diskPath := firstNonEmpty(mpath.Path, root.Path)
		result.Disks = append(result.Disks, diskPath)
		result.MapNames = append(result.MapNames, mpath.Name)
		if strings.EqualFold(osType, gfsManageHCIFilesystem) {
			result.Partitions = append(result.Partitions, diskPath)
		} else {
			result.Partitions = append(result.Partitions, diskPath+"1")
		}
		return result
	}
	result.Disks = append(result.Disks, firstNonEmpty(root.Path, "/dev/"+root.Name))
	result.Partitions = append(result.Partitions, firstNonEmpty(parent.Path, "/dev/"+parent.Name))
	return result
}

func nearestGFSManageMultipathAncestor(ancestors []gfsManageBlockDevice) (gfsManageBlockDevice, bool) {
	for i := len(ancestors) - 1; i >= 0; i-- {
		node := ancestors[i]
		if strings.EqualFold(node.Type, "mpath") || strings.Contains(strings.ToLower(node.Name), "mpath") {
			return node, true
		}
	}
	return gfsManageBlockDevice{}, false
}

func fanoutGFSManagePartprobe(disks []string, cfg *CubeModel.ClusterConfigSection) error {
	if len(disks) == 0 || cfg == nil {
		return nil
	}
	req := GFSManageRequest{Action: "partprobe", Disks: disks}
	if err := normalizeGFSManageRequest(&req); err != nil {
		return err
	}
	resp := runGFSManageFanout(req, cfg)
	if resp.Code != http.StatusOK {
		return fmt.Errorf("partprobe failed: %s", firstNonEmpty(resp.Message, fmt.Sprint(resp.Val)))
	}
	return nil
}

func fanoutGFSManageLVMDevices(disks []string, cfg *CubeModel.ClusterConfigSection) error {
	if len(disks) == 0 || cfg == nil {
		return nil
	}
	req := GFSManageRequest{Action: "lvmdevices-add", Disks: disks}
	if err := normalizeGFSManageRequest(&req); err != nil {
		return err
	}
	resp := runGFSManageFanout(req, cfg)
	if resp.Code != http.StatusOK {
		return fmt.Errorf("lvmdevices-add failed: %s", firstNonEmpty(resp.Message, fmt.Sprint(resp.Val)))
	}
	return nil
}

func firstGFSManageResultError(results []GFSManageTargetResult) error {
	for _, result := range results {
		if result.Code != http.StatusOK {
			return fmt.Errorf("%s: %s", result.Target, result.Message)
		}
	}
	return nil
}

func gfsManageTargetResult(target gfsManageTarget, resp GFSManageResponse) GFSManageTargetResult {
	return GFSManageTargetResult{
		Hostname: target.Hostname,
		Target:   firstNonEmpty(resp.Target, target.Target, "local"),
		Code:     resp.Code,
		Message:  firstNonEmpty(resp.Message, "ok"),
		Val:      resp.Val,
	}
}

func gfsManageOK(req GFSManageRequest, target string, val any, results []GFSManageTargetResult) GFSManageResponse {
	return GFSManageResponse{
		Code:    http.StatusOK,
		Val:     val,
		Message: "ok",
		Action:  req.Action,
		Target:  target,
		Results: results,
	}
}

func gfsManageError(req GFSManageRequest, target string, message string, results []GFSManageTargetResult) GFSManageResponse {
	return GFSManageResponse{
		Code:    http.StatusInternalServerError,
		Val:     message,
		Message: message,
		Action:  req.Action,
		Target:  target,
		Results: results,
	}
}

func statusCodeFromGFSManageResponse(resp GFSManageResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func runGFSManageCommand(timeout time.Duration, command string, args ...string) (string, error) {
	out, timedOut, err := runCommandOutputWithEnv(command, timeout, gfsManageCommandEnv(), args...)
	if timedOut {
		return out, fmt.Errorf("%s %s timed out after %s", command, strings.Join(args, " "), timeout)
	}
	if err != nil {
		return out, fmt.Errorf("%s %s failed: %s", command, strings.Join(args, " "), firstNonEmpty(strings.TrimSpace(out), err.Error()))
	}
	return out, nil
}

func runGFSManageCommandIgnore(command string, args ...string) (string, error) {
	return runGFSManageCommand(gfsManageCommandTimeout, command, args...)
}

func gfsManageCommandEnv() []string {
	return append([]string{"LANG=en_US.utf-8", "LANGUAGE=en"}, os.Environ()...)
}
