package cube

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type TimeServerRequest = CubeModel.TimeServerRequest
type TimeServerResponse = CubeModel.TimeServerResponse
type TimeServerConfig = CubeModel.TimeServerConfig

const (
	timeServerChronyPath     = "/etc/chrony.conf"
	timeServerRestartTimeout = 15 * time.Second
)

// ConfigureTimeServer는 수동 재적용용 endpoint다.
// cluster apply insert 흐름에서 자동 실행되므로 Swagger에는 노출하지 않는다.
func ConfigureTimeServer(context *gin.Context) {
	var req TimeServerRequest
	if context.Request != nil && context.Request.Body != nil && context.Request.ContentLength != 0 {
		if err := context.ShouldBindJSON(&req); err != nil {
			context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
				ErrCode: http.StatusBadRequest,
				Message: "invalid request",
			})
			return
		}
	}

	result, err := applyTimeServerConfig(req)
	if err != nil {
		context.JSON(http.StatusInternalServerError, timeServerError(err.Error()))
		return
	}

	context.JSON(http.StatusOK, TimeServerResponse{
		Code:    http.StatusOK,
		Val:     result,
		Message: "ok",
	})
}

func applyTimeServerConfig(req TimeServerRequest) (TimeServerConfig, error) {
	normalizeTimeServerRequest(&req)
	cfg, err := loadClusterConfigSection()
	if err != nil {
		return TimeServerConfig{}, fmt.Errorf("failed to read cluster.json: %w", err)
	}

	result, chronyText, err := buildTimeServerConfig(cfg, req)
	if err != nil {
		return TimeServerConfig{}, err
	}

	if err := os.WriteFile(timeServerChronyPath, []byte(chronyText), 0o644); err != nil {
		return TimeServerConfig{}, err
	}
	if err := os.Chmod(timeServerChronyPath, 0o644); err != nil {
		return TimeServerConfig{}, err
	}

	out, timedOut, err := runCommandOutputWithEnv("systemctl", timeServerRestartTimeout, nil, "restart", "chronyd")
	if timedOut {
		return TimeServerConfig{}, fmt.Errorf("chronyd restart timed out")
	}
	if err != nil {
		return TimeServerConfig{}, fmt.Errorf("%s", firstNonEmpty(strings.TrimSpace(out), err.Error()))
	}
	result.Restarted = true
	return result, nil
}

func normalizeTimeServerRequest(req *TimeServerRequest) {
	if req == nil {
		return
	}
	req.ExternalTimeserver = strings.TrimSpace(req.ExternalTimeserver)
	req.TimeServers = dedupeHosts(req.TimeServers)
}

func buildTimeServerConfig(cfg *CubeModel.ClusterConfigSection, req TimeServerRequest) (TimeServerConfig, string, error) {
	if cfg == nil {
		return TimeServerConfig{}, "", fmt.Errorf("cluster config not found")
	}

	selfIndex := ""
	selfHost, err := findSelfHost(cfg)
	if err == nil && selfHost != nil {
		selfIndex = strings.TrimSpace(selfHost.Index)
	}

	externalTimeserver := strings.TrimSpace(req.ExternalTimeserver)
	if externalTimeserver == "" {
		externalTimeserver = strings.TrimSpace(cfg.ExternalTimeserver)
	}

	timeServers := req.TimeServers
	if len(timeServers) == 0 {
		timeServers = defaultTimeServersFromClusterConfig(cfg)
	}
	timeServers = dedupeHosts(timeServers)
	appliedTimeServers := appliedTimeServersForSelf(timeServers, selfHost)

	mode := "none"
	localStratum := false
	if len(appliedTimeServers) > 0 {
		mode = "internal"
		localStratum = true
	} else if externalTimeserver != "" {
		mode = "external"
	}

	result := TimeServerConfig{
		ConfigPath:          timeServerChronyPath,
		Mode:                mode,
		SelfIndex:           selfIndex,
		TimeServers:         timeServers,
		AppliedTimeServers:  appliedTimeServers,
		ExternalTimeserver:  externalTimeserver,
		LocalStratumEnabled: localStratum,
	}
	text := buildChronyConfigText(externalTimeserver, appliedTimeServers, localStratum)
	return result, text, nil
}

