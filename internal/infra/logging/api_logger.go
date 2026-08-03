package logging

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	defaultAPILogPath     = "/var/log/ablestack/api.log"
	defaultDetailLogPath  = "/var/log/ablestack/detail.log"
	defaultJobLogPath     = "/var/log/ablestack/job.log"
	defaultArchiveLogDir  = "/var/log/ablestack/archive"
	defaultRetentionDays  = 90
	rotationCheckInterval = time.Minute
	requestIDContextKey   = "ablestack_request_id"
	panicValueContextKey  = "ablestack_panic_value"
	panicStackContextKey  = "ablestack_panic_stack"
	detailContextKey      = "ablestack_detail_entries"
	responseBodyLogLimit  = 8192
	panicStackLogLimit    = 65536
	actionMessageLogLimit = 8192
	archiveDateLayout     = "2006-01-02"
)

var (
	apiLogMu           sync.Mutex
	detailLogMu        sync.Mutex
	jobLogMu           sync.Mutex
	rotationWorkerOnce sync.Once
	requestCounter     uint64
)

type responseCaptureWriter struct {
	gin.ResponseWriter
	body  bytes.Buffer
	limit int
}

func (w *responseCaptureWriter) Write(data []byte) (int, error) {
	w.capture(data)
	return w.ResponseWriter.Write(data)
}

func (w *responseCaptureWriter) WriteString(data string) (int, error) {
	w.captureString(data)
	return w.ResponseWriter.WriteString(data)
}

func (w *responseCaptureWriter) capture(data []byte) {
	if w.limit <= 0 || w.body.Len() >= w.limit {
		return
	}
	remaining := w.limit - w.body.Len()
	if len(data) > remaining {
		data = data[:remaining]
	}
	_, _ = w.body.Write(data)
}

func (w *responseCaptureWriter) captureString(data string) {
	if w.limit <= 0 || w.body.Len() >= w.limit {
		return
	}
	remaining := w.limit - w.body.Len()
	if len(data) > remaining {
		data = data[:remaining]
	}
	_, _ = w.body.WriteString(data)
}

// GinRequestLogger writes one API log entry for every request and a detail log
// entry when the request ends with an error status or a handler-attached error.
func GinRequestLogger() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		rotateConfiguredLogs()

		started := time.Now()
		requestID := resolveRequestID(ctx)
		ctx.Set(requestIDContextKey, requestID)
		ctx.Writer.Header().Set("X-Request-ID", requestID)

		capture := &responseCaptureWriter{
			ResponseWriter: ctx.Writer,
			limit:          responseBodyLogLimit,
		}
		ctx.Writer = capture

		ctx.Next()

		status := ctx.Writer.Status()
		if status == 0 {
			status = http.StatusOK
		}
		elapsed := time.Since(started)
		outcome := "success"
		if status >= http.StatusBadRequest || len(ctx.Errors) > 0 || requestHadPanic(ctx) {
			outcome = "error"
		}

		apiEntry := requestLogEntry(ctx, requestID, status, outcome, elapsed)
		writeJSONLog(resolveAPILogPath(), &apiLogMu, apiEntry)

		if outcome == "error" {
			detailEntry := requestDetailLogEntry(ctx, requestID, status, elapsed, capture.body.String())
			writeJSONLog(resolveDetailLogPath(), &detailLogMu, detailEntry)
		}
	}
}

// GinRecovery records panic details for GinRequestLogger and returns HTTP 500.
func GinRecovery() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				ctx.Set(panicValueContextKey, fmt.Sprint(recovered))
				ctx.Set(panicStackContextKey, limitString(string(debug.Stack()), panicStackLogLimit))
				if !ctx.Writer.Written() {
					ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
						"code":    http.StatusInternalServerError,
						"message": "internal server error",
					})
					return
				}
				ctx.Abort()
			}
		}()
		ctx.Next()
	}
}

