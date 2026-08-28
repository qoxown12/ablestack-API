package glueservice

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
)

const hostsFilePath = "/etc/hosts"

var managementHostAliases = map[string]struct{}{
	"scvm1-mngt": {},
	"scvm2-mngt": {},
	"scvm3-mngt": {},
}

// hostsFileReader는 /etc/hosts 조회를 테스트에서 대체할 수 있게 한다.
var hostsFileReader = func() ([]byte, error) {
	return os.ReadFile(hostsFilePath)
}

// ListHosts는 Ceph orchestrator host 목록에 /etc/hosts의 SCVM 관리망 주소를
// mgmtaddr 필드로 추가해 반환한다.
func ListHosts(ctx context.Context) (any, error) {
	value, err := runJSON(ctx, "ceph", "orch", "host", "ls", "-f", "json")
	if err != nil {
		return nil, err
	}

	content, err := hostsFileReader()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", hostsFilePath, err)
	}
	addresses := managementAddressesFromHosts(content)
	return addManagementAddresses(value, addresses), nil
}

// managementAddressesFromHosts는 지정된 SCVM 관리망 alias의 IP만 추출한다.
func managementAddressesFromHosts(content []byte) map[string]string {
	addresses := make(map[string]string, len(managementHostAliases))
	for _, line := range strings.Split(string(content), "\n") {
		if comment := strings.IndexByte(line, '#'); comment >= 0 {
			line = line[:comment]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || net.ParseIP(fields[0]) == nil {
			continue
		}

		for _, alias := range fields[1:] {
			alias = normalizeHostName(alias)
			if _, wanted := managementHostAliases[alias]; !wanted {
				continue
			}
			// hosts(5)와 동일하게 먼저 나온 주소를 우선한다.
			if _, exists := addresses[alias]; !exists {
				addresses[alias] = fields[0]
			}
		}
	}
	return addresses
}

// addManagementAddresses는 ceph orch의 배열 또는 객체 응답을 모두 처리한다.
func addManagementAddresses(value any, addresses map[string]string) any {
	switch value := value.(type) {
	case []any:
		for i := range value {
			value[i] = addManagementAddresses(value[i], addresses)
		}
	case map[string]any:
		// 일부 Ceph 버전은 host 배열을 hosts 키 아래에 감쌀 수 있다.
		if hosts, ok := value["hosts"]; ok {
			value["hosts"] = addManagementAddresses(hosts, addresses)
		}
		if isHostEntry(value) {
			value["mgmtaddr"] = managementAddressForEntry(value, addresses)
		}
	}
	return value
}

func isHostEntry(value map[string]any) bool {
	for _, key := range []string{"hostname", "host_name", "host", "name"} {
		if name, ok := value[key].(string); ok && strings.TrimSpace(name) != "" {
			return true
		}
	}
	return false
}

func managementAddressForEntry(value map[string]any, addresses map[string]string) string {
	for _, key := range []string{"hostname", "host_name", "host", "name"} {
		name, ok := value[key].(string)
		if !ok {
			continue
		}
		name = normalizeHostName(name)
		if address := addresses[name]; address != "" {
			return address
		}
		if strings.HasPrefix(name, "scvm") && !strings.HasSuffix(name, "-mngt") {
			if address := addresses[name+"-mngt"]; address != "" {
				return address
			}
		}
	}

	// Ceph가 관리망 주소 자체를 addr로 반환하는 경우에도 값을 보존한다.
	if addr, ok := value["addr"].(string); ok {
		for _, address := range addresses {
			if addr == address {
				return address
			}
		}
	}
	return ""
}

func normalizeHostName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}
