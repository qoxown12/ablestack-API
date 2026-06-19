package cube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

const (
	pcsUpdateLocalHeader    = "X-Cube-PCS-Local"
	pcsCommandTimeout       = 2 * time.Minute
	pcsRemoteRequestTimeout = 5 * time.Minute
	pcsDefaultResourceID    = ccvmSnapPCSResourceID
	pcsSuccessMessage       = "ok"
)

type pcsExecutionTarget struct {
	Hostname string
	PCSHost  string
	Target   string
}

type ccvmSnapPCSNode struct {
	Name             string `xml:"name,attr"`
	Online           string `xml:"online,attr"`
	ResourcesRunning string `xml:"resources_running,attr"`
	Standby          string `xml:"standby,attr"`
	StandbyOnfail    string `xml:"standby_onfail,attr"`
	Maintenance      string `xml:"maintenance,attr"`
	Pending          string `xml:"pending,attr"`
	Unclean          string `xml:"unclean,attr"`
	Shutdown         string `xml:"shutdown,attr"`
	ExpectedUp       string `xml:"expected_up,attr"`
	IsDC             string `xml:"is_dc,attr"`
	Type             string `xml:"type,attr"`
}

func isPCSUpdateLocalRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(pcsUpdateLocalHeader)), "1")
}

func statusCodeFromCCVMPCSResponse(resp CCVMPCSControlResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func selectPCSExecutionTarget(cfg *CubeModel.ClusterConfigSection) (pcsExecutionTarget, bool) {
	targets := buildPCSExecutionTargets(cfg)
	if len(targets) == 0 {
		return pcsExecutionTarget{Target: "local"}, true
	}

	localName, _ := os.Hostname()
	for _, target := range targets {
		if isPCSLocalExecutionTarget(target, localName) {
			return target, true
		}
	}

	client := &http.Client{Timeout: 2 * time.Second}
	for _, target := range targets {
		if strings.TrimSpace(target.Target) == "" {
			continue
		}
		if err := callHealthTarget(client, target.Target); err == nil {
			return target, true
		}
	}

	return pcsExecutionTarget{Target: "local"}, true
}

func buildPCSExecutionTargets(cfg *CubeModel.ClusterConfigSection) []pcsExecutionTarget {
	if cfg == nil {
		return nil
	}
	pcsHosts := cfg.PCSCluster.HostnameList()

	out := make([]pcsExecutionTarget, 0, len(pcsHosts))
	seen := map[string]struct{}{}
	for _, pcsHost := range pcsHosts {
		if pcsHost == "" {
			continue
		}
		host, ok := findPCSClusterHost(cfg, pcsHost)
		target := pcsHost
		hostname := pcsHost
		if ok {
			hostname = strings.TrimSpace(host.Hostname)
			if hostTarget := ccvmHostAPITarget(cfg, host); hostTarget != "" {
				target = hostTarget
			}
		}
		key := target + "|" + pcsHost
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, pcsExecutionTarget{
			Hostname: hostname,
			PCSHost:  pcsHost,
			Target:   target,
		})
	}
	return out
}

func ccvmHostAPITarget(cfg *CubeModel.ClusterConfigSection, host CubeModel.ClusterHost) string {
	if cfg != nil &&
		strings.EqualFold(strings.TrimSpace(cfg.Type), "ablestack-vm") &&
		strings.EqualFold(strings.TrimSpace(cfg.StorageNetwork), "true") {
		if target := strings.TrimSpace(host.AblecubePn); target != "" {
			return target
		}
	}
	return strings.TrimSpace(host.Ablecube)
}

func findPCSClusterHost(cfg *CubeModel.ClusterConfigSection, pcsHost string) (CubeModel.ClusterHost, bool) {
	pcsHost = strings.ToLower(strings.TrimSpace(pcsHost))
	for _, host := range cfg.Hosts {
		if strings.ToLower(strings.TrimSpace(host.AblecubePn)) == pcsHost ||
			strings.ToLower(strings.TrimSpace(host.Ablecube)) == pcsHost ||
			strings.ToLower(strings.TrimSpace(host.Hostname)) == pcsHost {
			return host, true
		}
	}
	return CubeModel.ClusterHost{}, false
}

func isPCSLocalExecutionTarget(target pcsExecutionTarget, localName string) bool {
	shortName := strings.ToLower(strings.SplitN(strings.TrimSpace(localName), ".", 2)[0])
	fullName := strings.ToLower(strings.TrimSpace(localName))
	for _, value := range []string{target.Hostname, target.PCSHost, target.Target} {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if value == fullName || value == shortName || isLocalTarget(value) {
			return true
		}
	}
	return false
}

func runPCSCommand(timeout time.Duration, command string, args ...string) (string, error) {
	out, timedOut, err := runCommandOutputWithEnv(command, timeout, pcsCommandEnv(), args...)
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

func pcsCommandEnv() []string {
	return append([]string{"LANG=en_US.utf-8", "LANGUAGE=en"}, os.Environ()...)
}

func callCCVMPCSRemote(target string, req CCVMPCSControlRequest) (CCVMPCSControlResponse, error) {
	return callCCVMPCSRemoteWithTimeout(target, req, pcsRemoteRequestTimeout)
}

func callCCVMPCSRemoteWithTimeout(target string, req CCVMPCSControlRequest, timeout time.Duration) (CCVMPCSControlResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return CCVMPCSControlResponse{}, err
	}

	baseURL := buildTargetURL(target)
	url := fmt.Sprintf("%s/api/v1/cube/pcs/control", baseURL)
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return CCVMPCSControlResponse{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(pcsUpdateLocalHeader, "1")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return CCVMPCSControlResponse{}, err
	}
	defer resp.Body.Close()

	var out CCVMPCSControlResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CCVMPCSControlResponse{}, err
	}
	if out.Code == 0 {
		out.Code = resp.StatusCode
	}
	if strings.TrimSpace(out.Target) == "" {
		out.Target = target
	}
	if strings.TrimSpace(out.Action) == "" {
		out.Action = req.Action
	}
	return out, nil
}

func ccvmPCSOK(req CCVMPCSControlRequest, target string, val any) CCVMPCSControlResponse {
	return CCVMPCSControlResponse{
		Code:    http.StatusOK,
		Val:     val,
		Message: pcsSuccessMessage,
		Action:  req.Action,
		Target:  target,
	}
}

func ccvmPCSError(req CCVMPCSControlRequest, target string, code int, val any, message string) CCVMPCSControlResponse {
	if message == "" {
		message = fmt.Sprint(val)
	}
	return CCVMPCSControlResponse{
		Code:    code,
		Val:     val,
		Message: message,
		Action:  req.Action,
		Target:  target,
	}
}
