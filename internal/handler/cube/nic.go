package cube

import (
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
	"github.com/goccy/go-json"
)

type NICDevice = CubeModel.NICDevice
type TypeNICStatus = CubeModel.TypeNICStatus
type NICResponse = CubeModel.NICResponse
type NICAddress = CubeModel.NICAddress
type BondDetail = CubeModel.BondDetail
type NICIPInfo = CubeModel.NICIPInfo

// GetNICs godoc
//
//	@Summary		Show List of NIC
//	@Description	Cube의 NIC목록을 보여줍니다.
//	@Tags			CUBE - NIC
//	@Accept		x-www-form-urlencoded
//	@Produce		json
//	@Param			action	query	string	false	"nic action"	Enums(list,detail)
//	@Success		200	{object}	CubeModel.NICResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		404	{object}	HTTP404NotFound
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/nics [get]
func GetNICs(context *gin.Context) {
	action := normalizeNICAction(context.DefaultQuery("action", "list"))
	current := &TypeNICStatus{}
	if err := updateNICsWithAction(current, action); err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read nics",
		})
		return
	}
	context.IndentedJSON(http.StatusOK, buildNICResponse(current))
}

// UpdateNICs는 전역 NIC 상태 모델을 기본 list 기준으로 갱신한다.
func UpdateNICs() {
	n := CubeModel.NIC()
	n.Lock()
	defer n.Unlock()
	_ = updateNICsWithAction(n, "list")
}

// updateNICsWithAction은 action 모드에 맞게 NIC 목록을 재수집한다.
func updateNICsWithAction(nic *TypeNICStatus, action string) error {
	if nic == nil {
		return nil
	}

	includeDetail := action == "detail"
	var pciModelMap map[string]string
	if includeDetail {
		pciModelMap = loadPCIDeviceModelMap()
	}
	devices, err := listNICDevices(includeDetail, pciModelMap)
	if err != nil {
		return err
	}

	bridges, ethernets, bonds, others := splitByType(devices)
	nic.Bridges = bridges
	nic.Ethernets = ethernets
	nic.Bonds = bonds
	nic.Others = others
	nic.RefreshTime = time.Now()
	return nil
}

// buildNICResponse는 내부 NIC 상태 구조를 API 응답 형태로 변환한다.
func buildNICResponse(nic *TypeNICStatus) NICResponse {
	if nic == nil {
		return NICResponse{}
	}
	return NICResponse{
		Bridges:     nic.Bridges,
		Ethernets:   nic.Ethernets,
		Bonds:       nic.Bonds,
		Others:      nic.Others,
		RefreshTime: nic.RefreshTime.Format(time.RFC3339),
	}
}

// normalizeNICAction은 action 값을 list/detail 중 하나로 정규화한다.
func normalizeNICAction(action string) string {
	switch strings.ToLower(action) {
	case "detail":
		return "detail"
	default:
		return "list"
	}
}

// listNICDevices는 사용 가능한 수집 경로를 선택해 NIC 목록을 반환한다.
func listNICDevices(includeDetail bool, pciModelMap map[string]string) ([]NICDevice, error) {
	if includeDetail {
		return listNICDevicesByIP(true, pciModelMap)
	}
	if _, err := exec.LookPath("nmcli"); err == nil {
		if devices, err := listNICDevicesByNMCLI(false, pciModelMap); err == nil {
			return devices, nil
		}
	}
	return listNICDevicesByIP(false, pciModelMap)
}

