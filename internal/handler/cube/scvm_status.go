package cube

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	libvirtinfra "ablecloud.io/ablestack-api/internal/infra/libvirt"
	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
	libvirt "libvirt.org/go/libvirt"
)

type SCVMStatusDetail = CubeModel.SCVMStatusDetail
type SCVMStatusResponse = CubeModel.SCVMStatusResponse

const scvmDomainName = "scvm"
const scvmStatusTTL = 2 * time.Second

// scvmStatusCache는 짧은 시간 동안 조회 결과를 재사용하기 위한 메모리 캐시다.
var scvmStatusCache = struct {
	mu      sync.Mutex
	expires time.Time
	data    SCVMStatusDetail
}{}

// GetSCVMStatus godoc
//
//	@Summary		SCVM Status
//	@Description	SCVM의 상태를 조회합니다.
//	@Tags			Cube-SCVM
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Success		200	{object}	CubeModel.SCVMStatusResponse
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/scvm/status [get]
func GetSCVMStatus(context *gin.Context) {
	status := defaultSCVMStatus()

	cfg, err := loadClusterConfigSection()
	if err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}

	host, err := findSelfHost(cfg)
	if err != nil {
		context.JSON(http.StatusInternalServerError, SCVMStatusResponse{Code: 500, Data: status, Message: err.Error()})
		return
	}

	if !libvirtinfra.IsSocketAvailable() {
		if target := strings.TrimSpace(host.Ablecube); target != "" && !isLocalTarget(target) {
			if proxied, err := proxySCVMStatus(target); err == nil {
				context.JSON(http.StatusOK, SCVMStatusResponse{Code: 200, Data: proxied})
				return
			}
		}
	}

	status = getCachedSCVMStatus(host)
	context.JSON(http.StatusOK, SCVMStatusResponse{Code: 200, Data: status})
}

// defaultSCVMStatus는 조회 실패 전 기본 응답 구조를 만든다.
func defaultSCVMStatus() SCVMStatusDetail {
	return SCVMStatusDetail{
		ScvmStatus:                  "HEALTH_ERR",
		VCPU:                        "N/A",
		Socket:                      "N/A",
		Core:                        "N/A",
		Memory:                      "N/A",
		RootDiskSize:                "N/A",
		RootDiskAvail:               "N/A",
		RootDiskUsePer:              "N/A",
		ManageNicType:               "N/A",
		ManageNicParent:             "N/A",
		ManageNicIP:                 "N/A",
		ManageNicGw:                 "N/A",
		ManageNicDns:                "N/A",
		StorageServerNicType:        "N/A",
		StorageServerNicParent:      "N/A",
		StorageServerNicIP:          "N/A",
		StorageReplicationNicType:   "N/A",
		StorageReplicationNicParent: "N/A",
		StorageReplicationNicIP:     "N/A",
	}
}

// findSelfHost는 hostname 또는 로컬 IP를 기준으로 cluster.json에서 현재 host 항목을 찾는다.
func findSelfHost(cfg *CubeModel.ClusterConfigSection) (*CubeModel.ClusterHost, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cluster config not found")
	}
	name, _ := os.Hostname()
	name = strings.TrimSpace(name)
	short := strings.ToLower(strings.SplitN(name, ".", 2)[0])
	full := strings.ToLower(name)

	for i := range cfg.Hosts {
		host := &cfg.Hosts[i]
		if matchesLocalHostname(host.Hostname, full, short) {
			return host, nil
		}
	}

	for i := range cfg.Hosts {
		host := &cfg.Hosts[i]
		targets := []string{
			host.Ablecube,
			host.ScvmMngt,
			host.AblecubePn,
			host.Scvm,
			host.ScvmCn,
		}
		for _, target := range targets {
			if isLocalTarget(strings.TrimSpace(target)) {
				return host, nil
			}
		}
	}

	return nil, fmt.Errorf("self host not found in cluster.json")
}

