package cube

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	glueClusterStatusCommandTimeout = 3 * time.Second
	glueClusterStatusNA             = "N/A"
	glueClusterStatusDefaultHealth  = "HEALTH_ERR"
)

// GlueClusterStatusResponse는 스토리지 클러스터 상태 상세 조회 응답이다.
// @name GlueClusterStatusResponse
type GlueClusterStatusResponse struct {
	ClusterStatus     string         `json:"cluster_status" example:"HEALTH_WARN"`
	OSD               *int           `json:"osd,omitempty" example:"19"`
	OSDUp             *int           `json:"osd_up,omitempty" example:"19"`
	MonGW1            *int           `json:"mon_gw1,omitempty" example:"3"`
	MonGW2            []string       `json:"mon_gw2,omitempty" example:"scvm1,scvm2,scvm3"`
	Mgr               string         `json:"mgr" example:"scvm1"`
	MgrCnt            *int           `json:"mgr_cnt,omitempty" example:"2"`
	Pools             *int           `json:"pools,omitempty" example:"8"`
	Avail             string         `json:"avail" example:"14.81 TiB"`
	Used              string         `json:"used" example:"1.77 TiB"`
	UsagePercentage   string         `json:"usage_percentage" example:"10.68%"`
	MaintenanceStatus bool           `json:"maintenance_status"`
	JSONRaw           map[string]any `json:"json_raw,omitempty" swaggertype:"object"`
}

type cephStatusSummary struct {
	Health struct {
		Status string `json:"status"`
	} `json:"health"`
	Mgrmap struct {
		Services struct {
			Dashboard string `json:"dashboard"`
		} `json:"services"`
	} `json:"mgrmap"`
	Monmap struct {
		NumMons int `json:"num_mons"`
	} `json:"monmap"`
	QuorumNames []string `json:"quorum_names"`
	Osdmap      struct {
		NumOsds   int `json:"num_osds"`
		NumUpOsds int `json:"num_up_osds"`
	} `json:"osdmap"`
	Pgmap struct {
		NumPools int `json:"num_pools"`
	} `json:"pgmap"`
}

type cephMgrStat struct {
	ActiveName  string `json:"active_name"`
	NumStandbys int    `json:"num_standbys"`
}

type cephDFDetail struct {
	Stats struct {
		TotalAvailBytes   int64   `json:"total_avail_bytes"`
		TotalUsedBytes    int64   `json:"total_used_bytes"`
		TotalUsedRawRatio float64 `json:"total_used_raw_ratio"`
	} `json:"stats"`
}

type cephOSDDump struct {
	Flags    string   `json:"flags"`
	FlagsSet []string `json:"flags_set"`
}

// GlueClusterStatusDetail는 Ceph 명령 결과를 모아 스토리지 클러스터 상태 상세 응답을 만든다.
func GlueClusterStatusDetail() (*GlueClusterStatusResponse, error) {
	resp := &GlueClusterStatusResponse{
		ClusterStatus:   glueClusterStatusDefaultHealth,
		Mgr:             glueClusterStatusNA,
		Avail:           glueClusterStatusNA,
		Used:            glueClusterStatusNA,
		UsagePercentage: glueClusterStatusNA,
	}

	var wg sync.WaitGroup

	var (
		status    cephStatusSummary
		rawStatus map[string]any
		statusErr error

		mgrStat cephMgrStat
		mgrErr  error

		dfDetail cephDFDetail
		dfErr    error

		maintenance    bool
		maintenanceErr error
	)

	// Independent Ceph queries are issued concurrently so total latency is
	// bounded by the slowest command, not the sum of all commands.
	wg.Add(4)
	go func() {
		defer wg.Done()
		status, rawStatus, statusErr = loadCephStatusSummary()
	}()
	go func() {
		defer wg.Done()
		mgrStat, mgrErr = loadCephMgrStat()
	}()
	go func() {
		defer wg.Done()
		dfDetail, dfErr = loadCephDFDetail()
	}()
	go func() {
		defer wg.Done()
		maintenance, maintenanceErr = loadMaintenanceStatus()
	}()
	wg.Wait()

	if statusErr != nil {
		return resp, statusErr
	}

	if strings.TrimSpace(status.Health.Status) != "" {
		resp.ClusterStatus = strings.TrimSpace(status.Health.Status)
	}
	resp.OSD = intPtr(status.Osdmap.NumOsds)
	resp.OSDUp = intPtr(status.Osdmap.NumUpOsds)
	resp.MonGW1 = intPtr(status.Monmap.NumMons)
	resp.MonGW2 = append(resp.MonGW2, status.QuorumNames...)
	resp.Pools = intPtr(status.Pgmap.NumPools)
	resp.JSONRaw = rawStatus

	if mgrErr == nil {
		activeName := strings.TrimSpace(mgrStat.ActiveName)
		if activeName != "" {
			resp.Mgr = activeName
		}
		mgrCnt := mgrStat.NumStandbys
		if activeName != "" {
			mgrCnt++
		}
		resp.MgrCnt = intPtr(mgrCnt)
	}

	if dfErr == nil {
		resp.Avail = formatBinarySize(dfDetail.Stats.TotalAvailBytes)
		resp.Used = formatBinarySize(dfDetail.Stats.TotalUsedBytes)
		resp.UsagePercentage = formatPercent(dfDetail.Stats.TotalUsedRawRatio)
	}

	if maintenanceErr == nil {
		resp.MaintenanceStatus = maintenance
	}

	return resp, nil
}