// listNICDevicesByNMCLI는 nmcli 결과를 바탕으로 NIC 목록을 생성한다.
func listNICDevicesByNMCLI(includeDetail bool, pciModelMap map[string]string) ([]NICDevice, error) {
	cmd := exec.Command("nmcli", "-t", "-f", "DEVICE,TYPE,STATE,CON-PATH", "device", "status")
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		if gin.IsDebugging() {
			msg := strings.TrimSpace(string(stdout))
			if msg != "" {
				utils.FancyHandleError(err)
			}
		}
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	devices := make([]NICDevice, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 4)
		if len(parts) < 3 {
			continue
		}
		dev := strings.TrimSpace(parts[0])
		typeVal := strings.TrimSpace(parts[1])
		state := strings.TrimSpace(parts[2])
		conPath := "--"
		if len(parts) >= 4 {
			conPath = strings.TrimSpace(parts[3])
			if conPath == "" {
				conPath = "--"
			}
		}

		item := NICDevice{
			Device:  dev,
			Type:    typeVal,
			State:   state,
			ConPath: conPath,
		}
		enrichNICDevice(&item, includeDetail, pciModelMap)
		devices = append(devices, item)
	}
	return devices, nil
}

// ipLink는 `ip -j` 명령 JSON 출력을 파싱하기 위한 내부 구조체다.
type ipLink struct {
	Ifname    string `json:"ifname"`
	Operstate string `json:"operstate"`
	LinkType  string `json:"link_type"`
	Mtu       int    `json:"mtu"`
	Address   string `json:"address"`
	Master    string `json:"master,omitempty"`
	Linkinfo  struct {
		InfoKind string `json:"info_kind"`
		InfoData struct {
			Mode            string `json:"mode,omitempty"`
			ActiveSlave     string `json:"active_slave,omitempty"`
			Primary         string `json:"primary,omitempty"`
			PrimaryReselect string `json:"primary_reselect,omitempty"`
			XmitHashPolicy  string `json:"xmit_hash_policy,omitempty"`
			AdLacpActive    string `json:"ad_lacp_active,omitempty"`
			AdLacpRate      string `json:"ad_lacp_rate,omitempty"`
			AdSelect        string `json:"ad_select,omitempty"`
			FailOverMac     string `json:"fail_over_mac,omitempty"`
			Miimon          int    `json:"miimon,omitempty"`
			Updelay         int    `json:"updelay,omitempty"`
			Downdelay       int    `json:"downdelay,omitempty"`
		} `json:"info_data,omitempty"`
	} `json:"linkinfo"`
	AddrInfo []struct {
		Family    string `json:"family"`
		Local     string `json:"local"`
		Prefixlen int    `json:"prefixlen"`
		Scope     string `json:"scope"`
	} `json:"addr_info,omitempty"`
}

// listNICDevicesByIP는 ip 명령 JSON 결과를 바탕으로 NIC 목록을 생성한다.
func listNICDevicesByIP(includeDetail bool, pciModelMap map[string]string) ([]NICDevice, error) {
	args := []string{"-d", "-j", "link", "show"}
	if includeDetail {
		args = []string{"-d", "-j", "address", "show"}
	}
	cmd := exec.Command("ip", args...)
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		if gin.IsDebugging() {
			msg := strings.TrimSpace(string(stdout))
			if msg != "" {
				utils.FancyHandleError(err)
			}
		}
		return nil, err
	}

	var links []ipLink
	if err := json.Unmarshal(stdout, &links); err != nil {
		if gin.IsDebugging() {
			utils.FancyHandleError(err)
		}
		return nil, err
	}

	membersByBridge := map[string][]string{}
	for _, link := range links {
		if link.Master != "" {
			membersByBridge[link.Master] = append(membersByBridge[link.Master], link.Ifname)
		}
	}

	devices := make([]NICDevice, 0, len(links))
	for _, link := range links {
		typeVal := linkTypeToDeviceType(link)
		state := operstateToState(link.Operstate)
		item := NICDevice{
			Device:  link.Ifname,
			Type:    typeVal,
			State:   state,
			ConPath: "--",
		}

		if includeDetail {
			if link.Mtu > 0 {
				mtu := link.Mtu
				item.MTU = &mtu
			}
			item.MAC = link.Address
			item.IPv4 = buildNICIPInfo("inet", link.AddrInfo, boolPtr(hasIPv4(link.AddrInfo)))
			item.IPv6 = buildNICIPInfo("inet6", link.AddrInfo, boolPtr(resolveIPv6Enabled(link.Ifname, link.AddrInfo)))
			if strings.EqualFold(typeVal, "bridge") {
				item.Members = membersByBridge[link.Ifname]
			}
			if isBondType(typeVal, link.Linkinfo.InfoKind) {
				item.Bond = buildBondDetail(link)
			}
		}

		enrichNICDevice(&item, includeDetail, pciModelMap)
		devices = append(devices, item)
	}
	return devices, nil
}

