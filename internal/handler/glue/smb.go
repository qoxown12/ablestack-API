package glue

import (
	GlueModel "ablecloud.io/ablestack-api/internal/model/glue"
	"ablecloud.io/ablestack-api/internal/service/glueservice"
	"github.com/gin-gonic/gin"
)

var _ = GlueModel.Response{}

// SMBStatus는 SCVM 로컬 SMB service 상태를 조회한다.
func SMBStatus(context *gin.Context) {
	val, err := glueservice.SMBStatus(context.Request.Context())
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// SMBCreate는 SMB service와 최초 share를 생성한다.
func SMBCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.SMBCreate(
		context.Request.Context(),
		params["sec_type"],
		params["cache_policy"],
		params["username"],
		params["password"],
		params["folder_name"],
		params["path"],
		params["fs_name"],
		params["volume_path"],
		params["realm"],
		params["dns"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// SMBDelete는 SMB service 전체 구성을 삭제한다.
func SMBDelete(context *gin.Context) {
	val, err := glueservice.SMBDelete(context.Request.Context())
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// SMBFolderAdd는 SMB share folder를 추가한다.
func SMBFolderAdd(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.SMBShareFolderAdd(
		context.Request.Context(),
		params["cache_policy"],
		params["folder_name"],
		params["path"],
		params["fs_name"],
		params["volume_path"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// SMBFolderDelete는 SMB share folder를 삭제한다.
func SMBFolderDelete(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.SMBShareFolderDelete(
		context.Request.Context(),
		params["folder_name"],
		params["path"],
		params["fs_name"],
	)
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// SMBUserCreate는 SMB user를 생성한다.
func SMBUserCreate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.SMBUserCreate(context.Request.Context(), params["username"], params["password"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// SMBUserUpdate는 SMB user password를 수정한다.
func SMBUserUpdate(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.SMBUserUpdate(context.Request.Context(), params["username"], params["password"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// SMBUserDelete는 SMB user를 삭제한다.
func SMBUserDelete(context *gin.Context) {
	params := requestParams(context)
	val, err := glueservice.SMBUserDelete(context.Request.Context(), params["username"])
	if err != nil {
		serviceError(context, err)
		return
	}
	ok(context, val)
}

// Glue-SMBSMBStatusDoc Swagger 문서
//
//	@Summary		SMB 상태
//	@Description	SCVM 로컬 Samba 실행 스크립트의 select 결과로 SMB 상태를 조회합니다. 기존 glue-api의 SSH host 반복은 사용하지 않습니다.
//	@Tags			Glue-SMB
//	@Produce		json
//	@Success		200	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/smb [get]
func SMBStatusDoc() {}

// Glue-SMBSMBCreateDoc Swagger 문서
//
//	@Summary		SMB 생성
//	@Description	SCVM 로컬에서 기존 SMB 구성을 정리한 뒤 SMB service와 최초 share를 생성합니다.
//	@Tags			Glue-SMB
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.SMBShareRequest	true	"SMB share request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/smb [post]
func SMBCreateDoc() {}

// Glue-SMBSMBDeleteDoc Swagger 문서
//
//	@Summary		SMB 삭제
//	@Description	SCVM 로컬 SMB service와 share 구성을 삭제합니다.
//	@Tags			Glue-SMB
//	@Produce		json
//	@Success		200	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/smb [delete]
func SMBDeleteDoc() {}

// Glue-SMBSMBFolderAddDoc Swagger 문서
//
//	@Summary		SMB Folder 추가
//	@Description	SCVM 로컬 SMB service에 share folder를 추가합니다.
//	@Tags			Glue-SMB
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.SMBFolderRequest	true	"SMB folder request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/smb/folder [post]
func SMBFolderAddDoc() {}

// Glue-SMBSMBFolderDeleteDoc Swagger 문서
//
//	@Summary		SMB Folder 삭제
//	@Description	SCVM 로컬 SMB share folder와 관련 mount 구성을 삭제합니다.
//	@Tags			Glue-SMB
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.SMBFolderDeleteRequest	true	"SMB folder delete request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/smb/folder [delete]
func SMBFolderDeleteDoc() {}

// Glue-SMBSMBUserCreateDoc Swagger 문서
//
//	@Summary		SMB User 생성
//	@Description	SCVM 로컬 SMB user를 생성합니다.
//	@Tags			Glue-SMB
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.SMBUserRequest	true	"SMB user request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/smb/user [post]
func SMBUserCreateDoc() {}

// Glue-SMBSMBUserUpdateDoc Swagger 문서
//
//	@Summary		SMB User 수정
//	@Description	SCVM 로컬 SMB user password를 수정합니다.
//	@Tags			Glue-SMB
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.SMBUserRequest	true	"SMB user request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/smb/user [put]
func SMBUserUpdateDoc() {}

// Glue-SMBSMBUserDeleteDoc Swagger 문서
//
//	@Summary		SMB User 삭제
//	@Description	SCVM 로컬 SMB user를 삭제합니다.
//	@Tags			Glue-SMB
//	@Accept			json
//	@Produce		json
//	@Param			body	body		GlueModel.SMBUserDeleteRequest	true	"SMB user delete request"
//	@Success		200	{object}	GlueModel.Response
//	@Failure		400	{object}	GlueModel.Response
//	@Failure		403	{object}	GlueModel.Response
//	@Failure		500	{object}	GlueModel.Response
//	@Router			/glue/smb/user [delete]
func SMBUserDeleteDoc() {}
