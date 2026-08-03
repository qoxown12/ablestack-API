package cube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type DBDumpRequest = CubeModel.DBDumpRequest
type DBDumpResponse = CubeModel.DBDumpResponse

const (
	dbDumpLocalHeader      = "X-Cube-DB-Dump-Local"
	dbDumpCommandTimeout   = 2 * time.Minute
	dbDumpRequestTimeout   = 5 * time.Minute
	dbDumpUser             = "root"
	dbDumpPassword         = "Ablecloud1!"
	dbDumpDatabase         = "cloud"
	dbDumpDefaultPath      = "/home/db_backup"
	dbDumpBackupQueue      = "r"
	dbDumpDeleteQueue      = "d"
	dbDumpBackupMarker     = "RegularBackup"
	dbDumpDeleteMarker     = "DeleteOldBackup"
	dbDumpSuccessMessage   = "ok"
	dbDumpDateTimeLayout   = "2006-01-02 15:04"
	dbDumpFileTimeLayout   = "2006-01-02-15:04:05"
	dbDumpCronDateLayout   = "2006-01-02"
	dbDumpCronPercentToken = `\%Y\%m\%d_\%H\%M\%S`
)

// DBDump godoc
//
//	@Summary		CCVM DB Dump
//	@Description	CCVM의 cloud DB dump 생성, 백업/삭제 스케줄 설정, 조회, 비활성화를 수행합니다.
//	@Tags			Cube-DB
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.DBDumpRequest	true	"db dump request"
//	@Success		200	{object}	CubeModel.DBDumpResponse
//	@Failure		400	{object}	CubeModel.DBDumpResponse
//	@Failure		500	{object}	CubeModel.DBDumpResponse
//	@Router			/cube/db/dump [post]
func DBDump(context *gin.Context) {
	var req DBDumpRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, dbDumpError(req, "", http.StatusBadRequest, "invalid request"))
		return
	}

	if err := normalizeDBDumpRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, dbDumpError(req, "", http.StatusBadRequest, err.Error()))
		return
	}

	if isDBDumpLocalRequest(context) {
		resp := runDBDumpLocal(req, "local")
		context.JSON(statusCodeFromDBDumpResponse(resp), resp)
		return
	}

	target, err := resolveDBDumpCCVMTarget()
	if err != nil {
		context.JSON(http.StatusInternalServerError, dbDumpError(req, "", http.StatusInternalServerError, err.Error()))
		return
	}

	if isDBDumpLocalTarget(target) {
		resp := runDBDumpLocal(req, target)
		context.JSON(statusCodeFromDBDumpResponse(resp), resp)
		return
	}

	resp, err := callDBDumpRemote(target, req)
	if err != nil {
		resp = dbDumpError(req, target, http.StatusInternalServerError, err.Error())
	}
	context.JSON(statusCodeFromDBDumpResponse(resp), resp)
}

func normalizeDBDumpRequest(req *DBDumpRequest) error {
	req.Action = strings.TrimSpace(req.Action)
	switch strings.ToLower(req.Action) {
	case "instantbackup":
		req.Action = "instantBackup"
	case "regularbackup":
		req.Action = "regularBackup"
	case "deleteoldbackup":
		req.Action = "deleteOldBackup"
	case "checkbackup":
		req.Action = "checkBackup"
	case "deactivebackup":
		req.Action = "deactiveBackup"
	default:
		return fmt.Errorf("unsupported action")
	}

	req.Path = strings.TrimSpace(req.Path)
	req.Repeat = strings.ToLower(strings.TrimSpace(req.Repeat))
	req.TimeOne = strings.TrimSpace(req.TimeOne)
	req.TimeTwo = strings.TrimSpace(req.TimeTwo)
	req.Delete = strings.TrimSpace(req.Delete)
	req.CheckOption = strings.ToLower(strings.TrimSpace(req.CheckOption))

	switch req.Action {
	case "instantBackup":
		defaultDBDumpPath(req)
		return validateDBDumpPath(req.Path)
	case "regularBackup":
		defaultDBDumpPath(req)
		if err := validateDBDumpPath(req.Path); err != nil {
			return err
		}
		return validateDBDumpSchedule(req.Repeat, req.TimeOne, req.TimeTwo, false)
	case "deleteOldBackup":
		defaultDBDumpPath(req)
		if err := validateDBDumpPath(req.Path); err != nil {
			return err
		}
		if err := validateDBDumpSchedule(req.Repeat, req.TimeOne, req.TimeTwo, true); err != nil {
			return err
		}
		if _, err := strconv.Atoi(req.Delete); err != nil || strings.HasPrefix(req.Delete, "-") {
			return fmt.Errorf("delete must be a non-negative day number")
		}
	case "checkBackup", "deactiveBackup":
		if req.CheckOption != dbDumpBackupQueue && req.CheckOption != dbDumpDeleteQueue {
			return fmt.Errorf("checkOption must be r or d")
		}
	}
	return nil
}

