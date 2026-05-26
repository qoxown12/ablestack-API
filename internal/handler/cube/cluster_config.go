package cube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"ablecloud.io/ablestack-api/internal/service/clusterconfig"
	"github.com/gin-gonic/gin"
)

type TypeClusterConfig = CubeModel.TypeClusterConfig
type ClusterConfigApplyRequest = CubeModel.ClusterApplyRequest
type ClusterConfigApplyResult = CubeModel.ClusterApplyResult
type ClusterConfigApplyResponse = CubeModel.ClusterApplyResponse
type ClusterApplyLocalResponse = CubeModel.ClusterApplyLocalResponse
type ClusterHealthResponse = CubeModel.ClusterHealthResponse
type ClusterHealthTargetResult = CubeModel.ClusterHealthTargetResult

// ClusterHealth godoc
//
//	@Summary		Cluster Health
//	@Description	API 서버 생존 확인 및 대상 노드 상태 확인
//	@Tags			CUBE - Cluster
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Param			option			query	string	false	"host|scvm|ccvm"
//	@Param			target_hostname	query	string	false	"comma-separated hostnames"
//	@Success		200	{object}	CubeModel.ClusterHealthResponse
//	@Router			/cube/cluster/health [get]
func ClusterHealth(context *gin.Context) {
	option := strings.TrimSpace(context.Query("option"))
	if option == "" {
		context.JSON(http.StatusOK, ClusterHealthResponse{Status: "ok"})
		return
	}

	option = normalizeHealthOption(option)
	if option == "" {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid option",
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

	targetHostnames := parseTargetHostnames(context.Query("target_hostname"))
	targets, err := buildHealthTargets(option, cfg, targetHostnames)
	if err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	if len(targets) == 0 {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "no targets to apply",
		})
		return
	}

	results := checkHealthTargets(targets)
	hasFail := false
	for _, res := range results {
		if res.Code != 200 {
			hasFail = true
			break
		}
	}
	resp := ClusterHealthResponse{
		Code:    200,
		Message: "health check success",
		Results: results,
	}
	if hasFail {
		resp.Code = 500
		resp.Message = "health check failed"
		context.JSON(http.StatusInternalServerError, resp)
		return
	}
	context.JSON(http.StatusOK, resp)
}

// healthTarget은 health 점검 시 실제 호출할 대상 노드 정보를 담는 내부 모델이다.
type healthTarget struct {
	Role     string
	Hostname string
	Target   string
}

// normalizeHealthOption은 health 조회 옵션을 host/scvm/ccvm 중 하나로 정규화한다.
func normalizeHealthOption(option string) string {
	switch strings.ToLower(strings.TrimSpace(option)) {
	case "host":
		return "host"
	case "scvm":
		return "scvm"
	case "ccvm":
		return "ccvm"
	default:
		return ""
	}
}

// parseTargetHostnames는 콤마로 전달된 hostname 목록을 정리해 반환한다.
func parseTargetHostnames(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return dedupeHosts(out)
}

// buildHealthTargets는 옵션과 hostname 필터에 맞는 health 점검 대상 목록을 만든다.
func buildHealthTargets(option string, cfg *CubeModel.ClusterConfigSection, targetHostnames []string) ([]healthTarget, error) {
	var hosts []CubeModel.ClusterHost
	if len(targetHostnames) == 0 {
		hosts = cfg.Hosts
	} else {
		hostMap := map[string]CubeModel.ClusterHost{}
		for _, host := range cfg.Hosts {
			if strings.TrimSpace(host.Hostname) == "" {
				continue
			}
			hostMap[strings.TrimSpace(host.Hostname)] = host
		}
		for _, name := range targetHostnames {
			host, ok := hostMap[name]
			if !ok {
				continue
			}
			hosts = append(hosts, host)
		}
		if len(hosts) == 0 {
			return nil, fmt.Errorf("target_hostname not found")
		}
	}

	targets := make([]healthTarget, 0)
	switch option {
	case "host":
		for _, host := range hosts {
			if strings.TrimSpace(host.Ablecube) == "" {
				continue
			}
			targets = append(targets, healthTarget{
				Role:     "host",
				Hostname: host.Hostname,
				Target:   strings.TrimSpace(host.Ablecube),
			})
		}
	case "scvm":
		if !isHCITarget(cfg.Type) {
			return nil, fmt.Errorf("unsupported cluster type")
		}
		for _, host := range hosts {
			if strings.TrimSpace(host.ScvmMngt) == "" {
				continue
			}
			targets = append(targets, healthTarget{
				Role:     "scvm",
				Hostname: host.Hostname,
				Target:   strings.TrimSpace(host.ScvmMngt),
			})
		}
	case "ccvm":
		if strings.TrimSpace(cfg.CCVM.IP) == "" {
			return nil, fmt.Errorf("ccvm ip required")
		}
		targets = append(targets, healthTarget{
			Role:   "ccvm",
			Target: strings.TrimSpace(cfg.CCVM.IP),
		})
	}

	targets = dedupeHealthTargets(targets)
	return targets, nil
}

