package glueservice

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// GlueFSStatus는 GlueFS 상태와 filesystem 목록을 함께 반환한다.
func GlueFSStatus(ctx context.Context) (map[string]any, error) {
	status, err := runJSON(ctx, "ceph", "fs", "status", "-f", "json")
	if err != nil {
		return nil, err
	}
	list, err := runJSON(ctx, "ceph", "fs", "ls", "-f", "json")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"fs_status": status,
		"fs_list":   list,
	}, nil
}

// GlueFSInfo는 특정 filesystem의 상세 정보를 조회한다.
func GlueFSInfo(ctx context.Context, fsName string) (any, error) {
	fsName = strings.TrimSpace(fsName)
	if err := ValidateCephName("fs_name", fsName); err != nil {
		return nil, err
	}
	return runJSON(ctx, "ceph", "fs", "get", fsName, "-f", "json")
}

// GlueFSSubvolumeGroups는 subvolume group별 info/path/snapshot 정보를 묶어 반환한다.
func GlueFSSubvolumeGroups(ctx context.Context, volumeName string) ([]map[string]any, error) {
	volumeName = strings.TrimSpace(volumeName)
	if err := ValidateCephName("vol_name", volumeName); err != nil {
		return nil, err
	}

	rawList, err := runJSON(ctx, "ceph", "fs", "subvolumegroup", "ls", volumeName)
	if err != nil {
		return nil, err
	}
	groupNames, err := namesFromList(rawList)
	if err != nil {
		return nil, err
	}

	groups := make([]map[string]any, 0, len(groupNames))
	for _, groupName := range groupNames {
		if err := ValidateCephName("group_name", groupName); err != nil {
			return nil, err
		}
		info, err := runJSON(ctx, "ceph", "fs", "subvolumegroup", "info", volumeName, groupName)
		if err != nil {
			return nil, err
		}
		pathOutput, err := run(ctx, "ceph", "fs", "subvolumegroup", "getpath", volumeName, groupName)
		if err != nil {
			return nil, err
		}
		snapshots, err := runJSON(ctx, "ceph", "fs", "subvolumegroup", "snapshot", "ls", volumeName, groupName)
		if err != nil {
			return nil, err
		}
		groups = append(groups, map[string]any{
			"name":      groupName,
			"info":      info,
			"path":      strings.TrimSpace(string(pathOutput)),
			"snapshots": snapshots,
		})
	}
	return groups, nil
}

// GlueFSCreate는 filesystem volume을 생성하고 기본 pool 이름/replica size를 정리한다.
func GlueFSCreate(ctx context.Context, fsName string, hosts []string) (map[string]any, error) {
	fsName = strings.TrimSpace(fsName)
	hosts = trimStringSlice(hosts)
	if err := ValidateCephName("fs_name", fsName); err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("hosts is required")
	}
	for _, host := range hosts {
		if err := ValidateCephName("host", host); err != nil {
			return nil, err
		}
	}
	placement := strings.Join(hosts, ",")
	if _, err := run(ctx, "ceph", "fs", "volume", "create", fsName, "--placement", placement); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "ceph", "osd", "pool", "rename", "cephfs."+fsName+".data", fsName+".data"); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "ceph", "osd", "pool", "rename", "cephfs."+fsName+".meta", fsName+".meta"); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "ceph", "osd", "pool", "set", fsName+".data", "size", "2"); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "ceph", "osd", "pool", "set", fsName+".meta", "size", "2"); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "fs_name": fsName, "hosts": hosts}, nil
}

// GlueFSUpdate는 filesystem과 pool 이름을 변경하고 필요하면 MDS placement를 갱신한다.
func GlueFSUpdate(ctx context.Context, oldName string, newName string, hosts []string) (map[string]any, error) {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	hosts = trimStringSlice(hosts)
	if err := ValidateCephName("old_name", oldName); err != nil {
		return nil, err
	}
	if err := ValidateCephName("new_name", newName); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "ceph", "fs", "rename", oldName, newName, "--yes-i-really-mean-it"); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "ceph", "osd", "pool", "rename", oldName+".data", newName+".data"); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "ceph", "osd", "pool", "rename", oldName+".meta", newName+".meta"); err != nil {
		return nil, err
	}
	if len(hosts) > 0 {
		for _, host := range hosts {
			if err := ValidateCephName("host", host); err != nil {
				return nil, err
			}
		}
		if _, err := run(ctx, "ceph", "orch", "apply", "mds", newName, "--placement", strings.Join(hosts, ",")); err != nil {
			return nil, err
		}
	}
	return map[string]any{"status": "success", "old_name": oldName, "new_name": newName, "hosts": hosts}, nil
}

