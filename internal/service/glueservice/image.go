package glueservice

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// ListImages는 RBD image 목록 또는 상세 정보를 조회한다.
func ListImages(ctx context.Context, poolName string, imageName string) (any, error) {
	poolName = strings.TrimSpace(poolName)
	imageName = strings.TrimSpace(imageName)

	if poolName != "" {
		if err := ValidatePoolName(poolName); err != nil {
			return nil, err
		}
	}
	if imageName != "" {
		if poolName == "" {
			if err := ValidateImageRef(imageName); err != nil {
				return nil, err
			}
		} else if err := ValidateImageName(imageName); err != nil {
			return nil, err
		}
	}

	switch {
	case poolName == "" && imageName == "":
		return listAllRBDImages(ctx)
	case poolName != "" && imageName == "":
		return runJSON(ctx, "rbd", "ls", "-l", "-p", poolName, "--format", "json")
	default:
		return runJSON(ctx, "rbd", "info", imageRef(poolName, imageName), "--format", "json")
	}
}

// CreateImage는 GiB 단위 입력을 MiB로 변환해 RBD image를 생성한다.
func CreateImage(ctx context.Context, poolName string, imageName string, sizeGiB int64) (map[string]any, error) {
	poolName = strings.TrimSpace(poolName)
	imageName = strings.TrimSpace(imageName)
	if err := ValidatePoolName(poolName); err != nil {
		return nil, err
	}
	if err := ValidateImageName(imageName); err != nil {
		return nil, err
	}
	if sizeGiB <= 0 {
		return nil, fmt.Errorf("size must be greater than zero")
	}

	sizeMiB := sizeGiB * 1024
	if _, err := run(ctx, "rbd", "create", "--size", strconv.FormatInt(sizeMiB, 10), imageRef(poolName, imageName)); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":   "success",
		"pool":     poolName,
		"image":    imageName,
		"size_gib": sizeGiB,
		"size_mib": sizeMiB,
	}, nil
}

// DeleteImage는 RBD image를 삭제한다.
func DeleteImage(ctx context.Context, poolName string, imageName string) (map[string]any, error) {
	poolName = strings.TrimSpace(poolName)
	imageName = strings.TrimSpace(imageName)
	if err := ValidatePoolName(poolName); err != nil {
		return nil, err
	}
	if err := ValidateImageName(imageName); err != nil {
		return nil, err
	}

	if _, err := run(ctx, "rbd", "rm", imageRef(poolName, imageName)); err != nil {
		return nil, err
	}
	return map[string]any{
		"status": "success",
		"pool":   poolName,
		"image":  imageName,
	}, nil
}

// listAllRBDImages는 pool/image 문자열을 반환해 legacy "all images" 응답 형태를 유지한다.
// 단, pool 탐색은 grep이 아니라 JSON 출력 기반으로 처리한다.
func listAllRBDImages(ctx context.Context) ([]string, error) {
	pools, err := ListPools(ctx, "rbd")
	if err != nil {
		return nil, err
	}

	images := []string{}
	for _, pool := range pools {
		output, err := run(ctx, "rbd", "ls", "-p", pool, "--format", "json")
		if err != nil {
			return nil, err
		}
		names, err := decodeJSONStringSlice(output)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			images = append(images, imageRef(pool, name))
		}
	}
	return images, nil
}

func imageRef(poolName string, imageName string) string {
	if strings.TrimSpace(poolName) == "" {
		return imageName
	}
	return poolName + "/" + imageName
}
