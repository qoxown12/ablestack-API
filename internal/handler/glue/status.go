package glue

import (
	GlueModel "ablecloud.io/ablestack-api/internal/model/glue"
	"ablecloud.io/ablestack-api/internal/service/glueservice"
	"github.com/gin-gonic/gin"
)

// Status Swagger 문서
//
//	@Summary		Glue-Core API 상태
//	@Description	SCVM 전용 Glue-Core API 등록 상태와 현재 노드의 SCVM 판정 결과를 반환합니다.
//	@Tags			Glue-Core
//	@Produce		json
//	@Success		200	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Router			/glue [get]
func Status(context *gin.Context) {
	ok(context, GlueModel.RootStatus{
		Name:       "glue",
		SCVMOnly:   true,
		Node:       DetectNodeRole(),
		Endpoints:  endpointInfos(),
		Deprecated: deprecatedEndpointInfos(),
	})
}

// ClusterStatus는 SCVM 로컬 ceph status 조회 결과를 반환한다.
func ClusterStatus(context *gin.Context) {
	val, err := glueservice.Status(context.Request.Context())
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// Hosts는 Ceph orchestrator host 목록을 반환한다.
func Hosts(context *gin.Context) {
	val, err := glueservice.ListHosts(context.Request.Context())
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// Version은 Ceph daemon version 정보를 반환한다.
func Version(context *gin.Context) {
	val, err := glueservice.Versions(context.Request.Context())
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// Glue-CoreClusterStatusDoc Swagger 문서
//
//	@Summary		Cluster 상태
//	@Description	SCVM 로컬에서 ceph -s -f json 명령 기반으로 Glue-Core cluster 상태를 조회합니다.
//	@Tags			Glue-Core
//	@Produce		json
//	@Success		200	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/status [get]
func ClusterStatusDoc() {}

// Glue-CoreHostsDoc Swagger 문서
//
//	@Summary		Host 목록
//	@Description	SCVM 로컬에서 ceph orch host ls 명령 기반으로 Glue-Core host 목록을 조회합니다.
//	@Tags			Glue-Core
//	@Produce		json
//	@Success		200	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/hosts [get]
func HostsDoc() {}

// Glue-CoreVersionDoc Swagger 문서
//
//	@Summary		Daemon 버전
//	@Description	SCVM 로컬에서 ceph versions 명령 기반으로 Glue-Core daemon version을 조회합니다.
//	@Tags			Glue-Core
//	@Produce		json
//	@Success		200	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/version [get]
func VersionDoc() {}

// Glue-CorePasswordHelperDoc Swagger 문서
//
//	@Summary		Password Helper
//	@Description	Legacy password encryption helper API 골격입니다. 실제 사용 여부는 신규 Glue-Core 이식 범위에서 재검토합니다.
//	@Tags			Glue-Core
//	@Produce		json
//	@Success		501	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Router			/glue/pw [get]
func PasswordHelperDoc() {}