func matchesLocalHostname(hostname string, full string, short string) bool {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "" {
		return false
	}
	hostShort := strings.SplitN(hostname, ".", 2)[0]
	return hostname == full || hostname == short || hostShort == full || hostShort == short
}

// getCachedSCVMStatus는 TTL 캐시를 사용해 짧은 시간 내 반복 조회를 줄인다.
func getCachedSCVMStatus(host *CubeModel.ClusterHost) SCVMStatusDetail {
	now := time.Now()
	scvmStatusCache.mu.Lock()
	defer scvmStatusCache.mu.Unlock()
	if now.Before(scvmStatusCache.expires) {
		return scvmStatusCache.data
	}
	data := collectSCVMStatus(host)
	scvmStatusCache.data = data
	scvmStatusCache.expires = now.Add(scvmStatusTTL)
	return data
}

// collectSCVMStatus는 SCVM의 도메인, 디스크, NIC, 게이트웨이, DNS 정보를 한 번에 수집한다.
func collectSCVMStatus(host *CubeModel.ClusterHost) SCVMStatusDetail {
	status := defaultSCVMStatus()
	if host == nil {
		return status
	}

	state, vcpu, memory, timedOut, err := readSCVMDomInfo()
	if timedOut {
		status.ScvmStatus = timeoutValue
		status.VCPU = timeoutValue
		status.Memory = timeoutValue
	} else if err == nil {
		if state != "" {
			status.ScvmStatus = state
		}
		if vcpu != "" {
			status.VCPU = vcpu
		}
		if memory != "" {
			status.Memory = memory
		}
	}

	if size, avail, usePer, timedOut := readSCVMRootDisk(); timedOut {
		status.RootDiskSize = timeoutValue
		status.RootDiskAvail = timeoutValue
		status.RootDiskUsePer = timeoutValue
	} else {
		if size != "" {
			status.RootDiskSize = size
		}
		if avail != "" {
			status.RootDiskAvail = avail
		}
		if usePer != "" {
			status.RootDiskUsePer = usePer
		}
	}

	manageIP := strings.TrimSpace(host.ScvmMngt)
	storageIP := strings.TrimSpace(host.Scvm)
	replIP := strings.TrimSpace(host.ScvmCn)

	if manageIP != "" {
		status.ManageNicIP = manageIP
	}
	if storageIP != "" {
		status.StorageServerNicIP = storageIP
	}
	if replIP != "" {
		status.StorageReplicationNicIP = replIP
	}

	ifaddrMap, addrMap, ifaddrTimeout, _ := loadDomifaddrMaps(scvmDomainName)
	iflistMap, iflistTimeout, _ := loadDomiflistMap(scvmDomainName)
	hostdevSources, _, _ := libvirtinfra.LoadHostdevSourcesFromDomainXML(scvmDomainName)
	nicHostdevSources := filterNICPassthroughSources(hostdevSources)

	status.ManageNicIP, status.ManageNicType, status.ManageNicParent = resolveNICInfo(manageIP, ifaddrMap, addrMap, ifaddrTimeout, iflistMap, iflistTimeout, nil)
	status.StorageServerNicIP, status.StorageServerNicType, status.StorageServerNicParent = resolveNICInfo(storageIP, ifaddrMap, addrMap, ifaddrTimeout, iflistMap, iflistTimeout, nicHostdevSources)
	status.StorageReplicationNicIP, status.StorageReplicationNicType, status.StorageReplicationNicParent = resolveNICInfo(replIP, ifaddrMap, addrMap, ifaddrTimeout, iflistMap, iflistTimeout, nicHostdevSources)

	if gw, timedOut := readSCVMGateway(); timedOut {
		status.ManageNicGw = timeoutValue
	} else if gw != "" {
		status.ManageNicGw = gw
	}
	if dns, timedOut := readSCVMDNS(); timedOut {
		status.ManageNicDns = timeoutValue
	} else if dns != "" {
		status.ManageNicDns = dns
	}

	return status
}

