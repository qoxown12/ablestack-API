package main

import (
	"errors"
	"fmt"
	"net/http"

	C "ablecloud.io/ablestack-api/internal/service/controller"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	CubeHandler "ablecloud.io/ablestack-api/internal/handler/cube"
	"log"
	"time"

	"ablecloud.io/ablestack-api/docs"
	Auth "ablecloud.io/ablestack-api/internal/handler/auth"
	Dashboard "ablecloud.io/ablestack-api/internal/handler/dashboard"
	Glue "ablecloud.io/ablestack-api/internal/handler/glue"
	Mold "ablecloud.io/ablestack-api/internal/handler/mold"
	PCS "ablecloud.io/ablestack-api/internal/handler/pcs"
)

//	@title			Cube API
//	@version		1.0
//	@description	This is a Cube-API server.
//	@termsOfService	https://ablecloud.io/

//	@contact.name	API Support
//	@contact.url	https://www.ablecloud.io/support
//	@contact.email	ycyun@ablecloud.io

//	@license.name	Apache 2.0
//	@license.url	https://www.apache.org/licenses/LICENSE-2.0.html

//	@ssshost						10.211.55.11:8080
//	@BasePath					/api/v1
//	@Schemes					http https
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Enter the token with the `Bearer ` prefix, e.g. `Bearer eyJ...`
//	@Security					BearerAuth

