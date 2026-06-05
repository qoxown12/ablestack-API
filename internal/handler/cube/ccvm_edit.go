package cube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

type CCVMEditRequest = CubeModel.CCVMEditRequest
type CCVMEditResult = CubeModel.CCVMEditResult
type CCVMEditResponse = CubeModel.CCVMEditResponse

const (
	ccvmEditLocalHeader    = "X-Cube-CCVM-Edit-Local"
	ccvmXMLPathVM          = "/mnt/glue-gfs/ccvm.xml"
	ccvmXMLPathStandalone  = "/mnt/glue/ccvm.xml"
	ccvmEditRequestTimeout = 10 * time.Second
	ccvmEditSuccessMessage = "ccvm xml updated"
	ccvmEditFailureMessage = "ccvm xml update failed"
	ccvmEditSuccessStatus  = "ok"
)

// EditCCVM godoc
//
//	@Summary		CCVM XML Edit
//	@Description	CCVM XML의 vCPU와 메모리 값을 수정합니다.
//	@Tags			Cube-CCVM
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.CCVMEditRequest	true	"ccvm edit request"
//	@Success		200	{object}	CubeModel.CCVMEditResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/ccvm/edit [post]
func EditCCVM(context *gin.Context) {
	var req CCVMEditRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := normalizeCCVMEditRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	cfg, err := loadClusterConfigSection()
	if err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}

	if isCCVMEditLocalRequest(context) {
		result := runCCVMEditLocal(cfg, req)
		statusCode, response := buildCCVMEditResponse([]CCVMEditResult{result})
		context.JSON(statusCode, response)
		return
	}

	if isCCVMEditFanoutType(cfg.Type) {
		results, err := fanoutCCVMEdit(cfg, req)
		if err != nil {
			context.JSON(http.StatusInternalServerError, CCVMEditResponse{
				Code:    500,
				Message: err.Error(),
			})
			return
		}
		statusCode, response := buildCCVMEditResponse(results)
		context.JSON(statusCode, response)
		return
	}

	target := resolveSingleCCVMEditTarget(cfg)
	if strings.TrimSpace(target) == "" {
		context.JSON(http.StatusInternalServerError, CCVMEditResponse{
			Code:    500,
			Message: "ablecube host not found",
		})
		return
	}

	var result CCVMEditResult
	if isLocalTarget(target) {
		result = runCCVMEditLocal(cfg, req)
	} else {
		result, err = callCCVMEditRemote(target, req)
		if err != nil {
			result = CCVMEditResult{
				Target:  target,
				Code:    500,
				Message: err.Error(),
			}
		}
	}

	statusCode, response := buildCCVMEditResponse([]CCVMEditResult{result})
	context.JSON(statusCode, response)
}

// normalizeCCVMEditRequest는 CPU/메모리 요청값을 양의 정수 문자열로 정규화한다.
func normalizeCCVMEditRequest(req *CCVMEditRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	cpu, err := parsePositiveIntegerString(req.CPU)
	if err != nil {
		return fmt.Errorf("invalid cpu")
	}
	memory, err := parsePositiveIntegerString(req.Memory)
	if err != nil {
		return fmt.Errorf("invalid memory")
	}
	req.CPU = cpu
	req.Memory = memory
	return nil
}

// parsePositiveIntegerString은 입력 문자열을 양의 정수인지 검증하고 정규화해 반환한다.
func parsePositiveIntegerString(value string) (string, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return "", fmt.Errorf("invalid positive integer")
	}
	return strconv.Itoa(parsed), nil
}

// isCCVMEditLocalRequest는 현재 요청이 원격 fan-out 이후 로컬 실행 전용 요청인지 확인한다.
func isCCVMEditLocalRequest(context *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(context.GetHeader(ccvmEditLocalHeader)), "1")
}

// isCCVMEditFanoutType는 모든 host에 XML 수정을 전파해야 하는 클러스터 타입인지 판별한다.
func isCCVMEditFanoutType(clusterType string) bool {
	switch strings.ToLower(strings.TrimSpace(clusterType)) {
	case "ablestack-hci", "ablestack-hci-filesystem":
		return true
	default:
		return false
	}
}

// resolveCCVMXMLPath는 클러스터 타입에 맞는 CCVM XML 파일 경로를 결정한다.
func resolveCCVMXMLPath(clusterType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(clusterType)) {
	case "ablestack-vm":
		return ccvmXMLPathVM, nil
	case "ablestack-standalone":
		return ccvmXMLPathStandalone, nil
	case "ablestack-hci", "ablestack-hci-filesystem":
		return filepath.Join(resolveAbleStackVMConfigDir("ccvm"), "ccvm.xml"), nil
	default:
		return "", fmt.Errorf("unsupported cluster type")
	}
}

