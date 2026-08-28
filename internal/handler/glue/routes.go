package glue

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// glueRoutes는 Glue 루트 응답과 skeleton route 목록을 함께 관리한다.
// Status가 "active"이면 실제 SCVM 로컬 명령 handler에 연결된 endpoint이고,
// 비어 있으면 UI가 API 형태를 확인할 수 있도록 501 skeleton으로만 등록한다.
var glueRoutes = []routeSpec{
	// 핵심 Glue 상태 endpoint. 읽기 전용이며 로컬 ceph/orch 명령을 실행한다.
	{Method: "GET", Path: "", Module: "glue", Endpoint: "root", Description: "Glue API registration and SCVM role status", Status: "active"},
	{Method: "GET", Path: "/status", Module: "glue", Endpoint: "status", Description: "show Glue cluster status", Status: "active"},
	{Method: "GET", Path: "/hosts", Module: "glue", Endpoint: "hosts", Description: "list Glue hosts", Status: "active"},
	{Method: "GET", Path: "/version", Module: "glue", Endpoint: "version", Description: "show Glue daemon versions", Status: "active"},
	{Method: "GET", Path: "/pw", Module: "glue", Endpoint: "password_encryption", Description: "legacy password encryption helper"},

	// Pool/RBD image endpoint는 shell pipeline 없이 구현한다.
	{Method: "GET", Path: "/pool", Module: "pool", Endpoint: "list", Description: "list pools", Status: "active"},
	{Method: "DELETE", Path: "/pool/:pool_name", Module: "pool", Endpoint: "delete", Description: "delete pool", Status: "active"},

	{Method: "GET", Path: "/image", Module: "image", Endpoint: "list", Description: "list or inspect RBD images", Status: "active"},
	{Method: "POST", Path: "/image", Module: "image", Endpoint: "create", Description: "create RBD image", Status: "active"},
	{Method: "PUT", Path: "/image", Module: "image", Endpoint: "resize", Description: "resize RBD image", Status: "active"},
	{Method: "DELETE", Path: "/image", Module: "image", Endpoint: "delete", Description: "delete RBD image", Status: "active"},

	// Service endpoint는 ceph orch로만 매핑한다. legacy glue-api의 SMB systemctl 처리는 가져오지 않는다.
	{Method: "GET", Path: "/service", Module: "service", Endpoint: "list", Description: "list orchestrator services", Status: "active"},
	{Method: "POST", Path: "/service/:service_name", Module: "service", Endpoint: "control", Description: "control orchestrator service", Status: "active"},
	{Method: "DELETE", Path: "/service/:service_name", Module: "service", Endpoint: "delete", Description: "delete orchestrator service", Status: "active"},

	// GlueFS/NFS/iSCSI/RGW route는 SCVM 로컬 ceph/orch/dashboard 명령 흐름으로 연결한다.
	{Method: "GET", Path: "/gluefs", Module: "gluefs", Endpoint: "status", Description: "show GlueFS status", Status: "active"},
	{Method: "PUT", Path: "/gluefs", Module: "gluefs", Endpoint: "update", Description: "update GlueFS daemon placement", Status: "active"},
	{Method: "POST", Path: "/gluefs/:fs_name", Module: "gluefs", Endpoint: "create", Description: "create GlueFS", Status: "active"},
	{Method: "DELETE", Path: "/gluefs/:fs_name", Module: "gluefs", Endpoint: "delete", Description: "delete GlueFS", Status: "active"},
	{Method: "GET", Path: "/gluefs/info/:fs_name", Module: "gluefs", Endpoint: "info", Description: "show GlueFS info", Status: "active"},
	{Method: "GET", Path: "/gluefs/subvolume/group", Module: "gluefs", Endpoint: "subvolume_group_list", Description: "list subvolume groups", Status: "active"},
	{Method: "POST", Path: "/gluefs/subvolume/group", Module: "gluefs", Endpoint: "subvolume_group_create", Description: "create subvolume group", Status: "active"},
	{Method: "DELETE", Path: "/gluefs/subvolume/group", Module: "gluefs", Endpoint: "subvolume_group_delete", Description: "delete subvolume group", Status: "active"},
	{Method: "PUT", Path: "/gluefs/subvolume/group", Module: "gluefs", Endpoint: "subvolume_group_resize", Description: "resize subvolume group", Status: "active"},

	{Method: "GET", Path: "/nfs", Module: "nfs", Endpoint: "cluster_list", Description: "list NFS clusters", Status: "active"},
	{Method: "POST", Path: "/nfs/:cluster_id/:port", Module: "nfs", Endpoint: "cluster_create", Description: "create NFS cluster", Status: "active"},
	{Method: "PUT", Path: "/nfs/:cluster_id/:port", Module: "nfs", Endpoint: "cluster_update", Description: "update NFS cluster", Status: "active"},
	{Method: "DELETE", Path: "/nfs/:cluster_id", Module: "nfs", Endpoint: "cluster_delete", Description: "delete NFS cluster", Status: "active"},
	{Method: "POST", Path: "/nfs/ingress", Module: "nfs", Endpoint: "ingress_create", Description: "create NFS ingress", Status: "active"},
	{Method: "PUT", Path: "/nfs/ingress", Module: "nfs", Endpoint: "ingress_update", Description: "update NFS ingress", Status: "active"},
	{Method: "GET", Path: "/nfs/export", Module: "nfs", Endpoint: "export_list", Description: "list NFS exports", Status: "active"},
	{Method: "POST", Path: "/nfs/export/:cluster_id", Module: "nfs", Endpoint: "export_create", Description: "create NFS export", Status: "active"},
	{Method: "PUT", Path: "/nfs/export/:cluster_id", Module: "nfs", Endpoint: "export_update", Description: "update NFS export", Status: "active"},
	{Method: "DELETE", Path: "/nfs/export/:cluster_id/:export_id", Module: "nfs", Endpoint: "export_delete", Description: "delete NFS export", Status: "active"},

	// iSCSI endpoint는 ceph orch, Glue dashboard API, local podman gwcli로 연결한다.
	{Method: "POST", Path: "/iscsi", Module: "iscsi", Endpoint: "service_create", Description: "create iSCSI service", Status: "active"},
	{Method: "PUT", Path: "/iscsi", Module: "iscsi", Endpoint: "service_update", Description: "update iSCSI service", Status: "active"},
	{Method: "GET", Path: "/iscsi/discovery", Module: "iscsi", Endpoint: "discovery_get", Description: "show iSCSI discovery auth", Status: "active"},
	{Method: "PUT", Path: "/iscsi/discovery", Module: "iscsi", Endpoint: "discovery_update", Description: "update iSCSI discovery auth", Status: "active"},
	{Method: "GET", Path: "/iscsi/target", Module: "iscsi", Endpoint: "target_list", Description: "list iSCSI targets", Status: "active"},
	{Method: "PUT", Path: "/iscsi/image", Module: "iscsi", Endpoint: "image_resize", Description: "resize iSCSI RBD image", Status: "active"},
	{Method: "POST", Path: "/iscsi/target", Module: "iscsi", Endpoint: "target_create", Description: "create iSCSI target", Status: "active"},
	{Method: "PUT", Path: "/iscsi/target", Module: "iscsi", Endpoint: "target_update", Description: "update iSCSI target", Status: "active"},
	{Method: "DELETE", Path: "/iscsi/target", Module: "iscsi", Endpoint: "target_delete", Description: "delete iSCSI target", Status: "active"},
	{Method: "DELETE", Path: "/iscsi/target/purge", Module: "iscsi", Endpoint: "target_purge", Description: "purge iSCSI targets", Status: "active"},

	// SMB endpoint는 SCVM 로컬 Samba 실행 스크립트로 연결한다. legacy glue-api의 SSH host 반복은 제거한다.
	{Method: "GET", Path: "/smb", Module: "smb", Endpoint: "status", Description: "show SMB status", Status: "active"},
	{Method: "POST", Path: "/smb", Module: "smb", Endpoint: "create", Description: "create SMB share", Status: "active"},
	{Method: "DELETE", Path: "/smb", Module: "smb", Endpoint: "delete", Description: "delete SMB share", Status: "active"},
	{Method: "POST", Path: "/smb/folder", Module: "smb", Endpoint: "folder_add", Description: "add SMB share folder", Status: "active"},
	{Method: "DELETE", Path: "/smb/folder", Module: "smb", Endpoint: "folder_delete", Description: "delete SMB share folder", Status: "active"},
	{Method: "POST", Path: "/smb/user", Module: "smb", Endpoint: "user_create", Description: "create SMB user", Status: "active"},
	{Method: "PUT", Path: "/smb/user", Module: "smb", Endpoint: "user_update", Description: "update SMB user", Status: "active"},
	{Method: "DELETE", Path: "/smb/user", Module: "smb", Endpoint: "user_delete", Description: "delete SMB user", Status: "active"},

	{Method: "GET", Path: "/rgw", Module: "rgw", Endpoint: "daemon", Description: "show RGW daemon status", Status: "active"},
	{Method: "POST", Path: "/rgw", Module: "rgw", Endpoint: "service_create", Description: "create RGW service", Status: "active"},
	{Method: "PUT", Path: "/rgw", Module: "rgw", Endpoint: "service_update", Description: "update RGW service", Status: "active"},
	{Method: "POST", Path: "/rgw/quota", Module: "rgw", Endpoint: "quota", Description: "update RGW quota", Status: "active"},
	{Method: "GET", Path: "/rgw/user", Module: "rgw", Endpoint: "user_list", Description: "list RGW users", Status: "active"},
	{Method: "POST", Path: "/rgw/user", Module: "rgw", Endpoint: "user_create", Description: "create RGW user", Status: "active"},
	{Method: "PUT", Path: "/rgw/user", Module: "rgw", Endpoint: "user_update", Description: "update RGW user", Status: "active"},
	{Method: "DELETE", Path: "/rgw/user", Module: "rgw", Endpoint: "user_delete", Description: "delete RGW user", Status: "active"},
	{Method: "GET", Path: "/rgw/bucket", Module: "rgw", Endpoint: "bucket_list", Description: "list RGW buckets", Status: "active"},
	{Method: "POST", Path: "/rgw/bucket", Module: "rgw", Endpoint: "bucket_create", Description: "create RGW bucket", Status: "active"},
	{Method: "PUT", Path: "/rgw/bucket", Module: "rgw", Endpoint: "bucket_update", Description: "update RGW bucket", Status: "active"},
	{Method: "DELETE", Path: "/rgw/bucket", Module: "rgw", Endpoint: "bucket_delete", Description: "delete RGW bucket", Status: "active"},

	// NVMe-oF endpoint는 SSH 없이 SCVM 로컬 ceph orch/podman 명령으로 연결한다.
	{Method: "POST", Path: "/nvmeof", Module: "nvmeof", Endpoint: "service_create", Description: "create NVMe-oF service", Status: "active"},
	{Method: "POST", Path: "/nvmeof/image/download", Module: "nvmeof", Endpoint: "image_download", Description: "download NVMe-oF image", Status: "active"},
	{Method: "GET", Path: "/nvmeof/target", Module: "nvmeof", Endpoint: "target_list", Description: "list NVMe-oF targets", Status: "active"},
	{Method: "POST", Path: "/nvmeof/target", Module: "nvmeof", Endpoint: "target_create", Description: "create NVMe-oF target", Status: "active"},
	{Method: "GET", Path: "/nvmeof/subsystem", Module: "nvmeof", Endpoint: "subsystem_list", Description: "list NVMe-oF subsystems", Status: "active"},
	{Method: "POST", Path: "/nvmeof/subsystem", Module: "nvmeof", Endpoint: "subsystem_create", Description: "create NVMe-oF subsystem", Status: "active"},
	{Method: "DELETE", Path: "/nvmeof/subsystem", Module: "nvmeof", Endpoint: "subsystem_delete", Description: "delete NVMe-oF subsystem", Status: "active"},
	{Method: "GET", Path: "/nvmeof/namespace", Module: "nvmeof", Endpoint: "namespace_list", Description: "list NVMe-oF namespaces", Status: "active"},
	{Method: "POST", Path: "/nvmeof/namespace", Module: "nvmeof", Endpoint: "namespace_create", Description: "create NVMe-oF namespace", Status: "active"},
	{Method: "DELETE", Path: "/nvmeof/namespace", Module: "nvmeof", Endpoint: "namespace_delete", Description: "delete NVMe-oF namespace", Status: "active"},

	// Mirror는 SSH/scp 없이 local RBD mirror 명령과 bootstrap token import/export 방식으로 연결한다.
	{Method: "GET", Path: "/mirror", Module: "mirror", Endpoint: "status", Description: "show mirror status", Status: "active"},
	{Method: "POST", Path: "/mirror", Module: "mirror", Endpoint: "setup", Description: "setup mirror cluster", Status: "active"},
	{Method: "PUT", Path: "/mirror", Module: "mirror", Endpoint: "update", Description: "update mirror cluster", Status: "active"},
	{Method: "DELETE", Path: "/mirror", Module: "mirror", Endpoint: "delete", Description: "delete mirror cluster", Status: "active"},
	{Method: "POST", Path: "/mirror/:mirrorPool", Module: "mirror", Endpoint: "pool_enable", Description: "enable pool mirroring", Status: "active"},
	{Method: "DELETE", Path: "/mirror/:mirrorPool", Module: "mirror", Endpoint: "pool_disable", Description: "disable pool mirroring", Status: "active"},
	{Method: "DELETE", Path: "/mirror/garbage", Module: "mirror", Endpoint: "garbage_delete", Description: "delete mirror garbage", Status: "active"},
	{Method: "GET", Path: "/mirror/image/:mirrorPool", Module: "mirror", Endpoint: "image_list", Description: "list mirrored images", Status: "active"},
	{Method: "GET", Path: "/mirror/image/:mirrorPool/:imageName", Module: "mirror", Endpoint: "image_info", Description: "show mirrored image info", Status: "active"},
}

