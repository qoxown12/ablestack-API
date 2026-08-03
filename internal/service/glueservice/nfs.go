package glueservice

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type nfsServiceSpec struct {
	ServiceType string             `yaml:"service_type"`
	ServiceID   string             `yaml:"service_id"`
	Placement   nfsPlacementSpec   `yaml:"placement"`
	Spec        nfsServiceSpecBody `yaml:"spec"`
}

type nfsPlacementSpec struct {
	Count int      `yaml:"count,omitempty"`
	Hosts []string `yaml:"hosts"`
}

type nfsServiceSpecBody struct {
	Port int `yaml:"port"`
}

type nfsIngressServiceSpec struct {
	ServiceType string             `yaml:"service_type"`
	ServiceID   string             `yaml:"service_id"`
	Placement   nfsPlacementSpec   `yaml:"placement"`
	Spec        nfsIngressSpecBody `yaml:"spec"`
}

type nfsIngressSpecBody struct {
	BackendService           string   `yaml:"backend_service"`
	VirtualIP                string   `yaml:"virtual_ip"`
	FrontendPort             int      `yaml:"frontend_port"`
	MonitorPort              int      `yaml:"monitor_port"`
	VirtualInterfaceNetworks []string `yaml:"virtual_interface_networks,omitempty"`
	UseKeepalivedMulticast   bool     `yaml:"use_keepalived_multicast"`
}

type nfsExportPayload struct {
	ExportID      int               `json:"export_id,omitempty"`
	AccessType    string            `json:"access_type"`
	FSAL          map[string]string `json:"fsal"`
	Protocols     []int             `json:"protocols"`
	Path          string            `json:"path"`
	Pseudo        string            `json:"pseudo"`
	Squash        string            `json:"squash"`
	SecurityLabel bool              `json:"security_label,omitempty"`
	Transports    []string          `json:"transports"`
}

// NFSClusters는 cluster_id가 있으면 단일 cluster, 없으면 전체 cluster 정보를 조회한다.
func NFSClusters(ctx context.Context, clusterID string) (any, error) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID != "" {
		if err := ValidateCephName("cluster_id", clusterID); err != nil {
			return nil, err
		}
		return runJSON(ctx, "ceph", "nfs", "cluster", "info", clusterID)
	}
	return runJSON(ctx, "ceph", "nfs", "cluster", "info")
}

// NFSExports는 cluster_id가 없으면 cluster 목록을 먼저 조회한 뒤 export를 합쳐 반환한다.
func NFSExports(ctx context.Context, clusterID string) (any, error) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID != "" {
		if err := ValidateCephName("cluster_id", clusterID); err != nil {
			return nil, err
		}
		return runJSON(ctx, "ceph", "nfs", "export", "ls", clusterID, "--detailed")
	}

	rawClusters, err := runJSON(ctx, "ceph", "nfs", "cluster", "ls")
	if err != nil {
		return nil, err
	}
	clusterNames, err := namesFromList(rawClusters)
	if err != nil {
		return nil, err
	}

	exports := []any{}
	for _, name := range clusterNames {
		if err := ValidateCephName("cluster_id", name); err != nil {
			return nil, err
		}
		rawExports, err := runJSON(ctx, "ceph", "nfs", "export", "ls", name, "--detailed")
		if err != nil {
			return nil, err
		}
		if values, ok := rawExports.([]any); ok {
			exports = append(exports, values...)
		} else if rawExports != nil {
			exports = append(exports, rawExports)
		}
	}
	return exports, nil
}

// NFSClusterCreate는 NFS cluster service spec을 ceph orch에 적용한다.
func NFSClusterCreate(ctx context.Context, clusterID string, port string, hosts []string, serviceCount string) (map[string]any, error) {
	spec, err := buildNFSServiceSpec(clusterID, port, hosts, serviceCount)
	if err != nil {
		return nil, err
	}
	specPath, err := writeYAMLTemp("ablestack-nfs-*.yaml", spec)
	if err != nil {
		return nil, err
	}
	defer os.Remove(specPath)
	if _, err := run(ctx, "ceph", "orch", "apply", "-i", specPath); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "cluster_id": strings.TrimSpace(clusterID), "hosts": spec.Placement.Hosts, "port": spec.Spec.Port}, nil
}

