package glueservice

import (
	"bytes"
	"context"
	"os"
	"strings"
)

const (
	defaultSMBScriptPath = "/etc/ablestack/shell/Samba-Execute.sh"
	envSMBScriptPath     = "ABLESTACK_GLUE_SMB_SCRIPT"
)

type smbShareConfig struct {
	SecurityType string
	CachePolicy  string
	Username     string
	Password     string
	FolderName   string
	Path         string
	FSName       string
	VolumePath   string
	Realm        string
	DNS          string
}

type smbFolderConfig struct {
	CachePolicy string
	FolderName  string
	Path        string
	FSName      string
	VolumePath  string
}

// SMBStatus는 SCVM 로컬 Samba 실행 스크립트의 select 결과를 조회한다.
func SMBStatus(ctx context.Context) (any, error) {
	output, err := runSMBScript(ctx, "select")
	if err != nil {
		return nil, err
	}
	return decodeJSON(output)
}

// SMBCreate는 기존 SMB 구성을 정리한 뒤 SCVM 로컬 Samba share를 생성한다.
func SMBCreate(ctx context.Context, secType string, cachePolicy string, username string, password string, folderName string, path string, fsName string, volumePath string, realm string, dns string) (map[string]any, error) {
	config, err := normalizeSMBShareConfig(secType, cachePolicy, username, password, folderName, path, fsName, volumePath, realm, dns)
	if err != nil {
		return nil, err
	}

	if _, err := runSMBScript(ctx, "delete"); err != nil {
		return nil, err
	}

	args := []string{
		"create", config.SecurityType,
		"--username", config.Username,
		"--password", config.Password,
		"--cache_policy", config.CachePolicy,
		"--folder", config.FolderName,
		"--path", config.Path,
		"--fs_name", config.FSName,
		"--volume_path", config.VolumePath,
	}
	if config.SecurityType == "ads" {
		args = append(args, "--realm", config.Realm, "--dns", config.DNS)
	}
	if _, err := runSMBScript(ctx, args...); err != nil {
		_, _ = runSMBScript(ctx, "delete")
		return nil, err
	}

	return map[string]any{
		"status":       "success",
		"sec_type":     config.SecurityType,
		"cache_policy": config.CachePolicy,
		"folder_name":  config.FolderName,
		"path":         config.Path,
		"fs_name":      config.FSName,
		"volume_path":  config.VolumePath,
	}, nil
}

// SMBDelete는 SCVM 로컬 SMB service와 share 구성을 삭제한다.
func SMBDelete(ctx context.Context) (map[string]any, error) {
	if _, err := runSMBScript(ctx, "delete"); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success"}, nil
}

// SMBShareFolderAdd는 기존 SMB service에 share folder를 추가한다.
func SMBShareFolderAdd(ctx context.Context, cachePolicy string, folderName string, path string, fsName string, volumePath string) (map[string]any, error) {
	config, err := normalizeSMBFolderConfig(cachePolicy, folderName, path, fsName, volumePath)
	if err != nil {
		return nil, err
	}
	if _, err := runSMBScript(ctx,
		"share_folder_add",
		"--cache_policy", config.CachePolicy,
		"--folder", config.FolderName,
		"--path", config.Path,
		"--fs_name", config.FSName,
		"--volume_path", config.VolumePath,
	); err != nil {
		_, _ = runSMBScript(ctx, "share_folder_delete", "--folder", config.FolderName, "--path", config.Path, "--fs_name", config.FSName)
		return nil, err
	}
	return map[string]any{
		"status":       "success",
		"cache_policy": config.CachePolicy,
		"folder_name":  config.FolderName,
		"path":         config.Path,
		"fs_name":      config.FSName,
		"volume_path":  config.VolumePath,
	}, nil
}

// SMBShareFolderDelete는 SMB share folder와 관련 mount 구성을 삭제한다.
func SMBShareFolderDelete(ctx context.Context, folderName string, path string, fsName string) (map[string]any, error) {
	folderName = strings.TrimSpace(folderName)
	path = strings.TrimSpace(path)
	fsName = strings.TrimSpace(fsName)
	if err := ValidateSMBFolderName(folderName); err != nil {
		return nil, err
	}
	if err := ValidateSMBPath("path", path); err != nil {
		return nil, err
	}
	if err := ValidateCephName("fs_name", fsName); err != nil {
		return nil, err
	}
	if _, err := runSMBScript(ctx, "share_folder_delete", "--folder", folderName, "--path", path, "--fs_name", fsName); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":      "success",
		"folder_name": folderName,
		"path":        path,
		"fs_name":     fsName,
	}, nil
}

// SMBUserCreate는 로컬 SMB 계정을 생성한다.
func SMBUserCreate(ctx context.Context, username string, password string) (map[string]any, error) {
	username, password, err := normalizeSMBUser(username, password)
	if err != nil {
		return nil, err
	}
	if _, err := runSMBScript(ctx, "user_create", "normal", "--username", username, "--password", password); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "username": username}, nil
}