// RegisterRoutes는 SCVM에서 사용할 Glue handler를 연결한다.
// main에서 SCVM 판정 후 호출하지만, 방어적으로 RequireSCVM middleware도 유지한다.
func RegisterRoutes(group *gin.RouterGroup) {
	group.Use(RequireSCVM())
	registerImplementedRoutes(group)
	for _, route := range glueRoutes {
		if isImplementedRoute(route.Method, route.Path) {
			continue
		}
		group.Handle(route.Method, route.Path, notImplementedHandler(route.Module, route.Endpoint))
	}
}

// RegisterRoutesIfSCVM은 host/CCVM에서는 /api/v1/glue route 자체를 등록하지 않는다.
// 따라서 host/CCVM 호출자는 403이 아니라 404를 받는다.
func RegisterRoutesIfSCVM(group *gin.RouterGroup) bool {
	if !IsSCVMNode() {
		return false
	}
	RegisterRoutes(group)
	return true
}

func IsSCVMNode() bool {
	return DetectNodeRole().SCVM
}

// registerImplementedRoutes는 현재 실제 SCVM 로컬 명령을 실행하는 endpoint만 등록한다.
func registerImplementedRoutes(group *gin.RouterGroup) {
	group.GET("", Status)
	group.GET("/status", ClusterStatus)
	group.GET("/hosts", Hosts)
	group.GET("/version", Version)
	group.GET("/pool", PoolList)
	group.DELETE("/pool/:pool_name", PoolDelete)
	group.GET("/image", ImageList)
	group.POST("/image", ImageCreate)
	group.PUT("/image", ImageResize)
	group.DELETE("/image", ImageDelete)
	group.GET("/service", ServiceList)
	group.POST("/service/:service_name", ServiceControl)
	group.DELETE("/service/:service_name", ServiceDelete)
	group.GET("/gluefs", GlueFSStatus)
	group.PUT("/gluefs", GlueFSUpdate)
	group.POST("/gluefs/:fs_name", GlueFSCreate)
	group.DELETE("/gluefs/:fs_name", GlueFSDelete)
	group.GET("/gluefs/info/:fs_name", GlueFSInfo)
	group.GET("/gluefs/subvolume/group", GlueFSSubvolumeGroupList)
	group.POST("/gluefs/subvolume/group", GlueFSSubvolumeGroupCreate)
	group.DELETE("/gluefs/subvolume/group", GlueFSSubvolumeGroupDelete)
	group.PUT("/gluefs/subvolume/group", GlueFSSubvolumeGroupResize)
	group.GET("/nfs", NFSClusterList)
	group.POST("/nfs/:cluster_id/:port", NFSClusterCreate)
	group.PUT("/nfs/:cluster_id/:port", NFSClusterUpdate)
	group.DELETE("/nfs/:cluster_id", NFSClusterDelete)
	group.POST("/nfs/ingress", NFSIngressCreate)
	group.PUT("/nfs/ingress", NFSIngressUpdate)
	group.GET("/nfs/export", NFSExportList)
	group.POST("/nfs/export/:cluster_id", NFSExportCreate)
	group.PUT("/nfs/export/:cluster_id", NFSExportUpdate)
	group.DELETE("/nfs/export/:cluster_id/:export_id", NFSExportDelete)
	group.GET("/rgw", RGWDaemon)
	group.POST("/rgw", RGWServiceCreate)
	group.PUT("/rgw", RGWServiceUpdate)
	group.POST("/rgw/quota", RGWQuota)
	group.GET("/rgw/user", RGWUserList)
	group.POST("/rgw/user", RGWUserCreate)
	group.PUT("/rgw/user", RGWUserUpdate)
	group.DELETE("/rgw/user", RGWUserDelete)
	group.GET("/rgw/bucket", RGWBucketList)
	group.POST("/rgw/bucket", RGWBucketCreate)
	group.PUT("/rgw/bucket", RGWBucketUpdate)
	group.DELETE("/rgw/bucket", RGWBucketDelete)
	group.POST("/iscsi", ISCSIServiceCreate)
	group.PUT("/iscsi", ISCSIServiceUpdate)
	group.GET("/iscsi/discovery", ISCSIDiscoveryGet)
	group.PUT("/iscsi/discovery", ISCSIDiscoveryUpdate)
	group.GET("/iscsi/target", ISCSITargetList)
	group.PUT("/iscsi/image", ImageResize)
	group.POST("/iscsi/target", ISCSITargetCreate)
	group.PUT("/iscsi/target", ISCSITargetUpdate)
	group.DELETE("/iscsi/target", ISCSITargetDelete)
	group.DELETE("/iscsi/target/purge", ISCSITargetPurge)
	group.GET("/smb", SMBStatus)
	group.POST("/smb", SMBCreate)
	group.DELETE("/smb", SMBDelete)
	group.POST("/smb/folder", SMBFolderAdd)
	group.DELETE("/smb/folder", SMBFolderDelete)
	group.POST("/smb/user", SMBUserCreate)
	group.PUT("/smb/user", SMBUserUpdate)
	group.DELETE("/smb/user", SMBUserDelete)
	group.POST("/nvmeof", NVMeOfServiceCreate)
	group.POST("/nvmeof/image/download", NVMeOfImageDownload)
	group.GET("/nvmeof/target", NVMeOfTargetList)
	group.POST("/nvmeof/target", NVMeOfTargetCreate)
	group.GET("/nvmeof/subsystem", NVMeOfSubsystemList)
	group.POST("/nvmeof/subsystem", NVMeOfSubsystemCreate)
	group.DELETE("/nvmeof/subsystem", NVMeOfSubsystemDelete)
	group.GET("/nvmeof/namespace", NVMeOfNamespaceList)
	group.POST("/nvmeof/namespace", NVMeOfNamespaceCreate)
	group.DELETE("/nvmeof/namespace", NVMeOfNamespaceDelete)
	group.GET("/mirror", MirrorStatus)
	group.POST("/mirror", MirrorClusterSetup)
	group.PUT("/mirror", MirrorClusterUpdate)
	group.DELETE("/mirror", MirrorClusterDelete)
	group.DELETE("/mirror/garbage", MirrorGarbageDelete)
	group.POST("/mirror/:mirrorPool", MirrorPoolEnable)
	group.DELETE("/mirror/:mirrorPool", MirrorPoolDisable)
	group.GET("/mirror/image/:mirrorPool", MirrorImageList)
	group.GET("/mirror/image/:mirrorPool/:imageName", MirrorImageInfo)
}

