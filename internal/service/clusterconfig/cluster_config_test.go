package clusterconfig

import (
	"encoding/json"
	"testing"
)

func TestNormalizeClusterJSONMigratesIscsiStorageToStorageNetwork(t *testing.T) {
	root := map[string]any{
		"clusterConfig": map[string]any{
			"type":                "ablestack-vm",
			"ccvm":                map[string]any{"ip": "10.10.31.10"},
			"mngtNic":             map[string]any{"cidr": "16", "gw": "10.10.0.1", "dns": "8.8.8.8"},
			"pcsCluster":          map[string]any{},
			"hosts":               []any{},
			"external_timeserver": "time.google.com",
			"iscsi_storage":       "true",
		},
	}

	normalized := NormalizeClusterJSON(root)
	rawCfg, err := json.Marshal(normalized["clusterConfig"])
	if err != nil {
		t.Fatal(err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(rawCfg, &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg["storage_network"]; got != "true" {
		t.Fatalf("storage_network = %#v, want true", got)
	}
	if _, ok := cfg["iscsi_storage"]; ok {
		t.Fatalf("iscsi_storage should not be present")
	}
}
