package glueservice

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultNVMeOfCLIImage     = "localhost:15000/glue/nvmeof-cli:Diplo"
	defaultNVMeOfServerPort   = "5500"
	defaultNVMeOfTgtExtraArgs = "--cpumask=0xFF"
	envNVMeOfCLIImage         = "ABLESTACK_GLUE_NVME_OF_CLI_IMAGE"
	envNVMeOfServerAddress    = "ABLESTACK_GLUE_NVME_OF_SERVER_ADDRESS"
	envNVMeOfServerPort       = "ABLESTACK_GLUE_NVME_OF_SERVER_PORT"
	nvmeOfSPDKRPCPath         = "/usr/libexec/spdk/scripts/rpc.py"
)

type nvmeOfDaemon struct {
	Hostname   string
	DaemonName string
}

type nvmeOfServer struct {
	Address string
	Port    string
	Daemons []nvmeOfDaemon
}

type nvmeOfServiceSpec struct {
	ServiceType string                 `yaml:"service_type"`
	ServiceID   string                 `yaml:"service_id"`
	Placement   nvmeOfServicePlacement `yaml:"placement"`
	Spec        nvmeOfServiceSpecBody  `yaml:"spec"`
}

type nvmeOfServicePlacement struct {
	Hosts []string `yaml:"hosts"`
}

type nvmeOfServiceSpecBody struct {
	Pool            string `yaml:"pool"`
	TgtCmdExtraArgs string `yaml:"tgt_cmd_extra_args"`
}

// NVMeOfServiceCreate는 pool 초기화 후 ceph orch spec으로 NVMe-oF service를 생성한다.
func NVMeOfServiceCreate(ctx context.Context, poolName string, hosts []string, tgtCmdExtraArgs string) (map[string]any, error) {
	poolName = strings.TrimSpace(poolName)
	if err := ValidatePoolName(poolName); err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("hosts is required")
	}
	for _, host := range hosts {
		if err := ValidateCephName("host", host); err != nil {
			return nil, err
		}
	}
	tgtCmdExtraArgs = strings.TrimSpace(tgtCmdExtraArgs)
	if tgtCmdExtraArgs == "" {
		tgtCmdExtraArgs = defaultNVMeOfTgtExtraArgs
	}
	if strings.ContainsAny(tgtCmdExtraArgs, "\r\n") {
		return nil, fmt.Errorf("invalid tgt_cmd_extra_args")
	}

	spec := nvmeOfServiceSpec{
		ServiceType: "nvmeof",
		ServiceID:   poolName,
		Placement: nvmeOfServicePlacement{
			Hosts: hosts,
		},
		Spec: nvmeOfServiceSpecBody{
			Pool:            poolName,
			TgtCmdExtraArgs: tgtCmdExtraArgs,
		},
	}
	specPath, err := writeNVMeOfServiceSpec(spec)
	if err != nil {
		return nil, err
	}
	defer os.Remove(specPath)

	if _, err := run(ctx, "ceph", "osd", "pool", "create", poolName); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "rbd", "pool", "init", poolName); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "ceph", "osd", "pool", "set", poolName, "size", "2"); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "ceph", "orch", "apply", "-i", specPath); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":             "success",
		"pool":               poolName,
		"hosts":              hosts,
		"tgt_cmd_extra_args": tgtCmdExtraArgs,
	}, nil
}

// NVMeOfImageDownload는 SCVM 로컬 podman에 NVMe-oF CLI image를 pull한다.
func NVMeOfImageDownload(ctx context.Context, image string) (map[string]any, error) {
	image = strings.TrimSpace(image)
	if image == "" {
		image = nvmeOfCLIImage()
	}
	if err := ValidateContainerImage(image); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "podman", "pull", image); err != nil {
		return nil, err
	}
	return map[string]any{
		"status": "success",
		"image":  image,
	}, nil
}

