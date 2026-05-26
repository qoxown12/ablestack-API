package cube

// SCVMStatusDetail describes storage center VM status details.
// @name SCVMStatusDetail
type SCVMStatusDetail struct {
	ScvmStatus                  string `json:"scvm_status" example:"running"`
	VCPU                        string `json:"vcpu" example:"4"`
	Socket                      string `json:"socket" example:"N/A"`
	Core                        string `json:"core" example:"N/A"`
	Memory                      string `json:"memory" example:"4096 MiB"`
	RootDiskSize                string `json:"rootDiskSize" example:"N/A"`
	RootDiskAvail               string `json:"rootDiskAvail" example:"N/A"`
	RootDiskUsePer              string `json:"rootDiskUsePer" example:"N/A"`
	ManageNicType               string `json:"manageNicType" example:"bridge"`
	ManageNicParent             string `json:"manageNicParent" example:"br-mngt"`
	ManageNicIP                 string `json:"manageNicIp" example:"10.10.31.11"`
	ManageNicGw                 string `json:"manageNicGw" example:"N/A"`
	ManageNicDns                string `json:"manageNicDns" example:"N/A"`
	StorageServerNicType        string `json:"storageServerNicType" example:"bridge"`
	StorageServerNicParent      string `json:"storageServerNicParent" example:"br-storage"`
	StorageServerNicIP          string `json:"storageServerNicIp" example:"100.100.31.11"`
	StorageReplicationNicType   string `json:"storageReplicationNicType" example:"bridge"`
	StorageReplicationNicParent string `json:"storageReplicationNicParent" example:"br-repl"`
	StorageReplicationNicIP     string `json:"storageReplicationNicIp" example:"100.200.31.11"`
}

// SCVMStatusResponse describes storage center VM status response.
// @name SCVMStatusResponse
type SCVMStatusResponse struct {
	Code    int              `json:"code" example:"200"`
	Data    SCVMStatusDetail `json:"data"`
	Message string           `json:"message,omitempty"`
}