func defaultDBDumpPath(req *DBDumpRequest) {
	if req.Path == "" {
		req.Path = dbDumpDefaultPath
	}
}

func validateDBDumpPath(path string) error {
	if path == "" {
		return fmt.Errorf("path required")
	}
	if strings.ContainsAny(path, "\x00\r\n") {
		return fmt.Errorf("invalid path")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be an absolute path")
	}
	return nil
}

func validateDBDumpSchedule(repeat string, timeOne string, timeTwo string, needsDelete bool) error {
	switch repeat {
	case "no":
		if timeOne == "" {
			return fmt.Errorf("timeone required")
		}
	case "hourly", "daily":
		if _, _, err := parseDBDumpClock(timeOne); err != nil {
			return err
		}
	case "weekly":
		if _, _, err := parseDBDumpClock(timeOne); err != nil {
			return err
		}
		day, err := strconv.Atoi(timeTwo)
		if err != nil || day < 0 || day > 6 {
			return fmt.Errorf("timetwo must be weekday 0-6")
		}
	case "monthly":
		if _, _, err := parseDBDumpClock(timeOne); err != nil {
			return err
		}
		months, day, err := parseDBDumpMonthlySpec(timeTwo)
		if err != nil || months <= 0 || months > 12 || day <= 0 || day > 31 {
			return fmt.Errorf("timetwo must be month interval-day, for example 1-15")
		}
	default:
		return fmt.Errorf("repeat must be no/hourly/daily/weekly/monthly")
	}
	if needsDelete {
		return nil
	}
	return nil
}

func isDBDumpLocalRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(dbDumpLocalHeader)), "1")
}

func statusCodeFromDBDumpResponse(resp DBDumpResponse) int {
	if resp.Code == http.StatusOK {
		return http.StatusOK
	}
	if resp.Code == http.StatusBadRequest {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func runDBDumpLocal(req DBDumpRequest, target string) DBDumpResponse {
	var (
		val any
		err error
	)

	switch req.Action {
	case "instantBackup":
		val, err = instantDBDump(req.Path)
	case "regularBackup":
		err = scheduleRegularDBDump(req.Path, req.Repeat, req.TimeOne, req.TimeTwo)
		val = "Creation of mysqldump of ccvm is completed"
	case "deleteOldBackup":
		err = scheduleDeleteOldDBDump(req.Path, req.Repeat, req.TimeOne, req.TimeTwo, req.Delete)
		val = "Creation of mysqldump of ccvm is completed"
	case "checkBackup":
		val, err = checkDBDumpSchedule(req.CheckOption)
	case "deactiveBackup":
		val, err = deactivateDBDumpSchedule(req.CheckOption)
	default:
		err = fmt.Errorf("unsupported action")
	}
	if err != nil {
		return dbDumpError(req, target, http.StatusInternalServerError, err.Error())
	}
	return DBDumpResponse{Code: http.StatusOK, Val: val, Message: dbDumpSuccessMessage, Action: req.Action, Target: target}
}

func instantDBDump(path string) (string, error) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	filePath := filepath.Join(path, "ccvm_dump_"+time.Now().Format(dbDumpFileTimeLayout)+".sql")
	file, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	ctx, cancel := context.WithTimeout(context.Background(), dbDumpCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/usr/bin/mysqldump", "-u"+dbDumpUser, "-p"+dbDumpPassword, "--databases", dbDumpDatabase)
	cmd.Stdout = file
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("mysqldump timed out after %s", dbDumpCommandTimeout)
		}
		return "", fmt.Errorf("mysqldump failed: %s", firstNonEmptyLocal(stderr.String(), err.Error()))
	}
	return filePath, nil
}

