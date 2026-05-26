package cube

import (
	"bytes"
	"fmt"
	"log"
	"log/syslog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	sshScanInitialDelay = 15 * time.Second
	sshScanMaxAttempts  = 6
	sshScanMaxDelay     = 60 * time.Second
	autoSSHScanInterval = 24 * time.Hour
)

var (
	autoSSHScanMu      sync.Mutex
	autoSSHScanLastRun time.Time
)

// init은 ssh scan 로그를 syslog로 보내도록 logger 출력을 교체한다.
func init() {
	logger, err := syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, "ablestack-sshscan")
	if err == nil {
		log.SetOutput(logger)
	}
}

// AutoSSHKnownHostsScan은 백그라운드에서 주기적으로 known_hosts 갱신을 수행한다.
// bootstrap 상태가 준비된 경우에만 하루 최대 한 번 실행한다.
// - HCI/HCI-FS: scvm/ccvm/wall 준비 완료 시 ablecube + scvm + ccvm 스캔
// - VM/Standalone: ccvm/wall 준비 완료 시 ablecube + ccvm 스캔
func AutoSSHKnownHostsScan() {
	now := time.Now()
	if !isAutoSSHScanDue(now) {
		return
	}
	root, err := loadClusterJSONRoot()
	if err != nil {
		return
	}
	profile, err := extractSystemProfile(root)
	if err != nil {
		return
	}
	clusterType := extractClusterType(root)
	includeAble, includeScvm, includeCcvm, ok := resolveAutoSSHScanTargets(clusterType, profile)
	if !ok {
		return
	}
	hosts, err := collectSSHHostsFromClusterConfig(includeAble, includeScvm, includeCcvm)
	if err != nil || len(hosts) == 0 {
		return
	}
	if !markAutoSSHScanIfDue(now) {
		return
	}
	log.Printf("ssh-scan auto trigger: type=%s targets=%d", strings.ToLower(strings.TrimSpace(clusterType)), len(hosts))
	scheduleSSHKnownHostsScanForHosts(hosts)
}

// scanAttemptResult는 한 번의 ssh-keyscan 시도 결과를 담는다.
type scanAttemptResult struct {
	OpenHosts   []string
	Remaining   []string
	OutputLines int
}

// resolveAutoSSHScanTargets는 클러스터 타입과 bootstrap 상태에 따라 어떤 대상을 스캔할지 결정한다.
func resolveAutoSSHScanTargets(clusterType string, profile ClusterSystemProfile) (bool, bool, bool, bool) {
	switch strings.ToLower(strings.TrimSpace(clusterType)) {
	case "ablestack-hci", "ablestack-hci-filesystem":
		if !isBootstrapFlagTrue(profile.Bootstrap.Scvm) || !isBootstrapFlagTrue(profile.Bootstrap.Ccvm) || !isBootstrapFlagTrue(profile.Bootstrap.Wall) {
			return false, false, false, false
		}
		return true, true, true, true
	case "ablestack-vm", "ablestack-standalone":
		if !isBootstrapFlagTrue(profile.Bootstrap.Ccvm) || !isBootstrapFlagTrue(profile.Bootstrap.Wall) {
			return false, false, false, false
		}
		return true, false, true, true
	default:
		return false, false, false, false
	}
}

// isBootstrapFlagTrue는 bootstrap 문자열 값이 true인지 판별한다.
func isBootstrapFlagTrue(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

// isAutoSSHScanDue는 마지막 자동 스캔 이후 하루가 지났는지 확인한다.
func isAutoSSHScanDue(now time.Time) bool {
	autoSSHScanMu.Lock()
	defer autoSSHScanMu.Unlock()
	if autoSSHScanLastRun.IsZero() {
		return true
	}
	return now.Sub(autoSSHScanLastRun) >= autoSSHScanInterval
}

// markAutoSSHScanIfDue는 자동 스캔 실행 시각을 갱신하고 중복 실행을 막는다.
func markAutoSSHScanIfDue(now time.Time) bool {
	autoSSHScanMu.Lock()
	defer autoSSHScanMu.Unlock()
	if !autoSSHScanLastRun.IsZero() && now.Sub(autoSSHScanLastRun) < autoSSHScanInterval {
		return false
	}
	autoSSHScanLastRun = now
	return true
}

// collectHostsAliasesByIP는 /etc/hosts에서 지정한 IP에 연결된 alias 이름을 수집한다.
func collectHostsAliasesByIP(ipSet map[string]struct{}) ([]string, error) {
	if len(ipSet) == 0 {
		return nil, nil
	}
	raw, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(raw), "\n")
	hosts := make([]string, 0)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ip := fields[0]
		if _, ok := ipSet[ip]; !ok {
			continue
		}
		for _, name := range fields[1:] {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			hosts = append(hosts, name)
		}
	}
	return hosts, nil
}

