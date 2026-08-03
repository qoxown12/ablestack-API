package cube

// BootstrapRequest는 SCVM/CCVM bootstrap 스크립트 실행, API health 확인, 라이선스 후처리 요청 본문이다.
// @name BootstrapRequest
type BootstrapRequest struct {
	// VM API health/license 단계 전에 host qemu-guest-agent로 /root/bootstrap.sh를 실행할지 여부.
	RunScript *bool `json:"run_script,omitempty" example:"true"`
	// 라이선스 파일 내용(base64). 비어 있으면 현재 로컬 라이선스를 재사용한다.
	LicenseContent string `json:"license_content,omitempty" example:"BASE64_CONTENT"`
	// hostname, role, target IP, index, scvmN 이름별 라이선스 내용.
	Licenses map[string]string `json:"licenses,omitempty"`
	// 대상 노드에 저장할 라이선스 파일 이름.
	LicenseFilename string `json:"license_filename,omitempty" example:"license.lic"`
	// cluster.json hosts[].hostname 기준 명시 대상. ccvm은 "ccvm"으로 선택할 수 있다.
	TargetHostnames []string `json:"target_hostnames,omitempty" example:"scvm1,scvm2"`
	// 내부 bootstrap 스크립트 실행용 libvirt domain.
	ScriptDomain string `json:"script_domain,omitempty" swaggerignore:"true"`
	// 내부 bootstrap 스크립트 실행용 인자.
	ScriptArgs []string `json:"script_args,omitempty" swaggerignore:"true"`
	// 내부 bootstrap 스크립트 실행 결과 표시 hostname.
	ScriptHostname string `json:"script_hostname,omitempty" swaggerignore:"true"`
	// 내부 bootstrap 스크립트 실행 대상 물리 host API target.
	ScriptTarget string `json:"script_target,omitempty" swaggerignore:"true"`
}

// BootstrapScriptResult는 host API가 qemu-guest-agent로 VM 내부 bootstrap 스크립트를 실행한 결과이다.
// @name BootstrapScriptResult
type BootstrapScriptResult struct {
	Role     string `json:"role,omitempty" example:"scvm"`
	Hostname string `json:"hostname,omitempty" example:"scvm1"`
	Target   string `json:"target,omitempty" example:"10.10.31.1"`
	Domain   string `json:"domain,omitempty" example:"scvm"`
	Code     int    `json:"code,omitempty" example:"200"`
	Message  string `json:"message,omitempty" example:"ok"`
	Output   string `json:"output,omitempty" example:"bootstrap log tail"`
}

// BootstrapHealthResult는 bootstrap 대상 API health 확인 결과이다.
// @name BootstrapHealthResult
type BootstrapHealthResult struct {
	Role     string `json:"role,omitempty" example:"scvm"`
	Hostname string `json:"hostname,omitempty" example:"scvm1"`
	Target   string `json:"target" example:"10.10.31.11"`
	Code     int    `json:"code,omitempty" example:"200"`
	Message  string `json:"message,omitempty" example:"ok"`
	Attempts int    `json:"attempts,omitempty" example:"2"`
}

// BootstrapResponse는 SCVM/CCVM bootstrap 실행 결과이다.
// @name BootstrapResponse
type BootstrapResponse struct {
	Code          int                     `json:"code" example:"200"`
	Message       string                  `json:"message,omitempty" example:"scvm_bootstrap success"`
	Role          string                  `json:"role,omitempty" example:"scvm"`
	Script        []BootstrapScriptResult `json:"script,omitempty"`
	Health        []BootstrapHealthResult `json:"health,omitempty"`
	LicenseApply  *LicenseApplyResponse   `json:"license_apply,omitempty"`
	LicenseStatus *LicenseApplyResponse   `json:"license_status,omitempty"`
}
