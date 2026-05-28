package runtimehandlers

import (
	"net/http"

	"mcp-sentinel-api/internal/runtimeapi"
)

// HandleDashboardSummary routes the admin dashboard summary through the runtime API server.
func HandleDashboardSummary(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleDashboardSummary(w, r)
}

// HandleRuntimeServers routes runtime server collection requests through the runtime API server.
func HandleRuntimeServers(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleRuntimeServers(w, r)
}

// HandleRuntimeServerItem routes runtime server item requests through the runtime API server.
func HandleRuntimeServerItem(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleRuntimeServerItem(w, r)
}

// HandleRuntimeServerEvents routes runtime server event requests through the runtime API server.
func HandleRuntimeServerEvents(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleRuntimeServerEvents(w, r)
}

// HandleRuntimeTeams routes runtime team collection requests through the runtime API server.
func HandleRuntimeTeams(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleRuntimeTeams(w, r)
}

// HandleRuntimeTeamItemPath routes runtime team item requests through the runtime API server.
func HandleRuntimeTeamItemPath(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleRuntimeTeamItemPath(w, r)
}

// HandleRuntimeNamespaces routes runtime namespace collection requests through the runtime API server.
func HandleRuntimeNamespaces(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleRuntimeNamespaces(w, r)
}

// HandleRuntimeNamespaceItem routes runtime namespace item requests through the runtime API server.
func HandleRuntimeNamespaceItem(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleRuntimeNamespaceItem(w, r)
}

// HandleRuntimeGrants routes runtime grant collection requests through the runtime API server.
func HandleRuntimeGrants(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleRuntimeGrants(w, r)
}

// HandleRuntimeSessions routes runtime session collection requests through the runtime API server.
func HandleRuntimeSessions(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleRuntimeSessions(w, r)
}

// HandleAdapterSession routes adapter session requests through the runtime API server.
func HandleAdapterSession(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleAdapterSession(w, r)
}

// HandleRuntimeRegistryPush routes registry push requests through the runtime API server.
func HandleRuntimeRegistryPush(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleRuntimeRegistryPush(w, r)
}

// HandleRuntimeComponents routes runtime component requests through the runtime API server.
func HandleRuntimeComponents(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleRuntimeComponents(w, r)
}

// HandleRuntimePolicy routes runtime policy requests through the runtime API server.
func HandleRuntimePolicy(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleRuntimePolicy(w, r)
}

// HandleActionRestart routes restart actions through the runtime API server.
func HandleActionRestart(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleActionRestart(w, r)
}

// HandleGrantItemPath routes grant item requests through the runtime API server.
func HandleGrantItemPath(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleGrantItemPath(w, r)
}

// HandleSessionItemPath routes session item requests through the runtime API server.
func HandleSessionItemPath(server *runtimeapi.RuntimeServer, w http.ResponseWriter, r *http.Request) {
	server.HandleSessionItemPath(w, r)
}
