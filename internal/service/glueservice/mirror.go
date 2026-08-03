package glueservice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type mirrorToken struct {
	FSID     string `json:"fsid"`
	ClientID string `json:"client_id"`
	Key      string `json:"key"`
	MonHost  string `json:"mon_host"`
}

type mirrorAuthKey struct {
	Key string `json:"key"`
}

// MirrorStatus는 local SCVM에서 RBD mirror pool summary를 조회한다.
func MirrorStatus(ctx context.Context) (any, error) {
	raw, err := runJSON(ctx, "rbd", "mirror", "pool", "status", "--format", "json", "--pretty-format")
	if err != nil {
		return nil, err
	}
	if fields, ok := raw.(map[string]any); ok {
		if summary, ok := fields["summary"]; ok {
			return summary, nil
		}
	}
	return raw, nil
}

// MirrorImageList는 pool 단위 mirror image 상태를 조회한다.
func MirrorImageList(ctx context.Context, poolName string) (any, error) {
	poolName = strings.TrimSpace(poolName)
	if err := ValidatePoolName(poolName); err != nil {
		return nil, err
	}
	return runJSON(ctx, "rbd", "mirror", "pool", "status", poolName, "--verbose", "--format", "json", "--pretty-format")
}

// MirrorImageInfo는 단일 mirror image 상태를 조회한다.
func MirrorImageInfo(ctx context.Context, poolName string, imageName string) (any, error) {
	poolName = strings.TrimSpace(poolName)
	imageName = strings.TrimSpace(imageName)
	if err := ValidatePoolName(poolName); err != nil {
		return nil, err
	}
	if err := ValidateImageName(imageName); err != nil {
		return nil, err
	}
	return runJSON(ctx, "rbd", "mirror", "image", "status", "--pool", poolName, "--image", imageName, "--format", "json")
}

// MirrorSetup은 local RBD mirror pool을 활성화하고, 필요 시 상대 cluster token을 import한다.
// remote cluster는 SSH로 직접 건드리지 않으므로 상대 SCVM에서도 같은 API를 별도로 호출해야 한다.
func MirrorSetup(ctx context.Context, localClusterName string, poolName string, remoteToken string, hosts []string, interval string) (map[string]any, error) {
	config, err := buildMirrorConfig(localClusterName, poolName, remoteToken, hosts, interval)
	if err != nil {
		return nil, err
	}
	localToken := ""
	if !config.importOnly {
		localToken, err = enableMirrorPool(ctx, config.localClusterName, config.poolName, config.hosts)
		if err != nil {
			return nil, err
		}
	}
	importedPeer := false
	if config.remoteToken != "" {
		if err := importMirrorPeer(ctx, config.poolName, config.remoteToken); err != nil {
			return nil, err
		}
		importedPeer = true
	}
	if err := ensureMirrorMetadataImage(ctx, config.interval); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":             "success",
		"mirror_pool":        config.poolName,
		"local_cluster_name": config.localClusterName,
		"local_token":        localToken,
		"imported_peer":      importedPeer,
		"interval":           config.interval,
	}, nil
}

// MirrorUpdate는 local mirror metadata image의 snapshot interval만 갱신한다.
func MirrorUpdate(ctx context.Context, poolName string, interval string) (map[string]any, error) {
	poolName = strings.TrimSpace(poolName)
	if err := ValidatePoolName(poolName); err != nil {
		return nil, err
	}
	interval = strings.TrimSpace(firstNonEmpty(interval, "1h"))
	if err := validateMirrorInterval(interval); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "rbd", "image-meta", "set", "rbd/MOLD-DR", "interval", interval); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "mirror_pool": poolName, "interval": interval}, nil
}

// MirrorDelete는 local cluster의 mirror 설정과 metadata image를 정리한다.
func MirrorDelete(ctx context.Context, poolName string) (map[string]any, error) {
	poolName = strings.TrimSpace(poolName)
	if err := ValidatePoolName(poolName); err != nil {
		return nil, err
	}
	if err := disableMirrorPool(ctx, poolName); err != nil {
		return nil, err
	}
	if err := removeRBDMirrorService(ctx); err != nil {
		return nil, err
	}
	if err := removeMirrorMetadataImage(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "mirror_pool": poolName, "removed_service": "rbd-mirror", "removed_metadata": "rbd/MOLD-DR"}, nil
}

// MirrorPoolEnable은 local pool mirroring을 활성화하고 local bootstrap token을 반환한다.
func MirrorPoolEnable(ctx context.Context, poolName string, localClusterName string, remoteToken string, hosts []string, interval string) (map[string]any, error) {
	return MirrorSetup(ctx, localClusterName, poolName, remoteToken, hosts, interval)
}

