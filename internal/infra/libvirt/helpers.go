package libvirtinfra

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	libvirt "libvirt.org/go/libvirt"
)

// libvirt 연결 정보와 기본 타임아웃 설정이다.
// 모든 libvirt 조회는 동일한 URI와 제한 시간 기준으로 동작한다.
const (
	libvirtURI     = "qemu:///system"
	DefaultTimeout = 3 * time.Second
)

// DomainDetails는 도메인 기본 상태 조회 결과를 담는다.
type DomainDetails struct {
	Name        string
	ID          string
	UUID        string
	State       string
	VCPU        string
	MaxMemKiB   uint64
	UsedMemKiB  uint64
	CpuTimeSecs float64
}

// ReadDomainDetails는 도메인 이름 기준으로 CPU/메모리/상태 정보를 읽는다.
func ReadDomainDetails(name string) (DomainDetails, bool, error) {
	details := DomainDetails{Name: name, ID: "-"}
	timedOut, err := withLibvirtDomainTimeout(name, func(dom *libvirt.Domain) error {
		domName, err := dom.GetName()
		if err == nil && strings.TrimSpace(domName) != "" {
			details.Name = domName
		}
		if id, err := dom.GetID(); err == nil {
			details.ID = fmt.Sprintf("%d", id)
		}
		if uuid, err := dom.GetUUIDString(); err == nil {
			details.UUID = uuid
		}
		info, err := dom.GetInfo()
		if err != nil {
			return err
		}
		details.State = libvirtStateToString(info.State)
		details.VCPU = fmt.Sprintf("%d", info.NrVirtCpu)
		details.MaxMemKiB = info.MaxMem
		details.UsedMemKiB = info.Memory
		details.CpuTimeSecs = float64(info.CpuTime) / 1e9
		return nil
	})
	return details, timedOut, err
}

// readDomainXMLViaLibvirt는 도메인 XML 원문을 읽는다.
// NIC, hostdev 같은 세부 장치 정보가 필요할 때 사용한다.
func readDomainXMLViaLibvirt(name string) (string, bool, error) {
	var xmlDesc string
	timedOut, err := withLibvirtDomainTimeout(name, func(dom *libvirt.Domain) error {
		desc, err := dom.GetXMLDesc(0)
		if err != nil {
			return err
		}
		xmlDesc = desc
		return nil
	})
	return xmlDesc, timedOut, err
}

// readDomainInterfaceAddrsViaLibvirt는 libvirt가 알고 있는 인터페이스 주소 정보를 읽는다.
// source 값에 따라 agent/lease/arp 기반 주소를 선택할 수 있다.
func readDomainInterfaceAddrsViaLibvirt(name string, source libvirt.DomainInterfaceAddressesSource) ([]libvirt.DomainInterface, bool, error) {
	var ifaces []libvirt.DomainInterface
	timedOut, err := withLibvirtDomainTimeout(name, func(dom *libvirt.Domain) error {
		addresses, err := dom.ListAllInterfaceAddresses(source)
		if err != nil {
			return err
		}
		ifaces = addresses
		return nil
	})
	return ifaces, timedOut, err
}

// ReadDomainGuestInfo는 guest agent를 통해 수집된 게스트 정보를 읽는다.
func ReadDomainGuestInfo(name string, types libvirt.DomainGuestInfoTypes) (*libvirt.DomainGuestInfo, bool, error) {
	var info *libvirt.DomainGuestInfo
	timedOut, err := withLibvirtDomainTimeout(name, func(dom *libvirt.Domain) error {
		out, err := dom.GetGuestInfo(types, 0)
		if err != nil {
			return err
		}
		info = out
		return nil
	})
	return info, timedOut, err
}

// withLibvirtDomainTimeout은 기본 타임아웃으로 도메인 단위 작업을 실행한다.
func withLibvirtDomainTimeout(name string, fn func(*libvirt.Domain) error) (bool, error) {
	return withLibvirtDomainTimeoutCustom(name, DefaultTimeout, fn)
}

// withLibvirtDomainTimeoutCustom은 지정한 타임아웃으로 도메인 단위 작업을 감싼다.
func withLibvirtDomainTimeoutCustom(name string, timeout time.Duration, fn func(*libvirt.Domain) error) (bool, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ch := make(chan error, 1)
	go func() {
		ch <- withLibvirtDomain(name, fn)
	}()

	select {
	case err := <-ch:
		return false, err
	case <-ctx.Done():
		return true, ctx.Err()
	}
}

