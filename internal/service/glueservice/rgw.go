package glueservice

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type rgwBucketCreatePayload struct {
	Bucket                  string `json:"bucket"`
	UID                     string `json:"uid"`
	LockEnabled             string `json:"lock_enabled"`
	LockMode                string `json:"lock_mode,omitempty"`
	LockRetentionPeriodDays string `json:"lock_retention_period_days,omitempty"`
}

type rgwBucketUpdatePayload struct {
	BucketID                string `json:"bucket_id"`
	UID                     string `json:"uid"`
	VersioningState         string `json:"versioning_state,omitempty"`
	LockMode                string `json:"lock_mode,omitempty"`
	LockRetentionPeriodDays string `json:"lock_retention_period_days,omitempty"`
}

// RGWDaemons는 RGW service와 daemon 상태를 SCVM 로컬 명령으로 조회한다.
func RGWDaemons(ctx context.Context) (map[string]any, error) {
	services, err := ListServices(ctx, "rgw", "")
	if err != nil {
		return nil, err
	}
	daemons, err := runJSON(ctx, "ceph", "orch", "ps", "--daemon_type", "rgw", "-f", "json")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"services": services,
		"daemons":  daemons,
	}, nil
}

// RGWUsers는 user list/info/stats를 조회한다.
func RGWUsers(ctx context.Context, username string) (any, error) {
	username = strings.TrimSpace(username)
	if username != "" {
		if err := ValidateRGWName("username", username); err != nil {
			return nil, err
		}
		return rgwUserWithStats(ctx, username)
	}

	output, err := run(ctx, "radosgw-admin", "user", "list")
	if err != nil {
		return nil, err
	}
	usernames, err := decodeJSONStringSlice(output)
	if err != nil {
		return nil, err
	}
	users := make([]map[string]any, 0, len(usernames))
	for _, name := range usernames {
		if err := ValidateRGWName("username", name); err != nil {
			return nil, err
		}
		user, err := rgwUserWithStats(ctx, name)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

// RGWBuckets는 bucket 목록 또는 stats 정보를 조회한다.
func RGWBuckets(ctx context.Context, bucketName string, detail bool) (any, error) {
	bucketName = strings.TrimSpace(bucketName)
	if bucketName != "" {
		if err := ValidateBucketName(bucketName); err != nil {
			return nil, err
		}
	}
	if detail || bucketName != "" {
		args := []string{"bucket", "stats"}
		if bucketName != "" {
			args = append(args, "--bucket", bucketName)
		}
		return runJSON(ctx, "radosgw-admin", args...)
	}
	return runJSON(ctx, "radosgw-admin", "bucket", "list")
}

// RGWServiceCreateOrUpdate는 RGW service를 생성하거나 재적용한다.
func RGWServiceCreateOrUpdate(ctx context.Context, serviceName string, realmName string, zonegroupName string, zoneName string, hosts []string, port string) (map[string]any, error) {
	serviceName = strings.TrimSpace(serviceName)
	realmName = strings.TrimSpace(realmName)
	zonegroupName = strings.TrimSpace(zonegroupName)
	zoneName = strings.TrimSpace(zoneName)
	hosts = trimStringSlice(hosts)
	if err := ValidateServiceName(serviceName); err != nil {
		return nil, err
	}
	if err := ValidatePort(port); err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("hosts is required")
	}
	for _, host := range hosts {
		if err := ValidateCephName("host", host); err != nil {
			return nil, err
		}
	}
	args := []string{"orch", "apply", "rgw", serviceName}
	if realmName != "" {
		if err := ValidateCephName("realm_name", realmName); err != nil {
			return nil, err
		}
		if err := ValidateCephName("zonegroup_name", zonegroupName); err != nil {
			return nil, err
		}
		if err := ValidateCephName("zone_name", zoneName); err != nil {
			return nil, err
		}
		if _, err := run(ctx, "radosgw-admin", "realm", "create", "--rgw-realm", realmName); err != nil {
			return nil, err
		}
		if _, err := run(ctx, "radosgw-admin", "zonegroup", "create", "--rgw-zonegroup", zonegroupName, "--rgw-realm", realmName, "--master"); err != nil {
			return nil, err
		}
		if _, err := run(ctx, "radosgw-admin", "zone", "create", "--rgw-zonegroup", zonegroupName, "--rgw-zone", zoneName, "--master"); err != nil {
			return nil, err
		}
		args = append(args, "--realm", realmName, "--zone", zoneName, "--zonegroup", zonegroupName)
	}
	args = append(args, "--placement", strings.Join(hosts, ","), "--port", strings.TrimSpace(port))
	if _, err := run(ctx, "ceph", args...); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "service_name": serviceName, "hosts": hosts, "port": strings.TrimSpace(port)}, nil
}

