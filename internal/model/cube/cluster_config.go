package cube

import (
	"encoding/json"
	"sync"
)

type TypeClusterConfig struct {
	Data map[string]any `json:"-"`
	mu   *sync.RWMutex
}

var lockClusterConfig sync.Once
var _ClusterConfig *TypeClusterConfig

func ClusterConfig() *TypeClusterConfig {
	lockClusterConfig.Do(func() {
		_ClusterConfig = &TypeClusterConfig{mu: &sync.RWMutex{}}
	})
	return _ClusterConfig
}

func (c *TypeClusterConfig) Lock() {
	if c == nil || c.mu == nil {
		return
	}
	c.mu.Lock()
}

func (c *TypeClusterConfig) Unlock() {
	if c == nil || c.mu == nil {
		return
	}
	c.mu.Unlock()
}

func (c *TypeClusterConfig) RLock() {
	if c == nil || c.mu == nil {
		return
	}
	c.mu.RLock()
}

func (c *TypeClusterConfig) RUnlock() {
	if c == nil || c.mu == nil {
		return
	}
	c.mu.RUnlock()
}

func (c *TypeClusterConfig) ApplyFrom(src map[string]any) {
	if c == nil {
		return
	}
	if c.mu != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
	}
	c.Data = src
}

func (c *TypeClusterConfig) MarshalJSON() ([]byte, error) {
	if c == nil || c.Data == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(c.Data)
}

// Normalize fills legacy json_string from hosts when needed.
func (r *ClusterApplyRequest) Normalize() error {
	if r == nil {
		return nil
	}
	if r.RemoveHostname == "" && r.TargetHostname != "" {
		r.RemoveHostname = r.TargetHostname
	}
	if r.ExternalTimeserver == "" && r.DeprecatedExtenalTimeserver != "" {
		r.ExternalTimeserver = r.DeprecatedExtenalTimeserver
		r.DeprecatedExtenalTimeserver = ""
	}
	if r.CCVM != nil {
		if r.CCVMMngtIP == "" {
			r.CCVMMngtIP = r.CCVM.IP
		}
		// Backward compatibility: older clients sent management NIC values under ccvm.
		if r.MngtNicCIDR == "" {
			r.MngtNicCIDR = r.CCVM.CIDR
		}
		if r.MngtNicDNS == "" {
			r.MngtNicDNS = r.CCVM.DNS
		}
		if r.MngtNicGW == "" {
			r.MngtNicGW = r.CCVM.GW
		}
	}
	if r.MngtNic != nil {
		if r.MngtNic.CIDR != "" {
			r.MngtNicCIDR = r.MngtNic.CIDR
		}
		if r.MngtNic.GW != "" {
			r.MngtNicGW = r.MngtNic.GW
		}
		if r.MngtNic.DNS != "" {
			r.MngtNicDNS = r.MngtNic.DNS
		}
	}
	if r.JSONString == "" && len(r.Hosts) > 0 {
		raw, err := json.Marshal(r.Hosts)
		if err != nil {
			return err
		}
		r.JSONString = string(raw)
	}
	return nil
}

// ClusterApplyRequest describes the request body for cluster apply APIs.
// @name ClusterApplyRequest
type ClusterApplyRequest struct {
	// action: insert/remove/reset/check
	Action string `json:"action" example:"insert"`
	// option (e.g. add/hostOnly/withScvm/all/local)
	Option string `json:"option,omitempty" example:"add"`
	// cluster type (e.g. ablestack-vm, ablestack-hci)
	Type string `json:"type" example:"ablestack-vm"`
	// ccvm config (ip only)
	CCVM *ClusterCCVMConfig `json:"ccvm,omitempty"`
	// management NIC config
	MngtNic *ClusterMngtNicConfig `json:"mngtNic,omitempty"`
	// ccvm management IP (internal)
	CCVMMngtIP string `json:"-"`
	// management NIC CIDR (internal)
	MngtNicCIDR string `json:"-"`
	// management NIC gateway (internal)
	MngtNicGW string `json:"-"`
	// management NIC DNS (internal)
	MngtNicDNS string `json:"-"`
	// pcs cluster node IP list
	PCSClusterList []string `json:"pcs_cluster_list" example:"10.10.31.1,10.10.31.2,10.10.31.3"`
	// legacy hosts JSON string (internal only)
	JSONString string `json:"-"`
	// hosts file copy option (internal)
	CopyOption string `json:"-"`
	// exclude hostname (hci only)
	ExcludeHostname string `json:"exclude_hostname" example:"ablecube31-2"`
	// remove hostname
	RemoveHostname string `json:"remove_hostname" example:"ablecube31-3"`
	// target hostname (alias of remove_hostname)
	TargetHostname string `json:"target_hostname" example:"ablecube31-3"`
	// new hostname (option=add: exclude scvm fanout)
	NewHostname string `json:"new_hostname,omitempty" example:"ablecube12-3"`
	// external timeserver
	ExternalTimeserver string `json:"external_timeserver" example:"time.google.com"`
	// external timeserver (deprecated typo)
	DeprecatedExtenalTimeserver string `json:"extenal_timeserver,omitempty"`
	// iscsi storage usage (true/false)
	IscsiStorage string `json:"iscsi_storage" example:"false"`
	// hosts list (preferred)
	Hosts []ClusterHost `json:"hosts"`
	// existing hostnames from cluster.json (internal)
	ExistingHostnames []string `json:"-"`
}

