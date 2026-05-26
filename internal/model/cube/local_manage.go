package cube

// LocalManageRequest는 standalone 로컬 디스크 관리 요청 본문이다.
// @name LocalManageRequest
type LocalManageRequest struct {
	// action: create-local-disk/local-disk-status/reset
	Action string `json:"action" example:"local-disk-status"`
	// comma separated disk list for legacy clients
	Disk string `json:"disk,omitempty" example:"/dev/sdb"`
	// disk list
	Disks []string `json:"disks,omitempty" example:"/dev/sdb"`
}

// LocalManageStatusValue는 로컬 디스크 상태 값이다.
// @name LocalManageStatusValue
type LocalManageStatusValue struct {
	Status    string `json:"status" example:"Health OK"`
	MountPath string `json:"mount_path" example:"/mnt/glue"`
	PV        string `json:"pv" example:"/dev/sdb1"`
	VG        string `json:"vg" example:"/dev/mapper/vg_glue-lv_glue"`
	Size      string `json:"size" example:"100.00G"`
}

// LocalManageResponse는 standalone 로컬 디스크 관리 결과이다.
// @name LocalManageResponse
type LocalManageResponse struct {
	Code    int    `json:"code" example:"200"`
	Val     any    `json:"val,omitempty"`
	Message string `json:"message,omitempty" example:"ok"`
	Action  string `json:"action,omitempty" example:"local-disk-status"`
}
