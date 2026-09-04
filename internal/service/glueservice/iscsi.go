package glueservice

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultISCSIDashboardUser = "admin"
	envISCSIDashboardURL      = "ABLESTACK_GLUE_DASHBOARD_URL"
	envISCSIDashboardUser     = "ABLESTACK_GLUE_DASHBOARD_USER"
	envISCSIDashboardPassword = "ABLESTACK_GLUE_DASHBOARD_PASSWORD"
)

type iscsiServiceConfig struct {
	ServiceID     string
	Hosts         []string
	TrustedIPList []string
	Pool          string
	APIPort       int
	APIUser       string
	APIPassword   string
	Count         int
}

type iscsiServiceSpec struct {
	ServiceType string               `yaml:"service_type"`
	ServiceID   string               `yaml:"service_id"`
	Placement   iscsiPlacementSpec   `yaml:"placement"`
	Spec        iscsiServiceSpecBody `yaml:"spec"`
}

type iscsiPlacementSpec struct {
	Count int      `yaml:"count,omitempty"`
	Hosts []string `yaml:"hosts"`
}

type iscsiServiceSpecBody struct {
	APIPassword   string `yaml:"api_password"`
	APIUser       string `yaml:"api_user"`
	APIPort       int    `yaml:"api_port"`
	Pool          string `yaml:"pool"`
	TrustedIPList string `yaml:"trusted_ip_list"`
}

type iscsiOrchService struct {
	ServiceID   string `json:"service_id"`
	ServiceName string `json:"service_name"`
	Placement   struct {
		Hosts []string `json:"hosts"`
	} `json:"placement"`
}

type iscsiAuthPayload struct {
	User           string `json:"user"`
	Password       string `json:"password"`
	MutualUser     string `json:"mutual_user"`
	MutualPassword string `json:"mutual_password"`
}

type iscsiPortalPayload struct {
	Host string `json:"host"`
	IP   string `json:"ip"`
}

type iscsiDiskPayload struct {
	Pool      string         `json:"pool"`
	Image     string         `json:"image"`
	Controls  map[string]any `json:"controls"`
	Backstore string         `json:"backstore"`
	Lun       int            `json:"lun"`
}

type iscsiTargetPayload struct {
	// Glue dashboard requires the current target IQN in the update body as
	// well as in the URL. Keep this field non-omitempty so that a future
	// caller cannot silently send an invalid update payload.
	TargetIQN    string               `json:"target_iqn"`
	NewTargetIQN string               `json:"new_target_iqn,omitempty"`
	Portals      []iscsiPortalPayload `json:"portals"`
	Disks        []iscsiDiskPayload   `json:"disks"`
	Clients      []any                `json:"clients"`
	Groups       []any                `json:"groups"`
	ACLEnabled   bool                 `json:"acl_enabled"`
	Auth         iscsiAuthPayload     `json:"auth"`
}

// iscsiTargetUpdatePayload contains the identity fields required by the
// Glue dashboard PUT API. The dashboard requires new_target_iqn even when
// the target is not renamed, so it is populated with the current IQN then.
type iscsiTargetUpdatePayload struct {
	TargetIQN    string               `json:"target_iqn"`
	NewTargetIQN string               `json:"new_target_iqn,omitempty"`
	Portals      []iscsiPortalPayload `json:"portals"`
	Disks        []iscsiDiskPayload   `json:"disks"`
	Clients      []any                `json:"clients"`
	Groups       []any                `json:"groups"`
	ACLEnabled   bool                 `json:"acl_enabled"`
	Auth         iscsiAuthPayload     `json:"auth"`
}

type iscsiDashboardToken struct {
	Token string `json:"token"`
}

// ISCSIServiceCreate는 iSCSI gateway service spec을 ceph orch에 적용한다.
func ISCSIServiceCreate(ctx context.Context, serviceID string, hosts []string, trustedIPList []string, pool string, apiPort string, apiUser string, apiPassword string, count string) (map[string]any, error) {
	config, err := normalizeISCSIServiceConfig(serviceID, hosts, trustedIPList, pool, apiPort, apiUser, apiPassword, count)
	if err != nil {
		return nil, err
	}
	if err := validateISCSIServicePlacement(ctx, config.ServiceID, config.Hosts); err != nil {
		return nil, err
	}
	specPath, err := writeISCSIServiceSpec(config)
	if err != nil {
		return nil, err
	}
	defer os.Remove(specPath)

	if _, err := run(ctx, "ceph", "orch", "apply", "-i", specPath); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":          "success",
		"service_id":      config.ServiceID,
		"hosts":           config.Hosts,
		"trusted_ip_list": config.TrustedIPList,
		"pool":            config.Pool,
		"api_port":        config.APIPort,
		"count":           config.Count,
	}, nil
}

