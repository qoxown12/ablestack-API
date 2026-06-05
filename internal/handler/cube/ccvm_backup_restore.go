package cube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/logging"
	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type CCVMBackupRequest = CubeModel.CCVMBackupRequest
type CCVMRestoreRequest = CubeModel.CCVMRestoreRequest
type CCVMBackupResponse = CubeModel.CCVMBackupResponse

const (
	ccvmBackupLocalHeader          = "X-Cube-CCVM-Backup-Local"
	ccvmBackupResolveHeader        = "X-Cube-CCVM-Backup-Resolve"
	ccvmBackupRequestTimeout       = 30 * time.Minute
	ccvmBackupCommandTimeout       = 30 * time.Minute
	ccvmBackupShortCommandTimeout  = 10 * time.Second
	ccvmBackupRestoreTimeout       = 10 * time.Minute
	ccvmBackupRestorePollInterval  = 5 * time.Second
	ccvmBackupScheduleCheck        = time.Minute
	ccvmBackupDomainName           = "ccvm"
	ccvmBackupDiskName             = "vda"
	ccvmBackupTargetPrefix         = "ccvm.qcow2"
	ccvmBackupMaxFiles             = 10
	ccvmBackupScriptDirName        = "script"
	ccvmBackupConfigFileName       = "ccvm-backup-config.json"
	ccvmBackupDefaultDir           = "/mnt/glue-gfs/backup/ccvm"
	ccvmBackupStandaloneDefaultDir = "/mnt/glue/backup/ccvm"
	ccvmBackupImagePath            = "/mnt/glue-gfs/ccvm.qcow2"
	ccvmBackupStandaloneImagePath  = "/mnt/glue/ccvm.qcow2"
)

var autoCCVMBackupSchedulerOnce sync.Once

type ccvmBackupScheduleConfig struct {
	TargetDir string                     `json:"target_dir"`
	UpdatedAt string                     `json:"updated_at"`
	Backup    *ccvmBackupScheduleSection `json:"backup,omitempty"`
	Delete    *ccvmBackupScheduleSection `json:"delete,omitempty"`
}

type ccvmBackupScheduleSection struct {
	Active          bool   `json:"active"`
	Repeat          string `json:"repeat,omitempty"`
	Time            string `json:"time,omitempty"`
	Day             int    `json:"day,omitempty"`
	Month           int    `json:"month,omitempty"`
	RetentionMonths int    `json:"retention_months,omitempty"`
	LastRunKey      string `json:"last_run_key,omitempty"`
}

type ccvmBackupFileInfo struct {
	Name      string
	Path      string
	Size      int64
	Created   time.Time
	Completed time.Time
}

