package cube

// GlueClusterUpdateRequest는 스토리지 클러스터 유지보수 모드 변경 요청 본문이다.
// @name GlueClusterUpdateRequest
type GlueClusterUpdateRequest struct {
	// action: set_noout/unset_noout
	Action string `json:"action" example:"set_noout"`
}

// GlueClusterUpdateResponse는 스토리지 클러스터 유지보수 모드 변경 결과이다.
// @name GlueClusterUpdateResponse
type GlueClusterUpdateResponse struct {
	Code    int    `json:"code" example:"200"`
	Val     string `json:"val" example:"success"`
	RetName string `json:"retname,omitempty" example:"Maintenance Mode On"`
	Message string `json:"message,omitempty" example:"success"`
	Action  string `json:"action,omitempty" example:"set_noout"`
}
