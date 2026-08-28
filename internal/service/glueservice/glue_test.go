package glueservice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestListPoolsFiltersWithoutShell(t *testing.T) {
	withFakeRunner(t, map[string][]byte{
		"ceph osd pool ls --format json": []byte(`["rbd","rbd_data","cephfs_data"]`),
	})

	got, err := ListPools(context.Background(), "rbd")
	if err != nil {
		t.Fatalf("ListPools returned error: %v", err)
	}
	want := []string{"rbd", "rbd_data"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListPools = %#v, want %#v", got, want)
	}
}

func TestListImagesAllRBDPools(t *testing.T) {
	withFakeRunner(t, map[string][]byte{
		"ceph osd pool ls --format json":   []byte(`["rbd","rbd_data","cephfs_data"]`),
		"rbd ls -p rbd --format json":      []byte(`["vm01","vm02"]`),
		"rbd ls -p rbd_data --format json": []byte(`["data01"]`),
	})

	got, err := ListImages(context.Background(), "", "")
	if err != nil {
		t.Fatalf("ListImages returned error: %v", err)
	}
	want := []string{"rbd/vm01", "rbd/vm02", "rbd_data/data01"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListImages = %#v, want %#v", got, want)
	}
}

func TestListHostsAddsManagementAddresses(t *testing.T) {
	withFakeRunner(t, map[string][]byte{
		"ceph orch host ls -f json": []byte(`[
			{"hostname":"scvm1","addr":"100.100.33.11"},
			{"hostname":"scvm2-mngt","addr":"100.100.33.12"},
			{"hostname":"other","addr":"10.0.0.10"}
		]`),
	})

	oldReader := hostsFileReader
	hostsFileReader = func() ([]byte, error) {
		return []byte(`
# management addresses
10.10.33.11 scvm1-mngt scvm-mngt
10.10.33.12 scvm2-mngt
10.10.33.13 scvm3-mngt # inline comment
`), nil
	}
	t.Cleanup(func() { hostsFileReader = oldReader })

	value, err := ListHosts(context.Background())
	if err != nil {
		t.Fatalf("ListHosts returned error: %v", err)
	}
	hosts, ok := value.([]any)
	if !ok || len(hosts) != 3 {
		t.Fatalf("ListHosts = %#v, want three hosts", value)
	}

	want := []string{"10.10.33.11", "10.10.33.12", ""}
	for i, host := range hosts {
		entry, ok := host.(map[string]any)
		if !ok {
			t.Fatalf("host[%d] = %#v, want object", i, host)
		}
		if got := entry["mgmtaddr"]; got != want[i] {
			t.Fatalf("host[%d].mgmtaddr = %#v, want %q", i, got, want[i])
		}
	}
}

func TestManagementAddressesFromHostsIgnoresInvalidAndDuplicateEntries(t *testing.T) {
	got := managementAddressesFromHosts([]byte(`
not-an-ip scvm1-mngt
10.10.33.11 scvm1-mngt
10.10.33.99 scvm1-mngt
10.10.33.12 scvm2-mngt
10.10.33.13 scvm3-mngt other
`))

	want := map[string]string{
		"scvm1-mngt": "10.10.33.11",
		"scvm2-mngt": "10.10.33.12",
		"scvm3-mngt": "10.10.33.13",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("managementAddressesFromHosts = %#v, want %#v", got, want)
	}
}

func TestCreateImageBuildsRBDCreateCommand(t *testing.T) {
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"rbd create --size 10240 rbd/vm01": []byte(""),
	})

	got, err := CreateImage(context.Background(), "rbd", "vm01", 10)
	if err != nil {
		t.Fatalf("CreateImage returned error: %v", err)
	}
	if got["size_mib"] != int64(10240) {
		t.Fatalf("size_mib = %#v, want %d", got["size_mib"], int64(10240))
	}
	wantCommands := []string{"rbd create --size 10240 rbd/vm01"}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}
}

func TestResizeImageAcceptsPoolImageReferenceAndBuildsCommands(t *testing.T) {
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"rbd info rbd/iscsi-2 --format json":  []byte(`{"size":21474836480}`),
		"rbd resize --size 25600 rbd/iscsi-2": []byte(""),
	})

	got, err := ResizeImage(context.Background(), "", "rbd/iscsi-2", 25)
	if err != nil {
		t.Fatalf("ResizeImage returned error: %v", err)
	}
	if got["pool"] != "rbd" || got["image"] != "iscsi-2" || got["size_gib"] != int64(25) || got["previous_size_gib"] != int64(20) {
		t.Fatalf("ResizeImage = %#v, want resized image details", got)
	}
	want := []string{
		"rbd info rbd/iscsi-2 --format json",
		"rbd resize --size 25600 rbd/iscsi-2",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestResizeImageRejectsShrinkOrSameSize(t *testing.T) {
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"rbd info rbd/iscsi-2 --format json": []byte(`{"size":21474836480}`),
	})

	if _, err := ResizeImage(context.Background(), "rbd", "iscsi-2", 20); err == nil || !strings.Contains(err.Error(), "greater than current image size") {
		t.Fatalf("ResizeImage error = %v, want current size validation error", err)
	}
	if !reflect.DeepEqual(commands, []string{"rbd info rbd/iscsi-2 --format json"}) {
		t.Fatalf("commands = %#v, want info only", commands)
	}
}

