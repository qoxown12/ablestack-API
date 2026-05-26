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
		IscsiStorage:       "false",
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
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
