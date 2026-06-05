package cube

import (
	"crypto/rand"
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type SCVMXMLCreateRequest = CubeModel.SCVMXMLCreateRequest
type SCVMXMLCreateResponse = CubeModel.SCVMXMLCreateResponse

const (
	scvmXMLCreateCommandTimeout = 30 * time.Second
	scvmXMLCreateSuccessMessage = "scvm xml create success"

	scvmXMLTemplateName         = "scvm-xml-template.xml"
	scvmXMLFileName             = "scvm.xml"
	scvmXMLTempFileName         = "scvm-temp.xml"
	scvmXMLLimitsTemplateName   = "limits-template.conf"
	scvmXMLSysctlTemplateName   = "sysctl-template.conf"
	scvmXMLLimitsTargetPath     = "/etc/security/limits.conf"
	scvmXMLSysctlTargetPath     = "/etc/sysctl.conf"
	scvmXMLCloudInitISOPath     = "/var/lib/libvirt/images/scvm-cloudinit.iso"
	scvmXMLHugepageMemoryFactor = 1024
)

type scvmXMLSlotAllocator struct {
	next int
}

type scvmXMLPCIDevice struct {
	Domain   string
	Bus      string
	Slot     string
	Function string
}

// CreateSCVMXML godoc
//
//	@Summary		Create SCVM XML
//	@Description	Storage Center VM XML과 hugepage 설정을 생성합니다.
//	@Tags			Cube-SCVM
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.SCVMXMLCreateRequest	true	"scvm xml create request"
//	@Success		200	{object}	CubeModel.SCVMXMLCreateResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/scvm/xml [post]
func CreateSCVMXML(context *gin.Context) {
	var req SCVMXMLCreateRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}
	if err := normalizeSCVMXMLCreateRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	resp := runCreateSCVMXML(req)
	context.JSON(statusCodeFromSCVMXMLCreateResponse(resp), resp)
}

func normalizeSCVMXMLCreateRequest(req *SCVMXMLCreateRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	if req.CPU <= 0 {
		return fmt.Errorf("cpu required")
	}
	if req.Memory <= 0 {
		return fmt.Errorf("memory required")
	}

	req.DiskType = strings.ToLower(strings.TrimSpace(req.DiskType))
	switch req.DiskType {
	case "raid_passthrough":
		req.RaidPassthroughList = trimStringList(req.RaidPassthroughList)
		if len(req.RaidPassthroughList) == 0 {
			return fmt.Errorf("raid_passthrough_list required")
		}
	case "lun_passthrough":
		req.LunPassthroughList = trimStringList(req.LunPassthroughList)
		if len(req.LunPassthroughList) == 0 {
			return fmt.Errorf("lun_passthrough_list required")
		}
	case "disk_passthrough":
		req.DiskPassthroughList = trimStringList(req.DiskPassthroughList)
		if len(req.DiskPassthroughList) == 0 {
			return fmt.Errorf("disk_passthrough_list required")
		}
	default:
		return fmt.Errorf("disk_type must be raid_passthrough, lun_passthrough or disk_passthrough")
	}

	req.ManagementNetworkBridge = strings.TrimSpace(req.ManagementNetworkBridge)
	if req.ManagementNetworkBridge == "" {
		return fmt.Errorf("management_network_bridge required")
	}

	req.StorageTrafficNetworkType = strings.ToLower(strings.TrimSpace(req.StorageTrafficNetworkType))
	switch req.StorageTrafficNetworkType {
	case "nic_passthrough":
		req.ServerNicPassthrough = strings.TrimSpace(req.ServerNicPassthrough)
		req.ReplicationNicPassthrough = strings.TrimSpace(req.ReplicationNicPassthrough)
		if req.ServerNicPassthrough == "" || req.ReplicationNicPassthrough == "" {
			return fmt.Errorf("server_nic_passthrough and replication_nic_passthrough required")
		}
	case "nic_passthrough_bonding":
		req.ServerNicPassthroughBondingList = trimStringList(req.ServerNicPassthroughBondingList)
		req.ReplicationNicPassthroughBondingList = trimStringList(req.ReplicationNicPassthroughBondingList)
		if len(req.ServerNicPassthroughBondingList) != 2 || len(req.ReplicationNicPassthroughBondingList) != 2 {
			return fmt.Errorf("server_nic_passthrough_bonding_list and replication_nic_passthrough_bonding_list must have 2 values")
		}
	case "bridge":
		req.ServerNetworkBridge = strings.TrimSpace(req.ServerNetworkBridge)
		req.ReplicationNetworkBridge = strings.TrimSpace(req.ReplicationNetworkBridge)
		if req.ServerNetworkBridge == "" || req.ReplicationNetworkBridge == "" {
			return fmt.Errorf("server_network_bridge and replication_network_bridge required")
		}
	default:
		return fmt.Errorf("storage_traffic_network_type must be nic_passthrough, nic_passthrough_bonding or bridge")
	}

	return nil
}