func scheduleRegularDBDump(path string, repeat string, timeOne string, timeTwo string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	if err := cleanupDBDumpSchedule(dbDumpBackupQueue); err != nil {
		return err
	}

	dumpCommand := buildDBDumpCommand(path)
	if repeat == "no" {
		return runDBDumpShell(fmt.Sprintf("printf '%%s\\n' %s | at %s -q %s", shellQuote(dumpCommand), shellQuote(timeOne), dbDumpBackupQueue))
	}

	cronExpr, comment, err := buildDBDumpCron(repeat, timeOne, timeTwo, "", dumpCommand, dbDumpBackupMarker)
	if err != nil {
		return err
	}
	return appendDBDumpCrontab(comment, cronExpr)
}

func scheduleDeleteOldDBDump(path string, repeat string, timeOne string, timeTwo string, deleteDays string) error {
	if err := cleanupDBDumpSchedule(dbDumpDeleteQueue); err != nil {
		return err
	}

	deleteCommand := buildDBDumpDeleteCommand(path, deleteDays)
	if repeat == "no" {
		return runDBDumpShell(fmt.Sprintf("printf '%%s\\n' %s | at %s -q %s", shellQuote(deleteCommand), shellQuote(timeOne), dbDumpDeleteQueue))
	}

	cronExpr, comment, err := buildDBDumpCron(repeat, timeOne, timeTwo, deleteDays, deleteCommand, dbDumpDeleteMarker)
	if err != nil {
		return err
	}
	return appendDBDumpCrontab(comment, cronExpr)
}

func buildDBDumpCommand(path string) string {
	target := strings.TrimRight(shellQuote(path), "/") + "/ccvm_dump_$(date +" + dbDumpCronPercentToken + ").sql"
	return fmt.Sprintf("/usr/bin/mysqldump -u%s -p%s --databases %s > %s", dbDumpUser, dbDumpPassword, dbDumpDatabase, target)
}

func buildDBDumpDeleteCommand(path string, deleteDays string) string {
	return fmt.Sprintf("find %s -name %s -mtime %s -delete", shellQuote(path), shellQuote("ccvm_dump_*.sql"), deleteDays)
}

func buildDBDumpCron(repeat string, timeOne string, timeTwo string, deleteDays string, command string, marker string) (string, string, error) {
	hour, minute, err := parseDBDumpClock(timeOne)
	if err != nil {
		return "", "", err
	}

	now := time.Now()
	var next time.Time
	var cron string
	commentParts := []string{"#" + marker, repeat}

	switch repeat {
	case "hourly":
		next = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), minute, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(time.Hour)
		}
		cron = fmt.Sprintf("%d */1 * * * %s", minute, command)
		commentParts = append(commentParts, next.Format(dbDumpDateTimeLayout))
	case "daily":
		next = time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		cron = fmt.Sprintf("%d %d * * * %s", minute, hour, command)
		commentParts = append(commentParts, next.Format(dbDumpDateTimeLayout))
	case "weekly":
		weekday, err := strconv.Atoi(timeTwo)
		if err != nil {
			return "", "", err
		}
		next = nextDBDumpWeeklyTime(now, weekday, hour, minute)
		cron = fmt.Sprintf("%d %d * * %d %s", minute, hour, weekday, command)
		commentParts = append(commentParts, next.Format(dbDumpCronDateLayout), timeOne, timeTwo)
	case "monthly":
		months, day, err := parseDBDumpMonthlySpec(timeTwo)
		if err != nil {
			return "", "", err
		}
		next = nextDBDumpMonthlyTime(now, months, day, hour, minute)
		cron = fmt.Sprintf("%d %d %d */%d * %s", minute, hour, day, months, command)
		commentParts = append(commentParts, next.Format(dbDumpCronDateLayout), timeOne, timeTwo)
	default:
		return "", "", fmt.Errorf("unsupported repeat")
	}
	if deleteDays != "" {
		commentParts = append(commentParts, deleteDays)
	}
	return cron, strings.Join(commentParts, " "), nil
}

func appendDBDumpCrontab(comment string, cronExpr string) error {
	script := fmt.Sprintf(`set -e
tmp=$(mktemp)
crontab -u root -l 2>/dev/null > "$tmp" || true
printf '%%s\n' %s >> "$tmp"
printf '%%s\n' %s >> "$tmp"
crontab -u root "$tmp"
rm -f "$tmp"
`, shellQuote(comment), shellQuote(cronExpr))
	return runDBDumpShell(script)
}

