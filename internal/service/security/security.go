package security

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"ablecloud.io/ablestack-api/internal/service/clusterconfig"
)

const (
	InternalTokenHeader = "X-Cube-Internal-Token"
	internalTokenBytes  = 32
)

type ClusterSyncTarget struct {
	Role     string
	Hostname string
	Target   string
}

func GenerateToken() (string, error) {
	raw := make([]byte, internalTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func EnsureInternalToken() (string, bool, error) {
	if token := envInternalToken(); token != "" {
		return token, false, nil
	}
	root, err := loadClusterRoot()
	if err != nil {
		return "", false, err
	}
	if token := internalTokenFromRoot(root); token != "" {
		return token, false, nil
	}
	token, err := GenerateToken()
	if err != nil {
		return "", false, err
	}
	if err := saveInternalToken(root, token); err != nil {
		return "", false, err
	}
	return token, true, nil
}

func GetInternalToken() (string, error) {
	if token := envInternalToken(); token != "" {
		return token, nil
	}
	root, err := loadClusterRoot()
	if err != nil {
		return "", err
	}
	return internalTokenFromRoot(root), nil
}

func RotateInternalToken() (string, error) {
	if envInternalToken() != "" {
		return "", fmt.Errorf("internal token is managed by environment")
	}
	root, err := loadClusterRoot()
	if err != nil {
		return "", err
	}
	token, err := GenerateToken()
	if err != nil {
		return "", err
	}
	if err := saveInternalToken(root, token); err != nil {
		return "", err
	}
	return token, nil
}

func SetInternalToken(token string) error {
	if envInternalToken() != "" {
		return fmt.Errorf("internal token is managed by environment")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("internal token required")
	}
	root, err := loadClusterRoot()
	if err != nil {
		return err
	}
	return saveInternalToken(root, token)
}

func ValidateInternalToken(actual string) bool {
	expected, err := GetInternalToken()
	if err != nil {
		return false
	}
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)
	if expected == "" || actual == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func MaskToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 4 {
		return "****"
	}
	return "****" + token[len(token)-4:]
}

func ClusterAblecubeTargets() ([]string, error) {
	root, err := loadClusterRoot()
	if err != nil {
		return nil, err
	}
	cfg, ok := clusterConfigMap(root)
	if !ok {
		return nil, nil
	}
	rawHosts, ok := cfg["hosts"].([]any)
	if !ok {
		return nil, nil
	}
	seen := map[string]struct{}{}
	targets := make([]string, 0, len(rawHosts))
	for _, raw := range rawHosts {
		host, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		target := clusterString(host["ablecube"])
		if target == "" {
			continue
		}
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	return targets, nil
}

func ClusterAuthSyncTargets(rawOption string) ([]ClusterSyncTarget, error) {
	root, err := loadClusterRoot()
	if err != nil {
		return nil, err
	}
	cfg, ok := clusterConfigMap(root)
	if !ok {
		return nil, nil
	}
	clusterType := clusterString(cfg["type"])
	options, err := normalizeAuthSyncOptions(rawOption, clusterType)
	if err != nil {
		return nil, err
	}

	targets := make([]ClusterSyncTarget, 0)
	for _, option := range options {
		switch option {
		case "host":
			targets = append(targets, clusterHostSyncTargets(cfg)...)
		case "scvm":
			targets = append(targets, clusterSCVMSyncTargets(cfg)...)
		case "ccvm":
			if target, ok := clusterCCVMSyncTarget(cfg); ok {
				targets = append(targets, target)
			}
		}
	}
	return dedupeClusterSyncTargets(targets), nil
}

func normalizeAuthSyncOptions(raw string, clusterType string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "host"
	}

	parts := strings.Split(raw, ",")
	options := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	useAll := false
	for _, part := range parts {
		option := strings.ToLower(strings.TrimSpace(part))
		if option == "" {
			continue
		}
		switch option {
		case "all":
			useAll = true
		case "host", "scvm", "ccvm":
			if !authSyncOptionAllowed(option, clusterType) {
				return nil, fmt.Errorf("unsupported option for cluster type")
			}
			if _, ok := seen[option]; ok {
				continue
			}
			seen[option] = struct{}{}
			options = append(options, option)
		default:
			return nil, fmt.Errorf("invalid option")
		}
	}
	if useAll {
		return authSyncAllOptions(clusterType), nil
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("invalid option")
	}
	return options, nil
}

func authSyncAllOptions(clusterType string) []string {
	switch strings.ToLower(strings.TrimSpace(clusterType)) {
	case "ablestack-vm", "ablestack-standalone":
		return []string{"host", "ccvm"}
	default:
		return []string{"host", "scvm", "ccvm"}
	}
}

func authSyncOptionAllowed(option string, clusterType string) bool {
	if option != "scvm" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(clusterType)) {
	case "ablestack-vm", "ablestack-standalone":
		return false
	default:
		return true
	}
}

