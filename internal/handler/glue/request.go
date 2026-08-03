package glue

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"ablecloud.io/ablestack-api/internal/service/glueservice"
	"github.com/gin-gonic/gin"
)

// requestParams는 query, form, JSON payload를 하나의 map으로 정규화한다.
// 기존 glue-api 방식의 form 호출과 신규 JSON 호출이 같은 handler를 사용할 수 있게 한다.
func requestParams(context *gin.Context) map[string]string {
	params := map[string]string{}
	for key, values := range context.Request.URL.Query() {
		if len(values) > 0 {
			params[key] = joinParamValues(values)
		}
	}

	if err := context.Request.ParseForm(); err == nil {
		for key, values := range context.Request.PostForm {
			if len(values) > 0 {
				params[key] = joinParamValues(values)
			}
		}
	}

	if strings.Contains(strings.ToLower(context.GetHeader("Content-Type")), "application/json") && context.Request.Body != nil {
		var body map[string]any
		if err := json.NewDecoder(context.Request.Body).Decode(&body); err == nil {
			for key, value := range body {
				params[key] = formatParamValue(value)
			}
		}
	}
	return params
}

func parseSizeGiB(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("size is required")
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("size must be an integer")
	}
	if value <= 0 {
		return 0, fmt.Errorf("size must be greater than zero")
	}
	return value, nil
}

func parseOptionalSizeGiB(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	return parseSizeGiB(raw)
}

func splitParamList(raw string) []string {
	items := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\t'
	})
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func joinParamValues(values []string) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return strings.Join(out, ",")
}

func formatParamValue(value any) string {
	switch v := value.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			itemValue := strings.TrimSpace(fmt.Sprint(item))
			if itemValue != "" {
				out = append(out, itemValue)
			}
		}
		return strings.Join(out, ",")
	case []string:
		return joinParamValues(v)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

// badRequest는 Ceph/RBD 명령 실행 전에 발생한 로컬 입력 검증 실패를 반환한다.
func badRequest(context *gin.Context, err error) {
	context.JSON(http.StatusBadRequest, Response{
		Code:    http.StatusBadRequest,
		Message: err.Error(),
	})
}

// serviceError는 클라이언트 입력 오류와 SCVM 명령 실행 실패를 구분해 응답한다.
func serviceError(context *gin.Context, err error) {
	var commandErr glueservice.CommandError
	if errors.As(err, &commandErr) {
		context.JSON(http.StatusInternalServerError, Response{
			Code:    http.StatusInternalServerError,
			Message: commandErr.Error(),
		})
		return
	}

	switch {
	case strings.Contains(err.Error(), "required"),
		strings.Contains(err.Error(), "invalid "),
		strings.Contains(err.Error(), "must be "):
		badRequest(context, err)
	default:
		context.JSON(http.StatusInternalServerError, Response{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		})
	}
}
