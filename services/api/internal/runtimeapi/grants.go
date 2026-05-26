package runtimeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	sentinelaccess "mcp-runtime/pkg/access"
	"mcp-runtime/pkg/k8sclient"
	"mcp-runtime/pkg/serviceutil"
	runtimeaccess "mcp-sentinel-api/internal/runtimeapi/access"
)

type accessGrantRequest struct {
	Name               string                          `json:"name"`
	Namespace          string                          `json:"namespace"`
	ServerRef          sentinelaccess.ServerReference  `json:"serverRef"`
	Subject            sentinelaccess.SubjectRef       `json:"subject"`
	MaxTrust           sentinelaccess.TrustLevel       `json:"maxTrust"`
	AllowedSideEffects []sentinelaccess.ToolSideEffect `json:"allowedSideEffects"`
	PolicyVersion      string                          `json:"policyVersion"`
	Disabled           *bool                           `json:"disabled,omitempty"`
	ToolRules          []sentinelaccess.ToolRule       `json:"toolRules"`
}

type accessGrantPatchRequest struct {
	Disabled *bool `json:"disabled,omitempty"`
}

// HandleRuntimeGrants lists and applies MCPAccessGrant resources within the caller's allowed namespace scope.
func (s *RuntimeServer) HandleRuntimeGrants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRuntimeGrantList(w, r)
	case http.MethodPost:
		s.handleRuntimeGrantApply(w, r)
	default:
		w.Header().Set("allow", "GET, POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

// HandleGrantItemPath handles GET, DELETE, PATCH, and legacy enable or disable actions for one MCPAccessGrant.
func (s *RuntimeServer) HandleGrantItemPath(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ns, name, err := runtimeaccess.ExtractNamespacedPath(r.URL.Path, "/api/runtime/grants/", 2)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.handleGrantGet(w, r, ns, name)
		return
	case http.MethodDelete:
		ns, name, err := serviceutil.ExtractNamespacedResourceDelete(r, "/api/runtime/grants/")
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.handleGrantDelete(w, r, ns, name)
		return
	case http.MethodPatch:
		ns, name, err := runtimeaccess.ExtractNamespacedPath(r.URL.Path, "/api/runtime/grants/", 2)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.handleGrantPatch(w, r, ns, name)
		return
	case http.MethodPost:
		s.handleGrantPostTogglePath(w, r)
		return
	default:
		w.Header().Set("allow", "GET, POST, PATCH, DELETE")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

func (s *RuntimeServer) handleGrantGet(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if s.accessMgr == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "kubernetes not available")
		return
	}
	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), namespace)
	if err != nil {
		writeAPIError(w, http.StatusForbidden, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	grant, err := s.accessMgr.GetGrant(ctx, name, namespace)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		writeAPIError(w, code, msg)
		return
	}
	if !s.grantVisibleToPrincipal(ctx, *grant) {
		writeAPIError(w, http.StatusForbidden, "forbidden server")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grant": sentinelaccess.ToGrantSummary(*grant)})
}

func (s *RuntimeServer) handleGrantDelete(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if s.accessMgr == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "kubernetes not available")
		return
	}
	namespace, err := s.scopedAccessWriteNamespaceForPrincipal(r.Context(), namespace)
	if err != nil {
		writeAPIError(w, http.StatusForbidden, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	grant, err := s.accessMgr.GetGrant(ctx, name, namespace)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		log.Printf("delete grant %s/%s failed before authz (status=%d): %v", namespace, name, code, err)
		writeAPIError(w, code, fmt.Sprintf("failed to delete grant: %s", msg))
		return
	}
	allowed, err := s.canAdministerAccessServerRef(ctx, namespace, grant.Spec.ServerRef)
	if err != nil {
		log.Printf("delete grant %s/%s authz lookup failed: %v", namespace, name, err)
		writeAPIError(w, http.StatusInternalServerError, "failed to verify server reference")
		return
	}
	if !allowed {
		writeAPIError(w, http.StatusForbidden, "forbidden server")
		return
	}
	if err := s.accessMgr.DeleteGrant(ctx, name, namespace); err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		log.Printf("delete grant %s/%s failed (status=%d): %v", namespace, name, code, err)
		writeAPIError(w, code, fmt.Sprintf("failed to delete grant: %s", msg))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"name":      name,
		"namespace": namespace,
	})
}

func (s *RuntimeServer) handleGrantPatch(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if s.accessMgr == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "kubernetes not available")
		return
	}

	namespace, err := s.scopedAccessWriteNamespaceForPrincipal(r.Context(), namespace)
	if err != nil {
		writeAPIError(w, http.StatusForbidden, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	grant, err := s.accessMgr.GetGrant(ctx, name, namespace)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		writeAPIError(w, code, msg)
		return
	}
	allowed, err := s.canAdministerAccessServerRef(ctx, namespace, grant.Spec.ServerRef)
	if err != nil {
		log.Printf("patch grant %s/%s authz lookup failed: %v", namespace, name, err)
		writeAPIError(w, http.StatusInternalServerError, "failed to verify server reference")
		return
	}
	if !allowed {
		writeAPIError(w, http.StatusForbidden, "forbidden server")
		return
	}

	var req accessGrantPatchRequest
	r.Body = http.MaxBytesReader(w, r.Body, accessApplyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	if req.Disabled == nil {
		writeAPIError(w, http.StatusBadRequest, "disabled is required")
		return
	}
	s.handleGrantStateChange(w, ctx, namespace, name, *req.Disabled)
}

// handleGrantPostTogglePath handles legacy POST /api/runtime/grants/{namespace}/{name}/disable|enable.
func (s *RuntimeServer) handleGrantPostTogglePath(w http.ResponseWriter, r *http.Request) {
	params, err := serviceutil.ExtractGrantActionParams(r, "/api/runtime/grants/")
	if err != nil {
		if errors.Is(err, serviceutil.ErrMethodNotAllowed) {
			writeAPIError(w, http.StatusMethodNotAllowed, err.Error())
		} else {
			writeAPIError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	disable := !serviceutil.IsActionEnabled(params.Action)
	s.handleGrantToggle(w, r, params.Namespace, params.Name, disable)
}

func (s *RuntimeServer) handleGrantToggle(w http.ResponseWriter, r *http.Request, namespace, name string, disable bool) {
	if s.accessMgr == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "kubernetes not available")
		return
	}

	namespace, nsErr := s.scopedAccessWriteNamespaceForPrincipal(r.Context(), namespace)
	if nsErr != nil {
		writeAPIError(w, http.StatusForbidden, nsErr.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	grant, err := s.accessMgr.GetGrant(ctx, name, namespace)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		writeAPIError(w, code, msg)
		return
	}
	allowed, err := s.canAdministerAccessServerRef(ctx, namespace, grant.Spec.ServerRef)
	if err != nil {
		log.Printf("toggle grant %s/%s authz lookup failed: %v", namespace, name, err)
		writeAPIError(w, http.StatusInternalServerError, "failed to verify server reference")
		return
	}
	if !allowed {
		writeAPIError(w, http.StatusForbidden, "forbidden server")
		return
	}

	s.handleGrantStateChange(w, ctx, namespace, name, disable)
}

func (s *RuntimeServer) handleGrantStateChange(w http.ResponseWriter, ctx context.Context, namespace, name string, disable bool) {
	var updateErr error
	if disable {
		updateErr = s.accessMgr.DisableGrant(ctx, name, namespace)
	} else {
		updateErr = s.accessMgr.EnableGrant(ctx, name, namespace)
	}
	if updateErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to update grant")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"name":      name,
		"namespace": namespace,
		"disabled":  disable,
	})
}
