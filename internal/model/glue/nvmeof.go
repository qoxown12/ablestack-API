package glue

// NVMeOfServiceRequest는 NVMe-oF service 생성 요청이다.
// @name GlueNVMeOfServiceRequest
type NVMeOfServiceRequest struct {
	PoolName        string   `json:"pool_name" example:"nvmeof"`
	Hosts           []string `json:"hosts" example:"scvm"`
	TgtCmdExtraArgs string   `json:"tgt_cmd_extra_args,omitempty" example:"--cpumask=0xFF"`
}

// NVMeOfImageDownloadRequest는 NVMe-oF CLI image pull 요청이다.
// @name GlueNVMeOfImageDownloadRequest
type NVMeOfImageDownloadRequest struct {
	Image string `json:"image,omitempty" example:"localhost:15000/glue/nvmeof-cli:Diplo"`
}

// NVMeOfTargetRequest는 subsystem/listener/host/namespace를 한 번에 구성하는 요청이다.
// @name GlueNVMeOfTargetRequest
type NVMeOfTargetRequest struct {
	GatewayIP      string `json:"gateway_ip" example:"10.10.10.11"`
	GatewayName    string `json:"gateway_name,omitempty" example:"client.nvmeof.nvmeof.scvm"`
	SubsystemNQNID string `json:"subsystem_nqn_id" example:"nqn.2014-08.org.nvmexpress:uuid:target01"`
	PoolName       string `json:"pool_name" example:"rbd"`
	ImageName      string `json:"image_name" example:"vm01"`
	Size           int64  `json:"size,omitempty" example:"10"`
}

// NVMeOfSubsystemRequest는 NVMe-oF subsystem 생성/삭제 요청이다.
// @name GlueNVMeOfSubsystemRequest
type NVMeOfSubsystemRequest struct {
	GatewayIP      string `json:"gateway_ip,omitempty" example:"10.10.10.11"`
	GatewayName    string `json:"gateway_name,omitempty" example:"client.nvmeof.nvmeof.scvm"`
	SubsystemNQNID string `json:"subsystem_nqn_id" example:"nqn.2014-08.org.nvmexpress:uuid:subsys01"`
}

// NVMeOfNamespaceRequest는 NVMe-oF namespace 생성 요청이다.
// @name GlueNVMeOfNamespaceRequest
type NVMeOfNamespaceRequest struct {
	SubsystemNQNID string `json:"subsystem_nqn_id" example:"nqn.2014-08.org.nvmexpress:uuid:subsys01"`
	PoolName       string `json:"pool_name" example:"rbd"`
	ImageName      string `json:"image_name" example:"vm01"`
	Size           int64  `json:"size,omitempty" example:"10"`
}

// NVMeOfNamespaceDeleteRequest는 NVMe-oF namespace 삭제 요청이다.
// @name GlueNVMeOfNamespaceDeleteRequest
type NVMeOfNamespaceDeleteRequest struct {
	SubsystemNQNID string `json:"subsystem_nqn_id" example:"nqn.2014-08.org.nvmexpress:uuid:subsys01"`
	NamespaceUUID  string `json:"namespace_uuid" example:"c3a6c20e-437c-4b03-b58f-8d706c3e5d8d"`
	ImageDelCheck  bool   `json:"image_del_check,omitempty" example:"false"`
	PoolName       string `json:"pool_name,omitempty" example:"rbd"`
	ImageName      string `json:"image_name,omitempty" example:"vm01"`
}