// NVMeOfTargetList는 로컬 NVMe-oF daemon container의 SPDK RPC로 target 정보를 조회한다.
func NVMeOfTargetList(ctx context.Context, subsystemNQNID string) (any, error) {
	subsystemNQNID = strings.TrimSpace(subsystemNQNID)
	if err := ValidateOptionalNQN(subsystemNQNID); err != nil {
		return nil, err
	}

	server, ok, err := resolveNVMeOfServer(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []any{}, nil
	}

	containerID, err := nvmeOfContainerID(ctx)
	if err != nil {
		return nil, err
	}
	if containerID == "" {
		return []any{}, nil
	}

	args := []string{"exec", "-i", containerID, "python3", nvmeOfSPDKRPCPath, "nvmf_get_subsystems"}
	if subsystemNQNID != "" {
		args = append(args, subsystemNQNID)
	}
	raw, err := runJSON(ctx, "podman", args...)
	if err != nil {
		return nil, err
	}
	enrichNVMeOfTargetList(ctx, server, containerID, raw)
	return raw, nil
}

// NVMeOfTargetCreate는 subsystem/listener/host/namespace를 한 번에 구성한다.
func NVMeOfTargetCreate(ctx context.Context, gatewayIP string, gatewayName string, subsystemNQNID string, poolName string, imageName string, sizeGiB int64) (map[string]any, error) {
	if err := ValidateNQN(subsystemNQNID); err != nil {
		return nil, err
	}
	if err := ValidateIPAddress("gateway_ip", gatewayIP); err != nil {
		return nil, err
	}
	if err := ValidatePoolName(poolName); err != nil {
		return nil, err
	}
	if err := ValidateImageName(imageName); err != nil {
		return nil, err
	}

	server, err := requireNVMeOfServer(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := nvmeOfSubsystemCreateWithServer(ctx, server, gatewayIP, gatewayName, subsystemNQNID); err != nil {
		return nil, err
	}
	if sizeGiB > 0 {
		if _, err := CreateImage(ctx, poolName, imageName, sizeGiB); err != nil {
			return nil, err
		}
	}
	if _, err := nvmeOfNamespaceCreateWithServer(ctx, server, subsystemNQNID, poolName, imageName); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":           "success",
		"gateway_ip":       gatewayIP,
		"subsystem_nqn_id": subsystemNQNID,
		"pool":             poolName,
		"image":            imageName,
		"size_gib":         sizeGiB,
	}, nil
}

// NVMeOfSubsystemList는 NVMe-oF CLI로 subsystem 목록 또는 상세를 조회한다.
func NVMeOfSubsystemList(ctx context.Context, subsystemNQNID string) (any, error) {
	subsystemNQNID = strings.TrimSpace(subsystemNQNID)
	if err := ValidateOptionalNQN(subsystemNQNID); err != nil {
		return nil, err
	}
	server, ok, err := resolveNVMeOfServer(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []any{}, nil
	}
	return nvmeOfSubsystemListWithServer(ctx, server, subsystemNQNID)
}

// NVMeOfSubsystemCreate는 subsystem, listener, wildcard host allow를 생성한다.
func NVMeOfSubsystemCreate(ctx context.Context, gatewayIP string, gatewayName string, subsystemNQNID string) (map[string]any, error) {
	if err := ValidateNQN(subsystemNQNID); err != nil {
		return nil, err
	}
	if err := ValidateIPAddress("gateway_ip", gatewayIP); err != nil {
		return nil, err
	}
	server, err := requireNVMeOfServer(ctx)
	if err != nil {
		return nil, err
	}
	return nvmeOfSubsystemCreateWithServer(ctx, server, gatewayIP, gatewayName, subsystemNQNID)
}

// NVMeOfSubsystemDelete는 subsystem을 삭제한다.
func NVMeOfSubsystemDelete(ctx context.Context, subsystemNQNID string) (map[string]any, error) {
	if err := ValidateNQN(subsystemNQNID); err != nil {
		return nil, err
	}
	server, err := requireNVMeOfServer(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := nvmeOfCLI(ctx, server, false, "subsystem", "del", "--subsystem", subsystemNQNID); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":           "success",
		"subsystem_nqn_id": subsystemNQNID,
	}, nil
}