// NFSClusterUpdate는 NFS cluster service spec을 적용한 뒤 redeploy한다.
func NFSClusterUpdate(ctx context.Context, clusterID string, port string, hosts []string, serviceCount string) (map[string]any, error) {
	val, err := NFSClusterCreate(ctx, clusterID, port, hosts, serviceCount)
	if err != nil {
		return nil, err
	}
	serviceName := "nfs." + strings.TrimSpace(clusterID)
	if _, err := run(ctx, "ceph", "orch", "redeploy", serviceName); err != nil {
		return nil, err
	}
	val["redeployed"] = serviceName
	return val, nil
}

// NFSClusterDelete는 NFS cluster를 삭제한다.
func NFSClusterDelete(ctx context.Context, clusterID string) (map[string]any, error) {
	clusterID = strings.TrimSpace(clusterID)
	if err := ValidateCephName("cluster_id", clusterID); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "ceph", "nfs", "cluster", "rm", clusterID); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "cluster_id": clusterID}, nil
}

// NFSIngressCreate는 HAProxy/keepalived ingress service spec을 ceph orch에 적용한다.
func NFSIngressCreate(ctx context.Context, serviceID string, hosts []string, backendService string, virtualIP string, frontendPort string, monitorPort string, virtualInterfaceNetworks []string) (map[string]any, error) {
	spec, err := buildNFSIngressSpec(serviceID, hosts, backendService, virtualIP, frontendPort, monitorPort, virtualInterfaceNetworks)
	if err != nil {
		return nil, err
	}
	specPath, err := writeYAMLTemp("ablestack-nfs-ingress-*.yaml", spec)
	if err != nil {
		return nil, err
	}
	defer os.Remove(specPath)
	if _, err := run(ctx, "ceph", "orch", "apply", "-i", specPath); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":          "success",
		"service_id":      spec.ServiceID,
		"backend_service": spec.Spec.BackendService,
		"virtual_ip":      spec.Spec.VirtualIP,
		"frontend_port":   spec.Spec.FrontendPort,
		"monitor_port":    spec.Spec.MonitorPort,
	}, nil
}

// NFSIngressUpdate는 ingress service spec을 적용한 뒤 redeploy한다.
func NFSIngressUpdate(ctx context.Context, serviceID string, hosts []string, backendService string, virtualIP string, frontendPort string, monitorPort string, virtualInterfaceNetworks []string) (map[string]any, error) {
	val, err := NFSIngressCreate(ctx, serviceID, hosts, backendService, virtualIP, frontendPort, monitorPort, virtualInterfaceNetworks)
	if err != nil {
		return nil, err
	}
	serviceName := "ingress." + strings.TrimSpace(serviceID)
	if _, err := run(ctx, "ceph", "orch", "redeploy", serviceName); err != nil {
		return nil, err
	}
	val["redeployed"] = serviceName
	return val, nil
}

// NFSExportCreate는 NFS export spec을 적용한다.
func NFSExportCreate(ctx context.Context, clusterID string, accessType string, fsName string, storageName string, path string, pseudo string, squash string, transports []string, securityLabel bool) (map[string]any, error) {
	payload, err := buildNFSExportPayload(0, accessType, fsName, storageName, path, pseudo, squash, transports, securityLabel)
	if err != nil {
		return nil, err
	}
	return nfsExportApply(ctx, clusterID, payload)
}

// NFSExportUpdate는 export_id를 포함한 NFS export spec을 적용한다.
func NFSExportUpdate(ctx context.Context, clusterID string, exportID string, accessType string, fsName string, storageName string, path string, pseudo string, squash string, transports []string, securityLabel bool) (map[string]any, error) {
	id, err := parsePositiveInt("export_id", exportID)
	if err != nil {
		return nil, err
	}
	payload, err := buildNFSExportPayload(id, accessType, fsName, storageName, path, pseudo, squash, transports, securityLabel)
	if err != nil {
		return nil, err
	}
	return nfsExportApply(ctx, clusterID, payload)
}