// resolveSingleCCVMEditTarget는 VM/standalone 환경에서 단일 수정 대상으로 사용할 ablecube host를 고른다.
func resolveSingleCCVMEditTarget(cfg *CubeModel.ClusterConfigSection) string {
	if cfg == nil {
		return ""
	}
	for _, host := range cfg.Hosts {
		target := strings.TrimSpace(host.Ablecube)
		if target == "" {
			continue
		}
		if isLocalTarget(target) {
			return target
		}
	}
	for _, host := range cfg.Hosts {
		target := strings.TrimSpace(host.Ablecube)
		if target != "" {
			return target
		}
	}
	return ""
}

// resolveLocalCCVMEditTarget는 현재 노드가 어떤 ablecube host인지 식별해 결과값에 사용할 target을 만든다.
func resolveLocalCCVMEditTarget(cfg *CubeModel.ClusterConfigSection) string {
	if cfg != nil {
		for _, host := range cfg.Hosts {
			target := strings.TrimSpace(host.Ablecube)
			if target == "" {
				continue
			}
			if isLocalTarget(target) {
				return target
			}
		}
	}
	name, err := os.Hostname()
	if err == nil && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return "local"
}

// buildCCVMEditTargets는 HCI 계열에서 수정 요청을 전파할 ablecube host 목록을 만든다.
func buildCCVMEditTargets(cfg *CubeModel.ClusterConfigSection) []string {
	if cfg == nil {
		return nil
	}
	out := make([]string, 0, len(cfg.Hosts))
	seen := map[string]struct{}{}
	for _, host := range cfg.Hosts {
		target := strings.TrimSpace(host.Ablecube)
		if target == "" {
			continue
		}
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	return out
}

// runCCVMEditLocal은 현재 노드에서 XML 파일을 직접 수정하고 결과 구조체를 만든다.
func runCCVMEditLocal(cfg *CubeModel.ClusterConfigSection, req CCVMEditRequest) CCVMEditResult {
	target := resolveLocalCCVMEditTarget(cfg)
	clusterType := ""
	if cfg != nil {
		clusterType = strings.TrimSpace(cfg.Type)
	}
	xmlPath, err := applyCCVMEditLocal(clusterType, req.CPU, req.Memory)
	if err != nil {
		return CCVMEditResult{
			Target:  target,
			Code:    500,
			Message: err.Error(),
		}
	}
	return CCVMEditResult{
		Target:  target,
		Code:    200,
		Message: ccvmEditSuccessStatus,
		XMLPath: xmlPath,
	}
}

// applyCCVMEditLocal은 현재 노드의 CCVM XML 파일을 읽고 CPU/메모리 값을 수정해 저장한다.
func applyCCVMEditLocal(clusterType string, cpu string, memory string) (string, error) {
	xmlPath, err := resolveCCVMXMLPath(clusterType)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(xmlPath)
	if err != nil {
		return "", err
	}

	updated, err := updateCCVMXMLContent(string(content), cpu, memory)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(xmlPath)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(xmlPath, []byte(updated), info.Mode().Perm()); err != nil {
		return "", err
	}

	return xmlPath, nil
}

// updateCCVMXMLContent는 원본 XML의 형식을 최대한 유지하면서 필요한 태그 값만 부분 수정한다.
func updateCCVMXMLContent(content string, cpu string, memory string) (string, error) {
	updated, err := replaceCCVMXMLTagContent(content, "vcpu", cpu, "")
	if err != nil {
		return "", err
	}
	updated, err = replaceCCVMXMLTagContent(updated, "memory", memory, "GiB")
	if err != nil {
		return "", err
	}
	updated, err = replaceCCVMXMLTagContent(updated, "currentMemory", memory, "GiB")
	if err != nil {
		return "", err
	}
	return updated, nil
}

// replaceCCVMXMLTagContent는 지정한 태그의 시작/종료 태그는 유지하고 본문 값만 교체한다.
func replaceCCVMXMLTagContent(content string, tagName string, value string, unit string) (string, error) {
	tagExpr := regexp.MustCompile(`(?s)<` + regexp.QuoteMeta(tagName) + `\b[^>]*>.*?</` + regexp.QuoteMeta(tagName) + `>`)
	loc := tagExpr.FindStringIndex(content)
	if loc == nil {
		return "", fmt.Errorf("%s tag not found", tagName)
	}

	tagBlock := content[loc[0]:loc[1]]
	startEnd := strings.Index(tagBlock, ">")
	endStart := strings.LastIndex(tagBlock, "</")
	if startEnd < 0 || endStart < 0 || endStart <= startEnd {
		return "", fmt.Errorf("%s tag malformed", tagName)
	}

	startTag := tagBlock[:startEnd+1]
	endTag := tagBlock[endStart:]
	if unit != "" {
		startTag = upsertXMLTagAttribute(startTag, "unit", unit)
	}

	replacement := startTag + value + endTag
	return content[:loc[0]] + replacement + content[loc[1]:], nil
}

// upsertXMLTagAttribute는 시작 태그 문자열 안에서 속성을 추가하거나 기존 값을 교체한다.
func upsertXMLTagAttribute(startTag string, attrName string, attrValue string) string {
	attrExpr := regexp.MustCompile(`\b` + regexp.QuoteMeta(attrName) + `\s*=\s*("[^"]*"|'[^']*')`)
	attrLoc := attrExpr.FindStringSubmatchIndex(startTag)
	if attrLoc == nil {
		insertPos := strings.LastIndex(startTag, ">")
		if insertPos < 0 {
			return startTag
		}
		return startTag[:insertPos] + fmt.Sprintf(` %s="%s"`, attrName, attrValue) + startTag[insertPos:]
	}

	quoted := startTag[attrLoc[2]:attrLoc[3]]
	quote := `"`
	if strings.HasPrefix(quoted, `'`) {
		quote = `'`
	}
	replacement := fmt.Sprintf(`%s=%s%s%s`, attrName, quote, attrValue, quote)
	return startTag[:attrLoc[0]] + replacement + startTag[attrLoc[1]:]
}

// fanoutCCVMEdit는 HCI 계열에서 모든 ablecube host에 수정 요청을 전파한다.
func fanoutCCVMEdit(cfg *CubeModel.ClusterConfigSection, req CCVMEditRequest) ([]CCVMEditResult, error) {
	targets := buildCCVMEditTargets(cfg)
	if len(targets) == 0 {
		return nil, fmt.Errorf("ablecube host not found")
	}

	results := make([]CCVMEditResult, 0, len(targets))
	for _, target := range targets {
		if isLocalTarget(target) {
			results = append(results, runCCVMEditLocal(cfg, req))
			continue
		}
		result, err := callCCVMEditRemote(target, req)
		if err != nil {
			results = append(results, CCVMEditResult{
				Target:  target,
				Code:    500,
				Message: err.Error(),
			})
			continue
		}
		results = append(results, result)
	}

	return results, nil
}

// callCCVMEditRemote는 원격 ablecube host의 CCVM 수정 API를 로컬 실행 헤더와 함께 호출한다.
func callCCVMEditRemote(target string, req CCVMEditRequest) (CCVMEditResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return CCVMEditResult{}, err
	}

	baseURL := buildTargetURL(target)
	url := fmt.Sprintf("%s/api/v1/cube/ccvm/edit", baseURL)
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return CCVMEditResult{}, err
	}
	attachInternalToken(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(ccvmEditLocalHeader, "1")

	client := &http.Client{Timeout: ccvmEditRequestTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return CCVMEditResult{}, err
	}
	defer resp.Body.Close()

	var out CCVMEditResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CCVMEditResult{}, err
	}
	if out.Code == 0 {
		out.Code = resp.StatusCode
	}
	if strings.TrimSpace(out.Message) == "" {
		out.Message = resp.Status
	}

	if len(out.Results) > 0 {
		result := out.Results[0]
		if strings.TrimSpace(result.Target) == "" {
			result.Target = target
		}
		if result.Code == 0 {
			result.Code = out.Code
		}
		if strings.TrimSpace(result.Message) == "" {
			result.Message = out.Message
		}
		return result, nil
	}

	return CCVMEditResult{
		Target:  target,
		Code:    out.Code,
		Message: out.Message,
	}, nil
}

// buildCCVMEditResponse는 호스트별 결과를 바탕으로 전체 HTTP 응답 코드와 본문을 만든다.
func buildCCVMEditResponse(results []CCVMEditResult) (int, CCVMEditResponse) {
	hasFail := false
	for _, result := range results {
		if result.Code != 200 {
			hasFail = true
			break
		}
	}

	response := CCVMEditResponse{
		Code:    200,
		Message: ccvmEditSuccessMessage,
		Results: results,
	}
	if hasFail {
		response.Code = 500
		response.Message = ccvmEditFailureMessage
		return http.StatusInternalServerError, response
	}
	return http.StatusOK, response
}