// MirrorPoolDisable은 local pool의 peer와 image mirroring 설정을 정리한다.
func MirrorPoolDisable(ctx context.Context, poolName string) (map[string]any, error) {
	poolName = strings.TrimSpace(poolName)
	if err := ValidatePoolName(poolName); err != nil {
		return nil, err
	}
	if err := disableMirrorPool(ctx, poolName); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "mirror_pool": poolName}, nil
}

// MirrorGarbageDelete는 local mirror 잔여 peer/auth/daemon/metadata image를 정리한다.
func MirrorGarbageDelete(ctx context.Context, poolName string) (map[string]any, error) {
	return MirrorDelete(ctx, poolName)
}

type mirrorConfig struct {
	localClusterName string
	poolName         string
	remoteToken      string
	hosts            []string
	interval         string
	importOnly       bool
}

func buildMirrorConfig(localClusterName string, poolName string, remoteToken string, hosts []string, interval string) (mirrorConfig, error) {
	localClusterName = strings.TrimSpace(localClusterName)
	poolName = strings.TrimSpace(poolName)
	remoteToken = strings.TrimSpace(remoteToken)
	hosts = trimStringSlice(hosts)
	interval = strings.TrimSpace(firstNonEmpty(interval, "1h"))
	importOnly := localClusterName == "" && remoteToken != ""
	if !importOnly {
		if err := ValidateCephName("local_cluster_name", localClusterName); err != nil {
			return mirrorConfig{}, err
		}
	}
	if err := ValidatePoolName(poolName); err != nil {
		return mirrorConfig{}, err
	}
	for _, host := range hosts {
		if err := ValidateCephName("host", host); err != nil {
			return mirrorConfig{}, err
		}
	}
	if remoteToken != "" {
		if _, err := decodeMirrorToken(remoteToken); err != nil {
			return mirrorConfig{}, err
		}
	}
	if err := validateMirrorInterval(interval); err != nil {
		return mirrorConfig{}, err
	}
	return mirrorConfig{localClusterName: localClusterName, poolName: poolName, remoteToken: remoteToken, hosts: hosts, interval: interval, importOnly: importOnly}, nil
}

func enableMirrorPool(ctx context.Context, localClusterName string, poolName string, hosts []string) (string, error) {
	if _, err := run(ctx, "rbd", "mirror", "pool", "enable", "--site-name", localClusterName, "-p", poolName, "image"); err != nil {
		return "", err
	}
	args := []string{"orch", "apply", "rbd-mirror"}
	if len(hosts) > 0 {
		args = append(args, "--placement", strings.Join(hosts, ","))
	}
	if _, err := run(ctx, "ceph", args...); err != nil {
		return "", err
	}
	return createMirrorBootstrapToken(ctx, localClusterName, poolName)
}

func createMirrorBootstrapToken(ctx context.Context, localClusterName string, poolName string) (string, error) {
	rawToken, err := run(ctx, "rbd", "mirror", "pool", "peer", "bootstrap", "create", "--site-name", localClusterName, "-p", poolName)
	if err != nil {
		return "", err
	}
	token, err := decodeMirrorToken(string(rawToken))
	if err != nil {
		return "", err
	}
	clientName := "client." + token.ClientID
	if _, err := run(ctx, "ceph", "auth", "caps", clientName, "mgr", "profile rbd", "mon", "profile rbd-mirror-peer", "osd", "profile rbd"); err != nil {
		return "", err
	}
	rawKey, err := run(ctx, "ceph", "auth", "get-key", clientName, "--format", "json")
	if err != nil {
		return "", err
	}
	key, err := decodeMirrorAuthKey(rawKey)
	if err != nil {
		return "", err
	}
	token.Key = key
	return encodeMirrorToken(token)
}

func importMirrorPeer(ctx context.Context, poolName string, token string) error {
	if err := removeMirrorPeers(ctx, poolName); err != nil {
		return err
	}
	tokenPath, err := writeTokenTemp(token)
	if err != nil {
		return err
	}
	defer os.Remove(tokenPath)
	_, err = run(ctx, "rbd", "mirror", "pool", "peer", "bootstrap", "import", "--pool", poolName, "--token-path", tokenPath)
	return err
}

