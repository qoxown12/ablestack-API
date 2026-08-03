package glue

import (
	GlueModel "ablecloud.io/ablestack-api/internal/model/glue"
	"ablecloud.io/ablestack-api/internal/service/glueservice"
	"github.com/gin-gonic/gin"
)

var _ = GlueModel.Response{}

// GlueFSStatus는 GlueFS 상태와 filesystem 목록을 조회한다.
func GlueFSStatus(context *gin.Context) {
	val, err := glueservice.GlueFSStatus(context.Request.Context())
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// GlueFSInfo는 특정 filesystem 상세 정보를 조회한다.
func GlueFSInfo(context *gin.Context) {
	val, err := glueservice.GlueFSInfo(context.Request.Context(), context.Param("fs_name"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// GlueFSCreate는 GlueFS filesystem을 생성한다.
func GlueFSCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.GlueFSCreate(context.Request.Context(), context.Param("fs_name"), splitParamList(params["hosts"]))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// GlueFSUpdate는 GlueFS filesystem 이름과 MDS placement를 수정한다.
func GlueFSUpdate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.GlueFSUpdate(context.Request.Context(), params["old_name"], params["new_name"], splitParamList(params["hosts"]))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// GlueFSDelete는 GlueFS filesystem을 삭제한다.
func GlueFSDelete(context *gin.Context) {
	val, err := glueservice.GlueFSDelete(context.Request.Context(), context.Param("fs_name"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// GlueFSSubvolumeGroupList는 subvolume group 목록과 상세 정보를 조회한다.
func GlueFSSubvolumeGroupList(context *gin.Context) {
	val, err := glueservice.GlueFSSubvolumeGroups(context.Request.Context(), context.Query("vol_name"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// GlueFSSubvolumeGroupCreate는 subvolume group을 생성한다.
func GlueFSSubvolumeGroupCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.GlueFSSubvolumeGroupCreate(
		context.Request.Context(),
		params["vol_name"],
		params["group_name"],
		params["size"],
		params["data_pool_name"],
		params["mode"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// GlueFSSubvolumeGroupDelete는 subvolume group을 삭제한다.
func GlueFSSubvolumeGroupDelete(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.GlueFSSubvolumeGroupDelete(context.Request.Context(), params["vol_name"], params["group_name"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// GlueFSSubvolumeGroupResize는 subvolume group quota를 확장한다.
func GlueFSSubvolumeGroupResize(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.GlueFSSubvolumeGroupResize(context.Request.Context(), params["vol_name"], params["group_name"], params["new_size"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// GlueFSStatusDoc Swagger 문서
//
//	@Summary		GlueFS 상태
//	@Description	SCVM 로컬에서 ceph fs status, ceph fs ls 명령 기반으로 GlueFS 상태와 목록을 조회합니다.
//	@Tags			Glue-GlueFS
//	@Produce		json
//	@Success		200	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/gluefs [get]
func GlueFSStatusDoc() {}

// GlueFSPlacementUpdateDoc Swagger 문서
//
//	@Summary		GlueFS 배치 수정
//	@Description	SCVM 로컬에서 ceph fs rename과 MDS placement 적용 명령으로 GlueFS를 수정합니다.
//	@Tags			Glue-GlueFS
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.GlueFSPlacementRequest	true	"gluefs placement request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/gluefs [put]
func GlueFSPlacementUpdateDoc() {}

// GlueFSCreateDoc Swagger 문서
//
//	@Summary		GlueFS 생성
//	@Description	SCVM 로컬에서 ceph fs volume create와 pool rename/set 명령으로 GlueFS를 생성합니다.
//	@Tags			Glue-GlueFS
//	@Accept			json
//	@Produce		json
//	@Param			fs_name	path		string							true	"filesystem name"
//	@Param			body	body		GlueModel.GlueFSPlacementRequest	true	"gluefs create request"
//	@Success		200		{object}	GlueModel.Response
//	@Failure		400		{object}	GlueModel.Response
//	@Failure		403		{object}	GlueModel.Response
//	@Failure		500		{object}	GlueModel.Response
//	@Router			/glue/gluefs/{fs_name} [post]
func GlueFSCreateDoc() {}

// GlueFSDeleteDoc Swagger 문서
//
//	@Summary		GlueFS 삭제
//	@Description	하위 subvolume group이 없을 때 SCVM 로컬 ceph fs volume rm 명령으로 GlueFS를 삭제합니다.
//	@Tags			Glue-GlueFS
//	@Produce		json
//	@Param			fs_name	path		string	true	"filesystem name"
//	@Success		200		{object}	GlueModel.Response
//	@Failure		400		{object}	GlueModel.Response
//	@Failure		403		{object}	GlueModel.Response
//	@Failure		500		{object}	GlueModel.Response
//	@Router			/glue/gluefs/{fs_name} [delete]
func GlueFSDeleteDoc() {}

// GlueFSInfoDoc Swagger 문서
//
//	@Summary		GlueFS 상세
//	@Description	SCVM 로컬에서 ceph fs get 명령 기반으로 GlueFS 상세 정보를 조회합니다.
//	@Tags			Glue-GlueFS
//	@Produce		json
//	@Param			fs_name	path		string	true	"filesystem name"
//	@Success		200		{object}	GlueModel.Response
//	@Failure		400		{object}	GlueModel.Response
//	@Failure		403		{object}	GlueModel.Response
//	@Failure		500		{object}	GlueModel.Response
//	@Router			/glue/gluefs/info/{fs_name} [get]
func GlueFSInfoDoc() {}

// GlueFSSubvolumeGroupListDoc Swagger 문서
//
//	@Summary		Subvolume Group 목록
//	@Description	SCVM 로컬에서 ceph fs subvolumegroup ls/info/getpath/snapshot ls 명령 기반으로 subvolume group을 조회합니다.
//	@Tags			Glue-GlueFS
//	@Produce		json
//	@Param			vol_name	query		string	true	"filesystem volume name"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/gluefs/subvolume/group [get]
func GlueFSSubvolumeGroupListDoc() {}

// GlueFSSubvolumeGroupCreateDoc Swagger 문서
//
//	@Summary		Subvolume Group 생성
//	@Description	SCVM 로컬에서 ceph fs subvolumegroup create 명령으로 subvolume group을 생성합니다.
//	@Tags			Glue-GlueFS
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.GlueFSSubvolumeGroupRequest	true	"subvolume group create request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/gluefs/subvolume/group [post]
func GlueFSSubvolumeGroupCreateDoc() {}

// GlueFSSubvolumeGroupDeleteDoc Swagger 문서
//
//	@Summary		Subvolume Group 삭제
//	@Description	SCVM 로컬에서 ceph fs subvolumegroup rm 명령으로 subvolume group을 삭제합니다.
//	@Tags			Glue-GlueFS
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.GlueFSSubvolumeGroupRequest	true	"subvolume group delete request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/gluefs/subvolume/group [delete]
func GlueFSSubvolumeGroupDeleteDoc() {}

// GlueFSSubvolumeGroupResizeDoc Swagger 문서
//
//	@Summary		Subvolume Group 크기 수정
//	@Description	SCVM 로컬에서 ceph fs subvolumegroup resize --no_shrink 명령으로 quota를 확장합니다.
//	@Tags			Glue-GlueFS
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.GlueFSSubvolumeGroupRequest	true	"subvolume group resize request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/gluefs/subvolume/group [put]
func GlueFSSubvolumeGroupResizeDoc() {}