// ISCSIServiceUpdate는 iSCSI gateway service spec을 적용한 뒤 service를 redeploy한다.
func ISCSIServiceUpdate(ctx context.Context, serviceID string, hosts []string, trustedIPList []string, pool string, apiPort string, apiUser string, apiPassword string, count string) (map[string]any, error) {
	val, err := ISCSIServiceCreate(ctx, serviceID, hosts, trustedIPList, pool, apiPort, apiUser, apiPassword, count)
	if err != nil {
		return nil, err
	}
	serviceName := "iscsi." + strings.TrimSpace(serviceID)
	if _, err := run(ctx, "ceph", "orch", "redeploy", serviceName); err != nil {
		return nil, err
	}
	val["redeployed"] = serviceName
	return val, nil
}

// ISCSIDiscoveryAuth는 Glue dashboard iSCSI discovery auth 정보를 조회한다.
func ISCSIDiscoveryAuth(ctx context.Context) (any, error) {
	return cephDashboardRequest(ctx, http.MethodGet, "api/iscsi/discoveryauth", nil, http.StatusOK)
}

// ISCSIDiscoveryAuthUpdate는 Glue dashboard iSCSI discovery auth 정보를 수정한다.
func ISCSIDiscoveryAuthUpdate(ctx context.Context, user string, password string, mutualUser string, mutualPassword string) (any, error) {
	payload, err := normalizeISCSIAuthPayload(user, password, mutualUser, mutualPassword, false)
	if err != nil {
		return nil, err
	}
	return cephDashboardRequest(ctx, http.MethodPut, "api/iscsi/discoveryauth?user=%20&password=%20&mutual_user=%20&mutual_password=%20", payload, http.StatusOK)
}

// ISCSITargetList는 Glue dashboard에서 iSCSI target 목록 또는 상세를 조회한다.
func ISCSITargetList(ctx context.Context, iqnID string) (any, error) {
	iqnID = strings.TrimSpace(iqnID)
	path := "api/iscsi/target"
	if iqnID != "" {
		if err := ValidateISCSIIQN("iqn_id", iqnID); err != nil {
			return nil, err
		}
		path += "/" + url.PathEscape(iqnID)
	}
	return cephDashboardRequest(ctx, http.MethodGet, path, nil, http.StatusOK)
}

// ISCSITargetCreate는 Glue dashboard API로 iSCSI target을 생성한다.
func ISCSITargetCreate(ctx context.Context, iqnID string, hosts []string, ipAddresses []string, poolNames []string, imageNames []string, aclEnabled string, username string, password string, mutualUsername string, mutualPassword string) (any, error) {
	payload, err := normalizeISCSITargetPayload(iqnID, "", hosts, ipAddresses, poolNames, imageNames, aclEnabled, username, password, mutualUsername, mutualPassword)
	if err != nil {
		return nil, err
	}
	if err := validateISCSITargetGatewayPlacement(ctx, payload.Portals); err != nil {
		return nil, err
	}
	return cephDashboardRequest(ctx, http.MethodPost, "api/iscsi/target", payload, http.StatusCreated)
}

// ISCSITargetUpdate는 Glue dashboard API로 iSCSI target을 수정한다.
func ISCSITargetUpdate(ctx context.Context, iqnID string, newIQNID string, hosts []string, ipAddresses []string, poolNames []string, imageNames []string, aclEnabled string, username string, password string, mutualUsername string, mutualPassword string) (any, error) {
	payload, err := normalizeISCSITargetPayload(iqnID, newIQNID, hosts, ipAddresses, poolNames, imageNames, aclEnabled, username, password, mutualUsername, mutualPassword)
	if err != nil {
		return nil, err
	}
	currentIQN := strings.TrimSpace(iqnID)
	updatePayload := buildISCSITargetUpdatePayload(currentIQN, payload)
	if err := validateISCSITargetGatewayPlacement(ctx, payload.Portals); err != nil {
		return nil, err
	}
	return cephDashboardRequest(ctx, http.MethodPut, "api/iscsi/target/"+url.PathEscape(currentIQN), updatePayload, http.StatusOK)
}

