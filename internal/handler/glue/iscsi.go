package glue

import (
	GlueModel "ablecloud.io/ablestack-api/internal/model/glue"
	"ablecloud.io/ablestack-api/internal/service/glueservice"
	"github.com/gin-gonic/gin"
)

var _ = GlueModel.Response{}

// ISCSIServiceCreate는 iSCSI gateway service를 생성한다.
func ISCSIServiceCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.ISCSIServiceCreate(
		context.Request.Context(),
		params["service_id"],
		splitParamList(params["hosts"]),
		iscsiTrustedIPs(params),
		params["pool"],
		params["api_port"],
		params["api_user"],
		params["api_password"],
		params["count"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// ISCSIServiceUpdate는 iSCSI gateway service를 수정하고 redeploy한다.
func ISCSIServiceUpdate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.ISCSIServiceUpdate(
		context.Request.Context(),
		params["service_id"],
		splitParamList(params["hosts"]),
		iscsiTrustedIPs(params),
		params["pool"],
		params["api_port"],
		params["api_user"],
		params["api_password"],
		params["count"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// ISCSIDiscoveryGet은 iSCSI discovery auth 정보를 조회한다.
func ISCSIDiscoveryGet(context *gin.Context) {
	val, err := glueservice.ISCSIDiscoveryAuth(context.Request.Context())
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// ISCSIDiscoveryUpdate는 iSCSI discovery auth 정보를 수정한다.
func ISCSIDiscoveryUpdate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.ISCSIDiscoveryAuthUpdate(
		context.Request.Context(),
		params["user"],
		params["password"],
		params["mutual_user"],
		params["mutual_password"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// ISCSITargetList는 iSCSI target 목록 또는 상세를 조회한다.
func ISCSITargetList(context *gin.Context) {
	val, err := glueservice.ISCSITargetList(context.Request.Context(), context.Query("iqn_id"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// ISCSITargetCreate는 iSCSI target을 생성한다.
func ISCSITargetCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.ISCSITargetCreate(
		context.Request.Context(),
		params["iqn_id"],
		splitParamList(params["hosts"]),
		splitParamList(params["ip_address"]),
		splitParamList(params["pool_name"]),
		splitParamList(params["image_name"]),
		params["acl_enabled"],
		params["username"],
		params["password"],
		params["mutual_username"],
		params["mutual_password"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// ISCSITargetUpdate는 iSCSI target을 수정한다.
func ISCSITargetUpdate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.ISCSITargetUpdate(
		context.Request.Context(),
		params["iqn_id"],
		params["new_iqn_id"],
		splitParamList(params["hosts"]),
		splitParamList(params["ip_address"]),
		splitParamList(params["pool_name"]),
		splitParamList(params["image_name"]),
		params["acl_enabled"],
		params["username"],
		params["password"],
		params["mutual_username"],
		params["mutual_password"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// ISCSITargetDelete는 iSCSI target을 삭제한다.
func ISCSITargetDelete(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.ISCSITargetDelete(context.Request.Context(), params["iqn_id"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// ISCSITargetPurge는 gwcli로 iSCSI target을 강제 삭제한다.
func ISCSITargetPurge(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.ISCSITargetPurge(context.Request.Context(), params["iqn_id"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

func iscsiTrustedIPs(params map[string]string) []string {
	if values := splitParamList(params["trusted_ip_list"]); len(values) > 0 {
		return values
	}
	return splitParamList(params["ip_address"])
}

// Glue-ISCSIISCSIServiceCreateDoc Swagger 문서
//
//	@Summary		iSCSI Service 생성
//	@Description	SCVM 로컬에서 iSCSI gateway service spec을 생성하고 ceph orch apply로 적용합니다.
//	@Tags			Glue-ISCSI
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.ISCSIServiceRequest	true	"iSCSI service request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/iscsi [post]
func ISCSIServiceCreateDoc() {}

// Glue-ISCSIISCSIServiceUpdateDoc Swagger 문서
//
//	@Summary		iSCSI Service 수정
//	@Description	SCVM 로컬에서 iSCSI gateway service spec을 재적용한 뒤 ceph orch redeploy를 실행합니다.
//	@Tags			Glue-ISCSI
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.ISCSIServiceRequest	true	"iSCSI service request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/iscsi [put]
func ISCSIServiceUpdateDoc() {}

// Glue-ISCSIISCSIDiscoveryGetDoc Swagger 문서
//
//	@Summary		iSCSI Discovery 조회
//	@Description	Glue dashboard API로 iSCSI discovery auth 정보를 조회합니다.
//	@Tags			Glue-ISCSI
//	@Produce		json
//	@Success		200	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/iscsi/discovery [get]
func ISCSIDiscoveryGetDoc() {}

// Glue-ISCSIISCSIDiscoveryUpdateDoc Swagger 문서
//
//	@Summary		iSCSI Discovery 수정
//	@Description	Glue dashboard API로 iSCSI discovery auth 정보를 수정합니다.
//	@Tags			Glue-ISCSI
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.ISCSIAuthRequest	true	"iSCSI discovery request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/iscsi/discovery [put]
func ISCSIDiscoveryUpdateDoc() {}

// Glue-ISCSIISCSITargetListDoc Swagger 문서
//
//	@Summary		iSCSI Target 목록
//	@Description	Glue dashboard API로 iSCSI target 목록 또는 상세를 조회합니다.
//	@Tags			Glue-ISCSI
//	@Produce		json
//	@Param			iqn_id	query		string	false	"iSCSI target IQN"
//	@Success		200		{object}	GlueModel.Response
//	@Failure		400		{object}	GlueModel.Response
//	@Failure		403		{object}	GlueModel.Response
//	@Failure		500		{object}	GlueModel.Response
//	@Router			/glue/iscsi/target [get]
func ISCSITargetListDoc() {}

// Glue-ISCSIISCSITargetCreateDoc Swagger 문서
//
//	@Summary		iSCSI Target 생성
//	@Description	Glue dashboard API로 iSCSI target을 생성합니다.
//	@Tags			Glue-ISCSI
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.ISCSITargetRequest	true	"iSCSI target request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/iscsi/target [post]
func ISCSITargetCreateDoc() {}

// Glue-ISCSIISCSITargetUpdateDoc Swagger 문서
//
//	@Summary		iSCSI Target 수정
//	@Description	Glue dashboard API로 iSCSI target을 수정합니다.
//	@Tags			Glue-ISCSI
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.ISCSITargetUpdateRequest	true	"iSCSI target update request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/iscsi/target [put]
func ISCSITargetUpdateDoc() {}

// Glue-ISCSIISCSITargetDeleteDoc Swagger 문서
//
//	@Summary		iSCSI Target 삭제
//	@Description	Glue dashboard API로 iSCSI target을 삭제합니다.
//	@Tags			Glue-ISCSI
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.ISCSITargetDeleteRequest	true	"iSCSI target delete request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/iscsi/target [delete]
func ISCSITargetDeleteDoc() {}

// Glue-ISCSIISCSITargetPurgeDoc Swagger 문서
//
//	@Summary		iSCSI Target 강제 삭제
//	@Description	SCVM 로컬 iSCSI gateway container 안에서 gwcli delete를 실행합니다.
//	@Tags			Glue-ISCSI
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.ISCSITargetDeleteRequest	true	"iSCSI target purge request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/iscsi/target/purge [delete]
func ISCSITargetPurgeDoc() {}