// runSSHKeyscan은 주어진 host 목록에 대해 ssh-keyscan을 실행한다.
func runSSHKeyscan(hosts []string, port int) ([]byte, error) {
	if len(hosts) == 0 {
		return nil, nil
	}
	args := []string{"-4", "-T", "1"}
	if port != 22 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	args = append(args, hosts...)
	cmd := exec.Command("ssh-keyscan", args...)
	return cmd.CombinedOutput()
}

// scanAndUpdateKnownHostsForHosts는 열린 SSH 포트가 있는 host만 골라 known_hosts를 갱신한다.
func scanAndUpdateKnownHostsForHosts(hosts []string, port int) (scanAttemptResult, error) {
	if len(hosts) == 0 {
		return scanAttemptResult{}, nil
	}
	openHosts := make([]string, 0)
	remaining := make([]string, 0)
	for _, host := range hosts {
		if strings.TrimSpace(host) == "" {
			continue
		}
		if isPortOpen(host, port, 1*time.Second) {
			openHosts = append(openHosts, host)
		} else {
			remaining = append(remaining, host)
		}
	}
	if len(openHosts) == 0 {
		return scanAttemptResult{Remaining: remaining}, nil
	}
	output, err := runSSHKeyscan(openHosts, port)
	if err != nil && len(output) == 0 {
		return scanAttemptResult{OpenHosts: openHosts, Remaining: remaining}, err
	}
	if err := updateKnownHosts(openHosts, output, port); err != nil {
		return scanAttemptResult{OpenHosts: openHosts, Remaining: remaining}, err
	}
	return scanAttemptResult{
		OpenHosts:   openHosts,
		Remaining:   remaining,
		OutputLines: countNonEmptyLines(output),
	}, nil
}

// updateKnownHosts는 기존 키를 제거한 뒤 새 ssh-keyscan 결과를 known_hosts에 append한다.
func updateKnownHosts(hosts []string, output []byte, port int) error {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		home = os.Getenv("HOME")
	}
	if strings.TrimSpace(home) == "" {
		return fmt.Errorf("home directory not found")
	}

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return err
	}
	knownHostsFile := filepath.Join(sshDir, "known_hosts")
	log.Printf("ssh-scan update known_hosts: path=%s hosts=%d port=%d", knownHostsFile, len(hosts), port)

	for _, host := range hosts {
		_ = exec.Command("ssh-keygen", "-R", host, "-f", knownHostsFile).Run()
		if port != 22 {
			_ = exec.Command("ssh-keygen", "-R", fmt.Sprintf("[%s]:%d", host, port), "-f", knownHostsFile).Run()
		}
	}

	if len(bytes.TrimSpace(output)) == 0 {
		return nil
	}
	file, err := os.OpenFile(knownHostsFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := file.Write(output); err != nil {
		return err
	}
	if !bytes.HasSuffix(output, []byte("\n")) {
		if _, err := file.Write([]byte("\n")); err != nil {
			return err
		}
	}
	log.Printf("ssh-scan update known_hosts: appended_lines=%d path=%s", countNonEmptyLines(output), knownHostsFile)
	return nil
}

// isPortOpen은 지정한 host:port에 TCP 연결이 가능한지 확인한다.
func isPortOpen(host string, port int, timeout time.Duration) bool {
	address := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// getSSHScanPort는 sshd 설정을 읽어 실제 SSH 포트를 결정한다.
func getSSHScanPort() int {
	if port := readSSHDPort(); port > 0 {
		return port
	}
	return 22
}

// readSSHDPort는 /etc/ssh/sshd_config에서 기본 Port 값을 읽는다.
func readSSHDPort() int {
	raw, err := os.ReadFile("/etc/ssh/sshd_config")
	if err != nil {
		return 0
	}
	lines := strings.Split(string(raw), "\n")
	port := 0
	inMatch := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "match ") {
			inMatch = true
			continue
		}
		if inMatch {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if strings.EqualFold(fields[0], "Port") {
			val, err := strconv.Atoi(fields[1])
			if err == nil && val > 0 && val <= 65535 {
				port = val
			}
		}
	}
	return port
}

// formatHosts는 host 목록을 로그 출력용 콤마 문자열로 합친다.
func formatHosts(hosts []string) string {
	if len(hosts) == 0 {
		return ""
	}
	return strings.Join(hosts, ",")
}

// mapKeys는 set 형태 맵에서 key 목록만 추출한다.
func mapKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	return keys
}

// countNonEmptyLines는 비어 있지 않은 줄 수를 계산해 로그에 사용한다.
func countNonEmptyLines(input []byte) int {
	trimmed := bytes.TrimSpace(input)
	if len(trimmed) == 0 {
		return 0
	}
	return bytes.Count(trimmed, []byte("\n")) + 1
}