func buildISCSITargetUpdatePayload(iqnID string, payload iscsiTargetPayload) iscsiTargetUpdatePayload {
	currentIQN := strings.TrimSpace(iqnID)
	newTargetIQN := payload.NewTargetIQN
	if newTargetIQN == "" {
		// The dashboard's PUT handler validates new_target_iqn unconditionally.
		// For an ordinary edit, the current IQN is also the resulting IQN.
		newTargetIQN = currentIQN
	}
	return iscsiTargetUpdatePayload{
		TargetIQN:    currentIQN,
		NewTargetIQN: newTargetIQN,
		Portals:      payload.Portals,
		Disks:        payload.Disks,
		Clients:      payload.Clients,
		Groups:       payload.Groups,
		ACLEnabled:   payload.ACLEnabled,
		Auth:         payload.Auth,
	}
}

func listISCSIOrchServices(ctx context.Context) ([]iscsiOrchService, error) {
	value, err := ListServices(ctx, "iscsi", "")
	if err != nil {
		return nil, fmt.Errorf("list iSCSI gateway services: %w", err)
	}

	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode iSCSI gateway services: %w", err)
	}
	services := []iscsiOrchService{}
	if err := json.Unmarshal(raw, &services); err != nil {
		return nil, fmt.Errorf("decode iSCSI gateway services: %w", err)
	}
	return services, nil
}

func iscsiOrchServiceName(service iscsiOrchService) string {
	if name := strings.TrimSpace(service.ServiceName); name != "" {
		return name
	}
	if id := strings.TrimSpace(service.ServiceID); id != "" {
		return "iscsi." + id
	}
	return "unknown"
}

func iscsiOrchServiceID(service iscsiOrchService) string {
	if id := strings.TrimSpace(service.ServiceID); id != "" {
		return id
	}
	return strings.TrimPrefix(strings.TrimSpace(service.ServiceName), "iscsi.")
}

func normalizedISCSIHosts(hosts []string) map[string]string {
	result := make(map[string]string)
	for _, host := range hosts {
		normalized := strings.ToLower(strings.TrimSpace(host))
		if normalized != "" {
			result[normalized] = strings.TrimSpace(host)
		}
	}
	return result
}

func validateISCSIServicePlacement(ctx context.Context, serviceID string, hosts []string) error {
	services, err := listISCSIOrchServices(ctx)
	if err != nil {
		return err
	}
	requestedHosts := normalizedISCSIHosts(hosts)
	serviceID = strings.TrimSpace(serviceID)

	for _, service := range services {
		if iscsiOrchServiceID(service) == serviceID {
			continue
		}
		existingHosts := normalizedISCSIHosts(service.Placement.Hosts)
		for normalizedHost, displayHost := range requestedHosts {
			if _, exists := existingHosts[normalizedHost]; exists {
				return fmt.Errorf("iSCSI host %q is already assigned to service %q; only one iSCSI gateway service may run on a host", displayHost, iscsiOrchServiceName(service))
			}
		}
	}
	return nil
}

func validateISCSITargetGatewayPlacement(ctx context.Context, portals []iscsiPortalPayload) error {
	services, err := listISCSIOrchServices(ctx)
	if err != nil {
		return err
	}

	hostServices := make(map[string]map[string]struct{})
	for _, service := range services {
		serviceName := iscsiOrchServiceName(service)
		for host := range normalizedISCSIHosts(service.Placement.Hosts) {
			if hostServices[host] == nil {
				hostServices[host] = make(map[string]struct{})
			}
			hostServices[host][serviceName] = struct{}{}
		}
	}

	for _, portal := range portals {
		host := strings.ToLower(strings.TrimSpace(portal.Host))
		servicesForHost := hostServices[host]
		if len(servicesForHost) < 2 {
			continue
		}
		names := make([]string, 0, len(servicesForHost))
		for name := range servicesForHost {
			names = append(names, name)
		}
		return fmt.Errorf("iSCSI host %q is assigned to multiple gateway services (%s); remove the duplicate service before creating a target", strings.TrimSpace(portal.Host), strings.Join(names, ", "))
	}
	return nil
}

