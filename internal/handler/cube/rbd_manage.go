package cube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type RBDManageRequest = CubeModel.RBDManageRequest
type RBDManageResponse = CubeModel.RBDManageResponse
type RBDManageTargetResult = CubeModel.RBDManageTargetResult

const (
	rbdManageLocalHeader   = "X-Cube-RBD-Manage-Local"
	rbdManageRetName       = "RBD Manage"
	rbdManageDefaultPool   = "rbd"
	rbdManageDefaultPrefix = "gfs"
	rbdManageChunkGiB      = 2000
	rbdManageRBDMapPath    = "/etc/ceph/rbdmap"
	rbdManageKeyringOption = "id=admin,keyring=/etc/ceph/ceph.client.admin.keyring"
	rbdManageCommandTO     = 30 * time.Minute
	rbdManageShortTO       = 30 * time.Second
	rbdManageRequestTO     = 35 * time.Minute
)

type rbdManageTarget struct {
	Hostname string
	Target   string
}

// RBDManage godoc
//
//	@Summary		RBD Manage
//	@Description	GFS용 RBD 이미지를 생성/삭제하고 cluster.json hosts[].ablecube 대상 API를 호출해 각 호스트의 /etc/ceph/rbdmap을 반영합니다. SSH는 사용하지 않습니다.
//	@Tags			CUBE - GFS
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.RBDManageRequest	true	"rbd manage request"
//	@Success		200	{object}	CubeModel.RBDManageResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/rbd/manage [post]
func RBDManage(context *gin.Context) {
	var req RBDManageRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	localRequest := isRBDManageLocalRequest(context)
	if err := normalizeRBDManageRequest(&req, localRequest); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	if localRequest {
		result := runRBDManageLocal(req, rbdManageTarget{Target: "local"})
		resp := rbdManageResponse(req, result.Images, []RBDManageTargetResult{result})
		context.JSON(statusCodeFromRBDManageResponse(resp), resp)
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

	resp := runRBDManage(req, cfg)
	context.JSON(statusCodeFromRBDManageResponse(resp), resp)
}

func normalizeRBDManageRequest(req *RBDManageRequest, localRequest bool) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "create":
		req.Action = "create"
	case "delete":
		req.Action = "delete"
	default:
		return fmt.Errorf("unsupported action")
	}

	req.PoolName = strings.TrimSpace(req.PoolName)
	if req.PoolName == "" {
		req.PoolName = rbdManageDefaultPool
	}
	req.ImagePrefix = strings.TrimSpace(req.ImagePrefix)
	if req.ImagePrefix == "" {
		req.ImagePrefix = rbdManageDefaultPrefix
	}
	req.ImageName = strings.TrimSpace(req.ImageName)

	if req.Action == "create" && !localRequest && req.Size <= 0 {
		return fmt.Errorf("size required")
	}
	if req.Action == "create" && localRequest && len(req.Images) == 0 {
		return fmt.Errorf("images required")
	}
	if req.Action == "delete" && strings.TrimSpace(req.ImageName) == "" && len(req.Images) == 0 {
		return fmt.Errorf("image_name or images required")
	}
	return nil
}

func isRBDManageLocalRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(rbdManageLocalHeader)), "1")
}

func runRBDManage(req RBDManageRequest, cfg *CubeModel.ClusterConfigSection) RBDManageResponse {
	targets := buildRBDManageTargets(cfg)
	if len(targets) == 0 {
		return rbdManageError(req, nil, "hosts[].ablecube required", nil)
	}

	switch req.Action {
	case "create":
		created, err := createRBDManageImages(req.PoolName, req.ImagePrefix, req.Size)
		if err != nil {
			return rbdManageError(req, created, err.Error(), nil)
		}
		results := applyRBDManageRBDMap(req, targets, created)
		return rbdManageResponse(req, created, results)
	case "delete":
		images := rbdManageRequestFullNames(req)
		results := applyRBDManageRBDMap(req, targets, images)
		if err := firstRBDManageResultError(results); err != nil {
			return rbdManageError(req, images, err.Error(), results)
		}
		deleted, err := deleteRBDManageImages(images)
		if err != nil {
			return rbdManageError(req, deleted, err.Error(), results)
		}
		return rbdManageResponse(req, deleted, results)
	default:
		return rbdManageError(req, nil, "unsupported action", nil)
	}
}

