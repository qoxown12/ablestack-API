package cube

// CCVMSnapRequest는 CCVM RBD snapshot 작업 요청 본문이다.
// @name CCVMSnapRequest
type CCVMSnapRequest struct {
	// action: list/backup/rollback
	Action string `json:"action" example:"backup"`
	// snapshot name for rollback, or optional custom name for backup
	SnapName string `json:"snap_name,omitempty" example:"2026-04-29-10:00:00"`
}

// CCVMSnapResponse는 CCVM RBD snapshot 작업 응답이다.
// @name CCVMSnapResponse
type CCVMSnapResponse struct {
	// 처리 결과 코드
	Code int `json:"code" example:"200"`
	// 기존 Python createReturn 호환 결과값
	Val any `json:"val,omitempty"`
	// 처리 결과 메시지
	Message string `json:"message,omitempty" example:"CCVM Snapshot Backup Create Success"`
	// 실제 작업을 수행한 대상 ablecube IP 또는 식별자
	Target string `json:"target,omitempty" example:"10.10.31.1"`
	// 수행한 action
	Action string `json:"action,omitempty" example:"backup"`
	// 생성 또는 복구 대상 snapshot 이름
	SnapName string `json:"snap_name,omitempty" example:"2026-04-29-10:00:00"`
	// backup 후 보관 정책으로 삭제한 snapshot 이름 목록
	DeletedSnapshots []string `json:"deleted_snapshots,omitempty"`
}
