package cube

// VersionUpdateRequest는 ABLESTACK ISO 업데이트 요청 본문이다.
// @name VersionUpdateRequest
type VersionUpdateRequest struct {
	// action: info/run
	Action string `json:"action" example:"info"`
	// mounted ABLESTACK ISO path
	MountPath string `json:"mount_path" example:"/mnt/ablestack-iso"`
	// update type: all/mold
	UpdateType string `json:"update_type,omitempty" example:"all"`
}

// VersionUpdateInfo는 현재/대상 ABLESTACK 버전 정보를 담는다.
// @name VersionUpdateInfo
type VersionUpdateInfo struct {
	MountPath               string `json:"mount_path" example:"/mnt/ablestack-iso"`
	CopyPath                string `json:"copy_path" example:"/opt/ABLESTACK_UPDATE"`
	CurrentOSVersion        string `json:"current_os_version" example:"ABLESTACK Diplo v4.6.1"`
	CurrentMoldVersion      string `json:"current_mold_version" example:"4.20.0"`
	TargetOSVersion         string `json:"target_os_version" example:"ABLESTACK Diplo v4.7.0"`
	TargetMoldVersion       string `json:"target_mold_version" example:"4.21.0.0-Mold.Diplo.202606121525"`
	CurrentAblestackVersion string `json:"current_ablestack_version" example:"ABLESTACK 5.0"`
	TargetAblestackVersion  string `json:"target_ablestack_version" example:"5.1.0"`
	UpdateType              string `json:"update_type" example:"all"`
	UpdateLabel             string `json:"update_label" example:"전체 업데이트"`
	UpdateScript            string `json:"update_script" example:"/mnt/ablestack-iso/update-all.sh"`
	WorkUpdateScript        string `json:"work_update_script" example:"/opt/ABLESTACK_UPDATE/update-all.sh"`
}

// VersionUpdateRunResult는 ABLESTACK 업데이트 실행 결과이다.
// @name VersionUpdateRunResult
type VersionUpdateRunResult struct {
	Message            string `json:"message" example:"ABLESTACK Version 업데이트 실행이 완료되었습니다."`
	MountPath          string `json:"mount_path" example:"/mnt/ablestack-iso"`
	CopyPath           string `json:"copy_path" example:"/opt/ABLESTACK_UPDATE"`
	CurrentOSVersion   string `json:"current_os_version" example:"ABLESTACK Diplo v4.6.1"`
	CurrentMoldVersion string `json:"current_mold_version" example:"4.20.0"`
	TargetOSVersion    string `json:"target_os_version" example:"ABLESTACK Diplo v4.7.0"`
	TargetMoldVersion  string `json:"target_mold_version" example:"4.21.0.0-Mold.Diplo.202606121525"`
	UpdateType         string `json:"update_type" example:"all"`
	UpdateLabel        string `json:"update_label" example:"전체 업데이트"`
	UpdateScript       string `json:"update_script" example:"/opt/ABLESTACK_UPDATE/update-all.sh"`
	Stdout             string `json:"stdout,omitempty"`
	Stderr             string `json:"stderr,omitempty"`
}

// VersionUpdateResponse는 ABLESTACK 업데이트 API 응답이다.
// @name VersionUpdateResponse
type VersionUpdateResponse struct {
	Code    int    `json:"code" example:"200"`
	Val     any    `json:"val,omitempty"`
	RetName string `json:"retname,omitempty"`
	Message string `json:"message,omitempty"`
	Action  string `json:"action,omitempty" example:"info"`
}
