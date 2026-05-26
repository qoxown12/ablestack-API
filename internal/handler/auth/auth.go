package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/service/security"
	"github.com/gin-gonic/gin"
)

const (
	defaultAccessTokenTTL = time.Hour
	tokenTypeBearer       = "Bearer"
	tokenUseAccess        = "access"
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
	Code int        `json:"code" example:"200"`
	Val  tokenClaim `json:"val"`
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

type InternalTokenResult struct {
	Target  string `json:"target"`
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type authConfig struct {
	Linux                 authLinuxConfig `json:"linux"`
	AccessTokenTTLSeconds int64           `json:"access_token_ttl_seconds"`
	AccessTokenSecret     string          `json:"access_token_secret"`
}

type authLinuxConfig struct {
	AllowedUsers  []string `json:"allowed_users"`
	AllowedGroups []string `json:"allowed_groups"`
}

type tokenHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type tokenClaim struct {
	Subject string `json:"sub"`
	Role    string `json:"role"`
	Use     string `json:"use"`
	Issued  int64  `json:"iat"`
	Expires int64  `json:"exp"`
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
	if !verifyCredentials(req.ID, req.Password) {
		context.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "invalid credentials"})
		return
	}

	secret, err := signingSecret()
	if err != nil {
		context.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
		return
	}
	ttl := accessTokenTTL()
	now := time.Now()
	claim := tokenClaim{
		Subject: strings.TrimSpace(req.ID),
		Role:    "admin",
		Use:     tokenUseAccess,
		Issued:  now.Unix(),
		Expires: now.Add(ttl).Unix(),
	}
	token, err := signToken(claim, secret)
	if err != nil {
		context.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
		return
	}
	context.JSON(http.StatusOK, LoginResponse{
		Code:          http.StatusOK,
		TokenType:     tokenTypeBearer,
		AccessToken:   token,
		Authorization: tokenTypeBearer + " " + token,
		ExpiresIn:     int64(ttl.Seconds()),
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
	claim, err := ClaimsFromRequest(context.Request)
	if err != nil {
		context.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": err.Error()})
		return
	}
	context.JSON(http.StatusOK, MeResponse{Code: http.StatusOK, Val: claim})
}

func RotateInternalToken(context *gin.Context) {
	if _, err := ClaimsFromRequest(context.Request); err != nil {
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

func Middleware() gin.HandlerFunc {
	return func(context *gin.Context) {
		if context.Request.Method == http.MethodOptions || isPublicPath(context.Request.URL.Path) {
			context.Next()
			return
		}

		if hasCubeInternalHeader(context.Request.Header) {
			if !security.ValidateInternalToken(context.GetHeader(security.InternalTokenHeader)) {
				context.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "invalid internal token"})
				return
			}
			context.Next()
			return
		}

		if !authRequired() {
			context.Next()
			return
		}
		if _, err := ClaimsFromRequest(context.Request); err != nil {
			context.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": err.Error()})
			return
		}
		context.Next()
	}
}

func ClaimsFromRequest(req *http.Request) (tokenClaim, error) {
	authHeader := strings.TrimSpace(req.Header.Get("Authorization"))
	if authHeader == "" {
		return tokenClaim{}, fmt.Errorf("authorization header required")
	}
	parts := strings.Fields(authHeader)
	if len(parts) != 2 || !strings.EqualFold(parts[0], tokenTypeBearer) {
		return tokenClaim{}, fmt.Errorf("bearer token required")
	}
	secret, err := signingSecret()
	if err != nil {
		return tokenClaim{}, err
	}
	return verifyToken(parts[1], secret)
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

func authRequired() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ABLESTACK_AUTH_REQUIRED"))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return authConfigured()
}

func authConfigured() bool {
	return true
}

func verifyCredentials(username string, password string) bool {
	return verifyLinuxCredentials(username, password)
}

func verifyLinuxCredentials(username string, password string) bool {
	username = strings.TrimSpace(username)
	if username == "" || password == "" || !isSafeLinuxUsername(username) {
		return false
	}
	cfg := loadAuthConfig()
	if !linuxAccountAllowed(username, cfg.Linux) {
		return false
	}
	return verifyLinuxShadowPassword(username, password)
}

func isSafeLinuxUsername(username string) bool {
	if username == "" || len(username) > 128 {
		return false
	}
	for _, r := range username {
		if r == 0 || r == ':' || r == '/' || r == '\\' || r <= 31 || r == 127 {
			return false
		}
	}
	return true
}

func linuxAccountAllowed(username string, cfg authLinuxConfig) bool {
	if len(cfg.AllowedUsers) == 0 && len(cfg.AllowedGroups) == 0 {
		return true
	}
	for _, allowed := range cfg.AllowedUsers {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" || allowed == username {
			return true
		}
	}
	if len(cfg.AllowedGroups) == 0 {
		return false
	}
	groups := linuxUserGroups(username)
	if len(groups) == 0 {
		return false
	}
	allowedGroups := map[string]struct{}{}
	for _, group := range cfg.AllowedGroups {
		group = strings.TrimSpace(group)
		if group != "" {
			allowedGroups[group] = struct{}{}
		}
	}
	for _, group := range groups {
		if _, ok := allowedGroups[group]; ok {
			return true
		}
	}
	return false
}

func linuxUserGroups(username string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "id", "-nG", username).Output()
	if err != nil || ctx.Err() != nil {
		return nil
	}
	fields := strings.Fields(string(out))
	groups := make([]string, 0, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(field) != "" {
			groups = append(groups, strings.TrimSpace(field))
		}
	}
	return groups
}

