package cube

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type CCVMStatusResponse = CubeModel.CCVMStatusResponse
type CCVMLocalStatus = CubeModel.CCVMLocalStatus

const (
	localCmdTimeout = 3 * time.Second
	timeoutValue    = "N/A(timeout)"
	ccvmLocalHeader = "X-Cube-CCVM-Local"
)

// GetCCVMStatus godoc
//
//	@Summary		CCVM Status
//	@Description	CCVM의 상태를 조회합니다.
//	@Tags			CUBE - CCVM
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Success		200	{object}	CubeModel.CCVMStatusResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/ccvm/status [get]
func GetCCVMStatus(context *gin.Context) {
	if strings.EqualFold(strings.TrimSpace(context.GetHeader(ccvmLocalHeader)), "1") {
		localStatus, err := collectCCVMLocalStatus()
		if err != nil {
			context.JSON(http.StatusInternalServerError, CCVMStatusResponse{Code: 500, Message: err.Error()})
			return
		}
		context.JSON(http.StatusOK, CCVMStatusResponse{Code: 200, Data: localStatus})
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

	ccvmIP := strings.TrimSpace(cfg.CCVM.IP)
	if ccvmIP == "" {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "ccvm ip required",
		})
		return
	}

	if isLocalTarget(ccvmIP) {
		localStatus, err := collectCCVMLocalStatus()
		if err != nil {
			context.JSON(http.StatusInternalServerError, CCVMStatusResponse{Code: 500, Message: err.Error()})
			return
		}
		context.JSON(http.StatusOK, CCVMStatusResponse{Code: 200, Data: localStatus})
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	proxied, err := callCCVMStatus(client, ccvmIP)
	if err != nil {
		context.JSON(http.StatusInternalServerError, CCVMStatusResponse{Code: 500, Message: err.Error()})
		return
	}
	context.JSON(http.StatusOK, proxied)
}

// callCCVMStatus는 지정한 CCVM 노드에 로컬 상태 조회 헤더를 붙여 상태 API를 호출한다.
func callCCVMStatus(client *http.Client, target string) (CCVMStatusResponse, error) {
	baseURL := buildTargetURL(target)
	url := fmt.Sprintf("%s/api/v1/cube/ccvm/status", baseURL)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return CCVMStatusResponse{}, err
	}
	attachInternalToken(req)
	req.Header.Set(ccvmLocalHeader, "1")

	resp, err := client.Do(req)
	if err != nil {
		return CCVMStatusResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return CCVMStatusResponse{}, fmt.Errorf("ccvm status failed: %s", resp.Status)
	}

	var out CCVMStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CCVMStatusResponse{}, err
	}
	if out.Code != 200 {
		return CCVMStatusResponse{}, fmt.Errorf("ccvm status failed: %s", out.Message)
	}
	if out.Data == nil {
		return CCVMStatusResponse{}, fmt.Errorf("ccvm status empty from %s", target)
	}
	return out, nil
}

// collectCCVMLocalStatus는 현재 노드에서 명령어 기반으로 CCVM 로컬 상태를 수집한다.
// CPU, 메모리, NIC, UUID, 디스크, 게이트웨이, DNS, 서비스 상태를 한 번에 채운다.
func collectCCVMLocalStatus() (CCVMLocalStatus, error) {
	status := defaultCCVMLocalStatus()

	if cpu, timedOut := readLocalCPUCount(); timedOut {
		status.CPU = timeoutValue
	} else if cpu != "" {
		status.CPU = cpu
	}

	if maxMem, usedMem, timedOut := readLocalMemoryStatus(); timedOut {
		status.MaxMemory = timeoutValue
		status.UsedMemory = timeoutValue
	} else {
		if maxMem != "" {
			status.MaxMemory = maxMem
		}
		if usedMem != "" {
			status.UsedMemory = usedMem
		}
	}

	if ip, prefix, mac, timedOut := readLocalPrimaryInterfaceStatus(); timedOut {
		status.IP = timeoutValue
		status.Prefix = timeoutValue
		status.MAC = timeoutValue
		status.NICType = timeoutValue
		status.NICBridge = timeoutValue
	} else {
		if ip != "" {
			status.IP = ip
		}
		if prefix != "" {
			status.Prefix = prefix
		}
		if mac != "" {
			status.MAC = mac
		}
		status.NICType = "bridge"
		status.NICBridge = "bridge0"
	}

	if uuid, timedOut := readLocalUUID(); timedOut {
		status.UUID = timeoutValue
	} else if uuid != "" {
		status.UUID = uuid
	}

	if lines, timedOut, err := runCommandLines("df", localCmdTimeout, "-h"); timedOut {
		setCCVMDiskStatusTimeout(&status)
	} else if err == nil && len(lines) > 0 {
		applyCCVMDiskStatusFromDFLines(&status, lines)
	}

	if gw := readDefaultGateway(); gw != "" {
		status.GW = gw
	}
	if dns := readFirstNameserver(); dns != "" {
		status.DNS = dns
	}
	status.MoldServiceStatus = systemctlActive("mold.service")
	status.MoldDBStatus = systemctlActive("mysqld")

	return status, nil
}