func runCreateSCVMXML(req SCVMXMLCreateRequest) SCVMXMLCreateResponse {
	if err := createSCVMHugePageConfig(req.Memory); err != nil {
		return scvmXMLCreateError(err.Error())
	}
	xmlPath, err := createSCVMXML(req)
	if err != nil {
		return scvmXMLCreateError(err.Error())
	}
	return SCVMXMLCreateResponse{
		Code:    http.StatusOK,
		Val:     map[string]any{},
		Message: scvmXMLCreateSuccessMessage,
		XMLPath: xmlPath,
	}
}

func createSCVMXML(req SCVMXMLCreateRequest) (string, error) {
	templatePath := filepath.Join(resolveAbleStackXMLTemplatePath(), scvmXMLTemplateName)
	vmConfigDir := resolveAbleStackVMConfigDir("scvm")
	tempPath := filepath.Join(vmConfigDir, scvmXMLTempFileName)
	xmlPath := filepath.Join(vmConfigDir, scvmXMLFileName)

	raw, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("read scvm xml template: %w", err)
	}
	if err := os.MkdirAll(vmConfigDir, 0755); err != nil {
		return "", err
	}

	content, err := renderSCVMXMLTemplate(string(raw), req, isSCVMXMLOpenVSwitchActive())
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(tempPath, []byte(content), 0644); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, xmlPath); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}
	_ = os.Remove(tempPath + ".bak")
	if err := chmodRecursive(vmConfigDir, 0755); err != nil {
		return "", err
	}
	return xmlPath, nil
}

