package cube

// GlueConfigUpdateRequest는 Glue/Ceph 설정 파일 동기화 요청 본문이다.
// @name GlueConfigUpdateRequest
type GlueConfigUpdateRequest struct {
	// action: update
	Action string `json:"action" example:"update"`
}

// GlueConfigUpdateTargetResult는 대상별 health/copy 처리 결과이다.
// @name GlueConfigUpdateTargetResult
type GlueConfigUpdateTargetResult struct {
	Step     string `json:"step" example:"copy"`
	Role     string `json:"role" example:"ablecube"`
	Hostname string `json:"hostname,omitempty" example:"ablecube31-1"`
	Target   string `json:"target" example:"10.10.31.1"`
	Code     int    `json:"code" example:"200"`
	Message  string `json:"message" example:"ok"`
}

// GlueConfigUpdateResponse는 Glue/Ceph 설정 파일 동기화 결과이다.
// @name GlueConfigUpdateResponse
type GlueConfigUpdateResponse struct {
	Code    int                            `json:"code" example:"200"`
	Val     any                            `json:"val,omitempty"`
	Message string                         `json:"message,omitempty" example:"Glue Config All cube host and scvm update Success"`
	Action  string                         `json:"action,omitempty" example:"update"`
	Source  string                         `json:"source,omitempty" example:"local"`
	Results []GlueConfigUpdateTargetResult `json:"results,omitempty"`
}