// CCVMBackup godoc
//
//	@Summary		CCVM File Backup
//	@Description	virsh backup-begin 기반 CCVM 파일 백업/상태/목록/스케줄 관리를 수행합니다.
//	@Tags			Cube-CCVM
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.CCVMBackupRequest	true	"ccvm backup request"
//	@Success		200	{object}	CubeModel.CCVMBackupResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/ccvm/backup [post]
func CCVMBackup(context *gin.Context) {
	var req CCVMBackupRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := normalizeCCVMBackupRequest(&req); err != nil {
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

	var resp CCVMBackupResponse
	if isCCVMBackupLocalRequest(context) {
		resp = runCCVMBackupLocal(cfg, req)
	} else if isCCVMBackupResolveRequest(context) {
		resp = runCCVMBackupResolvedFromPCS(cfg, req)
	} else if isCCVMBackupStandalone(cfg) {
		resp = runCCVMBackupLocal(cfg, req)
	} else {
		resp = runCCVMBackupViaPCS(cfg, req)
	}

	context.JSON(statusCodeFromCCVMBackupResponse(resp), resp)
}

// CCVMRestore godoc
//
//	@Summary		CCVM File Restore
//	@Description	CCVM 파일 백업으로 디스크를 복구합니다.
//	@Tags			Cube-CCVM
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.CCVMRestoreRequest	true	"ccvm restore request"
//	@Success		200	{object}	CubeModel.CCVMBackupResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/ccvm/restore [post]
func CCVMRestore(context *gin.Context) {
	var req CCVMRestoreRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := normalizeCCVMRestoreRequest(&req); err != nil {
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

	var resp CCVMBackupResponse
	if isCCVMBackupLocalRequest(context) || isCCVMBackupStandalone(cfg) {
		resp = runCCVMRestoreLocal(cfg, req)
	} else {
		resp = runCCVMRestoreViaPCS(cfg, req)
	}

	context.JSON(statusCodeFromCCVMBackupResponse(resp), resp)
}

// AutoCCVMFileBackupSchedule은 API 서버 내부 CCVM 파일 백업 스케줄러를 시작한다.
func AutoCCVMFileBackupSchedule() {
	autoCCVMBackupSchedulerOnce.Do(func() {
		go runAutoCCVMBackupScheduler()
	})
}

func normalizeCCVMBackupRequest(req *CCVMBackupRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "backup":
		req.Action = "backup"
	case "status":
		req.Action = "status"
	case "list":
		req.Action = "list"
	case "overview":
		req.Action = "overview"
	case "schedule":
		req.Action = "schedule"
	case "unschedule":
		req.Action = "unschedule"
	case "schedule-delete":
		req.Action = "schedule-delete"
	case "unschedule-delete":
		req.Action = "unschedule-delete"
	default:
		return fmt.Errorf("unsupported action")
	}

	req.Repeat = strings.ToLower(strings.TrimSpace(req.Repeat))
	req.Time = strings.TrimSpace(req.Time)
	req.TargetDir = strings.TrimSpace(req.TargetDir)

	switch req.Action {
	case "backup":
		if req.TargetDir == "" {
			return fmt.Errorf("target_dir required")
		}
		if !filepath.IsAbs(req.TargetDir) {
			return fmt.Errorf("target_dir must be an absolute path")
		}
	case "schedule":
		if err := validateCCVMBackupSchedule(req.Repeat, req.Time, req.Day, req.Month, false); err != nil {
			return err
		}
	case "schedule-delete":
		if req.RetainMonths < 1 {
			return fmt.Errorf("retain_months required")
		}
		if err := validateCCVMBackupSchedule(req.Repeat, req.Time, req.Day, 0, true); err != nil {
			return err
		}
	}
	return nil
}

func normalizeCCVMRestoreRequest(req *CCVMRestoreRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	req.TargetFile = strings.TrimSpace(req.TargetFile)
	if req.TargetFile == "" {
		return fmt.Errorf("target_file required")
	}
	if filepath.IsAbs(req.TargetFile) || strings.Contains(req.TargetFile, "/") || strings.Contains(req.TargetFile, "\\") {
		return fmt.Errorf("target_file must be a file name")
	}
	if req.TargetFile == "." || req.TargetFile == ".." || filepath.Clean(req.TargetFile) != req.TargetFile {
		return fmt.Errorf("target_file must be a file name")
	}
	return nil
}

func validateCCVMBackupSchedule(repeat string, rawTime string, day int, month int, deleteSchedule bool) error {
	if _, _, err := parseCCVMBackupTime(rawTime); err != nil {
		return err
	}
	switch repeat {
	case "hourly":
		if deleteSchedule {
			return fmt.Errorf("delete repeat option must be daily or monthly")
		}
	case "daily":
	case "monthly":
		if day < 1 || day > 31 {
			return fmt.Errorf("day is required for monthly schedule")
		}
	case "yearly":
		if deleteSchedule {
			return fmt.Errorf("delete repeat option must be daily or monthly")
		}
		if month < 1 || month > 12 || day < 1 || day > 31 {
			return fmt.Errorf("day and month are required for yearly schedule")
		}
	default:
		return fmt.Errorf("invalid repeat option")
	}
	return nil
}

func isCCVMBackupLocalRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(ccvmBackupLocalHeader)), "1")
}

func isCCVMBackupResolveRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(ccvmBackupResolveHeader)), "1")
}