func cleanupDBDumpSchedule(option string) error {
	var marker string
	var pattern string
	switch option {
	case dbDumpBackupQueue:
		marker = dbDumpBackupMarker
		pattern = "mysqldump.*ccvm_dump_|ccvm_dump_.*mysqldump"
	case dbDumpDeleteQueue:
		marker = dbDumpDeleteMarker
		pattern = "delete.*ccvm_dump_|ccvm_dump_.*delete"
	default:
		return fmt.Errorf("unsupported checkOption")
	}

	script := fmt.Sprintf(`set -e
atq 2>/dev/null | awk '$7==%s {print $1}' | xargs -r atrm 2>/dev/null || true
grep -lrEZ %s /var/spool/at 2>/dev/null | xargs -0 -r rm -f 2>/dev/null || true
tmp=$(mktemp)
crontab -u root -l 2>/dev/null | grep -Ev %s > "$tmp" || true
crontab -u root "$tmp"
rm -f "$tmp"
`, shellQuote(option), shellQuote(pattern), shellQuote(marker+"|"+pattern))
	return runDBDumpShell(script)
}

func deactivateDBDumpSchedule(option string) (string, error) {
	if err := cleanupDBDumpSchedule(option); err != nil {
		return "", err
	}
	return "deactive", nil
}

func checkDBDumpSchedule(option string) (string, error) {
	atJobs, err := listDBDumpAtJobs(option)
	if err != nil {
		return "", err
	}
	if len(atJobs) > 0 {
		resultDate := atJobs[0].When.Format(dbDumpDateTimeLayout)
		if option == dbDumpBackupQueue {
			return resultDate + " (반복 없음)", nil
		}
		deleteDays, _ := readDBDumpDeleteDaysFromAtJob(atJobs[0].ID)
		if deleteDays == "" {
			deleteDays = "N/A"
		}
		return fmt.Sprintf("%s (반복 없음, %s일 지난 파일 삭제)", resultDate, strings.TrimPrefix(deleteDays, "+")), nil
	}

	crontab, err := runDBDumpCommandOutput(dbDumpCommandTimeout, "crontab", "-u", "root", "-l")
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "no crontab") {
		return "", err
	}

	marker := dbDumpBackupMarker
	if option == dbDumpDeleteQueue {
		marker = dbDumpDeleteMarker
	}
	for _, line := range strings.Split(crontab, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#"+marker) {
			return formatDBDumpScheduleComment(line, option), nil
		}
	}
	return "None", nil
}

type dbDumpAtJob struct {
	ID   string
	When time.Time
}

func listDBDumpAtJobs(queue string) ([]dbDumpAtJob, error) {
	out, err := runDBDumpCommandOutput(dbDumpCommandTimeout, "atq")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such file") {
			return nil, nil
		}
		return nil, nil
	}

	jobs := make([]dbDumpAtJob, 0)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 7 || fields[6] != queue {
			continue
		}
		when, err := parseDBDumpAtTime(strings.Join(fields[1:6], " "))
		if err != nil {
			continue
		}
		jobs = append(jobs, dbDumpAtJob{ID: fields[0], When: when})
	}
	return jobs, nil
}

func parseDBDumpAtTime(raw string) (time.Time, error) {
	layouts := []string{
		"Mon Jan _2 15:04:05 2006",
		"Mon Jan 2 15:04:05 2006",
		"Mon Jan _2 15:04 2006",
		"Mon Jan 2 15:04 2006",
	}
	var last error
	for _, layout := range layouts {
		parsed, err := time.ParseInLocation(layout, raw, time.Local)
		if err == nil {
			return parsed, nil
		}
		last = err
	}
	return time.Time{}, last
}

func readDBDumpDeleteDaysFromAtJob(jobID string) (string, error) {
	out, err := runDBDumpCommandOutput(dbDumpCommandTimeout, "at", "-c", jobID)
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`-mtime\s+([+ -]?\d+)`)
	match := re.FindStringSubmatch(out)
	if len(match) < 2 {
		return "", nil
	}
	return strings.TrimSpace(match[1]), nil
}

func formatDBDumpScheduleComment(line string, option string) string {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return "None"
	}
	repeat := fields[1]
	dateTime := fields[2]
	if len(fields) > 3 && strings.Contains(fields[3], ":") {
		dateTime += " " + fields[3]
	}

	repeatText := ""
	extra := ""
	switch repeat {
	case "hourly":
		repeatText = "한 시간 마다"
	case "daily":
		repeatText = "매일"
	case "weekly":
		repeatText = "매주"
		if len(fields) > 4 {
			extra = " " + koreanWeekday(fields[4])
		}
	case "monthly":
		repeatText = "매월"
		if len(fields) > 4 {
			if months, day, err := parseDBDumpMonthlySpec(fields[4]); err == nil {
				extra = fmt.Sprintf("%d개월 %d일 마다 ", months, day)
			}
		}
	default:
		repeatText = repeat
	}

	repeatLabel := repeatText
	if strings.TrimSpace(extra) != "" {
		repeatLabel += " " + strings.TrimSpace(extra)
	}

	if option == dbDumpDeleteQueue {
		deleteDays := ""
		if len(fields) > 4 {
			deleteDays = fields[len(fields)-1]
		}
		if deleteDays == "" {
			deleteDays = "N/A"
		}
		return fmt.Sprintf("%s (%s, %s일 지난 파일 삭제)", dateTime, repeatLabel, strings.TrimPrefix(deleteDays, "+"))
	}
	return fmt.Sprintf("%s (%s)", dateTime, repeatLabel)
}