func verifyLinuxShadowPassword(username string, password string) bool {
	payload, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		return false
	}
	const script = `
import crypt
import hmac
import json
import spwd
import sys

try:
    req = json.load(sys.stdin)
    username = req.get("username", "")
    password = req.get("password", "")
    entry = spwd.getspnam(username)
    hashed = entry.sp_pwdp or ""
    if not hashed or hashed[0] in ("!", "*"):
        sys.exit(1)
    candidate = crypt.crypt(password, hashed)
    if candidate and hmac.compare_digest(candidate, hashed):
        sys.exit(0)
    sys.exit(1)
except Exception:
    sys.exit(1)
`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	cmd.Stdin = bytes.NewReader(payload)
	if err := cmd.Run(); err != nil {
		return false
	}
	return ctx.Err() == nil
}

func accessTokenTTL() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("ABLESTACK_ACCESS_TOKEN_TTL_SECONDS")); raw != "" {
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	cfg := loadAuthConfig()
	if cfg.AccessTokenTTLSeconds > 0 {
		return time.Duration(cfg.AccessTokenTTLSeconds) * time.Second
	}
	return defaultAccessTokenTTL
}

func signingSecret() (string, error) {
	if secret := strings.TrimSpace(os.Getenv("ABLESTACK_AUTH_TOKEN_SECRET")); secret != "" {
		return secret, nil
	}
	cfg := loadAuthConfig()
	if strings.TrimSpace(cfg.AccessTokenSecret) != "" {
		return strings.TrimSpace(cfg.AccessTokenSecret), nil
	}
	secret, err := security.GenerateToken()
	if err != nil {
		return "", err
	}
	cfg.AccessTokenSecret = secret
	if err := saveAuthConfig(cfg); err != nil {
		return "", err
	}
	return secret, nil
}

func signToken(claim tokenClaim, secret string) (string, error) {
	header := tokenHeader{Alg: "HS256", Typ: "JWT"}
	headerRaw, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimRaw, err := json.Marshal(claim)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerRaw) + "." + base64.RawURLEncoding.EncodeToString(claimRaw)
	signature := sign(unsigned, secret)
	return unsigned + "." + signature, nil
}

func verifyToken(token string, secret string) (tokenClaim, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaim{}, fmt.Errorf("invalid token")
	}
	unsigned := parts[0] + "." + parts[1]
	expected := sign(unsigned, secret)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[2])) != 1 {
		return tokenClaim{}, fmt.Errorf("invalid token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaim{}, fmt.Errorf("invalid token")
	}
	var claim tokenClaim
	if err := json.Unmarshal(payload, &claim); err != nil {
		return tokenClaim{}, fmt.Errorf("invalid token")
	}
	if claim.Use != tokenUseAccess || claim.Expires <= time.Now().Unix() {
		return tokenClaim{}, fmt.Errorf("token expired")
	}
	return claim, nil
}

func sign(unsigned string, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsigned))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func loadAuthConfig() authConfig {
	path := authConfigPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return authConfig{}
	}
	cfg := authConfig{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return cfg
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return authConfig{}
	}
	return cfg
}

func saveAuthConfig(cfg authConfig) error {
	path := authConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}

func authConfigPath() string {
	if path := strings.TrimSpace(os.Getenv("ABLESTACK_AUTH_CONFIG")); path != "" {
		return path
	}
	base := strings.TrimSpace(os.Getenv("ABLESTACK_CONFIG_PATH"))
	if base == "" {
		base = "/etc/ablestack"
		if _, err := os.Stat(filepath.Join(base, "auth.json")); err != nil {
			if _, devErr := os.Stat(filepath.Join("configs", "auth.json")); devErr == nil {
				return filepath.Join("configs", "auth.json")
			}
		}
	}
	return filepath.Join(base, "auth.json")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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

func buildTargetURL(target string) string {
	scheme := firstNonEmpty(os.Getenv("ABLESTACK_API_SCHEME"), "http")
	port := firstNonEmpty(os.Getenv("ABLESTACK_API_PORT"), "8090")
	return fmt.Sprintf("%s://%s:%s", scheme, strings.TrimSpace(target), port)
}