// withLibvirtDomain은 연결 생성과 도메인 lookup/free를 공통 처리한다.
func withLibvirtDomain(name string, fn func(*libvirt.Domain) error) error {
	conn, err := libvirt.NewConnect(libvirtURI)
	if err != nil {
		return err
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return err
	}
	defer dom.Free()

	return fn(dom)
}

// libvirtStateToString은 libvirt 상태 값을 사람이 읽는 문자열로 변환한다.
func libvirtStateToString(state libvirt.DomainState) string {
	switch state {
	case libvirt.DOMAIN_NOSTATE:
		return "no state"
	case libvirt.DOMAIN_RUNNING:
		return "running"
	case libvirt.DOMAIN_BLOCKED:
		return "idle"
	case libvirt.DOMAIN_PAUSED:
		return "paused"
	case libvirt.DOMAIN_SHUTDOWN:
		return "shutdown"
	case libvirt.DOMAIN_SHUTOFF:
		return "shut off"
	case libvirt.DOMAIN_CRASHED:
		return "crashed"
	case libvirt.DOMAIN_PMSUSPENDED:
		return "pmsuspended"
	default:
		return "unknown"
	}
}

// domainXML은 libvirt 도메인 XML에서 devices 영역만 추출하기 위한 루트 구조체다.
type domainXML struct {
	Devices struct {
		Interfaces []domainInterface `xml:"interface"`
		Hostdevs   []domainHostdev   `xml:"hostdev"`
	} `xml:"devices"`
}

// domainInterface는 도메인 XML의 단일 interface 노드를 담는다.
type domainInterface struct {
	Type   string       `xml:"type,attr"`
	MAC    interfaceMAC `xml:"mac"`
	Source interfaceSrc `xml:"source"`
}

// interfaceMAC은 interface 노드의 MAC 주소 속성을 담는다.
type interfaceMAC struct {
	Address string `xml:"address,attr"`
}

// interfaceSrc는 interface가 연결된 bridge/network/dev 정보를 담는다.
type interfaceSrc struct {
	Bridge  string `xml:"bridge,attr"`
	Network string `xml:"network,attr"`
	Dev     string `xml:"dev,attr"`
}

// domainHostdev는 도메인 XML의 PCI passthrough hostdev 노드를 담는다.
type domainHostdev struct {
	Type    string            `xml:"type,attr"`
	Mode    string            `xml:"mode,attr"`
	Managed string            `xml:"managed,attr"`
	Source  domainHostdevSrc  `xml:"source"`
	Address domainHostdevAddr `xml:"address"`
}

// domainHostdevSrc는 hostdev source 내부의 PCI 주소를 담는다.
type domainHostdevSrc struct {
	Address domainHostdevAddr `xml:"address"`
}

// domainHostdevAddr는 domain/hostdev XML에 쓰이는 PCI address 조각을 담는다.
type domainHostdevAddr struct {
	Domain   string `xml:"domain,attr"`
	Bus      string `xml:"bus,attr"`
	Slot     string `xml:"slot,attr"`
	Function string `xml:"function,attr"`
}

// LoadInterfaceMapFromDomainXML은 MAC 주소 기준으로 NIC 타입과 source를 매핑한다.
// 예: MAC -> {bridge, bridge0}
func LoadInterfaceMapFromDomainXML(domain string) (map[string][2]string, bool, error) {
	xmlDesc, timedOut, err := readDomainXMLViaLibvirt(domain)
	if timedOut {
		return nil, true, err
	}
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(xmlDesc) == "" {
		return nil, false, fmt.Errorf("empty domain xml")
	}

	var doc domainXML
	if err := xml.Unmarshal([]byte(xmlDesc), &doc); err != nil {
		return nil, false, err
	}
	macMap := map[string][2]string{}
	for _, iface := range doc.Devices.Interfaces {
		mac := strings.ToLower(strings.TrimSpace(iface.MAC.Address))
		if mac == "" {
			continue
		}
		source := strings.TrimSpace(iface.Source.Bridge)
		if source == "" {
			source = strings.TrimSpace(iface.Source.Network)
		}
		if source == "" {
			source = strings.TrimSpace(iface.Source.Dev)
		}
		if source == "" {
			source = "N/A"
		}
		typeName := strings.TrimSpace(iface.Type)
		if typeName == "" {
			typeName = "N/A"
		}
		macMap[mac] = [2]string{typeName, source}
	}
	return macMap, false, nil
}