// readSCVMDomInfo는 libvirt 우선, virsh fallback 순서로 SCVM 도메인 정보를 읽는다.
func readSCVMDomInfo() (string, string, string, bool, error) {
	details, timedOut, err := libvirtinfra.ReadDomainDetails(scvmDomainName)
	if timedOut {
		return "", "", "", true, err
	}
	if err == nil {
		memory := formatMemoryKiB(int64(details.MaxMemKiB))
		if memory == "N/A" && details.UsedMemKiB > 0 {
			memory = formatMemoryKiB(int64(details.UsedMemKiB))
		}
		return details.State, details.VCPU, memory, false, nil
	}

	return readSCVMDomInfoViaVirsh()
}

// readSCVMDomInfoViaVirsh는 `virsh dominfo`를 파싱해 상태, vCPU, 메모리 정보를 읽는다.
func readSCVMDomInfoViaVirsh() (string, string, string, bool, error) {
	lines, timedOut, err := runVirshLines("dominfo", scvmDomainName)
	if timedOut {
		return "", "", "", true, err
	}
	if err != nil {
		return "", "", "", false, err
	}

	var state, vcpu, memory string
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "state":
			state = val
		case "cpu(s)":
			vcpu = val
		case "max memory":
			if parsed := parseDominfoMemory(val); parsed != "" {
				memory = parsed
			}
		case "used memory":
			if memory == "" {
				if parsed := parseDominfoMemory(val); parsed != "" {
					memory = parsed
				}
			}
		}
	}
	return state, vcpu, memory, false, nil
}

// parseDominfoMemory는 dominfo 메모리 문자열을 공통 메모리 표기 문자열로 바꾼다.
func parseDominfoMemory(val string) string {
	if strings.TrimSpace(val) == "" {
		return ""
	}
	fields := strings.Fields(val)
	if len(fields) == 0 {
		return ""
	}
	raw, err := strconv.ParseInt(strings.ReplaceAll(fields[0], ",", ""), 10, 64)
	if err != nil {
		return ""
	}
	unit := ""
	if len(fields) > 1 {
		unit = strings.ToLower(fields[1])
	}
	switch unit {
	case "kib", "kb", "k":
		return formatMemoryKiB(raw)
	case "mib", "mb", "m":
		return formatMemoryKiB(raw * 1024)
	case "gib", "gb", "g":
		return formatMemoryKiB(raw * 1024 * 1024)
	default:
		return formatMemoryKiB(raw)
	}
}

// fsInfo는 domfsinfo 파싱 결과를 내부 계산용으로 보관하는 구조체다.
type fsInfo struct {
	mount string
	name  string
	total int64
	used  int64
}

// readSCVMRootDisk는 루트 파일시스템 사용량을 libvirt 우선, virsh fallback 순서로 읽는다.
func readSCVMRootDisk() (string, string, string, bool) {
	if size, avail, usePer, timedOut, ok := readSCVMRootDiskViaLibvirt(); timedOut {
		return "", "", "", true
	} else if ok {
		return size, avail, usePer, false
	}
	return readSCVMRootDiskViaVirsh()
}

// readSCVMRootDiskViaLibvirt는 guest filesystem 정보에서 루트 디스크 용량을 계산한다.
func readSCVMRootDiskViaLibvirt() (string, string, string, bool, bool) {
	info, timedOut, err := libvirtinfra.ReadDomainGuestInfo(scvmDomainName, libvirt.DOMAIN_GUEST_INFO_FILESYSTEM)
	if timedOut {
		return "", "", "", true, false
	}
	if err != nil || info == nil || len(info.FileSystems) == 0 {
		return "", "", "", false, false
	}
	fs := pickGuestRootFS(info.FileSystems)
	if fs == nil {
		return "", "", "", false, false
	}
	var (
		totalBytes uint64
		usedBytes  uint64
	)
	if fs.TotalBytesSet {
		totalBytes = fs.TotalBytes
	}
	if fs.UsedBytesSet {
		usedBytes = fs.UsedBytes
	}
	if totalBytes == 0 {
		return "", "", "", false, false
	}
	size := formatBytesHuman(int64(totalBytes))
	avail := ""
	usePer := ""
	if fs.UsedBytesSet {
		used := int64(usedBytes)
		total := int64(totalBytes)
		if used < 0 {
			used = 0
		}
		if used > total {
			used = total
		}
		availVal := total - used
		if availVal < 0 {
			availVal = 0
		}
		avail = formatBytesHuman(availVal)
		usePer = fmt.Sprintf("%.0f%%", float64(used)/float64(total)*100)
	}
	ok := size != "" || avail != "" || usePer != ""
	return size, avail, usePer, false, ok
}