func renderSCVMXMLTemplate(template string, req SCVMXMLCreateRequest, activeOpenVSwitch bool) (string, error) {
	slots := &scvmXMLSlotAllocator{next: 20}
	hostDevNum := 0
	bridgeNum := 0

	var out strings.Builder
	for _, line := range strings.SplitAfter(template, "\n") {
		var err error
		switch {
		case strings.Contains(line, "<!--memory-->"):
			line = strings.ReplaceAll(line, "<!--memory-->", strconv.Itoa(req.Memory))
		case strings.Contains(line, "<!--cpu-->"):
			line = strings.ReplaceAll(line, "<!--cpu-->", strconv.Itoa(req.CPU))
		case strings.Contains(line, "<!--scvm_cloudinit-->"):
			line = strings.ReplaceAll(line, "<!--scvm_cloudinit-->", scvmXMLCloudInitDisk())
		case strings.Contains(line, "<!--lun_passthrough-->"):
			if req.DiskType != "lun_passthrough" && req.DiskType != "disk_passthrough" {
				line = ""
				break
			}
			var block string
			if req.DiskType == "disk_passthrough" {
				block, err = scvmXMLBlockPassthrough(req.DiskPassthroughList, "disk", "disk_passthrough_list")
			} else {
				block, err = scvmXMLBlockPassthrough(req.LunPassthroughList, "lun", "lun_passthrough_list")
			}
			line = strings.ReplaceAll(line, "<!--lun_passthrough-->", block)
		case strings.Contains(line, "<!--management_network_bridge-->"):
			block := scvmXMLBridgeInterface(req.ManagementNetworkBridge, bridgeNum, slots, activeOpenVSwitch, true)
			bridgeNum++
			line = strings.ReplaceAll(line, "<!--management_network_bridge-->", block)
		case strings.Contains(line, "<!--server_network_bridge-->"):
			if req.StorageTrafficNetworkType != "bridge" {
				line = ""
				break
			}
			block := scvmXMLBridgeInterface(req.ServerNetworkBridge, bridgeNum, slots, activeOpenVSwitch, false)
			bridgeNum++
			line = strings.ReplaceAll(line, "<!--server_network_bridge-->", block)
		case strings.Contains(line, "<!--replication_network_bridge-->"):
			if req.StorageTrafficNetworkType != "bridge" {
				line = ""
				break
			}
			block := scvmXMLBridgeInterface(req.ReplicationNetworkBridge, bridgeNum, slots, activeOpenVSwitch, false)
			bridgeNum++
			line = strings.ReplaceAll(line, "<!--replication_network_bridge-->", block)
		case strings.Contains(line, "<!--raid_passthrough-->"):
			if req.DiskType != "raid_passthrough" {
				line = ""
				break
			}
			var block string
			block, err = scvmXMLRaidPassthrough(req.RaidPassthroughList, slots, &hostDevNum)
			line = strings.ReplaceAll(line, "<!--raid_passthrough-->", block)
		case strings.Contains(line, "<!--nic_passthrough-->"):
			values := scvmXMLNICPassthroughValues(req)
			if len(values) == 0 {
				line = ""
				break
			}
			var block string
			block, err = scvmXMLNICPassthrough(values, slots, &hostDevNum)
			line = strings.ReplaceAll(line, "<!--nic_passthrough-->", block)
		}
		if err != nil {
			return "", err
		}
		out.WriteString(line)
	}
	return out.String(), nil
}

func scvmXMLCloudInitDisk() string {
	return strings.Join([]string{
		"    <disk type='file' device='cdrom'>",
		"      <driver name='qemu' type='raw'/>",
		"      <source file='" + xmlAttr(scvmXMLCloudInitISOPath) + "'/>",
		"      <target dev='sdz' bus='sata'/>",
		"      <readonly/>",
		"      <shareable/>",
		"      <address type='drive' controller='0' bus='0' target='0' unit='0'/>",
		"    </disk>",
	}, "\n")
}

func scvmXMLBlockPassthrough(devices []string, deviceType string, fieldName string) (string, error) {
	if len(devices) > 26 {
		return "", fmt.Errorf("%s supports up to 26 values", fieldName)
	}
	blocks := make([]string, 0, len(devices))
	for i, device := range devices {
		dev := fmt.Sprintf("sd%c", 'a'+rune(i))
		unit := strconv.Itoa(i)
		blocks = append(blocks, strings.Join([]string{
			"    <disk type='block' device='" + xmlAttr(deviceType) + "'>",
			"      <driver name='qemu' type='raw'/>",
			"      <source dev='" + xmlAttr(device) + "'/>",
			"      <target dev='" + dev + "' bus='scsi'/>",
			"      <alias name='scsi0-0-0-" + unit + "'/>",
			"      <address type='drive' controller='0' bus='0' target='0' unit='" + unit + "'/>",
			"    </disk>",
		}, "\n"))
	}
	return strings.Join(blocks, "\n"), nil
}

func scvmXMLRaidPassthrough(values []string, slots *scvmXMLSlotAllocator, hostDevNum *int) (string, error) {
	blocks := make([]string, 0, len(values))
	for _, value := range values {
		dev, err := parseSCVMXMLShortPCI(value)
		if err != nil {
			return "", err
		}
		blocks = append(blocks, scvmXMLHostdev(dev, *hostDevNum, slots.Next()))
		*hostDevNum++
	}
	return strings.Join(blocks, "\n"), nil
}