// dedupeHealthTargets는 role/hostname/target 조합 기준으로 health 대상을 중복 제거한다.
func dedupeHealthTargets(targets []healthTarget) []healthTarget {
	seen := map[string]struct{}{}
	out := make([]healthTarget, 0, len(targets))
	for _, target := range targets {
		key := fmt.Sprintf("%s|%s|%s", target.Role, target.Hostname, target.Target)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, target)
	}
	return out
}

// checkHealthTargets는 대상별 cluster health API 호출 결과를 수집한다.
func checkHealthTargets(targets []healthTarget) []ClusterHealthTargetResult {
	client := &http.Client{Timeout: 5 * time.Second}
	results := make([]ClusterHealthTargetResult, 0, len(targets))
	for _, target := range targets {
		res := ClusterHealthTargetResult{
			Role:     target.Role,
			Hostname: target.Hostname,
			Target:   target.Target,
		}
		if err := callHealthTarget(client, target.Target); err != nil {
			res.Code = 500
			res.Message = err.Error()
		} else {
			res.Code = 200
			res.Message = "ok"
		}
		results = append(results, res)
	}
	return results
}

// callHealthTarget은 원격 노드의 cluster health API를 호출한다.
func callHealthTarget(client *http.Client, target string) error {
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("empty target")
	}
	baseURL := buildTargetURL(target)
	url := fmt.Sprintf("%s/api/v1/cube/cluster/health", baseURL)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	attachInternalToken(req)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status: %s", resp.Status)
	}
	return nil
}

// GetClusterConfig godoc
//
//	@Summary		Show Cluster Config
//	@Description	cluster.json의 clusterConfig만 반환합니다.
//	@Tags			CUBE - Cluster
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Success		200	{object}	CubeModel.ClusterConfigSection
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		404	{object}	HTTP404NotFound
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/cluster/config [get]
func GetClusterConfig(context *gin.Context) {
	cfg, err := loadClusterConfigSection()
	if err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}
	context.IndentedJSON(http.StatusOK, cfg)
}

