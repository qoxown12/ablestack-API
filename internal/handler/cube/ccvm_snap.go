package cube

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type CCVMSnapRequest = CubeModel.CCVMSnapRequest
type CCVMSnapResponse = CubeModel.CCVMSnapResponse

const (
	ccvmSnapLocalHeader         = "X-Cube-CCVM-Snap-Local"
	ccvmSnapResolveHeader       = "X-Cube-CCVM-Snap-Resolve"
	ccvmSnapStartedHeader       = "X-Cube-CCVM-Snap-Started"
	ccvmSnapRequestTimeout      = 30 * time.Minute
	ccvmSnapCommandTimeout      = 30 * time.Minute
	ccvmSnapShortCommandTimeout = 10 * time.Second
	ccvmSnapName                = "ccvm"
	ccvmSnapImageName           = "ccvm"
	ccvmSnapPoolName            = "rbd"
	ccvmSnapLimit               = 10
	ccvmSnapPCSResourceID       = "cloudcenter_res"
)

var ccvmSnapMu sync.Mutex

type ccvmSnapPCSTarget struct {
	Hostname string
	PCSIP    string
	Target   string
}

type ccvmSnapPCSResourceStatus struct {
	Role        string
	StartedNode string
}

type ccvmSnapPCSStatusXML struct {
	Summary struct {
		CurrentDC struct {
			Name string `xml:"name,attr"`
		} `xml:"current_dc"`
	} `xml:"summary"`
	Nodes struct {
		Node []ccvmSnapPCSNode `xml:"node"`
	} `xml:"nodes"`
	Resources struct {
		Resource []ccvmSnapPCSResource `xml:"resource"`
		Clone    []ccvmSnapPCSClone    `xml:"clone"`
	} `xml:"resources"`
}

type ccvmSnapPCSClone struct {
	Resource []ccvmSnapPCSResource      `xml:"resource"`
	Group    []ccvmSnapPCSResourceGroup `xml:"group"`
}

type ccvmSnapPCSResourceGroup struct {
	Resource []ccvmSnapPCSResource `xml:"resource"`
}

type ccvmSnapPCSResource struct {
	ID             string `xml:"id,attr"`
	Role           string `xml:"role,attr"`
	Active         string `xml:"active,attr"`
	Blocked        string `xml:"blocked,attr"`
	Failed         string `xml:"failed,attr"`
	NodesRunningOn string `xml:"nodes_running_on,attr"`
	Node           struct {
		Name string `xml:"name,attr"`
	} `xml:"node"`
}

// CCVMSnap godoc
//
//	@Summary		CCVM Snapshot
//	@Description	HCI/HCI-filesystem 환경에서 CCVM RBD snapshot list/backup/rollback 작업을 수행합니다.
//	@Tags			CUBE - CCVM
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.CCVMSnapRequest	true	"ccvm snapshot request"
//	@Success		200	{object}	CubeModel.CCVMSnapResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/ccvm/snap [post]
func CCVMSnap(context *gin.Context) {
	var req CCVMSnapRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := normalizeCCVMSnapRequest(&req); err != nil {
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
	if !isHCITarget(cfg.Type) {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "unsupported cluster type",
		})
		return
	}

	var resp CCVMSnapResponse
	if isCCVMSnapLocalRequest(context) {
		resp = runCCVMSnapLocal(cfg, req, isCCVMSnapStartedRequest(context))
	} else if isCCVMSnapResolveRequest(context) {
		resp = runCCVMSnapResolvedFromPCS(cfg, req)
	} else {
		resp = runCCVMSnapViaPCS(cfg, req)
	}

	context.JSON(statusCodeFromCCVMSnapResponse(resp), resp)
}

