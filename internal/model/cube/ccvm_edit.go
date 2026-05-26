package cube

// CCVMEditRequest는 CCVM XML의 CPU/메모리 값을 수정할 때 사용하는 요청 본문이다.
// @name CCVMEditRequest
type CCVMEditRequest struct {
	// vCPU 개수
	CPU string `json:"cpu" example:"8"`
	// 메모리 크기(GiB)
	Memory string `json:"memory" example:"16"`
}

// CCVMEditResult는 대상 호스트별 CCVM XML 수정 결과를 담는다.
// @name CCVMEditResult
type CCVMEditResult struct {
	// 수정 대상 호스트 IP 또는 식별자
	Target string `json:"target" example:"10.10.31.1"`
	// 처리 결과 코드
	Code int `json:"code" example:"200"`
	// 처리 결과 메시지
	Message string `json:"message" example:"ok"`
	// 수정된 XML 파일 경로
	XMLPath string `json:"xml_path,omitempty" example:"/etc/ablestack/vmconfig/ccvm/ccvm.xml"`
}

// CCVMEditResponse는 CCVM XML 수정 API의 최종 응답이다.
// @name CCVMEditResponse
type CCVMEditResponse struct {
	// 전체 처리 결과 코드
	Code int `json:"code" example:"200"`
	// 전체 처리 결과 메시지
	Message string `json:"message" example:"ccvm xml updated"`
	// 대상 호스트별 상세 결과 목록
	Results []CCVMEditResult `json:"results,omitempty"`
}
