package cube

// TimeServerRequest는 chrony 시간 서버 구성 요청 본문이다.
// @name TimeServerRequest
type TimeServerRequest struct {
	// local time servers. 비어 있으면 cluster.json hosts 중 index 1,2의 ablecube IP를 사용한다.
	TimeServers []string `json:"time_servers,omitempty" example:"10.10.31.1,10.10.31.2"`
	// external time server. 비어 있으면 cluster.json의 clusterConfig.external_timeserver 값을 사용한다.
	ExternalTimeserver string `json:"external_timeserver,omitempty" example:"time.google.com"`
}

// TimeServerConfig는 적용된 chrony 시간 서버 구성 정보이다.
// @name TimeServerConfig
type TimeServerConfig struct {
	ConfigPath          string   `json:"config_path" example:"/etc/chrony.conf"`
	Mode                string   `json:"mode" example:"internal"`
	SelfIndex           string   `json:"self_index,omitempty" example:"3"`
	TimeServers         []string `json:"time_servers,omitempty" example:"10.10.31.1,10.10.31.2"`
	AppliedTimeServers  []string `json:"applied_time_servers,omitempty" example:"10.10.31.1,10.10.31.2"`
	ExternalTimeserver  string   `json:"external_timeserver,omitempty" example:"time.google.com"`
	LocalStratumEnabled bool     `json:"local_stratum_enabled" example:"true"`
	Restarted           bool     `json:"restarted" example:"true"`
}

// TimeServerResponse는 chrony 시간 서버 구성 API 응답이다.
// @name TimeServerResponse
type TimeServerResponse struct {
	Code    int    `json:"code" example:"200"`
	Val     any    `json:"val,omitempty"`
	Message string `json:"message,omitempty" example:"ok"`
}