// NVMeOfNamespaceList는 subsystem 지정 여부에 따라 namespace 목록을 조회한다.
func NVMeOfNamespaceList(ctx context.Context, subsystemNQNID string) (any, error) {
	subsystemNQNID = strings.TrimSpace(subsystemNQNID)
	if err := ValidateOptionalNQN(subsystemNQNID); err != nil {
		return nil, err
	}
	server, ok, err := resolveNVMeOfServer(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []any{}, nil
	}
	if subsystemNQNID != "" {
		return nvmeOfNamespaceListWithServer(ctx, server, subsystemNQNID)
	}

	subsystems, err := nvmeOfSubsystemListWithServer(ctx, server, "")
	if err != nil {
		return nil, err
	}
	names := nqnListFromSubsystems(subsystems)
	namespaces := make([]any, 0, len(names))
	for _, nqn := range names {
		value, err := nvmeOfNamespaceListWithServer(ctx, server, nqn)
		if err != nil {
			return nil, err
		}
		namespaces = append(namespaces, value)
	}
	return namespaces, nil
}

// NVMeOfNamespaceCreate는 필요 시 RBD image를 생성한 뒤 namespace를 추가한다.
func NVMeOfNamespaceCreate(ctx context.Context, subsystemNQNID string, poolName string, imageName string, sizeGiB int64) (map[string]any, error) {
	if err := ValidateNQN(subsystemNQNID); err != nil {
		return nil, err
	}
	if err := ValidatePoolName(poolName); err != nil {
		return nil, err
	}
	if err := ValidateImageName(imageName); err != nil {
		return nil, err
	}
	server, err := requireNVMeOfServer(ctx)
	if err != nil {
		return nil, err
	}
	if sizeGiB > 0 {
		if _, err := CreateImage(ctx, poolName, imageName, sizeGiB); err != nil {
			return nil, err
		}
	}
	if _, err := nvmeOfNamespaceCreateWithServer(ctx, server, subsystemNQNID, poolName, imageName); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":           "success",
		"subsystem_nqn_id": subsystemNQNID,
		"pool":             poolName,
		"image":            imageName,
		"size_gib":         sizeGiB,
	}, nil
}

// NVMeOfNamespaceDelete는 namespace를 삭제하고, 요청 시 연결된 RBD image도 삭제한다.
func NVMeOfNamespaceDelete(ctx context.Context, subsystemNQNID string, namespaceUUID string, imageDelete bool, poolName string, imageName string) (map[string]any, error) {
	if err := ValidateNQN(subsystemNQNID); err != nil {
		return nil, err
	}
	if err := ValidateNamespaceUUID(namespaceUUID); err != nil {
		return nil, err
	}
	if imageDelete {
		if err := ValidatePoolName(poolName); err != nil {
			return nil, err
		}
		if err := ValidateImageName(imageName); err != nil {
			return nil, err
		}
	}
	server, err := requireNVMeOfServer(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := nvmeOfCLI(ctx, server, false, "namespace", "del", "--subsystem", subsystemNQNID, "--uuid", namespaceUUID); err != nil {
		return nil, err
	}
	if imageDelete {
		if _, err := DeleteImage(ctx, poolName, imageName); err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"status":           "success",
		"subsystem_nqn_id": subsystemNQNID,
		"namespace_uuid":   namespaceUUID,
		"image_deleted":    imageDelete,
	}, nil
}

func writeNVMeOfServiceSpec(spec nvmeOfServiceSpec) (string, error) {
	raw, err := yaml.Marshal(spec)
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", "ablestack-nvmeof-*.yaml")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.Write(raw); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func nvmeOfCLIImage() string {
	if image := strings.TrimSpace(os.Getenv(envNVMeOfCLIImage)); image != "" {
		return image
	}
	return defaultNVMeOfCLIImage
}

func requireNVMeOfServer(ctx context.Context) (nvmeOfServer, error) {
	server, ok, err := resolveNVMeOfServer(ctx)
	if err != nil {
		return nvmeOfServer{}, err
	}
	if !ok {
		return nvmeOfServer{}, fmt.Errorf("nvmeof service is not running")
	}
	return server, nil
}

func resolveNVMeOfServer(ctx context.Context) (nvmeOfServer, bool, error) {
	raw, err := runJSON(ctx, "ceph", "orch", "ps", "--daemon_type", "nvmeof", "-f", "json")
	if err != nil {
		return nvmeOfServer{}, false, err
	}
	daemons := parseNVMeOfDaemons(raw)
	if len(daemons) == 0 {
		return nvmeOfServer{}, false, nil
	}

	address := strings.TrimSpace(os.Getenv(envNVMeOfServerAddress))
	if address == "" {
		address = hostsAddressForName(daemons[0].Hostname)
	}
	if address == "" {
		address = daemons[0].Hostname
	}
	port := strings.TrimSpace(os.Getenv(envNVMeOfServerPort))
	if port == "" {
		port = defaultNVMeOfServerPort
	}
	if err := ValidatePort(port); err != nil {
		return nvmeOfServer{}, false, err
	}
	return nvmeOfServer{
		Address: address,
		Port:    port,
		Daemons: daemons,
	}, true, nil
}

func parseNVMeOfDaemons(value any) []nvmeOfDaemon {
	items, ok := value.([]any)
	if !ok {
		return []nvmeOfDaemon{}
	}
	out := make([]nvmeOfDaemon, 0, len(items))
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		hostname := mapString(fields, "hostname")
		daemonName := mapString(fields, "daemon_name")
		if daemonName == "" {
			daemonName = mapString(fields, "daemon_id")
		}
		if hostname == "" && daemonName == "" {
			continue
		}
		out = append(out, nvmeOfDaemon{
			Hostname:   hostname,
			DaemonName: daemonName,
		})
	}
	return out
}