// StartRotationWorker periodically rotates logs whose last write date is older
// than the current local date.
func StartRotationWorker() {
	rotationWorkerOnce.Do(func() {
		go func() {
			rotateConfiguredLogs()
			ticker := time.NewTicker(rotationCheckInterval)
			defer ticker.Stop()
			for range ticker.C {
				rotateConfiguredLogs()
			}
		}()
	})
}

// AppendActionLog appends an application-level action event to api.log. Events
// that look like failures are mirrored to detail.log.
func AppendActionLog(component string, format string, args ...any) {
	rotateConfiguredLogs()

	message := sanitizeLogString(fmt.Sprintf(format, args...), actionMessageLogLimit)
	entry := map[string]any{
		"time":      time.Now().Format(time.RFC3339Nano),
		"type":      "action",
		"component": defaultString(strings.TrimSpace(component), "api"),
		"message":   message,
	}
	writeJSONLog(resolveAPILogPath(), &apiLogMu, entry)
	if actionMessageLooksLikeError(message) {
		detailEntry := map[string]any{
			"time":      time.Now().Format(time.RFC3339Nano),
			"type":      "action_error",
			"component": defaultString(strings.TrimSpace(component), "api"),
			"message":   message,
		}
		writeJSONLog(resolveDetailLogPath(), &detailLogMu, detailEntry)
	}
}

// AddDetail attaches diagnostic fields to the current request. GinRequestLogger
// writes these fields to detail.log when the request ends with an error status.
func AddDetail(ctx *gin.Context, component string, operation string, stage string, reason string, fields map[string]any) {
	if ctx == nil {
		return
	}
	entry := map[string]any{
		"component": defaultString(strings.TrimSpace(component), "api"),
		"operation": defaultString(strings.TrimSpace(operation), "operation"),
		"stage":     defaultString(strings.TrimSpace(stage), "stage"),
		"reason":    sanitizeLogString(reason, actionMessageLogLimit),
	}
	if _, file, line, ok := runtime.Caller(1); ok {
		entry["source"] = fmt.Sprintf("%s:%d", trimWorkingDir(file), line)
	}
	for key, value := range sanitizeLogFields(fields) {
		entry[key] = value
	}

	existing, _ := ctx.Get(detailContextKey)
	entries, _ := existing.([]map[string]any)
	entries = append(entries, entry)
	ctx.Set(detailContextKey, entries)
}

func requestLogEntry(ctx *gin.Context, requestID string, status int, outcome string, elapsed time.Duration) map[string]any {
	entry := baseRequestEntry(ctx, requestID, status, elapsed)
	entry["type"] = "api_request"
	entry["outcome"] = outcome
	return entry
}

func requestDetailLogEntry(ctx *gin.Context, requestID string, status int, elapsed time.Duration, responseBody string) map[string]any {
	entry := baseRequestEntry(ctx, requestID, status, elapsed)
	entry["type"] = "api_error"
	if body := sanitizeLogString(responseBody, responseBodyLogLimit); body != "" {
		entry["response_body"] = body
	}
	if errs := ginErrors(ctx); len(errs) > 0 {
		entry["gin_errors"] = errs
	}
	if details := requestDetails(ctx); len(details) > 0 {
		entry["details"] = details
	}
	if value, ok := ctx.Get(panicValueContextKey); ok {
		entry["panic"] = fmt.Sprint(value)
	}
	if stack, ok := ctx.Get(panicStackContextKey); ok {
		entry["stack"] = fmt.Sprint(stack)
	}
	return entry
}

func baseRequestEntry(ctx *gin.Context, requestID string, status int, elapsed time.Duration) map[string]any {
	entry := map[string]any{
		"time":       time.Now().Format(time.RFC3339Nano),
		"request_id": requestID,
		"status":     status,
		"latency_ms": float64(elapsed.Microseconds()) / 1000,
	}
	if ctx == nil || ctx.Request == nil {
		return entry
	}
	entry["method"] = ctx.Request.Method
	if ctx.Request.URL != nil {
		entry["path"] = ctx.Request.URL.Path
		if ctx.Request.URL.RawQuery != "" {
			entry["query"] = ctx.Request.URL.RawQuery
		}
	}
	if route := ctx.FullPath(); route != "" {
		entry["route"] = route
	}
	if clientIP := ctx.ClientIP(); clientIP != "" {
		entry["client_ip"] = clientIP
	}
	if userAgent := strings.TrimSpace(ctx.Request.UserAgent()); userAgent != "" {
		entry["user_agent"] = sanitizeLogString(userAgent, 512)
	}
	return entry
}

