package cube

import "testing"

func TestParseGFSDiskUsageDF(t *testing.T) {
	raw := []byte(`Filesystem               Size  Used Avail Use% Mounted on
/dev/mapper/vg_glue-lv_glue  800G   57G  744G   8% /mnt/glue-gfs
`)

	usageByMountpoint := ParseGFSDiskUsageDF(raw)
	usage, ok := usageByMountpoint["/mnt/glue-gfs"]
	if !ok {
		t.Fatalf("usage for /mnt/glue-gfs not found")
	}
	if usage.Filesystem != "/dev/mapper/vg_glue-lv_glue" {
		t.Fatalf("Filesystem = %q", usage.Filesystem)
	}
	if usage.Size != "800G" || usage.Used != "57G" || usage.Avail != "744G" || usage.UsePercent != "8%" {
		t.Fatalf("unexpected usage: %#v", usage)
	}
}

func TestBuildGFSDiskStatusAppliesDFUsage(t *testing.T) {
	size := "800G"
	mountpoint := "/mnt/glue-gfs"
	devices := []GFSBlockDevice{
		{
			Name:  "sdb",
			Kname: "sdb",
			Path:  strPtr("/dev/sdb"),
			Size:  strPtr(size),
			Type:  strPtr("disk"),
			Children: []GFSBlockDevice{
				{
					Name:       "vg_glue-lv_glue",
					Kname:      "dm-0",
					Path:       strPtr("/dev/mapper/vg_glue-lv_glue"),
					Size:       strPtr(size),
					Type:       strPtr("lvm"),
					Mountpoint: strPtr(mountpoint),
				},
			},
		},
	}
	mounts := []GFSMount{
		{Device: "/dev/mapper/vg_glue-lv_glue", Mountpoint: mountpoint},
	}
	usageByMountpoint := map[string]GFSDiskUsage{
		mountpoint: {
			Filesystem: "/dev/mapper/vg_glue-lv_glue",
			Mountpoint: mountpoint,
			Size:       "800G",
			Used:       "57G",
			Avail:      "744G",
			UsePercent: "8%",
		},
	}

	status := BuildGFSDiskStatus(devices, mounts, "ablestack-vm", false, nil, usageByMountpoint)
	if len(status.Blockdevices) != 1 {
		t.Fatalf("Blockdevices length = %d", len(status.Blockdevices))
	}
	got := status.Blockdevices[0]
	if got.Size != "800G" || got.Used != "57G" || got.Avail != "744G" || got.UsePercent != "8%" {
		t.Fatalf("unexpected disk usage: %#v", got)
	}
}

func TestBuildGFSDiskStatusDefaultsMissingDFUsage(t *testing.T) {
	mountpoint := "/mnt/glue-gfs"
	devices := []GFSBlockDevice{
		{
			Name:       "vg_glue-lv_glue",
			Kname:      "dm-0",
			Path:       strPtr("/dev/mapper/vg_glue-lv_glue"),
			Size:       strPtr("800G"),
			Type:       strPtr("lvm"),
			Mountpoint: strPtr(mountpoint),
		},
	}
	mounts := []GFSMount{
		{Device: "/dev/mapper/vg_glue-lv_glue", Mountpoint: mountpoint},
	}

	status := BuildGFSDiskStatus(devices, mounts, "ablestack-vm", false, nil, nil)
	if len(status.Blockdevices) != 1 {
		t.Fatalf("Blockdevices length = %d", len(status.Blockdevices))
	}
	got := status.Blockdevices[0]
	if got.Used != "N/A" || got.Avail != "N/A" || got.UsePercent != "N/A" {
		t.Fatalf("unexpected default usage: %#v", got)
	}
}

func strPtr(value string) *string {
	return &value
}