func buildRBDManageTargets(cfg *CubeModel.ClusterConfigSection) []rbdManageTarget {
	if cfg == nil {
		return nil
	}
	seen := map[string]struct{}{}
	targets := make([]rbdManageTarget, 0, len(cfg.Hosts))
	for _, host := range cfg.Hosts {
		target := strings.TrimSpace(host.Ablecube)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, rbdManageTarget{
			Hostname: strings.TrimSpace(host.Hostname),
			Target:   target,
		})
	}
	return targets
}

func createRBDManageImages(poolName string, imagePrefix string, totalSizeGiB int) ([]string, error) {
	sizes := splitRBDManageSize(totalSizeGiB, rbdManageChunkGiB)
	if len(sizes) == 0 {
		return nil, fmt.Errorf("size must be greater than 0")
	}
	startIndex, err := nextRBDManageImageIndex(poolName, imagePrefix)
	if err != nil {
		return nil, err
	}

	created := make([]string, 0, len(sizes))
	for offset, sizeGiB := range sizes {
		imageName := fmt.Sprintf("%s%02d", imagePrefix, startIndex+offset)
		fullName := rbdManageFullName(poolName, imageName)
		if _, err := runRBDManageCommand(rbdManageCommandTO, "rbd", "create", fullName, "--size", fmt.Sprintf("%dG", sizeGiB), "--image-feature", "layering"); err != nil {
			return created, err
		}
		created = append(created, fullName)
	}
	return created, nil
}

func splitRBDManageSize(sizeGiB int, chunkGiB int) []int {
	if sizeGiB <= 0 || chunkGiB <= 0 {
		return nil
	}
	parts := make([]int, 0, (sizeGiB/chunkGiB)+1)
	remaining := sizeGiB
	for remaining > chunkGiB {
		parts = append(parts, chunkGiB)
		remaining -= chunkGiB
	}
	if remaining > 0 {
		parts = append(parts, remaining)
	}
	return parts
}

func nextRBDManageImageIndex(poolName string, imagePrefix string) (int, error) {
	out, err := runRBDManageCommand(rbdManageShortTO, "rbd", "ls", poolName)
	if err != nil {
		return 0, err
	}

	pattern, err := regexp.Compile("^" + regexp.QuoteMeta(imagePrefix) + `(\d+)$`)
	if err != nil {
		return 0, err
	}
	maxIndex := 0
	for _, line := range splitLines(out) {
		match := pattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) != 2 {
			continue
		}
		idx, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		if idx > maxIndex {
			maxIndex = idx
		}
	}
	return maxIndex + 1, nil
}

func applyRBDManageRBDMap(req RBDManageRequest, targets []rbdManageTarget, images []string) []RBDManageTargetResult {
	client := &http.Client{Timeout: rbdManageRequestTO}
	results := make([]RBDManageTargetResult, 0, len(targets))
	localReq := req
	localReq.Images = images

	for _, target := range targets {
		if isLocalTarget(target.Target) {
			results = append(results, runRBDManageLocal(localReq, target))
			continue
		}
		result, err := callRBDManageRemote(client, target, localReq)
		if err != nil {
			result = RBDManageTargetResult{
				Hostname: target.Hostname,
				Target:   target.Target,
				Code:     http.StatusInternalServerError,
				Message:  err.Error(),
				Images:   images,
			}
		}
		results = append(results, result)
	}
	return results
}