func ginErrors(ctx *gin.Context) []string {
	if ctx == nil || len(ctx.Errors) == 0 {
		return nil
	}
	errs := make([]string, 0, len(ctx.Errors))
	for _, err := range ctx.Errors {
		if err == nil {
			continue
		}
		errs = append(errs, sanitizeLogString(err.Error(), actionMessageLogLimit))
	}
	return errs
}

func requestHadPanic(ctx *gin.Context) bool {
	if ctx == nil {
		return false
	}
	_, ok := ctx.Get(panicValueContextKey)
	return ok
}

func requestDetails(ctx *gin.Context) []map[string]any {
	if ctx == nil {
		return nil
	}
	value, ok := ctx.Get(detailContextKey)
	if !ok {
		return nil
	}
	entries, ok := value.([]map[string]any)
	if !ok || len(entries) == 0 {
		return nil
	}
	return entries
}

func resolveRequestID(ctx *gin.Context) string {
	if ctx != nil && ctx.Request != nil {
		if requestID := strings.TrimSpace(ctx.GetHeader("X-Request-ID")); requestID != "" {
			return sanitizeLogString(requestID, 128)
		}
	}
	seq := atomic.AddUint64(&requestCounter, 1)
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatUint(seq, 36)
}

func sanitizeLogFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	sanitized := make(map[string]any, len(fields))
	for key, value := range fields {
		key = strings.TrimSpace(key)
		if key == "" || value == nil {
			continue
		}
		sanitized[key] = sanitizeLogValue(value)
	}
	return sanitized
}

func sanitizeLogValue(value any) any {
	switch typed := value.(type) {
	case string:
		return sanitizeLogString(typed, actionMessageLogLimit)
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeLogString(item, 512))
		}
		return out
	case error:
		return sanitizeLogString(typed.Error(), actionMessageLogLimit)
	default:
		return typed
	}
}

func trimWorkingDir(path string) string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return path
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

func resolveAPILogPath() string {
	if path := strings.TrimSpace(os.Getenv("ABLESTACK_API_LOG")); path != "" {
		return path
	}
	return defaultAPILogPath
}

func resolveDetailLogPath() string {
	if path := strings.TrimSpace(os.Getenv("ABLESTACK_DETAIL_LOG")); path != "" {
		return path
	}
	return defaultDetailLogPath
}

func resolveJobLogPath() string {
	if path := strings.TrimSpace(os.Getenv("ABLESTACK_JOB_LOG")); path != "" {
		return path
	}
	return defaultJobLogPath
}

func resolveArchiveLogDir(logPath string) string {
	if path := strings.TrimSpace(os.Getenv("ABLESTACK_LOG_ARCHIVE_DIR")); path != "" {
		return path
	}
	if filepath.Dir(logPath) == filepath.Dir(defaultAPILogPath) {
		return defaultArchiveLogDir
	}
	return filepath.Join(filepath.Dir(logPath), "archive")
}

