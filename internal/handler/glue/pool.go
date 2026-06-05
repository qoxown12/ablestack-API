package glue

import (
	GlueModel "ablecloud.io/ablestack-api/internal/model/glue"
	"ablecloud.io/ablestack-api/internal/service/glueservice"
	"github.com/gin-gonic/gin"
)

var _ = GlueModel.Response{}

// PoolList는 pool 목록을 조회한다.
func PoolList(context *gin.Context) {
	val, err := glueservice.ListPools(context.Request.Context(), context.Query("pool_type"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// PoolDelete는 지정한 pool을 삭제한다.
func PoolDelete(context *gin.Context) {
	val, err := glueservice.DeletePool(context.Request.Context(), context.Param("pool_name"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// Glue-PoolPoolListDoc Swagger 문서
//
//	@Summary		Pool 목록
//	@Description	SCVM 로컬에서 ceph osd pool ls 명령 기반으로 pool 목록을 조회합니다.
//	@Tags			Glue-Pool
//	@Produce		json
//	@Param			pool_type	query		string	false	"pool type/name filter"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/pool [get]
func PoolListDoc() {}

// Glue-PoolPoolDeleteDoc Swagger 문서
//
//	@Summary		Pool 삭제
//	@Description	SCVM 로컬에서 ceph osd pool rm 명령 기반으로 pool을 삭제합니다.
//	@Tags			Glue-Pool
//	@Produce		json
//	@Param			pool_name	path		string	true	"pool name"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/pool/{pool_name} [delete]
func PoolDeleteDoc() {}