// NFSExportDelete는 export_id에 해당하는 pseudo를 찾아 export를 삭제한다.
func NFSExportDelete(ctx context.Context, clusterID string, exportID string) (map[string]any, error) {
	clusterID = strings.TrimSpace(clusterID)
	if err := ValidateCephName("cluster_id", clusterID); err != nil {
		return nil, err
	}
	id, err := parsePositiveInt("export_id", exportID)
	if err != nil {
		return nil, err
	}
	raw, err := runJSON(ctx, "ceph", "nfs", "export", "ls", clusterID, "--detailed")
	if err != nil {
		return nil, err
	}
	pseudo := nfsPseudoByExportID(raw, id)
	if pseudo == "" {
		return nil, fmt.Errorf("export_id not found")
	}
	if _, err := run(ctx, "ceph", "nfs", "export", "rm", clusterID, pseudo); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "cluster_id": clusterID, "export_id": id, "pseudo": pseudo}, nil
}

func buildNFSServiceSpec(clusterID string, port string, hosts []string, serviceCount string) (nfsServiceSpec, error) {
	clusterID = strings.TrimSpace(clusterID)
	hosts = trimStringSlice(hosts)
	if err := ValidateCephName("cluster_id", clusterID); err != nil {
		return nfsServiceSpec{}, err
	}
	if err := ValidatePort(port); err != nil {
		return nfsServiceSpec{}, err
	}
	if len(hosts) == 0 {
		return nfsServiceSpec{}, fmt.Errorf("hosts is required")
	}
	for _, host := range hosts {
		if err := ValidateCephName("host", host); err != nil {
			return nfsServiceSpec{}, err
		}
	}
	portValue, _ := strconv.Atoi(strings.TrimSpace(port))
	count := 0
	if strings.TrimSpace(serviceCount) != "" {
		value, err := parsePositiveInt("service_count", serviceCount)
		if err != nil {
			return nfsServiceSpec{}, err
		}
		count = value
	}
	return nfsServiceSpec{
		ServiceType: "nfs",
		ServiceID:   clusterID,
		Placement:   nfsPlacementSpec{Count: count, Hosts: hosts},
		Spec:        nfsServiceSpecBody{Port: portValue},
	}, nil
}

func buildNFSIngressSpec(serviceID string, hosts []string, backendService string, virtualIP string, frontendPort string, monitorPort string, virtualInterfaceNetworks []string) (nfsIngressServiceSpec, error) {
	serviceID = strings.TrimSpace(serviceID)
	backendService = strings.TrimSpace(backendService)
	virtualIP = strings.TrimSpace(virtualIP)
	hosts = trimStringSlice(hosts)
	virtualInterfaceNetworks = trimStringSlice(virtualInterfaceNetworks)
	if err := ValidateCephName("service_id", serviceID); err != nil {
		return nfsIngressServiceSpec{}, err
	}
	if err := ValidateCephName("backend_service", backendService); err != nil {
		return nfsIngressServiceSpec{}, err
	}
	if len(hosts) == 0 {
		return nfsIngressServiceSpec{}, fmt.Errorf("hosts is required")
	}
	for _, host := range hosts {
		if err := ValidateCephName("host", host); err != nil {
			return nfsIngressServiceSpec{}, err
		}
	}
	if err := ValidateIPOrCIDR("virtual_ip", virtualIP); err != nil {
		return nfsIngressServiceSpec{}, err
	}
	if err := ValidatePort(frontendPort); err != nil {
		return nfsIngressServiceSpec{}, err
	}
	if err := ValidatePort(monitorPort); err != nil {
		return nfsIngressServiceSpec{}, err
	}
	for _, network := range virtualInterfaceNetworks {
		if err := ValidateCIDR("virtual_interface_networks", network); err != nil {
			return nfsIngressServiceSpec{}, err
		}
	}
	frontendPortValue, _ := strconv.Atoi(strings.TrimSpace(frontendPort))
	monitorPortValue, _ := strconv.Atoi(strings.TrimSpace(monitorPort))
	return nfsIngressServiceSpec{
		ServiceType: "ingress",
		ServiceID:   serviceID,
		Placement:   nfsPlacementSpec{Hosts: hosts},
		Spec: nfsIngressSpecBody{
			BackendService:           backendService,
			VirtualIP:                virtualIP,
			FrontendPort:             frontendPortValue,
			MonitorPort:              monitorPortValue,
			VirtualInterfaceNetworks: virtualInterfaceNetworks,
			UseKeepalivedMulticast:   false,
		},
	}, nil
}

