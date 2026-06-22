package deployments

import (
	"net/http"

	"mcp-runtime-api/internal/runtimeapi"
)

// HandleDeployments routes deployment collection requests through the deployment service.
func HandleDeployments(service *runtimeapi.DeploymentService, w http.ResponseWriter, r *http.Request) {
	service.HandleDeployments(w, r)
}

// HandleDeploymentItem routes deployment item requests through the deployment service.
func HandleDeploymentItem(service *runtimeapi.DeploymentService, w http.ResponseWriter, r *http.Request) {
	service.HandleDeploymentItem(w, r)
}

// HandleAdminDeployments routes admin deployment listing through the deployment service.
func HandleAdminDeployments(service *runtimeapi.DeploymentService, w http.ResponseWriter, r *http.Request) {
	service.HandleAdminDeployments(w, r)
}
