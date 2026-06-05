package cube

import (
	"net/http"
)

func runClusterConfigApplyForDeploy(req ClusterConfigApplyRequest) ClusterConfigApplyResponse {
	if err := req.Normalize(); err != nil {
		return ClusterConfigApplyResponse{Code: http.StatusBadRequest, Message: err.Error()}
	}
	if err := requireInsertFields(req); err != nil {
		return ClusterConfigApplyResponse{Code: http.StatusBadRequest, Message: err.Error()}
	}
	if err := ensureInsertAddContext(&req); err != nil {
		return ClusterConfigApplyResponse{Code: http.StatusInternalServerError, Message: "failed to read cluster.json"}
	}
	if err := ensureInsertAllHosts(&req); err != nil {
		return ClusterConfigApplyResponse{Code: http.StatusInternalServerError, Message: "failed to read cluster.json"}
	}
	if err := requirePCSClusterList(req); err != nil {
		return ClusterConfigApplyResponse{Code: http.StatusBadRequest, Message: err.Error()}
	}
	if err := ensureHostsFromClusterConfig(req.Action, &req); err != nil {
		return ClusterConfigApplyResponse{Code: http.StatusInternalServerError, Message: "failed to read cluster.json"}
	}
	if err := ensureTypeFromClusterConfig(req.Action, &req); err != nil {
		return ClusterConfigApplyResponse{Code: http.StatusInternalServerError, Message: "failed to read cluster.json"}
	}
	if err := ensureClusterInternalToken(&req); err != nil {
		return ClusterConfigApplyResponse{Code: http.StatusInternalServerError, Message: err.Error()}
	}

	targets, err := buildClusterTargets(req)
	if err != nil {
		return ClusterConfigApplyResponse{Code: http.StatusBadRequest, Message: err.Error()}
	}
	if len(targets) == 0 && (isResetAction(req.Action) || isRemoveAction(req.Action) || isCheckAction(req.Action)) {
		targets, err = buildClusterTargetsFromClusterConfig(req.Action)
		if err != nil {
			return ClusterConfigApplyResponse{Code: http.StatusInternalServerError, Message: "failed to read cluster.json"}
		}
	}
	if isInsertAction(req.Action) && normalizeOption(req.Option) == "local" {
		targets = []string{localTarget()}
	}
	if len(targets) == 0 {
		return ClusterConfigApplyResponse{Code: http.StatusBadRequest, Message: "no targets to apply"}
	}

	req.CopyOption = "hostOnly"
	results := applyWithoutProbe(targets, req)
	resp := ClusterConfigApplyResponse{
		Code:    http.StatusOK,
		Message: "apply success",
		Results: results,
	}
	if failed := firstFailedClusterApplyResult(results); failed != nil {
		resp.Code = http.StatusInternalServerError
		resp.Message = "apply failed"
	}
	return resp
}