// RGWUserCreate는 RGW admin user를 생성한다.
func RGWUserCreate(ctx context.Context, username string, displayName string, email string) (map[string]any, error) {
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	email = strings.TrimSpace(email)
	if err := ValidateRGWName("username", username); err != nil {
		return nil, err
	}
	if displayName == "" {
		return nil, fmt.Errorf("display_name is required")
	}
	args := []string{"user", "create", "--uid", username, "--display-name", displayName, "--admin"}
	if email != "" {
		args = append(args, "--email", email)
	}
	if _, err := run(ctx, "radosgw-admin", args...); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "username": username}, nil
}

// RGWUserUpdate는 RGW user metadata/key를 수정한다.
func RGWUserUpdate(ctx context.Context, username string, displayName string, email string, keyType string, accessKey string, secretKey string) (map[string]any, error) {
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	email = strings.TrimSpace(email)
	keyType = strings.TrimSpace(keyType)
	accessKey = strings.TrimSpace(accessKey)
	secretKey = strings.TrimSpace(secretKey)
	if err := ValidateRGWName("username", username); err != nil {
		return nil, err
	}
	args := []string{"user", "modify", "--uid", username}
	if displayName != "" {
		args = append(args, "--display-name", displayName)
	}
	if email != "" {
		args = append(args, "--email", email)
	}
	if keyType != "" {
		if err := ValidateRGWName("key_type", keyType); err != nil {
			return nil, err
		}
		if accessKey == "" || secretKey == "" {
			return nil, fmt.Errorf("access_key and secret_key are required when key_type is set")
		}
		args = append(args, "--key-type", keyType, "--access-key", accessKey, "--secret-key", secretKey)
	}
	if _, err := runRGW(ctx, args...); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "username": username}, nil
}

// RGWUserDelete는 RGW user를 삭제한다.
func RGWUserDelete(ctx context.Context, username string) (map[string]any, error) {
	username = strings.TrimSpace(username)
	if err := ValidateRGWName("username", username); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "radosgw-admin", "user", "rm", "--uid", username); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "username": username}, nil
}

// RGWQuotaSet은 RGW quota를 설정하고 enable/disable을 적용한다.
func RGWQuotaSet(ctx context.Context, username string, scope string, maxObjects string, maxSize string, state string) (map[string]any, error) {
	username = strings.TrimSpace(username)
	scope = strings.TrimSpace(scope)
	maxObjects = strings.TrimSpace(maxObjects)
	maxSize = strings.TrimSpace(maxSize)
	state = strings.ToLower(strings.TrimSpace(firstNonEmpty(state, "enable")))
	if err := ValidateRGWName("username", username); err != nil {
		return nil, err
	}
	if scope != "user" && scope != "bucket" {
		return nil, fmt.Errorf("scope must be one of user, bucket")
	}
	if maxObjects == "" || maxSize == "" {
		return nil, fmt.Errorf("max_objects and max_size are required")
	}
	if state != "enable" && state != "disable" {
		return nil, fmt.Errorf("state must be one of enable, disable")
	}
	if _, err := run(ctx, "radosgw-admin", "quota", "set", "--uid", username, "--quota-scope", scope, "--max-objects", maxObjects, "--max-size", maxSize); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "radosgw-admin", "quota", state, "--uid", username, "--quota-scope", scope); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "username": username, "scope": scope, "state": state}, nil
}

