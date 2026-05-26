package cube

// CCVMSecondaryResizeRequest는 CCVM secondary 용량 추가 요청 본문이다.
// @name CCVMSecondaryResizeRequest
type CCVMSecondaryResizeRequest struct {
	// additional capacity size in GiB, 1~500
	AddSize int `json:"add_size" example:"100"`
}

// CCVMSecondaryResizeResponse는 CCVM secondary 용량 추가 결과이다.
// @name CCVMSecondaryResizeResponse
type CCVMSecondaryResizeResponse struct {
	Code    int    `json:"code" example:"200"`
	Val     string `json:"val" example:"CCVM secondary filesystem expansion success."`
	RetName string `json:"retname,omitempty" example:"CCVM Secondary Resize"`
	Message string `json:"message,omitempty" example:"ok"`
	Target  string `json:"target,omitempty" example:"10.10.31.1"`
	OSType  string `json:"os_type,omitempty" example:"ablestack-hci"`
}