// ApplyClusterConfig godoc
//
//	@Summary		Apply Cluster Config (Orchestrator)
//	@Description	입력된 hosts 수만큼 각 노드 API로 fan-out 호출합니다.
//	@Tags			CUBE - Cluster
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.ClusterApplyRequest	true	"apply request"\texample({"action":"","option":"","type":"","ccvm":{"ip":""},"mngtNic":{"cidr":"","gw":"","dns":""},"pcs_cluster_list":[],"hosts":[{"index":"","hostname":"","ablecube":"","scvmMngt":"","ablecubePn":"","scvm":"","scvmCn":""}],"exclude_hostname":"","remove_hostname":"","new_hostname":"","external_timeserver":"","iscsi_storage":""})
//	@Success		200	{object}	CubeModel.ClusterApplyResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		404	{object}	HTTP404NotFound
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/cluster/apply [post]
func ApplyClusterConfig(context *gin.Context) {
	var req ClusterConfigApplyRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := req.Normalize(); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	if err := requireInsertFields(req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	if err := ensureInsertAddContext(&req); err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}
	if err := ensureInsertAllHosts(&req); err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}
	if err := requirePCSClusterList(req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	if err := ensureHostsFromClusterConfig(req.Action, &req); err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}
	if err := ensureTypeFromClusterConfig(req.Action, &req); err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}

	targets, err := buildClusterTargets(req)
	if err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	if len(targets) == 0 && (isResetAction(req.Action) || isRemoveAction(req.Action)) {
		targets, err = buildClusterTargetsFromClusterConfig(req.Action)
		if err != nil {
			context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
				ErrCode: http.StatusInternalServerError,
				Message: "failed to read cluster.json",
			})
			return
		}
	}
	if len(targets) == 0 && isCheckAction(req.Action) {
		targets, err = buildClusterTargetsFromClusterConfig(req.Action)
		if err != nil {
			context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
				ErrCode: http.StatusInternalServerError,
				Message: "failed to read cluster.json",
			})
			return
		}
	}
	if isInsertAction(req.Action) && normalizeOption(req.Option) == "local" {
		targets = []string{localTarget()}
	}
	if len(targets) == 0 {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "no targets to apply",
		})
		return
	}

	req.CopyOption = "hostOnly"
	results := applyWithoutProbe(targets, req)

	hasFail := false
	for _, res := range results {
		if res.Code != 200 {
			hasFail = true
			break
		}
	}

	resp := ClusterConfigApplyResponse{
		Code:    200,
		Message: "apply success",
		Results: results,
	}
	if hasFail {
		resp.Code = 500
		resp.Message = "apply failed"
		context.JSON(http.StatusInternalServerError, resp)
		return
	}

	context.JSON(http.StatusOK, resp)
}

// ApplyClusterConfigLocal godoc
//
//	@Summary		Apply Cluster Config (Local)
//	@Description	로컬 노드에서만 cluster_config CLI를 실행합니다.
//	@Tags			CUBE - Cluster
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.ClusterApplyRequest	true	"apply request"\texample({"action":"","option":"","type":"","ccvm":{"ip":""},"mngtNic":{"cidr":"","gw":"","dns":""},"pcs_cluster_list":[],"hosts":[{"index":"","hostname":"","ablecube":"","scvmMngt":"","ablecubePn":"","scvm":"","scvmCn":""}],"exclude_hostname":"","remove_hostname":"","new_hostname":"","external_timeserver":"","iscsi_storage":""})
//	@Success		200	{object}	CubeModel.ClusterApplyLocalResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		404	{object}	HTTP404NotFound
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/cluster/apply-local [post]
func ApplyClusterConfigLocal(context *gin.Context) {
	var req ClusterConfigApplyRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := req.Normalize(); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	if err := requireInsertFields(req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	if err := ensureInsertAddContext(&req); err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}
	if err := requirePCSClusterList(req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	if err := ensureHostsFromClusterConfig(req.Action, &req); err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}
	if err := ensureTypeFromClusterConfig(req.Action, &req); err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}

	action := normalizeLocalAction(req.Action)
	if action == "" {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "unsupported action",
		})
		return
	}

	req.CopyOption = "hostOnly"
	result, err := clusterconfig.ApplyLocal(action, req)
	if err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: err.Error(),
		})
		return
	}
	if isInsertAction(req.Action) {
		scheduleSSHKnownHostsScan()
	}
	context.JSON(http.StatusOK, result)
}

// UpdateClusterConfig는 전역 cluster config 모델을 현재 cluster.json 기준으로 갱신한다.
func UpdateClusterConfig() {
	_ = updateClusterConfig(CubeModel.ClusterConfig())
}

// updateClusterConfig는 cluster.json 파일을 읽어 메모리 모델에 반영한다.
func updateClusterConfig(cfg *TypeClusterConfig) error {
	if cfg == nil {
		return nil
	}

	path := resolveClusterJSONPath()
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	data := map[string]any{}
	if err := json.Unmarshal(content, &data); err != nil {
		return err
	}
	data = clusterconfig.NormalizeClusterJSON(data)
	cfg.ApplyFrom(data)
	return nil
}