func statusCodeFromCCVMBackupResponse(resp CCVMBackupResponse) int {
	if resp.Code == 200 {
		return http.StatusOK
	}
	if resp.Code == 400 {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func runCCVMBackupViaPCS(cfg *CubeModel.ClusterConfigSection, req CCVMBackupRequest) CCVMBackupResponse {
	targets := buildCCVMSnapPCSTargets(cfg)
	if len(targets) == 0 {
		return ccvmBackupError(req, "", "pcs cluster host not found")
	}

	var lastErr error
	client := &http.Client{Timeout: 5 * time.Second}
	for _, target := range targets {
		if !isLocalTarget(target.Target) {
			if err := callHealthTarget(client, target.Target); err != nil {
				lastErr = fmt.Errorf("%s health check failed: %w", target.Target, err)
				continue
			}
		}

		if isLocalTarget(target.Target) {
			return runCCVMBackupResolvedFromPCS(cfg, req)
		}

		resp, err := callCCVMBackupRemote(target.Target, req, ccvmBackupResolveHeader)
		if err != nil {
			lastErr = err
			continue
		}
		return resp
	}

	if lastErr != nil {
		return ccvmBackupError(req, "", lastErr.Error())
	}
	return ccvmBackupError(req, "", "healthy pcs cluster host not found")
}

func runCCVMRestoreViaPCS(cfg *CubeModel.ClusterConfigSection, req CCVMRestoreRequest) CCVMBackupResponse {
	targets := buildCCVMSnapPCSTargets(cfg)
	if len(targets) == 0 {
		return ccvmRestoreError("", "pcs cluster host not found")
	}

	var lastErr error
	client := &http.Client{Timeout: 5 * time.Second}
	for _, target := range targets {
		if !isLocalTarget(target.Target) {
			if err := callHealthTarget(client, target.Target); err != nil {
				lastErr = fmt.Errorf("%s health check failed: %w", target.Target, err)
				continue
			}
		}

		if isLocalTarget(target.Target) {
			return runCCVMRestoreLocal(cfg, req)
		}

		resp, err := callCCVMRestoreRemote(target.Target, req, ccvmBackupLocalHeader)
		if err != nil {
			lastErr = err
			continue
		}
		return resp
	}

	if lastErr != nil {
		return ccvmRestoreError("", lastErr.Error())
	}
	return ccvmRestoreError("", "healthy pcs cluster host not found")
}

func runCCVMBackupResolvedFromPCS(cfg *CubeModel.ClusterConfigSection, req CCVMBackupRequest) CCVMBackupResponse {
	switch req.Action {
	case "backup", "status":
		status, err := loadCCVMPcsResourceStatus()
		if err != nil {
			return ccvmBackupError(req, "", err.Error())
		}
		if !strings.EqualFold(status.Role, "Started") || strings.TrimSpace(status.StartedNode) == "" {
			if req.Action == "status" {
				return ccvmBackupOK(req, "", map[string]any{
					"active":  false,
					"message": "ccvm running host not found",
					"fields":  map[string]string{},
					"raw":     "",
				})
			}
			return ccvmBackupError(req, "", "ccvm running host not found")
		}
		target, ok := resolveCCVMSnapStartedTarget(cfg, status.StartedNode)
		if !ok || strings.TrimSpace(target.Target) == "" {
			return ccvmBackupError(req, "", fmt.Sprintf("cloudcenter_res started node not found in cluster.json: %s", status.StartedNode))
		}
		if !isLocalTarget(target.Target) {
			resp, err := callCCVMBackupRemote(target.Target, req, ccvmBackupLocalHeader)
			if err != nil {
				return ccvmBackupError(req, target.Target, err.Error())
			}
			return resp
		}
		return runCCVMBackupLocal(cfg, req)
	default:
		return runCCVMBackupLocal(cfg, req)
	}
}

func runCCVMRestoreLocal(cfg *CubeModel.ClusterConfigSection, req CCVMRestoreRequest) CCVMBackupResponse {
	target := resolveLocalCCVMEditTarget(cfg)
	targetFile, err := resolveCCVMRestoreTargetFile(cfg, req.TargetFile)
	if err != nil {
		return ccvmRestoreError(target, err.Error())
	}
	val, err := restoreCCVMBackupFile(cfg, targetFile)
	if err != nil {
		return ccvmRestoreError(target, err.Error())
	}
	return ccvmRestoreOK(target, val)
}

func runCCVMBackupLocal(cfg *CubeModel.ClusterConfigSection, req CCVMBackupRequest) CCVMBackupResponse {
	target := resolveLocalCCVMEditTarget(cfg)
	targetDir, err := resolveCCVMBackupTargetDir(cfg, req.TargetDir)
	if err != nil {
		return ccvmBackupError(req, target, err.Error())
	}

	switch req.Action {
	case "backup":
		val, err := createCCVMFileBackup(targetDir)
		if err != nil {
			return ccvmBackupError(req, target, err.Error())
		}
		return ccvmBackupOK(req, target, val)
	case "status":
		val, err := getCCVMBackupStatus()
		if err != nil {
			return ccvmBackupError(req, target, err.Error())
		}
		return ccvmBackupOK(req, target, val)
	case "list":
		val, err := listCCVMBackupFiles(targetDir)
		if err != nil {
			return ccvmBackupError(req, target, err.Error())
		}
		return ccvmBackupOK(req, target, val)
	case "overview":
		val, err := buildCCVMBackupOverview(targetDir)
		if err != nil {
			return ccvmBackupError(req, target, err.Error())
		}
		return ccvmBackupOK(req, target, val)
	case "schedule":
		if err := updateCCVMBackupConfigSection(targetDir, "backup", &ccvmBackupScheduleSection{
			Active: true,
			Repeat: req.Repeat,
			Time:   req.Time,
			Day:    req.Day,
			Month:  req.Month,
		}); err != nil {
			return ccvmBackupError(req, target, err.Error())
		}
		return ccvmBackupOK(req, target, "정기 백업 설정 완료")
	case "unschedule":
		if err := updateCCVMBackupConfigSection(targetDir, "backup", &ccvmBackupScheduleSection{Active: false}); err != nil {
			return ccvmBackupError(req, target, err.Error())
		}
		return ccvmBackupOK(req, target, "정기 백업 비활성화 완료")
	case "schedule-delete":
		if err := updateCCVMBackupConfigSection(targetDir, "delete", &ccvmBackupScheduleSection{
			Active:          true,
			Repeat:          req.Repeat,
			Time:            req.Time,
			Day:             req.Day,
			RetentionMonths: req.RetainMonths,
		}); err != nil {
			return ccvmBackupError(req, target, err.Error())
		}
		return ccvmBackupOK(req, target, "삭제 관리 설정 완료")
	case "unschedule-delete":
		if err := updateCCVMBackupConfigSection(targetDir, "delete", &ccvmBackupScheduleSection{Active: false}); err != nil {
			return ccvmBackupError(req, target, err.Error())
		}
		return ccvmBackupOK(req, target, "삭제 관리 비활성화 완료")
	default:
		return ccvmBackupError(req, target, "unsupported action")
	}
}

func createCCVMFileBackup(targetDir string) (map[string]any, error) {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, err
	}
	timestamp := time.Now().Format("20060102_150405")
	targetFile := filepath.Join(targetDir, fmt.Sprintf("%s-%s", ccvmBackupTargetPrefix, timestamp))
	xmlPath := resolveCCVMBackupXMLPath()
	if err := os.MkdirAll(filepath.Dir(xmlPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(xmlPath, []byte(buildCCVMBackupXML(targetFile)), 0o644); err != nil {
		return nil, err
	}
	if _, err := runCCVMBackupCommand(ccvmBackupCommandTimeout, "virsh", "backup-begin", ccvmBackupDomainName, "--backupxml", xmlPath); err != nil {
		return nil, err
	}
	deleted, err := pruneCCVMBackupFiles(targetDir, ccvmBackupMaxFiles)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"target_file":     targetFile,
		"xml_path":        xmlPath,
		"deleted_backups": deleted,
		"retention_count": ccvmBackupMaxFiles,
	}, nil
}

func buildCCVMBackupXML(targetFile string) string {
	return strings.Join([]string{
		"<domainbackup>",
		"  <disks>",
		fmt.Sprintf("    <disk name=\"%s\" type=\"file\">", ccvmBackupDiskName),
		fmt.Sprintf("      <target file=\"%s\"/>", targetFile),
		"    </disk>",
		"  </disks>",
		"</domainbackup>",
		"",
	}, "\n")
}

func getCCVMBackupStatus() (map[string]any, error) {
	out, err := runCCVMBackupCommand(ccvmBackupShortCommandTimeout, "virsh", "domjobinfo", ccvmBackupDomainName)
	raw := strings.TrimSpace(out)
	lower := strings.ToLower(raw)
	if err != nil || strings.Contains(lower, "no current") || strings.Contains(lower, "no job") {
		return map[string]any{
			"active":  false,
			"message": firstNonEmpty(raw, "no active job"),
			"fields":  map[string]string{},
			"raw":     raw,
		}, nil
	}

	fields := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	jobType := strings.ToLower(strings.TrimSpace(fields["Job type"]))
	active := jobType != "" && jobType != "none" && jobType != "unknown"
	return map[string]any{
		"active":  active,
		"message": "",
		"fields":  fields,
		"raw":     raw,
	}, nil
}

func listCCVMBackupFiles(targetDir string) ([]map[string]any, error) {
	files, err := listCCVMBackupFileInfos(targetDir)
	if err != nil {
		return nil, err
	}

	backups := make([]map[string]any, 0, len(files))
	for _, file := range files {
		backups = append(backups, map[string]any{
			"name":              file.Name,
			"path":              file.Path,
			"size_bytes":        file.Size,
			"size_human":        formatCCVMBackupSize(file.Size),
			"mtime_epoch":       file.Created.Unix(),
			"mtime":             file.Created.Format("2006-01-02 15:04:05"),
			"mtime_display":     file.Created.Format("01-02 15:04"),
			"created_epoch":     file.Created.Unix(),
			"created_time":      file.Created.Format("2006-01-02 15:04:05"),
			"created_display":   file.Created.Format("01-02 15:04"),
			"completed_epoch":   file.Completed.Unix(),
			"completed_time":    file.Completed.Format("2006-01-02 15:04:05"),
			"completed_display": file.Completed.Format("01-02 15:04"),
		})
	}
	return backups, nil
}

func listCCVMBackupFileInfos(targetDir string) ([]ccvmBackupFileInfo, error) {
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ccvmBackupFileInfo{}, nil
		}
		return nil, err
	}

	files := make([]ccvmBackupFileInfo, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), ccvmBackupTargetPrefix+"-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(targetDir, entry.Name())
		completed := info.ModTime()
		created := parseCCVMBackupTimeFromName(entry.Name(), completed)
		files = append(files, ccvmBackupFileInfo{
			Name:      entry.Name(),
			Path:      path,
			Size:      info.Size(),
			Created:   created,
			Completed: completed,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Created.After(files[j].Created)
	})
	return files, nil
}

