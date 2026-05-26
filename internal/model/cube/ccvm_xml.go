package cube

// CCVMXMLCreateRequest는 Cloud Center VM XML 생성 요청 본문이다.
// @name CCVMXMLCreateRequest
type CCVMXMLCreateRequest struct {
	// vCPU 개수
	CPU int `json:"cpu" example:"4"`
	// 메모리 크기(GiB)
	Memory int `json:"memory" example:"16"`
	// GFS mount point. ablestack-vm에서 ccvm.qcow2 경로 계산에 사용한다.
	GFSMountPoint string `json:"gfs_mount_point,omitempty" example:"/mnt/glue-gfs"`
	// management network bridge
	ManagementNetworkBridge string `json:"management_network_bridge" example:"br0"`
	// optional service network bridge
	ServiceNetworkBridge string `json:"service_network_bridge,omitempty" example:"br1"`
	// 내부 fan-out용 XML 내용
	XMLContent string `json:"xml_content,omitempty" swaggerignore:"true"`
}

// CCVMXMLCreateResponse는 Cloud Center VM XML 생성 결과이다.
// @name CCVMXMLCreateResponse
type CCVMXMLCreateResponse struct {
	// 처리 결과 코드
	Code int `json:"code" example:"200"`
	// 기존 Python createReturn 호환 결과값
	Val any `json:"val"`
	// 처리 결과 메시지
	Message string `json:"message,omitempty" example:"클라우드센터 가상머신 xml 생성 성공"`
	// 생성된 XML 파일 경로
	XMLPath string `json:"xml_path,omitempty" example:"/etc/ablestack/vmconfig/ccvm/ccvm.xml"`
	// fan-out 대상별 결과
	Results []ClusterApplyResult `json:"results,omitempty"`
}
