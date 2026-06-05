package glue

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	GlueModel "ablecloud.io/ablestack-api/internal/model/glue"
	"ablecloud.io/ablestack-api/internal/service/clusterconfig"
	"github.com/gin-gonic/gin"
)

const (
	nodeRoleSCVM = "scvm"
	nodeRoleHost = "host"
	nodeRoleCCVM = "ccvm"

	defaultAbleStackConfigPath = "/etc/ablestack"
	defaultNodeRolePath        = "/etc/ablestack/node-role"
)

// RequireSCVM은 물리 host와 CCVM에서 Glue API 호출을 차단한다.
func RequireSCVM() gin.HandlerFunc {
	return func(context *gin.Context) {
		status := DetectNodeRole()
		if status.SCVM {
			context.Next()
			return
		}
		context.AbortWithStatusJSON(http.StatusForbidden, Response{
			Code:    http.StatusForbidden,
			Message: "glue api is only available on scvm",
			Val:     status,
		})
	}
}

func DetectNodeRole() GlueModel.NodeRoleStatus {
	if status, ok := detectNodeRoleFromEnv(); ok {
		return status
	}
	if status, ok := detectNodeRoleFromFile(); ok {
		return status
	}
	if status, ok := detectNodeRoleFromClusterJSON(); ok {
		return status
	}
	return GlueModel.NodeRoleStatus{
		Role:   "unknown",
		Source: "none",
		SCVM:   false,
		Reason: "ABLESTACK_NODE_ROLE, node-role file, and cluster.json did not identify this node as scvm",
	}
}

func detectNodeRoleFromEnv() (GlueModel.NodeRoleStatus, bool) {
	raw := strings.TrimSpace(os.Getenv("ABLESTACK_NODE_ROLE"))
	if raw == "" {
		return GlueModel.NodeRoleStatus{}, false
	}
	role, scvm := normalizeNodeRole(raw)
	return GlueModel.NodeRoleStatus{
		Role:   role,
		Source: "env",
		SCVM:   scvm,
		Reason: "ABLESTACK_NODE_ROLE=" + raw,
	}, true
}

func detectNodeRoleFromFile() (GlueModel.NodeRoleStatus, bool) {
	path := strings.TrimSpace(os.Getenv("ABLESTACK_NODE_ROLE_FILE"))
	if path == "" {
		path = defaultNodeRolePath
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return GlueModel.NodeRoleStatus{}, false
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return GlueModel.NodeRoleStatus{}, false
	}
	role, scvm := normalizeNodeRole(value)
	return GlueModel.NodeRoleStatus{
		Role:   role,
		Source: "file",
		SCVM:   scvm,
		Reason: path + "=" + value,
	}, true
}

func detectNodeRoleFromClusterJSON() (GlueModel.NodeRoleStatus, bool) {
	root, err := readClusterJSONRoot()
	if err != nil {
		return GlueModel.NodeRoleStatus{}, false
	}
	normalized := clusterconfig.NormalizeClusterJSON(root)
	rawCfg, ok := normalized["clusterConfig"]
	if !ok {
		return GlueModel.NodeRoleStatus{}, false
	}
	raw, err := json.Marshal(rawCfg)
	if err != nil {
		return GlueModel.NodeRoleStatus{}, false
	}
	var cfg struct {
		CCVM struct {
			IP string `json:"ip"`
		} `json:"ccvm"`
		Hosts []struct {
			Hostname string `json:"hostname"`
			Ablecube string `json:"ablecube"`
			ScvmMngt string `json:"scvmMngt"`
			Scvm     string `json:"scvm"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return GlueModel.NodeRoleStatus{}, false
	}

	localIPs := localIPSet()
	for _, host := range cfg.Hosts {
		if hasIP(localIPs, host.ScvmMngt, host.Scvm) {
			return GlueModel.NodeRoleStatus{
				Role:   nodeRoleSCVM,
				Source: "cluster.json",
				SCVM:   true,
				Reason: "local IP matches hosts[].scvmMngt/scvm for " + strings.TrimSpace(host.Hostname),
			}, true
		}
	}
	for _, host := range cfg.Hosts {
		if hasIP(localIPs, host.Ablecube) {
			return GlueModel.NodeRoleStatus{
				Role:   nodeRoleHost,
				Source: "cluster.json",
				SCVM:   false,
				Reason: "local IP matches hosts[].ablecube for " + strings.TrimSpace(host.Hostname),
			}, true
		}
	}
	if hasIP(localIPs, cfg.CCVM.IP) {
		return GlueModel.NodeRoleStatus{
			Role:   nodeRoleCCVM,
			Source: "cluster.json",
			SCVM:   false,
			Reason: "local IP matches clusterConfig.ccvm.ip",
		}, true
	}
	return GlueModel.NodeRoleStatus{}, false
}

func normalizeNodeRole(raw string) (string, bool) {
	role := strings.ToLower(strings.TrimSpace(raw))
	switch role {
	case "scvm", "storage", "storage-vm", "storage_vm":
		return nodeRoleSCVM, true
	case "ccvm", "cloud", "cloud-vm", "cloud_vm":
		return nodeRoleCCVM, false
	case "host", "ablecube", "cube", "physical", "node":
		return nodeRoleHost, false
	default:
		return role, false
	}
}

func readClusterJSONRoot() (map[string]any, error) {
	path := strings.TrimSpace(os.Getenv("ABLESTACK_CLUSTER_JSON"))
	if path == "" {
		base := strings.TrimSpace(os.Getenv("ABLESTACK_CONFIG_PATH"))
		if base == "" {
			base = defaultAbleStackConfigPath
		}
		path = filepath.Join(base, "properties", "cluster.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	root := map[string]any{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

func localIPSet() map[string]struct{} {
	out := map[string]struct{}{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, addr := range addrs {
		switch v := addr.(type) {
		case *net.IPNet:
			if ip := normalizeIP(v.IP.String()); ip != "" {
				out[ip] = struct{}{}
			}
		case *net.IPAddr:
			if ip := normalizeIP(v.IP.String()); ip != "" {
				out[ip] = struct{}{}
			}
		}
	}
	return out
}

func hasIP(localIPs map[string]struct{}, values ...string) bool {
	for _, value := range values {
		if _, ok := localIPs[normalizeIP(value)]; ok {
			return true
		}
	}
	return false
}

func normalizeIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	host := value
	if strings.Contains(value, "/") {
		if ip, _, err := net.ParseCIDR(value); err == nil {
			host = ip.String()
		}
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}
	return ip.String()
}
