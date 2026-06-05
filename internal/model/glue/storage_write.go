package glue

// GlueFSPlacementRequest는 GlueFS 생성/수정 시 MDS 배치에 사용하는 요청이다.
// @name GlueGlueFSPlacementRequest
type GlueFSPlacementRequest struct {
	Hosts   []string `json:"hosts,omitempty" example:"scvm"`
	OldName string   `json:"old_name,omitempty" example:"gluefs"`
	NewName string   `json:"new_name,omitempty" example:"gluefs2"`
}

// GlueFSSubvolumeGroupRequest는 GlueFS subvolume group 생성/수정 요청이다.
// @name GlueGlueFSSubvolumeGroupRequest
type GlueFSSubvolumeGroupRequest struct {
	VolName      string `json:"vol_name" example:"gluefs"`
	GroupName    string `json:"group_name" example:"group01"`
	Size         int64  `json:"size,omitempty" example:"10"`
	NewSize      int64  `json:"new_size,omitempty" example:"20"`
	DataPoolName string `json:"data_pool_name,omitempty" example:"gluefs.data"`
	Mode         string `json:"mode,omitempty" example:"755"`
}

// NFSClusterRequest는 NFS cluster service 생성/수정 요청이다.
// @name GlueNFSClusterRequest
type NFSClusterRequest struct {
	Hosts        []string `json:"hosts" example:"scvm"`
	ServiceCount int      `json:"service_count,omitempty" example:"1"`
}

// NFSIngressRequest는 NFS ingress service 생성/수정 요청이다.
// @name GlueNFSIngressRequest
type NFSIngressRequest struct {
	ServiceID                string   `json:"service_id" example:"nfs-ingress"`
	Hosts                    []string `json:"hosts" example:"scvm"`
	BackendService           string   `json:"backend_service" example:"nfs.nfs-a"`
	VirtualIP                string   `json:"virtual_ip" example:"10.10.10.100/24"`
	FrontendPort             string   `json:"frontend_port" example:"2049"`
	MonitorPort              string   `json:"monitor_port" example:"9049"`
	VirtualInterfaceNetworks []string `json:"virtual_interface_networks,omitempty" example:"10.10.10.0/24"`
}

// NFSExportRequest는 NFS export 생성/수정 요청이다.
// @name GlueNFSExportRequest
type NFSExportRequest struct {
	ExportID      int      `json:"export_id,omitempty" example:"1"`
	AccessType    string   `json:"access_type" example:"RW" enums:"RW,RO,NONE"`
	FSName        string   `json:"fs_name,omitempty" example:"gluefs"`
	StorageName   string   `json:"storage_name" example:"CEPH" enums:"CEPH,RGW"`
	Path          string   `json:"path" example:"/volumes/group01"`
	Pseudo        string   `json:"pseudo" example:"/export01"`
	Squash        string   `json:"squash" example:"no_root_squash"`
	Transports    []string `json:"transports,omitempty" example:"TCP"`
	SecurityLabel bool     `json:"security_label,omitempty" example:"false"`
}

// RGWServiceRequest는 RGW service 생성/수정 요청이다.
// @name GlueRGWServiceRequest
type RGWServiceRequest struct {
	ServiceName   string   `json:"service_name" example:"rgw.default"`
	RealmName     string   `json:"realm_name,omitempty" example:"realm01"`
	ZonegroupName string   `json:"zonegroup_name,omitempty" example:"zonegroup01"`
	ZoneName      string   `json:"zone_name,omitempty" example:"zone01"`
	Hosts         []string `json:"hosts" example:"scvm"`
	Port          string   `json:"port" example:"8080"`
}

// RGWUserRequest는 RGW user 생성/수정 요청이다.
// @name GlueRGWUserRequest
type RGWUserRequest struct {
	Username    string `json:"username" example:"user01"`
	DisplayName string `json:"display_name,omitempty" example:"User 01"`
	Email       string `json:"email,omitempty" example:"user01@example.com"`
	KeyType     string `json:"key_type,omitempty" example:"s3"`
	AccessKey   string `json:"access_key,omitempty" example:"accesskey"`
	SecretKey   string `json:"secret_key,omitempty" example:"secretkey"`
}

// RGWQuotaRequest는 RGW quota 설정 요청이다.
// @name GlueRGWQuotaRequest
type RGWQuotaRequest struct {
	Username   string `json:"username" example:"user01"`
	Scope      string `json:"scope" example:"user" enums:"user,bucket"`
	MaxObjects string `json:"max_objects" example:"1000"`
	MaxSize    string `json:"max_size" example:"10G"`
	State      string `json:"state" example:"enable" enums:"enable,disable"`
}

// RGWBucketRequest는 RGW bucket 생성/수정 요청이다.
// @name GlueRGWBucketRequest
type RGWBucketRequest struct {
	BucketName             string `json:"bucket_name" example:"bucket01"`
	BucketID               string `json:"bucket_id,omitempty" example:"bucket-id"`
	Username               string `json:"username,omitempty" example:"user01"`
	LockEnabled            string `json:"lock_enabled,omitempty" example:"false" enums:"true,false"`
	LockMode               string `json:"lock_mode,omitempty" example:"governance" enums:"compliance,governance"`
	LockRetentionPeriodDay string `json:"lock_retention_period_days,omitempty" example:"30"`
	Versioning             string `json:"versioning,omitempty" example:"Enabled" enums:"Enabled,Suspended"`
}