// buildClusterTargets는 요청 액션에 따라 apply 대상 host/scvm/ccvm 주소 목록을 만든다.
func buildClusterTargets(req ClusterConfigApplyRequest) ([]string, error) {
	hosts := req.Hosts

	targetMap := map[string]struct{}{}
	existingHostnames := map[string]struct{}{}
	if isInsertAddAction(req.Action, req.Option) {
		for _, name := range req.ExistingHostnames {
			if strings.TrimSpace(name) == "" {
				continue
			}
			existingHostnames[name] = struct{}{}
		}
	}
	newHostnames := map[string]struct{}{}
	if isInsertAddAction(req.Action, req.Option) && strings.TrimSpace(req.NewHostname) != "" {
		for _, name := range splitHostnameList(req.NewHostname) {
			if strings.TrimSpace(name) == "" {
				continue
			}
			newHostnames[name] = struct{}{}
		}
	}
	includeScvm := shouldIncludeScvm(req)
	includeCcvm := shouldIncludeCcvm(req)
	for _, host := range hosts {
		if host.Ablecube != "" {
			targetMap[host.Ablecube] = struct{}{}
		}
		if includeScvm && isHCITarget(req.Type) && host.ScvmMngt != "" {
			if req.ExcludeHostname != "" && req.ExcludeHostname == host.Hostname {
				continue
			}
			if len(newHostnames) > 0 {
				if _, ok := newHostnames[host.Hostname]; ok {
					continue
				}
			}
			if len(existingHostnames) > 0 {
				if _, ok := existingHostnames[host.Hostname]; !ok {
					continue
				}
			}
			targetMap[host.ScvmMngt] = struct{}{}
		}
	}

	if includeCcvm && req.CCVMMngtIP != "" {
		targetMap[req.CCVMMngtIP] = struct{}{}
	}

	targets := make([]string, 0, len(targetMap))
	for target := range targetMap {
		targets = append(targets, target)
	}
	return targets, nil
}

// splitHostnameList는 공백/콤마 기반 hostname 문자열을 배열로 분리한다.
func splitHostnameList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ',', ' ', '\t', '\n', '\r':
			return true
		default:
			return false
		}
	})
}

// buildClusterTargetsFromClusterConfig는 현재 cluster.json 기준으로 apply 대상 목록을 계산한다.
func buildClusterTargetsFromClusterConfig(action string) ([]string, error) {
	cfg, err := loadClusterConfigSection()
	if err != nil {
		return nil, err
	}

	req := ClusterConfigApplyRequest{
		Action:  action,
		Type:    cfg.Type,
		Hosts:   cfg.Hosts,
		CCVM:    &cfg.CCVM,
		MngtNic: &cfg.MngtNic,
	}
	if err := req.Normalize(); err != nil {
		return nil, err
	}
	return buildClusterTargets(req)
}

// ensureTypeFromClusterConfig는 remove 계열 요청에서 비어 있는 type 값을 cluster.json으로 보완한다.
func ensureTypeFromClusterConfig(action string, req *ClusterConfigApplyRequest) error {
	if req == nil {
		return nil
	}
	if strings.TrimSpace(req.Type) != "" {
		return nil
	}
	if !isRemoveAction(action) {
		return nil
	}
	cfg, err := loadClusterConfigSection()
	if err != nil {
		return err
	}
	req.Type = cfg.Type
	return nil
}

// ensureHostsFromClusterConfig는 check 요청에서 비어 있는 host/ccvm/type 정보를 cluster.json으로 채운다.
func ensureHostsFromClusterConfig(action string, req *ClusterConfigApplyRequest) error {
	if req == nil {
		return nil
	}
	if !isCheckAction(action) {
		return nil
	}
	cfg, err := loadClusterConfigSection()
	if err != nil {
		return err
	}
	if strings.TrimSpace(req.Type) == "" {
		req.Type = cfg.Type
	}
	if req.CCVM == nil {
		req.CCVM = &cfg.CCVM
	}
	if req.MngtNic == nil {
		req.MngtNic = &cfg.MngtNic
	}
	if len(req.Hosts) == 0 {
		req.Hosts = cfg.Hosts
	}
	return req.Normalize()
}