func TestDeletePoolEnablesPoolDeleteWhenNeeded(t *testing.T) {
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"ceph config get mon mon_allow_pool_delete":                        []byte("false\n"),
		"ceph config set mon mon_allow_pool_delete true":                   []byte(""),
		"ceph osd pool rm rbd_data rbd_data --yes-i-really-really-mean-it": []byte(""),
	})

	if _, err := DeletePool(context.Background(), "rbd_data"); err != nil {
		t.Fatalf("DeletePool returned error: %v", err)
	}
	want := []string{
		"ceph config get mon mon_allow_pool_delete",
		"ceph config set mon mon_allow_pool_delete true",
		"ceph osd pool rm rbd_data rbd_data --yes-i-really-really-mean-it",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestValidateRejectsUnsafeNames(t *testing.T) {
	if err := ValidatePoolName("rbd;rm -rf /"); err == nil {
		t.Fatalf("ValidatePoolName accepted unsafe value")
	}
	if err := ValidateImageName("../vm01"); err == nil {
		t.Fatalf("ValidateImageName accepted unsafe value")
	}
	if err := ValidateServiceName("rgw.default && reboot"); err == nil {
		t.Fatalf("ValidateServiceName accepted unsafe value")
	}
}

func TestGlueFSStatusBuildsStatusAndListCommands(t *testing.T) {
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"ceph fs status -f json": []byte(`{"filesystems":[{"name":"gluefs"}]}`),
		"ceph fs ls -f json":     []byte(`[{"name":"gluefs"}]`),
	})

	got, err := GlueFSStatus(context.Background())
	if err != nil {
		t.Fatalf("GlueFSStatus returned error: %v", err)
	}
	if got["fs_status"] == nil || got["fs_list"] == nil {
		t.Fatalf("GlueFSStatus = %#v, want fs_status and fs_list", got)
	}
	want := []string{"ceph fs status -f json", "ceph fs ls -f json"}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestGlueFSSubvolumeGroupsCollectsDetails(t *testing.T) {
	withFakeRunner(t, map[string][]byte{
		"ceph fs subvolumegroup ls gluefs":               []byte(`[{"name":"grp1"}]`),
		"ceph fs subvolumegroup info gluefs grp1":        []byte(`{"bytes_used":10}`),
		"ceph fs subvolumegroup getpath gluefs grp1":     []byte(`/volumes/grp1`),
		"ceph fs subvolumegroup snapshot ls gluefs grp1": []byte(`[{"name":"snap1"}]`),
	})

	got, err := GlueFSSubvolumeGroups(context.Background(), "gluefs")
	if err != nil {
		t.Fatalf("GlueFSSubvolumeGroups returned error: %v", err)
	}
	if len(got) != 1 || got[0]["name"] != "grp1" || got[0]["path"] != "/volumes/grp1" {
		t.Fatalf("GlueFSSubvolumeGroups = %#v", got)
	}
}

func TestNFSExportsWithoutClusterListsAllClusters(t *testing.T) {
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"ceph nfs cluster ls":                 []byte(`["nfs-a","nfs-b"]`),
		"ceph nfs export ls nfs-a --detailed": []byte(`[{"cluster_id":"nfs-a","pseudo":"/a"}]`),
		"ceph nfs export ls nfs-b --detailed": []byte(`[{"cluster_id":"nfs-b","pseudo":"/b"}]`),
	})

	got, err := NFSExports(context.Background(), "")
	if err != nil {
		t.Fatalf("NFSExports returned error: %v", err)
	}
	values, ok := got.([]any)
	if !ok || len(values) != 2 {
		t.Fatalf("NFSExports = %#v, want two exports", got)
	}
	wantCommands := []string{
		"ceph nfs cluster ls",
		"ceph nfs export ls nfs-a --detailed",
		"ceph nfs export ls nfs-b --detailed",
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}
}

func TestRGWUsersIncludesStats(t *testing.T) {
	withFakeRunner(t, map[string][]byte{
		"radosgw-admin user list":              []byte(`["admin"]`),
		"radosgw-admin user info --uid admin":  []byte(`{"user_id":"admin"}`),
		"radosgw-admin user stats --uid admin": []byte(`{"stats":{"total_bytes":1}}`),
	})

	got, err := RGWUsers(context.Background(), "")
	if err != nil {
		t.Fatalf("RGWUsers returned error: %v", err)
	}
	users, ok := got.([]map[string]any)
	if !ok || len(users) != 1 || users[0]["user_id"] != "admin" || users[0]["stats"] == nil {
		t.Fatalf("RGWUsers = %#v", got)
	}
}

func TestRGWBucketDetailUsesStatsCommand(t *testing.T) {
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"radosgw-admin bucket stats --bucket bucket-a": []byte(`{"bucket":"bucket-a"}`),
	})

	if _, err := RGWBuckets(context.Background(), "bucket-a", true); err != nil {
		t.Fatalf("RGWBuckets returned error: %v", err)
	}
	want := []string{"radosgw-admin bucket stats --bucket bucket-a"}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestRGWBucketDeleteReturnsNonEmptyWarning(t *testing.T) {
	withCustomRunner(t, func(ctx context.Context, command string, args ...string) ([]byte, bool, error) {
		return []byte("ERROR: Bucket is not empty. Remove all objects before deletion."), false, errors.New("exit status 1")
	})

	_, err := RGWBucketDelete(context.Background(), "bucket-a")
	if err == nil {
		t.Fatalf("RGWBucketDelete returned nil error")
	}
	if got := err.Error(); got != rgwBucketNotEmptyMessage {
		t.Fatalf("error = %q, want %q", got, rgwBucketNotEmptyMessage)
	}
}

func TestNVMeOfServiceCreateBuildsCephOrchSpec(t *testing.T) {
	commands := []string{}
	withCustomRunner(t, func(ctx context.Context, command string, args ...string) ([]byte, bool, error) {
		key := commandLine(command, args)
		commands = append(commands, key)
		switch {
		case key == "ceph osd pool create nvmeof":
			return []byte(""), false, nil
		case key == "rbd pool init nvmeof":
			return []byte(""), false, nil
		case key == "ceph osd pool set nvmeof size 2":
			return []byte(""), false, nil
		case strings.HasPrefix(key, "ceph orch apply -i "):
			raw, err := os.ReadFile(args[3])
			if err != nil {
				return nil, false, err
			}
			spec := string(raw)
			if !strings.Contains(spec, "service_type: nvmeof") ||
				!strings.Contains(spec, "service_id: nvmeof") ||
				!strings.Contains(spec, "pool: nvmeof") ||
				!strings.Contains(spec, "scvm") {
				return nil, false, errors.New("unexpected nvmeof spec: " + spec)
			}
			return []byte(""), false, nil
		default:
			return nil, false, errors.New("unexpected command: " + key)
		}
	})

	if _, err := NVMeOfServiceCreate(context.Background(), "nvmeof", []string{"scvm"}, ""); err != nil {
		t.Fatalf("NVMeOfServiceCreate returned error: %v", err)
	}
	wantPrefix := []string{
		"ceph osd pool create nvmeof",
		"rbd pool init nvmeof",
		"ceph osd pool set nvmeof size 2",
	}
	if len(commands) != 4 {
		t.Fatalf("commands = %#v, want 4 commands", commands)
	}
	for i, want := range wantPrefix {
		if commands[i] != want {
			t.Fatalf("commands[%d] = %q, want %q", i, commands[i], want)
		}
	}
	if !strings.HasPrefix(commands[3], "ceph orch apply -i ") {
		t.Fatalf("commands[3] = %q, want ceph orch apply", commands[3])
	}
}

