package glue

import (
	"strings"

	GlueModel "ablecloud.io/ablestack-api/internal/model/glue"
	"ablecloud.io/ablestack-api/internal/service/glueservice"
	"github.com/gin-gonic/gin"
)

var _ = GlueModel.Response{}

// NFSClusterList는 NFS cluster 정보를 조회한다.
func NFSClusterList(context *gin.Context) {
	val, err := glueservice.NFSClusters(context.Request.Context(), context.Query("cluster_id"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NFSExportList는 NFS export 정보를 조회한다.
func NFSExportList(context *gin.Context) {
	val, err := glueservice.NFSExports(context.Request.Context(), context.Query("cluster_id"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NFSClusterCreate는 NFS cluster service를 생성한다.
func NFSClusterCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.NFSClusterCreate(context.Request.Context(), context.Param("cluster_id"), context.Param("port"), splitParamList(params["hosts"]), params["service_count"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NFSClusterUpdate는 NFS cluster service를 수정한다.
func NFSClusterUpdate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.NFSClusterUpdate(context.Request.Context(), context.Param("cluster_id"), context.Param("port"), splitParamList(params["hosts"]), params["service_count"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NFSClusterDelete는 NFS cluster service를 삭제한다.
func NFSClusterDelete(context *gin.Context) {
	val, err := glueservice.NFSClusterDelete(context.Request.Context(), context.Param("cluster_id"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NFSIngressCreate는 NFS ingress service를 생성한다.
func NFSIngressCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.NFSIngressCreate(
		context.Request.Context(),
		params["service_id"],
		splitParamList(params["hosts"]),
		params["backend_service"],
		params["virtual_ip"],
		params["frontend_port"],
		params["monitor_port"],
		splitParamList(params["virtual_interface_networks"]),
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NFSIngressUpdate는 NFS ingress service를 수정한다.
func NFSIngressUpdate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.NFSIngressUpdate(
		context.Request.Context(),
		params["service_id"],
		splitParamList(params["hosts"]),
		params["backend_service"],
		params["virtual_ip"],
		params["frontend_port"],
		params["monitor_port"],
		splitParamList(params["virtual_interface_networks"]),
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NFSExportCreate는 NFS export를 생성한다.
func NFSExportCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.NFSExportCreate(
		context.Request.Context(),
		context.Param("cluster_id"),
		params["access_type"],
		params["fs_name"],
		params["storage_name"],
		params["path"],
		params["pseudo"],
		params["squash"],
		splitParamList(params["transports"]),
		strings.EqualFold(params["security_label"], "true"),
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NFSExportUpdate는 NFS export를 수정한다.
func NFSExportUpdate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.NFSExportUpdate(
		context.Request.Context(),
		context.Param("cluster_id"),
		params["export_id"],
		params["access_type"],
		params["fs_name"],
		params["storage_name"],
		params["path"],
		params["pseudo"],
		params["squash"],
		splitParamList(params["transports"]),
		strings.EqualFold(params["security_label"], "true"),
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NFSExportDelete는 NFS export를 삭제한다.
func NFSExportDelete(context *gin.Context) {
	val, err := glueservice.NFSExportDelete(context.Request.Context(), context.Param("cluster_id"), context.Param("export_id"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// Glue-NFSNFSClusterListDoc Swagger 문서
//
//	@Summary		NFS Cluster 목록
//	@Description	SCVM 로컬에서 ceph nfs cluster info 명령 기반으로 NFS cluster 정보를 조회합니다.
//	@Tags			Glue-NFS
//	@Produce		json
//	@Param			cluster_id	query		string	false	"NFS cluster ID"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/nfs [get]
func NFSClusterListDoc() {}

// Glue-NFSNFSClusterCreateDoc Swagger 문서
//
//	@Summary		NFS Cluster 생성
//	@Description	SCVM 로컬에서 NFS service spec을 생성하고 ceph orch apply로 적용합니다.
//	@Tags			Glue-NFS
//	@Accept			json
//	@Produce		json
//	@Param			cluster_id	path		string							true	"NFS cluster ID"
//	@Param			port		path		string							true	"NFS port"
//	@Param			body		body		GlueModel.NFSClusterRequest	true	"NFS cluster create request"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/nfs/{cluster_id}/{port} [post]
func NFSClusterCreateDoc() {}

// Glue-NFSNFSClusterUpdateDoc Swagger 문서
//
//	@Summary		NFS Cluster 수정
//	@Description	SCVM 로컬에서 NFS service spec을 재적용한 뒤 ceph orch redeploy를 실행합니다.
//	@Tags			Glue-NFS
//	@Accept			json
//	@Produce		json
//	@Param			cluster_id	path		string							true	"NFS cluster ID"
//	@Param			port		path		string							true	"NFS port"
//	@Param			body		body		GlueModel.NFSClusterRequest	true	"NFS cluster update request"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/nfs/{cluster_id}/{port} [put]
func NFSClusterUpdateDoc() {}

// Glue-NFSNFSClusterDeleteDoc Swagger 문서
//
//	@Summary		NFS Cluster 삭제
//	@Description	SCVM 로컬에서 ceph nfs cluster rm 명령으로 NFS cluster를 삭제합니다.
//	@Tags			Glue-NFS
//	@Produce		json
//	@Param			cluster_id	path		string	true	"NFS cluster ID"
//	@Success		200		{object}	GlueModel.Response
//	@Failure		400		{object}	GlueModel.Response
//	@Failure		403		{object}	GlueModel.Response
//	@Failure		500		{object}	GlueModel.Response
//	@Router			/glue/nfs/{cluster_id} [delete]
func NFSClusterDeleteDoc() {}

// Glue-NFSNFSIngressCreateDoc Swagger 문서
//
//	@Summary		NFS Ingress 생성
//	@Description	SCVM 로컬에서 HAProxy/keepalived ingress spec을 생성하고 ceph orch apply로 적용합니다.
//	@Tags			Glue-NFS
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.NFSIngressRequest	true	"NFS ingress create request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/nfs/ingress [post]
func NFSIngressCreateDoc() {}

// Glue-NFSNFSIngressUpdateDoc Swagger 문서
//
//	@Summary		NFS Ingress 수정
//	@Description	SCVM 로컬에서 HAProxy/keepalived ingress spec을 재적용한 뒤 ceph orch redeploy를 실행합니다.
//	@Tags			Glue-NFS
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.NFSIngressRequest	true	"NFS ingress update request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/nfs/ingress [put]
func NFSIngressUpdateDoc() {}

// Glue-NFSNFSExportListDoc Swagger 문서
//
//	@Summary		NFS Export 목록
//	@Description	SCVM 로컬에서 ceph nfs export ls --detailed 명령 기반으로 NFS export를 조회합니다.
//	@Tags			Glue-NFS
//	@Produce		json
//	@Param			cluster_id	query		string	false	"NFS cluster ID"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/nfs/export [get]
func NFSExportListDoc() {}

// Glue-NFSNFSExportCreateDoc Swagger 문서
//
//	@Summary		NFS Export 생성
//	@Description	SCVM 로컬에서 NFS export JSON spec을 생성하고 ceph nfs export apply로 적용합니다.
//	@Tags			Glue-NFS
//	@Accept			json
//	@Produce		json
//	@Param			cluster_id	path		string							true	"NFS cluster ID"
//	@Param			body		body		GlueModel.NFSExportRequest	true	"NFS export create request"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/nfs/export/{cluster_id} [post]
func NFSExportCreateDoc() {}

// Glue-NFSNFSExportUpdateDoc Swagger 문서
//
//	@Summary		NFS Export 수정
//	@Description	SCVM 로컬에서 NFS export JSON spec을 재적용합니다.
//	@Tags			Glue-NFS
//	@Accept			json
//	@Produce		json
//	@Param			cluster_id	path		string							true	"NFS cluster ID"
//	@Param			body		body		GlueModel.NFSExportRequest	true	"NFS export update request"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/nfs/export/{cluster_id} [put]
func NFSExportUpdateDoc() {}

// Glue-NFSNFSExportDeleteDoc Swagger 문서
//
//	@Summary		NFS Export 삭제
//	@Description	export_id에 해당하는 pseudo를 조회한 뒤 ceph nfs export rm 명령으로 삭제합니다.
//	@Tags			Glue-NFS
//	@Produce		json
//	@Param			cluster_id	path		string	true	"NFS cluster ID"
//	@Param			export_id	path		string	true	"NFS export ID"
//	@Success		200		{object}	GlueModel.Response
//	@Failure		400		{object}	GlueModel.Response
//	@Failure		403		{object}	GlueModel.Response
//	@Failure		500		{object}	GlueModel.Response
//	@Router			/glue/nfs/export/{cluster_id}/{export_id} [delete]
func NFSExportDeleteDoc() {}
