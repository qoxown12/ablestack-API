package cube

// URLResponse describes the response body for URL APIs.
// @name URLResponse
type URLResponse struct {
	Code int `json:"code" example:"200"`
	Val  any `json:"val"`
}
