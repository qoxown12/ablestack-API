package cube

// RBDManageRequest는 GFS용 RBD 이미지 생성/삭제 요청이다.
// @name RBDManageRequest
type RBDManageRequest struct {
	// action: create/delete
	Action string `json:"action" example:"create"`
	// create action에서 생성할 전체 용량 GiB
	Size int `json:"size,omitempty" example:"5000"`
	// delete action에서 삭제할 이미지명. 콤마 구분 또는 /dev/rbd/rbd/gfs07 경로 허용
	ImageName string `json:"image_name,omitempty" example:"gfs07,gfs08"`
	// image name list. 원격 rbdmap 반영 내부 호출에도 사용한다.
	Images []string `json:"images,omitempty" example:"rbd/gfs07,rbd/gfs08"`
	// RBD pool name. 비어 있으면 rbd
	PoolName string `json:"pool_name,omitempty" example:"rbd"`
	// create action 이미지 prefix. 비어 있으면 gfs
	ImagePrefix string `json:"image_prefix,omitempty" example:"gfs"`
}

// RBDManageTargetResult는 호스트별 rbdmap 반영 결과이다.
// @name RBDManageTargetResult
type RBDManageTargetResult struct {
	Hostname string   `json:"hostname,omitempty" example:"ablecube31-1"`
	Target   string   `json:"target" example:"10.10.31.1"`
	Code     int      `json:"code" example:"200"`
	Message  string   `json:"message" example:"ok"`
	Images   []string `json:"images,omitempty" example:"rbd/gfs07,rbd/gfs08"`
}

// RBDManageResponse는 GFS용 RBD 이미지 생성/삭제 응답이다.
// @name RBDManageResponse
type RBDManageResponse struct {
	Code    int                     `json:"code" example:"200"`
	Val     any                     `json:"val,omitempty"`
	RetName string                  `json:"retname,omitempty" example:"RBD Manage"`
	Message string                  `json:"message,omitempty" example:"rbd manage success"`
	Action  string                  `json:"action,omitempty" example:"create"`
	Pool    string                  `json:"pool,omitempty" example:"rbd"`
	Results []RBDManageTargetResult `json:"results,omitempty"`
}
