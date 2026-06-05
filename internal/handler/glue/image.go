package glue

import (
	GlueModel "ablecloud.io/ablestack-api/internal/model/glue"
	"ablecloud.io/ablestack-api/internal/service/glueservice"
	"github.com/gin-gonic/gin"
)

var _ = GlueModel.Response{}

// ImageList는 RBD image 목록 또는 상세 정보를 조회한다.
func ImageList(context *gin.Context) {
	val, err := glueservice.ListImages(
		context.Request.Context(),
		context.Query("pool_name"),
		context.Query("image_name"),
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// ImageCreate는 JSON, form, query 입력을 받아 RBD image를 생성한다.
func ImageCreate(context *gin.Context) {
	params := requestParams(context)
	size, err := parseSizeGiB(params["size"])
	if err != nil {
		badRequest(context, err)
		return
	}
	val, err := glueservice.CreateImage(
		context.Request.Context(),
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

// ImageDelete는 JSON, form, query 입력을 받아 RBD image를 삭제한다.
func ImageDelete(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.DeleteImage(
		context.Request.Context(),
		params["pool_name"],
		params["image_name"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// Glue-ImageImageListDoc Swagger 문서
//
//	@Summary		RBD Image 목록
//	@Description	SCVM 로컬에서 rbd ls/info 명령 기반으로 image 목록 또는 상세 정보를 조회합니다.
//	@Tags			Glue-Image
//	@Produce		json
//	@Param			pool_name	query		string	false	"pool name"
//	@Param			image_name	query		string	false	"image name"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/image [get]
func ImageListDoc() {}

// Glue-ImageImageCreateDoc Swagger 문서
//
//	@Summary		RBD Image 생성
//	@Description	SCVM 로컬에서 rbd create 명령 기반으로 image를 생성합니다. size는 GiB 단위입니다.
//	@Tags			Glue-Image
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.ImageRequest	true	"image create request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/image [post]
func ImageCreateDoc() {}

// Glue-ImageImageDeleteDoc Swagger 문서
//
//	@Summary		RBD Image 삭제
//	@Description	SCVM 로컬에서 rbd rm 명령 기반으로 image를 삭제합니다.
//	@Tags			Glue-Image
//	@Produce		json
//	@Param			pool_name	query		string	true	"pool name"
//	@Param			image_name	query		string	true	"image name"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/image [delete]
func ImageDeleteDoc() {}
