package cube

// SCVMUpdateRequest는 Storage Center VM lifecycle 제어 요청 본문이다.
// @name SCVMUpdateRequest
type SCVMUpdateRequest struct {
	// action: start/stop/delete/resource/setup/reset
	Action string `json:"action" example:"start"`
	// target ablecube host IP. 비어 있으면 요청을 받은 노드에서 실행한다.
	Target string `json:"target,omitempty" example:"10.10.31.1"`
	// target hostname. target이 비어 있을 때 cluster.json hosts[].hostname으로 ablecube를 찾는다.
	TargetHostname string `json:"target_hostname,omitempty" example:"ablecube31-1"`
	// vCPU cores for resource action
	CPU int `json:"cpu,omitempty" example:"4"`
	// memory GiB for resource action
	Memory int `json:"memory,omitempty" example:"16"`
}

// SCVMUpdateResponse는 Storage Center VM lifecycle 제어 결과이다.
// @name SCVMUpdateResponse
type SCVMUpdateResponse struct {
	// 처리 결과 코드
	Code int `json:"code" example:"200"`
	// 기존 Python createReturn 호환 결과값
	Val any `json:"val"`
	// 기존 Python createReturn 호환 결과명
	RetName string `json:"retname,omitempty" example:"Storage Center VM Start"`
	// 처리 결과 메시지
	Message string `json:"message,omitempty" example:"Storage Center VM Start Success"`
	// 실제 작업 대상
	Target string `json:"target,omitempty" example:"10.10.31.1"`
	// 수행한 action
	Action string `json:"action,omitempty" example:"start"`
}