func pruneCCVMBackupFiles(targetDir string, keep int) ([]string, error) {
	if keep < 1 {
		keep = 1
	}
	files, err := listCCVMBackupFileInfos(targetDir)
	if err != nil {
		return nil, err
	}
	if len(files) <= keep {
		return []string{}, nil
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Created.Before(files[j].Created)
	})

	removeCount := len(files) - keep
	deleted := make([]string, 0, removeCount)
	for _, file := range files[:removeCount] {
		if err := os.Remove(file.Path); err != nil {
			return deleted, err
		}
		deleted = append(deleted, file.Name)
	}
	return deleted, nil
}

func resolveCCVMRestoreTargetFile(cfg *CubeModel.ClusterConfigSection, fileName string) (string, error) {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return "", fmt.Errorf("target_file required")
	}
	if filepath.IsAbs(fileName) || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") {
		return "", fmt.Errorf("target_file must be a file name")
	}
	if fileName == "." || fileName == ".." || filepath.Clean(fileName) != fileName {
		return "", fmt.Errorf("target_file must be a file name")
	}
	targetDir, err := resolveCCVMBackupTargetDir(cfg, "")
	if err != nil {
		return "", err
	}
	return filepath.Join(targetDir, fileName), nil
}

func restoreCCVMBackupFile(cfg *CubeModel.ClusterConfigSection, targetFile string) (map[string]string, error) {
	targetFile = strings.TrimSpace(targetFile)
	if targetFile == "" {
		return nil, fmt.Errorf("복구 대상 파일을 지정해야 합니다")
	}
	if !filepath.IsAbs(targetFile) {
		return nil, fmt.Errorf("복구 대상 파일은 절대 경로여야 합니다")
	}
	if _, err := os.Stat(targetFile); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("복구 대상 파일이 존재하지 않습니다")
		}
		return nil, err
	}

	if isCCVMBackupStandalone(cfg) {
		if err := stopStandaloneCCVMForRestore(); err != nil {
			return nil, err
		}
		if err := copyFile(targetFile, ccvmBackupStandaloneImagePath); err != nil {
			return nil, err
		}
		if _, err := runCCVMBackupCommand(ccvmBackupShortCommandTimeout, "virsh", "start", ccvmBackupDomainName); err != nil {
			return nil, err
		}
		return map[string]string{"disk_path": ccvmBackupStandaloneImagePath, "source": targetFile}, nil
	}

	if err := setCCVMPcsResourceEnabled(false); err != nil {
		return nil, err
	}
	if err := waitCCVMPcsRole("Stopped", ccvmBackupRestoreTimeout); err != nil {
		return nil, err
	}
	if err := copyFile(targetFile, ccvmBackupImagePath); err != nil {
		_ = setCCVMPcsResourceEnabled(true)
		return nil, err
	}
	if err := setCCVMPcsResourceEnabled(true); err != nil {
		return nil, err
	}
	return map[string]string{"disk_path": ccvmBackupImagePath, "source": targetFile}, nil
}

