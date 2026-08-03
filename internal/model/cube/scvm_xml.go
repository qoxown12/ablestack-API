package cube

// SCVMXMLCreateRequest는 Storage Center VM XML 생성 요청 본문이다.
// @name SCVMXMLCreateRequest
type SCVMXMLCreateRequest struct {
	// vCPU 개수
	CPU int `json:"cpu" example:"4"`
	// 메모리 크기(GiB)
	Memory int `json:"memory" example:"16"`
	// disk type: raid_passthrough/lun_passthrough/disk_passthrough
	DiskType string `json:"disk_type" example:"disk_passthrough"`
	// RAID passthrough PCI 목록
	RaidPassthroughList []string `json:"raid_passthrough_list,omitempty" example:"00:1f.2"`
	// LUN passthrough block device 목록
	LunPassthroughList []string `json:"lun_passthrough_list,omitempty" example:"/dev/disk/by-id/wwn-0x1234"`
	// DISK passthrough block device 목록
	DiskPassthroughList []string `json:"disk_passthrough_list,omitempty" example:"/dev/disk/by-id/wwn-0x1234"`
	// management network bridge
	ManagementNetworkBridge string `json:"management_network_bridge" example:"br0"`
	// storage traffic network type: nic_passthrough/nic_passthrough_bonding/bridge
	StorageTrafficNetworkType string `json:"storage_traffic_network_type" example:"bridge"`
	// server network PCI ID for nic_passthrough
	ServerNicPassthrough string `json:"server_nic_passthrough,omitempty" example:"0000:03:00.0"`
	// replication network PCI ID for nic_passthrough
	ReplicationNicPassthrough string `json:"replication_nic_passthrough,omitempty" example:"0000:04:00.0"`
	// server network PCI IDs for nic_passthrough_bonding
	ServerNicPassthroughBondingList []string `json:"server_nic_passthrough_bonding_list,omitempty" example:"0000:03:00.0,0000:03:00.1"`
	// replication network PCI IDs for nic_passthrough_bonding
	ReplicationNicPassthroughBondingList []string `json:"replication_nic_passthrough_bonding_list,omitempty" example:"0000:04:00.0,0000:04:00.1"`
	// server network bridge for bridge mode
	ServerNetworkBridge string `json:"server_network_bridge,omitempty" example:"br1"`
	// replication network bridge for bridge mode
	ReplicationNetworkBridge string `json:"replication_network_bridge,omitempty" example:"br2"`
}

// SCVMXMLCreateResponse는 Storage Center VM XML 생성 결과이다.
// @name SCVMXMLCreateResponse
type SCVMXMLCreateResponse struct {
	// 처리 결과 코드
	Code int `json:"code" example:"200"`
	// 기존 Python createReturn 호환 결과값
	Val any `json:"val"`
	// 처리 결과 메시지
	Message string `json:"message,omitempty" example:"scvm xml create success"`
	// 생성된 XML 파일 경로
	XMLPath string `json:"xml_path,omitempty" example:"/etc/ablestack/vmconfig/scvm/scvm.xml"`
}
