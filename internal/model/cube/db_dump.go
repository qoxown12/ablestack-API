package cube

// DBDumpRequest는 Cloud Center VM의 cloud DB dump/스케줄 제어 요청 본문이다.
// @name DBDumpRequest
type DBDumpRequest struct {
	// action: instantBackup/regularBackup/deleteOldBackup/checkBackup/deactiveBackup
	Action string `json:"action" example:"instantBackup"`
	// backup directory path on CCVM
	Path string `json:"path,omitempty" example:"/home/db_backup"`
	// repeat: no/hourly/daily/weekly/monthly
	Repeat string `json:"repeat,omitempty" example:"daily"`
	// timeone: at schedule text for repeat=no, HH:mm for repeat jobs
	TimeOne string `json:"timeone,omitempty" example:"02:00"`
	// timetwo: weekday(0-6) for weekly, month interval-day for monthly such as 1-15
	TimeTwo string `json:"timetwo,omitempty" example:"1-15"`
	// delete old backup mtime days
	Delete string `json:"delete,omitempty" example:"30"`
	// checkOption: r for backup schedule, d for delete schedule
	CheckOption string `json:"checkOption,omitempty" example:"r"`
}

// DBDumpResponse는 Cloud Center VM DB dump/스케줄 제어 결과이다.
// @name DBDumpResponse
type DBDumpResponse struct {
	Code    int    `json:"code" example:"200"`
	Val     any    `json:"val,omitempty"`
	Message string `json:"message,omitempty"`
	Action  string `json:"action,omitempty" example:"instantBackup"`
	Target  string `json:"target,omitempty" example:"10.10.31.10"`
}
