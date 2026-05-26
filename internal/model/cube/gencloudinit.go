package cube

// GenCloudInitRequest는 cloud-init ISO 생성 요청 본문이다.
// @name GenCloudInitRequest
type GenCloudInitRequest struct {
	// vm type: ccvm/scvm
	Type string `json:"type" example:"scvm"`
	// output ISO path
	ISOPath string `json:"iso_path" example:"/var/lib/libvirt/images/scvm-cloudinit.iso"`
	// VM hostname
	Hostname string `json:"hostname" example:"scvm1"`
	// public key file path
	PubKey string `json:"pubkey" example:"/root/.ssh/id_rsa.pub"`
	// private key file path
	PrivKey string `json:"privkey" example:"/root/.ssh/id_rsa"`
	// hosts file path
	Hosts string `json:"hosts" example:"/etc/hosts"`
	// management NIC name
	MgmtNIC string `json:"mgmt_nic" example:"ens12"`
	// management IP
	MgmtIP string `json:"mgmt_ip" example:"10.10.14.150"`
	// management prefix
	MgmtPrefix int `json:"mgmt_prefix" example:"16"`
	// management gateway
	MgmtGW string `json:"mgmt_gw,omitempty" example:"10.10.0.1"`
	// management DNS
	DNS string `json:"dns,omitempty" example:"8.8.8.8"`
	// service NIC for ccvm
	SNNIC string `json:"sn_nic,omitempty" example:"ens13"`
	// service IP for ccvm
	SNIP string `json:"sn_ip,omitempty" example:"10.10.14.151"`
	// service prefix for ccvm
	SNPrefix int `json:"sn_prefix,omitempty" example:"16"`
	// service gateway for ccvm
	SNGW string `json:"sn_gw,omitempty" example:"10.10.0.1"`
	// service DNS for ccvm
	SNDNS string `json:"sn_dns,omitempty" example:"8.8.8.8"`
	// storage NIC for scvm
	PNNIC string `json:"pn_nic,omitempty" example:"ens13"`
	// storage IP for scvm
	PNIP string `json:"pn_ip,omitempty" example:"10.10.14.151"`
	// storage prefix for scvm
	PNPrefix int `json:"pn_prefix,omitempty" example:"24"`
	// cluster NIC for scvm
	CNNIC string `json:"cn_nic,omitempty" example:"ens14"`
	// cluster IP for scvm
	CNIP string `json:"cn_ip,omitempty" example:"10.10.14.152"`
	// cluster prefix for scvm
	CNPrefix int `json:"cn_prefix,omitempty" example:"24"`
	// scvm master flag
	Master bool `json:"master,omitempty" example:"false"`
}

// GenCloudInitISOInfo는 생성된 ISO 파일 정보이다.
// @name GenCloudInitISOInfo
type GenCloudInitISOInfo struct {
	CTime    string `json:"ctime" example:"2026-05-21T10:00:00+09:00"`
	MTime    string `json:"mtime" example:"2026-05-21T10:00:00+09:00"`
	ATime    string `json:"atime" example:"2026-05-21T10:00:00+09:00"`
	Size     int64  `json:"size" example:"4096"`
	Filepath string `json:"filepath" example:"/var/lib/libvirt/images/scvm-cloudinit.iso"`
	TmpDir   string `json:"tmpdir" example:"/tmp/gencloudinit-123456"`
}

// GenCloudInitResponse는 cloud-init ISO 생성 결과이다.
// @name GenCloudInitResponse
type GenCloudInitResponse struct {
	Code    int                 `json:"code" example:"200"`
	Val     GenCloudInitISOInfo `json:"val,omitempty"`
	Message string              `json:"message,omitempty" example:"ok"`
	Action  string              `json:"action,omitempty" example:"generate"`
}

// CCVMCloudInitCreateRequest는 cluster.json 기반 CCVM cloud-init ISO 생성 요청 본문이다.
// @name CCVMCloudInitCreateRequest
type CCVMCloudInitCreateRequest struct {
	// service NIC for ccvm
	SNNIC string `json:"sn_nic,omitempty" example:"enp0s21"`
	// service IP for ccvm
	SNIP string `json:"sn_ip,omitempty" example:"10.10.14.151"`
	// service prefix for ccvm
	SNPrefix int `json:"sn_prefix,omitempty" example:"16"`
	// service gateway for ccvm
	SNGW string `json:"sn_gw,omitempty" example:"10.10.0.1"`
	// service DNS for ccvm
	SNDNS string `json:"sn_dns,omitempty" example:"8.8.8.8"`
}
