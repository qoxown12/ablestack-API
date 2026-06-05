package glue

// MirrorClusterRequest는 local RBD mirror cluster 설정/수정/삭제 요청이다.
// @name GlueMirrorClusterRequest
type MirrorClusterRequest struct {
	LocalClusterName string   `json:"local_cluster_name" example:"scvm-a"`
	MirrorPool       string   `json:"mirror_pool" example:"rbd"`
	RemoteToken      string   `json:"remote_token,omitempty" example:"eyJmc2lkIjoi..."`
	Hosts            []string `json:"hosts,omitempty" example:"scvm"`
	Interval         string   `json:"interval,omitempty" example:"1h"`
}

// MirrorPoolRequest는 mirror pool enable/disable 요청이다.
// @name GlueMirrorPoolRequest
type MirrorPoolRequest struct {
	LocalClusterName string   `json:"local_cluster_name" example:"scvm-a"`
	RemoteToken      string   `json:"remote_token,omitempty" example:"eyJmc2lkIjoi..."`
	Hosts            []string `json:"hosts,omitempty" example:"scvm"`
	Interval         string   `json:"interval,omitempty" example:"1h"`
}