// ClusterHost describes a single host item.
// @name ClusterHost
type ClusterHost struct {
	Index      string `json:"index" example:"1"`
	Hostname   string `json:"hostname" example:"ablecube31-1"`
	Ablecube   string `json:"ablecube" example:"10.10.31.1"`
	ScvmMngt   string `json:"scvmMngt,omitempty" example:"10.10.31.11"`
	AblecubePn string `json:"ablecubePn,omitempty" example:"100.100.31.1"`
	Scvm       string `json:"scvm,omitempty" example:"100.100.31.11"`
	ScvmCn     string `json:"scvmCn,omitempty" example:"100.200.31.11"`
}

// ClusterApplyResult is a per-target result.
// @name ClusterApplyResult
type ClusterApplyResult struct {
	Target  string `json:"target" example:"10.10.31.1"`
	Code    int    `json:"code" example:"200"`
	Message string `json:"message" example:"ok"`
}

// ClusterApplyResponse is the orchestrator response.
// @name ClusterApplyResponse
type ClusterApplyResponse struct {
	Code    int                  `json:"code" example:"200"`
	Message string               `json:"message" example:"apply success"`
	Results []ClusterApplyResult `json:"results"`
}

// ClusterApplyLocalResponse is the local apply response.
// @name ClusterApplyLocalResponse
type ClusterApplyLocalResponse struct {
	Code int    `json:"code" example:"200"`
	Val  string `json:"val" example:"Cluster Config insert Success"`
}

// ClusterHealthTargetResult is a per-target health result.
// @name ClusterHealthTargetResult
type ClusterHealthTargetResult struct {
	// target role (host/scvm/ccvm)
	Role string `json:"role" example:"host"`
	// hostname for host/scvm targets
	Hostname string `json:"hostname,omitempty" example:"ablecube12-1"`
	// target IP
	Target string `json:"target" example:"10.10.31.1"`
	// response code
	Code int `json:"code" example:"200"`
	// response message
	Message string `json:"message" example:"ok"`
}

// ClusterHealthResponse is the health response.
// @name ClusterHealthResponse
type ClusterHealthResponse struct {
	// ok status for default health
	Status string `json:"status,omitempty" example:"ok"`
	// result code for option-based checks
	Code int `json:"code,omitempty" example:"200"`
	// result message for option-based checks
	Message string `json:"message,omitempty" example:"health check success"`
	// per-target results
	Results []ClusterHealthTargetResult `json:"results,omitempty"`
}

// ClusterConfigResponse is the cluster.json response body.
// @name ClusterConfigResponse
type ClusterConfigResponse struct {
	// cluster configuration data
	ClusterConfig ClusterConfigSection `json:"clusterConfig"`
	// system profile data
	SystemProfile ClusterSystemProfile `json:"systemProfile"`
}

