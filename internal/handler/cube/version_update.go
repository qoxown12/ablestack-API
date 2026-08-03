package cube

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type VersionUpdateRequest = CubeModel.VersionUpdateRequest
type VersionUpdateResponse = CubeModel.VersionUpdateResponse
type VersionUpdateInfo = CubeModel.VersionUpdateInfo
type VersionUpdateRunResult = CubeModel.VersionUpdateRunResult

const (
	versionUpdateOSReleasePath  = "/etc/os-release"
	versionUpdateTargetKSPath   = "ks/ablestack-ks.cfg"
	versionUpdateMoldRPMDir     = "AppStream/Packages/mold"
	versionUpdateCopyPath       = "/opt/ABLESTACK_UPDATE"
	versionUpdateTypeAll        = "all"
	versionUpdateTypeMold       = "mold"
	versionUpdateScriptAll      = "update-all.sh"
	versionUpdateScriptMold     = "update-mold.sh"
	versionUpdateCommandTimeout = 6 * time.Hour
	versionUpdateSuccessRetName = "ABLESTACK Version Update"
	versionUpdateInfoRetName    = "ABLESTACK Version Update Info"
)

var versionUpdateTargetMoldVersionKeys = []string{
	"MOLD_VERSION",
	"ACS_VERSION",
	"CLOUDSTACK_VERSION",
}

var versionUpdateMoldHelpTextRelativePaths = []string{
	strings.TrimPrefix(versionMoldHelpTextPath, "/"),
	filepath.Base(versionMoldHelpTextPath),
}

var versionUpdateMoldRPMPrefixes = []string{
	"cloudstack-common",
	"cloudstack-management",
	"cloudstack-agent",
	"cloudstack-ui",
	"cloudstack-usage",
	"mold",
}

type versionUpdateScriptInfo struct {
	UpdateType string
	Label      string
	Script     string
}

// VersionUpdate godoc
//
//	@Summary		ABLESTACK Version Update
//	@Description	마운트된 ABLESTACK ISO의 버전 정보를 조회하거나 /opt/ABLESTACK_UPDATE로 복사한 뒤 update-all.sh/update-mold.sh를 실행합니다.
//	@Tags			Cube-Version
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.VersionUpdateRequest	true	"version update request"
//	@Success		200	{object}	CubeModel.VersionUpdateResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/version/update [post]
func VersionUpdate(context *gin.Context) {
	var req VersionUpdateRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := normalizeVersionUpdateRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	var (
		resp VersionUpdateResponse
		err  error
	)
	switch req.Action {
	case "info":
		resp, err = readVersionUpdateInfo(req.MountPath, req.UpdateType)
	case "run":
		if err = ensureVersionUpdateReady(); err == nil {
			resp, err = runVersionUpdate(req.MountPath, req.UpdateType)
		}
	default:
		err = fmt.Errorf("unsupported action")
	}

	if err != nil {
		context.JSON(http.StatusInternalServerError, VersionUpdateResponse{
			Code:    http.StatusInternalServerError,
			Val:     err.Error(),
			Message: err.Error(),
			Action:  req.Action,
		})
		return
	}

	context.JSON(statusCodeFromVersionUpdateResponse(resp), resp)
}

func normalizeVersionUpdateRequest(req *VersionUpdateRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "info":
		req.Action = "info"
	case "run":
		req.Action = "run"
	default:
		return fmt.Errorf("unsupported action")
	}
	req.MountPath = strings.TrimSpace(req.MountPath)
	if req.MountPath == "" {
		return fmt.Errorf("mount_path required")
	}
	updateType, err := normalizeVersionUpdateType(req.UpdateType)
	if err != nil {
		return err
	}
	req.UpdateType = updateType
	return nil
}

