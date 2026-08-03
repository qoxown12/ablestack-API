package glueservice

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// 아래 pattern은 SCVM 로컬 Ceph/RBD 명령을 위한 입력 안전장치다.
// 각 명령의 허용 문자를 검증한 뒤에만 endpoint별 허용 범위를 넓힌다.
var (
	poolNamePattern       = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	imageNamePattern      = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	imageRefPattern       = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	cephNamePattern       = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	serviceNamePattern    = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	serviceTypePattern    = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	rgwNamePattern        = regexp.MustCompile(`^[A-Za-z0-9._@:-]+$`)
	bucketNamePattern     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	nqnPattern            = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	uuidPattern           = regexp.MustCompile(`^[A-Fa-f0-9-]+$`)
	containerImagePattern = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)
	smbNamePattern        = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	smbUsernamePattern    = regexp.MustCompile(`^[A-Za-z0-9._@+-]+$`)
	smbPathPattern        = regexp.MustCompile(`^/[A-Za-z0-9._/@:+-]*$`)
	smbRealmPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	iscsiIQNPattern       = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	iscsiUserPattern      = regexp.MustCompile(`^[A-Za-z0-9._@+-]+$`)
)

func ValidatePoolName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("pool_name is required")
	}
	if !poolNamePattern.MatchString(value) {
		return fmt.Errorf("invalid pool_name")
	}
	return nil
}

func ValidateImageName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("image_name is required")
	}
	if !imageNamePattern.MatchString(value) {
		return fmt.Errorf("invalid image_name")
	}
	return nil
}

func ValidateImageRef(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("image_name is required")
	}
	if !imageRefPattern.MatchString(value) {
		return fmt.Errorf("invalid image_name")
	}
	return nil
}

func ValidateCephName(field string, value string) error {
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if field == "" {
		field = "name"
	}
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !cephNamePattern.MatchString(value) {
		return fmt.Errorf("invalid %s", field)
	}
	return nil
}

func ValidateServiceName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("service_name is required")
	}
	if !serviceNamePattern.MatchString(value) {
		return fmt.Errorf("invalid service_name")
	}
	return nil
}

func ValidateServiceToken(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if !serviceTypePattern.MatchString(value) {
		return fmt.Errorf("invalid service_type")
	}
	return nil
}

func ValidateServiceControl(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "start", "stop", "restart", "redeploy":
		return nil
	default:
		return fmt.Errorf("control must be one of start, stop, restart, redeploy")
	}
}

func ValidateRGWName(field string, value string) error {
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if field == "" {
		field = "name"
	}
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !rgwNamePattern.MatchString(value) {
		return fmt.Errorf("invalid %s", field)
	}
	return nil
}

func ValidateBucketName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("bucket_name is required")
	}
	if !bucketNamePattern.MatchString(value) {
		return fmt.Errorf("invalid bucket_name")
	}
	return nil
}

func ValidateNQN(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("subsystem_nqn_id is required")
	}
	if !nqnPattern.MatchString(value) {
		return fmt.Errorf("invalid subsystem_nqn_id")
	}
	return nil
}

func ValidateOptionalNQN(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return ValidateNQN(value)
}

func ValidateNamespaceUUID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("namespace_uuid is required")
	}
	if !uuidPattern.MatchString(value) {
		return fmt.Errorf("invalid namespace_uuid")
	}
	return nil
}

func ValidateIPAddress(field string, value string) error {
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if field == "" {
		field = "ip"
	}
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if net.ParseIP(value) == nil {
		return fmt.Errorf("invalid %s", field)
	}
	return nil
}

func ValidateIPOrCIDR(field string, value string) error {
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if field == "" {
		field = "ip"
	}
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if net.ParseIP(value) != nil {
		return nil
	}
	if _, _, err := net.ParseCIDR(value); err == nil {
		return nil
	}
	return fmt.Errorf("invalid %s", field)
}

func ValidateCIDR(field string, value string) error {
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if field == "" {
		field = "cidr"
	}
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if _, _, err := net.ParseCIDR(value); err != nil {
		return fmt.Errorf("invalid %s", field)
	}
	return nil
}

func ValidatePort(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("port is required")
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

func ValidateContainerImage(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("image is required")
	}
	if !containerImagePattern.MatchString(value) {
		return fmt.Errorf("invalid image")
	}
	return nil
}

func ValidateSMBSecurityType(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "normal", "ads":
		return nil
	default:
		return fmt.Errorf("sec_type must be one of normal, ads")
	}
}

func ValidateSMBCachePolicy(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "false":
		return nil
	default:
		return fmt.Errorf("cache_policy must be one of true, false")
	}
}

func ValidateSMBFolderName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("folder_name is required")
	}
	if !smbNamePattern.MatchString(value) {
		return fmt.Errorf("invalid folder_name")
	}
	return nil
}

func ValidateSMBUsername(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("username is required")
	}
	if !smbUsernamePattern.MatchString(value) {
		return fmt.Errorf("invalid username")
	}
	return nil
}

func ValidateSMBPassword(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("password is required")
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("invalid password")
	}
	return nil
}

func ValidateSMBPath(field string, value string) error {
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if field == "" {
		field = "path"
	}
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if value == "/" || !smbPathPattern.MatchString(value) || hasParentPathSegment(value) {
		return fmt.Errorf("invalid %s", field)
	}
	return nil
}

func ValidateSMBRealm(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("realm is required")
	}
	if !smbRealmPattern.MatchString(value) {
		return fmt.Errorf("invalid realm")
	}
	return nil
}

func hasParentPathSegment(value string) bool {
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func ValidateISCSIIQN(field string, value string) error {
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if field == "" {
		field = "iqn_id"
	}
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !strings.HasPrefix(strings.ToLower(value), "iqn.") || !iscsiIQNPattern.MatchString(value) {
		return fmt.Errorf("invalid %s", field)
	}
	return nil
}

func ValidateISCSIUser(field string, value string, required bool) error {
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if field == "" {
		field = "user"
	}
	if value == "" {
		if required {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	if len(value) < 3 || len(value) > 64 || !iscsiUserPattern.MatchString(value) {
		return fmt.Errorf("invalid %s", field)
	}
	return nil
}

func ValidateISCSISecret(field string, value string, required bool) error {
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if field == "" {
		field = "password"
	}
	if value == "" {
		if required {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("invalid %s", field)
	}
	return nil
}
