package cube

// LicenseRequest describes the request body for license APIs.
// @name LicenseRequest
type LicenseRequest struct {
	// action: status/register
	Action string `json:"action" example:"status"`
	// license file content (base64)
	LicenseContent string `json:"license_content,omitempty" example:"BASE64_CONTENT"`
}

// LicenseStatus describes the license status payload.
// @name LicenseStatus
type LicenseStatus struct {
	Status   string `json:"status" example:"active"`
	Expired  string `json:"expired" example:"2030-01-01"`
	Issued   string `json:"issued" example:"2024-01-01"`
	OEM      string `json:"oem,omitempty" example:"ablecloud"`
	FilePath string `json:"file_path" example:"/usr/share/<uuid>/af864912-402d-425c-8a16-ad0a6efccf61"`
}

// LicenseResponse describes the response body for license APIs.
// @name LicenseResponse
type LicenseResponse struct {
	Code int `json:"code" example:"200"`
	Val  any `json:"val"`
}