func stopStandaloneCCVMForRestore() error {
	if state, exists, err := readLocalCCVMState(); err == nil && exists && strings.EqualFold(state, "shut off") {
		return nil
	}
	_, err := runCCVMBackupCommand(ccvmBackupShortCommandTimeout, "virsh", "shutdown", ccvmBackupDomainName)
	if err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "domain is not running") {
			return err
		}
	}
	return waitLocalCCVMState("shut off", 2*time.Minute)
}

func setCCVMPcsResourceEnabled(enable bool) error {
	action := "disable"
	if enable {
		action = "enable"
	}
	_, err := runCCVMBackupCommand(time.Minute, "pcs", "resource", action, ccvmSnapPCSResourceID)
	return err
}

func waitCCVMPcsRole(role string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, err := loadCCVMPcsResourceStatus()
		if err == nil && strings.EqualFold(status.Role, role) {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("cloudcenter_res %s timeout: %w", role, err)
			}
			return fmt.Errorf("cloudcenter_res %s timeout", role)
		}
		time.Sleep(ccvmBackupRestorePollInterval)
	}
}

func waitLocalCCVMState(desired string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		state, exists, err := readLocalCCVMState()
		if err == nil && exists && strings.EqualFold(state, desired) {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("ccvm %s timeout: %w", desired, err)
			}
			return fmt.Errorf("ccvm %s timeout: current state=%s", desired, state)
		}
		time.Sleep(2 * time.Second)
	}
}