// @externalDocs.description	ABLECLOUD
// @externalDocs.url			https://www.ablecloud.io
func main() {
	// 시간대 설정
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		panic(err)
	}
	// Set the timezone for the current process
	time.Local = location

	c := C.Init()
	c.LoadConfig()
	//c.StatusRegister(Mold.MonitorStatus)
	c.StatusRegister(Glue.Monitor)
	//c.StatusRegister(Dashboard.Monitor)
	c.StatusRegister(PCS.Monitor)
	c.StatusRegister(CubeHandler.UpdateHosts)
	c.StatusRegister(CubeHandler.UpdateClusterConfig)
	// Background daily ssh-keyscan based on cluster.json + systemProfile
	c.StatusRegister(CubeHandler.AutoSSHKnownHostsScan)
	c.StatusRegister(CubeHandler.AutoCCVMSnapshotBackup)
	c.StatusRegister(CubeHandler.AutoCCVMFileBackupSchedule)
	c.StatusRegister(CubeHandler.UpdateNICs)
	c.StatusRegister(CubeHandler.UpdateDisk)
	c.StatusRegister(C.SaveConfig)

	go c.Start()
	APIPort := "8090"
	docs.SwaggerInfo.Schemes = []string{"http", "https"}
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	//gin.SetMode(gin.DebugMode)
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.ForwardedByClientIP = true
	err = r.SetTrustedProxies(nil)
	if err != nil {
		c.AddError(err)
	}

	r.Use(gin.Logger())
	// Recovery 미들웨어는 panic이 발생하면 500 에러를 씁니다.
	r.Use(gin.Recovery())
	// CORS (Swagger/웹 클라이언트 대응)
	r.Use(func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.Writer.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		ctx.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Cube-Internal-Token")
		if ctx.Request.Method == http.MethodOptions {
			ctx.AbortWithStatus(http.StatusNoContent)
			return
		}
		ctx.Next()
	})
	r.Use(Auth.Middleware())

	v1 := r.Group("/api/v1")
	{
		auth := v1.Group("/auth")
		{
			auth.POST("/login", Auth.Login)
			auth.GET("/me", Auth.Me)
			auth.POST("/internal-token/rotate", Auth.RotateInternalToken)
			auth.POST("/internal-token/apply", Auth.ApplyInternalToken)
		}
		v1.GET("/neighbor", c.GetNeighbor)
		v1.GET("/neighbor/info", c.GetNeighborInfo)
		v1.POST("/neighbor", c.PutNeighbor)
		v1.PUT("/neighbor", c.PutNeighbor)
		v1.DELETE("/neighbor", c.DeleteNeighbor)
		cube := v1.Group("/cube")
		{
			cube.GET("/cluster/health", CubeHandler.ClusterHealth)
			cube.GET("/deploy/status", CubeHandler.GetDeployStatus)
			cube.GET("/hosts", CubeHandler.GetHosts)
			cube.GET("/test", CubeHandler.GetHosts)
			cube.GET("/cluster/config", CubeHandler.GetClusterConfig)
			cube.POST("/cluster/apply", CubeHandler.ApplyClusterConfig)
			cube.POST("/cluster/apply-local", CubeHandler.ApplyClusterConfigLocal)
			cube.POST("/cloudinit/status", CubeHandler.CloudInitStatus)
			cube.POST("/cloudinit/generate", CubeHandler.GenCloudInit)
			cube.POST("/cloudinit/ccvm/generate", CubeHandler.CreateCCVMCloudInit)
			cube.POST("/cloudinit/scvm/generate", CubeHandler.CreateSCVMCloudInit)
			cube.GET("/url", CubeHandler.GetURL)
			cube.GET("/system/config", CubeHandler.GetSystemConfig)
			cube.POST("/system/config", CubeHandler.UpdateSystemConfig)
			cube.GET("/ccvm/status", CubeHandler.GetCCVMStatus)
			cube.POST("/ccvm/edit", CubeHandler.EditCCVM)
			cube.POST("/ccvm/xml", CubeHandler.CreateCCVMXML)
			cube.POST("/ccvm/snap", CubeHandler.CCVMSnap)
			cube.POST("/ccvm/backup", CubeHandler.CCVMBackup)
			cube.POST("/ccvm/restore", CubeHandler.CCVMRestore)
			cube.POST("/ccvm/lifecycle", CubeHandler.CCVMLifecycle)
			cube.POST("/ccvm/secondary/resize", CubeHandler.CCVMSecondaryResize)
			cube.POST("/auto-shutdown", CubeHandler.AutoShutdown)
			cube.POST("/local/manage", CubeHandler.LocalManage)
			cube.POST("/clvm/manage", CubeHandler.CLVMManage)
			cube.POST("/hba/manage", CubeHandler.HBAManage)
			cube.GET("/gfs/disk/status", CubeHandler.GetGFSDiskStatus)
			cube.GET("/gfs/resource/status", CubeHandler.GetGFSResourceStatus)
			cube.POST("/gfs/manage", CubeHandler.GFSManage)
			cube.POST("/rbd/manage", CubeHandler.RBDManage)
			cube.GET("/gluecluster/status", CubeHandler.GetGlueClusterStatus)
			cube.POST("/gluecluster/update", CubeHandler.UpdateGlueCluster)
			cube.POST("/glue/config/update", CubeHandler.UpdateGlueConfig)
			cube.GET("/scvm/status", CubeHandler.GetSCVMStatus)
			cube.POST("/scvm/xml", CubeHandler.CreateSCVMXML)
			cube.POST("/scvm/lifecycle", CubeHandler.SCVMLifecycle)
			cube.POST("/pcs/control", CubeHandler.CCVMPCSControl)
			cube.POST("/ccvm/service/control", CubeHandler.CCVMServiceControl)
			cube.POST("/version/update", CubeHandler.VersionUpdate)
			cube.POST("/security/patch", CubeHandler.SecurityPatch)
			cube.POST("/license", CubeHandler.LicenseControl)
			cube.POST("/db/dump", CubeHandler.DBDump)
			cube.GET("/nics", CubeHandler.GetNICs)
			cube.GET("/disk", CubeHandler.GetDisk)
		}
		glue := v1.Group("/glue")
		{
			glue.GET("/", Glue.GetGlueStatus)
			glue.GET("/auth", Glue.GetGlueAuth)
			glue.GET("/auth/:username", Glue.GetGlueAuth)
			glue.GET("/auths", Glue.GetGlueAuths)
		}
		mold := v1.Group("/mold")
		{
			mold.GET("", Mold.GetStatus)
			mold.GET("/ccvm", Mold.GetCCVMInfo)
		}
		pcs := v1.Group("/pcs")
		{
			pcs.GET("", PCS.GetStatus)
			pcs.GET("/resources", PCS.GetResource)
		}
		dashboard := v1.Group("/dashboard")
		{
			dashboard.GET("", Dashboard.GetStatus)

		}
		//v1.Any("/version", Cube.Version)
		v1.GET("/err", c.Error)
		v1.DELETE("/err", c.DeleteError)
		v1.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}
	// Convenience: allow /swagger and /swagger/index.html without /api/v1 prefix.
	r.GET("/swagger", func(ctx *gin.Context) {
		ctx.Redirect(302, "/swagger/index.html")
	})
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	err = r.Run(":" + APIPort)
	if err != nil {
		c.AddError(err)
	}

	c.Stop()
	fmt.Println("end")
}

func errorMaker() {
	c := C.Init()
	c.AddError(errors.New(time.Now().String()))
}
