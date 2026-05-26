package cube

// CCVMLocalStatus describes local ccvm status fields.
// @name CCVMLocalStatus
type CCVMLocalStatus struct {
	Name                string   `json:"Name" example:"ccvm"`
	State               string   `json:"State" example:"running"`
	CPU                 string   `json:"CPU(s)" example:"8"`
	MaxMemory           string   `json:"Max memory" example:"16384000 KiB"`
	UsedMemory          string   `json:"Used memory" example:"4096000 KiB"`
	IP                  string   `json:"ip" example:"10.10.32.10"`
	MAC                 string   `json:"mac" example:"00:24:81:34:dd:89"`
	NICType             string   `json:"nictype" example:"bridge"`
	NICBridge           string   `json:"nicbridge" example:"bridge"`
	UUID                string   `json:"UUID" example:"93ec0102-0939-464f-b060-348141d6929e"`
	Prefix              string   `json:"prefix" example:"16"`
	Blk                 []string `json:"blk,omitempty"`
	DiskCap             string   `json:"DISK_CAP" example:"50G"`
	DiskAlloc           string   `json:"DISK_ALLOC" example:"10G"`
	DiskPhy             string   `json:"DISK_PHY" example:"40G"`
	DiskUsageRate       string   `json:"DISK_USAGE_RATE" example:"20%"`
	SecondDiskCap       string   `json:"SECOND_DISK_CAP" example:"100G"`
	SecondDiskAlloc     string   `json:"SECOND_DISK_ALLOC" example:"20G"`
	SecondDiskPhy       string   `json:"SECOND_DISK_PHY" example:"80G"`
	SecondDiskUsageRate string   `json:"SECOND_DISK_USAGE_RATE" example:"20%"`
	GW                  string   `json:"GW" example:"10.10.0.1"`
	DNS                 string   `json:"DNS" example:"8.8.8.8"`
	MoldServiceStatus   string   `json:"MOLD_SERVICE_STATUE" example:"active"`
	MoldDBStatus        string   `json:"MOLD_DB_STATUE" example:"active"`
}

// CCVMLocalStatusResponse describes local ccvm status response.
// @name CCVMLocalStatusResponse
type CCVMLocalStatusResponse struct {
	Code    int             `json:"code" example:"200"`
	Data    CCVMLocalStatus `json:"data"`
	Message string          `json:"message,omitempty"`
}

// CCVMStatusResponse describes ccvm status response.
// @name CCVMStatusResponse
type CCVMStatusResponse struct {
	Code    int    `json:"code" example:"200"`
	Data    any    `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}
