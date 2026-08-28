package glueservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const bytesPerGiB int64 = 1024 * 1024 * 1024

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

// ResizeImage는 RBD image를 GiB 단위의 새 크기로 확장한다.
// 현재 크기보다 작거나 같은 값은 축소 방지를 위해 거부한다.
func ResizeImage(ctx context.Context, poolName string, imageName string, sizeGiB int64) (map[string]any, error) {
	poolName, imageName, ref, err := normalizeImageReference(poolName, imageName)
	if err != nil {
		return nil, err
	}
	if sizeGiB <= 0 {
		return nil, fmt.Errorf("size must be greater than zero")
	}
	if sizeGiB > maxInt64/bytesPerGiB {
		return nil, fmt.Errorf("size is too large")
	}

	currentBytes, err := rbdImageSizeBytes(ctx, ref)
	if err != nil {
		return nil, err
	}
	newBytes := sizeGiB * bytesPerGiB
	if newBytes <= currentBytes {
		return nil, fmt.Errorf("size must be greater than current image size")
	}

	sizeMiB := sizeGiB * 1024
	if _, err := run(ctx, "rbd", "resize", "--size", strconv.FormatInt(sizeMiB, 10), ref); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":            "success",
		"pool":              poolName,
		"image":             imageName,
		"size_gib":          sizeGiB,
		"size_mib":          sizeMiB,
		"previous_size_gib": ceilGiB(currentBytes),
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

func normalizeImageReference(poolName string, imageName string) (string, string, string, error) {
	poolName = strings.TrimSpace(poolName)
	imageName = strings.TrimSpace(imageName)
	if poolName == "" {
		if err := ValidateImageRef(imageName); err != nil {
			return "", "", "", err
		}
		parts := strings.Split(imageName, "/")
		if len(parts) != 2 {
			return "", "", "", fmt.Errorf("image_name must be in pool/image format")
		}
		poolName = parts[0]
		imageName = parts[1]
	}
	if err := ValidatePoolName(poolName); err != nil {
		return "", "", "", err
	}
	if err := ValidateImageName(imageName); err != nil {
		return "", "", "", err
	}
	return poolName, imageName, imageRef(poolName, imageName), nil
}

func rbdImageSizeBytes(ctx context.Context, ref string) (int64, error) {
	output, err := run(ctx, "rbd", "info", ref, "--format", "json")
	if err != nil {
		return 0, err
	}
	var info struct {
		Size int64 `json:"size"`
	}
	if err := json.Unmarshal(output, &info); err != nil {
		return 0, fmt.Errorf("decode rbd image info: %w", err)
	}
	if info.Size <= 0 {
		return 0, fmt.Errorf("invalid rbd image size")
	}
	return info.Size, nil
}

func ceilGiB(sizeBytes int64) int64 {
	sizeGiB := sizeBytes / bytesPerGiB
	if sizeBytes%bytesPerGiB != 0 {
		sizeGiB++
	}
	return sizeGiB
}

const maxInt64 = int64(^uint64(0) >> 1)

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