// defaultCCVMLocalStatus는 조회 실패 전 기본 응답 골격을 만든다.
func defaultCCVMLocalStatus() CCVMLocalStatus {
	return CCVMLocalStatus{
		Name:                "ccvm",
		State:               "running",
		CPU:                 "N/A",
		MaxMemory:           "N/A",
		UsedMemory:          "N/A",
		IP:                  "N/A",
		MAC:                 "N/A",
		NICType:             "bridge",
		NICBridge:           "bridge0",
		UUID:                "N/A",
		Prefix:              "N/A",
		DiskCap:             "N/A",
		DiskAlloc:           "N/A",
		DiskPhy:             "N/A",
		DiskUsageRate:       "N/A",
		SecondDiskCap:       "N/A",
		SecondDiskAlloc:     "N/A",
		SecondDiskPhy:       "N/A",
		SecondDiskUsageRate: "N/A",
		GW:                  "N/A",
		DNS:                 "N/A",
		MoldServiceStatus:   "N/A",
		MoldDBStatus:        "N/A",
	}
}

// parseDfLine은 `df -h` 한 줄에서 용량 관련 4개 필드를 추출한다.
func parseDfLine(line string) (string, string, string, string) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return "", "", "", ""
	}
	return fields[1], fields[2], fields[3], fields[4]
}

// applyCCVMDiskStatusFromDFLines는 `df -h` 결과를 루트/NFS 디스크 필드에 매핑한다.
func applyCCVMDiskStatusFromDFLines(status *CCVMLocalStatus, lines []string) {
	if status == nil || len(lines) == 0 {
		return
	}
	status.Blk = lines
	for _, line := range lines {
		if strings.Contains(line, "rl-root") {
			if cap, alloc, phy, usage := parseDfLine(line); cap != "" {
				status.DiskCap = cap
				status.DiskAlloc = alloc
				status.DiskPhy = phy
				status.DiskUsageRate = usage
			}
		}
		if strings.Contains(line, "rl-nfs") {
			if cap, alloc, phy, usage := parseDfLine(line); cap != "" {
				status.SecondDiskCap = cap
				status.SecondDiskAlloc = alloc
				status.SecondDiskPhy = phy
				status.SecondDiskUsageRate = usage
			}
		}
	}
}

// setCCVMDiskStatusTimeout은 디스크 조회 timeout 발생 시 관련 필드를 timeout 값으로 채운다.
func setCCVMDiskStatusTimeout(status *CCVMLocalStatus) {
	if status == nil {
		return
	}
	status.Blk = []string{timeoutValue}
	status.DiskCap = timeoutValue
	status.DiskAlloc = timeoutValue
	status.DiskPhy = timeoutValue
	status.DiskUsageRate = timeoutValue
	status.SecondDiskCap = timeoutValue
	status.SecondDiskAlloc = timeoutValue
	status.SecondDiskPhy = timeoutValue
	status.SecondDiskUsageRate = timeoutValue
}

// readDefaultGateway는 route/ip 명령으로 기본 게이트웨이를 읽는다.
func readDefaultGateway() string {
	if lines, timedOut, err := runCommandLines("route", localCmdTimeout, "-n"); timedOut {
		return timeoutValue
	} else if err == nil {
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			dest := fields[0]
			flags := ""
			if len(fields) >= 4 {
				flags = fields[3]
			}
			if dest == "0.0.0.0" || strings.Contains(flags, "G") {
				return fields[1]
			}
		}
	}

	if lines, timedOut, err := runCommandLines("ip", localCmdTimeout, "route", "show", "default"); timedOut {
		return timeoutValue
	} else if err == nil {
		for _, line := range lines {
			fields := strings.Fields(line)
			for i := 0; i < len(fields)-1; i++ {
				if fields[i] == "via" {
					return fields[i+1]
				}
			}
		}
	}
	return ""
}