func runRBDManageLocal(req RBDManageRequest, target rbdManageTarget) RBDManageTargetResult {
	images := rbdManageRequestFullNames(req)
	result := RBDManageTargetResult{
		Hostname: target.Hostname,
		Target:   firstNonEmpty(target.Target, "local"),
		Code:     http.StatusOK,
		Message:  "ok",
		Images:   images,
	}
	if len(images) == 0 {
		result.Code = http.StatusBadRequest
		result.Message = "images required"
		return result
	}

	var err error
	switch req.Action {
	case "create":
		err = addRBDManageMapEntries(images)
		if err == nil {
			err = enableOrRestartRBDMapService()
		}
	case "delete":
		err = removeRBDManageMapEntries(images)
		if err == nil {
			err = restartRBDMapService()
		}
	default:
		err = fmt.Errorf("unsupported action")
	}
	if err != nil {
		result.Code = http.StatusInternalServerError
		result.Message = err.Error()
	}
	return result
}

func addRBDManageMapEntries(images []string) error {
	lines, err := readRBDManageMapLines()
	if err != nil {
		return err
	}
	exists := map[string]struct{}{}
	for _, line := range lines {
		exists[line] = struct{}{}
	}
	for _, image := range images {
		line := rbdManageMapLine(image)
		if _, ok := exists[line]; ok {
			continue
		}
		lines = append(lines, line)
		exists[line] = struct{}{}
	}
	return writeRBDManageMapLines(lines)
}

func removeRBDManageMapEntries(images []string) error {
	lines, err := readRBDManageMapLines()
	if err != nil {
		return err
	}
	removeSet := map[string]struct{}{}
	for _, image := range images {
		removeSet[image] = struct{}{}
	}

	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) > 0 {
			if _, ok := removeSet[fields[0]]; ok {
				continue
			}
		}
		kept = append(kept, line)
	}
	return writeRBDManageMapLines(kept)
}

