package cube

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
)

const (
	autoCCVMSnapHour   = 1
	autoCCVMSnapMinute = 0
)

var autoCCVMSnapSchedulerOnce sync.Once

// AutoCCVMSnapshotBackup은 API 서버 내부에서 CCVM snapshot 자동 백업 스케줄러를 시작한다.
// controller의 10초 주기 StatusRegister에서 여러 번 호출되어도 실제 scheduler는 한 번만 실행된다.
func AutoCCVMSnapshotBackup() {
	autoCCVMSnapSchedulerOnce.Do(func() {
		go runAutoCCVMSnapScheduler()
	})
}

func runAutoCCVMSnapScheduler() {
	for {
		next := nextAutoCCVMSnapTime(time.Now())
		time.Sleep(time.Until(next))
		triggerAutoCCVMSnapshotBackup(next)
	}
}

func nextAutoCCVMSnapTime(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), autoCCVMSnapHour, autoCCVMSnapMinute, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func triggerAutoCCVMSnapshotBackup(scheduleTime time.Time) {
	root, err := loadClusterJSONRoot()
	if err != nil {
		log.Printf("ccvm-snap auto skip: failed to read cluster.json: %v", err)
		return
	}

	profile, err := extractSystemProfile(root)
	if err != nil {
		log.Printf("ccvm-snap auto skip: failed to read systemProfile: %v", err)
		return
	}
	if !isBootstrapFlagTrue(profile.Bootstrap.Ccvm) {
		log.Printf("ccvm-snap auto skip: bootstrap.ccvm is not true")
		return
	}

	clusterType := extractClusterType(root)
	if !isHCITarget(clusterType) {
		log.Printf("ccvm-snap auto skip: unsupported cluster type=%s", strings.TrimSpace(clusterType))
		return
	}

	cfg, err := loadClusterConfigSection()
	if err != nil {
		log.Printf("ccvm-snap auto skip: failed to load clusterConfig: %v", err)
		return
	}
	if !isLocalAutoCCVMSnapOwner(cfg) {
		return
	}

	snapName := "auto-" + scheduleTime.Format("2006-01-02")
	resp := runCCVMSnapResolvedFromPCS(cfg, CCVMSnapRequest{
		Action:   "backup",
		SnapName: snapName,
	})
	if resp.Code != http.StatusOK {
		log.Printf("ccvm-snap auto failed: snap=%s code=%d message=%s", snapName, resp.Code, resp.Message)
		return
	}
	log.Printf("ccvm-snap auto success: snap=%s target=%s", snapName, resp.Target)
}

func isLocalAutoCCVMSnapOwner(cfg *CubeModel.ClusterConfigSection) bool {
	if cfg == nil {
		return false
	}

	currentDC, err := loadCCVMPcsCurrentDC()
	if err != nil {
		log.Printf("ccvm-snap auto skip: failed to read pcs current dc: %v", err)
		return false
	}
	if strings.TrimSpace(currentDC) == "" {
		log.Printf("ccvm-snap auto skip: pcs current dc is empty")
		return false
	}

	target, ok := resolveCCVMSnapStartedTarget(cfg, currentDC)
	if ok && strings.TrimSpace(target.Target) != "" {
		return isLocalTarget(target.Target)
	}
	return isLocalTarget(currentDC)
}
