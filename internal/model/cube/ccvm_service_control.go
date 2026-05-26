package cube

// CCVMServiceControlRequest describes the request body for CCVM service control.
// @name CCVMServiceControlRequest
type CCVMServiceControlRequest struct {
	// action: start/restart/stop/status
	Action string `json:"action" example:"start"`
	// service name
	ServiceName string `json:"service_name" example:"mold"`
}

// CCVMServiceControlResponse describes the response body for CCVM service control.
// @name CCVMServiceControlResponse
type CCVMServiceControlResponse struct {
	Code int    `json:"code" example:"200"`
	Val  string `json:"val" example:"mold service start control success"`
}