func clusterHostSyncTargets(cfg map[string]any) []ClusterSyncTarget {
	hosts, ok := cfg["hosts"].([]any)
	if !ok {
		return nil
	}
	targets := make([]ClusterSyncTarget, 0, len(hosts))
	for _, raw := range hosts {
		host, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		target := clusterString(host["ablecube"])
		hostname := clusterString(host["hostname"])
		if target == "" || hostname == "" {
			continue
		}
		targets = append(targets, ClusterSyncTarget{
			Role:     "host",
			Hostname: hostname,
			Target:   target,
		})
	}
	return targets
}

func clusterSCVMSyncTargets(cfg map[string]any) []ClusterSyncTarget {
	hosts, ok := cfg["hosts"].([]any)
	if !ok {
		return nil
	}
	targets := make([]ClusterSyncTarget, 0, len(hosts))
	for _, raw := range hosts {
		host, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		target := clusterString(host["scvm"])
		if target == "" {
			continue
		}
		index := clusterString(host["index"])
		hostname := "scvm"
		if index != "" {
			hostname += index
		}
		targets = append(targets, ClusterSyncTarget{
			Role:     "scvm",
			Hostname: hostname,
			Target:   target,
		})
	}
	return targets
}

func clusterCCVMSyncTarget(cfg map[string]any) (ClusterSyncTarget, bool) {
	ccvm, ok := cfg["ccvm"].(map[string]any)
	if !ok {
		return ClusterSyncTarget{}, false
	}
	target := clusterString(ccvm["ip"])
	if target == "" {
		return ClusterSyncTarget{}, false
	}
	return ClusterSyncTarget{
		Role:     "ccvm",
		Hostname: "ccvm",
		Target:   target,
	}, true
}

func dedupeClusterSyncTargets(targets []ClusterSyncTarget) []ClusterSyncTarget {
	seen := map[string]struct{}{}
	out := make([]ClusterSyncTarget, 0, len(targets))
	for _, target := range targets {
		key := target.Role + "|" + target.Hostname + "|" + target.Target
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, target)
	}
	return out
}

func clusterConfigMap(root map[string]any) (map[string]any, bool) {
	if cfg, ok := mapFromAny(root["clusterConfig"]); ok {
		return cfg, true
	}
	if _, ok := root["type"]; ok {
		return root, true
	}
	return nil, false
}

func IsLocalTarget(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if strings.EqualFold(target, "localhost") || target == "127.0.0.1" || target == "::1" {
		return true
	}
	ip := net.ParseIP(target)
	if ip == nil {
		return false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		switch typed := addr.(type) {
		case *net.IPNet:
			if typed.IP.Equal(ip) {
				return true
			}
		case *net.IPAddr:
			if typed.IP.Equal(ip) {
				return true
			}
		}
	}
	return false
}

func envInternalToken() string {
	if token := strings.TrimSpace(os.Getenv("CUBE_INTERNAL_TOKEN")); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("ABLESTACK_INTERNAL_TOKEN"))
}

func clusterJSONPath() string {
	if path := strings.TrimSpace(os.Getenv("ABLESTACK_CLUSTER_JSON")); path != "" {
		return path
	}
	base := strings.TrimSpace(os.Getenv("ABLESTACK_CONFIG_PATH"))
	if base == "" {
		base = "/etc/ablestack"
		if _, err := os.Stat(filepath.Join(base, "properties", "cluster.json")); err != nil {
			if _, devErr := os.Stat(filepath.Join("properties", "cluster.json")); devErr == nil {
				return filepath.Join("properties", "cluster.json")
			}
		}
	}
	return filepath.Join(base, "properties", "cluster.json")
}

func loadClusterRoot() (map[string]any, error) {
	path := clusterJSONPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	root := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &root); err != nil {
			return nil, err
		}
	}
	root = clusterconfig.NormalizeClusterJSON(root)
	return root, nil
}

func saveClusterRoot(root map[string]any) error {
	root = clusterconfig.NormalizeClusterJSON(root)
	raw, err := json.MarshalIndent(root, "", "    ")
	if err != nil {
		return err
	}
	path := clusterJSONPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}

func internalTokenFromRoot(root map[string]any) string {
	securityMap, ok := root["security"].(map[string]any)
	if !ok {
		return ""
	}
	return clusterString(securityMap["internal_token"])
}

func saveInternalToken(root map[string]any, token string) error {
	securityMap, ok := root["security"].(map[string]any)
	if !ok {
		securityMap = map[string]any{}
		root["security"] = securityMap
	}
	securityMap["internal_token"] = strings.TrimSpace(token)
	return saveClusterRoot(root)
}

func clusterString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func mapFromAny(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case nil:
		return nil, false
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return nil, false
		}
		out := map[string]any{}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, false
		}
		return out, true
	}
}