func normalizeCCVMSnapRequest(req *CCVMSnapRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "list":
		req.Action = "list"
	case "backup":
		req.Action = "backup"
	case "rollback":
		req.Action = "rollback"
	default:
		return fmt.Errorf("unsupported action")
	}

	req.SnapName = strings.TrimSpace(req.SnapName)
	if req.Action == "rollback" && req.SnapName == "" {
		return fmt.Errorf("snap_name required")
	}
	if req.SnapName != "" && strings.ContainsAny(req.SnapName, "/@") {
		return fmt.Errorf("invalid snap_name")
	}
	return nil
}

func isCCVMSnapLocalRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(ccvmSnapLocalHeader)), "1")
}

func isCCVMSnapResolveRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(ccvmSnapResolveHeader)), "1")
}

func isCCVMSnapStartedRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(ccvmSnapStartedHeader)), "1")
}

func statusCodeFromCCVMSnapResponse(resp CCVMSnapResponse) int {
	if resp.Code == 200 {
		return http.StatusOK
	}
	if resp.Code == 400 {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// runCCVMSnapViaPCS는 cluster.json의 pcsCluster hostnameN을 ablecubePn과 매핑해
// health가 성공하는 PCS 구성 노드로 snapshot 요청을 전달한다.
func runCCVMSnapViaPCS(cfg *CubeModel.ClusterConfigSection, req CCVMSnapRequest) CCVMSnapResponse {
	targets := buildCCVMSnapPCSTargets(cfg)
	if len(targets) == 0 {
		return ccvmSnapError(req, "pcs cluster host not found")
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
			return runCCVMSnapResolvedFromPCS(cfg, req)
		}

		resp, err := callCCVMSnapRemote(target.Target, req, ccvmSnapResolveHeader, false)
		if err != nil {
			lastErr = err
			continue
		}
		return resp
	}

	if lastErr != nil {
		return ccvmSnapError(req, lastErr.Error())
	}
	return ccvmSnapError(req, "healthy pcs cluster host not found")
}

// runCCVMSnapResolvedFromPCS는 PCS 구성 노드에서 cloudcenter_res 위치를 확인한 뒤
// CCVM이 실행 중인 ablecube로 backup 작업을 전달한다.
func runCCVMSnapResolvedFromPCS(cfg *CubeModel.ClusterConfigSection, req CCVMSnapRequest) CCVMSnapResponse {
	if req.Action == "list" {
		return runCCVMSnapLocal(cfg, req, false)
	}

	status, err := loadCCVMPcsResourceStatus()
	if err != nil {
		return ccvmSnapError(req, err.Error())
	}

	if req.Action == "rollback" {
		if strings.EqualFold(status.Role, "Started") {
			return ccvmSnapError(req, "CCVM Snapshot Rollback Fail. Check if ccvm is Stopped")
		}
		return runCCVMSnapLocal(cfg, req, false)
	}

	if !strings.EqualFold(status.Role, "Started") || strings.TrimSpace(status.StartedNode) == "" {
		return runCCVMSnapLocal(cfg, req, false)
	}

	target, ok := resolveCCVMSnapStartedTarget(cfg, status.StartedNode)
	if !ok || strings.TrimSpace(target.Target) == "" {
		return ccvmSnapError(req, fmt.Sprintf("cloudcenter_res started node not found in cluster.json: %s", status.StartedNode))
	}

	client := &http.Client{Timeout: 5 * time.Second}
	if !isLocalTarget(target.Target) {
		if err := callHealthTarget(client, target.Target); err != nil {
			return ccvmSnapError(req, fmt.Sprintf("%s health check failed: %v", target.Target, err))
		}
		returnValue, err := callCCVMSnapRemote(target.Target, req, ccvmSnapLocalHeader, true)
		if err != nil {
			return ccvmSnapError(req, err.Error())
		}
		return returnValue
	}

	return runCCVMSnapLocal(cfg, req, true)
}

func runCCVMSnapLocal(cfg *CubeModel.ClusterConfigSection, req CCVMSnapRequest, expectStarted bool) CCVMSnapResponse {
	ccvmSnapMu.Lock()
	defer ccvmSnapMu.Unlock()

	target := resolveLocalCCVMEditTarget(cfg)
	switch req.Action {
	case "list":
		snapshots, err := listCCVMSnapshots()
		if err != nil {
			return ccvmSnapLocalError(req, target, err.Error())
		}
		return CCVMSnapResponse{
			Code:    200,
			Val:     snapshots,
			Message: "CCVM Snapshot List Success",
			Target:  target,
			Action:  req.Action,
		}
	case "rollback":
		if err := rollbackCCVMSnapshot(req.SnapName); err != nil {
			return ccvmSnapLocalError(req, target, "CCVM Snapshot Rollback Fail. Check if ccvm is Stopped")
		}
		return CCVMSnapResponse{
			Code:     200,
			Val:      "CCVM Snapshot Rollback Success",
			Message:  "CCVM Snapshot Rollback Success",
			Target:   target,
			Action:   req.Action,
			SnapName: req.SnapName,
		}
	case "backup":
		snapName := req.SnapName
		if snapName == "" {
			snapName = time.Now().Format("2006-01-02-15:04:05")
		}
		deleted, err := createCCVMSnapshot(snapName, expectStarted)
		if err != nil {
			return ccvmSnapLocalError(req, target, err.Error())
		}
		return CCVMSnapResponse{
			Code:             200,
			Val:              "CCVM Snapshot Backup Create Success",
			Message:          "CCVM Snapshot Backup Create Success",
			Target:           target,
			Action:           req.Action,
			SnapName:         snapName,
			DeletedSnapshots: deleted,
		}
	default:
		return ccvmSnapLocalError(req, target, "unsupported action")
	}
}

func listCCVMSnapshots() ([]map[string]any, error) {
	out, err := runCCVMSnapCommand(ccvmSnapShortCommandTimeout, "rbd", "snap", "list", ccvmSnapImageRef(), "--format", "json")
	if err != nil {
		return nil, err
	}
	snapshots := []map[string]any{}
	if err := json.Unmarshal([]byte(out), &snapshots); err != nil {
		return nil, fmt.Errorf("failed to parse rbd snapshot list: %w", err)
	}
	return snapshots, nil
}

func rollbackCCVMSnapshot(snapName string) error {
	_, err := runCCVMSnapCommand(ccvmSnapCommandTimeout, "rbd", "snap", "rollback", "--no-progress", ccvmSnapRef(snapName))
	return err
}

func createCCVMSnapshot(snapName string, expectStarted bool) ([]string, error) {
	if isAutoCCVMSnapshotName(snapName) {
		exists, err := ccvmSnapshotExists(snapName)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, nil
		}
	}

	state, exists, err := readLocalCCVMState()
	if expectStarted {
		if err != nil || !exists || !strings.EqualFold(state, "running") {
			if err != nil {
				return nil, fmt.Errorf("CCVM Snapshot Backup Create Fail. failed to confirm running ccvm: %w", err)
			}
			return nil, fmt.Errorf("CCVM Snapshot Backup Create Fail. failed to confirm running ccvm")
		}
	}

	suspended := false
	if strings.EqualFold(state, "running") {
		if err := suspendLocalCCVM(); err != nil {
			return nil, err
		}
		suspended = true
	}

	_, createErr := runCCVMSnapCommand(ccvmSnapCommandTimeout, "rbd", "snap", "create", ccvmSnapRef(snapName))
	var resumeErr error
	if suspended {
		resumeErr = resumeLocalCCVM()
	}
	if createErr != nil {
		return nil, fmt.Errorf("CCVM Snapshot Backup Create Fail. %w", createErr)
	}
	if resumeErr != nil {
		return nil, fmt.Errorf("CCVM Snapshot Backup Create Fail. %w", resumeErr)
	}

	deleted, err := cleanupOldCCVMSnapshots()
	if err != nil {
		return deleted, err
	}
	return deleted, nil
}