// RGWBucketCreate는 Ceph dashboard API로 RGW bucket을 생성한다.
func RGWBucketCreate(ctx context.Context, bucketName string, username string, lockEnabled string, lockMode string, lockRetentionPeriodDays string) (any, error) {
	bucketName = strings.TrimSpace(bucketName)
	username = strings.TrimSpace(username)
	lockEnabled = strings.ToLower(strings.TrimSpace(firstNonEmpty(lockEnabled, "false")))
	if err := ValidateBucketName(bucketName); err != nil {
		return nil, err
	}
	if err := ValidateRGWName("username", username); err != nil {
		return nil, err
	}
	if lockEnabled != "true" && lockEnabled != "false" {
		return nil, fmt.Errorf("lock_enabled must be true or false")
	}
	payload := rgwBucketCreatePayload{
		Bucket:                  bucketName,
		UID:                     username,
		LockEnabled:             lockEnabled,
		LockMode:                strings.TrimSpace(lockMode),
		LockRetentionPeriodDays: strings.TrimSpace(lockRetentionPeriodDays),
	}
	return cephDashboardRequest(ctx, http.MethodPost, "api/rgw/bucket", payload, http.StatusCreated, http.StatusOK)
}

// RGWBucketUpdate는 Ceph dashboard API로 RGW bucket 설정을 수정한다.
func RGWBucketUpdate(ctx context.Context, bucketName string, bucketID string, username string, versioning string, lockMode string, lockRetentionPeriodDays string) (any, error) {
	bucketName = strings.TrimSpace(bucketName)
	bucketID = strings.TrimSpace(bucketID)
	username = strings.TrimSpace(username)
	if err := ValidateBucketName(bucketName); err != nil {
		return nil, err
	}
	if bucketID == "" {
		return nil, fmt.Errorf("bucket_id is required")
	}
	if err := ValidateRGWName("username", username); err != nil {
		return nil, err
	}
	payload := rgwBucketUpdatePayload{
		BucketID:                bucketID,
		UID:                     username,
		VersioningState:         strings.TrimSpace(versioning),
		LockMode:                strings.TrimSpace(lockMode),
		LockRetentionPeriodDays: strings.TrimSpace(lockRetentionPeriodDays),
	}
	return cephDashboardRequest(ctx, http.MethodPut, "api/rgw/bucket/"+url.PathEscape(bucketName), payload, http.StatusOK)
}

// RGWBucketDelete는 RGW bucket을 삭제한다.
func RGWBucketDelete(ctx context.Context, bucketName string) (map[string]any, error) {
	bucketName = strings.TrimSpace(bucketName)
	if err := ValidateBucketName(bucketName); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "radosgw-admin", "bucket", "rm", "--bucket", bucketName); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "bucket_name": bucketName}, nil
}

func rgwUserWithStats(ctx context.Context, username string) (map[string]any, error) {
	info, err := runJSON(ctx, "radosgw-admin", "user", "info", "--uid", username)
	if err != nil {
		return nil, err
	}
	stats, err := runJSON(ctx, "radosgw-admin", "user", "stats", "--uid", username)
	if err != nil {
		return nil, err
	}
	if value, ok := info.(map[string]any); ok {
		value["stats"] = stats
		return value, nil
	}
	return map[string]any{
		"info":  info,
		"stats": stats,
	}, nil
}

func runRGW(ctx context.Context, args ...string) ([]byte, error) {
	output, timedOut, err := runCommand(ctx, "radosgw-admin", args...)
	if err != nil {
		return nil, CommandError{
			Command:  "radosgw-admin",
			Args:     redactFlagValue(args, "--secret-key"),
			Output:   string(output),
			TimedOut: timedOut,
			Err:      err,
		}
	}
	return output, nil
}

func redactFlagValue(args []string, flag string) []string {
	out := append([]string(nil), args...)
	for i, arg := range out {
		if strings.EqualFold(arg, flag) && i+1 < len(out) {
			out[i+1] = "****"
		}
	}
	return out
}