// ISCSITargetDelete는 Glue dashboard API로 iSCSI target을 삭제한다.
func ISCSITargetDelete(ctx context.Context, iqnID string) (map[string]any, error) {
	iqnID = strings.TrimSpace(iqnID)
	if err := ValidateISCSIIQN("iqn_id", iqnID); err != nil {
		return nil, err
	}
	if _, err := cephDashboardRequest(ctx, http.MethodDelete, "api/iscsi/target/"+url.PathEscape(iqnID), nil, http.StatusNoContent); err != nil {
		return nil, err
	}
	return map[string]any{
		"status": "success",
		"iqn_id": iqnID,
	}, nil
}

// ISCSITargetPurge는 local iSCSI gateway container 안에서 gwcli delete를 실행한다.
func ISCSITargetPurge(ctx context.Context, iqnID string) (map[string]any, error) {
	iqnID = strings.TrimSpace(iqnID)
	if err := ValidateISCSIIQN("iqn_id", iqnID); err != nil {
		return nil, err
	}
	containerID, err := iscsiContainerID(ctx)
	if err != nil {
		return nil, err
	}
	if containerID == "" {
		return nil, fmt.Errorf("iSCSI gateway container not found")
	}
	if _, err := run(ctx, "podman", "exec", "-i", containerID, "gwcli", "/iscsi-targets", "delete", iqnID); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":       "success",
		"iqn_id":       iqnID,
		"container_id": containerID,
	}, nil
}

func normalizeISCSIServiceConfig(serviceID string, hosts []string, trustedIPList []string, pool string, apiPort string, apiUser string, apiPassword string, count string) (iscsiServiceConfig, error) {
	config := iscsiServiceConfig{
		ServiceID:   strings.TrimSpace(serviceID),
		Hosts:       trimStringSlice(hosts),
		Pool:        strings.TrimSpace(pool),
		APIUser:     strings.TrimSpace(apiUser),
		APIPassword: strings.TrimSpace(apiPassword),
	}
	if err := ValidateCephName("service_id", config.ServiceID); err != nil {
		return config, err
	}
	if len(config.Hosts) == 0 {
		return config, fmt.Errorf("hosts is required")
	}
	for _, host := range config.Hosts {
		if err := ValidateCephName("host", host); err != nil {
			return config, err
		}
	}
	if err := ValidatePoolName(config.Pool); err != nil {
		return config, err
	}
	if err := ValidatePort(apiPort); err != nil {
		return config, err
	}
	port, _ := strconv.Atoi(strings.TrimSpace(apiPort))
	config.APIPort = port
	if err := ValidateISCSIUser("api_user", config.APIUser, true); err != nil {
		return config, err
	}
	if err := ValidateISCSISecret("api_password", config.APIPassword, true); err != nil {
		return config, err
	}
	count = strings.TrimSpace(count)
	if count != "" {
		value, err := strconv.Atoi(count)
		if err != nil || value < 1 {
			return config, fmt.Errorf("count must be greater than zero")
		}
		config.Count = value
	}

	config.TrustedIPList = trimStringSlice(trustedIPList)
	if len(config.TrustedIPList) == 0 {
		resolved, err := resolveHostIPs(config.Hosts)
		if err != nil {
			return config, err
		}
		config.TrustedIPList = resolved
	}
	for _, ip := range config.TrustedIPList {
		if err := ValidateIPAddress("trusted_ip_list", ip); err != nil {
			return config, err
		}
	}
	return config, nil
}