func hostsAddressForName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	raw, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		for _, alias := range parts[1:] {
			if alias == name {
				return parts[0]
			}
		}
	}
	return ""
}

func hostsNameForAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	raw, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 || parts[0] != address {
			continue
		}
		return parts[1]
	}
	return ""
}

func nvmeOfCLI(ctx context.Context, server nvmeOfServer, formatJSON bool, args ...string) ([]byte, error) {
	podmanArgs := []string{"run", "--rm", "-i", nvmeOfCLIImage()}
	if formatJSON {
		podmanArgs = append(podmanArgs, "--format", "json")
	}
	podmanArgs = append(podmanArgs, "--server-address", server.Address, "--server-port", server.Port)
	podmanArgs = append(podmanArgs, args...)
	return run(ctx, "podman", podmanArgs...)
}

func nvmeOfCLIJSON(ctx context.Context, server nvmeOfServer, args ...string) (any, error) {
	output, err := nvmeOfCLI(ctx, server, true, args...)
	if err != nil {
		return nil, err
	}
	return decodeJSON(output)
}

func nvmeOfSubsystemListWithServer(ctx context.Context, server nvmeOfServer, subsystemNQNID string) (any, error) {
	args := []string{"subsystem", "list"}
	if strings.TrimSpace(subsystemNQNID) != "" {
		args = append(args, "--subsystem", strings.TrimSpace(subsystemNQNID))
	}
	return nvmeOfCLIJSON(ctx, server, args...)
}

func nvmeOfSubsystemCreateWithServer(ctx context.Context, server nvmeOfServer, gatewayIP string, gatewayName string, subsystemNQNID string) (map[string]any, error) {
	gatewayName = pickNVMeOfGatewayName(server.Daemons, gatewayIP, gatewayName)
	if err := ValidateServiceName(gatewayName); err != nil {
		return nil, err
	}
	if _, err := nvmeOfCLI(ctx, server, false, "subsystem", "add", "--subsystem", subsystemNQNID); err != nil {
		return nil, err
	}
	if _, err := nvmeOfCLI(ctx, server, false, "listener", "add", "--subsystem", subsystemNQNID, "--host-name", gatewayName, "--traddr", gatewayIP, "--trsvcid", "4420"); err != nil {
		return nil, err
	}
	if _, err := nvmeOfCLI(ctx, server, false, "host", "add", "--subsystem", subsystemNQNID, "--host", "*"); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":           "success",
		"gateway_ip":       gatewayIP,
		"gateway_name":     gatewayName,
		"subsystem_nqn_id": subsystemNQNID,
	}, nil
}

