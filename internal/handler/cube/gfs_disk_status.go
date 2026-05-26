package cube

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type GFSDiskStatusResponse = CubeModel.GFSDiskStatusResponse

const gfsDiskStatusCommandTimeout = 10 * time.Second

// GetGFSDiskStatus godoc
//
//	@Summary		GFS Disk Status
//	@Description	GFS2로 마운트된 디스크 목록을 조회합니다.
//	@Tags			CUBE - GFS
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Success		200	{object}	CubeModel.GFSDiskStatusResponse
//	@Failure		500	{object}	CubeModel.GFSDiskStatusResponse
//	@Router			/cube/gfs/disk/status [get]
func GetGFSDiskStatus(context *gin.Context) {
	status, err := loadGFSDiskStatus()
	if err != nil {
		context.JSON(http.StatusInternalServerError, GFSDiskStatusResponse{
			Code: http.StatusInternalServerError,
			Val:  err.Error(),
		})
		return
	}
	if len(status.Blockdevices) == 0 {
		context.JSON(http.StatusInternalServerError, GFSDiskStatusResponse{
			Code: http.StatusInternalServerError,
			Val:  map[string]string{"message": "GFS2로 마운트된 디스크가 없습니다."},
		})
		return
	}

	context.IndentedJSON(http.StatusOK, GFSDiskStatusResponse{
		Code: http.StatusOK,
		Val:  status,
	})
}

func loadGFSDiskStatus() (CubeModel.GFSDiskStatusValue, error) {
	lsblkOut, err := runGFSDiskLsblk()
	if err != nil {
		return CubeModel.GFSDiskStatusValue{}, err
	}
	blockdevices, err := CubeModel.ParseGFSLSBLK([]byte(lsblkOut))
	if err != nil {
		return CubeModel.GFSDiskStatusValue{}, err
	}

	mounts, err := readGFS2Mounts()
	if err != nil {
		return CubeModel.GFSDiskStatusValue{}, err
	}

	osType := ""
	if cfg, err := loadClusterConfigSection(); err == nil && cfg != nil {
		osType = strings.TrimSpace(cfg.Type)
	}

	multipathMode := isGFSMultipathMode(blockdevices)
	diskIDByKname := readGFSDiskIDByKname()
	return CubeModel.BuildGFSDiskStatus(blockdevices, mounts, osType, multipathMode, diskIDByKname), nil
}

func runGFSDiskLsblk() (string, error) {
	out, timedOut, err := runCommandOutputWithEnv("lsblk", gfsDiskStatusCommandTimeout, gfsDiskCommandEnv(), "-J", "-o", "name,kname,path,size,group,type,mountpoint,dm_uuid")
	if timedOut {
		return out, fmt.Errorf("lsblk timed out after %s", gfsDiskStatusCommandTimeout)
	}
	if err == nil {
		return out, nil
	}
	if strings.Contains(out, "unknown column") && strings.Contains(out, "dm_uuid") {
		out, timedOut, err = runCommandOutputWithEnv("lsblk", gfsDiskStatusCommandTimeout, gfsDiskCommandEnv(), "-J", "-o", "name,kname,path,size,group,type,mountpoint")
		if timedOut {
			return out, fmt.Errorf("lsblk timed out after %s", gfsDiskStatusCommandTimeout)
		}
	}
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("lsblk failed: %s", msg)
	}
	return out, nil
}

func readGFS2Mounts() ([]CubeModel.GFSMount, error) {
	data, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		data, err = os.ReadFile("/proc/mounts")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read mounts: %w", err)
	}
	return CubeModel.ParseGFS2Mounts(data), nil
}

func isGFSMultipathMode(blockdevices []CubeModel.GFSBlockDevice) bool {
	out, timedOut, err := runCommandOutputWithEnv("systemctl", 2*time.Second, gfsDiskCommandEnv(), "is-active", "multipathd")
	if !timedOut && err == nil && strings.TrimSpace(out) == "active" {
		return true
	}
	return CubeModel.HasGFSMultipathDevice(blockdevices)
}

func readGFSDiskIDByKname() map[string][]string {
	const byIDDir = "/dev/disk/by-id"

	result := map[string][]string{}
	entries, err := os.ReadDir(byIDDir)
	if err != nil {
		return result
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		if !strings.Contains(name, "dm-uuid") || strings.Contains(name, "LVM") {
			continue
		}

		fullPath := filepath.Join(byIDDir, name)
		target, err := os.Readlink(fullPath)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(byIDDir, target)
		}
		kname := filepath.Base(filepath.Clean(target))
		if kname == "" || kname == "." || kname == "/" {
			continue
		}
		result[kname] = append(result[kname], fullPath)
	}
	return result
}

func gfsDiskCommandEnv() []string {
	return append([]string{"LANG=en_US.utf-8", "LANGUAGE=en"}, os.Environ()...)
}