// SMBUserUpdate는 로컬 SMB 계정의 password를 변경한다.
func SMBUserUpdate(ctx context.Context, username string, password string) (map[string]any, error) {
	username, password, err := normalizeSMBUser(username, password)
	if err != nil {
		return nil, err
	}
	if _, err := runSMBScript(ctx, "user_update", "normal", "--username", username, "--password", password); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "username": username}, nil
}

// SMBUserDelete는 로컬 SMB 계정을 삭제한다.
func SMBUserDelete(ctx context.Context, username string) (map[string]any, error) {
	username = strings.TrimSpace(username)
	if err := ValidateSMBUsername(username); err != nil {
		return nil, err
	}
	if _, err := runSMBScript(ctx, "user_delete", "normal", "--username", username); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "username": username}, nil
}

func normalizeSMBShareConfig(secType string, cachePolicy string, username string, password string, folderName string, path string, fsName string, volumePath string, realm string, dns string) (smbShareConfig, error) {
	config := smbShareConfig{
		SecurityType: strings.ToLower(strings.TrimSpace(firstNonEmpty(secType, "normal"))),
		CachePolicy:  strings.ToLower(strings.TrimSpace(firstNonEmpty(cachePolicy, "true"))),
		Username:     strings.TrimSpace(username),
		Password:     strings.TrimSpace(password),
		FolderName:   strings.TrimSpace(folderName),
		Path:         strings.TrimSpace(path),
		FSName:       strings.TrimSpace(fsName),
		VolumePath:   strings.TrimSpace(volumePath),
		Realm:        strings.TrimSpace(realm),
		DNS:          strings.TrimSpace(dns),
	}
	if err := ValidateSMBSecurityType(config.SecurityType); err != nil {
		return config, err
	}
	if err := ValidateSMBCachePolicy(config.CachePolicy); err != nil {
		return config, err
	}
	if err := ValidateSMBUsername(config.Username); err != nil {
		return config, err
	}
	if err := ValidateSMBPassword(config.Password); err != nil {
		return config, err
	}
	if err := ValidateSMBFolderName(config.FolderName); err != nil {
		return config, err
	}
	if err := ValidateSMBPath("path", config.Path); err != nil {
		return config, err
	}
	if err := ValidateCephName("fs_name", config.FSName); err != nil {
		return config, err
	}
	if err := ValidateSMBPath("volume_path", config.VolumePath); err != nil {
		return config, err
	}
	if config.SecurityType == "ads" {
		if err := ValidateSMBRealm(config.Realm); err != nil {
			return config, err
		}
		if err := ValidateIPAddress("dns", config.DNS); err != nil {
			return config, err
		}
	}
	return config, nil
}

func normalizeSMBFolderConfig(cachePolicy string, folderName string, path string, fsName string, volumePath string) (smbFolderConfig, error) {
	config := smbFolderConfig{
		CachePolicy: strings.ToLower(strings.TrimSpace(firstNonEmpty(cachePolicy, "true"))),
		FolderName:  strings.TrimSpace(folderName),
		Path:        strings.TrimSpace(path),
		FSName:      strings.TrimSpace(fsName),
		VolumePath:  strings.TrimSpace(volumePath),
	}
	if err := ValidateSMBCachePolicy(config.CachePolicy); err != nil {
		return config, err
	}
	if err := ValidateSMBFolderName(config.FolderName); err != nil {
		return config, err
	}
	if err := ValidateSMBPath("path", config.Path); err != nil {
		return config, err
	}
	if err := ValidateCephName("fs_name", config.FSName); err != nil {
		return config, err
	}
	if err := ValidateSMBPath("volume_path", config.VolumePath); err != nil {
		return config, err
	}
	return config, nil
}

func normalizeSMBUser(username string, password string) (string, string, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if err := ValidateSMBUsername(username); err != nil {
		return "", "", err
	}
	if err := ValidateSMBPassword(password); err != nil {
		return "", "", err
	}
	return username, password, nil
}

func runSMBScript(ctx context.Context, args ...string) ([]byte, error) {
	scriptPath := smbScriptPath()
	output, timedOut, err := runCommand(ctx, scriptPath, args...)
	if err != nil {
		return nil, CommandError{
			Command:  scriptPath,
			Args:     redactSMBArgs(args),
			Output:   string(output),
			TimedOut: timedOut,
			Err:      err,
		}
	}
	return bytes.TrimSpace(output), nil
}

func smbScriptPath() string {
	if value := strings.TrimSpace(os.Getenv(envSMBScriptPath)); value != "" {
		return value
	}
	return defaultSMBScriptPath
}

func redactSMBArgs(args []string) []string {
	out := append([]string(nil), args...)
	for i, arg := range out {
		if strings.EqualFold(arg, "--password") && i+1 < len(out) {
			out[i+1] = "****"
		}
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