// pickGuestRootFS는 guest filesystem 목록 중 `/` 또는 root 후보를 선택한다.
func pickGuestRootFS(filesystems []libvirt.DomainGuestInfoFileSystem) *libvirt.DomainGuestInfoFileSystem {
	var fallback *libvirt.DomainGuestInfoFileSystem
	for i := range filesystems {
		fs := &filesystems[i]
		mount := ""
		name := ""
		if fs.MountPointSet {
			mount = strings.TrimSpace(fs.MountPoint)
		}
		if fs.NameSet {
			name = strings.TrimSpace(fs.Name)
		}
		if mount == "/" {
			return fs
		}
		if fallback == nil && strings.Contains(strings.ToLower(name), "root") {
			fallback = fs
		}
	}
	if fallback != nil {
		return fallback
	}
	if len(filesystems) == 1 {
		return &filesystems[0]
	}
	return nil
}

// readSCVMRootDiskViaVirsh는 `virsh domfsinfo` 출력에서 루트 디스크 용량을 계산한다.
func readSCVMRootDiskViaVirsh() (string, string, string, bool) {
	lines, timedOut, err := runVirshLines("domfsinfo", scvmDomainName)
	if timedOut {
		return "", "", "", true
	}
	if err != nil {
		return "", "", "", false
	}
	filesystems := parseDomfsinfo(lines)
	if len(filesystems) == 0 {
		return "", "", "", false
	}
	info := pickRootFilesystem(filesystems)
	if info == nil || info.total <= 0 {
		return "", "", "", false
	}
	used := info.used
	if used < 0 {
		used = 0
	}
	avail := info.total - used
	if avail < 0 {
		avail = 0
	}
	usePer := ""
	if info.total > 0 {
		usePer = fmt.Sprintf("%.0f%%", float64(used)/float64(info.total)*100)
	}
	return formatBytesHuman(info.total), formatBytesHuman(avail), usePer, false
}

// parseDomfsinfo는 domfsinfo 텍스트 출력을 내부 구조체 목록으로 변환한다.
func parseDomfsinfo(lines []string) []fsInfo {
	filesystems := make([]fsInfo, 0)
	var current *fsInfo

	flush := func() {
		if current == nil {
			return
		}
		filesystems = append(filesystems, *current)
		current = nil
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		if key == "mountpoint" {
			flush()
			current = &fsInfo{mount: value}
			continue
		}
		if current == nil {
			current = &fsInfo{}
		}
		switch key {
		case "name":
			current.name = value
		case "bytes", "capacity", "total bytes", "size":
			if parsed := parseInt64Value(value); parsed > 0 {
				current.total = parsed
			}
		case "bytes used", "used", "allocation":
			if parsed := parseInt64Value(value); parsed > 0 {
				current.used = parsed
			}
		}
	}
	flush()
	return filesystems
}

// pickRootFilesystem는 domfsinfo 결과 중 루트(`/`) 파일시스템을 우선 선택한다.
func pickRootFilesystem(filesystems []fsInfo) *fsInfo {
	var fallback *fsInfo
	for i := range filesystems {
		fs := &filesystems[i]
		if fs.mount == "/" {
			return fs
		}
		if fallback == nil && strings.Contains(strings.ToLower(fs.name), "root") {
			fallback = fs
		}
	}
	if fallback != nil {
		return fallback
	}
	if len(filesystems) == 1 {
		return &filesystems[0]
	}
	return nil
}