func TestNVMeOfSubsystemCreateUsesLocalPodman(t *testing.T) {
	t.Setenv(envNVMeOfServerAddress, "10.10.10.10")
	commands := []string{}
	nqn := "nqn.2014-08.org.nvmexpress:uuid:subsys01"
	withRecordingRunner(t, &commands, map[string][]byte{
		"ceph orch ps --daemon_type nvmeof -f json": []byte(`[{"hostname":"scvm","daemon_name":"nvmeof.scvm"}]`),
		"podman run --rm -i localhost:15000/glue/nvmeof-cli:Diplo --server-address 10.10.10.10 --server-port 5500 subsystem add --subsystem " + nqn:                                                                        []byte(""),
		"podman run --rm -i localhost:15000/glue/nvmeof-cli:Diplo --server-address 10.10.10.10 --server-port 5500 listener add --subsystem " + nqn + " --host-name client.nvmeof.scvm --traddr 10.10.10.11 --trsvcid 4420": []byte(""),
		"podman run --rm -i localhost:15000/glue/nvmeof-cli:Diplo --server-address 10.10.10.10 --server-port 5500 host add --subsystem " + nqn + " --host *":                                                               []byte(""),
	})

	if _, err := NVMeOfSubsystemCreate(context.Background(), "10.10.10.11", "client.nvmeof.scvm", nqn); err != nil {
		t.Fatalf("NVMeOfSubsystemCreate returned error: %v", err)
	}
	want := []string{
		"ceph orch ps --daemon_type nvmeof -f json",
		"podman run --rm -i localhost:15000/glue/nvmeof-cli:Diplo --server-address 10.10.10.10 --server-port 5500 subsystem add --subsystem " + nqn,
		"podman run --rm -i localhost:15000/glue/nvmeof-cli:Diplo --server-address 10.10.10.10 --server-port 5500 listener add --subsystem " + nqn + " --host-name client.nvmeof.scvm --traddr 10.10.10.11 --trsvcid 4420",
		"podman run --rm -i localhost:15000/glue/nvmeof-cli:Diplo --server-address 10.10.10.10 --server-port 5500 host add --subsystem " + nqn + " --host *",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestNVMeOfNamespaceCreateCreatesImageThenNamespace(t *testing.T) {
	t.Setenv(envNVMeOfServerAddress, "10.10.10.10")
	commands := []string{}
	nqn := "nqn.2014-08.org.nvmexpress:uuid:subsys01"
	withRecordingRunner(t, &commands, map[string][]byte{
		"ceph orch ps --daemon_type nvmeof -f json": []byte(`[{"hostname":"scvm","daemon_name":"nvmeof.scvm"}]`),
		"rbd create --size 10240 rbd/vm01":          []byte(""),
		"podman run --rm -i localhost:15000/glue/nvmeof-cli:Diplo --server-address 10.10.10.10 --server-port 5500 namespace add --subsystem " + nqn + " --rbd-pool rbd --rbd-image vm01": []byte(""),
	})

	if _, err := NVMeOfNamespaceCreate(context.Background(), nqn, "rbd", "vm01", 10); err != nil {
		t.Fatalf("NVMeOfNamespaceCreate returned error: %v", err)
	}
	want := []string{
		"ceph orch ps --daemon_type nvmeof -f json",
		"rbd create --size 10240 rbd/vm01",
		"podman run --rm -i localhost:15000/glue/nvmeof-cli:Diplo --server-address 10.10.10.10 --server-port 5500 namespace add --subsystem " + nqn + " --rbd-pool rbd --rbd-image vm01",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestNVMeOfTargetListUsesLocalPodmanExec(t *testing.T) {
	t.Setenv(envNVMeOfServerAddress, "10.10.10.10")
	commands := []string{}
	nqn := "nqn.2014-08.org.nvmexpress:uuid:subsys01"
	withRecordingRunner(t, &commands, map[string][]byte{
		"ceph orch ps --daemon_type nvmeof -f json": []byte(`[{"hostname":"scvm","daemon_name":"nvmeof.scvm"}]`),
		"podman ps --format json":                   []byte(`[{"ID":"abc123","Image":"localhost:15000/glue/nvmeof:Diplo","Names":"nvmeof.scvm"}]`),
		"podman exec -i abc123 python3 /usr/libexec/spdk/scripts/rpc.py nvmf_get_subsystems":                                                                       []byte(`[{"nqn":"` + nqn + `"}]`),
		"podman exec -i abc123 python3 /usr/libexec/spdk/scripts/rpc.py nvmf_subsystem_get_controllers " + nqn:                                                     []byte(`[{"cntlid":1}]`),
		"podman run --rm -i localhost:15000/glue/nvmeof-cli:Diplo --format json --server-address 10.10.10.10 --server-port 5500 namespace list --subsystem " + nqn: []byte(`{"namespaces":[]}`),
	})

	got, err := NVMeOfTargetList(context.Background(), "")
	if err != nil {
		t.Fatalf("NVMeOfTargetList returned error: %v", err)
	}
	targets, ok := got.([]any)
	if !ok || len(targets) != 1 {
		t.Fatalf("NVMeOfTargetList = %#v, want one target", got)
	}
	target, ok := targets[0].(map[string]any)
	if !ok || target["session"] != 1 || target["namespace_detail"] == nil {
		t.Fatalf("target = %#v, want session and namespace_detail", target)
	}
	want := []string{
		"ceph orch ps --daemon_type nvmeof -f json",
		"podman ps --format json",
		"podman exec -i abc123 python3 /usr/libexec/spdk/scripts/rpc.py nvmf_get_subsystems",
		"podman exec -i abc123 python3 /usr/libexec/spdk/scripts/rpc.py nvmf_subsystem_get_controllers " + nqn,
		"podman run --rm -i localhost:15000/glue/nvmeof-cli:Diplo --format json --server-address 10.10.10.10 --server-port 5500 namespace list --subsystem " + nqn,
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestSMBStatusUsesLocalScript(t *testing.T) {
	t.Setenv(envSMBScriptPath, "/opt/Samba-Execute.sh")
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"/opt/Samba-Execute.sh select": []byte(`{"status":"active","hostname":"scvm"}`),
	})

	got, err := SMBStatus(context.Background())
	if err != nil {
		t.Fatalf("SMBStatus returned error: %v", err)
	}
	status, ok := got.(map[string]any)
	if !ok || status["status"] != "active" || status["hostname"] != "scvm" {
		t.Fatalf("SMBStatus = %#v, want active scvm", got)
	}
	want := []string{"/opt/Samba-Execute.sh select"}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestSMBCreateUsesLocalScriptWithoutSSH(t *testing.T) {
	t.Setenv(envSMBScriptPath, "/opt/Samba-Execute.sh")
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"/opt/Samba-Execute.sh delete": []byte(""),
		"/opt/Samba-Execute.sh create normal --username smbuser --password secret --cache_policy true --folder share01 --path /gluefs/volumes/share01 --fs_name gluefs --volume_path /volumes/share01": []byte(""),
	})

	got, err := SMBCreate(context.Background(), "normal", "true", "smbuser", "secret", "share01", "/gluefs/volumes/share01", "gluefs", "/volumes/share01", "", "")
	if err != nil {
		t.Fatalf("SMBCreate returned error: %v", err)
	}
	if got["status"] != "success" || got["folder_name"] != "share01" {
		t.Fatalf("SMBCreate = %#v, want success share01", got)
	}
	want := []string{
		"/opt/Samba-Execute.sh delete",
		"/opt/Samba-Execute.sh create normal --username smbuser --password secret --cache_policy true --folder share01 --path /gluefs/volumes/share01 --fs_name gluefs --volume_path /volumes/share01",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestSMBCreateAdsPassesRealmAndDNS(t *testing.T) {
	t.Setenv(envSMBScriptPath, "/opt/Samba-Execute.sh")
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"/opt/Samba-Execute.sh delete": []byte(""),
		"/opt/Samba-Execute.sh create ads --username admin --password secret --cache_policy false --folder share01 --path /gluefs/volumes/share01 --fs_name gluefs --volume_path /volumes/share01 --realm EXAMPLE.LOCAL --dns 10.10.10.10": []byte(""),
	})

	if _, err := SMBCreate(context.Background(), "ads", "false", "admin", "secret", "share01", "/gluefs/volumes/share01", "gluefs", "/volumes/share01", "EXAMPLE.LOCAL", "10.10.10.10"); err != nil {
		t.Fatalf("SMBCreate returned error: %v", err)
	}
	want := []string{
		"/opt/Samba-Execute.sh delete",
		"/opt/Samba-Execute.sh create ads --username admin --password secret --cache_policy false --folder share01 --path /gluefs/volumes/share01 --fs_name gluefs --volume_path /volumes/share01 --realm EXAMPLE.LOCAL --dns 10.10.10.10",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestSMBFolderAddUsesLocalScript(t *testing.T) {
	t.Setenv(envSMBScriptPath, "/opt/Samba-Execute.sh")
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"/opt/Samba-Execute.sh share_folder_add --cache_policy true --folder share02 --path /gluefs/volumes/share02 --fs_name gluefs --volume_path /volumes/share02": []byte(""),
	})

	if _, err := SMBShareFolderAdd(context.Background(), "true", "share02", "/gluefs/volumes/share02", "gluefs", "/volumes/share02"); err != nil {
		t.Fatalf("SMBShareFolderAdd returned error: %v", err)
	}
	want := []string{
		"/opt/Samba-Execute.sh share_folder_add --cache_policy true --folder share02 --path /gluefs/volumes/share02 --fs_name gluefs --volume_path /volumes/share02",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestSMBPasswordIsRedactedInCommandError(t *testing.T) {
	t.Setenv(envSMBScriptPath, "/opt/Samba-Execute.sh")
	withCustomRunner(t, func(ctx context.Context, command string, args ...string) ([]byte, bool, error) {
		return []byte("failed"), false, errors.New("exit status 1")
	})

	_, err := SMBUserCreate(context.Background(), "smbuser", "super-secret")
	if err == nil {
		t.Fatalf("SMBUserCreate returned nil error")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("error leaked password: %v", err)
	}
	if !strings.Contains(err.Error(), "****") {
		t.Fatalf("error did not include password redaction marker: %v", err)
	}
}

func TestISCSIServiceCreateBuildsCephOrchSpec(t *testing.T) {
	commands := []string{}
	withCustomRunner(t, func(ctx context.Context, command string, args ...string) ([]byte, bool, error) {
		key := commandLine(command, args)
		commands = append(commands, key)
		if !strings.HasPrefix(key, "ceph orch apply -i ") {
			return nil, false, errors.New("unexpected command: " + key)
		}
		raw, err := os.ReadFile(args[3])
		if err != nil {
			return nil, false, err
		}
		spec := string(raw)
		for _, want := range []string{
			"service_type: iscsi",
			"service_id: iscsi",
			"api_user: admin",
			"api_password: secret",
			"api_port: 5000",
			"pool: rbd",
			"trusted_ip_list: 10.10.10.11",
			"scvm",
		} {
			if !strings.Contains(spec, want) {
				return nil, false, errors.New("missing spec value: " + want + "\n" + spec)
			}
		}
		return []byte(""), false, nil
	})

	got, err := ISCSIServiceCreate(context.Background(), "iscsi", []string{"scvm"}, []string{"10.10.10.11"}, "rbd", "5000", "admin", "secret", "")
	if err != nil {
		t.Fatalf("ISCSIServiceCreate returned error: %v", err)
	}
	if got["status"] != "success" || got["service_id"] != "iscsi" {
		t.Fatalf("ISCSIServiceCreate = %#v, want success iscsi", got)
	}
	if len(commands) != 1 || !strings.HasPrefix(commands[0], "ceph orch apply -i ") {
		t.Fatalf("commands = %#v, want ceph orch apply", commands)
	}
}

func TestISCSIServiceUpdateRedeploysService(t *testing.T) {
	commands := []string{}
	withCustomRunner(t, func(ctx context.Context, command string, args ...string) ([]byte, bool, error) {
		key := commandLine(command, args)
		commands = append(commands, key)
		if strings.HasPrefix(key, "ceph orch apply -i ") || key == "ceph orch redeploy iscsi.iscsi" {
			return []byte(""), false, nil
		}
		return nil, false, errors.New("unexpected command: " + key)
	})

	if _, err := ISCSIServiceUpdate(context.Background(), "iscsi", []string{"scvm"}, []string{"10.10.10.11"}, "rbd", "5000", "admin", "secret", "1"); err != nil {
		t.Fatalf("ISCSIServiceUpdate returned error: %v", err)
	}
	if len(commands) != 2 || !strings.HasPrefix(commands[0], "ceph orch apply -i ") || commands[1] != "ceph orch redeploy iscsi.iscsi" {
		t.Fatalf("commands = %#v, want apply then redeploy", commands)
	}
}

func TestISCSITargetCreateUsesDashboardAPI(t *testing.T) {
	t.Setenv(envISCSIDashboardUser, "admin")
	t.Setenv(envISCSIDashboardPassword, "secret")
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /api/auth":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"token-1"}`))
		case "POST /api/iscsi/target":
			if r.Header.Get("Authorization") != "Bearer token-1" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if payload["target_iqn"] != "iqn.2026-06.io.ablecloud:target01" || payload["acl_enabled"] != false {
				t.Fatalf("payload = %#v", payload)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"status":"created"}`))
		default:
			t.Fatalf("unexpected dashboard request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv(envISCSIDashboardURL, server.URL)

	got, err := ISCSITargetCreate(
		context.Background(),
		"iqn.2026-06.io.ablecloud:target01",
		[]string{"scvm"},
		[]string{"10.10.10.11"},
		[]string{"rbd"},
		[]string{"vm01"},
		"false",
		"",
		"",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("ISCSITargetCreate returned error: %v", err)
	}
	value, ok := got.(map[string]any)
	if !ok || value["status"] != "created" {
		t.Fatalf("ISCSITargetCreate = %#v, want created", got)
	}
	want := []string{"POST /api/auth", "POST /api/iscsi/target"}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
}

func TestNormalizeISCSIAuthPayloadAcceptsCHAPWithoutMutualAuth(t *testing.T) {
	payload, err := normalizeISCSIAuthPayload("ablecloud", "AbleCloud123", "", "", true)
	if err != nil {
		t.Fatalf("normalizeISCSIAuthPayload returned error: %v", err)
	}
	if payload.User != "ablecloud" || payload.Password != "AbleCloud123" {
		t.Fatalf("payload = %#v, want regular CHAP credentials", payload)
	}
	if payload.MutualUser != "" || payload.MutualPassword != "" {
		t.Fatalf("payload = %#v, want empty mutual CHAP credentials", payload)
	}
}

func TestNormalizeISCSIAuthPayloadRejectsInvalidCHAPCredentials(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		password string
		want     string
	}{
		{name: "short username", user: "iscsi", password: "AbleCloud123", want: "user must be between 8 and 64 characters"},
		{name: "short password", user: "ablecloud", password: "AbleCloud12", want: "password must be between 12 and 16 characters"},
		{name: "password with unsupported character", user: "ablecloud", password: "AbleCloud!23", want: "invalid password"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeISCSIAuthPayload(tt.user, tt.password, "", "", true)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNormalizeISCSIAuthPayloadRequiresMutualCredentialsTogether(t *testing.T) {
	_, err := normalizeISCSIAuthPayload("ablecloud", "AbleCloud123", "mutualuser", "", true)
	if err == nil || err.Error() != "mutual_user and mutual_password must be provided together" {
		t.Fatalf("error = %v, want incomplete mutual credential error", err)
	}
}

func TestNormalizeISCSITargetPayloadAllowsACLWithoutTargetCHAP(t *testing.T) {
	payload, err := normalizeISCSITargetPayload(
		"iqn.2026-08.ablecloud.io:1787895553",
		"",
		[]string{"scvm3"},
		[]string{"10.10.32.13"},
		[]string{"rbd"},
		[]string{"iscsi-1efefv"},
		"true",
		"",
		"",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("normalizeISCSITargetPayload returned error: %v", err)
	}
	if !payload.ACLEnabled {
		t.Fatalf("acl_enabled = false, want true")
	}
	if payload.Auth != (iscsiAuthPayload{}) {
		t.Fatalf("auth = %#v, want empty target CHAP credentials", payload.Auth)
	}
}

func TestNormalizeISCSITargetPayloadRejectsACLWithTargetCHAP(t *testing.T) {
	_, err := normalizeISCSITargetPayload(
		"iqn.2026-08.ablecloud.io:1787895553",
		"",
		[]string{"scvm3"},
		[]string{"10.10.32.13"},
		[]string{"rbd"},
		[]string{"iscsi-1efefv"},
		"true",
		"ablecloud",
		"AbleCloud123",
		"",
		"",
	)
	if err == nil || err.Error() != "acl_enabled must be disabled when target CHAP authentication is configured" {
		t.Fatalf("error = %v, want ACL/CHAP conflict error", err)
	}
}

func TestNormalizeISCSITargetPayloadKeepsTargetIQNWhenRenaming(t *testing.T) {
	oldIQN := "iqn.2026-08.ablecloud.io:1786434358"
	newIQN := "iqn.2026-08.ablecloud.io:1786434359"

	payload, err := normalizeISCSITargetPayload(
		oldIQN,
		newIQN,
		[]string{"scvm"},
		[]string{"10.10.10.11"},
		[]string{"rbd"},
		[]string{"iscsi-2"},
		"false",
		"",
		"",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("normalizeISCSITargetPayload returned error: %v", err)
	}
	if payload.TargetIQN != oldIQN {
		t.Fatalf("target_iqn = %q, want %q", payload.TargetIQN, oldIQN)
	}
	if payload.NewTargetIQN != newIQN {
		t.Fatalf("new_target_iqn = %q, want %q", payload.NewTargetIQN, newIQN)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload) returned error: %v", err)
	}
	if !strings.Contains(string(encoded), `"target_iqn":"`+oldIQN+`"`) {
		t.Fatalf("encoded payload = %s, want target_iqn", encoded)
	}
}

func TestNormalizeISCSITargetPayloadOmitsSameIQNRename(t *testing.T) {
	iqn := "iqn.2026-08.ablecloud.io:1787881832"

	payload, err := normalizeISCSITargetPayload(
		iqn,
		iqn,
		[]string{"scvm"},
		[]string{"10.10.10.11"},
		[]string{"rbd"},
		[]string{"iscsi-1"},
		"false",
		"",
		"",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("normalizeISCSITargetPayload returned error: %v", err)
	}
	if payload.TargetIQN != iqn {
		t.Fatalf("target_iqn = %q, want %q", payload.TargetIQN, iqn)
	}
	if payload.NewTargetIQN != "" {
		t.Fatalf("new_target_iqn = %q, want empty for an unchanged IQN", payload.NewTargetIQN)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload) returned error: %v", err)
	}
	if !strings.Contains(string(encoded), `"target_iqn":"`+iqn+`"`) {
		t.Fatalf("encoded payload = %s, want target_iqn", encoded)
	}
	if strings.Contains(string(encoded), `"new_target_iqn"`) {
		t.Fatalf("encoded payload = %s, want no new_target_iqn for an unchanged IQN", encoded)
	}
}

func TestISCSITargetUpdateIncludesTargetIQNWhenRemovingDisk(t *testing.T) {
	t.Setenv(envISCSIDashboardUser, "admin")
	t.Setenv(envISCSIDashboardPassword, "secret")
	iqn := "iqn.2026-08.ablecloud.io:1787881832"
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /api/auth":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"token-1"}`))
		case "PUT /api/iscsi/target/" + iqn:
			var payload struct {
				TargetIQN    string             `json:"target_iqn"`
				NewTargetIQN string             `json:"new_target_iqn"`
				Disks        []iscsiDiskPayload `json:"disks"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if payload.TargetIQN != iqn {
				t.Fatalf("target_iqn = %q, want %q", payload.TargetIQN, iqn)
			}
			if payload.NewTargetIQN != "" {
				t.Fatalf("new_target_iqn = %q, want empty", payload.NewTargetIQN)
			}
			if len(payload.Disks) != 1 || payload.Disks[0].Image != "iscsi-1" {
				t.Fatalf("disks = %#v, want one remaining disk", payload.Disks)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"updated"}`))
		default:
			t.Fatalf("unexpected dashboard request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv(envISCSIDashboardURL, server.URL)

	got, err := ISCSITargetUpdate(
		context.Background(),
		iqn,
		iqn,
		[]string{"scvm"},
		[]string{"10.10.10.11"},
		[]string{"rbd"},
		[]string{"iscsi-1"},
		"false",
		"",
		"",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("ISCSITargetUpdate returned error: %v", err)
	}
	value, ok := got.(map[string]any)
	if !ok || value["status"] != "updated" {
		t.Fatalf("ISCSITargetUpdate = %#v, want updated", got)
	}
	want := []string{"POST /api/auth", "PUT /api/iscsi/target/" + iqn}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
}

func TestISCSITargetPurgeUsesLocalPodmanExec(t *testing.T) {
	commands := []string{}
	iqn := "iqn.2026-06.io.ablecloud:target01"
	withRecordingRunner(t, &commands, map[string][]byte{
		"podman ps --format json":                                  []byte(`[{"ID":"abc123","Image":"localhost/glue/tcmu:latest","Names":"iscsi.scvm"}]`),
		"podman exec -i abc123 gwcli /iscsi-targets delete " + iqn: []byte(""),
	})

	if _, err := ISCSITargetPurge(context.Background(), iqn); err != nil {
		t.Fatalf("ISCSITargetPurge returned error: %v", err)
	}
	want := []string{
		"podman ps --format json",
		"podman exec -i abc123 gwcli /iscsi-targets delete " + iqn,
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestGlueFSCreateBuildsLocalCephCommands(t *testing.T) {
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"ceph fs volume create gluefs --placement scvm":       []byte(""),
		"ceph osd pool rename cephfs.gluefs.data gluefs.data": []byte(""),
		"ceph osd pool rename cephfs.gluefs.meta gluefs.meta": []byte(""),
		"ceph osd pool set gluefs.data size 2":                []byte(""),
		"ceph osd pool set gluefs.meta size 2":                []byte(""),
	})

	if _, err := GlueFSCreate(context.Background(), "gluefs", []string{"scvm"}); err != nil {
		t.Fatalf("GlueFSCreate returned error: %v", err)
	}
	want := []string{
		"ceph fs volume create gluefs --placement scvm",
		"ceph osd pool rename cephfs.gluefs.data gluefs.data",
		"ceph osd pool rename cephfs.gluefs.meta gluefs.meta",
		"ceph osd pool set gluefs.data size 2",
		"ceph osd pool set gluefs.meta size 2",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestNFSExportCreateWritesSpecAndApplies(t *testing.T) {
	commands := []string{}
	withCustomRunner(t, func(ctx context.Context, command string, args ...string) ([]byte, bool, error) {
		key := commandLine(command, args)
		commands = append(commands, key)
		if !strings.HasPrefix(key, "ceph nfs export apply nfs-a -i ") {
			return nil, false, errors.New("unexpected command: " + key)
		}
		raw, err := os.ReadFile(args[5])
		if err != nil {
			return nil, false, err
		}
		spec := string(raw)
		for _, want := range []string{`"access_type": "RW"`, `"name": "CEPH"`, `"fs_name": "gluefs"`, `"pseudo": "/export01"`} {
			if !strings.Contains(spec, want) {
				return nil, false, errors.New("missing NFS export spec value: " + want + "\n" + spec)
			}
		}
		return []byte(""), false, nil
	})

	if _, err := NFSExportCreate(context.Background(), "nfs-a", "RW", "gluefs", "CEPH", "/volumes/group01", "/export01", "no_root_squash", []string{"TCP"}, false); err != nil {
		t.Fatalf("NFSExportCreate returned error: %v", err)
	}
	if len(commands) != 1 || !strings.HasPrefix(commands[0], "ceph nfs export apply nfs-a -i ") {
		t.Fatalf("commands = %#v, want ceph nfs export apply", commands)
	}
}

func TestNFSIngressCreateWritesSpecAndApplies(t *testing.T) {
	commands := []string{}
	withCustomRunner(t, func(ctx context.Context, command string, args ...string) ([]byte, bool, error) {
		key := commandLine(command, args)
		commands = append(commands, key)
		if !strings.HasPrefix(key, "ceph orch apply -i ") {
			return nil, false, errors.New("unexpected command: " + key)
		}
		raw, err := os.ReadFile(args[3])
		if err != nil {
			return nil, false, err
		}
		spec := string(raw)
		for _, want := range []string{
			"service_type: ingress",
			"service_id: nfs-ingress",
			"backend_service: nfs.nfs-a",
			"virtual_ip: 10.10.10.100/24",
			"frontend_port: 2049",
			"monitor_port: 9049",
			"10.10.10.0/24",
			"use_keepalived_multicast: false",
		} {
			if !strings.Contains(spec, want) {
				return nil, false, errors.New("missing NFS ingress spec value: " + want + "\n" + spec)
			}
		}
		return []byte(""), false, nil
	})

	if _, err := NFSIngressCreate(context.Background(), "nfs-ingress", []string{"scvm"}, "nfs.nfs-a", "10.10.10.100/24", "2049", "9049", []string{"10.10.10.0/24"}); err != nil {
		t.Fatalf("NFSIngressCreate returned error: %v", err)
	}
	if len(commands) != 1 || !strings.HasPrefix(commands[0], "ceph orch apply -i ") {
		t.Fatalf("commands = %#v, want ceph orch apply", commands)
	}
}

func TestMirrorSetupCreatesLocalTokenAndImportsRemoteToken(t *testing.T) {
	remoteToken, err := encodeMirrorToken(mirrorToken{FSID: "remote-fsid", ClientID: "remote-peer", Key: "remote-key", MonHost: "10.10.10.20"})
	if err != nil {
		t.Fatalf("encode remote token: %v", err)
	}
	localToken, err := encodeMirrorToken(mirrorToken{FSID: "local-fsid", ClientID: "rbd-mirror-peer", Key: "old-key", MonHost: "10.10.10.10"})
	if err != nil {
		t.Fatalf("encode local token: %v", err)
	}
	commands := []string{}
	withCustomRunner(t, func(ctx context.Context, command string, args ...string) ([]byte, bool, error) {
		key := commandLine(command, args)
		commands = append(commands, key)
		switch {
		case key == "rbd mirror pool enable --site-name local-a -p rbd image":
			return []byte(""), false, nil
		case key == "ceph orch apply rbd-mirror --placement scvm":
			return []byte(""), false, nil
		case key == "rbd mirror pool peer bootstrap create --site-name local-a -p rbd":
			return []byte(localToken), false, nil
		case key == "ceph auth caps client.rbd-mirror-peer mgr profile rbd mon profile rbd-mirror-peer osd profile rbd":
			return []byte(""), false, nil
		case key == "ceph auth get-key client.rbd-mirror-peer --format json":
			return []byte(`{"key":"new-key"}`), false, nil
		case key == "rbd mirror pool info --pool rbd --format json":
			return []byte(`{"peers":[{"uuid":"peer-uuid","client_name":"remote-peer"}]}`), false, nil
		case key == "rbd mirror pool peer remove --pool rbd peer-uuid":
			return []byte(""), false, nil
		case key == "ceph auth del client.remote-peer":
			return []byte(""), false, nil
		case strings.HasPrefix(key, "rbd mirror pool peer bootstrap import --pool rbd --token-path "):
			raw, err := os.ReadFile(args[len(args)-1])
			if err != nil {
				return nil, false, err
			}
			if string(raw) != remoteToken {
				return nil, false, errors.New("unexpected remote token file content")
			}
			return []byte(""), false, nil
		case key == "rbd info rbd/MOLD-DR --format json":
			return []byte(`{"name":"MOLD-DR"}`), false, nil
		case key == "rbd image-meta set rbd/MOLD-DR interval 1h":
			return []byte(""), false, nil
		default:
			return nil, false, errors.New("unexpected command: " + key)
		}
	})

	got, err := MirrorSetup(context.Background(), "local-a", "rbd", remoteToken, []string{"scvm"}, "1h")
	if err != nil {
		t.Fatalf("MirrorSetup returned error: %v", err)
	}
	encoded, ok := got["local_token"].(string)
	if !ok || encoded == "" {
		t.Fatalf("local_token = %#v, want encoded token", got["local_token"])
	}
	decoded, err := decodeMirrorToken(encoded)
	if err != nil {
		t.Fatalf("decode local token: %v", err)
	}
	if decoded.Key != "new-key" {
		t.Fatalf("local token key = %q, want new-key", decoded.Key)
	}
	for _, command := range commands {
		if strings.Contains(command, "ssh") || strings.Contains(command, "scp") {
			t.Fatalf("mirror setup used remote shell command: %s", command)
		}
	}
}

func TestMirrorPoolDisableUsesLocalCommands(t *testing.T) {
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"rbd mirror pool status rbd --verbose --format json --pretty-format": []byte(`{"images":[{"name":"vm01"}]}`),
		"rbd mirror image disable --pool rbd --image vm01":                   []byte(""),
		"rbd image-meta remove rbd/MOLD-DR vm01":                             []byte(""),
		"rbd mirror pool info --pool rbd --format json":                      []byte(`{"peers":[{"uuid":"peer-uuid","client_name":"remote-peer"}]}`),
		"rbd mirror pool peer remove --pool rbd peer-uuid":                   []byte(""),
		"ceph auth del client.remote-peer":                                   []byte(""),
		"rbd mirror pool disable --pool rbd":                                 []byte(""),
		"ceph auth del client.rbd-mirror-peer":                               []byte(""),
	})

	if _, err := MirrorPoolDisable(context.Background(), "rbd"); err != nil {
		t.Fatalf("MirrorPoolDisable returned error: %v", err)
	}
	want := []string{
		"rbd mirror pool status rbd --verbose --format json --pretty-format",
		"rbd mirror image disable --pool rbd --image vm01",
		"rbd image-meta remove rbd/MOLD-DR vm01",
		"rbd mirror pool info --pool rbd --format json",
		"rbd mirror pool peer remove --pool rbd peer-uuid",
		"ceph auth del client.remote-peer",
		"rbd mirror pool disable --pool rbd",
		"ceph auth del client.rbd-mirror-peer",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestMirrorSetupCanImportRemoteTokenOnly(t *testing.T) {
	remoteToken, err := encodeMirrorToken(mirrorToken{FSID: "remote-fsid", ClientID: "remote-peer", Key: "remote-key", MonHost: "10.10.10.20"})
	if err != nil {
		t.Fatalf("encode remote token: %v", err)
	}
	commands := []string{}
	withCustomRunner(t, func(ctx context.Context, command string, args ...string) ([]byte, bool, error) {
		key := commandLine(command, args)
		commands = append(commands, key)
		switch {
		case key == "rbd mirror pool info --pool rbd --format json":
			return []byte(`{"peers":[]}`), false, nil
		case strings.HasPrefix(key, "rbd mirror pool peer bootstrap import --pool rbd --token-path "):
			return []byte(""), false, nil
		case key == "rbd info rbd/MOLD-DR --format json":
			return []byte(`{"name":"MOLD-DR"}`), false, nil
		case key == "rbd image-meta set rbd/MOLD-DR interval 1h":
			return []byte(""), false, nil
		default:
			return nil, false, errors.New("unexpected command: " + key)
		}
	})

	got, err := MirrorSetup(context.Background(), "", "rbd", remoteToken, nil, "1h")
	if err != nil {
		t.Fatalf("MirrorSetup returned error: %v", err)
	}
	if got["local_token"] != "" {
		t.Fatalf("local_token = %#v, want empty import-only token", got["local_token"])
	}
	for _, command := range commands {
		if strings.Contains(command, "bootstrap create") {
			t.Fatalf("import-only setup created new token: %s", command)
		}
	}
}

func TestRGWUserUpdateRedactsSecretKeyInCommandError(t *testing.T) {
	withCustomRunner(t, func(ctx context.Context, command string, args ...string) ([]byte, bool, error) {
		return []byte("failed"), false, errors.New("exit status 1")
	})

	_, err := RGWUserUpdate(context.Background(), "user01", "", "", "", "s3", "access", "secret-value")
	if err == nil {
		t.Fatalf("RGWUserUpdate returned nil error")
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("error leaked secret key: %v", err)
	}
	if !strings.Contains(err.Error(), "****") {
		t.Fatalf("error did not include secret redaction marker: %v", err)
	}
}

func TestRGWUserUpdateSetsSuspendedState(t *testing.T) {
	tests := []struct {
		name      string
		suspended string
	}{
		{name: "activate", suspended: "0"},
		{name: "suspend", suspended: "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commands := []string{}
			stateCommand := "suspend"
			if tt.suspended == "0" {
				stateCommand = "enable"
			}
			withRecordingRunner(t, &commands, map[string][]byte{
				"radosgw-admin user " + stateCommand + " --uid user01": []byte(""),
			})

			if _, err := RGWUserUpdate(context.Background(), "user01", "", "", tt.suspended, "", "", ""); err != nil {
				t.Fatalf("RGWUserUpdate returned error: %v", err)
			}
			want := []string{"radosgw-admin user " + stateCommand + " --uid user01"}
			if !reflect.DeepEqual(commands, want) {
				t.Fatalf("commands = %#v, want %#v", commands, want)
			}
		})
	}
}

func TestRGWUserUpdateModifiesMetadataBeforeChangingState(t *testing.T) {
	commands := []string{}
	withRecordingRunner(t, &commands, map[string][]byte{
		"radosgw-admin user modify --uid user01 --display-name qqq --email tj@ablecloud.io": []byte(""),
		"radosgw-admin user enable --uid user01":                                            []byte(""),
	})

	if _, err := RGWUserUpdate(context.Background(), "user01", "qqq", "tj@ablecloud.io", "0", "", "", ""); err != nil {
		t.Fatalf("RGWUserUpdate returned error: %v", err)
	}
	want := []string{
		"radosgw-admin user modify --uid user01 --display-name qqq --email tj@ablecloud.io",
		"radosgw-admin user enable --uid user01",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestRGWUserUpdateRejectsInvalidSuspendedState(t *testing.T) {
	withCustomRunner(t, func(ctx context.Context, command string, args ...string) ([]byte, bool, error) {
		return nil, false, errors.New("command should not run")
	})

	if _, err := RGWUserUpdate(context.Background(), "user01", "", "", "2", "", "", ""); err == nil {
		t.Fatalf("RGWUserUpdate accepted invalid suspended state")
	}
}

func withFakeRunner(t *testing.T, outputs map[string][]byte) {
	t.Helper()
	withRecordingRunner(t, nil, outputs)
}

func withRecordingRunner(t *testing.T, commands *[]string, outputs map[string][]byte) {
	t.Helper()
	old := runCommand
	runCommand = func(ctx context.Context, command string, args ...string) ([]byte, bool, error) {
		key := commandLine(command, args)
		if commands != nil {
			*commands = append(*commands, key)
		}
		output, ok := outputs[key]
		if !ok {
			return nil, false, errors.New("unexpected command: " + key)
		}
		return output, false, nil
	}
	t.Cleanup(func() {
		runCommand = old
	})
}

func withCustomRunner(t *testing.T, runner commandRunner) {
	t.Helper()
	old := runCommand
	runCommand = runner
	t.Cleanup(func() {
		runCommand = old
	})
}