func buildCCVMBackupOverview(targetDir string) (map[string]any, error) {
	config, _ := loadCCVMBackupConfig(targetDir)
	backups, err := listCCVMBackupFiles(targetDir)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"schedule":        describeCCVMBackupSchedule(config.Backup, "설정된 정기 백업이 없습니다"),
		"delete":          describeCCVMBackupSchedule(config.Delete, "설정된 삭제 관리가 없습니다"),
		"backups":         backups,
		"backups_message": "",
		"target_dir":      targetDir,
	}, nil
}

func describeCCVMBackupSchedule(section *ccvmBackupScheduleSection, emptyMessage string) map[string]any {
	if section == nil || !section.Active {
		return map[string]any{"active": false, "message": emptyMessage}
	}
	info := map[string]any{
		"active":     true,
		"message":    "",
		"repeat":     section.Repeat,
		"time":       section.Time,
		"day":        section.Day,
		"month":      section.Month,
		"cron":       buildCCVMBackupCronExpr(section),
		"last_run":   section.LastRunKey,
		"time_label": section.Time,
	}
	if section.RetentionMonths > 0 {
		info["retention_months"] = section.RetentionMonths
		info["retention_label"] = fmt.Sprintf("%d개월", section.RetentionMonths)
	}
	return info
}

func buildCCVMBackupCronExpr(section *ccvmBackupScheduleSection) string {
	if section == nil {
		return ""
	}
	hour, minute, err := parseCCVMBackupTime(section.Time)
	if err != nil {
		return ""
	}
	switch section.Repeat {
	case "hourly":
		return fmt.Sprintf("%d * * * *", minute)
	case "daily":
		return fmt.Sprintf("%d %d * * *", minute, hour)
	case "monthly":
		return fmt.Sprintf("%d %d %d * *", minute, hour, section.Day)
	case "yearly":
		return fmt.Sprintf("%d %d %d %d *", minute, hour, section.Day, section.Month)
	default:
		return ""
	}
}

func runAutoCCVMBackupScheduler() {
	for {
		now := time.Now()
		next := now.Truncate(ccvmBackupScheduleCheck).Add(ccvmBackupScheduleCheck)
		time.Sleep(time.Until(next))
		triggerAutoCCVMBackupSchedule(next)
	}
}

func triggerAutoCCVMBackupSchedule(now time.Time) {
	job := "cube.AutoCCVMFileBackupSchedule"
	cfg, err := loadClusterConfigSection()
	if err != nil {
		logging.RecordJobResult(job, err, nil)
		return
	}
	if !isCCVMBackupScheduleOwner(cfg) {
		return
	}
	targetDir, err := resolveCCVMBackupTargetDir(cfg, "")
	if err != nil {
		logging.RecordJobResult(job, err, nil)
		return
	}
	config, err := loadCCVMBackupConfig(targetDir)
	if err != nil {
		logging.RecordJobResult(job, err, map[string]any{"target_dir": targetDir})
		return
	}

	changed := false
	if due, key := isCCVMBackupScheduleDue(config.Backup, now); due {
		config.Backup.LastRunKey = key
		changed = true
		req := CCVMBackupRequest{Action: "backup", TargetDir: targetDir}
		var resp CCVMBackupResponse
		if isCCVMBackupStandalone(cfg) {
			resp = runCCVMBackupLocal(cfg, req)
		} else {
			resp = runCCVMBackupResolvedFromPCS(cfg, req)
		}
		if resp.Code != 200 {
			config.Backup.LastRunKey = ""
			logging.AppendJobLog(job, "backup_failed", "error", resp.Message, map[string]any{
				"code":       resp.Code,
				"target":     resp.Target,
				"target_dir": targetDir,
				"run_key":    key,
			})
		} else {
			logging.AppendJobLog(job, "backup_success", "success", "file backup completed", map[string]any{
				"target":     resp.Target,
				"target_dir": targetDir,
				"run_key":    key,
				"val":        fmt.Sprint(resp.Val),
			})
		}
	}
	if due, key := isCCVMBackupScheduleDue(config.Delete, now); due {
		config.Delete.LastRunKey = key
		changed = true
		if err := deleteExpiredCCVMBackups(targetDir, config.Delete.RetentionMonths); err != nil {
			config.Delete.LastRunKey = ""
			logging.AppendJobLog(job, "cleanup_failed", "error", err.Error(), map[string]any{
				"target_dir":         targetDir,
				"retention_months":   config.Delete.RetentionMonths,
				"run_key":            key,
				"delete_schedule_on": true,
			})
		} else {
			logging.AppendJobLog(job, "cleanup_success", "success", "expired file backups cleaned", map[string]any{
				"target_dir":       targetDir,
				"retention_months": config.Delete.RetentionMonths,
				"run_key":          key,
			})
		}
	}
	if changed {
		err := saveCCVMBackupConfig(targetDir, config)
		logging.RecordJobResult(job, err, map[string]any{"target_dir": targetDir})
		return
	}
	logging.RecordJobResult(job, nil, map[string]any{"target_dir": targetDir})
}