// readSCVMGateway는 guest route 정보나 `/proc/net/route`에서 기본 게이트웨이를 찾는다.
func readSCVMGateway() (string, bool) {
	if gw, timedOut := readGatewayFromProcRoute(); timedOut {
		return "", true
	} else if gw != "" {
		return gw, false
	}

	routes, timedOut, err := libvirtinfra.ReadGuestRoutes(scvmDomainName, libvirtinfra.DefaultTimeout)
	if timedOut {
		return "", true
	}
	if err == nil {
		if gw := libvirtinfra.PickDefaultGateway(routes); gw != "" {
			return gw, false
		}
	}
	return "", false
}

// readSCVMDNS는 guest 내부 resolv.conf에서 첫 번째 DNS 서버를 읽는다.
func readSCVMDNS() (string, bool) {
	content, timedOut, err := libvirtinfra.ReadGuestFile(scvmDomainName, "/etc/resolv.conf", libvirtinfra.DefaultTimeout)
	if timedOut {
		return "", true
	}
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nameserver") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return strings.TrimSpace(fields[1]), false
			}
		}
	}
	return "", false
}

// readGatewayFromProcRoute는 guest의 `/proc/net/route` 파일을 직접 읽어 게이트웨이를 구한다.
func readGatewayFromProcRoute() (string, bool) {
	content, timedOut, err := libvirtinfra.ReadGuestFile(scvmDomainName, "/proc/net/route", libvirtinfra.DefaultTimeout)
	if timedOut || err != nil {
		return "", timedOut
	}
	return parseDefaultGatewayFromRouteTable(content), false
}

// parseDefaultGatewayFromRouteTable는 route 테이블 텍스트에서 기본 게이트웨이 hex 값을 추출한다.
func parseDefaultGatewayFromRouteTable(content string) string {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 3 {
			continue
		}
		if fields[1] != "00000000" {
			continue
		}
		gwHex := fields[2]
		if gwHex == "" || gwHex == "00000000" {
			continue
		}
		if gw := parseGatewayHex(gwHex); gw != "" {
			return gw
		}
	}
	return ""
}

// parseGatewayHex는 little-endian 16진 게이트웨이 값을 일반 IPv4 문자열로 변환한다.
func parseGatewayHex(value string) string {
	raw, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return ""
	}
	ip := net.IPv4(byte(raw), byte(raw>>8), byte(raw>>16), byte(raw>>24))
	return ip.String()
}

// formatMemoryKiB는 KiB 단위 메모리를 MiB/GiB/TiB 문자열로 변환한다.
func formatMemoryKiB(kib int64) string {
	if kib <= 0 {
		return "N/A"
	}
	mib := float64(kib) / 1024
	if mib < 1024 {
		return fmt.Sprintf("%.0f MiB", mib)
	}
	gib := mib / 1024
	if gib < 1024 {
		return fmt.Sprintf("%.0f GiB", gib)
	}
	tib := gib / 1024
	return fmt.Sprintf("%.0f TiB", tib)
}

// formatBytesHuman은 바이트 단위를 사람이 읽기 쉬운 B/K/M/G/T/P 형식으로 변환한다.
func formatBytesHuman(value int64) string {
	if value < 0 {
		return "N/A"
	}
	units := []string{"B", "K", "M", "G", "T", "P"}
	val := float64(value)
	for i, unit := range units {
		if val < 1024 || i == len(units)-1 {
			if unit == "B" {
				return fmt.Sprintf("%d%s", int64(val), unit)
			}
			if val >= 10 {
				return fmt.Sprintf("%.0f%s", val, unit)
			}
			return fmt.Sprintf("%.1f%s", val, unit)
		}
		val /= 1024
	}
	return fmt.Sprintf("%dB", value)
}