func nvmeOfNamespaceListWithServer(ctx context.Context, server nvmeOfServer, subsystemNQNID string) (any, error) {
	return nvmeOfCLIJSON(ctx, server, "namespace", "list", "--subsystem", subsystemNQNID)
}

func nvmeOfNamespaceCreateWithServer(ctx context.Context, server nvmeOfServer, subsystemNQNID string, poolName string, imageName string) (map[string]any, error) {
	if _, err := nvmeOfCLI(ctx, server, false, "namespace", "add", "--subsystem", subsystemNQNID, "--rbd-pool", poolName, "--rbd-image", imageName); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":           "success",
		"subsystem_nqn_id": subsystemNQNID,
		"pool":             poolName,
		"image":            imageName,
	}, nil
}

func pickNVMeOfGatewayName(daemons []nvmeOfDaemon, gatewayIP string, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return clientGatewayName(requested)
	}
	hostName := hostsNameForAddress(gatewayIP)
	for _, daemon := range daemons {
		if daemon.DaemonName == "" {
			continue
		}
		if hostName != "" && (daemon.Hostname == hostName || strings.Contains(daemon.DaemonName, hostName)) {
			return clientGatewayName(daemon.DaemonName)
		}
		if net.ParseIP(gatewayIP) != nil && hostsAddressForName(daemon.Hostname) == gatewayIP {
			return clientGatewayName(daemon.DaemonName)
		}
	}
	for _, daemon := range daemons {
		if daemon.DaemonName != "" {
			return clientGatewayName(daemon.DaemonName)
		}
	}
	return ""
}

func clientGatewayName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "client.") {
		return value
	}
	return "client." + value
}

func nqnListFromSubsystems(value any) []string {
	fields, ok := value.(map[string]any)
	if !ok {
		return []string{}
	}
	items, ok := fields["subsystems"].([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		nqn := mapString(entry, "nqn")
		if nqn != "" {
			out = append(out, nqn)
		}
	}
	return out
}

func nvmeOfContainerID(ctx context.Context) (string, error) {
	raw, err := runJSON(ctx, "podman", "ps", "--format", "json")
	if err != nil {
		return "", err
	}
	items, ok := raw.([]any)
	if !ok {
		return "", nil
	}
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		image := strings.ToLower(mapString(entry, "Image"))
		name := strings.ToLower(mapString(entry, "Names"))
		if name == "" {
			name = strings.ToLower(mapString(entry, "Name"))
		}
		if !strings.Contains(image+" "+name, "nvmeof") || strings.Contains(image+" "+name, "nvmeof-cli") {
			continue
		}
		for _, key := range []string{"ID", "Id", "ContainerID", "ContainerId"} {
			id := mapString(entry, key)
			if id != "" && id != "<nil>" {
				return id, nil
			}
		}
	}
	return "", nil
}

func enrichNVMeOfTargetList(ctx context.Context, server nvmeOfServer, containerID string, raw any) {
	targets, ok := raw.([]any)
	if !ok {
		return
	}
	for _, item := range targets {
		target, ok := item.(map[string]any)
		if !ok {
			continue
		}
		nqn := mapString(target, "nqn")
		if nqn == "" {
			continue
		}
		if controllers, err := runJSON(ctx, "podman", "exec", "-i", containerID, "python3", nvmeOfSPDKRPCPath, "nvmf_subsystem_get_controllers", nqn); err == nil {
			if values, ok := controllers.([]any); ok {
				target["session"] = len(values)
			}
		}
		if namespace, err := nvmeOfNamespaceListWithServer(ctx, server, nqn); err == nil {
			target["namespace_detail"] = namespace
		}
	}
}

func mapString(fields map[string]any, key string) string {
	value, ok := fields[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