func normalizeISCSITargetPayload(iqnID string, newIQNID string, hosts []string, ipAddresses []string, poolNames []string, imageNames []string, aclEnabled string, username string, password string, mutualUsername string, mutualPassword string) (iscsiTargetPayload, error) {
	iqnID = strings.TrimSpace(iqnID)
	newIQNID = strings.TrimSpace(newIQNID)
	if err := ValidateISCSIIQN("iqn_id", iqnID); err != nil {
		return iscsiTargetPayload{}, err
	}
	// The UI sends the current IQN in the new-IQN field for ordinary target
	// edits. Treat that as no rename; otherwise the dashboard may attempt a
	// self-rename while the user only changed the disk list.
	if newIQNID == iqnID {
		newIQNID = ""
	}
	if newIQNID != "" {
		if err := ValidateISCSIIQN("new_iqn_id", newIQNID); err != nil {
			return iscsiTargetPayload{}, err
		}
	}

	hosts = trimStringSlice(hosts)
	ipAddresses = trimStringSlice(ipAddresses)
	if len(hosts) == 0 {
		return iscsiTargetPayload{}, fmt.Errorf("hosts is required")
	}
	if len(hosts) != len(ipAddresses) {
		return iscsiTargetPayload{}, fmt.Errorf("hosts and ip_address must have the same length")
	}
	portals := make([]iscsiPortalPayload, 0, len(hosts))
	for i := range hosts {
		if err := ValidateCephName("host", hosts[i]); err != nil {
			return iscsiTargetPayload{}, err
		}
		if err := ValidateIPAddress("ip_address", ipAddresses[i]); err != nil {
			return iscsiTargetPayload{}, err
		}
		portals = append(portals, iscsiPortalPayload{Host: hosts[i], IP: ipAddresses[i]})
	}

	poolNames = trimStringSlice(poolNames)
	imageNames = trimStringSlice(imageNames)
	if len(poolNames) != len(imageNames) {
		return iscsiTargetPayload{}, fmt.Errorf("pool_name and image_name must have the same length")
	}
	disks := make([]iscsiDiskPayload, 0, len(imageNames))
	for i := range imageNames {
		if err := ValidatePoolName(poolNames[i]); err != nil {
			return iscsiTargetPayload{}, err
		}
		if err := ValidateImageName(imageNames[i]); err != nil {
			return iscsiTargetPayload{}, err
		}
		disks = append(disks, iscsiDiskPayload{
			Pool:      poolNames[i],
			Image:     imageNames[i],
			Controls:  map[string]any{},
			Backstore: "user:rbd",
			Lun:       i,
		})
	}

	acl, err := parseBoolDefault(aclEnabled, false)
	if err != nil {
		return iscsiTargetPayload{}, fmt.Errorf("acl_enabled must be true or false")
	}
	// ceph-iscsi treats initiator-IQN ACL authentication and target CHAP as
	// mutually exclusive modes. ACL mode therefore does not require CHAP
	// credentials; credentials are only used when ACL mode is disabled.
	auth, err := normalizeISCSIAuthPayload(username, password, mutualUsername, mutualPassword, false)
	if err != nil {
		return iscsiTargetPayload{}, err
	}
	if acl && (auth.User != "" || auth.Password != "" || auth.MutualUser != "" || auth.MutualPassword != "") {
		return iscsiTargetPayload{}, fmt.Errorf("acl_enabled must be disabled when target CHAP authentication is configured")
	}

	targetIQN := iqnID
	return iscsiTargetPayload{
		TargetIQN:    targetIQN,
		NewTargetIQN: newIQNID,
		Portals:      portals,
		Disks:        disks,
		Clients:      []any{},
		Groups:       []any{},
		ACLEnabled:   acl,
		Auth:         auth,
	}, nil
}

func normalizeISCSIAuthPayload(user string, password string, mutualUser string, mutualPassword string, required bool) (iscsiAuthPayload, error) {
	payload := iscsiAuthPayload{
		User:           strings.TrimSpace(user),
		Password:       strings.TrimSpace(password),
		MutualUser:     strings.TrimSpace(mutualUser),
		MutualPassword: strings.TrimSpace(mutualPassword),
	}
	if err := ValidateISCSIChapUser("user", payload.User, required); err != nil {
		return payload, err
	}
	if err := ValidateISCSIChapSecret("password", payload.Password, required); err != nil {
		return payload, err
	}
	if payload.User == "" || payload.Password == "" {
		if payload.User != payload.Password {
			return payload, fmt.Errorf("user and password must be provided together")
		}
	}
	if payload.MutualUser == "" || payload.MutualPassword == "" {
		if payload.MutualUser != payload.MutualPassword {
			return payload, fmt.Errorf("mutual_user and mutual_password must be provided together")
		}
	}
	if err := ValidateISCSIChapUser("mutual_user", payload.MutualUser, false); err != nil {
		return payload, err
	}
	if err := ValidateISCSIChapSecret("mutual_password", payload.MutualPassword, false); err != nil {
		return payload, err
	}
	return payload, nil
}

