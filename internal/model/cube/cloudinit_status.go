package cube

// CloudInitStatusRequest는 SCVM/CCVM cloud-init 상태 확인 요청이다.
// @name CloudInitStatusRequest
type CloudInitStatusRequest struct {
	// action: status/ping
	Action string `json:"action" example:"status"`
	// target role or direct target IP: ccvm/scvm/10.10.31.10
	Target string `json:"target" example:"ccvm"`
	// comma-separated hostnames for scvm target
	TargetHostname string `json:"target_hostname,omitempty" example:"ablecube31-1"`
}

// CloudInitStatusResult는 대상별 cloud-init 상태 확인 결과이다.
// @name CloudInitStatusResult
type CloudInitStatusResult struct {
	Role     string `json:"role,omitempty" example:"ccvm"`
	Hostname string `json:"hostname,omitempty" example:"ablecube31-1"`
	Target   string `json:"target" example:"10.10.31.10"`
	Code     int    `json:"code" example:"200"`
	Message  string `json:"message" example:"ok"`
	Val      any    `json:"val,omitempty"`
}

// CloudInitStatusResponse는 SCVM/CCVM cloud-init 상태 확인 응답이다.
// @name CloudInitStatusResponse
type CloudInitStatusResponse struct {
	Code    int                     `json:"code" example:"200"`
	Val     any                     `json:"val,omitempty"`
	RetName string                  `json:"retname,omitempty" example:"CloudInit Status"`
	Message string                  `json:"message,omitempty" example:"cloud-init status check success"`
	Action  string                  `json:"action,omitempty" example:"status"`
	Results []CloudInitStatusResult `json:"results,omitempty"`
}
