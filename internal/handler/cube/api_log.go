package cube

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const defaultAbleStackAPILogPath = "/var/log/ablestack-api.log"

var ableStackAPILogMu sync.Mutex

func appendAbleStackAPILog(component string, format string, args ...any) {
	path := resolveAbleStackAPILogPath()
	line := formatAbleStackAPILogLine(component, format, args...)

	ableStackAPILogMu.Lock()
	defer ableStackAPILogMu.Unlock()

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("ablestack api log mkdir failed: path=%s err=%v", path, err)
			return
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("ablestack api log open failed: path=%s err=%v", path, err)
		return
	}
	defer file.Close()

	if _, err := file.WriteString(line); err != nil {
		log.Printf("ablestack api log write failed: path=%s err=%v", path, err)
	}
}

func resolveAbleStackAPILogPath() string {
	if path := strings.TrimSpace(os.Getenv("ABLESTACK_API_LOG")); path != "" {
		return path
	}
	return defaultAbleStackAPILogPath
}

func formatAbleStackAPILogLine(component string, format string, args ...any) string {
	component = strings.TrimSpace(component)
	if component == "" {
		component = "api"
	}
	message := strings.TrimSpace(fmt.Sprintf(format, args...))
	message = strings.NewReplacer("\r", " ", "\n", " ").Replace(message)
	return fmt.Sprintf("%s component=%s %s\n", time.Now().Format(time.RFC3339), component, message)
}
