package cube

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ablecloud.io/ablestack-api/internal/service/licenseservice"
)

func TestSyncLicenseSystemProfileWritesOEMAsLicenseType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cluster.json")
	t.Setenv("ABLESTACK_CLUSTER_JSON", path)
	raw := []byte(`{
		"clusterConfig": {
			"type": "ablestack-vm",
			"backup_path": "",
			"ccvm": {"ip": ""},
			"mngtNic": {"cidr": "", "gw": "", "dns": ""},
			"pcsCluster": {},
			"hosts": [],
			"external_timeserver": "",
			"storage_network": ""
		},
		"systemProfile": {
			"bootstrap": {
				"scvm": "false",
				"ccvm": "false",
				"wall": "false",
				"gfs_configure": "false",
				"local_configure": "false"
			},
			"license": {
				"status": "false",
				"type": ""
			},
			"security_patch": {
				"status": "false"
			}
		}
	}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write cluster.json: %v", err)
	}

	if err := syncLicenseSystemProfile(licenseservice.Status{OEM: "ablecloud"}); err != nil {
		t.Fatalf("syncLicenseSystemProfile error = %v", err)
	}

	root := map[string]any{}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cluster.json: %v", err)
	}
	if err := json.Unmarshal(updated, &root); err != nil {
		t.Fatalf("unmarshal cluster.json: %v", err)
	}

	profile, ok := root["systemProfile"].(map[string]any)
	if !ok {
		t.Fatalf("systemProfile missing: %#v", root["systemProfile"])
	}
	license, ok := profile["license"].(map[string]any)
	if !ok {
		t.Fatalf("systemProfile.license missing: %#v", profile)
	}
	if got := license["status"]; got != "true" {
		t.Fatalf("license.status = %#v, want true", got)
	}
	if got := license["type"]; got != "ablecloud" {
		t.Fatalf("license.type = %#v, want ablecloud", got)
	}
}

func TestSyncLicenseSystemProfileSkipsMissingClusterJSON(t *testing.T) {
	t.Setenv("ABLESTACK_CLUSTER_JSON", filepath.Join(t.TempDir(), "missing-cluster.json"))
	if err := syncLicenseSystemProfile(licenseservice.Status{OEM: "ablecloud"}); err != nil {
		t.Fatalf("syncLicenseSystemProfile error = %v", err)
	}
}