func readRBDManageMapLines() ([]string, error) {
	data, err := os.ReadFile(rbdManageRBDMapPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	raw := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func writeRBDManageMapLines(lines []string) error {
	if err := os.MkdirAll(filepath.Dir(rbdManageRBDMapPath), 0o755); err != nil {
		return err
	}
	content := ""
	if len(lines) > 0 {
		content = strings.Join(lines, "\n") + "\n"
	}
	tmp := rbdManageRBDMapPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, rbdManageRBDMapPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func enableOrRestartRBDMapService() error {
	_, timedOut, err := runCommandOutputWithEnv("systemctl", rbdManageShortTO, rbdManageCommandEnv(), "is-enabled", "--quiet", "rbdmap.service")
	if timedOut {
		return fmt.Errorf("systemctl is-enabled rbdmap.service timed out after %s", rbdManageShortTO)
	}
	if err == nil {
		return restartRBDMapService()
	}
	_, err = runRBDManageCommand(rbdManageShortTO, "systemctl", "enable", "--now", "rbdmap.service")
	return err
}

func restartRBDMapService() error {
	_, err := runRBDManageCommand(rbdManageShortTO, "systemctl", "restart", "rbdmap.service")
	return err
}

func deleteRBDManageImages(images []string) ([]string, error) {
	deleted := make([]string, 0, len(images))
	for _, image := range images {
		if _, err := runRBDManageCommand(rbdManageCommandTO, "rbd", "rm", "--no-progress", image); err != nil {
			return deleted, err
		}
		deleted = append(deleted, image)
	}
	return deleted, nil
}

func callRBDManageRemote(client *http.Client, target rbdManageTarget, req RBDManageRequest) (RBDManageTargetResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return RBDManageTargetResult{}, err
	}
	url := fmt.Sprintf("%s/api/v1/cube/rbd/manage", buildTargetURL(target.Target))
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return RBDManageTargetResult{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(rbdManageLocalHeader, "1")

	resp, err := client.Do(httpReq)
	if err != nil {
		return RBDManageTargetResult{}, err
	}
	defer resp.Body.Close()

	var out RBDManageResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		if resp.StatusCode >= 300 {
			return RBDManageTargetResult{}, fmt.Errorf("rbd manage failed: %s", resp.Status)
		}
		return RBDManageTargetResult{}, err
	}
	if len(out.Results) > 0 {
		result := out.Results[0]
		result.Hostname = firstNonEmpty(result.Hostname, target.Hostname)
		if strings.TrimSpace(result.Target) == "" || strings.EqualFold(strings.TrimSpace(result.Target), "local") {
			result.Target = target.Target
		}
		return result, nil
	}
	if out.Code != http.StatusOK {
		return RBDManageTargetResult{}, fmt.Errorf("rbd manage failed: %s", firstNonEmpty(out.Message, fmt.Sprint(out.Val), resp.Status))
	}
	return RBDManageTargetResult{
		Hostname: target.Hostname,
		Target:   target.Target,
		Code:     http.StatusOK,
		Message:  "ok",
		Images:   req.Images,
	}, nil
}

func rbdManageRequestFullNames(req RBDManageRequest) []string {
	raw := make([]string, 0, len(req.Images)+1)
	for _, image := range req.Images {
		image = strings.TrimSpace(image)
		if image != "" {
			raw = append(raw, image)
		}
	}
	for _, image := range strings.Split(req.ImageName, ",") {
		image = strings.TrimSpace(image)
		if image != "" {
			raw = append(raw, image)
		}
	}

	seen := map[string]struct{}{}
	images := make([]string, 0, len(raw))
	for _, image := range raw {
		imageName := normalizeRBDManageImageName(image)
		if imageName == "" {
			continue
		}
		fullName := rbdManageFullName(req.PoolName, imageName)
		if _, ok := seen[fullName]; ok {
			continue
		}
		seen[fullName] = struct{}{}
		images = append(images, fullName)
	}
	return images
}

func normalizeRBDManageImageName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return ""
	}
	parts := strings.Split(name, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

func rbdManageFullName(poolName string, imageName string) string {
	return strings.TrimSpace(poolName) + "/" + strings.TrimSpace(imageName)
}

func rbdManageMapLine(image string) string {
	return strings.TrimSpace(image) + " " + rbdManageKeyringOption
}

func runRBDManageCommand(timeout time.Duration, command string, args ...string) (string, error) {
	out, timedOut, err := runCommandOutputWithEnv(command, timeout, rbdManageCommandEnv(), args...)
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

func rbdManageCommandEnv() []string {
	return append([]string{"LANG=en_US.utf-8", "LANGUAGE=en"}, os.Environ()...)
}

func firstRBDManageResultError(results []RBDManageTargetResult) error {
	for _, result := range results {
		if result.Code != http.StatusOK {
			return fmt.Errorf("rbdmap update failed: %s: %s", result.Target, result.Message)
		}
	}
	return nil
}

func rbdManageResponse(req RBDManageRequest, val any, results []RBDManageTargetResult) RBDManageResponse {
	code := http.StatusOK
	message := "rbd manage success"
	if err := firstRBDManageResultError(results); err != nil {
		code = http.StatusInternalServerError
		message = err.Error()
	}
	return RBDManageResponse{
		Code:    code,
		Val:     val,
		RetName: rbdManageRetName,
		Message: message,
		Action:  req.Action,
		Pool:    req.PoolName,
		Results: results,
	}
}

func rbdManageError(req RBDManageRequest, val any, message string, results []RBDManageTargetResult) RBDManageResponse {
	return RBDManageResponse{
		Code:    http.StatusInternalServerError,
		Val:     val,
		RetName: rbdManageRetName,
		Message: message,
		Action:  req.Action,
		Pool:    req.PoolName,
		Results: results,
	}
}

func statusCodeFromRBDManageResponse(resp RBDManageResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
