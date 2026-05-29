package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/service/authservice"
	"ablecloud.io/ablestack-api/internal/service/security"
	"github.com/gin-gonic/gin"
)

type LoginRequest struct {
	ID       string `json:"id" example:"root"`
	Password string `json:"password" example:"password"`
}

type LoginResponse struct {
	Code          int    `json:"code" example:"200"`
	TokenType     string `json:"token_type" example:"Bearer"`
	AccessToken   string `json:"access_token"`
	Authorization string `json:"authorization" example:"Bearer eyJ..."`
	ExpiresIn     int64  `json:"expires_in" example:"3600"`
}

type MeResponse struct {
	Code int                    `json:"code" example:"200"`
	Val  authservice.TokenClaim `json:"val"`
}

type InternalTokenRotateResponse struct {
	Code    int                   `json:"code" example:"200"`
	Val     map[string]any        `json:"val"`
	Results []InternalTokenResult `json:"results,omitempty"`
	Message string                `json:"message,omitempty"`
}

type InternalTokenApplyRequest struct {
	InternalToken string `json:"internal_token"`
}

type AuthApplyRequest struct {
	Token             string `json:"token"`
	AccessTokenSecret string `json:"access_token_secret,omitempty" swaggerignore:"true"`
	InternalToken     string `json:"internal_token,omitempty" swaggerignore:"true"`
}

func (r AuthApplyRequest) applyToken() string {
	return firstNonEmpty(r.Token, r.AccessTokenSecret)
}

type AuthSyncResponse struct {
	Code    int              `json:"code" example:"200"`
	Val     map[string]any   `json:"val"`
	Results []AuthSyncResult `json:"results,omitempty"`
	Message string           `json:"message,omitempty"`
}

type AuthSyncRequest struct {
	Option string `json:"option,omitempty" example:"all" enums:"host,scvm,ccvm,all"`
}