func resolveRetentionDays() int {
	raw := strings.TrimSpace(os.Getenv("ABLESTACK_LOG_RETENTION_DAYS"))
	if raw == "" {
		return defaultRetentionDays
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days <= 0 {
		log.Printf("ablestack log retention invalid: value=%q using=%d", raw, defaultRetentionDays)
		return defaultRetentionDays
	}
	return days
}

func writeJSONLog(path string, mu *sync.Mutex, entry map[string]any) {
	if path == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()

	if err := rotateLogIfNeeded(path); err != nil {
		log.Printf("ablestack log rotate failed: path=%s err=%v", path, err)
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("ablestack log mkdir failed: path=%s err=%v", path, err)
			return
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("ablestack log open failed: path=%s err=%v", path, err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(entry); err != nil {
		log.Printf("ablestack log write failed: path=%s err=%v", path, err)
	}
}

func rotateConfiguredLogs() {
	apiPath := resolveAPILogPath()
	detailPath := resolveDetailLogPath()
	jobPath := resolveJobLogPath()
	rotateLogWithLock(apiPath, &apiLogMu)
	rotateLogWithLock(detailPath, &detailLogMu)
	rotateLogWithLock(jobPath, &jobLogMu)
	cleanupConfiguredArchiveLogs([]string{apiPath, detailPath, jobPath}, resolveRetentionDays(), time.Now())
}

func rotateLogWithLock(path string, mu *sync.Mutex) {
	if path == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if err := rotateLogIfNeeded(path); err != nil {
		log.Printf("ablestack log rotate failed: path=%s err=%v", path, err)
	}
}

func rotateLogIfNeeded(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() || info.Size() == 0 {
		return nil
	}

	logDate := info.ModTime().In(time.Local).Format(archiveDateLayout)
	today := time.Now().In(time.Local).Format(archiveDateLayout)
	if logDate >= today {
		return nil
	}

	archivePath := filepath.Join(resolveArchiveLogDir(path), filepath.Base(path)+"-"+logDate+".gz")
	return archiveLogFile(path, archivePath, info.ModTime())
}

func archiveLogFile(path string, archivePath string, modTime time.Time) error {
	if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
		return err
	}

	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()

	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	gzipWriter := gzip.NewWriter(archive)
	gzipWriter.Name = filepath.Base(path)
	gzipWriter.ModTime = modTime

	copyErr := copyLogToArchive(source, gzipWriter)
	closeErr := gzipWriter.Close()
	fileCloseErr := archive.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if fileCloseErr != nil {
		return fileCloseErr
	}
	return os.Remove(path)
}

func copyLogToArchive(source *os.File, gzipWriter *gzip.Writer) error {
	_, err := io.Copy(gzipWriter, source)
	return err
}

func cleanupConfiguredArchiveLogs(logPaths []string, retentionDays int, now time.Time) {
	seen := map[string]struct{}{}
	for _, logPath := range logPaths {
		archiveDir := resolveArchiveLogDir(logPath)
		if archiveDir == "" {
			continue
		}
		if _, ok := seen[archiveDir]; ok {
			continue
		}
		seen[archiveDir] = struct{}{}
		cleanupArchiveLogs(archiveDir, retentionDays, now)
	}
}

func cleanupArchiveLogs(archiveDir string, retentionDays int, now time.Time) {
	if archiveDir == "" || retentionDays <= 0 {
		return
	}

	entries, err := os.ReadDir(archiveDir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		log.Printf("ablestack log archive read failed: path=%s err=%v", archiveDir, err)
		return
	}

	cutoff := now.In(time.Local).AddDate(0, 0, -retentionDays)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		logDate, ok := archiveLogDate(entry.Name())
		if !ok || !logDate.Before(dateOnly(cutoff)) {
			continue
		}
		path := filepath.Join(archiveDir, entry.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("ablestack log archive cleanup failed: path=%s err=%v", path, err)
		}
	}
}

func archiveLogDate(name string) (time.Time, bool) {
	for _, prefix := range []string{"api.log-", "detail.log-", "job.log-"} {
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".gz") {
			continue
		}
		datePart := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".gz")
		logDate, err := time.ParseInLocation(archiveDateLayout, datePart, time.Local)
		if err != nil {
			return time.Time{}, false
		}
		return logDate, true
	}
	return time.Time{}, false
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.In(time.Local).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.Local)
}

func sanitizeLogString(value string, limit int) string {
	value = strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(value))
	return limitString(value, limit)
}

func limitString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "...(truncated)"
}

func actionMessageLooksLikeError(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "error=") ||
		strings.Contains(lower, "failed") ||
		strings.Contains(lower, "fail") ||
		strings.Contains(lower, "bad_request") ||
		strings.Contains(lower, "invalid_request")
}

func defaultString(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