func readVersionUpdateInfo(rawMountPath string, updateType string) (VersionUpdateResponse, error) {
	mountPath, err := validateVersionUpdateMountPath(rawMountPath)
	if err != nil {
		return VersionUpdateResponse{}, err
	}
	scriptInfo, err := versionUpdateScriptInfoFor(updateType)
	if err != nil {
		return VersionUpdateResponse{}, err
	}

	ksPath := filepath.Join(mountPath, versionUpdateTargetKSPath)
	updateScript := filepath.Join(mountPath, scriptInfo.Script)
	if err := requireRegularFile(ksPath, fmt.Sprintf("%s 파일을 찾을 수 없습니다.", versionUpdateTargetKSPath)); err != nil {
		return VersionUpdateResponse{}, err
	}
	if err := requireRegularFile(updateScript, fmt.Sprintf("%s 파일을 찾을 수 없습니다.", scriptInfo.Script)); err != nil {
		return VersionUpdateResponse{}, err
	}

	currentInfo, err := parseVersionUpdateKeyValues(versionUpdateOSReleasePath)
	if err != nil && !os.IsNotExist(err) {
		return VersionUpdateResponse{}, err
	}
	targetInfo, err := parseVersionUpdateKeyValues(ksPath)
	if err != nil {
		return VersionUpdateResponse{}, err
	}

	targetVersion := strings.TrimSpace(targetInfo["ABLESTACK_VERSION"])
	if targetVersion == "" {
		return VersionUpdateResponse{}, fmt.Errorf("%s 파일에서 ABLESTACK_VERSION 값을 찾을 수 없습니다.", versionUpdateTargetKSPath)
	}

	currentOSVersion := firstNonEmpty(currentInfo["PRETTY_NAME"], "N/A")
	currentMoldVersion := readVersionValue(versionMoldHelpTextPath, "ACS_VERSION")
	targetMoldVersion := readVersionUpdateTargetMoldVersion(mountPath, targetInfo)

	info := VersionUpdateInfo{
		MountPath:               mountPath,
		CopyPath:                versionUpdateCopyPath,
		CurrentOSVersion:        currentOSVersion,
		CurrentMoldVersion:      currentMoldVersion,
		TargetOSVersion:         targetVersion,
		TargetMoldVersion:       targetMoldVersion,
		CurrentAblestackVersion: currentOSVersion,
		TargetAblestackVersion:  targetVersion,
		UpdateType:              scriptInfo.UpdateType,
		UpdateLabel:             scriptInfo.Label,
		UpdateScript:            updateScript,
		WorkUpdateScript:        filepath.Join(versionUpdateCopyPath, scriptInfo.Script),
	}
	return VersionUpdateResponse{
		Code:    http.StatusOK,
		Val:     info,
		RetName: versionUpdateInfoRetName,
		Message: "ok",
		Action:  "info",
	}, nil
}

