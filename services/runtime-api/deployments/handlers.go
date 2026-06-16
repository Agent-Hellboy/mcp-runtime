package deployments

import (
	"net/http"

	"mcp-runtime-api/internal/runtimeapi"
)

// HandleDeployments routes deployment collection requests through the runtime API server.
func HandleDeployments(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleDeployments(w, r)
}

// HandleDeploymentItem routes deployment item requests through the runtime API server.
func HandleDeploymentItem(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleDeploymentItem(w, r)
}

// HandleAdminDeployments routes admin deployment listing through the runtime API server.
func HandleAdminDeployments(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleAdminDeployments(w, r)
}