// linkTypeToDeviceType은 ip link 메타데이터를 사람이 읽는 장치 타입으로 변환한다.
func linkTypeToDeviceType(link ipLink) string {
	if link.Linkinfo.InfoKind != "" {
		return link.Linkinfo.InfoKind
	}
	switch strings.ToLower(link.LinkType) {
	case "ether":
		return "ethernet"
	case "loopback":
		return "loopback"
	case "tun":
		return "tun"
	default:
		if link.LinkType != "" {
			return link.LinkType
		}
		return "other"
	}
}

// operstateToState는 커널 operstate 값을 API 상태 문자열로 바꾼다.
func operstateToState(operstate string) string {
	state := strings.ToLower(operstate)
	switch state {
	case "up", "unknown":
		return "connected"
	case "down", "dormant", "lowerlayerdown", "notpresent":
		return "disconnected"
	default:
		if state == "" {
			return "disconnected"
		}
		return state
	}
}

// enrichNICDevice는 드라이버, PCI, 속도, 모델 같은 보조 정보를 채운다.
func enrichNICDevice(item *NICDevice, includeDetail bool, pciModelMap map[string]string) {
	if item == nil || item.Device == "" {
		return
	}
	if item.Driver == "" {
		item.Driver = readNetDeviceDriver(item.Device)
	}
	if item.PCI == "" {
		item.PCI = readNetDevicePCI(item.Device)
	}
	if includeDetail {
		if item.Speed == "" {
			item.Speed = readNetDeviceSpeed(item.Device)
		}
		if item.Model == "" {
			item.Model = readNetDeviceModel(item.PCI, pciModelMap)
		}
	}
}

// readNetDeviceDriver는 sysfs를 통해 NIC 드라이버 이름을 읽는다.
func readNetDeviceDriver(dev string) string {
	link, err := os.Readlink(filepath.Join("/sys/class/net", dev, "device", "driver"))
	if err != nil {
		return ""
	}
	return filepath.Base(link)
}

// readNetDevicePCI는 sysfs를 통해 NIC의 PCI 슬롯 주소를 읽는다.
func readNetDevicePCI(dev string) string {
	link, err := os.Readlink(filepath.Join("/sys/class/net", dev, "device"))
	if err != nil {
		return ""
	}
	return filepath.Base(link)
}

