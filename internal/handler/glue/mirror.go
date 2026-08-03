package glue

import (
	GlueModel "ablecloud.io/ablestack-api/internal/model/glue"
	"ablecloud.io/ablestack-api/internal/service/glueservice"
	"github.com/gin-gonic/gin"
)

var _ = GlueModel.Response{}

// MirrorStatus는 local RBD mirror 상태를 조회한다.
func MirrorStatus(context *gin.Context) {
	val, err := glueservice.MirrorStatus(context.Request.Context())
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// MirrorClusterSetup은 local RBD mirror pool을 설정하고 bootstrap token을 반환한다.
func MirrorClusterSetup(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.MirrorSetup(
		context.Request.Context(),
		firstNonEmpty(params["local_cluster_name"], params["localClusterName"]),
		firstNonEmpty(params["mirror_pool"], params["mirrorPool"]),
		firstNonEmpty(params["remote_token"], params["remoteToken"]),
		splitParamList(params["hosts"]),
		params["interval"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// MirrorClusterUpdate는 local mirror interval metadata를 수정한다.
func MirrorClusterUpdate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.MirrorUpdate(
		context.Request.Context(),
		firstNonEmpty(params["mirror_pool"], params["mirrorPool"]),
		params["interval"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// MirrorClusterDelete는 local RBD mirror 설정과 metadata image를 정리한다.
func MirrorClusterDelete(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.MirrorDelete(context.Request.Context(), firstNonEmpty(params["mirror_pool"], params["mirrorPool"]))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// MirrorPoolEnable은 path pool에 대해 local mirroring을 활성화한다.
func MirrorPoolEnable(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.MirrorPoolEnable(
		context.Request.Context(),
		context.Param("mirrorPool"),
		firstNonEmpty(params["local_cluster_name"], params["localClusterName"]),
		firstNonEmpty(params["remote_token"], params["remoteToken"]),
		splitParamList(params["hosts"]),
		params["interval"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// MirrorPoolDisable은 path pool의 local mirroring을 비활성화한다.
func MirrorPoolDisable(context *gin.Context) {
	val, err := glueservice.MirrorPoolDisable(context.Request.Context(), context.Param("mirrorPool"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// MirrorGarbageDelete는 local mirror 잔여 설정을 정리한다.
func MirrorGarbageDelete(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.MirrorGarbageDelete(context.Request.Context(), firstNonEmpty(params["mirror_pool"], params["mirrorPool"], "rbd"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// MirrorImageList는 pool의 mirrored image 상태를 조회한다.
func MirrorImageList(context *gin.Context) {
	val, err := glueservice.MirrorImageList(context.Request.Context(), context.Param("mirrorPool"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// MirrorImageInfo는 단일 mirrored image 상태를 조회한다.
func MirrorImageInfo(context *gin.Context) {
	val, err := glueservice.MirrorImageInfo(context.Request.Context(), context.Param("mirrorPool"), context.Param("imageName"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// Glue-MirrorMirrorStatusDoc Swagger 문서
//
//	@Summary		Mirror 상태
//	@Description	SCVM 로컬에서 rbd mirror pool status 명령 기반으로 mirror 상태를 조회합니다.
//	@Tags			Glue-Mirror
//	@Produce		json
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/mirror [get]
func MirrorStatusDoc() {}

// Glue-MirrorMirrorClusterSetupDoc Swagger 문서
//
//	@Summary		Mirror Cluster 설정
//	@Description	SCVM 로컬에서 RBD mirror pool을 활성화하고 bootstrap token을 생성합니다. remote_token이 있으면 local pool에 peer로 import합니다.
//	@Tags			Glue-Mirror
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.MirrorClusterRequest	true	"mirror cluster setup request"
//	@Success		200		{object}	GlueModel.Response
//	@Failure		400		{object}	GlueModel.Response
//	@Failure		403		{object}	GlueModel.Response
//	@Failure		500		{object}	GlueModel.Response
//	@Router			/glue/mirror [post]
func MirrorClusterSetupDoc() {}

// Glue-MirrorMirrorClusterUpdateDoc Swagger 문서
//
//	@Summary		Mirror Cluster 수정
//	@Description	SCVM 로컬에서 rbd/MOLD-DR metadata image의 mirror interval을 수정합니다.
//	@Tags			Glue-Mirror
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.MirrorClusterRequest	true	"mirror cluster update request"
//	@Success		200		{object}	GlueModel.Response
//	@Failure		400		{object}	GlueModel.Response
//	@Failure		403		{object}	GlueModel.Response
//	@Failure		500		{object}	GlueModel.Response
//	@Router			/glue/mirror [put]
func MirrorClusterUpdateDoc() {}

// Glue-MirrorMirrorClusterDeleteDoc Swagger 문서
//
//	@Summary		Mirror Cluster 삭제
//	@Description	SCVM 로컬에서 mirror peer, pool mirroring, rbd-mirror service, metadata image를 정리합니다. remote cluster는 해당 SCVM에서 같은 API를 호출해야 합니다.
//	@Tags			Glue-Mirror
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.MirrorClusterRequest	true	"mirror cluster delete request"
//	@Success		200		{object}	GlueModel.Response
//	@Failure		400		{object}	GlueModel.Response
//	@Failure		403		{object}	GlueModel.Response
//	@Failure		500		{object}	GlueModel.Response
//	@Router			/glue/mirror [delete]
func MirrorClusterDeleteDoc() {}

// Glue-MirrorMirrorPoolEnableDoc Swagger 문서
//
//	@Summary		Mirror Pool 활성화
//	@Description	SCVM 로컬에서 path pool의 RBD mirroring을 활성화하고 bootstrap token을 반환합니다.
//	@Tags			Glue-Mirror
//	@Accept			json
//	@Produce		json
//	@Param			mirrorPool	path		string						true	"mirror pool"
//	@Param			body		body		GlueModel.MirrorPoolRequest	true	"mirror pool enable request"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/mirror/{mirrorPool} [post]
func MirrorPoolEnableDoc() {}

// Glue-MirrorMirrorPoolDisableDoc Swagger 문서
//
//	@Summary		Mirror Pool 비활성화
//	@Description	SCVM 로컬에서 path pool의 mirror peer와 image mirroring을 정리하고 pool mirroring을 비활성화합니다.
//	@Tags			Glue-Mirror
//	@Produce		json
//	@Param			mirrorPool	path		string	true	"mirror pool"
//	@Success		200		{object}	GlueModel.Response
//	@Failure		400		{object}	GlueModel.Response
//	@Failure		403		{object}	GlueModel.Response
//	@Failure		500		{object}	GlueModel.Response
//	@Router			/glue/mirror/{mirrorPool} [delete]
func MirrorPoolDisableDoc() {}

// Glue-MirrorMirrorGarbageDeleteDoc Swagger 문서
//
//	@Summary		Mirror Garbage 삭제
//	@Description	SCVM 로컬에서 mirror 잔여 peer/auth/service/metadata image를 정리합니다. mirror_pool query가 없으면 rbd pool을 대상으로 사용합니다.
//	@Tags			Glue-Mirror
//	@Produce		json
//	@Param			mirror_pool	query		string	false	"mirror pool"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/mirror/garbage [delete]
func MirrorGarbageDeleteDoc() {}

// Glue-MirrorMirrorImageListDoc Swagger 문서
//
//	@Summary		Mirror Image 목록
//	@Description	SCVM 로컬에서 rbd mirror pool status --verbose 명령 기반으로 mirrored image 목록을 조회합니다.
//	@Tags			Glue-Mirror
//	@Produce		json
//	@Param			mirrorPool	path		string	true	"mirror pool"
//	@Success		200		{object}	GlueModel.Response
//	@Failure		400		{object}	GlueModel.Response
//	@Failure		403		{object}	GlueModel.Response
//	@Failure		500		{object}	GlueModel.Response
//	@Router			/glue/mirror/image/{mirrorPool} [get]
func MirrorImageListDoc() {}

// Glue-MirrorMirrorImageInfoDoc Swagger 문서
//
//	@Summary		Mirror Image 상세
//	@Description	SCVM 로컬에서 rbd mirror image status 명령 기반으로 mirrored image 상세 상태를 조회합니다.
//	@Tags			Glue-Mirror
//	@Produce		json
//	@Param			mirrorPool	path		string	true	"mirror pool"
//	@Param			imageName	path		string	true	"image name"
//	@Success		200		{object}	GlueModel.Response
//	@Failure		400		{object}	GlueModel.Response
//	@Failure		403		{object}	GlueModel.Response
//	@Failure		500		{object}	GlueModel.Response
//	@Router			/glue/mirror/image/{mirrorPool}/{imageName} [get]
func MirrorImageInfoDoc() {}