func writeISCSIServiceSpec(config iscsiServiceConfig) (string, error) {
	spec := iscsiServiceSpec{
		ServiceType: "iscsi",
		ServiceID:   config.ServiceID,
		Placement: iscsiPlacementSpec{
			Count: config.Count,
			Hosts: config.Hosts,
		},
		Spec: iscsiServiceSpecBody{
			APIPassword:   config.APIPassword,
			APIUser:       config.APIUser,
			APIPort:       config.APIPort,
			Pool:          config.Pool,
			TrustedIPList: strings.Join(config.TrustedIPList, ","),
		},
	}
	raw, err := yaml.Marshal(spec)
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", "ablestack-iscsi-*.yaml")
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

func cephDashboardRequest(ctx context.Context, method string, path string, body any, successStatuses ...int) (any, error) {
	if len(successStatuses) == 0 {
		successStatuses = []int{http.StatusOK}
	}
	baseURL, err := cephDashboardBaseURL(ctx)
	if err != nil {
		return nil, err
	}
	token, err := cephDashboardAuthToken(ctx, baseURL)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	requestURL := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, method, requestURL, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/vnd.ceph.api.v1.0+json")
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := cephDashboardHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if !statusAllowed(resp.StatusCode, successStatuses) {
		return nil, fmt.Errorf("Glue dashboard %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if resp.StatusCode == http.StatusNoContent || len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{"status": "success"}, nil
	}
	return decodeJSON(raw)
}

func statusAllowed(status int, allowed []int) bool {
	for _, value := range allowed {
		if status == value {
			return true
		}
	}
	return false
}

func cephDashboardAuthToken(ctx context.Context, baseURL string) (string, error) {
	user := strings.TrimSpace(os.Getenv(envISCSIDashboardUser))
	if user == "" {
		user = defaultISCSIDashboardUser
	}
	password := strings.TrimSpace(os.Getenv(envISCSIDashboardPassword))
	if password == "" {
		return "", fmt.Errorf("%s is required for Glue dashboard API", envISCSIDashboardPassword)
	}
	payload := map[string]string{"username": user, "password": password}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/auth", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("accept", "application/vnd.ceph.api.v1.0+json")
	resp, err := cephDashboardHTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respRaw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Glue dashboard auth returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respRaw)))
	}
	var token iscsiDashboardToken
	if err := json.Unmarshal(respRaw, &token); err != nil {
		return "", err
	}
	if strings.TrimSpace(token.Token) == "" {
		return "", fmt.Errorf("Glue dashboard auth token is empty")
	}
	return token.Token, nil
}

func cephDashboardBaseURL(ctx context.Context) (string, error) {
	if raw := strings.TrimSpace(os.Getenv(envISCSIDashboardURL)); raw != "" {
		return strings.TrimRight(raw, "/"), nil
	}
	raw, err := runJSON(ctx, "ceph", "mgr", "services", "-f", "json")
	if err != nil {
		return "", err
	}
	services, ok := raw.(map[string]any)
	if !ok {
		return "", fmt.Errorf("ceph mgr services output is not an object")
	}
	dashboard := strings.TrimSpace(mapString(services, "dashboard"))
	if dashboard == "" {
		return "", fmt.Errorf("%s is required or ceph mgr dashboard service must be enabled", envISCSIDashboardURL)
	}
	return strings.TrimRight(dashboard, "/"), nil
}

func cephDashboardHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

func iscsiContainerID(ctx context.Context) (string, error) {
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
		text := image + " " + name
		if !strings.Contains(text, "tcmu") && !strings.Contains(text, "iscsi") {
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

func resolveHostIPs(hosts []string) ([]string, error) {
	raw, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(hosts))
	for _, host := range hosts {
		ip := resolveHostIP(raw, host)
		if ip == "" {
			return nil, fmt.Errorf("host %s not found in /etc/hosts; trusted_ip_list is required", host)
		}
		out = append(out, ip)
	}
	return out, nil
}

func resolveHostIP(raw []byte, host string) string {
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if comment := strings.Index(line, "#"); comment >= 0 {
			line = strings.TrimSpace(line[:comment])
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, name := range fields[1:] {
			if name == host {
				return fields[0]
			}
		}
	}
	return ""
}

func trimStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func parseBoolDefault(value string, defaultValue bool) (bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue, nil
	}
	return strconv.ParseBool(value)
}