func runVersionUpdate(rawMountPath string, updateType string) (VersionUpdateResponse, error) {
	infoResp, err := readVersionUpdateInfo(rawMountPath, updateType)
	if err != nil {
		return VersionUpdateResponse{}, err
	}
	info, ok := infoResp.Val.(VersionUpdateInfo)
	if !ok {
		return VersionUpdateResponse{}, fmt.Errorf("version update info parse failed")
	}
	scriptInfo, err := versionUpdateScriptInfoFor(info.UpdateType)
	if err != nil {
		return VersionUpdateResponse{}, err
	}
	workPath, err := prepareVersionUpdateWorkDir(info.MountPath)
	if err != nil {
		return VersionUpdateResponse{}, err
	}
	workUpdateScript := filepath.Join(workPath, scriptInfo.Script)
	if err := requireRegularFile(workUpdateScript, fmt.Sprintf("%s 파일을 찾을 수 없습니다.", scriptInfo.Script)); err != nil {
		return VersionUpdateResponse{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), versionUpdateCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/bash", "./"+scriptInfo.Script)
	cmd.Dir = workPath
	cmd.Env = append(
		os.Environ(),
		"ABLESTACK_UPDATE_MOUNT_PATH="+info.MountPath,
		"ABLESTACK_UPDATE_WORK_PATH="+workPath,
		"ABLESTACK_UPDATE_COPY_PATH="+workPath,
		"ABLESTACK_UPDATE_TYPE="+scriptInfo.UpdateType,
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return VersionUpdateResponse{}, fmt.Errorf("ABLESTACK Version 업데이트 실행 시간이 초과되었습니다.")
	}
	if err != nil {
		message := firstNonEmpty(strings.TrimSpace(stderr.String()), strings.TrimSpace(stdout.String()), "ABLESTACK Version 업데이트 실행 중 오류가 발생했습니다.")
		return VersionUpdateResponse{}, fmt.Errorf("%s", message)
	}

	result := VersionUpdateRunResult{
		Message:            fmt.Sprintf("ABLESTACK %s 실행이 완료되었습니다.", scriptInfo.Label),
		MountPath:          info.MountPath,
		CopyPath:           workPath,
		CurrentOSVersion:   info.CurrentOSVersion,
		CurrentMoldVersion: info.CurrentMoldVersion,
		TargetOSVersion:    info.TargetOSVersion,
		TargetMoldVersion:  info.TargetMoldVersion,
		UpdateType:         scriptInfo.UpdateType,
		UpdateLabel:        scriptInfo.Label,
		UpdateScript:       workUpdateScript,
		Stdout:             strings.TrimSpace(stdout.String()),
		Stderr:             strings.TrimSpace(stderr.String()),
	}
	return VersionUpdateResponse{
		Code:    http.StatusOK,
		Val:     result,
		RetName: versionUpdateSuccessRetName,
		Message: result.Message,
		Action:  "run",
	}, nil
}

func ensureVersionUpdateReady() error {
	data, err := cachedDeployStatus()
	if err != nil {
		return fmt.Errorf("ABLESTACK 업데이트 실행 조건 확인 실패: %w", err)
	}
	if data.Stage != CubeModel.DeployStageReady {
		return fmt.Errorf("ABLESTACK 업데이트는 전체 구성이 완료된 후 실행할 수 있습니다. current_stage=%s", firstNonEmpty(data.Stage, "unknown"))
	}
	return nil
}

func readVersionUpdateTargetMoldVersion(mountPath string, targetInfo map[string]string) string {
	if value := findVersionUpdateRPMVersion(mountPath); value != "" {
		return value
	}
	for _, key := range versionUpdateTargetMoldVersionKeys {
		if value := strings.TrimSpace(targetInfo[key]); value != "" {
			return value
		}
	}
	for _, relativePath := range versionUpdateMoldHelpTextRelativePaths {
		if value := readVersionUpdateMountedKeyValue(mountPath, relativePath, "ACS_VERSION"); value != "" {
			return value
		}
	}
	if value := findVersionUpdateMountedKeyValue(mountPath, filepath.Base(versionMoldHelpTextPath), "ACS_VERSION"); value != "" {
		return value
	}
	return "N/A"
}

func readVersionUpdateMountedKeyValue(mountPath string, relativePath string, key string) string {
	values, err := parseVersionUpdateKeyValues(filepath.Join(mountPath, relativePath))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(values[key])
}

func findVersionUpdateMountedKeyValue(root string, filename string, key string) string {
	found := ""
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if entry.IsDir() || entry.Name() != filename {
			return nil
		}
		values, parseErr := parseVersionUpdateKeyValues(path)
		if parseErr != nil {
			return nil
		}
		found = strings.TrimSpace(values[key])
		if found != "" {
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func findVersionUpdateRPMVersion(root string) string {
	if value := findVersionUpdateRPMVersionInDir(filepath.Join(root, filepath.FromSlash(versionUpdateMoldRPMDir))); value != "" {
		return value
	}

	versionsByPrefix := make(map[string][]string, len(versionUpdateMoldRPMPrefixes))
	_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".rpm") {
			return nil
		}
		for _, prefix := range versionUpdateMoldRPMPrefixes {
			if version := parseVersionUpdateRPMVersion(name, prefix); version != "" {
				versionsByPrefix[prefix] = append(versionsByPrefix[prefix], version)
			}
		}
		return nil
	})

	return latestVersionUpdateRPMVersion(versionsByPrefix)
}

func findVersionUpdateRPMVersionInDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	versionsByPrefix := make(map[string][]string, len(versionUpdateMoldRPMPrefixes))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".rpm") {
			continue
		}
		for _, prefix := range versionUpdateMoldRPMPrefixes {
			if version := parseVersionUpdateRPMVersion(name, prefix); version != "" {
				versionsByPrefix[prefix] = append(versionsByPrefix[prefix], version)
			}
		}
	}
	return latestVersionUpdateRPMVersion(versionsByPrefix)
}

func latestVersionUpdateRPMVersion(versionsByPrefix map[string][]string) string {
	for _, prefix := range versionUpdateMoldRPMPrefixes {
		versions := versionsByPrefix[prefix]
		if len(versions) == 0 {
			continue
		}
		sort.Strings(versions)
		return versions[len(versions)-1]
	}
	return ""
}

func parseVersionUpdateRPMVersion(filename string, prefix string) string {
	trimmed := strings.TrimSuffix(filename, ".rpm")
	rest := strings.TrimPrefix(trimmed, prefix+"-")
	if rest == trimmed {
		return ""
	}
	version, releaseAndArch, ok := strings.Cut(rest, "-")
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	if version[0] < '0' || version[0] > '9' {
		return ""
	}
	if ok {
		if displayVersion := formatVersionUpdateRPMDisplayVersion(version, releaseAndArch); displayVersion != "" {
			return displayVersion
		}
	}
	return version
}

func formatVersionUpdateRPMDisplayVersion(version string, releaseAndArch string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(releaseAndArch), ".")
	for i, part := range parts {
		if isVersionUpdateRPMDateToken(part) {
			return version + "-" + strings.Join(parts[:i+1], ".")
		}
	}
	return version
}

func isVersionUpdateRPMDateToken(value string) bool {
	if len(value) < 8 || len(value) > 14 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func normalizeVersionUpdateType(rawType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(rawType)) {
	case "", versionUpdateTypeAll, "update-all", versionUpdateScriptAll:
		return versionUpdateTypeAll, nil
	case versionUpdateTypeMold, "update-mold", versionUpdateScriptMold:
		return versionUpdateTypeMold, nil
	default:
		return "", fmt.Errorf("unsupported update_type")
	}
}

func versionUpdateScriptInfoFor(updateType string) (versionUpdateScriptInfo, error) {
	normalizedType, err := normalizeVersionUpdateType(updateType)
	if err != nil {
		return versionUpdateScriptInfo{}, err
	}
	switch normalizedType {
	case versionUpdateTypeAll:
		return versionUpdateScriptInfo{
			UpdateType: versionUpdateTypeAll,
			Label:      "전체 업데이트",
			Script:     versionUpdateScriptAll,
		}, nil
	case versionUpdateTypeMold:
		return versionUpdateScriptInfo{
			UpdateType: versionUpdateTypeMold,
			Label:      "Mold 업데이트",
			Script:     versionUpdateScriptMold,
		}, nil
	default:
		return versionUpdateScriptInfo{}, fmt.Errorf("unsupported update_type")
	}
}

