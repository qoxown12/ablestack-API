package glue

// ISCSIServiceRequest는 iSCSI gateway service 생성/수정 요청이다.
// @name GlueISCSIServiceRequest
type ISCSIServiceRequest struct {
	ServiceID     string   `json:"service_id" example:"iscsi"`
	Hosts         []string `json:"hosts" example:"scvm"`
	TrustedIPList []string `json:"trusted_ip_list,omitempty" example:"10.10.10.11"`
	Pool          string   `json:"pool" example:"rbd"`
	APIPort       int      `json:"api_port" example:"5000"`
	APIUser       string   `json:"api_user" example:"admin"`
	APIPassword   string   `json:"api_password" example:"password"`
	Count         int      `json:"count,omitempty" example:"1"`
}

// ISCSIAuthRequest는 iSCSI target/discovery 인증 요청이다.
// @name GlueISCSIAuthRequest
type ISCSIAuthRequest struct {
	User           string `json:"user,omitempty" example:"iscsiuser"`
	Password       string `json:"password,omitempty" example:"password1234"`
	MutualUser     string `json:"mutual_user,omitempty" example:"mutualuser"`
	MutualPassword string `json:"mutual_password,omitempty" example:"password5678"`
}

// ISCSITargetRequest는 iSCSI target 생성 요청이다.
// @name GlueISCSITargetRequest
type ISCSITargetRequest struct {
	IQNID          string   `json:"iqn_id" example:"iqn.2026-06.io.ablecloud:target01"`
	Hosts          []string `json:"hosts" example:"scvm"`
	IPAddress      []string `json:"ip_address" example:"10.10.10.11"`
	PoolName       []string `json:"pool_name,omitempty" example:"rbd"`
	ImageName      []string `json:"image_name,omitempty" example:"vm01"`
	ACLEnabled     bool     `json:"acl_enabled" example:"false"`
	Username       string   `json:"username,omitempty" example:"iscsiuser"`
	Password       string   `json:"password,omitempty" example:"password1234"`
	MutualUsername string   `json:"mutual_username,omitempty" example:"mutualuser"`
	MutualPassword string   `json:"mutual_password,omitempty" example:"password5678"`
}

// ISCSITargetUpdateRequest는 iSCSI target 수정 요청이다.
// @name GlueISCSITargetUpdateRequest
type ISCSITargetUpdateRequest struct {
	IQNID          string   `json:"iqn_id" example:"iqn.2026-06.io.ablecloud:target01"`
	NewIQNID       string   `json:"new_iqn_id" example:"iqn.2026-06.io.ablecloud:target02"`
	Hosts          []string `json:"hosts" example:"scvm"`
	IPAddress      []string `json:"ip_address" example:"10.10.10.11"`
	PoolName       []string `json:"pool_name,omitempty" example:"rbd"`
	ImageName      []string `json:"image_name,omitempty" example:"vm01"`
	ACLEnabled     bool     `json:"acl_enabled" example:"false"`
	Username       string   `json:"username,omitempty" example:"iscsiuser"`
	Password       string   `json:"password,omitempty" example:"password1234"`
	MutualUsername string   `json:"mutual_username,omitempty" example:"mutualuser"`
	MutualPassword string   `json:"mutual_password,omitempty" example:"password5678"`
}

// ISCSITargetDeleteRequest는 iSCSI target 삭제 요청이다.
// @name GlueISCSITargetDeleteRequest
type ISCSITargetDeleteRequest struct {
	IQNID string `json:"iqn_id" example:"iqn.2026-06.io.ablecloud:target01"`
}