func koreanWeekday(value string) string {
	switch strings.TrimSpace(value) {
	case "0":
		return "일요일"
	case "1":
		return "월요일"
	case "2":
		return "화요일"
	case "3":
		return "수요일"
	case "4":
		return "목요일"
	case "5":
		return "금요일"
	case "6":
		return "토요일"
	default:
		return value
	}
}

func parseDBDumpClock(raw string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("timeone must be HH:mm")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("timeone hour must be 0-23")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("timeone minute must be 0-59")
	}
	return hour, minute, nil
}

func parseDBDumpMonthlySpec(raw string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(raw), "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid monthly spec")
	}
	months, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	day, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return months, day, nil
}

func nextDBDumpWeeklyTime(now time.Time, cronWeekday int, hour int, minute int) time.Time {
	goWeekday := time.Weekday(cronWeekday)
	days := (int(goWeekday) - int(now.Weekday()) + 7) % 7
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location()).AddDate(0, 0, days)
	if !next.After(now) {
		next = next.AddDate(0, 0, 7)
	}
	return next
}

func nextDBDumpMonthlyTime(now time.Time, months int, day int, hour int, minute int) time.Time {
	next := safeMonthlyDate(now.Year(), now.Month(), day, hour, minute, now.Location())
	if !next.After(now) {
		next = safeMonthlyDate(now.AddDate(0, months, 0).Year(), now.AddDate(0, months, 0).Month(), day, hour, minute, now.Location())
	}
	return next
}

func safeMonthlyDate(year int, month time.Month, day int, hour int, minute int, loc *time.Location) time.Time {
	lastDay := time.Date(year, month+1, 0, hour, minute, 0, 0, loc).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, month, day, hour, minute, 0, 0, loc)
}

func runDBDumpCommandOutput(timeout time.Duration, command string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = append([]string{"LANG=en_US.utf-8", "LANGUAGE=en"}, os.Environ()...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("%s timed out after %s", command, timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		return string(output), fmt.Errorf("%s failed: %s", command, msg)
	}
	return string(output), nil
}

func runDBDumpShell(script string) error {
	_, err := runDBDumpCommandOutput(dbDumpCommandTimeout, "/bin/bash", "-lc", script)
	return err
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func callDBDumpRemote(target string, req DBDumpRequest) (DBDumpResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return DBDumpResponse{}, err
	}

	url := fmt.Sprintf("%s/api/v1/cube/db/dump", buildTargetURL(target))
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return DBDumpResponse{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(dbDumpLocalHeader, "1")

	client := &http.Client{Timeout: dbDumpRequestTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return DBDumpResponse{}, err
	}
	defer resp.Body.Close()

	var out DBDumpResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return DBDumpResponse{}, err
	}
	if out.Code == 0 {
		out.Code = resp.StatusCode
	}
	if strings.TrimSpace(out.Target) == "" {
		out.Target = target
	}
	return out, nil
}

func resolveDBDumpCCVMTarget() (string, error) {
	cfg, err := loadClusterConfigSection()
	if err != nil {
		return "ccvm", nil
	}
	if cfg == nil || strings.TrimSpace(cfg.CCVM.IP) == "" {
		return "ccvm", nil
	}
	return strings.TrimSpace(cfg.CCVM.IP), nil
}

func isDBDumpLocalTarget(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" || target == "ccvm" {
		name, _ := os.Hostname()
		return strings.EqualFold(strings.TrimSpace(name), "ccvm")
	}
	return isLocalTarget(target)
}

func dbDumpError(req DBDumpRequest, target string, code int, message string) DBDumpResponse {
	return DBDumpResponse{Code: code, Val: message, Message: message, Action: req.Action, Target: target}
}

func firstNonEmptyLocal(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