// ensureInsertAllHosts는 insert all 요청에서 host 목록이 비어 있으면 전체 host를 채운다.
func ensureInsertAllHosts(req *ClusterConfigApplyRequest) error {
	if req == nil {
		return nil
	}
	if !isInsertAction(req.Action) || normalizeOption(req.Option) != "all" {
		return nil
	}
	if len(req.Hosts) != 0 {
		return nil
	}
	cfg, err := loadClusterConfigSection()
	if err != nil {
		return err
	}
	req.Hosts = cfg.Hosts
	if req.CCVM == nil {
		req.CCVM = &cfg.CCVM
	}
	if req.MngtNic == nil {
		req.MngtNic = &cfg.MngtNic
	}
	return req.Normalize()
}

// ensureInsertAddContext는 insert add 요청에서 기존 host 문맥을 병합해 전체 상태로 만든다.
func ensureInsertAddContext(req *ClusterConfigApplyRequest) error {
	if req == nil {
		return nil
	}
	if !isInsertAddAction(req.Action, req.Option) {
		return nil
	}
	cfg, err := loadClusterConfigSection()
	if err != nil {
		return err
	}

	if strings.TrimSpace(req.Type) == "" {
		req.Type = cfg.Type
	}
	if req.CCVM == nil {
		req.CCVM = &cfg.CCVM
	}
	if req.MngtNic == nil {
		req.MngtNic = &cfg.MngtNic
	}

	if len(cfg.Hosts) > 0 {
		merged := make([]ClusterHost, len(cfg.Hosts))
		copy(merged, cfg.Hosts)
		indexByName := map[string]int{}
		for i, host := range merged {
			if strings.TrimSpace(host.Hostname) == "" {
				continue
			}
			indexByName[host.Hostname] = i
		}
		for _, host := range req.Hosts {
			if strings.TrimSpace(host.Hostname) == "" {
				merged = append(merged, host)
				continue
			}
			if idx, ok := indexByName[host.Hostname]; ok {
				merged[idx] = host
				continue
			}
			indexByName[host.Hostname] = len(merged)
			merged = append(merged, host)
		}
		req.Hosts = merged
	}

	req.ExistingHostnames = req.ExistingHostnames[:0]
	for _, host := range cfg.Hosts {
		if strings.TrimSpace(host.Hostname) == "" {
			continue
		}
		req.ExistingHostnames = append(req.ExistingHostnames, host.Hostname)
	}

	return req.Normalize()
}

// isResetAction은 action이 reset인지 확인한다.
func isResetAction(action string) bool {
	return strings.EqualFold(strings.TrimSpace(action), "reset")
}

// isRemoveAction은 action이 remove인지 확인한다.
func isRemoveAction(action string) bool {
	return strings.EqualFold(strings.TrimSpace(action), "remove")
}

// isInsertAction은 action이 insert 계열인지 확인한다.
func isInsertAction(action string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(action)), "insert")
}

// isCheckAction은 action이 check인지 확인한다.
func isCheckAction(action string) bool {
	return strings.EqualFold(strings.TrimSpace(action), "check")
}

// isInsertAddAction은 insert add 조합인지 판별한다.
func isInsertAddAction(action string, option string) bool {
	if !strings.EqualFold(strings.TrimSpace(action), "insert") {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(option), "add")
}

// normalizeOption은 option 문자열의 구분 문자를 제거해 비교용 형태로 만든다.
func normalizeOption(option string) string {
	normalized := strings.ToLower(strings.TrimSpace(option))
	replacer := strings.NewReplacer("-", "", "_", "", " ", "")
	return replacer.Replace(normalized)
}

// localTarget은 자기 자신을 의미하는 loopback 타깃 문자열을 반환한다.
func localTarget() string {
	return "127.0.0.1"
}