// ClusterConfigSection describes the clusterConfig block.
// @name ClusterConfigSection
type ClusterConfigSection struct {
	// cluster type
	Type string `json:"type" example:"ablestack-vm"`
	// backup path for ccvm
	BackupPath string `json:"backup_path" example:"/mnt/glue-gfs/backup/ccvm"`
	// ccvm config
	CCVM ClusterCCVMConfig `json:"ccvm"`
	// management NIC config
	MngtNic ClusterMngtNicConfig `json:"mngtNic"`
	// pcs cluster IPs
	PCSCluster ClusterPCSClusterConfig `json:"pcsCluster"`
	// host list
	Hosts []ClusterHost `json:"hosts"`
	// external timeserver
	ExternalTimeserver string `json:"external_timeserver" example:"time.google.com"`
	// iscsi storage usage
	IscsiStorage string `json:"iscsi_storage" example:"false"`
}

// ClusterCCVMConfig describes ccvm network info.
// @name ClusterCCVMConfig
type ClusterCCVMConfig struct {
	// ccvm IP
	IP string `json:"ip" example:"10.10.31.10"`
	// management NIC CIDR (deprecated: use mngtNic.cidr)
	CIDR string `json:"cidr,omitempty" swaggerignore:"true"`
	// management NIC gateway (deprecated: use mngtNic.gw)
	GW string `json:"gw,omitempty" swaggerignore:"true"`
	// management NIC DNS (deprecated: use mngtNic.dns)
	DNS string `json:"dns,omitempty" swaggerignore:"true"`
}

// ClusterMngtNicConfig describes management NIC network info.
// @name ClusterMngtNicConfig
type ClusterMngtNicConfig struct {
	// management NIC CIDR
	CIDR string `json:"cidr" example:"16"`
	// management NIC gateway
	GW string `json:"gw" example:"10.10.0.1"`
	// management NIC DNS
	DNS string `json:"dns" example:"8.8.8.8"`
}

// ClusterPCSClusterConfig describes pcs cluster IPs.
// @name ClusterPCSClusterConfig
type ClusterPCSClusterConfig struct {
	// pcs cluster node #1
	Hostname1 string `json:"hostname1" example:"10.10.31.1"`
	// pcs cluster node #2
	Hostname2 string `json:"hostname2" example:"10.10.31.2"`
	// pcs cluster node #3
	Hostname3 string `json:"hostname3" example:"10.10.31.3"`
}

// ClusterBootstrapConfig describes bootstrap flags.
// @name ClusterBootstrapConfig
type ClusterBootstrapConfig struct {
	// scvm bootstrap flag
	Scvm string `json:"scvm" example:"false"`
	// ccvm bootstrap flag
	Ccvm string `json:"ccvm" example:"true"`
	// wall configuration flag
	Wall string `json:"wall" example:"true"`
	// gfs configure flag
	GFSConfigure string `json:"gfs_configure" example:"true"`
	// local configure flag
	LocalConfigure string `json:"local_configure" example:"false"`
}

// ClusterSystemProfile describes system profile blocks.
// @name ClusterSystemProfile
type ClusterSystemProfile struct {
	// bootstrap flags
	Bootstrap ClusterBootstrapConfig `json:"bootstrap"`
	// license info
	License ClusterLicenseConfig `json:"license"`
	// security patch status
	SecurityPatch ClusterSecurityPatchConfig `json:"security_patch"`
}

// ClusterLicenseConfig describes license info.
// @name ClusterLicenseConfig
type ClusterLicenseConfig struct {
	// license status
	Status string `json:"status" example:"true"`
	// license type
	Type string `json:"type" example:"ablestack"`
}

// ClusterSecurityPatchConfig describes security patch status.
// @name ClusterSecurityPatchConfig
type ClusterSecurityPatchConfig struct {
	// security patch status
	Status string `json:"status" example:"false"`
}

// SystemConfigRequest describes the request body for system config APIs.
// @name SystemConfigRequest
type SystemConfigRequest struct {
	// action: status/update/allUpdate/reset
	Action string `json:"action" example:"status"`
	// option: all (fan-out to all ablecube hosts)
	Option string `json:"option,omitempty" example:"all"`
	// depth1 key (bootstrap/license/security_patch)
	Depth1 string `json:"depth1,omitempty" example:"bootstrap"`
	// depth2 key (e.g. scvm/ccvm/wall/gfs_configure/local_configure/status/type)
	Depth2 string `json:"depth2,omitempty" example:"scvm"`
	// value for update
	Value string `json:"value,omitempty" example:"true"`
}

// SystemConfigResponse describes the response body for system config APIs.
// @name SystemConfigResponse
type SystemConfigResponse struct {
	Code int `json:"code" example:"200"`
	Val  any `json:"val"`
}
