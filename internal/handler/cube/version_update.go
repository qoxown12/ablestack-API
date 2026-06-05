package cube

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
	versionUpdateOSReleasePath   = "/etc/os-release"
	versionUpdateTargetKSPath    = "ks/ablestack-ks.cfg"
	versionUpdateTargetScript    = "update.sh"
	versionUpdateCommandTimeout  = 6 * time.Hour
	versionUpdateSuccessRetName  = "ABLESTACK Version Update"
	versionUpdateInfoRetName     = "ABLESTACK Version Update Info"
	versionUpdateCompleteMessage = "ABLESTACK Version 업데이트 실행이 완료되었습니다."
)

// VersionUpdate godoc
//
//	@Summary		ABLESTACK Version Update
//	@Description	마운트된 ABLESTACK ISO의 버전 정보를 조회하거나 update.sh를 실행합니다.
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
		resp, err = readVersionUpdateInfo(req.MountPath)
	case "run":
		resp, err = runVersionUpdate(req.MountPath)
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
	return nil
}

func readVersionUpdateInfo(rawMountPath string) (VersionUpdateResponse, error) {
	mountPath, err := validateVersionUpdateMountPath(rawMountPath)
	if err != nil {
		return VersionUpdateResponse{}, err
	}

	ksPath := filepath.Join(mountPath, versionUpdateTargetKSPath)
	updateScript := filepath.Join(mountPath, versionUpdateTargetScript)
	if err := requireRegularFile(ksPath, fmt.Sprintf("%s 파일을 찾을 수 없습니다.", versionUpdateTargetKSPath)); err != nil {
		return VersionUpdateResponse{}, err
	}
	if err := requireRegularFile(updateScript, fmt.Sprintf("%s 파일을 찾을 수 없습니다.", versionUpdateTargetScript)); err != nil {
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

	info := VersionUpdateInfo{
		MountPath:               mountPath,
		CurrentAblestackVersion: firstNonEmpty(currentInfo["PRETTY_NAME"], "N/A"),
		TargetAblestackVersion:  targetVersion,
		UpdateScript:            updateScript,
	}
	return VersionUpdateResponse{
		Code:    http.StatusOK,
		Val:     info,
		RetName: versionUpdateInfoRetName,
		Message: "ok",
		Action:  "info",
	}, nil
}

func runVersionUpdate(rawMountPath string) (VersionUpdateResponse, error) {
	infoResp, err := readVersionUpdateInfo(rawMountPath)
	if err != nil {
		return VersionUpdateResponse{}, err
	}
	info, ok := infoResp.Val.(VersionUpdateInfo)
	if !ok {
		return VersionUpdateResponse{}, fmt.Errorf("version update info parse failed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), versionUpdateCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/bash", info.UpdateScript)
	cmd.Dir = info.MountPath
	cmd.Env = append(os.Environ(), "ABLESTACK_UPDATE_MOUNT_PATH="+info.MountPath)

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
		Message: versionUpdateCompleteMessage,
		Stdout:  strings.TrimSpace(stdout.String()),
		Stderr:  strings.TrimSpace(stderr.String()),
	}
	return VersionUpdateResponse{
		Code:    http.StatusOK,
		Val:     result,
		RetName: versionUpdateSuccessRetName,
		Message: versionUpdateCompleteMessage,
		Action:  "run",
	}, nil
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
