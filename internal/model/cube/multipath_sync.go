package cube

// MultipathSyncRequest는 multipath 설정 동기화/재스캔 요청 본문이다.
// @name MultipathSyncRequest
type MultipathSyncRequest struct {
	// action: sync/rescan
	Action string `json:"action" example:"sync"`
	// explicit target IPs. If empty, clusterConfig.hosts[].ablecube is used.
	Targets []string `json:"targets,omitempty" example:"10.10.31.1,10.10.31.2"`
	// explicit target hostnames from cluster.json hosts[].hostname.
	TargetHostnames []string `json:"target_hostnames,omitempty" example:"ablecube31-1,ablecube31-2"`
	// internal copy of /etc/multipath/bindings
	Bindings string `json:"bindings,omitempty" swaggerignore:"true"`
	// internal copy of /etc/multipath/wwids
	WWIDs string `json:"wwids,omitempty" swaggerignore:"true"`
	// internal marker that source files were loaded by the orchestrator.
	SourceProvided bool `json:"source_provided,omitempty" swaggerignore:"true"`
}

// MultipathSyncStepResult는 호스트 내부의 단계별 실행 결과이다.
// @name MultipathSyncStepResult
type MultipathSyncStepResult struct {
	Name    string `json:"name" example:"rescan_scsi"`
	Status  string `json:"status" example:"succeeded"`
	Message string `json:"message,omitempty" example:"ok"`
	Output  string `json:"output,omitempty"`
}

// MultipathSyncTargetResult는 대상 호스트별 multipath 동기화 결과이다.
// @name MultipathSyncTargetResult
type MultipathSyncTargetResult struct {
	Hostname string                    `json:"hostname,omitempty" example:"ablecube31-1"`
	Target   string                    `json:"target" example:"10.10.31.1"`
	Code     int                       `json:"code" example:"200"`
	Message  string                    `json:"message,omitempty" example:"ok"`
	Steps    []MultipathSyncStepResult `json:"steps,omitempty"`
}

// MultipathSyncResponse는 multipath 동기화/재스캔 결과이다.
// @name MultipathSyncResponse
type MultipathSyncResponse struct {
	Code    int                         `json:"code" example:"200"`
	Message string                      `json:"message,omitempty" example:"ok"`
	Action  string                      `json:"action,omitempty" example:"sync"`
	Target  string                      `json:"target,omitempty" example:"fanout"`
	Results []MultipathSyncTargetResult `json:"results,omitempty"`
	Steps   []MultipathSyncStepResult   `json:"steps,omitempty"`
}
