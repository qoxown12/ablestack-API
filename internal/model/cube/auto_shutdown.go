package cube

// AutoShutdownRequest는 전체 호스트 정상 종료 절차 요청 본문이다.
// @name AutoShutdownRequest
type AutoShutdownRequest struct {
	// action: check_mount/stop_scvms/shutdown_hosts
	Action string `json:"action" example:"check_mount"`
}

// AutoShutdownMountResult는 로컬 fstab UUID 마운트 처리 결과이다.
// @name AutoShutdownMountResult
type AutoShutdownMountResult struct {
	Source     string `json:"source" example:"UUID=xxxx"`
	MountPoint string `json:"mount_point" example:"/mnt/data"`
	Status     string `json:"status" example:"unmounted"`
	Message    string `json:"message,omitempty" example:"ok"`
}

// AutoShutdownTargetResult는 종료 절차 대상별 실행 결과이다.
// @name AutoShutdownTargetResult
type AutoShutdownTargetResult struct {
	Hostname string                    `json:"hostname,omitempty" example:"ablecube31-1"`
	Target   string                    `json:"target" example:"10.10.31.1"`
	Code     int                       `json:"code" example:"200"`
	Val      any                       `json:"val,omitempty"`
	RetName  string                    `json:"retname,omitempty" example:"Hosts Shutdown"`
	Message  string                    `json:"message,omitempty" example:"ok"`
	Action   string                    `json:"action,omitempty" example:"shutdown_hosts"`
	Mounts   []AutoShutdownMountResult `json:"mounts,omitempty"`
}

// AutoShutdownResponse는 전체 호스트 정상 종료 절차 결과이다.
// @name AutoShutdownResponse
type AutoShutdownResponse struct {
	Code    int                        `json:"code" example:"200"`
	Val     any                        `json:"val"`
	RetName string                     `json:"retname,omitempty" example:"Hosts Shutdown"`
	Message string                     `json:"message,omitempty" example:"ok"`
	Action  string                     `json:"action,omitempty" example:"shutdown_hosts"`
	Target  string                     `json:"target,omitempty" example:"10.10.31.1"`
	Results []AutoShutdownTargetResult `json:"results,omitempty"`
	Mounts  []AutoShutdownMountResult  `json:"mounts,omitempty"`
}
