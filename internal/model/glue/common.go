package glue

// Response는 Glue API 공통 응답 본문이다.
// @name GlueResponse
type Response struct {
	Code    int    `json:"code" example:"200"`
	Message string `json:"message,omitempty" example:"ok"`
	Val     any    `json:"val,omitempty"`
}

// NodeRoleStatus는 Glue API 등록/차단에 사용하는 현재 노드 role 판정 결과다.
// @name GlueNodeRoleStatus
type NodeRoleStatus struct {
	Role   string `json:"role" example:"scvm"`
	Source string `json:"source" example:"env"`
	SCVM   bool   `json:"scvm" example:"true"`
	Reason string `json:"reason,omitempty" example:"ABLESTACK_NODE_ROLE=scvm"`
}

// EndpointInfo는 Glue API 루트 응답에 표시할 endpoint 정보를 담는다.
// @name GlueEndpointInfo
type EndpointInfo struct {
	Method      string `json:"method" example:"GET"`
	Path        string `json:"path" example:"/api/v1/glue/pool"`
	Description string `json:"description,omitempty" example:"list pools"`
	Status      string `json:"status" example:"skeleton"`
}

// RootStatus는 Glue API 등록 상태와 현재 SCVM role 판정 결과를 함께 반환한다.
// @name GlueRootStatus
type RootStatus struct {
	Name       string         `json:"name" example:"glue"`
	SCVMOnly   bool           `json:"scvm_only" example:"true"`
	Node       NodeRoleStatus `json:"node"`
	Endpoints  []EndpointInfo `json:"endpoints"`
	Hold       []string       `json:"hold,omitempty" example:"mirror"`
	Deprecated []EndpointInfo `json:"deprecated,omitempty"`
}

// NotImplementedValue는 아직 실제 이식되지 않은 skeleton endpoint에서 반환한다.
// @name GlueNotImplementedValue
type NotImplementedValue struct {
	Method   string `json:"method" example:"GET"`
	Path     string `json:"path" example:"/api/v1/glue/mirror"`
	Module   string `json:"module" example:"mirror"`
	Endpoint string `json:"endpoint" example:"status"`
	Note     string `json:"note" example:"skeleton only; implementation pending"`
}

// GenericRequest는 skeleton Glue endpoint 문서화를 위한 임시 요청 본문이다.
// @name GlueGenericRequest
type GenericRequest struct {
	Action string         `json:"action,omitempty" example:"status"`
	Values map[string]any `json:"values,omitempty"`
}

// ImageRequest는 Glue RBD image 생성/삭제 API에서 사용하는 요청 본문이다.
// @name GlueImageRequest
type ImageRequest struct {
	PoolName  string `json:"pool_name" example:"rbd"`
	ImageName string `json:"image_name" example:"vm01"`
	Size      int64  `json:"size,omitempty" example:"10"`
}

// ServiceControlRequest는 Glue service 제어 API에서 사용하는 요청 본문이다.
// @name GlueServiceControlRequest
type ServiceControlRequest struct {
	Control string `json:"control" example:"restart" enums:"start,stop,restart,redeploy"`
}