// applyWithoutProbe는 사전 health probe 없이 apply-local 요청을 바로 전파한다.
func applyWithoutProbe(targets []string, req ClusterConfigApplyRequest) []ClusterConfigApplyResult {
	client := &http.Client{Timeout: 10 * time.Second}
	results := make([]ClusterConfigApplyResult, 0, len(targets))
	for _, target := range targets {
		res := ClusterConfigApplyResult{Target: target}
		if err := callApplyLocal(client, target, req); err != nil {
			res.Code = 500
			res.Message = err.Error()
		} else {
			res.Code = 200
			res.Message = "ok"
		}
		results = append(results, res)
	}
	return results
}

// callApplyLocal은 대상 노드의 cluster apply-local API를 호출한다.
func callApplyLocal(client *http.Client, target string, req ClusterConfigApplyRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	baseURL := buildTargetURL(target)
	url := fmt.Sprintf("%s/api/v1/cube/cluster/apply-local", baseURL)
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("apply-local failed: %s", resp.Status)
	}
	return nil
}

// normalizeLocalAction은 내부 apply-local 처리용 action 문자열을 표준화한다.
func normalizeLocalAction(action string) string {
	switch strings.ToLower(action) {
	case "insert":
		return "insert"
	case "remove":
		return "remove"
	case "reset":
		return "reset"
	case "check":
		return "check"
	case "insertallhost", "insertscvmhost":
		return "insert"
	default:
		return ""
	}
}

// shouldIncludeScvm은 현재 요청에서 SCVM 대상이 포함되어야 하는지 계산한다.
func shouldIncludeScvm(req ClusterConfigApplyRequest) bool {
	if isResetAction(req.Action) {
		return false
	}
	if isCheckAction(req.Action) {
		switch normalizeOption(req.Option) {
		case "hostonly":
			return false
		case "withscvm", "all":
			return true
		default:
			return true
		}
	}
	if isInsertAddAction(req.Action, req.Option) {
		return isHCITarget(req.Type)
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if strings.HasPrefix(action, "insert") {
		return false
	}
	return true
}

// shouldIncludeCcvm은 현재 요청에서 CCVM 대상이 포함되어야 하는지 계산한다.
func shouldIncludeCcvm(req ClusterConfigApplyRequest) bool {
	if isResetAction(req.Action) {
		return false
	}
	if isCheckAction(req.Action) {
		switch normalizeOption(req.Option) {
		case "all", "":
			return true
		default:
			return false
		}
	}
	if isInsertAddAction(req.Action, req.Option) {
		return true
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if strings.HasPrefix(action, "insert") {
		return false
	}
	return true
}

// requirePCSClusterList는 insert 계열 요청에 pcs cluster list가 필요한지 검증한다.
func requirePCSClusterList(req ClusterConfigApplyRequest) error {
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if strings.HasPrefix(action, "insert") {
		minHosts := minPCSClusterHostsForType(req.Type)
		if err := CubeModel.ValidatePCSClusterList(req.PCSClusterList, minHosts); err != nil {
			return err
		}
	}
	return nil
}

func minPCSClusterHostsForType(clusterType string) int {
	if strings.EqualFold(strings.TrimSpace(clusterType), "ablestack-standalone") {
		return 0
	}
	if isHCITarget(clusterType) {
		return 3
	}
	return 1
}

// requireInsertFields는 insert 요청에 필요한 필수 필드가 모두 있는지 검증한다.
func requireInsertFields(req ClusterConfigApplyRequest) error {
	if !strings.EqualFold(strings.TrimSpace(req.Action), "insert") {
		return nil
	}
	if strings.TrimSpace(req.Type) == "" {
		return fmt.Errorf("type required")
	}
	if req.CCVM == nil || strings.TrimSpace(req.CCVM.IP) == "" {
		return fmt.Errorf("ccvm required")
	}
	if strings.TrimSpace(req.IscsiStorage) == "" {
		return fmt.Errorf("iscsi_storage required")
	}
	return nil
}
