package cube

import (
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

type LocalManageRequest = CubeModel.LocalManageRequest
type LocalManageResponse = CubeModel.LocalManageResponse
type LocalManageStatusValue = CubeModel.LocalManageStatusValue

const (
	localManageVGName        = "vg_glue"
	localManageLVName        = "lv_glue"
	localManageMountPath     = "/mnt/glue"
	localManageFSTabPath     = "/etc/fstab"
	localManageCommandTO     = 5 * time.Minute
	localManageShortTO       = 30 * time.Second
	localManageFSTabDevice   = "/dev/vg_glue/lv_glue"
	localManageFSTabLine     = "/dev/vg_glue/lv_glue /mnt/glue xfs defaults 0 0"
	localManageHealthOK      = "Health OK"
	localManageHealthErr     = "Health Err"
	localManageCreateSuccess = "Create Local Disk Success"
	localManageResetSuccess  = "Success Reset Local Disk"
)

// LocalManage godoc
//
//	@Summary		Local Disk Manage
//	@Description	standalone 환경의 로컬 디스크를 생성, 조회, 초기화합니다. 로컬 전용 API이며 SSH를 사용하지 않습니다.
//	@Tags			Cube-Local
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.LocalManageRequest	true	"local manage request"
//	@Success		200	{object}	CubeModel.LocalManageResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/local/manage [post]
func LocalManage(context *gin.Context) {
	var req LocalManageRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := normalizeLocalManageRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	resp := runLocalManage(req)
	context.JSON(statusCodeFromLocalManageResponse(resp), resp)
}

func normalizeLocalManageRequest(req *LocalManageRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "create-local-disk", "create":
		req.Action = "create-local-disk"
	case "local-disk-status", "status":
		req.Action = "local-disk-status"
	case "reset":
		req.Action = "reset"
	default:
		return fmt.Errorf("unsupported action")
	}

	req.Disk = strings.TrimSpace(req.Disk)
	req.Disks = normalizeStringSlice(append(req.Disks, splitCommaValues(req.Disk)...))
	if req.Action == "create-local-disk" && len(req.Disks) == 0 {
		return fmt.Errorf("disks required")
	}
	return nil
}

func runLocalManage(req LocalManageRequest) LocalManageResponse {
	switch req.Action {
	case "create-local-disk":
		if err := createLocalManageDisk(req.Disks); err != nil {
			return localManageError(req, "Create Local Disk Failure: "+err.Error())
		}
		return localManageOK(req, localManageCreateSuccess)
	case "local-disk-status":
		status, code := localManageStatus()
		resp := localManageOK(req, status)
		resp.Code = code
		if code != http.StatusOK {
			resp.Message = localManageHealthErr
		}
		return resp
	case "reset":
		if err := resetLocalManageDisk(); err != nil {
			return localManageError(req, "Failed Local Disk Check: "+err.Error())
		}
		return localManageOK(req, localManageResetSuccess)
	default:
		return localManageError(req, "unsupported action")
	}
}