func buildNFSExportPayload(exportID int, accessType string, fsName string, storageName string, path string, pseudo string, squash string, transports []string, securityLabel bool) (nfsExportPayload, error) {
	accessType = strings.ToUpper(strings.TrimSpace(firstNonEmpty(accessType, "RW")))
	storageName = strings.ToUpper(strings.TrimSpace(storageName))
	path = strings.TrimSpace(path)
	pseudo = strings.TrimSpace(pseudo)
	squash = strings.TrimSpace(firstNonEmpty(squash, "no_root_squash"))
	transports = trimStringSlice(transports)
	if len(transports) == 0 {
		transports = []string{"TCP"}
	}
	switch accessType {
	case "RW", "RO", "NONE":
	default:
		return nfsExportPayload{}, fmt.Errorf("access_type must be one of RW, RO, NONE")
	}
	if storageName != "CEPH" && storageName != "RGW" {
		return nfsExportPayload{}, fmt.Errorf("storage_name must be one of CEPH, RGW")
	}
	if err := ValidateSMBPath("pseudo", pseudo); err != nil {
		return nfsExportPayload{}, err
	}
	if path == "" {
		return nfsExportPayload{}, fmt.Errorf("path is required")
	}
	fsal := map[string]string{"name": storageName}
	if storageName == "CEPH" {
		fsName = strings.TrimSpace(fsName)
		if err := ValidateCephName("fs_name", fsName); err != nil {
			return nfsExportPayload{}, err
		}
		if err := ValidateSMBPath("path", path); err != nil {
			return nfsExportPayload{}, err
		}
		fsal["fs_name"] = fsName
	} else if err := ValidateBucketName(path); err != nil {
		return nfsExportPayload{}, err
	}
	for _, transport := range transports {
		if transport != "TCP" && transport != "UDP" {
			return nfsExportPayload{}, fmt.Errorf("transports must be TCP or UDP")
		}
	}
	return nfsExportPayload{
		ExportID:      exportID,
		AccessType:    accessType,
		FSAL:          fsal,
		Protocols:     []int{4},
		Path:          path,
		Pseudo:        pseudo,
		Squash:        squash,
		SecurityLabel: securityLabel,
		Transports:    transports,
	}, nil
}

func nfsExportApply(ctx context.Context, clusterID string, payload nfsExportPayload) (map[string]any, error) {
	clusterID = strings.TrimSpace(clusterID)
	if err := ValidateCephName("cluster_id", clusterID); err != nil {
		return nil, err
	}
	specPath, err := writeJSONTemp("ablestack-nfs-export-*.json", payload)
	if err != nil {
		return nil, err
	}
	defer os.Remove(specPath)
	if _, err := run(ctx, "ceph", "nfs", "export", "apply", clusterID, "-i", specPath); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "cluster_id": clusterID, "pseudo": payload.Pseudo, "export_id": payload.ExportID}, nil
}

func nfsPseudoByExportID(raw any, exportID int) string {
	items, ok := raw.([]any)
	if !ok {
		return ""
	}
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, ok := numericField(entry, "export_id")
		if ok && id == exportID {
			return mapString(entry, "pseudo")
		}
	}
	return ""
}

func writeYAMLTemp(pattern string, value any) (string, error) {
	raw, err := yaml.Marshal(value)
	if err != nil {
		return "", err
	}
	return writeTemp(pattern, raw)
}

func writeJSONTemp(pattern string, value any) (string, error) {
	raw, err := json.MarshalIndent(value, "", " ")
	if err != nil {
		return "", err
	}
	return writeTemp(pattern, raw)
}

func writeTemp(pattern string, raw []byte) (string, error) {
	file, err := os.CreateTemp("", pattern)
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

func parsePositiveInt(field string, raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 {
		return 0, fmt.Errorf("%s must be greater than zero", field)
	}
	return value, nil
}

func numericField(entry map[string]any, key string) (int, bool) {
	value, ok := entry[key]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case json.Number:
		i, err := v.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}