func isAutoCCVMSnapshotName(snapName string) bool {
	return strings.HasPrefix(strings.TrimSpace(snapName), "auto-")
}

func ccvmSnapshotExists(snapName string) (bool, error) {
	snapshots, err := listCCVMSnapshots()
	if err != nil {
		return false, err
	}
	for _, snapshot := range snapshots {
		name, _ := snapshot["name"].(string)
		if strings.TrimSpace(name) == snapName {
			return true, nil
		}
	}
	return false, nil
}

func cleanupOldCCVMSnapshots() ([]string, error) {
	snapshots, err := listCCVMSnapshots()
	if err != nil {
		return nil, err
	}
	excess := len(snapshots) - ccvmSnapLimit
	if excess <= 0 {
		return nil, nil
	}

	deleted := make([]string, 0, excess)
	for i := 0; i < excess; i++ {
		name, _ := snapshots[i]["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, err := runCCVMSnapCommand(ccvmSnapCommandTimeout, "rbd", "snap", "rm", ccvmSnapRef(name)); err != nil {
			return deleted, err
		}
		deleted = append(deleted, name)
	}
	return deleted, nil
}

func readLocalCCVMState() (string, bool, error) {
	out, _, err := runCommandOutputWithEnv("virsh", ccvmSnapShortCommandTimeout, virshEnv(), "domstate", ccvmSnapName)
	state := strings.TrimSpace(out)
	if err != nil {
		if strings.Contains(strings.ToLower(state), "failed to get domain") ||
			strings.Contains(strings.ToLower(state), "domain not found") {
			return "", false, nil
		}
		return state, false, err
	}
	return state, true, nil
}