// LoadHostdevSourcesFromDomainXML은 XML에 정의된 PCI hostdev source 목록을 반환한다.
// passthrough NIC 여부 판단에 사용한다.
func LoadHostdevSourcesFromDomainXML(domain string) ([]string, bool, error) {
	xmlDesc, timedOut, err := readDomainXMLViaLibvirt(domain)
	if timedOut {
		return nil, true, err
	}
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(xmlDesc) == "" {
		return nil, false, fmt.Errorf("empty domain xml")
	}

	var doc domainXML
	if err := xml.Unmarshal([]byte(xmlDesc), &doc); err != nil {
		return nil, false, err
	}
	out := make([]string, 0)
	seen := map[string]struct{}{}
	for _, hostdev := range doc.Devices.Hostdevs {
		if !strings.EqualFold(strings.TrimSpace(hostdev.Type), "pci") {
			continue
		}
		addr := hostdev.Source.Address
		if addr.Domain == "" && addr.Bus == "" && addr.Slot == "" && addr.Function == "" {
			addr = hostdev.Address
		}
		source := formatPCIAddress(addr)
		if source == "" {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		out = append(out, source)
	}
	return out, false, nil
}

// formatPCIAddress는 XML address 조각을 사람이 읽는 PCI 주소 문자열로 변환한다.
func formatPCIAddress(addr domainHostdevAddr) string {
	if addr.Domain == "" && addr.Bus == "" && addr.Slot == "" && addr.Function == "" {
		return ""
	}
	domain, okDomain := parseHexPart(addr.Domain, 4)
	bus, okBus := parseHexPart(addr.Bus, 2)
	slot, okSlot := parseHexPart(addr.Slot, 2)
	function, okFunc := parseHexPart(addr.Function, 1)
	if !okDomain && !okBus && !okSlot && !okFunc {
		return ""
	}
	if !okDomain {
		domain = strings.TrimPrefix(strings.TrimSpace(addr.Domain), "0x")
	}
	if !okBus {
		bus = strings.TrimPrefix(strings.TrimSpace(addr.Bus), "0x")
	}
	if !okSlot {
		slot = strings.TrimPrefix(strings.TrimSpace(addr.Slot), "0x")
	}
	if !okFunc {
		function = strings.TrimPrefix(strings.TrimSpace(addr.Function), "0x")
	}
	return fmt.Sprintf("pci %s:%s:%s.%s", domain, bus, slot, function)
}

// parseHexPart는 XML의 16진수 값을 지정한 폭으로 정규화한다.
func parseHexPart(value string, width int) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	value = strings.TrimPrefix(value, "0x")
	parsed, err := strconv.ParseUint(value, 16, 64)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("%0*x", width, parsed), true
}

// LoadDomifaddrMaps는 IPv4 기준으로 IP->MAC, IP->CIDR 맵을 만든다.
func LoadDomifaddrMaps(domain string, source libvirt.DomainInterfaceAddressesSource) (map[string]string, map[string]string, bool, error) {
	ifaces, timedOut, err := readDomainInterfaceAddrsViaLibvirt(domain, source)
	if timedOut {
		return nil, nil, true, err
	}
	if err != nil {
		return nil, nil, false, err
	}
	ipToMac := map[string]string{}
	ipToAddr := map[string]string{}
	for _, iface := range ifaces {
		mac := strings.ToLower(strings.TrimSpace(iface.Hwaddr))
		for _, addr := range iface.Addrs {
			if addr.Type != libvirt.IP_ADDR_TYPE_IPV4 {
				continue
			}
			ip := strings.TrimSpace(addr.Addr)
			if ip == "" {
				continue
			}
			if mac != "" {
				ipToMac[ip] = mac
			}
			addrStr := ip
			if addr.Prefix > 0 {
				addrStr = fmt.Sprintf("%s/%d", ip, addr.Prefix)
			}
			ipToAddr[ip] = addrStr
		}
	}
	return ipToMac, ipToAddr, false, nil
}