type InternalTokenResult struct {
	Target  string `json:"target"`
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type AuthSyncResult struct {
	Role     string `json:"role,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	Target   string `json:"target"`
	Code     int    `json:"code"`
	Message  string `json:"message,omitempty"`
}

// Login godoc
//
//	@Summary		Login
//	@Description	웹 UI/API용 access token을 발급합니다.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		LoginRequest	true	"login request"
//	@Success		200	{object}	LoginResponse
//	@Failure		401	{object}	map[string]any
//	@Router			/auth/login [post]
func Login(context *gin.Context) {
	var req LoginRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid request"})
		return
	}
	if !authservice.VerifyLinuxCredentials(req.ID, req.Password) {
		context.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "invalid credentials"})
		return
	}

	token, err := authservice.IssueAccessToken(strings.TrimSpace(req.ID))
	if err != nil {
		context.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
		return
	}
	context.JSON(http.StatusOK, LoginResponse{
		Code:          http.StatusOK,
		TokenType:     token.TokenType,
		AccessToken:   token.AccessToken,
		Authorization: token.Authorization,
		ExpiresIn:     token.ExpiresIn,
	})
}

// Me godoc
//
//	@Summary		Current User
//	@Description	Bearer token을 검증하고 현재 token claim을 반환합니다.
//	@Tags			Auth
//	@Produce		json
//	@Success		200	{object}	MeResponse
//	@Failure		401	{object}	map[string]any
//	@Router			/auth/me [get]
func Me(context *gin.Context) {
	claim, err := authservice.ClaimsFromRequest(context.Request)
	if err != nil {
		context.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": err.Error()})
		return
	}
	context.JSON(http.StatusOK, MeResponse{Code: http.StatusOK, Val: claim})
}

func RotateInternalToken(context *gin.Context) {
	if _, err := authservice.ClaimsFromRequest(context.Request); err != nil {
		context.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": err.Error()})
		return
	}
	if strings.TrimSpace(os.Getenv("CUBE_INTERNAL_TOKEN")) != "" || strings.TrimSpace(os.Getenv("ABLESTACK_INTERNAL_TOKEN")) != "" {
		context.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "internal token is managed by environment"})
		return
	}

	oldToken, _, err := security.EnsureInternalToken()
	if err != nil {
		context.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
		return
	}
	newToken, err := security.GenerateToken()
	if err != nil {
		context.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
		return
	}

	targets, err := security.ClusterAblecubeTargets()
	if err != nil {
		context.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
		return
	}
	results := applyInternalTokenToTargets(targets, oldToken, newToken)
	for _, result := range results {
		if result.Code >= 300 {
			context.JSON(http.StatusInternalServerError, InternalTokenRotateResponse{
				Code:    http.StatusInternalServerError,
				Message: "internal token rotation failed",
				Results: results,
			})
			return
		}
	}

	if err := security.SetInternalToken(newToken); err != nil {
		context.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
		return
	}
	context.JSON(http.StatusOK, InternalTokenRotateResponse{
		Code: http.StatusOK,
		Val: map[string]any{
			"message":    "internal token rotated",
			"token":      security.MaskToken(newToken),
			"target_cnt": len(results),
		},
		Results: results,
	})
}

func ApplyInternalToken(context *gin.Context) {
	if !security.ValidateInternalToken(context.GetHeader(security.InternalTokenHeader)) {
		context.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "invalid internal token"})
		return
	}
	var req InternalTokenApplyRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid request"})
		return
	}
	if strings.TrimSpace(req.InternalToken) == "" {
		context.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "internal_token required"})
		return
	}
	if err := security.SetInternalToken(req.InternalToken); err != nil {
		context.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
		return
	}
	context.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "val": "internal token applied"})
}

// SyncAuth godoc
//
//	@Summary		Sync Auth
//	@Description	현재 호스트의 API 인증 값을 선택한 대상(host/scvm/ccvm/all) API 서버에 동기화합니다. host는 hosts[].ablecube, scvm은 hosts[].scvm, ccvm은 ccvm.ip를 사용합니다. ablestack-vm/ablestack-standalone의 all은 host,ccvm만 포함합니다.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		AuthSyncRequest	false	"sync request"
//	@Success		200	{object}	AuthSyncResponse
//	@Failure		400	{object}	map[string]any
//	@Failure		401	{object}	map[string]any
//	@Failure		500	{object}	map[string]any
//	@Router			/auth/sync [post]
func SyncAuth(context *gin.Context) {
	if _, err := authservice.ClaimsFromRequest(context.Request); err != nil {
		context.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": err.Error()})
		return
	}
	req, err := bindAuthSyncRequest(context)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid request"})
		return
	}
	option := firstNonEmpty(context.Query("option"), req.Option, "host")

	accessTokenSecret, err := authservice.ExistingSigningSecret()
	if err != nil {
		context.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "auth token is not initialized"})
		return
	}
	internalToken, _, err := security.EnsureInternalToken()
	if err != nil {
		context.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
		return
	}
	targets, err := security.ClusterAuthSyncTargets(option)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	if len(targets) == 0 {
		context.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "no auth sync targets"})
		return
	}

	results := applyAccessTokenSecretToTargets(targets, internalToken, accessTokenSecret)
	successCnt, failCnt := countAuthSyncResults(results)
	for _, result := range results {
		if result.Code >= 300 {
			context.JSON(http.StatusInternalServerError, AuthSyncResponse{
				Code:    http.StatusInternalServerError,
				Val:     authSyncResponseVal("auth sync failed", option, accessTokenSecret, len(results), successCnt, failCnt),
				Message: "auth sync failed",
				Results: results,
			})
			return
		}
	}

	context.JSON(http.StatusOK, AuthSyncResponse{
		Code:    http.StatusOK,
		Val:     authSyncResponseVal("auth synced", option, accessTokenSecret, len(results), successCnt, failCnt),
		Results: results,
	})
}

// ApplyAuth godoc
//
//	@Summary		Apply Auth
//	@Description	내부 호출로 전달받은 API 인증 값을 현재 호스트에 적용합니다.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		AuthApplyRequest	true	"auth apply request"
//	@Success		200	{object}	map[string]any
//	@Failure		401	{object}	map[string]any
//	@Failure		409	{object}	map[string]any
//	@Router			/auth/apply [post]
func ApplyAuth(context *gin.Context) {
	var req AuthApplyRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid request"})
		return
	}
	headerToken := context.GetHeader(security.InternalTokenHeader)
	if !security.ValidateInternalToken(headerToken) {
		if !allowAuthApplySync(headerToken, req.InternalToken) {
			context.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "invalid internal token"})
			return
		}
		if err := security.SetInternalToken(req.InternalToken); err != nil {
			context.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
			return
		}
	}
	token := strings.TrimSpace(req.applyToken())
	if token == "" {
		context.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "token required"})
		return
	}
	if err := authservice.SetAccessTokenSecret(token); err != nil {
		status := http.StatusInternalServerError
		if authservice.AccessTokenSecretManagedByEnv() {
			status = http.StatusConflict
		}
		context.JSON(status, gin.H{"code": status, "message": "auth apply failed"})
		return
	}
	context.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"val": map[string]any{
			"message": "auth applied",
			"token":   security.MaskToken(token),
		},
	})
}

func Middleware() gin.HandlerFunc {
	return func(context *gin.Context) {
		if context.Request.Method == http.MethodOptions || isPublicPath(context.Request.URL.Path) {
			context.Next()
			return
		}

		if hasCubeInternalHeader(context.Request.Header) {
			if !security.ValidateInternalToken(context.GetHeader(security.InternalTokenHeader)) {
				if allowClusterApplyLocalBootstrap(context) || allowAuthApplyRequestSync(context) {
					context.Next()
					return
				}
				context.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "invalid internal token"})
				return
			}
			context.Next()
			return
		}

		if !authservice.AuthRequired() {
			context.Next()
			return
		}
		if _, err := authservice.ClaimsFromRequest(context.Request); err != nil {
			context.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": err.Error()})
			return
		}
		context.Next()
	}
}

func allowClusterApplyLocalBootstrap(context *gin.Context) bool {
	if context.Request.Method != http.MethodPost || context.Request.URL.Path != "/api/v1/cube/cluster/apply-local" {
		return false
	}
	currentToken, err := security.GetInternalToken()
	if err != nil || strings.TrimSpace(currentToken) != "" {
		return false
	}
	headerToken := strings.TrimSpace(context.GetHeader(security.InternalTokenHeader))
	if len(headerToken) < 16 {
		return false
	}
	body, err := io.ReadAll(context.Request.Body)
	if err != nil {
		return false
	}
	context.Request.Body = io.NopCloser(bytes.NewReader(body))

	var payload struct {
		Action   string `json:"action"`
		Security struct {
			InternalToken string `json:"internal_token"`
		} `json:"security"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Action), "insert") {
		return false
	}
	return strings.TrimSpace(payload.Security.InternalToken) == headerToken
}