func isCCVMBackupScheduleOwner(cfg *CubeModel.ClusterConfigSection) bool {
	if cfg == nil {
		return false
	}
	if isCCVMBackupStandalone(cfg) {
		return true
	}
	currentDC, err := loadCCVMPcsCurrentDC()
	if err != nil || strings.TrimSpace(currentDC) == "" {
		return false
	}
	target, ok := resolveCCVMSnapStartedTarget(cfg, currentDC)
	if ok && strings.TrimSpace(target.Target) != "" {
		return isLocalTarget(target.Target)
	}
	return isLocalTarget(currentDC)
}

func isCCVMBackupScheduleDue(section *ccvmBackupScheduleSection, now time.Time) (bool, string) {
	if section == nil || !section.Active {
		return false, ""
	}
	hour, minute, err := parseCCVMBackupTime(section.Time)
	if err != nil {
		return false, ""
	}
	if now.Minute() != minute {
		return false, ""
	}
	key := ""
	switch section.Repeat {
	case "hourly":
		key = now.Format("2006010215")
	case "daily":
		if now.Hour() != hour {
			return false, ""
		}
		key = now.Format("20060102")
	case "monthly":
		if now.Hour() != hour || now.Day() != section.Day {
			return false, ""
		}
		key = now.Format("200601")
	case "yearly":
		if now.Hour() != hour || int(now.Month()) != section.Month || now.Day() != section.Day {
			return false, ""
		}
		key = now.Format("2006")
	default:
		return false, ""
	}
	if section.LastRunKey == key {
		return false, ""
	}
	return true, key
}

func deleteExpiredCCVMBackups(targetDir string, retentionMonths int) error {
	if retentionMonths < 1 {
		retentionMonths = 1
	}
	cutoff := time.Now().Add(-time.Duration(retentionMonths*30) * 24 * time.Hour)
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), ccvmBackupTargetPrefix+"-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(targetDir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveCCVMBackupTargetDir(cfg *CubeModel.ClusterConfigSection, requested string) (string, error) {
	targetDir := strings.TrimSpace(requested)
	if targetDir == "" && cfg != nil {
		targetDir = strings.TrimSpace(cfg.BackupPath)
	}
	if targetDir == "" {
		if isCCVMBackupStandalone(cfg) {
			targetDir = ccvmBackupStandaloneDefaultDir
		} else if _, err := os.Stat("/mnt/glue-gfs"); err == nil {
			targetDir = ccvmBackupDefaultDir
		} else if _, err := os.Stat("/mnt/glue"); err == nil {
			targetDir = ccvmBackupStandaloneDefaultDir
		} else {
			targetDir = ccvmBackupDefaultDir
		}
	}
	if !filepath.IsAbs(targetDir) {
		return "", fmt.Errorf("backup target dir must be an absolute path")
	}
	return filepath.Clean(targetDir), nil
}

func updateCCVMBackupConfigSection(targetDir string, sectionName string, section *ccvmBackupScheduleSection) error {
	config, _ := loadCCVMBackupConfig(targetDir)
	config.TargetDir = targetDir
	config.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	switch sectionName {
	case "backup":
		config.Backup = section
	case "delete":
		config.Delete = section
	default:
		return fmt.Errorf("invalid config section")
	}
	return saveCCVMBackupConfig(targetDir, config)
}

func loadCCVMBackupConfig(targetDir string) (ccvmBackupScheduleConfig, error) {
	config := ccvmBackupScheduleConfig{TargetDir: targetDir}
	content, err := os.ReadFile(ccvmBackupConfigPath(targetDir))
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return config, err
	}
	if err := json.Unmarshal(content, &config); err != nil {
		return ccvmBackupScheduleConfig{TargetDir: targetDir}, err
	}
	if strings.TrimSpace(config.TargetDir) == "" {
		config.TargetDir = targetDir
	}
	return config, nil
}

