package cube

// CCVMBackupRequest는 CCVM 파일 백업/상태/목록/스케줄 관리 요청 본문이다.
// @name CCVMBackupRequest
type CCVMBackupRequest struct {
	// action: backup/status/list/overview/schedule/unschedule/schedule-delete/unschedule-delete
	Action string `json:"action" example:"backup"`
	// repeat option: hourly/daily/monthly/yearly
	Repeat string `json:"repeat,omitempty" example:"daily"`
	// time in HH:MM
	Time string `json:"time,omitempty" example:"01:00"`
	// day of month
	Day int `json:"day,omitempty" example:"1"`
	// month
	Month int `json:"month,omitempty" example:"1"`
	// retention months for delete schedule
	RetainMonths int `json:"retain_months,omitempty" example:"3"`
	// backup target directory
	TargetDir string `json:"target_dir,omitempty" example:"/mnt/glue-gfs/backup/ccvm"`
}

// CCVMRestoreRequest는 CCVM 파일 백업 복구 요청 본문이다.
// @name CCVMRestoreRequest
type CCVMRestoreRequest struct {
	// backup file name for restore
	TargetFile string `json:"target_file" example:"ccvm.qcow2-20260430_010000"`
}

// CCVMBackupResponse는 CCVM 파일 백업/복구/스케줄 관리 결과이다.
// @name CCVMBackupResponse
type CCVMBackupResponse struct {
	// 처리 결과 코드
	Code int `json:"code" example:"200"`
	// 기존 Python createReturn 호환 결과값
	Val any `json:"val,omitempty"`
	// 기존 Python createReturn 호환 결과명
	RetName string `json:"retname,omitempty"`
	// 처리 결과 메시지
	Message string `json:"message,omitempty"`
	// 실제 처리 대상
	Target string `json:"target,omitempty" example:"10.10.31.1"`
	// 수행한 action
	Action string `json:"action,omitempty" example:"backup"`
}
