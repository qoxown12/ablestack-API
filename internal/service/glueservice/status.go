package glueservice

import "context"

// Status는 SCVM 로컬 Ceph cluster 상태를 조회한다.
func Status(ctx context.Context) (any, error) {
	return runJSON(ctx, "ceph", "-s", "-f", "json")
}

// Versions는 Ceph daemon version 정보를 조회한다.
func Versions(ctx context.Context) (any, error) {
	return runJSON(ctx, "ceph", "versions")
}

// ListHosts는 Ceph orchestrator에 등록된 host 목록을 조회한다.
func ListHosts(ctx context.Context) (any, error) {
	return runJSON(ctx, "ceph", "orch", "host", "ls", "-f", "json")
}
