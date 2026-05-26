package cube

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	cloudInitHostsPath   = "/etc/hosts"
	cloudInitPrivKeyPath = "/root/.ssh/id_rsa"
	cloudInitPubKeyPath  = "/root/.ssh/id_rsa.pub"
	cloudInitMgmtNIC     = "enp0s20"
)

func parseCloudInitPrefix(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("mngtNic.cidr required")
	}
	prefix, err := strconv.Atoi(value)
	if err != nil || prefix <= 0 {
		return 0, fmt.Errorf("invalid mngtNic.cidr")
	}
	return prefix, nil
}
