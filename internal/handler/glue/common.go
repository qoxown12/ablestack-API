package glue

import (
	"net/http"
	"strings"

	GlueModel "ablecloud.io/ablestack-api/internal/model/glue"
	"github.com/gin-gonic/gin"
)

const (
	glueBasePath       = "/api/v1/glue"
	glueSkeletonStatus = "skeleton"
)

type Response = GlueModel.Response
type NodeRoleStatus = GlueModel.NodeRoleStatus
type EndpointInfo = GlueModel.EndpointInfo
type RootStatus = GlueModel.RootStatus
type NotImplementedValue = GlueModel.NotImplementedValue
type GenericRequest = GlueModel.GenericRequest
type ImageRequest = GlueModel.ImageRequest
type ServiceControlRequest = GlueModel.ServiceControlRequest

type routeSpec struct {
	Method      string
	Path        string
	Module      string
	Endpoint    string
	Description string
	Status      string
}

func ok(context *gin.Context, val any) {
	context.JSON(http.StatusOK, Response{Code: http.StatusOK, Message: "ok", Val: val})
}

func notImplemented(context *gin.Context, module string, endpoint string) {
	context.JSON(http.StatusNotImplemented, Response{
		Code:    http.StatusNotImplemented,
		Message: "glue api skeleton only; implementation pending",
		Val: NotImplementedValue{
			Method:   context.Request.Method,
			Path:     context.FullPath(),
			Module:   module,
			Endpoint: endpoint,
			Note:     "legacy glue-api behavior will be ported without SSH after validation",
		},
	})
}

func notImplementedHandler(module string, endpoint string) gin.HandlerFunc {
	return func(context *gin.Context) {
		notImplemented(context, module, endpoint)
	}
}

func endpointInfos() []EndpointInfo {
	out := make([]EndpointInfo, 0, len(glueRoutes))
	for _, route := range glueRoutes {
		if strings.EqualFold(route.Status, "deprecated") {
			continue
		}
		out = append(out, EndpointInfo{
			Method:      route.Method,
			Path:        glueBasePath + route.Path,
			Description: route.Description,
			Status:      firstNonEmpty(route.Status, glueSkeletonStatus),
		})
	}
	return out
}

func deprecatedEndpointInfos() []EndpointInfo {
	out := make([]EndpointInfo, 0)
	for _, route := range glueRoutes {
		if !strings.EqualFold(route.Status, "deprecated") {
			continue
		}
		out = append(out, EndpointInfo{
			Method:      route.Method,
			Path:        route.Path,
			Description: route.Description,
			Status:      route.Status,
		})
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