func saveCCVMBackupConfig(targetDir string, config ccvmBackupScheduleConfig) error {
	config.TargetDir = targetDir
	config.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	path := ccvmBackupConfigPath(targetDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func ccvmBackupConfigPath(targetDir string) string {
	return filepath.Join(targetDir, ccvmBackupScriptDirName, ccvmBackupConfigFileName)
}

func parseCCVMBackupTime(raw string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("time must be HH:MM")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("time must be HH:MM")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("time must be HH:MM")
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("time out of range")
	}
	return hour, minute, nil
}

func parseCCVMBackupTimeFromName(name string, fallback time.Time) time.Time {
	re := regexp.MustCompile(`(\d{8})_(\d{6})`)
	matches := re.FindStringSubmatch(name)
	if len(matches) != 3 {
		return fallback
	}
	parsed, err := time.ParseInLocation("20060102_150405", matches[1]+"_"+matches[2], time.Local)
	if err != nil {
		return fallback
	}
	return parsed
}

func formatCCVMBackupSize(sizeBytes int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	size := float64(sizeBytes)
	for i, unit := range units {
		if size < 1024 || i == len(units)-1 {
			if unit == "B" {
				return fmt.Sprintf("%d %s", int64(size), unit)
			}
			return fmt.Sprintf("%.1f %s", size, unit)
		}
		size /= 1024
	}
	return fmt.Sprintf("%d B", sizeBytes)
}

func copyFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func resolveCCVMBackupXMLPath() string {
	return filepath.Join(resolveAbleStackXMLTemplatePath(), "ccvm-backup.xml")
}

func runCCVMBackupCommand(timeout time.Duration, command string, args ...string) (string, error) {
	env := ccvmSnapCommandEnv()
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

func callCCVMBackupRemote(target string, req CCVMBackupRequest, modeHeader string) (CCVMBackupResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return CCVMBackupResponse{}, err
	}
	url := fmt.Sprintf("%s/api/v1/cube/ccvm/backup", buildTargetURL(target))
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return CCVMBackupResponse{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	if modeHeader != "" {
		httpReq.Header.Set(modeHeader, "1")
	}

	client := &http.Client{Timeout: ccvmBackupRequestTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return CCVMBackupResponse{}, err
	}
	defer resp.Body.Close()

	var out CCVMBackupResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CCVMBackupResponse{}, err
	}
	if out.Code == 0 {
		out.Code = resp.StatusCode
	}
	if strings.TrimSpace(out.Target) == "" {
		out.Target = target
	}
	return out, nil
}

func callCCVMRestoreRemote(target string, req CCVMRestoreRequest, modeHeader string) (CCVMBackupResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return CCVMBackupResponse{}, err
	}
	url := fmt.Sprintf("%s/api/v1/cube/ccvm/restore", buildTargetURL(target))
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return CCVMBackupResponse{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	if modeHeader != "" {
		httpReq.Header.Set(modeHeader, "1")
	}

	client := &http.Client{Timeout: ccvmBackupRequestTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return CCVMBackupResponse{}, err
	}
	defer resp.Body.Close()

	var out CCVMBackupResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CCVMBackupResponse{}, err
	}
	if out.Code == 0 {
		out.Code = resp.StatusCode
	}
	if strings.TrimSpace(out.Target) == "" {
		out.Target = target
	}
	return out, nil
}

func isCCVMBackupStandalone(cfg *CubeModel.ClusterConfigSection) bool {
	if cfg == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg.Type), "ablestack-standalone")
}

func ccvmRestoreOK(target string, val any) CCVMBackupResponse {
	return CCVMBackupResponse{
		Code:    200,
		Val:     val,
		Message: "ok",
		Target:  target,
		Action:  "restore",
	}
}

func ccvmRestoreError(target string, message string) CCVMBackupResponse {
	return CCVMBackupResponse{
		Code:    500,
		Val:     message,
		Message: message,
		Target:  target,
		Action:  "restore",
	}
}

func ccvmBackupOK(req CCVMBackupRequest, target string, val any) CCVMBackupResponse {
	return CCVMBackupResponse{
		Code:    200,
		Val:     val,
		Message: "ok",
		Target:  target,
		Action:  req.Action,
	}
}

func ccvmBackupError(req CCVMBackupRequest, target string, message string) CCVMBackupResponse {
	return CCVMBackupResponse{
		Code:    500,
		Val:     message,
		Message: message,
		Target:  target,
		Action:  req.Action,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
