package cube

import (
	"fmt"
	"net/http"
	"strings"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type URLResponse = CubeModel.URLResponse

// GetURL godoc
//
//	@Summary		Get Connection URL
//	@Description	스토리지 및 클라우드 센터 연결 주소를 반환합니다.
//	@Tags			Cube-URL
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Param			option	query	string	false	"cloudCenter|wallCenter|storageCenter"
//	@Success		200		{object}	CubeModel.URLResponse
//	@Failure		400		{object}	HTTP400BadRequest
//	@Failure		500		{object}	HTTP500InternalServerError
//	@Router			/cube/url [get]
func GetURL(context *gin.Context) {
	cfg, err := loadClusterConfigSection()
	if err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}

	option := strings.TrimSpace(context.Query("option"))
	if option == "" {
		val := map[string]any{}
		if url, err := buildURLForAction("cloudCenter", cfg); err == nil {
			val["cloudCenter"] = url
		} else {
			val["cloudCenter"] = err.Error()
		}
		if url, err := buildURLForAction("wallCenter", cfg); err == nil {
			val["wallCenter"] = url
		} else {
			val["wallCenter"] = err.Error()
		}
		if isHCITarget(cfg.Type) {
			if url, err := buildURLForAction("storageCenter", cfg); err == nil {
				val["storageCenter"] = url
			} else {
				val["storageCenter"] = err.Error()
			}
		}
		scheduleURLHostScan("", isHCITarget(cfg.Type))
		context.JSON(http.StatusOK, URLResponse{Code: 200, Val: val})
		return
	}

	option = normalizeURLAction(option)
	if option == "" {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid option",
		})
		return
	}

	url, err := buildURLForAction(option, cfg)
	if err != nil {
		context.JSON(http.StatusInternalServerError, URLResponse{Code: 500, Val: err.Error()})
		return
	}

	scheduleURLHostScan(option, isHCITarget(cfg.Type))
	context.JSON(http.StatusOK, URLResponse{Code: 200, Val: map[string]string{option: url}})
}

// normalizeURLAction은 query option 문자열을 내부 표준 액션 이름으로 정규화한다.
func normalizeURLAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "cloudcenter":
		return "cloudCenter"
	case "wallcenter":
		return "wallCenter"
	case "storagecenter":
		return "storageCenter"
	default:
		return ""
	}
}

// buildURLForAction은 요청된 액션에 맞는 Cloud/Wall/Storage 접속 URL을 만든다.
func buildURLForAction(action string, cfg *CubeModel.ClusterConfigSection) (string, error) {
	ccvmIP := strings.TrimSpace(cfg.CCVM.IP)
	switch action {
	case "cloudCenter":
		if ccvmIP == "" {
			return "", fmt.Errorf("ccvm ip required")
		}
		return fmt.Sprintf("http://%s:8080", ccvmIP), nil
	case "wallCenter":
		if ccvmIP == "" {
			return "", fmt.Errorf("ccvm ip required")
		}
		return fmt.Sprintf("https://%s:8081/login", ccvmIP), nil
	case "storageCenter":
		if !isHCITarget(cfg.Type) {
			return "", fmt.Errorf("unsupported cluster type")
		}
		dashboardURL, err := CubeModel.GlueDashboardURL()
		if err != nil {
			return "", err
		}
		return strings.TrimRight(dashboardURL, "/"), nil
	default:
		return "", fmt.Errorf("invalid action")
	}
}

// scheduleURLHostScan은 URL 조회 후 관련 VM host에 대한 SSH known_hosts 갱신을 예약한다.
func scheduleURLHostScan(option string, isHCI bool) {
	var prefixes []string
	switch option {
	case "cloudCenter", "wallCenter":
		prefixes = []string{"ccvm"}
	case "storageCenter":
		prefixes = []string{"scvm"}
	case "":
		prefixes = []string{"ccvm"}
		if isHCI {
			prefixes = append(prefixes, "scvm")
		}
	}
	if len(prefixes) == 0 {
		return
	}
	includeAble := false
	includeScvm := false
	includeCcvm := false
	for _, prefix := range prefixes {
		switch prefix {
		case "scvm":
			includeScvm = true
		case "ccvm":
			includeCcvm = true
		case "able":
			includeAble = true
		}
	}
	hosts, err := collectSSHHostsFromClusterConfig(includeAble, includeScvm, includeCcvm)
	if err != nil || len(hosts) == 0 {
		return
	}
	scheduleSSHKnownHostsScanForHosts(hosts)
}