// GlueDashboardURL reads the Ceph manager dashboard URL used by Storage Center.
func GlueDashboardURL() (string, error) {
	status, _, err := loadCephStatusSummary()
	if err != nil {
		return "", err
	}
	dashboardURL := strings.TrimSpace(status.Mgrmap.Services.Dashboard)
	if dashboardURL == "" {
		return "", fmt.Errorf("glue dashboard 정보를 확인할 수 없습니다.")
	}
	return dashboardURL, nil
}

func loadCephStatusSummary() (cephStatusSummary, map[string]any, error) {
	stdout, err := runCephCommand("-s", "-f", "json")
	if err != nil {
		return cephStatusSummary{}, nil, err
	}

	var summary cephStatusSummary
	if err := json.Unmarshal(stdout, &summary); err != nil {
		return cephStatusSummary{}, nil, fmt.Errorf("failed to parse ceph status: %w", err)
	}

	raw := map[string]any{}
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return cephStatusSummary{}, nil, fmt.Errorf("failed to parse ceph status raw payload: %w", err)
	}

	return summary, raw, nil
}

func loadCephMgrStat() (cephMgrStat, error) {
	stdout, err := runCephCommand("mgr", "stat", "-f", "json")
	if err != nil {
		return cephMgrStat{}, err
	}

	var status cephMgrStat
	if err := json.Unmarshal(stdout, &status); err != nil {
		return cephMgrStat{}, fmt.Errorf("failed to parse ceph mgr stat: %w", err)
	}
	return status, nil
}

func loadCephDFDetail() (cephDFDetail, error) {
	stdout, err := runCephCommand("df", "detail", "-f", "json")
	if err != nil {
		return cephDFDetail{}, err
	}

	var detail cephDFDetail
	if err := json.Unmarshal(stdout, &detail); err != nil {
		return cephDFDetail{}, fmt.Errorf("failed to parse ceph df detail: %w", err)
	}
	return detail, nil
}

func loadMaintenanceStatus() (bool, error) {
	stdout, err := runCephCommand("osd", "dump", "-f", "json")
	if err != nil {
		return false, err
	}

	var dump cephOSDDump
	if err := json.Unmarshal(stdout, &dump); err != nil {
		return false, fmt.Errorf("failed to parse ceph osd dump: %w", err)
	}
	return hasNooutFlag(dump, stdout), nil
}

// runCephCommand는 locale을 고정하고 3초 안에 종료되지 않으면 중단한다.
func runCephCommand(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), glueClusterStatusCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ceph", args...)
	cmd.Env = buildGlueCommandEnv()

	stdout, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("ceph %s timed out after %s", strings.Join(args, " "), glueClusterStatusCommandTimeout)
	}
	if err != nil {
		msg := strings.TrimSpace(string(stdout))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ceph %s failed: %s", strings.Join(args, " "), msg)
	}
	return stdout, nil
}

func buildGlueCommandEnv() []string {
	env := os.Environ()
	env = append(env, "LANG=en_US.utf-8", "LANGUAGE=en")
	return env
}

func hasNooutFlag(dump cephOSDDump, raw []byte) bool {
	for _, token := range splitFlagTokens(dump.Flags) {
		if token == "noout" {
			return true
		}
	}
	for _, token := range dump.FlagsSet {
		if strings.EqualFold(strings.TrimSpace(token), "noout") {
			return true
		}
	}
	return strings.Contains(strings.ToLower(string(raw)), "\"noout\"")
}

func splitFlagTokens(flags string) []string {
	return strings.FieldsFunc(strings.ToLower(flags), func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	})
}

func formatBinarySize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}

	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	value := float64(size)
	unitIndex := -1
	for value >= 1024 && unitIndex < len(units)-1 {
		value /= 1024
		unitIndex++
	}

	if unitIndex < 0 {
		return fmt.Sprintf("%d B", size)
	}
	return fmt.Sprintf("%s %s", trimTrailingZeros(fmt.Sprintf("%.2f", value)), units[unitIndex])
}

func formatPercent(ratio float64) string {
	return trimTrailingZeros(fmt.Sprintf("%.2f", ratio*100)) + "%"
}

func trimTrailingZeros(value string) string {
	value = strings.TrimSuffix(value, "00")
	value = strings.TrimSuffix(value, "0")
	value = strings.TrimSuffix(value, ".")
	return value
}

func intPtr(value int) *int {
	v := value
	return &v
}