func defaultTimeServersFromClusterConfig(cfg *CubeModel.ClusterConfigSection) []string {
	byIndex := map[string]string{}
	for _, host := range cfg.Hosts {
		index := strings.TrimSpace(host.Index)
		if index != "1" && index != "2" {
			continue
		}
		target := firstNonEmpty(host.Ablecube, host.Hostname)
		if target != "" {
			byIndex[index] = target
		}
	}

	indexes := make([]string, 0, len(byIndex))
	for index := range byIndex {
		indexes = append(indexes, index)
	}
	sort.Strings(indexes)

	servers := make([]string, 0, len(indexes))
	for _, index := range indexes {
		servers = append(servers, byIndex[index])
	}
	return servers
}

func appliedTimeServersForSelf(timeServers []string, selfHost *CubeModel.ClusterHost) []string {
	if len(timeServers) == 0 {
		return nil
	}
	selfTargets := map[string]struct{}{}
	if selfHost != nil {
		for _, target := range []string{selfHost.Ablecube, selfHost.Hostname, selfHost.ScvmMngt, selfHost.AblecubePn, selfHost.Scvm, selfHost.ScvmCn} {
			target = strings.TrimSpace(target)
			if target != "" {
				selfTargets[target] = struct{}{}
			}
		}
	}

	applied := make([]string, 0, len(timeServers))
	for _, server := range timeServers {
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		if _, ok := selfTargets[server]; ok {
			continue
		}
		if isLocalTarget(server) {
			continue
		}
		applied = append(applied, server)
	}
	return dedupeHosts(applied)
}

func buildChronyConfigText(externalTimeserver string, timeServers []string, localStratum bool) string {
	var b strings.Builder
	b.WriteString("# These servers were defined in the installation:\n")
	b.WriteString("# Use public servers from the pool.ntp.org project.\n")
	b.WriteString("# Please consider joining the pool (http://www.pool.ntp.org/join.html).\n")
	if externalTimeserver != "" {
		b.WriteString("server " + externalTimeserver + " iburst\n")
	}
	for i, server := range timeServers {
		if i == 1 {
			b.WriteString("server " + server + " prefer iburst minpoll 4 maxpoll 6\n")
		} else {
			b.WriteString("server " + server + " iburst minpoll 4 maxpoll 6\n")
		}
	}
	b.WriteString("\n")
	b.WriteString("# Record the rate at which the system clock gains/losses time.\n")
	b.WriteString("driftfile /var/lib/chrony/drift\n\n")
	b.WriteString("# Allow the system clock to be stepped in the first three updates\n")
	b.WriteString("# if its offset is larger than 1 second.\n")
	b.WriteString("makestep 1.0 3\n\n")
	b.WriteString("# Enable kernel synchronization of the real-time clock (RTC).\n")
	b.WriteString("rtcsync\n\n")
	b.WriteString("# Enable hardware timestamping on all interfaces that support it.\n")
	b.WriteString("#hwtimestamp *\n\n")
	b.WriteString("# Increase the minimum number of selectable sources required to adjust\n")
	b.WriteString("# the system clock.\n")
	b.WriteString("#minsources 2\n\n")
	b.WriteString("# Allow NTP client access from local network.\n")
	b.WriteString("allow 0.0.0.0/0\n\n")
	b.WriteString("# Serve time even if not synchronized to a time source.\n")
	if localStratum {
		b.WriteString("local stratum 10\n")
	} else {
		b.WriteString("#local stratum 10\n")
	}
	b.WriteString("\n")
	b.WriteString("# Specify file containing keys for NTP authentication.\n")
	b.WriteString("keyfile /etc/chrony.keys\n\n")
	b.WriteString("# Get TAI-UTC offset and leap seconds from the system tz database.\n")
	b.WriteString("leapsectz right/UTC\n\n")
	b.WriteString("# Specify directory for log files.\n")
	b.WriteString("logdir /var/log/chrony\n\n")
	b.WriteString("# Select which information is logged.\n")
	b.WriteString("#log measurements statistics tracking\n")
	return b.String()
}

func timeServerError(message string) TimeServerResponse {
	return TimeServerResponse{
		Code:    http.StatusInternalServerError,
		Val:     message,
		Message: message,
	}
}