// IsSocketAvailable은 현재 노드에 libvirt 소켓이 존재하는지 확인한다.
// host가 아닌 게스트 노드에서는 빠르게 fallback 경로로 보내기 위해 사용한다.
func IsSocketAvailable() bool {
	paths := []string{
		"/var/run/libvirt/libvirt-sock",
		"/run/libvirt/libvirt-sock",
		"/var/run/libvirt/libvirt-sock-ro",
		"/run/libvirt/libvirt-sock-ro",
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// decodeGuestBase64는 guest agent가 base64로 반환한 문자열을 복원한다.
func decodeGuestBase64(data string) string {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return strings.TrimSpace(data)
	}
	return strings.TrimSpace(string(decoded))
}

// secondsFromDuration은 guest agent timeout 인자에 맞게 초 단위로 변환한다.
func secondsFromDuration(timeout time.Duration) int {
	sec := int(timeout.Seconds())
	if sec <= 0 {
		return int(DefaultTimeout.Seconds())
	}
	return sec
}

// GuestAgentCommandRequest는 일반 guest agent RPC 호출용 공통 request 모델이다.
type GuestAgentCommandRequest struct {
	Execute   string `json:"execute"`
	Arguments any    `json:"arguments,omitempty"`
}

// RunGuestAgentCommand는 guest agent RPC를 raw JSON 응답 문자열로 수행한다.
func RunGuestAgentCommand(domain string, request any, timeout time.Duration) (string, bool, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return "", false, err
	}
	var output string
	timedOut, err := withLibvirtDomainTimeoutCustom(domain, timeout, func(dom *libvirt.Domain) error {
		qgaTimeout := libvirt.DomainQemuAgentCommandTimeout(secondsFromDuration(timeout))
		resp, err := dom.QemuAgentCommand(string(payload), qgaTimeout, 0)
		if err != nil {
			return err
		}
		output = resp
		return nil
	})
	if !timedOut && err != nil && errors.Is(err, context.DeadlineExceeded) {
		timedOut = true
	}
	return output, timedOut, err
}

// guestRoutesResponse는 guest-network-get-route 응답 모델이다.
type guestRoutesResponse struct {
	Return []map[string]any `json:"return"`
}

// ReadGuestRoutes는 guest agent 라우팅 테이블을 읽는다.
func ReadGuestRoutes(domain string, timeout time.Duration) ([]map[string]any, bool, error) {
	req := GuestAgentCommandRequest{Execute: "guest-network-get-route"}
	resp, timedOut, err := RunGuestAgentCommand(domain, req, timeout)
	if timedOut {
		return nil, true, err
	}
	if err != nil {
		return nil, false, err
	}
	var out guestRoutesResponse
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		return nil, false, err
	}
	return out.Return, false, nil
}

// guestNetworkInterfacesResponse는 guest-network-get-interfaces RPC의 최상위 응답 구조다.
type guestNetworkInterfacesResponse struct {
	Return []guestNetworkInterface `json:"return"`
}

// guestNetworkInterface는 게스트 내부 단일 네트워크 인터페이스 정보를 담는다.
type guestNetworkInterface struct {
	Name            string           `json:"name"`
	HardwareAddress string           `json:"hardware-address"`
	IPAddresses     []guestIPAddress `json:"ip-addresses"`
}

// guestIPAddress는 guest agent가 반환한 개별 IP 주소 정보를 담는다.
type guestIPAddress struct {
	IPAddress     string `json:"ip-address"`
	Prefix        uint   `json:"prefix"`
	IPAddressType string `json:"ip-address-type"`
}

// readGuestNetworkInterfaces는 guest agent 네트워크 인터페이스 목록을 읽는다.
func readGuestNetworkInterfaces(domain string, timeout time.Duration) ([]guestNetworkInterface, bool, error) {
	req := GuestAgentCommandRequest{Execute: "guest-network-get-interfaces"}
	resp, timedOut, err := RunGuestAgentCommand(domain, req, timeout)
	if timedOut {
		return nil, true, err
	}
	if err != nil {
		return nil, false, err
	}
	var out guestNetworkInterfacesResponse
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		return nil, false, err
	}
	return out.Return, false, nil
}

// LoadDomifaddrMapsFromGuestAgent는 guest agent 인터페이스 응답을 IP 기준 맵으로 변환한다.
func LoadDomifaddrMapsFromGuestAgent(domain string, timeout time.Duration) (map[string]string, map[string]string, bool, error) {
	ifaces, timedOut, err := readGuestNetworkInterfaces(domain, timeout)
	if timedOut {
		return nil, nil, true, err
	}
	if err != nil {
		return nil, nil, false, err
	}
	ipToMac := map[string]string{}
	ipToAddr := map[string]string{}
	for _, iface := range ifaces {
		mac := strings.ToLower(strings.TrimSpace(iface.HardwareAddress))
		for _, addr := range iface.IPAddresses {
			if !strings.EqualFold(addr.IPAddressType, "ipv4") {
				continue
			}
			ip := strings.TrimSpace(addr.IPAddress)
			if ip == "" {
				continue
			}
			if mac != "" {
				ipToMac[ip] = mac
			}
			addrStr := ip
			if addr.Prefix > 0 {
				addrStr = fmt.Sprintf("%s/%d", ip, addr.Prefix)
			}
			ipToAddr[ip] = addrStr
		}
	}
	return ipToMac, ipToAddr, false, nil
}