func prepareVersionUpdateWorkDir(sourcePath string) (string, error) {
	targetPath := filepath.Clean(versionUpdateCopyPath)
	if err := validateVersionUpdateCopyTarget(sourcePath, targetPath); err != nil {
		return "", err
	}

	info, err := os.Lstat(targetPath)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s 경로가 심볼릭 링크입니다.", targetPath)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s 경로가 디렉터리가 아닙니다.", targetPath)
		}
		mounted, err := isVersionUpdateMountPoint(targetPath)
		if err != nil {
			return "", err
		}
		if mounted {
			return "", fmt.Errorf("%s 경로가 마운트 지점입니다.", targetPath)
		}
		if err := os.RemoveAll(targetPath); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return "", err
	}
	if err := copyVersionUpdateFiles(sourcePath, targetPath); err != nil {
		_ = os.RemoveAll(targetPath)
		return "", err
	}
	return targetPath, nil
}

func validateVersionUpdateCopyTarget(sourcePath string, targetPath string) error {
	sourcePath = filepath.Clean(sourcePath)
	targetPath = filepath.Clean(targetPath)
	if sourcePath == targetPath || pathIsInsideVersionUpdatePath(sourcePath, targetPath) || pathIsInsideVersionUpdatePath(targetPath, sourcePath) {
		return fmt.Errorf("ISO 마운트 경로와 복사 대상 경로를 분리해야 합니다.")
	}
	return nil
}

func pathIsInsideVersionUpdatePath(path string, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func isVersionUpdateMountPoint(path string) (bool, error) {
	raw, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	cleanPath := filepath.Clean(path)
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if filepath.Clean(unescapeAutoShutdownMountPath(fields[4])) == cleanPath {
			return true, nil
		}
	}
	return false, nil
}

func copyVersionUpdateFiles(sourcePath string, targetPath string) error {
	if err := runVersionUpdateCopyCommand(sourcePath, targetPath, "-rRp"); err == nil {
		return nil
	} else if !strings.Contains(err.Error(), "R and -r options may not be specified together") {
		return err
	}
	return runVersionUpdateCopyCommand(sourcePath, targetPath, "-Rp")
}

func runVersionUpdateCopyCommand(sourcePath string, targetPath string, option string) error {
	ctx, cancel := context.WithTimeout(context.Background(), versionUpdateCommandTimeout)
	defer cancel()

	sourceContents := filepath.Clean(sourcePath) + string(os.PathSeparator) + "."
	targetDir := filepath.Clean(targetPath) + string(os.PathSeparator)
	cmd := exec.CommandContext(ctx, "cp", option, sourceContents, targetDir)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("ISO 파일 복사 시간이 초과되었습니다.")
	}
	if err != nil {
		message := firstNonEmpty(strings.TrimSpace(stderr.String()), strings.TrimSpace(stdout.String()), "ISO 파일 복사 중 오류가 발생했습니다.")
		return fmt.Errorf("%s", message)
	}
	return nil
}

func validateVersionUpdateMountPath(rawPath string) (string, error) {
	if strings.TrimSpace(rawPath) == "" {
		return "", fmt.Errorf("ISO 마운트 경로를 입력해야 합니다.")
	}
	path := strings.TrimSpace(rawPath)
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("ISO 마운트 경로는 절대 경로로 입력해야 합니다.")
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("입력한 ISO 마운트 경로가 존재하지 않습니다.")
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("입력한 ISO 마운트 경로가 디렉터리가 아닙니다.")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func requireRegularFile(path string, missingMessage string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s", missingMessage)
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s 파일이 아닙니다.", filepath.Base(path))
	}
	return nil
}

func parseVersionUpdateKeyValues(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}, err
	}
	values := make(map[string]string)
	for _, rawLine := range strings.Split(string(raw), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		key, value, _ := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values[key] = normalizeVersionUpdateValue(value)
	}
	return values, nil
}

func normalizeVersionUpdateValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if first == last && (first == '\'' || first == '"') {
			if first == '"' {
				if unquoted, err := strconv.Unquote(value); err == nil {
					return unquoted
				}
			}
			return value[1 : len(value)-1]
		}
	}
	return value
}

func statusCodeFromVersionUpdateResponse(resp VersionUpdateResponse) int {
	if resp.Code >= 200 && resp.Code < 300 {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