// isImplementedRoute는 같은 method/path에 실제 handler와 skeleton handler가
// 동시에 등록되지 않도록 구분한다.
func isImplementedRoute(method string, path string) bool {
	switch strings.ToUpper(method) + " " + path {
	case "GET ",
		"GET /status",
		"GET /hosts",
		"GET /version",
		"GET /pool",
		"DELETE /pool/:pool_name",
		"GET /image",
		"POST /image",
		"PUT /image",
		"DELETE /image",
		"GET /service",
		"POST /service/:service_name",
		"DELETE /service/:service_name",
		"GET /gluefs",
		"PUT /gluefs",
		"POST /gluefs/:fs_name",
		"DELETE /gluefs/:fs_name",
		"GET /gluefs/info/:fs_name",
		"GET /gluefs/subvolume/group",
		"POST /gluefs/subvolume/group",
		"DELETE /gluefs/subvolume/group",
		"PUT /gluefs/subvolume/group",
		"GET /nfs",
		"POST /nfs/:cluster_id/:port",
		"PUT /nfs/:cluster_id/:port",
		"DELETE /nfs/:cluster_id",
		"POST /nfs/ingress",
		"PUT /nfs/ingress",
		"GET /nfs/export",
		"POST /nfs/export/:cluster_id",
		"PUT /nfs/export/:cluster_id",
		"DELETE /nfs/export/:cluster_id/:export_id",
		"GET /rgw",
		"POST /rgw",
		"PUT /rgw",
		"POST /rgw/quota",
		"GET /rgw/user",
		"POST /rgw/user",
		"PUT /rgw/user",
		"DELETE /rgw/user",
		"GET /rgw/bucket",
		"POST /rgw/bucket",
		"PUT /rgw/bucket",
		"DELETE /rgw/bucket",
		"POST /iscsi",
		"PUT /iscsi",
		"GET /iscsi/discovery",
		"PUT /iscsi/discovery",
		"GET /iscsi/target",
		"PUT /iscsi/image",
		"POST /iscsi/target",
		"PUT /iscsi/target",
		"DELETE /iscsi/target",
		"DELETE /iscsi/target/purge",
		"GET /smb",
		"POST /smb",
		"DELETE /smb",
		"POST /smb/folder",
		"DELETE /smb/folder",
		"POST /smb/user",
		"PUT /smb/user",
		"DELETE /smb/user",
		"POST /nvmeof",
		"POST /nvmeof/image/download",
		"GET /nvmeof/target",
		"POST /nvmeof/target",
		"GET /nvmeof/subsystem",
		"POST /nvmeof/subsystem",
		"DELETE /nvmeof/subsystem",
		"GET /nvmeof/namespace",
		"POST /nvmeof/namespace",
		"DELETE /nvmeof/namespace",
		"GET /mirror",
		"POST /mirror",
		"PUT /mirror",
		"DELETE /mirror",
		"DELETE /mirror/garbage",
		"POST /mirror/:mirrorPool",
		"DELETE /mirror/:mirrorPool",
		"GET /mirror/image/:mirrorPool",
		"GET /mirror/image/:mirrorPool/:imageName":
		return true
	default:
		return false
	}
}