// readNetDeviceSpeed는 sysfs speed 값을 읽어 표시용 속도 문자열로 바꾼다.
func readNetDeviceSpeed(dev string) string {
	data, err := os.ReadFile(filepath.Join("/sys/class/net", dev, "speed"))
	if err != nil {
		return ""
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "-1" {
		return ""
	}
	mbps, err := strconv.Atoi(raw)
	if err != nil || mbps <= 0 {
		return ""
	}
	return formatSpeed(mbps)
}

// formatSpeed는 Mbps 값을 M/G 단위 문자열로 포맷한다.
func formatSpeed(mbps int) string {
	if mbps%1000 == 0 {
		return fmt.Sprintf("%dG", mbps/1000)
	}
	if mbps >= 1000 {
		gbps := float64(mbps) / 1000.0
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1fG", gbps), "0"), ".") + "G"
	}
	return fmt.Sprintf("%dM", mbps)
}

// readNetDeviceModel은 PCI 슬롯 기준으로 장치 모델명을 조회한다.
func readNetDeviceModel(pci string, pciModelMap map[string]string) string {
	if pci == "" || pciModelMap == nil {
		return ""
	}
	key := normalizePCISlot(pci)
	if key == "" {
		return ""
	}
	return pciModelMap[key]
}

// buildNICAddressesByFamily는 주소 목록에서 특정 family(inet/inet6)만 골라 구조체로 만든다.
func buildNICAddressesByFamily(addrInfo []struct {
	Family    string `json:"family"`
	Local     string `json:"local"`
	Prefixlen int    `json:"prefixlen"`
	Scope     string `json:"scope"`
}, family string) []NICAddress {
	if len(addrInfo) == 0 {
		return nil
	}
	out := make([]NICAddress, 0, len(addrInfo))
	for _, addr := range addrInfo {
		if addr.Local == "" {
			continue
		}
		if family != "" && addr.Family != family {
			continue
		}
		out = append(out, NICAddress{
			Family:    addr.Family,
			Address:   addr.Local,
			Prefixlen: addr.Prefixlen,
			Scope:     addr.Scope,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildNICIPInfo는 IP 활성 여부와 주소 배열을 하나의 구조체로 합친다.
func buildNICIPInfo(family string, addrInfo []struct {
	Family    string `json:"family"`
	Local     string `json:"local"`
	Prefixlen int    `json:"prefixlen"`
	Scope     string `json:"scope"`
}, enabled *bool) *NICIPInfo {
	info := &NICIPInfo{
		Enable: enabled,
	}
	addrs := buildNICAddressesByFamily(addrInfo, family)
	if len(addrs) > 0 {
		info.Addresses = addrs
	}
	return info
}

// hasIPv4는 주소 목록 안에 IPv4 주소가 존재하는지 확인한다.
func hasIPv4(addrInfo []struct {
	Family    string `json:"family"`
	Local     string `json:"local"`
	Prefixlen int    `json:"prefixlen"`
	Scope     string `json:"scope"`
}) bool {
	for _, addr := range addrInfo {
		if addr.Family == "inet" && addr.Local != "" {
			return true
		}
	}
	return false
}

// resolveIPv6Enabled는 sysctl과 주소 존재 여부를 이용해 IPv6 활성 상태를 판단한다.
func resolveIPv6Enabled(dev string, addrInfo []struct {
	Family    string `json:"family"`
	Local     string `json:"local"`
	Prefixlen int    `json:"prefixlen"`
	Scope     string `json:"scope"`
}) bool {
	disabled, ok := readIPv6Disabled(dev)
	if ok {
		return !disabled
	}
	for _, addr := range addrInfo {
		if addr.Family == "inet6" && addr.Local != "" {
			return true
		}
	}
	return false
}

// readIPv6Disabled는 인터페이스별 disable_ipv6 sysctl 값을 읽는다.
func readIPv6Disabled(dev string) (bool, bool) {
	data, err := os.ReadFile(filepath.Join("/proc/sys/net/ipv6/conf", dev, "disable_ipv6"))
	if err != nil {
		return false, false
	}
	val := strings.TrimSpace(string(data))
	return val == "1", true
}

// boolPtr는 bool 값을 포인터로 감싸기 위한 보조 함수다.
func boolPtr(v bool) *bool {
	return &v
}

// isBondType은 장치가 bond 계열 인터페이스인지 판별한다.
func isBondType(devType string, infoKind string) bool {
	if strings.EqualFold(devType, "bond") || strings.EqualFold(devType, "bonding") {
		return true
	}
	return strings.EqualFold(infoKind, "bond")
}

// buildBondDetail은 bond 인터페이스의 상세 설정을 응답 구조체로 변환한다.
func buildBondDetail(link ipLink) *BondDetail {
	data := link.Linkinfo.InfoData
	detail := &BondDetail{
		Mode:            data.Mode,
		ActiveSlave:     data.ActiveSlave,
		Primary:         data.Primary,
		PrimaryReselect: data.PrimaryReselect,
		XmitHashPolicy:  data.XmitHashPolicy,
		LACPActive:      data.AdLacpActive,
		LACPRate:        data.AdLacpRate,
		AdSelect:        data.AdSelect,
		FailOverMac:     data.FailOverMac,
	}
	if data.Miimon > 0 {
		val := data.Miimon
		detail.Miimon = &val
	}
	if data.Updelay > 0 {
		val := data.Updelay
		detail.UpDelay = &val
	}
	if data.Downdelay > 0 {
		val := data.Downdelay
		detail.DownDelay = &val
	}
	if isEmptyBondDetail(detail) {
		return nil
	}
	return detail
}

// isEmptyBondDetail은 bond 상세 구조체에 실제 값이 들어 있는지 확인한다.
func isEmptyBondDetail(detail *BondDetail) bool {
	if detail == nil {
		return true
	}
	return detail.Mode == "" &&
		detail.ActiveSlave == "" &&
		detail.Primary == "" &&
		detail.PrimaryReselect == "" &&
		detail.XmitHashPolicy == "" &&
		detail.LACPActive == "" &&
		detail.LACPRate == "" &&
		detail.AdSelect == "" &&
		detail.FailOverMac == "" &&
		detail.Miimon == nil &&
		detail.UpDelay == nil &&
		detail.DownDelay == nil
}

// normalizePCISlot은 PCI 주소에서 0000: prefix를 제거해 비교용 키로 맞춘다.
func normalizePCISlot(slot string) string {
	return strings.TrimPrefix(slot, "0000:")
}

// loadPCIDeviceModelMap은 lspci 결과를 읽어 슬롯별 모델명 맵을 만든다.
func loadPCIDeviceModelMap() map[string]string {
	devices := listPCIDevicesForNIC()
	if len(devices) == 0 {
		return map[string]string{}
	}
	models := map[string]string{}
	for _, dev := range devices {
		slot := strings.TrimSpace(dev["Slot"])
		if slot == "" {
			continue
		}
		vendor := strings.TrimSpace(dev["Vendor"])
		device := strings.TrimSpace(dev["Device"])
		subDevice := strings.TrimSpace(dev["SDevice"])
		if vendor == "" && device == "" {
			continue
		}
		model := strings.TrimSpace(strings.TrimSpace(vendor + " " + device))
		if subDevice != "" {
			model = fmt.Sprintf("%s (%s)", model, subDevice)
		}
		key := normalizePCISlot(slot)
		if key != "" && model != "" {
			models[key] = model
		}
	}
	return models
}

// listPCIDevicesForNIC은 lspci -vmm 출력 전체를 key/value 맵 배열로 파싱한다.
func listPCIDevicesForNIC() []map[string]string {
	cmd := exec.Command("/usr/sbin/lspci", "-vmm", "-k")
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		if gin.IsDebugging() {
			utils.FancyHandleError(err)
		}
		return nil
	}

	lines := strings.Split(string(stdout), "\n")
	devices := make([]map[string]string, 0)

	current := map[string]string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(current) > 0 {
				devices = append(devices, current)
				current = map[string]string{}
			}
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key != "" {
			current[key] = val
		}
	}
	if len(current) > 0 {
		devices = append(devices, current)
	}

	return devices
}

// splitByType는 NIC 목록을 bridge/ethernet/bond/others로 분류한다.
func splitByType(devices []NICDevice) (bridges, ethernets, bonds, others []NICDevice) {
	for _, dev := range devices {
		switch strings.ToLower(dev.Type) {
		case "bridge":
			bridges = append(bridges, dev)
		case "ethernet":
			ethernets = append(ethernets, dev)
		case "bond", "bonding":
			bonds = append(bonds, dev)
		default:
			others = append(others, dev)
		}
	}
	return bridges, ethernets, bonds, others
}
