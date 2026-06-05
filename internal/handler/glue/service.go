package glue

import (
	GlueModel "ablecloud.io/ablestack-api/internal/model/glue"
	"ablecloud.io/ablestack-api/internal/service/glueservice"
	"github.com/gin-gonic/gin"
)

var _ = GlueModel.Response{}

// ServiceList는 Ceph orchestrator service 목록을 조회한다.
func ServiceList(context *gin.Context) {
	val, err := glueservice.ListServices(
		context.Request.Context(),
		context.Query("service_type"),
		context.Query("service_name"),
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// ServiceControl은 ceph orch 기반 service 제어 명령을 실행한다.
func ServiceControl(context *gin.Context) {
	params := requestParams(context)
	control := firstNonEmpty(params["control"], context.Query("control"))
	val, err := glueservice.ControlService(
		context.Request.Context(),
		context.Param("service_name"),
		control,
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// ServiceDelete는 Ceph orchestrator service를 삭제한다.
func ServiceDelete(context *gin.Context) {
	val, err := glueservice.DeleteService(context.Request.Context(), context.Param("service_name"))
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// Glue-ServiceServiceListDoc Swagger 문서
//
//	@Summary		Service 목록
//	@Description	SCVM 로컬에서 ceph orch ls 명령 기반으로 orchestrator service를 조회합니다.
//	@Tags			Glue-Service
//	@Produce		json
//	@Param			service_name	query		string	false	"service name"
//	@Param			service_type	query		string	false	"service type"
//	@Success		200				{object}	GlueModel.Response
//	@Failure		400				{object}	GlueModel.Response
//	@Failure		403				{object}	GlueModel.Response
//	@Failure		500				{object}	GlueModel.Response
//	@Router			/glue/service [get]
func ServiceListDoc() {}

// Glue-ServiceServiceControlDoc Swagger 문서
//
//	@Summary		Service 제어
//	@Description	SCVM 로컬에서 ceph orch start/stop/restart/redeploy 명령 기반으로 orchestrator service를 제어합니다.
//	@Tags			Glue-Service
//	@Accept			json
//	@Produce		json
//	@Param			service_name	path		string								true	"service name"
//	@Param			control		query		string								false	"service control"	Enums(start, stop, restart, redeploy)
//	@Param			body		body		GlueModel.ServiceControlRequest	false	"service control request"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/service/{service_name} [post]
func ServiceControlDoc() {}

// Glue-ServiceServiceDeleteDoc Swagger 문서
//
//	@Summary		Service 삭제
//	@Description	SCVM 로컬에서 ceph orch rm 명령 기반으로 orchestrator service를 삭제합니다.
//	@Tags			Glue-Service
//	@Produce		json
//	@Param			service_name	path		string	true	"service name"
//	@Success		200			{object}	GlueModel.Response
//	@Failure		400			{object}	GlueModel.Response
//	@Failure		403			{object}	GlueModel.Response
//	@Failure		500			{object}	GlueModel.Response
//	@Router			/glue/service/{service_name} [delete]
func ServiceDeleteDoc() {}