// parseInt64Value는 문자열 숫자 첫 토큰을 int64로 변환한다.
func parseInt64Value(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return 0
	}
	candidate := strings.ReplaceAll(parts[0], ",", "")
	parsed, err := strconv.ParseInt(candidate, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

// loadDomifaddrMaps는 libvirt, guest agent, virsh 순서로 IP/MAC 매핑 정보를 수집한다.
func loadDomifaddrMaps(domain string) (map[string]string, map[string]string, bool, error) {
	ipToMac, ipToAddr, timedOut, err := libvirtinfra.LoadDomifaddrMaps(domain, libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_AGENT)
	if timedOut {
		return nil, nil, true, err
	}
	if err == nil && (len(ipToAddr) > 0 || len(ipToMac) > 0) {
		return ipToMac, ipToAddr, false, nil
	}

	ipToMac, ipToAddr, timedOut, err = libvirtinfra.LoadDomifaddrMaps(domain, libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE)
	if timedOut {
		return nil, nil, true, err
	}
	if err == nil && (len(ipToAddr) > 0 || len(ipToMac) > 0) {
		return ipToMac, ipToAddr, false, nil
	}

	ipToMac, ipToAddr, timedOut, err = libvirtinfra.LoadDomifaddrMapsFromGuestAgent(domain, libvirtinfra.DefaultTimeout)
	if timedOut {
		return nil, nil, true, err
	}
	if err == nil && (len(ipToAddr) > 0 || len(ipToMac) > 0) {
		return ipToMac, ipToAddr, false, nil
	}

	lines, timedOut, err := runVirshLines("domifaddr", "--domain", domain, "--source", "agent", "--full")
	if timedOut {
		return nil, nil, true, err
	}
	if err != nil {
		return nil, nil, false, err
	}
	ipToMac = map[string]string{}
	ipToAddr = map[string]string{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if !strings.Contains(fields[2], "ipv4") {
			continue
		}
		addr := fields[3]
		parts := strings.SplitN(addr, "/", 2)
		ip := parts[0]
		if ip == "" {
			continue
		}
		ipToMac[ip] = strings.ToLower(fields[1])
		ipToAddr[ip] = addr
	}
	return ipToMac, ipToAddr, false, nil
}

// loadDomiflistMap는 MAC 주소 기준으로 NIC 타입과 parent 정보를 매핑한다.
func loadDomiflistMap(domain string) (map[string][2]string, bool, error) {
	if macMap, timedOut, err := libvirtinfra.LoadInterfaceMapFromDomainXML(domain); timedOut || err == nil {
		return macMap, timedOut, err
	}

	lines, timedOut, err := runVirshLines("domiflist", domain)
	if timedOut {
		return nil, true, err
	}
	if err != nil {
		return nil, false, err
	}
	macMap := map[string][2]string{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if strings.HasPrefix(fields[0], "Interface") || strings.HasPrefix(fields[0], "--") {
			continue
		}
		mac := strings.ToLower(fields[4])
		macMap[mac] = [2]string{fields[1], fields[2]}
	}
	return macMap, false, nil
}

// resolveNICInfo는 IP를 기준으로 최종 NIC 주소, 타입, parent 값을 결정한다.
func resolveNICInfo(ip string, ipToMac map[string]string, ipToAddr map[string]string, ifaddrTimeout bool, macToInfo map[string][2]string, iflistTimeout bool, hostdevSources []string) (string, string, string) {
	if strings.TrimSpace(ip) == "" {
		return "N/A", "N/A", "N/A"
	}
	resolvedIP := ip
	if ifaddrTimeout {
		return resolvedIP, timeoutValue, timeoutValue
	}
	mac := strings.ToLower(ipToMac[ip])
	if mac == "" {
		if len(hostdevSources) > 0 {
			parent := "N/A"
			if len(hostdevSources) == 1 && strings.TrimSpace(hostdevSources[0]) != "" {
				parent = hostdevSources[0]
			}
			return resolvedIP, "NIC Passthrough", parent
		}
		return resolvedIP, "N/A", "N/A"
	}
	if addr, ok := ipToAddr[ip]; ok && addr != "" {
		resolvedIP = addr
	}
	if iflistTimeout {
		return resolvedIP, timeoutValue, timeoutValue
	}
	if info, ok := macToInfo[mac]; ok {
		return resolvedIP, info[0], info[1]
	}
	if len(hostdevSources) > 0 {
		parent := "N/A"
		if len(hostdevSources) == 1 && strings.TrimSpace(hostdevSources[0]) != "" {
			parent = hostdevSources[0]
		}
		return resolvedIP, "NIC Passthrough", parent
	}
	return resolvedIP, "N/A", "N/A"
}

// filterNICPassthroughSources는 hostdev source 중 실제 NIC로 판별된 PCI 장치만 남긴다.
func filterNICPassthroughSources(hostdevSources []string) []string {
	if len(hostdevSources) == 0 {
		return nil
	}
	out := make([]string, 0, len(hostdevSources))
	seen := map[string]struct{}{}
	for _, source := range hostdevSources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		class, timedOut, err := readPCIClassBySource(source)
		if timedOut || err != nil || !isNICPCIClass(class) {
			continue
		}
		seen[source] = struct{}{}
		out = append(out, source)
	}
	return out
}

