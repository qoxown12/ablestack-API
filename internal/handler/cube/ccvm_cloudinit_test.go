package cube

import (
	"reflect"
	"testing"

	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
)

func TestCCVMCloudInitCopyTargetsForVMUsesAblecubeWhenIscsiDisabled(t *testing.T) {
	cfg := ccvmCloudInitTargetTestConfig("ablestack-vm", "false")

	targets, field := ccvmCloudInitCopyTargets(&cfg)

	if field != "hosts[].ablecube" {
		t.Fatalf("field = %q, want hosts[].ablecube", field)
	}
	want := []string{"10.10.31.1", "10.10.31.2"}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets = %#v, want %#v", targets, want)
	}
}

func TestCCVMCloudInitCopyTargetsForVMUsesAblecubePnOnlyWhenIscsiEnabled(t *testing.T) {
	cfg := ccvmCloudInitTargetTestConfig("ablestack-vm", "true")

	targets, field := ccvmCloudInitCopyTargets(&cfg)

	if field != "hosts[].ablecubePn" {
		t.Fatalf("field = %q, want hosts[].ablecubePn", field)
	}
	want := []string{"100.100.31.1", "100.100.31.2"}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets = %#v, want %#v", targets, want)
	}
}

func TestCCVMCloudInitCopyTargetsKeepsAblecubePnForHCI(t *testing.T) {
	cfg := ccvmCloudInitTargetTestConfig("ablestack-hci", "false")

	targets, field := ccvmCloudInitCopyTargets(&cfg)

	if field != "hosts[].ablecubePn" {
		t.Fatalf("field = %q, want hosts[].ablecubePn", field)
	}
	want := []string{"100.100.31.1", "100.100.31.2"}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets = %#v, want %#v", targets, want)
	}
}

func ccvmCloudInitTargetTestConfig(clusterType string, iscsiStorage string) CubeModel.ClusterConfigSection {
	return CubeModel.ClusterConfigSection{
		Type:           clusterType,
		StorageNetwork: iscsiStorage,
		Hosts: []CubeModel.ClusterHost{
			{
				Index:      "1",
				Hostname:   "ablecube31-1",
				Ablecube:   "10.10.31.1",
				AblecubePn: "100.100.31.1",
			},
			{
				Index:      "2",
				Hostname:   "ablecube31-2",
				Ablecube:   "10.10.31.2",
				AblecubePn: "100.100.31.2",
			},
		},
	}
}
