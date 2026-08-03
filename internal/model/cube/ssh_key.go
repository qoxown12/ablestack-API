package cube

// SSHKeyRequest는 /root/.ssh SSH key 파일 관리 요청 본문이다.
// @name SSHKeyRequest
type SSHKeyRequest struct {
	// action: generate/download/upload
	Action string `json:"action" example:"generate"`
	// 기존 파일이 있으면 덮어쓴다. 기본값은 true다.
	Overwrite *bool `json:"overwrite,omitempty" example:"true"`
	// generate 액션에서 사용할 RSA key bit 수. 비어 있으면 4096을 사용한다.
	Bits int `json:"bits,omitempty" example:"4096"`
}

// SSHKeyFileInfo는 SSH key 파일 상태를 담는다.
// @name SSHKeyFileInfo
type SSHKeyFileInfo struct {
	Name       string `json:"name" example:"id_rsa"`
	Path       string `json:"path" example:"/root/.ssh/id_rsa"`
	Exists     bool   `json:"exists" example:"true"`
	Size       int64  `json:"size,omitempty" example:"3243"`
	Mode       string `json:"mode,omitempty" example:"0600"`
	ModifiedAt string `json:"modified_at,omitempty" example:"2026-05-29T12:00:00+09:00"`
}

// SSHKeyResult는 SSH key 액션 결과이다.
// @name SSHKeyResult
type SSHKeyResult struct {
	Message   string           `json:"message,omitempty" example:"ssh key files generated"`
	Directory string           `json:"directory" example:"/root/.ssh"`
	Filename  string           `json:"filename,omitempty" example:"7f8c8df7e2f01d6a.dat"`
	Files     []SSHKeyFileInfo `json:"files,omitempty"`
}

// SSHKeyResponse는 SSH key 관리 API 응답이다.
// @name SSHKeyResponse
type SSHKeyResponse struct {
	Code    int    `json:"code" example:"200"`
	Val     any    `json:"val,omitempty"`
	RetName string `json:"retname,omitempty" example:"SSH Key"`
	Message string `json:"message,omitempty" example:"ok"`
	Action  string `json:"action,omitempty" example:"generate"`
}