func scvmXMLNICPassthrough(values []string, slots *scvmXMLSlotAllocator, hostDevNum *int) (string, error) {
	blocks := make([]string, 0, len(values))
	for _, value := range values {
		dev, err := parseSCVMXMLFullPCI(value)
		if err != nil {
			return "", err
		}
		blocks = append(blocks, scvmXMLHostdev(dev, *hostDevNum, slots.Next()))
		*hostDevNum++
	}
	return strings.Join(blocks, "\n"), nil
}

func scvmXMLNICPassthroughValues(req SCVMXMLCreateRequest) []string {
	switch req.StorageTrafficNetworkType {
	case "nic_passthrough":
		return []string{req.ServerNicPassthrough, req.ReplicationNicPassthrough}
	case "nic_passthrough_bonding":
		values := make([]string, 0, 4)
		for i := 0; i < 2; i++ {
			values = append(values, req.ServerNicPassthroughBondingList[i], req.ReplicationNicPassthroughBondingList[i])
		}
		return values
	default:
		return nil
	}
}

func scvmXMLHostdev(dev scvmXMLPCIDevice, alias int, slot string) string {
	return strings.Join([]string{
		"    <hostdev mode='subsystem' type='pci' managed='yes'>",
		"      <driver name='vfio'/>",
		"      <source>",
		"        <address domain='0x" + xmlAttr(dev.Domain) + "' bus='0x" + xmlAttr(dev.Bus) + "' slot='0x" + xmlAttr(dev.Slot) + "' function='0x" + xmlAttr(dev.Function) + "'/>",
		"      </source>",
		"      <alias name='hostdev" + strconv.Itoa(alias) + "'/>",
		"      <address type='pci' domain='0x0000' bus='0x00' slot='" + slot + "' function='0x0'/>",
		"    </hostdev>",
	}, "\n")
}

func scvmXMLBridgeInterface(bridge string, netIndex int, slots *scvmXMLSlotAllocator, activeOpenVSwitch bool, addOpenVSwitchVirtualPort bool) string {
	lines := []string{
		"    <interface type='bridge'>",
	}
	if !addOpenVSwitchVirtualPort && !activeOpenVSwitch {
		lines = append(lines, "      <filterref filter='allow-all-traffic'/>")
	}
	lines = append(lines,
		"      <mac address='"+generateSCVMXMLMacAddress()+"'/>",
		"      <source bridge='"+xmlAttr(bridge)+"'/>",
		"      <target dev='vnet"+strconv.Itoa(netIndex)+"'/>",
		"      <model type='virtio'/>",
		"      <alias name='net"+strconv.Itoa(netIndex)+"'/>",
	)
	if addOpenVSwitchVirtualPort {
		if activeOpenVSwitch {
			lines = append(lines, "      <virtualport type='openvswitch' />")
		} else {
			lines = append(lines, "      <filterref filter='allow-all-traffic'/>")
		}
	}
	lines = append(lines,
		"      <address type='pci' domain='0x0000' bus='0x00' slot='"+slots.Next()+"' function='0x0'/>",
		"    </interface>",
	)
	return strings.Join(lines, "\n")
}

func createSCVMHugePageConfig(memoryGiB int) error {
	vmConfigDir := resolveAbleStackVMConfigDir("scvm")
	if err := os.MkdirAll(vmConfigDir, 0755); err != nil {
		return err
	}
	if err := renderSCVMConfigTemplate(
		filepath.Join(resolveAbleStackXMLTemplatePath(), scvmXMLLimitsTemplateName),
		filepath.Join(vmConfigDir, scvmXMLLimitsTemplateName),
		scvmXMLLimitsTargetPath,
		strconv.Itoa(memoryGiB*scvmXMLHugepageMemoryFactor*scvmXMLHugepageMemoryFactor),
	); err != nil {
		return err
	}
	if err := renderSCVMConfigTemplate(
		filepath.Join(resolveAbleStackXMLTemplatePath(), scvmXMLSysctlTemplateName),
		filepath.Join(vmConfigDir, scvmXMLSysctlTemplateName),
		scvmXMLSysctlTargetPath,
		strconv.Itoa(memoryGiB*scvmXMLHugepageMemoryFactor),
	); err != nil {
		return err
	}

	if _, err := runSCVMXMLCommand("/usr/sbin/sysctl", "-p"); err != nil {
		return err
	}
	_, err := runSCVMXMLCommand("/usr/sbin/sysctl", "-a")
	return err
}

