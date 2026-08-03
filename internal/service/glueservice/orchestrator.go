package glueservice

import (
	"context"
	"strings"
)

// ListServices는 Ceph orchestrator service 목록을 조회한다.
func ListServices(ctx context.Context, serviceType string, serviceName string) (any, error) {
	serviceType = strings.TrimSpace(serviceType)
	serviceName = strings.TrimSpace(serviceName)
	if serviceType != "" {
		if err := ValidateServiceToken(serviceType); err != nil {
			return nil, err
		}
	}
	if serviceName != "" {
		if err := ValidateServiceName(serviceName); err != nil {
			return nil, err
		}
	}

	args := []string{"orch", "ls"}
	if serviceType != "" {
		args = append(args, "--service_type", serviceType)
	}
	if serviceName != "" {
		args = append(args, "--service_name", serviceName)
	}
	args = append(args, "-f", "json")

	output, err := run(ctx, "ceph", args...)
	if err != nil {
		return nil, err
	}
	if strings.Contains(string(output), "No services reported") {
		return []any{}, nil
	}
	return decodeJSON(output)
}

// ControlService는 ceph orch start/stop/restart/redeploy만 허용한다.
func ControlService(ctx context.Context, serviceName string, control string) (map[string]any, error) {
	serviceName = strings.TrimSpace(serviceName)
	control = strings.ToLower(strings.TrimSpace(control))
	if err := ValidateServiceName(serviceName); err != nil {
		return nil, err
	}
	if err := ValidateServiceControl(control); err != nil {
		return nil, err
	}

	output, err := run(ctx, "ceph", "orch", control, serviceName)
	if err != nil {
		return nil, err
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		message = "Success"
	}
	return map[string]any{
		"status":  "success",
		"service": serviceName,
		"control": control,
		"output":  message,
	}, nil
}

// DeleteService는 Ceph orchestrator service를 삭제한다.
func DeleteService(ctx context.Context, serviceName string) (map[string]any, error) {
	serviceName = strings.TrimSpace(serviceName)
	if err := ValidateServiceName(serviceName); err != nil {
		return nil, err
	}

	if _, err := run(ctx, "ceph", "orch", "rm", serviceName); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":  "success",
		"service": serviceName,
	}, nil
}