// readPCIClassBySource는 PCI source 주소를 기준으로 lspci class 정보를 읽는다.
func readPCIClassBySource(source string) (string, bool, error) {
	slot := normalizeHostdevPCISource(source)
	if slot == "" {
		return "", false, fmt.Errorf("empty pci source")
	}
	args := []string{"-D", "-vmm", "-n", "-s", slot}
	info, timedOut, err := readLSPCIDeviceInfo("/usr/sbin/lspci", args)
	if err == nil || timedOut {
		return info["Class"], timedOut, err
	}
	info, timedOut, err = readLSPCIDeviceInfo("lspci", args)
	if err != nil || timedOut {
		return "", timedOut, err
	}
	return info["Class"], false, nil
}

// readLSPCIDeviceInfo는 lspci의 key:value 출력을 map 형태로 정리한다.
func readLSPCIDeviceInfo(command string, args []string) (map[string]string, bool, error) {
	lines, timedOut, err := runCommandLines(command, localCmdTimeout, args...)
	if timedOut || err != nil {
		return nil, timedOut, err
	}
	info := map[string]string{}
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key != "" {
			info[key] = value
		}
	}
	if len(info) == 0 {
		return nil, false, fmt.Errorf("empty lspci output")
	}
	return info, false, nil
}

// normalizeHostdevPCISource는 hostdev source 문자열을 lspci 조회용 표준 PCI 주소로 맞춘다.
func normalizeHostdevPCISource(source string) string {
	source = strings.TrimSpace(source)
	source = strings.TrimPrefix(source, "pci ")
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	if strings.Count(source, ":") == 1 {
		return "0000:" + source
	}
	return source
}

// isNICPCIClass는 lspci class 문자열이 NIC 계열 장치인지 판별한다.
func isNICPCIClass(class string) bool {
	class = strings.ToLower(strings.TrimSpace(class))
	if class == "" {
		return false
	}
	if strings.HasPrefix(class, "02") {
		return true
	}
	switch class {
	case "ethernet controller", "network controller", "infiniband controller":
		return true
	default:
		return false
	}
}

// proxySCVMStatus는 다른 host의 SCVM 상태 API를 호출해 동일한 응답 구조로 돌려준다.
func proxySCVMStatus(target string) (SCVMStatusDetail, error) {
	baseURL := buildTargetURL(target)
	url := fmt.Sprintf("%s/api/v1/cube/scvm/status", baseURL)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return SCVMStatusDetail{}, err
	}
	attachInternalToken(req)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return SCVMStatusDetail{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return SCVMStatusDetail{}, fmt.Errorf("scvm status failed: %s", resp.Status)
	}
	var out SCVMStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return SCVMStatusDetail{}, err
	}
	if out.Code != 200 {
		return SCVMStatusDetail{}, fmt.Errorf("scvm status failed: %s", out.Message)
	}
	return out.Data, nil
}