// readLocalCPUCount는 nproc 결과를 이용해 CPU 개수를 읽는다.
func readLocalCPUCount() (string, bool) {
	lines, timedOut, err := runCommandLines("nproc", localCmdTimeout)
	if timedOut {
		return "", true
	}
	if err != nil || len(lines) == 0 {
		return "", false
	}
	return strings.TrimSpace(lines[0]), false
}

// readLocalMemoryStatus는 free -k 결과에서 총 메모리와 사용 메모리를 읽는다.
func readLocalMemoryStatus() (string, string, bool) {
	lines, timedOut, err := runCommandLines("free", localCmdTimeout, "-k")
	if timedOut {
		return "", "", true
	}
	if err != nil {
		return "", "", false
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "Mem:" {
			continue
		}
		totalKiB, errTotal := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
		usedKiB, errUsed := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
		if errTotal != nil || errUsed != nil {
			return "", "", false
		}
		return fmt.Sprintf("%d KiB", totalKiB), fmt.Sprintf("%d KiB", usedKiB), false
	}
	return "", "", false
}

// readLocalPrimaryInterfaceStatus는 기본 라우트 인터페이스의 IP, prefix, MAC 정보를 읽는다.
func readLocalPrimaryInterfaceStatus() (string, string, string, bool) {
	iface, timedOut := readLocalDefaultInterface()
	if timedOut {
		return "", "", "", true
	}
	ip, prefix, resolvedIface, timedOut := readLocalIPv4ByInterface(iface)
	if timedOut {
		return "", "", "", true
	}
	if strings.TrimSpace(resolvedIface) == "" {
		return "", "", "", false
	}
	mac := readInterfaceMAC(resolvedIface)
	return ip, prefix, mac, false
}

// readLocalDefaultInterface는 기본 라우트가 연결된 인터페이스 이름을 찾는다.
func readLocalDefaultInterface() (string, bool) {
	lines, timedOut, err := runCommandLines("ip", localCmdTimeout, "route", "show", "default")
	if timedOut {
		return "", true
	}
	if err != nil {
		return "", false
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		for i := 0; i < len(fields)-1; i++ {
			if fields[i] == "dev" {
				return strings.TrimSpace(fields[i+1]), false
			}
		}
	}
	return "", false
}

// readLocalIPv4ByInterface는 특정 인터페이스의 IPv4 주소와 prefix를 읽는다.
func readLocalIPv4ByInterface(iface string) (string, string, string, bool) {
	args := []string{"-o", "-4", "addr", "show"}
	if strings.TrimSpace(iface) != "" {
		args = append(args, "dev", iface)
	}
	lines, timedOut, err := runCommandLines("ip", localCmdTimeout, args...)
	if timedOut {
		return "", "", "", true
	}
	if err != nil {
		return "", "", "", false
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if fields[2] != "inet" {
			continue
		}
		resolvedIface := strings.TrimSuffix(strings.TrimSpace(fields[1]), ":")
		addr := strings.TrimSpace(fields[3])
		parts := strings.SplitN(addr, "/", 2)
		ip := parts[0]
		prefix := ""
		if len(parts) == 2 {
			prefix = parts[1]
		}
		return ip, prefix, resolvedIface, false
	}
	return "", "", "", false
}

// readInterfaceMAC은 sysfs에서 인터페이스 MAC 주소를 읽는다.
func readInterfaceMAC(iface string) string {
	if strings.TrimSpace(iface) == "" {
		return ""
	}
	content, err := os.ReadFile("/sys/class/net/" + iface + "/address")
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(string(content)))
}

// readLocalUUID는 시스템 product UUID를 읽는다.
func readLocalUUID() (string, bool) {
	lines, timedOut, err := runCommandLines("cat", localCmdTimeout, "/sys/class/dmi/id/product_uuid")
	if timedOut {
		return "", true
	}
	if err != nil || len(lines) == 0 {
		return "", false
	}
	return strings.TrimSpace(lines[0]), false
}

// readFirstNameserver는 resolv.conf의 첫 번째 nameserver를 읽는다.
func readFirstNameserver() string {
	content, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return ""
	}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nameserver") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1]
			}
		}
	}
	return ""
}

// systemctlActive는 서비스 active 상태를 문자열로 반환한다.
func systemctlActive(service string) string {
	lines, timedOut, err := runCommandLines("systemctl", localCmdTimeout, "is-active", service)
	if timedOut {
		return timeoutValue
	}
	if err != nil {
		return "inactive"
	}
	value := strings.TrimSpace(strings.Join(lines, " "))
	if value == "" {
		return "inactive"
	}
	return value
}