func renderSCVMConfigTemplate(templatePath string, tempPath string, targetPath string, memoryValue string) error {
	raw, err := os.ReadFile(templatePath)
	if err != nil {
		return err
	}
	content := strings.ReplaceAll(string(raw), "{memory}", memoryValue)
	if err := os.WriteFile(tempPath, []byte(content), 0644); err != nil {
		return err
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func parseSCVMXMLShortPCI(value string) (scvmXMLPCIDevice, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return scvmXMLPCIDevice{}, fmt.Errorf("invalid pci address: %s", value)
	}
	slotParts := strings.Split(parts[1], ".")
	if len(slotParts) != 2 {
		return scvmXMLPCIDevice{}, fmt.Errorf("invalid pci address: %s", value)
	}
	return scvmXMLPCIDevice{
		Domain:   "0000",
		Bus:      normalizeHexComponent(parts[0]),
		Slot:     normalizeHexComponent(slotParts[0]),
		Function: normalizeHexComponent(slotParts[1]),
	}, nil
}

func parseSCVMXMLFullPCI(value string) (scvmXMLPCIDevice, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return scvmXMLPCIDevice{}, fmt.Errorf("invalid pci address: %s", value)
	}
	slotParts := strings.Split(parts[2], ".")
	if len(slotParts) != 2 {
		return scvmXMLPCIDevice{}, fmt.Errorf("invalid pci address: %s", value)
	}
	return scvmXMLPCIDevice{
		Domain:   normalizeHexComponent(parts[0]),
		Bus:      normalizeHexComponent(parts[1]),
		Slot:     normalizeHexComponent(slotParts[0]),
		Function: normalizeHexComponent(slotParts[1]),
	}, nil
}

func normalizeHexComponent(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
}

func (a *scvmXMLSlotAllocator) Next() string {
	if a.next == 0 {
		a.next = 20
	}
	slot := fmt.Sprintf("0x%x", a.next)
	a.next++
	return slot
}

func isSCVMXMLOpenVSwitchActive() bool {
	_, err := runSCVMXMLCommand("systemctl", "is-active", "openvswitch")
	return err == nil
}

func runSCVMXMLCommand(command string, args ...string) (string, error) {
	out, timedOut, err := runCommandOutputWithEnv(command, scvmXMLCreateCommandTimeout, scvmUpdateCommandEnv(), args...)
	if timedOut {
		return out, fmt.Errorf("%s %s timed out after %s", command, strings.Join(args, " "), scvmXMLCreateCommandTimeout)
	}
	if err != nil {
		return out, fmt.Errorf("%s %s failed: %s", command, strings.Join(args, " "), firstNonEmpty(strings.TrimSpace(out), err.Error()))
	}
	return out, nil
}

func generateSCVMXMLMacAddress() string {
	buf := []byte{0x00, 0x24, 0x81, 0x00, 0x00, 0x00}
	random := make([]byte, 3)
	if _, err := rand.Read(random); err == nil {
		buf[3] = random[0] & 0x7f
		buf[4] = random[1]
		buf[5] = random[2]
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])
}

func xmlAttr(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return strings.NewReplacer("'", "&apos;", "\"", "&quot;").Replace(b.String())
}

func trimStringList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func chmodRecursive(root string, mode os.FileMode) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return os.Chmod(path, mode)
	})
}

func statusCodeFromSCVMXMLCreateResponse(resp SCVMXMLCreateResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func scvmXMLCreateError(message string) SCVMXMLCreateResponse {
	return SCVMXMLCreateResponse{
		Code:    http.StatusInternalServerError,
		Val:     map[string]any{},
		Message: message,
	}
}