func suspendLocalCCVM() error {
	_, err := runCCVMSnapCommand(ccvmSnapShortCommandTimeout, "virsh", "suspend", ccvmSnapName)
	return err
}

func resumeLocalCCVM() error {
	_, err := runCCVMSnapCommand(ccvmSnapShortCommandTimeout, "virsh", "resume", ccvmSnapName)
	return err
}

func runCCVMSnapCommand(timeout time.Duration, command string, args ...string) (string, error) {
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

func ccvmSnapCommandEnv() []string {
	return append([]string{"LANG=en_US.utf-8", "LANGUAGE=en"}, os.Environ()...)
}

func ccvmSnapImageRef() string {
	return ccvmSnapPoolName + "/" + ccvmSnapImageName
}

func ccvmSnapRef(snapName string) string {
	return ccvmSnapImageRef() + "@" + snapName
}

func loadCCVMPcsResourceStatus() (ccvmSnapPCSResourceStatus, error) {
	pcsStatus, err := loadCCVMPcsStatusXML()
	if err != nil {
		return ccvmSnapPCSResourceStatus{}, err
	}

	for _, resource := range collectCCVMSnapPCSResources(pcsStatus) {
		if strings.TrimSpace(resource.ID) != ccvmSnapPCSResourceID {
			continue
		}
		return ccvmSnapPCSResourceStatus{
			Role:        strings.TrimSpace(resource.Role),
			StartedNode: strings.TrimSpace(resource.Node.Name),
		}, nil
	}
	return ccvmSnapPCSResourceStatus{Role: "Stopped"}, nil
}

func loadCCVMPcsCurrentDC() (string, error) {
	pcsStatus, err := loadCCVMPcsStatusXML()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(pcsStatus.Summary.CurrentDC.Name), nil
}

func loadCCVMPcsStatusXML() (ccvmSnapPCSStatusXML, error) {
	out, err := runCCVMSnapCommand(ccvmSnapShortCommandTimeout, "pcs", "status", "xml")
	if err != nil {
		return ccvmSnapPCSStatusXML{}, err
	}

	var pcsStatus ccvmSnapPCSStatusXML
	if err := xml.Unmarshal([]byte(out), &pcsStatus); err != nil {
		return ccvmSnapPCSStatusXML{}, fmt.Errorf("failed to parse pcs status xml: %w", err)
	}
	return pcsStatus, nil
}

func collectCCVMSnapPCSResources(pcsStatus ccvmSnapPCSStatusXML) []ccvmSnapPCSResource {
	resources := make([]ccvmSnapPCSResource, 0)
	resources = append(resources, pcsStatus.Resources.Resource...)
	for _, clone := range pcsStatus.Resources.Clone {
		resources = append(resources, clone.Resource...)
		for _, group := range clone.Group {
			resources = append(resources, group.Resource...)
		}
	}
	return resources
}

func buildCCVMSnapPCSTargets(cfg *CubeModel.ClusterConfigSection) []ccvmSnapPCSTarget {
	if cfg == nil {
		return nil
	}

	pcsIPs := cfg.PCSCluster.HostnameList()

	out := make([]ccvmSnapPCSTarget, 0, len(pcsIPs))
	seen := map[string]struct{}{}
	for _, pcsIP := range pcsIPs {
		if pcsIP == "" {
			continue
		}
		host, ok := findCCVMSnapHostByPCSIP(cfg, pcsIP)
		if !ok || strings.TrimSpace(host.Ablecube) == "" {
			continue
		}
		target := strings.TrimSpace(host.Ablecube)
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, ccvmSnapPCSTarget{
			Hostname: strings.TrimSpace(host.Hostname),
			PCSIP:    pcsIP,
			Target:   target,
		})
	}
	return out
}

