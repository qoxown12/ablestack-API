package cube

import (
	"encoding/json"
	"testing"
)

func TestClusterApplyRequestNormalizeUsesMngtNic(t *testing.T) {
	req := &ClusterApplyRequest{
		CCVM:    &ClusterCCVMConfig{IP: "10.10.31.10", CIDR: "24"},
		MngtNic: &ClusterMngtNicConfig{CIDR: "16", GW: "10.10.0.1", DNS: "8.8.8.8"},
	}

	if err := req.Normalize(); err != nil {
		t.Fatal(err)
	}

	if req.CCVMMngtIP != "10.10.31.10" {
		t.Fatalf("CCVMMngtIP = %q", req.CCVMMngtIP)
	}
	if req.MngtNicCIDR != "16" || req.MngtNicGW != "10.10.0.1" || req.MngtNicDNS != "8.8.8.8" {
		t.Fatalf("unexpected management NIC values: cidr=%q gw=%q dns=%q", req.MngtNicCIDR, req.MngtNicGW, req.MngtNicDNS)
	}
}

func TestClusterApplyRequestNormalizeAcceptsLegacyIscsiStorage(t *testing.T) {
	req := &ClusterApplyRequest{
		DeprecatedIscsiStorage: "true",
	}

	if err := req.Normalize(); err != nil {
		t.Fatal(err)
	}

	if req.StorageNetwork != "true" {
		t.Fatalf("StorageNetwork = %q, want true", req.StorageNetwork)
	}
	if req.DeprecatedIscsiStorage != "" {
		t.Fatalf("DeprecatedIscsiStorage = %q, want empty", req.DeprecatedIscsiStorage)
	}
}

func TestClusterConfigSectionMarshalOmitsEmptyVMScvmFields(t *testing.T) {
	cfg := ClusterConfigSection{
		Type:       "ablestack-vm",
		BackupPath: "/mnt/glue-gfs/backup/ccvm",
		CCVM:       ClusterCCVMConfig{IP: "10.10.31.10"},
		MngtNic:    ClusterMngtNicConfig{CIDR: "16", GW: "10.10.0.1", DNS: "8.8.8.8"},
		Hosts: []ClusterHost{
			{Index: "1", Hostname: "ablecube31-1", Ablecube: "10.10.31.1"},
		},
		ExternalTimeserver: "time.google.com",
		StorageNetwork:     "false",
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if got := out["storage_network"]; got != "false" {
		t.Fatalf("storage_network = %#v, want false", got)
	}
	if _, ok := out["iscsi_storage"]; ok {
		t.Fatalf("iscsi_storage should not be present")
	}

	ccvm := out["ccvm"].(map[string]any)
	for _, key := range []string{"cidr", "gw", "dns"} {
		if _, ok := ccvm[key]; ok {
			t.Fatalf("ccvm.%s should not be present", key)
		}
	}

	hosts := out["hosts"].([]any)
	host := hosts[0].(map[string]any)
	for _, key := range []string{"scvmMngt", "ablecubePn", "scvm", "scvmCn"} {
		if _, ok := host[key]; ok {
			t.Fatalf("vm host field %s should not be present", key)
		}
	}
}

func TestClusterConfigSectionUnmarshalAcceptsLegacyIscsiStorage(t *testing.T) {
	var cfg ClusterConfigSection
	if err := json.Unmarshal([]byte(`{"type":"ablestack-vm","iscsi_storage":"true"}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.StorageNetwork != "true" {
		t.Fatalf("StorageNetwork = %q, want true", cfg.StorageNetwork)
	}
	if cfg.DeprecatedIscsiStorage != "" {
		t.Fatalf("DeprecatedIscsiStorage = %q, want empty", cfg.DeprecatedIscsiStorage)
	}
}

func TestClusterPCSClusterConfigDynamicHostnames(t *testing.T) {
	var cfg ClusterPCSClusterConfig
	if err := json.Unmarshal([]byte(`{"hostname1":"10.0.0.1","hostname4":"10.0.0.4","hostname2":"10.0.0.2"}`), &cfg); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}

	hostnames := cfg.HostnameList()
	if len(hostnames) != 3 {
		t.Fatalf("HostnameList() length = %d, want 3", len(hostnames))
	}
	if hostnames[0] != "10.0.0.1" || hostnames[1] != "10.0.0.2" || hostnames[2] != "10.0.0.4" {
		t.Fatalf("HostnameList() = %#v", hostnames)
	}

	out, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	var raw map[string]string
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("marshal output unmarshal error = %v", err)
	}
	if raw["hostname3"] != "10.0.0.4" {
		t.Fatalf("hostname3 = %q, want 10.0.0.4", raw["hostname3"])
	}
}

func TestValidatePCSClusterList(t *testing.T) {
	if err := ValidatePCSClusterList(nil, 0); err != nil {
		t.Fatalf("ValidatePCSClusterList() standalone-style optional list error = %v", err)
	}
	if err := ValidatePCSClusterList([]string{"10.0.0.1"}, 1); err != nil {
		t.Fatalf("ValidatePCSClusterList() error = %v", err)
	}
	if err := ValidatePCSClusterList([]string{"10.0.0.1"}, 3); err == nil {
		t.Fatalf("ValidatePCSClusterList() expected min host error")
	}

	values := make([]string, 0, PCSClusterMaxHosts+1)
	for i := 0; i <= PCSClusterMaxHosts; i++ {
		values = append(values, string(rune('a'+i)))
	}
	if err := ValidatePCSClusterList(values, 1); err == nil {
		t.Fatalf("ValidatePCSClusterList() expected max host error")
	}
}