func allowAuthApplyRequestSync(context *gin.Context) bool {
	if context.Request.Method != http.MethodPost || context.Request.URL.Path != "/api/v1/auth/apply" {
		return false
	}
	headerToken := strings.TrimSpace(context.GetHeader(security.InternalTokenHeader))
	if len(headerToken) < 16 {
		return false
	}
	body, err := io.ReadAll(context.Request.Body)
	if err != nil {
		return false
	}
	context.Request.Body = io.NopCloser(bytes.NewReader(body))

	var payload AuthApplyRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return strings.TrimSpace(payload.InternalToken) == headerToken
}

func isPublicPath(path string) bool {
	return path == "/api/v1/auth/login" ||
		strings.HasPrefix(path, "/api/v1/swagger") ||
		strings.HasPrefix(path, "/swagger")
}

func hasCubeInternalHeader(header http.Header) bool {
	for key, values := range header {
		if strings.EqualFold(key, security.InternalTokenHeader) {
			return true
		}
		if strings.HasPrefix(strings.ToLower(key), "x-cube-") && len(values) > 0 {
			for _, value := range values {
				if strings.TrimSpace(value) != "" {
					return true
				}
			}
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func allowAuthApplySync(headerToken string, bodyToken string) bool {
	headerToken = strings.TrimSpace(headerToken)
	bodyToken = strings.TrimSpace(bodyToken)
	if len(headerToken) < 16 || headerToken != bodyToken {
		return false
	}
	return true
}

func bindAuthSyncRequest(context *gin.Context) (AuthSyncRequest, error) {
	var req AuthSyncRequest
	body, err := io.ReadAll(context.Request.Body)
	if err != nil {
		return req, err
	}
	context.Request.Body = io.NopCloser(bytes.NewReader(body))
	if len(bytes.TrimSpace(body)) == 0 {
		return req, nil
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return req, err
	}
	return req, nil
}

func authSyncResponseVal(message string, option string, token string, targetCnt int, successCnt int, failCnt int) map[string]any {
	return map[string]any{
		"message":     message,
		"option":      option,
		"token":       security.MaskToken(token),
		"target_cnt":  targetCnt,
		"success_cnt": successCnt,
		"fail_cnt":    failCnt,
	}
}

func countAuthSyncResults(results []AuthSyncResult) (int, int) {
	successCnt := 0
	failCnt := 0
	for _, result := range results {
		if result.Code >= 200 && result.Code < 300 {
			successCnt++
			continue
		}
		failCnt++
	}
	return successCnt, failCnt
}

func applyInternalTokenToTargets(targets []string, oldToken string, newToken string) []InternalTokenResult {
	results := make([]InternalTokenResult, 0, len(targets))
	for _, target := range targets {
		if security.IsLocalTarget(target) {
			continue
		}
		result := applyInternalTokenToTarget(target, oldToken, newToken)
		results = append(results, result)
	}
	return results
}

func applyInternalTokenToTarget(target string, oldToken string, newToken string) InternalTokenResult {
	body, _ := json.Marshal(InternalTokenApplyRequest{InternalToken: newToken})
	url := fmt.Sprintf("%s/api/v1/auth/internal-token/apply", buildTargetURL(target))
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return InternalTokenResult{Target: target, Code: http.StatusInternalServerError, Message: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(security.InternalTokenHeader, oldToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return InternalTokenResult{Target: target, Code: http.StatusInternalServerError, Message: err.Error()}
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return InternalTokenResult{Target: target, Code: resp.StatusCode, Message: strings.TrimSpace(string(raw))}
	}
	return InternalTokenResult{Target: target, Code: resp.StatusCode}
}

func applyAccessTokenSecretToTargets(targets []security.ClusterSyncTarget, internalToken string, accessTokenSecret string) []AuthSyncResult {
	results := make([]AuthSyncResult, 0, len(targets))
	for _, target := range targets {
		if security.IsLocalTarget(target.Target) {
			results = append(results, authSyncResult(target, http.StatusOK, "local target"))
			continue
		}
		result := applyAccessTokenSecretToTarget(target, internalToken, accessTokenSecret)
		results = append(results, result)
	}
	return results
}

func applyAccessTokenSecretToTarget(target security.ClusterSyncTarget, internalToken string, accessTokenSecret string) AuthSyncResult {
	body, _ := json.Marshal(AuthApplyRequest{Token: accessTokenSecret, InternalToken: internalToken})
	url := fmt.Sprintf("%s/api/v1/auth/apply", buildTargetURL(target.Target))
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return authSyncResult(target, http.StatusInternalServerError, err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(security.InternalTokenHeader, internalToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return authSyncResult(target, http.StatusInternalServerError, err.Error())
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return authSyncResult(target, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return authSyncResult(target, resp.StatusCode, "")
}

func authSyncResult(target security.ClusterSyncTarget, code int, message string) AuthSyncResult {
	return AuthSyncResult{
		Role:     target.Role,
		Hostname: target.Hostname,
		Target:   target.Target,
		Code:     code,
		Message:  strings.TrimSpace(message),
	}
}

func buildTargetURL(target string) string {
	scheme := firstNonEmpty(os.Getenv("ABLESTACK_API_SCHEME"), "http")
	port := firstNonEmpty(os.Getenv("ABLESTACK_API_PORT"), "8090")
	return fmt.Sprintf("%s://%s:%s", scheme, strings.TrimSpace(target), port)
}