func findCCVMSnapHostByPCSIP(cfg *CubeModel.ClusterConfigSection, pcsIP string) (CubeModel.ClusterHost, bool) {
	for _, host := range cfg.Hosts {
		if strings.TrimSpace(host.AblecubePn) == pcsIP {
			return host, true
		}
	}
	return CubeModel.ClusterHost{}, false
}

func resolveCCVMSnapStartedTarget(cfg *CubeModel.ClusterConfigSection, nodeName string) (ccvmSnapPCSTarget, bool) {
	nodeName = strings.TrimSpace(nodeName)
	if cfg == nil || nodeName == "" {
		return ccvmSnapPCSTarget{}, false
	}

	for _, host := range cfg.Hosts {
		if strings.TrimSpace(host.AblecubePn) == nodeName ||
			strings.TrimSpace(host.Ablecube) == nodeName ||
			strings.TrimSpace(host.Hostname) == nodeName {
			return ccvmSnapPCSTarget{
				Hostname: strings.TrimSpace(host.Hostname),
				PCSIP:    strings.TrimSpace(host.AblecubePn),
				Target:   strings.TrimSpace(host.Ablecube),
			}, true
		}
	}
	return ccvmSnapPCSTarget{}, false
}

func callCCVMSnapRemote(target string, req CCVMSnapRequest, modeHeader string, expectStarted bool) (CCVMSnapResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return CCVMSnapResponse{}, err
	}

	baseURL := buildTargetURL(target)
	url := fmt.Sprintf("%s/api/v1/cube/ccvm/snap", baseURL)
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return CCVMSnapResponse{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	if modeHeader != "" {
		httpReq.Header.Set(modeHeader, "1")
	}
	if expectStarted {
		httpReq.Header.Set(ccvmSnapStartedHeader, "1")
	}

	client := &http.Client{Timeout: ccvmSnapRequestTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return CCVMSnapResponse{}, err
	}
	defer resp.Body.Close()

	var out CCVMSnapResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CCVMSnapResponse{}, err
	}
	if out.Code == 0 {
		out.Code = resp.StatusCode
	}
	if strings.TrimSpace(out.Target) == "" {
		out.Target = target
	}
	return out, nil
}

func ccvmSnapError(req CCVMSnapRequest, message string) CCVMSnapResponse {
	return CCVMSnapResponse{
		Code:     500,
		Val:      message,
		Message:  message,
		Action:   req.Action,
		SnapName: req.SnapName,
	}
}

func ccvmSnapLocalError(req CCVMSnapRequest, target string, message string) CCVMSnapResponse {
	resp := ccvmSnapError(req, message)
	resp.Target = target
	return resp
}