// PickDefaultGateway는 guest route 목록에서 기본 게이트웨이를 추출한다.
func PickDefaultGateway(routes []map[string]any) string {
	if len(routes) == 0 {
		return ""
	}
	for _, route := range routes {
		dest := strings.ToLower(strings.TrimSpace(getMapString(route, "destination", "dest")))
		gw := strings.TrimSpace(getMapString(route, "gateway"))
		if gw == "" {
			continue
		}
		if dest == "" || dest == "default" || dest == "0.0.0.0" || strings.HasPrefix(dest, "0.0.0.0/") {
			return gw
		}
	}
	for _, route := range routes {
		if gw := strings.TrimSpace(getMapString(route, "gateway")); gw != "" {
			return gw
		}
	}
	return ""
}

// getMapString은 맵에서 여러 후보 키를 순서대로 조회해 문자열 값을 꺼낸다.
func getMapString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			switch v := value.(type) {
			case string:
				return v
			case fmt.Stringer:
				return v.String()
			}
		}
	}
	return ""
}

// guestFileOpenResponse는 guest-file-open RPC의 파일 핸들 반환값을 담는다.
type guestFileOpenResponse struct {
	Return int `json:"return"`
}

// guestFileReadResponse는 guest-file-read RPC의 읽기 결과를 담는 최상위 구조다.
type guestFileReadResponse struct {
	Return guestFileReadReturn `json:"return"`
}

// guestFileReadReturn은 guest-file-read가 반환한 청크 데이터와 EOF 상태를 담는다.
type guestFileReadReturn struct {
	Count  int    `json:"count"`
	EOF    bool   `json:"eof"`
	BufB64 string `json:"buf-b64"`
}

// ReadGuestFile은 guest agent 파일 API를 사용해 게스트 내부 파일을 읽는다.
// 큰 파일을 모두 읽지 않도록 길이 제한을 두고 chunk 단위로 읽는다.
func ReadGuestFile(domain string, path string, timeout time.Duration) (string, bool, error) {
	handle, timedOut, err := guestFileOpen(domain, path, timeout)
	if timedOut {
		return "", true, err
	}
	if err != nil {
		return "", false, err
	}
	defer guestFileClose(domain, handle, timeout)

	var builder strings.Builder
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return builder.String(), true, context.DeadlineExceeded
		}
		chunk, eof, timedOut, err := guestFileRead(domain, handle, timeout)
		if timedOut {
			return builder.String(), true, err
		}
		if err != nil {
			return builder.String(), false, err
		}
		if chunk != "" {
			builder.WriteString(chunk)
		}
		if eof {
			break
		}
		if builder.Len() > 65536 {
			break
		}
	}
	return strings.TrimSpace(builder.String()), false, nil
}

// guestFileOpen은 읽기 모드로 게스트 파일 핸들을 연다.
func guestFileOpen(domain string, path string, timeout time.Duration) (int, bool, error) {
	req := GuestAgentCommandRequest{
		Execute: "guest-file-open",
		Arguments: map[string]any{
			"path": path,
			"mode": "r",
		},
	}
	resp, timedOut, err := RunGuestAgentCommand(domain, req, timeout)
	if timedOut {
		return 0, true, err
	}
	if err != nil {
		return 0, false, err
	}
	var out guestFileOpenResponse
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		return 0, false, err
	}
	if out.Return == 0 {
		return 0, false, fmt.Errorf("guest-file-open returned empty handle")
	}
	return out.Return, false, nil
}

// guestFileRead는 열린 파일 핸들에서 한 번의 chunk를 읽는다.
func guestFileRead(domain string, handle int, timeout time.Duration) (string, bool, bool, error) {
	req := GuestAgentCommandRequest{
		Execute: "guest-file-read",
		Arguments: map[string]any{
			"handle": handle,
		},
	}
	resp, timedOut, err := RunGuestAgentCommand(domain, req, timeout)
	if timedOut {
		return "", false, true, err
	}
	if err != nil {
		return "", false, false, err
	}
	var out guestFileReadResponse
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		return "", false, false, err
	}
	data := ""
	if out.Return.BufB64 != "" {
		data = decodeGuestBase64(out.Return.BufB64)
	}
	return data, out.Return.EOF, false, nil
}

// guestFileClose는 guest agent 파일 핸들을 정리한다.
func guestFileClose(domain string, handle int, timeout time.Duration) {
	req := GuestAgentCommandRequest{
		Execute: "guest-file-close",
		Arguments: map[string]any{
			"handle": handle,
		},
	}
	_, _, _ = RunGuestAgentCommand(domain, req, timeout)
}
