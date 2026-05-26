package cube

// HBAManageRequest는 HBA 정보 조회 요청 본문이다.
// @name HBAManageRequest
type HBAManageRequest struct {
	// action: list-hba-wwn
	Action string `json:"action" example:"list-hba-wwn"`
}

// HBAWWNResult는 호스트별 HBA WWN 조회 결과이다.
// @name HBAWWNResult
type HBAWWNResult struct {
	Hostname string   `json:"hostname" example:"ablecube31-1"`
	Target   string   `json:"target,omitempty" example:"10.10.31.1"`
	WWN      []string `json:"wwn"`
	Error    string   `json:"error,omitempty"`
}

// HBAManageResponse는 HBA 정보 조회 결과이다.
// @name HBAManageResponse
type HBAManageResponse struct {
	Code    int            `json:"code" example:"200"`
	Val     []HBAWWNResult `json:"val,omitempty"`
	Message string         `json:"message,omitempty" example:"ok"`
	Action  string         `json:"action,omitempty" example:"list-hba-wwn"`
}
