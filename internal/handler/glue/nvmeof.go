package glue

import (
	"strings"

	GlueModel "ablecloud.io/ablestack-api/internal/model/glue"
	"ablecloud.io/ablestack-api/internal/service/glueservice"
	"github.com/gin-gonic/gin"
)

var _ = GlueModel.Response{}

// NVMeOfServiceCreate는 NVMe-oF service spec을 생성하고 ceph orch에 적용한다.
func NVMeOfServiceCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.NVMeOfServiceCreate(
		context.Request.Context(),
		params["pool_name"],
		splitParamList(params["hosts"]),
		params["tgt_cmd_extra_args"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NVMeOfImageDownload는 SCVM 로컬 podman에 NVMe-oF CLI image를 pull한다.
func NVMeOfImageDownload(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.NVMeOfImageDownload(context.Request.Context(), params["image"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NVMeOfTargetList는 로컬 NVMe-oF daemon container의 SPDK RPC로 target 정보를 조회한다.
func NVMeOfTargetList(context *gin.Context) {
	val, err := glueservice.NVMeOfTargetList(context.Request.Context(), context.Query("subsystem_nqn_id"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NVMeOfTargetCreate는 subsystem/listener/host/namespace를 한 번에 구성한다.
func NVMeOfTargetCreate(context *gin.Context) {
	params := requestParams(context)
	size, err := parseOptionalSizeGiB(params["size"])
	if err != nil {
		badRequest(context, err)
		return
	}
	val, err := glueservice.NVMeOfTargetCreate(
		context.Request.Context(),
		params["gateway_ip"],
		params["gateway_name"],
		params["subsystem_nqn_id"],
		params["pool_name"],
		params["image_name"],
		size,
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NVMeOfSubsystemList는 NVMe-oF subsystem 목록 또는 상세를 조회한다.
func NVMeOfSubsystemList(context *gin.Context) {
	val, err := glueservice.NVMeOfSubsystemList(context.Request.Context(), context.Query("subsystem_nqn_id"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NVMeOfSubsystemCreate는 subsystem, listener, host allow를 생성한다.
func NVMeOfSubsystemCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.NVMeOfSubsystemCreate(
		context.Request.Context(),
		params["gateway_ip"],
		params["gateway_name"],
		params["subsystem_nqn_id"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NVMeOfSubsystemDelete는 subsystem을 삭제한다.
func NVMeOfSubsystemDelete(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.NVMeOfSubsystemDelete(context.Request.Context(), params["subsystem_nqn_id"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NVMeOfNamespaceList는 subsystem 지정 여부에 따라 namespace를 조회한다.
func NVMeOfNamespaceList(context *gin.Context) {
	val, err := glueservice.NVMeOfNamespaceList(context.Request.Context(), context.Query("subsystem_nqn_id"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NVMeOfNamespaceCreate는 필요 시 RBD image를 생성한 뒤 namespace를 추가한다.
func NVMeOfNamespaceCreate(context *gin.Context) {
	params := requestParams(context)
	size, err := parseOptionalSizeGiB(params["size"])
	if err != nil {
		badRequest(context, err)
		return
	}
	val, err := glueservice.NVMeOfNamespaceCreate(
		context.Request.Context(),
		params["subsystem_nqn_id"],
		params["pool_name"],
		params["image_name"],
		size,
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// NVMeOfNamespaceDelete는 namespace를 삭제하고, 요청 시 연결된 RBD image도 삭제한다.
func NVMeOfNamespaceDelete(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.NVMeOfNamespaceDelete(
		context.Request.Context(),
		params["subsystem_nqn_id"],
		params["namespace_uuid"],
		strings.EqualFold(params["image_del_check"], "true"),
		params["pool_name"],
		params["image_name"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// Glue-NVMeOFNVMeOfServiceDoc Swagger 문서
//
//	@Summary		NVMe-oF Service 생성
//	@Description	SCVM 로컬에서 pool을 초기화하고 ceph orch apply로 NVMe-oF service를 생성합니다.
//	@Tags			Glue-NVMeOF
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.NVMeOfServiceRequest	true	"NVMe-oF service request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/nvmeof [post]
func NVMeOfServiceDoc() {}

// Glue-NVMeOFNVMeOfImageDownloadDoc Swagger 문서
//
//	@Summary		NVMe-oF Image 다운로드
//	@Description	SCVM 로컬 podman에 NVMe-oF CLI image를 pull합니다. image를 생략하면 기본 local registry image를 사용합니다.
//	@Tags			Glue-NVMeOF
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.NVMeOfImageDownloadRequest	false	"NVMe-oF image request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/nvmeof/image/download [post]
func NVMeOfImageDownloadDoc() {}

// Glue-NVMeOFNVMeOfTargetListDoc Swagger 문서
//
//	@Summary		NVMe-oF Target 목록
//	@Description	SCVM 로컬 NVMe-oF daemon container의 SPDK RPC로 target 목록 또는 상세를 조회합니다.
//	@Tags			Glue-NVMeOF
//	@Produce		json
//	@Param			subsystem_nqn_id	query		string	false	"NVMe-oF subsystem NQN"
//	@Success		200					{object}	GlueModel.Response
//	@Failure		400					{object}	GlueModel.Response
//	@Failure		403					{object}	GlueModel.Response
//	@Failure		500					{object}	GlueModel.Response
//	@Router			/glue/nvmeof/target [get]
func NVMeOfTargetListDoc() {}

// Glue-NVMeOFNVMeOfTargetCreateDoc Swagger 문서
//
//	@Summary		NVMe-oF Target 생성
//	@Description	subsystem, listener, host allow, namespace를 순서대로 구성합니다. size가 있으면 RBD image도 생성합니다.
//	@Tags			Glue-NVMeOF
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.NVMeOfTargetRequest	true	"NVMe-oF target request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/nvmeof/target [post]
func NVMeOfTargetCreateDoc() {}

// Glue-NVMeOFNVMeOfSubsystemListDoc Swagger 문서
//
//	@Summary		NVMe-oF Subsystem 목록
//	@Description	SCVM 로컬 podman의 NVMe-oF CLI로 subsystem 목록 또는 상세를 조회합니다.
//	@Tags			Glue-NVMeOF
//	@Produce		json
//	@Param			subsystem_nqn_id	query		string	false	"NVMe-oF subsystem NQN"
//	@Success		200					{object}	GlueModel.Response
//	@Failure		400					{object}	GlueModel.Response
//	@Failure		403					{object}	GlueModel.Response
//	@Failure		500					{object}	GlueModel.Response
//	@Router			/glue/nvmeof/subsystem [get]
func NVMeOfSubsystemListDoc() {}

// Glue-NVMeOFNVMeOfSubsystemCreateDoc Swagger 문서
//
//	@Summary		NVMe-oF Subsystem 생성
//	@Description	subsystem을 생성하고 listener와 wildcard host allow를 추가합니다.
//	@Tags			Glue-NVMeOF
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.NVMeOfSubsystemRequest	true	"NVMe-oF subsystem request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/nvmeof/subsystem [post]
func NVMeOfSubsystemCreateDoc() {}

// Glue-NVMeOFNVMeOfSubsystemDeleteDoc Swagger 문서
//
//	@Summary		NVMe-oF Subsystem 삭제
//	@Description	지정한 subsystem을 삭제합니다.
//	@Tags			Glue-NVMeOF
//	@Produce		json
//	@Param			subsystem_nqn_id	query		string	true	"NVMe-oF subsystem NQN"
//	@Success		200					{object}	GlueModel.Response
//	@Failure		400					{object}	GlueModel.Response
//	@Failure		403					{object}	GlueModel.Response
//	@Failure		500					{object}	GlueModel.Response
//	@Router			/glue/nvmeof/subsystem [delete]
func NVMeOfSubsystemDeleteDoc() {}

// Glue-NVMeOFNVMeOfNamespaceListDoc Swagger 문서
//
//	@Summary		NVMe-oF Namespace 목록
//	@Description	SCVM 로컬 podman의 NVMe-oF CLI로 namespace 목록을 조회합니다.
//	@Tags			Glue-NVMeOF
//	@Produce		json
//	@Param			subsystem_nqn_id	query		string	false	"NVMe-oF subsystem NQN"
//	@Success		200					{object}	GlueModel.Response
//	@Failure		400					{object}	GlueModel.Response
//	@Failure		403					{object}	GlueModel.Response
//	@Failure		500					{object}	GlueModel.Response
//	@Router			/glue/nvmeof/namespace [get]
func NVMeOfNamespaceListDoc() {}

// Glue-NVMeOFNVMeOfNamespaceCreateDoc Swagger 문서
//
//	@Summary		NVMe-oF Namespace 생성
//	@Description	필요 시 RBD image를 생성한 뒤 subsystem에 namespace를 추가합니다.
//	@Tags			Glue-NVMeOF
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.NVMeOfNamespaceRequest	true	"NVMe-oF namespace request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/nvmeof/namespace [post]
func NVMeOfNamespaceCreateDoc() {}

// Glue-NVMeOFNVMeOfNamespaceDeleteDoc Swagger 문서
//
//	@Summary		NVMe-oF Namespace 삭제
//	@Description	namespace를 삭제하고, image_del_check=true이면 연결된 RBD image도 삭제합니다.
//	@Tags			Glue-NVMeOF
//	@Produce		json
//	@Param			subsystem_nqn_id	query		string	true	"NVMe-oF subsystem NQN"
//	@Param			namespace_uuid		query		string	true	"NVMe-oF namespace UUID"
//	@Param			image_del_check		query		string	false	"delete backing RBD image"	Enums(true, false)
//	@Param			pool_name			query		string	false	"pool name"
//	@Param			image_name			query		string	false	"image name"
//	@Success		200					{object}	GlueModel.Response
//	@Failure		400					{object}	GlueModel.Response
//	@Failure		403					{object}	GlueModel.Response
//	@Failure		500					{object}	GlueModel.Response
//	@Router			/glue/nvmeof/namespace [delete]
func NVMeOfNamespaceDeleteDoc() {}