// GlueFSDelete는 하위 subvolume group이 없을 때 filesystem volume을 삭제한다.
func GlueFSDelete(ctx context.Context, fsName string) (map[string]any, error) {
	fsName = strings.TrimSpace(fsName)
	if err := ValidateCephName("fs_name", fsName); err != nil {
		return nil, err
	}
	rawGroups, err := runJSON(ctx, "ceph", "fs", "subvolumegroup", "ls", fsName)
	if err != nil {
		return nil, err
	}
	groupNames, err := namesFromList(rawGroups)
	if err != nil {
		return nil, err
	}
	if len(groupNames) > 0 {
		return nil, fmt.Errorf("subvolume groups exist; delete groups before deleting filesystem")
	}
	current, err := run(ctx, "ceph", "config", "get", "mon", "mon_allow_pool_delete")
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(string(current)), "true") {
		if _, err := run(ctx, "ceph", "config", "set", "mon", "mon_allow_pool_delete", "true"); err != nil {
			return nil, err
		}
	}
	if _, err := run(ctx, "ceph", "fs", "volume", "rm", fsName, "--yes-i-really-mean-it"); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "fs_name": fsName}, nil
}

// GlueFSSubvolumeGroupCreate는 subvolume group을 생성한다.
func GlueFSSubvolumeGroupCreate(ctx context.Context, volumeName string, groupName string, sizeGiB string, dataPoolName string, mode string) (map[string]any, error) {
	volumeName = strings.TrimSpace(volumeName)
	groupName = strings.TrimSpace(groupName)
	dataPoolName = strings.TrimSpace(dataPoolName)
	mode = strings.TrimSpace(firstNonEmpty(mode, "755"))
	if err := ValidateCephName("vol_name", volumeName); err != nil {
		return nil, err
	}
	if err := ValidateCephName("group_name", groupName); err != nil {
		return nil, err
	}
	if err := ValidatePoolName(dataPoolName); err != nil {
		return nil, err
	}
	sizeBytes, err := sizeGiBToBytesString(sizeGiB)
	if err != nil {
		return nil, err
	}
	if _, err := strconv.ParseInt(mode, 8, 64); err != nil {
		return nil, fmt.Errorf("mode must be an octal value")
	}
	if _, err := run(ctx, "ceph", "fs", "subvolumegroup", "create", volumeName, groupName, sizeBytes, dataPoolName, "--mode", mode); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "vol_name": volumeName, "group_name": groupName, "size_bytes": sizeBytes}, nil
}

// GlueFSSubvolumeGroupDelete는 subvolume group을 삭제한다.
func GlueFSSubvolumeGroupDelete(ctx context.Context, volumeName string, groupName string) (map[string]any, error) {
	volumeName = strings.TrimSpace(volumeName)
	groupName = strings.TrimSpace(groupName)
	if err := ValidateCephName("vol_name", volumeName); err != nil {
		return nil, err
	}
	if err := ValidateCephName("group_name", groupName); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "ceph", "fs", "subvolumegroup", "rm", volumeName, groupName); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "vol_name": volumeName, "group_name": groupName}, nil
}

// GlueFSSubvolumeGroupResize는 subvolume group quota를 확장한다.
func GlueFSSubvolumeGroupResize(ctx context.Context, volumeName string, groupName string, newSizeGiB string) (map[string]any, error) {
	volumeName = strings.TrimSpace(volumeName)
	groupName = strings.TrimSpace(groupName)
	if err := ValidateCephName("vol_name", volumeName); err != nil {
		return nil, err
	}
	if err := ValidateCephName("group_name", groupName); err != nil {
		return nil, err
	}
	sizeBytes, err := sizeGiBToBytesString(newSizeGiB)
	if err != nil {
		return nil, err
	}
	if _, err := run(ctx, "ceph", "fs", "subvolumegroup", "resize", volumeName, groupName, sizeBytes, "--no_shrink"); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "vol_name": volumeName, "group_name": groupName, "size_bytes": sizeBytes}, nil
}

func sizeGiBToBytesString(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("size is required")
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return "", fmt.Errorf("size must be greater than zero")
	}
	return strconv.FormatInt(value*1024*1024*1024, 10), nil
}
