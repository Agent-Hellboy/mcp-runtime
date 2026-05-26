package runtimeapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HandleRuntimeNamespaces lists namespaces visible to the caller, including catalog namespace entries for the current platform mode.
func (s *RuntimeServer) HandleRuntimeNamespaces(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "platform identity database not configured")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("allow", http.MethodGet)
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if p.Role == roleAdmin {
		namespaces, err := s.platform.ListNamespaces(ctx)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "failed to list namespaces")
			return
		}
		namespaces = appendCatalogNamespaceEntries(namespaces)
		writeJSON(w, http.StatusOK, map[string]any{"namespaces": namespaces})
		return
	}

	entries := make([]map[string]any, 0, len(p.AllowedNamespaces))
	for _, namespace := range catalogNamespacesForPrincipal(p) {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			continue
		}
		if isModeCatalogNamespace(namespace) {
			entries = append(entries, catalogNamespaceEntry(namespace))
			continue
		}
		entry := map[string]any{
			"namespace": namespace,
			"is_shared": namespace == sharedCatalogNamespace,
		}
		for _, team := range p.Teams {
			if strings.TrimSpace(team.Namespace) == namespace {
				entry["team_id"] = team.ID
				entry["team_slug"] = team.Slug
				entry["team_name"] = team.Name
				entry["team_role"] = team.Role
				entry["scope"] = namespaceScopeTeam
			}
		}
		if _, ok := entry["scope"]; !ok {
			entry["scope"] = namespaceScopeUser
		}
		entries = append(entries, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespaces": entries})
}

// HandleRuntimeNamespaceItem returns one visible namespace record or synthetic catalog namespace entry.
func (s *RuntimeServer) HandleRuntimeNamespaceItem(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "platform identity database not configured")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("allow", http.MethodGet)
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	namespace := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/runtime/namespaces/"))
	namespace = strings.Trim(namespace, "/")
	if namespace == "" {
		writeAPIError(w, http.StatusBadRequest, "namespace required")
		return
	}

	if p.Role != roleAdmin && !principalCanReadNamespace(p, namespace) {
		writeAPIError(w, http.StatusForbidden, "forbidden")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	item, ok, err := s.platform.GetNamespace(ctx, namespace)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to fetch namespace")
		return
	}
	if ok {
		writeJSON(w, http.StatusOK, map[string]any{"namespace": item})
		return
	}
	if namespace == sharedCatalogNamespace || isModeCatalogNamespace(namespace) {
		entry := catalogNamespaceEntry(namespace)
		if namespace == sharedCatalogNamespace && !sharedCatalogWritableForUsers() {
			entry["scope"] = "shared"
			entry["is_public"] = false
		}
		writeJSON(w, http.StatusOK, map[string]any{"namespace": entry})
		return
	}
	writeAPIError(w, http.StatusNotFound, "namespace not found")
}

func catalogNamespaceEntry(namespace string) map[string]any {
	scope := "shared"
	isPublic := false
	scopeName := "Shared catalog"
	if namespace != sharedCatalogNamespace {
		switch PlatformMode() {
		case platformModePublic:
			scope = "public"
			isPublic = true
			scopeName = "Public preview"
		case platformModeOrg:
			scope = "org"
			scopeName = "Organization"
		}
	}
	return map[string]any{
		"namespace":  namespace,
		"is_shared":  namespace == sharedCatalogNamespace,
		"is_public":  isPublic,
		"scope":      scope,
		"scope_name": scopeName,
	}
}

func appendCatalogNamespaceEntries(namespaces []map[string]any) []map[string]any {
	seen := map[string]struct{}{}
	for _, entry := range namespaces {
		if namespace := strings.TrimSpace(fmt.Sprint(entry["namespace"])); namespace != "" {
			seen[namespace] = struct{}{}
		}
	}
	catalogNamespaces := append([]string{sharedCatalogNamespace}, modeCatalogNamespaces()...)
	for _, namespace := range dedupeNonEmptyStrings(catalogNamespaces) {
		if _, ok := seen[namespace]; ok {
			continue
		}
		namespaces = append(namespaces, catalogNamespaceEntry(namespace))
	}
	return namespaces
}
