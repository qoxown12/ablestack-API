package glue

import (
	"strings"

	GlueModel "ablecloud.io/ablestack-api/internal/model/glue"
	"ablecloud.io/ablestack-api/internal/service/glueservice"
	"github.com/gin-gonic/gin"
)

var _ = GlueModel.Response{}

// RGWDaemon은 RGW service와 daemon 상태를 조회한다.
func RGWDaemon(context *gin.Context) {
	val, err := glueservice.RGWDaemons(context.Request.Context())
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// RGWUserList는 RGW user 정보를 조회한다.
func RGWUserList(context *gin.Context) {
	val, err := glueservice.RGWUsers(context.Request.Context(), context.Query("username"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// RGWBucketList는 RGW bucket 목록 또는 stats 정보를 조회한다.
func RGWBucketList(context *gin.Context) {
	val, err := glueservice.RGWBuckets(
		context.Request.Context(),
		context.Query("bucket_name"),
		strings.EqualFold(context.Query("detail"), "true"),
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// RGWServiceCreate는 RGW service를 생성한다.
func RGWServiceCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.RGWServiceCreateOrUpdate(
		context.Request.Context(),
		params["service_name"],
		params["realm_name"],
		params["zonegroup_name"],
		params["zone_name"],
		splitParamList(params["hosts"]),
		params["port"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// RGWServiceUpdate는 RGW service를 재적용한다.
func RGWServiceUpdate(context *gin.Context) {
	RGWServiceCreate(context)
}

// RGWQuota는 RGW quota를 설정한다.
func RGWQuota(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.RGWQuotaSet(context.Request.Context(), params["username"], params["scope"], params["max_objects"], params["max_size"], params["state"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// RGWUserCreate는 RGW user를 생성한다.
func RGWUserCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.RGWUserCreate(context.Request.Context(), params["username"], params["display_name"], params["email"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// RGWUserUpdate는 RGW user를 수정한다.
func RGWUserUpdate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.RGWUserUpdate(context.Request.Context(), params["username"], params["display_name"], params["email"], params["key_type"], params["access_key"], params["secret_key"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// RGWUserDelete는 RGW user를 삭제한다.
func RGWUserDelete(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.RGWUserDelete(context.Request.Context(), params["username"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// RGWBucketCreate는 RGW bucket을 생성한다.
func RGWBucketCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.RGWBucketCreate(context.Request.Context(), params["bucket_name"], params["username"], params["lock_enabled"], params["lock_mode"], params["lock_retention_period_days"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// RGWBucketUpdate는 RGW bucket을 수정한다.
func RGWBucketUpdate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.RGWBucketUpdate(context.Request.Context(), params["bucket_name"], params["bucket_id"], params["username"], params["versioning"], params["lock_mode"], params["lock_retention_period_days"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// RGWBucketDelete는 RGW bucket을 삭제한다.
func RGWBucketDelete(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.RGWBucketDelete(context.Request.Context(), params["bucket_name"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// Glue-RGWRGWDaemonDoc Swagger 문서
//
//	@Summary		RGW Daemon 상태
//	@Description	SCVM 로컬에서 ceph orch ls/ps 명령 기반으로 RGW service와 daemon 상태를 조회합니다.
//	@Tags			Glue-RGW
//	@Produce		json
//	@Success		200	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/rgw [get]
func RGWDaemonDoc() {}

// Glue-RGWRGWServiceDoc Swagger 문서
//
//	@Summary		RGW Service 변경
//	@Description	SCVM 로컬에서 radosgw-admin realm/zone 명령과 ceph orch apply rgw 명령으로 RGW service를 생성/수정합니다.
//	@Tags			Glue-RGW
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.RGWServiceRequest	true	"RGW service request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/rgw [post]
//	@Router			/glue/rgw [put]
func RGWServiceDoc() {}

// Glue-RGWRGWQuotaDoc Swagger 문서
//
//	@Summary		RGW Quota 수정
//	@Description	SCVM 로컬에서 radosgw-admin quota set/enable/disable 명령으로 quota를 설정합니다.
//	@Tags			Glue-RGW
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.RGWQuotaRequest	true	"RGW quota request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/rgw/quota [post]
func RGWQuotaDoc() {}

// Glue-RGWRGWUserListDoc Swagger 문서
//
//	@Summary		RGW User 목록
//	@Description	SCVM 로컬에서 radosgw-admin user list/info/stats 명령 기반으로 RGW user를 조회합니다.
//	@Tags			Glue-RGW
//	@Produce		json
//	@Param			username	query		string	false	"RGW user name"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/rgw/user [get]
func RGWUserListDoc() {}

// Glue-RGWRGWUserDoc Swagger 문서
//
//	@Summary		RGW User 변경
//	@Description	SCVM 로컬에서 radosgw-admin user create/modify/rm 명령으로 RGW user를 변경합니다.
//	@Tags			Glue-RGW
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.RGWUserRequest	true	"RGW user request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/rgw/user [post]
//	@Router			/glue/rgw/user [put]
//	@Router			/glue/rgw/user [delete]
func RGWUserDoc() {}

// Glue-RGWRGWBucketListDoc Swagger 문서
//
//	@Summary		RGW Bucket 목록
//	@Description	SCVM 로컬에서 radosgw-admin bucket list/stats 명령 기반으로 RGW bucket을 조회합니다.
//	@Tags			Glue-RGW
//	@Produce		json
//	@Param			bucket_name	query		string	false	"RGW bucket name"
//	@Param			detail		query		string	false	"show bucket stats"	Enums(true, false)
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/rgw/bucket [get]
func RGWBucketListDoc() {}

// Glue-RGWRGWBucketDoc Swagger 문서
//
//	@Summary		RGW Bucket 변경
//	@Description	Ceph dashboard API로 RGW bucket을 생성/수정하고, 삭제는 radosgw-admin bucket rm 명령으로 실행합니다.
//	@Tags			Glue-RGW
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.RGWBucketRequest	true	"RGW bucket request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/rgw/bucket [post]
//	@Router			/glue/rgw/bucket [put]
//	@Router			/glue/rgw/bucket [delete]
func RGWBucketDoc() {}
