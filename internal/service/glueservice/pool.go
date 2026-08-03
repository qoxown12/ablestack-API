package glueservice

import (
	"context"
	"strings"
)

// ListPools는 legacy "ceph ... | grep" pipeline을 JSON 출력과 Go 필터링으로 대체한다.
// poolType은 UI 필터 값일 뿐 shell 조각으로 사용하지 않는다.
func ListPools(ctx context.Context, poolType string) ([]string, error) {
	poolType = strings.TrimSpace(poolType)
	output, err := run(ctx, "ceph", "osd", "pool", "ls", "--format", "json")
	if err != nil {
		return nil, err
	}
	pools, err := decodeJSONStringSlice(output)
	if err != nil {
		return nil, err
	}
	if poolType == "" {
		return pools, nil
	}

	filtered := make([]string, 0, len(pools))
	for _, pool := range pools {
		if strings.Contains(pool, poolType) {
			filtered = append(filtered, pool)
		}
	}
	return filtered, nil
}

// DeletePool은 pool 삭제 전에 Ceph의 mon_allow_pool_delete flag를 활성화한다.
func DeletePool(ctx context.Context, poolName string) (map[string]any, error) {
	poolName = strings.TrimSpace(poolName)
	if err := ValidatePoolName(poolName); err != nil {
		return nil, err
	}

	raw, err := run(ctx, "ceph", "config", "get", "mon", "mon_allow_pool_delete")
	if err != nil {
		return nil, err
	}
	allowDelete := strings.EqualFold(strings.TrimSpace(string(raw)), "true")
	if !allowDelete {
		if _, err := run(ctx, "ceph", "config", "set", "mon", "mon_allow_pool_delete", "true"); err != nil {
			return nil, err
		}
	}
	if _, err := run(ctx, "ceph", "osd", "pool", "rm", poolName, poolName, "--yes-i-really-really-mean-it"); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":                            "success",
		"pool":                              poolName,
		"mon_allow_pool_delete_was_enabled": allowDelete,
	}, nil
}