func removeMirrorPeers(ctx context.Context, poolName string) error {
	info, err := runJSON(ctx, "rbd", "mirror", "pool", "info", "--pool", poolName, "--format", "json")
	if err != nil {
		return err
	}
	for _, peer := range mirrorPeers(info) {
		uuid := mapString(peer, "uuid")
		if uuid == "" {
			continue
		}
		if _, err := run(ctx, "rbd", "mirror", "pool", "peer", "remove", "--pool", poolName, uuid); err != nil {
			return err
		}
		clientName := mapString(peer, "client_name")
		if clientName != "" {
			_ = removeCephAuth(ctx, clientName)
		}
	}
	return nil
}

func disableMirrorPool(ctx context.Context, poolName string) error {
	if images, err := MirrorImageList(ctx, poolName); err == nil {
		for _, imageName := range mirrorImageNames(images) {
			if _, err := run(ctx, "rbd", "mirror", "image", "disable", "--pool", poolName, "--image", imageName); err != nil {
				return err
			}
			_, _ = run(ctx, "rbd", "image-meta", "remove", "rbd/MOLD-DR", imageName)
		}
	}
	if err := removeMirrorPeers(ctx, poolName); err != nil {
		return err
	}
	if _, err := run(ctx, "rbd", "mirror", "pool", "disable", "--pool", poolName); err != nil {
		return err
	}
	_ = removeCephAuth(ctx, "rbd-mirror-peer")
	return nil
}

func ensureMirrorMetadataImage(ctx context.Context, interval string) error {
	if _, err := run(ctx, "rbd", "info", "rbd/MOLD-DR", "--format", "json"); err != nil {
		if _, createErr := run(ctx, "rbd", "create", "--size", "1", "rbd/MOLD-DR"); createErr != nil {
			return createErr
		}
	}
	_, err := run(ctx, "rbd", "image-meta", "set", "rbd/MOLD-DR", "interval", interval)
	return err
}

func removeRBDMirrorService(ctx context.Context) error {
	if _, err := run(ctx, "ceph", "orch", "rm", "rbd-mirror"); err != nil {
		if strings.Contains(err.Error(), "Invalid service") {
			return nil
		}
		return err
	}
	return nil
}

func removeMirrorMetadataImage(ctx context.Context) error {
	if _, err := run(ctx, "rbd", "rm", "rbd/MOLD-DR"); err != nil {
		if strings.Contains(err.Error(), "No such file") || strings.Contains(err.Error(), "No such file or directory") {
			return nil
		}
		return err
	}
	return nil
}

func removeCephAuth(ctx context.Context, clientID string) error {
	clientID = strings.TrimSpace(strings.TrimPrefix(clientID, "client."))
	if clientID == "" {
		return nil
	}
	if _, err := run(ctx, "ceph", "auth", "del", "client."+clientID); err != nil {
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "ENOENT") {
			return nil
		}
		return err
	}
	return nil
}

func mirrorPeers(value any) []map[string]any {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	values, ok := fields["peers"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		peer, ok := value.(map[string]any)
		if ok {
			out = append(out, peer)
		}
	}
	return out
}

func mirrorImageNames(value any) []string {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	values, ok := fields["images"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		image, ok := value.(map[string]any)
		if !ok {
			continue
		}
		name := mapString(image, "name")
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func decodeMirrorToken(raw string) (mirrorToken, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return mirrorToken{}, fmt.Errorf("remote_token is required")
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return mirrorToken{}, fmt.Errorf("invalid remote_token")
	}
	var token mirrorToken
	if err := json.Unmarshal(decoded, &token); err != nil {
		return mirrorToken{}, fmt.Errorf("invalid remote_token")
	}
	if token.ClientID == "" || token.FSID == "" || token.MonHost == "" {
		return mirrorToken{}, fmt.Errorf("invalid remote_token")
	}
	return token, nil
}

func encodeMirrorToken(token mirrorToken) (string, error) {
	raw, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func decodeMirrorAuthKey(raw []byte) (string, error) {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return "", fmt.Errorf("empty auth key")
	}
	var wrapped mirrorAuthKey
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Key != "" {
		return wrapped.Key, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value), nil
	}
	return strings.TrimSpace(string(raw)), nil
}

func writeTokenTemp(token string) (string, error) {
	file, err := os.CreateTemp("", "ablestack-mirror-token-*")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.WriteString(strings.TrimSpace(token)); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func validateMirrorInterval(interval string) error {
	if interval == "" {
		return fmt.Errorf("interval is required")
	}
	unit := interval[len(interval)-1]
	if unit != 'm' && unit != 'h' && unit != 'd' {
		return fmt.Errorf("interval must end with m, h, or d")
	}
	value, err := strconv.Atoi(interval[:len(interval)-1])
	if err != nil || value < 1 {
		return fmt.Errorf("interval must be greater than zero")
	}
	return nil
}