func createLocalManageDisk(disks []string) error {
	if localManageVGExists() {
		return fmt.Errorf("%s already exists", localManageVGName)
	}

	partitions := make([]string, 0, len(disks))
	for _, disk := range disks {
		if err := createLocalManagePartition(disk); err != nil {
			return err
		}
		partition, err := waitForLocalManagePartition(disk, localManageShortTO)
		if err != nil {
			return err
		}
		if _, err := execLocalManageCommand(localManageCommandTO, "pvcreate", "-ff", "--yes", partition); err != nil {
			return err
		}
		partitions = append(partitions, partition)
	}

	if _, err := execLocalManageCommand(localManageCommandTO, "vgcreate", append([]string{localManageVGName}, partitions...)...); err != nil {
		return err
	}
	if _, err := execLocalManageCommand(localManageCommandTO, "lvcreate", "-n", localManageLVName, localManageVGName, "-l", "+100%FREE", "-y"); err != nil {
		return err
	}
	if err := os.MkdirAll(localManageMountPath, 0o755); err != nil {
		return err
	}
	if _, err := execLocalManageCommand(localManageCommandTO, "mkfs.xfs", "-K", localManageFSTabDevice); err != nil {
		return err
	}
	if err := ensureLocalManageFSTabLine(); err != nil {
		return err
	}
	if _, err := execLocalManageCommand(localManageShortTO, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	if _, err := execLocalManageCommand(localManageCommandTO, "mount", localManageMountPath); err != nil {
		return err
	}
	return setLocalManageConfigureFlag("true")
}

func createLocalManagePartition(disk string) error {
	if strings.TrimSpace(disk) == "" {
		return fmt.Errorf("disk required")
	}
	if _, err := os.Stat(disk); err != nil {
		return err
	}
	_, err := execLocalManageCommand(
		localManageCommandTO,
		"parted",
		"-s",
		disk,
		"mklabel", "gpt",
		"mkpart", "primary", "0%", "100%",
		"set", "1", "lvm", "on",
	)
	if err != nil {
		return err
	}
	_, _ = execLocalManageCommand(localManageShortTO, "partprobe", disk)
	return nil
}

func localManageStatus() (LocalManageStatusValue, int) {
	status := LocalManageStatusValue{
		Status:    localManageHealthErr,
		MountPath: "N/A",
		PV:        "N/A",
		VG:        "N/A",
		Size:      "N/A",
	}

	if mountPath, ok := findLocalManageMountPath(); ok {
		status.Status = localManageHealthOK
		status.MountPath = mountPath
		status.PV = strings.Join(localManagePVs(), "\n")
		if status.PV == "" {
			status.PV = "N/A"
		}
		status.VG = strings.Join(localManageMapperPaths(), "\n")
		if status.VG == "" {
			status.VG = localManageFSTabDevice
		}
		if size := localManageLVSize(); size != "" {
			status.Size = size
		}
		return status, http.StatusOK
	}
	return status, http.StatusInternalServerError
}

func resetLocalManageDisk() error {
	pvs := localManagePVs()
	_, _ = execLocalManageCommand(localManageShortTO, "umount", "-fl", localManageMountPath)
	_, _ = execLocalManageCommand(localManageShortTO, "lvremove", "-f", localManageFSTabDevice)
	_, _ = execLocalManageCommand(localManageShortTO, "vgremove", "-f", localManageVGName)

	for _, pv := range pvs {
		_, _ = execLocalManageCommand(localManageShortTO, "pvremove", "-ff", "--yes", pv)
	}
	for _, disk := range localManageBaseDisks(pvs) {
		_, _ = execLocalManageCommand(localManageShortTO, "parted", "-s", disk, "rm", "1")
		_, _ = execLocalManageCommand(localManageShortTO, "partprobe", disk)
	}

	if err := removeLocalManageFSTabLine(); err != nil {
		return err
	}
	_, _ = execLocalManageCommand(localManageShortTO, "systemctl", "daemon-reload")
	return setLocalManageConfigureFlag("false")
}

func localManageVGExists() bool {
	_, err := execLocalManageCommand(localManageShortTO, "vgs", "--noheadings", localManageVGName)
	return err == nil
}

func localManagePVs() []string {
	out, err := execLocalManageCommand(localManageShortTO, "pvs", "--noheadings", "-o", "pv_name,vg_name")
	if err != nil {
		return nil
	}
	pvs := make([]string, 0)
	for _, line := range splitLines(out) {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != localManageVGName {
			continue
		}
		pvs = append(pvs, fields[0])
	}
	return normalizeStringSlice(pvs)
}

func localManageBaseDisks(pvs []string) []string {
	disks := make([]string, 0, len(pvs))
	for _, pv := range pvs {
		disks = append(disks, resetCloudCenterBaseDisk(pv))
	}
	return normalizeStringSlice(disks)
}

func localManageMapperPaths() []string {
	matches, err := filepath.Glob("/dev/mapper/*" + localManageVGName + "*")
	if err != nil {
		return nil
	}
	return normalizeStringSlice(matches)
}

func localManageLVSize() string {
	out, err := execLocalManageCommand(localManageShortTO, "lsblk", "-dn", "-o", "SIZE", localManageFSTabDevice)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func findLocalManageMountPath() (string, bool) {
	raw, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return "", false
	}
	for _, line := range splitLines(string(raw)) {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[1] == localManageMountPath {
			return fields[1], true
		}
	}
	return "", false
}

func waitForLocalManagePartition(disk string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	candidates := localManagePartitionCandidates(disk)
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

func localManagePartitionCandidates(disk string) []string {
	disk = strings.TrimSpace(disk)
	if disk == "" {
		return nil
	}
	candidates := []string{disk + "1", disk + "p1", disk + "-part1"}
	return normalizeStringSlice(candidates)
}

func ensureLocalManageFSTabLine() error {
	lines, err := readLocalManageFSTabLines()
	if err != nil {
		return err
	}
	lines = filterLocalManageFSTabLines(lines)
	lines = append(lines, localManageFSTabLine)
	return writeLocalManageFSTabLines(lines)
}

func removeLocalManageFSTabLine() error {
	lines, err := readLocalManageFSTabLines()
	if err != nil {
		return err
	}
	return writeLocalManageFSTabLines(filterLocalManageFSTabLines(lines))
}

func readLocalManageFSTabLines() ([]string, error) {
	raw, err := os.ReadFile(localManageFSTabPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	return strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n"), nil
}

func filterLocalManageFSTabLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if strings.Contains(trimmed, localManageFSTabDevice) || (len(fields) >= 2 && fields[1] == localManageMountPath) {
			continue
		}
		out = append(out, line)
	}
	return out
}

func writeLocalManageFSTabLines(lines []string) error {
	content := ""
	if len(lines) > 0 {
		content = strings.Join(lines, "\n") + "\n"
	}
	tmp := localManageFSTabPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, localManageFSTabPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func setLocalManageConfigureFlag(value string) error {
	_, err := resetCloudCenterApplyLocalSystemFlags([]resetCloudCenterSystemFlag{
		{Depth1: "bootstrap", Depth2: "local_configure", Value: value},
	})
	return err
}

func execLocalManageCommand(timeout time.Duration, command string, args ...string) (string, error) {
	out, timedOut, err := runCommandOutputWithEnv(command, timeout, resetCloudCenterCommandEnv(), args...)
	if timedOut {
		return out, fmt.Errorf("%s %s timed out after %s", command, strings.Join(args, " "), timeout)
	}
	if err != nil {
		return out, fmt.Errorf("%s %s failed: %s", command, strings.Join(args, " "), firstNonEmpty(strings.TrimSpace(out), err.Error()))
	}
	return out, nil
}

func localManageOK(req LocalManageRequest, val any) LocalManageResponse {
	return LocalManageResponse{
		Code:    http.StatusOK,
		Val:     val,
		Message: "ok",
		Action:  req.Action,
	}
}

func localManageError(req LocalManageRequest, message string) LocalManageResponse {
	return LocalManageResponse{
		Code:    http.StatusInternalServerError,
		Val:     message,
		Message: message,
		Action:  req.Action,
	}
}

func statusCodeFromLocalManageResponse(resp LocalManageResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
