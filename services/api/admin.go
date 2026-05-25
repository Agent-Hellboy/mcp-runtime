package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"mcp-runtime/pkg/metadata"
)

func (s *apiServer) handleAdminNamespaces(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	namespaces, err := s.platform.ListNamespaces(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list namespaces"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespaces": namespaces})
}

func (s *apiServer) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	filter, err := adminOperationsFilterFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	auditLogs, err := s.platform.ListAuditLogs(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list audit logs"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit_logs": auditLogs})
}

func (s *apiServer) handleAdminOperations(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	filter, err := adminOperationsFilterFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	users, err := s.platform.ListUserActivity(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list users"})
		return
	}
	auditLogs, err := s.platform.ListAuditLogs(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list audit logs"})
		return
	}
	images, err := s.platform.ListImageActivity(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list image activity"})
		return
	}

	deployments := []map[string]any{}
	if s.runtime != nil && s.runtime.KubernetesAvailable() {
		deployments, err = s.runtime.ListAdminDeploymentSummaries(r.Context(), "")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list deployments"})
			return
		}
		deployments = filterDeploymentsForOperations(deployments, filter)
		images = mergeDeploymentImageActivity(images, deployments, filter)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"filters":     adminOperationsFilterResponseFor(filter),
		"users":       users,
		"audit_logs":  auditLogs,
		"images":      images,
		"deployments": deployments,
	})
}

func adminOperationsFilterFromRequest(r *http.Request) (adminOperationsFilter, error) {
	user := strings.TrimSpace(r.URL.Query().Get("user"))
	filter := adminOperationsFilter{
		User:       user,
		UserSearch: strings.ToLower(user),
		Limit:      clampInt(queryInt(r, "limit", 50), 1, 200),
	}
	since, err := parseOptionalTimeQuery(r, "since", false)
	if err != nil {
		return adminOperationsFilter{}, err
	}
	until, err := parseOptionalTimeQuery(r, "until", true)
	if err != nil {
		return adminOperationsFilter{}, err
	}
	filter.Since = since
	filter.Until = until
	if !filter.Since.IsZero() && !filter.Until.IsZero() && filter.Since.After(filter.Until) {
		return adminOperationsFilter{}, errInvalidTimeRange
	}
	return filter, nil
}

var errInvalidTimeRange = adminFilterError("since must be before until")

type adminFilterError string

func (e adminFilterError) Error() string {
	return string(e)
}

func parseOptionalTimeQuery(r *http.Request, key string, endOfDay bool) (time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		if endOfDay {
			return parsed.Add(24*time.Hour - time.Nanosecond), nil
		}
		return parsed, nil
	}
	return time.Time{}, adminFilterError(key + " must be RFC3339 or YYYY-MM-DD")
}

func adminOperationsFilterResponseFor(filter adminOperationsFilter) adminOperationsFilterResponse {
	out := adminOperationsFilterResponse{
		User:  filter.User,
		Limit: filter.Limit,
	}
	if !filter.Since.IsZero() {
		out.Since = filter.Since.UTC().Format(time.RFC3339)
	}
	if !filter.Until.IsZero() {
		out.Until = filter.Until.UTC().Format(time.RFC3339)
	}
	return out
}

func mergeDeploymentImageActivity(items []platformImageActivity, deployments []map[string]any, filter adminOperationsFilter) []platformImageActivity {
	limit := operationsLimit(filter)
	if len(items) >= limit {
		return sanitizeImageActivity(items[:limit])
	}
	seen := map[string]struct{}{}
	for _, item := range items {
		key := strings.Join([]string{item.UserID, item.Namespace, item.ImageRef, item.DeploymentTarget}, "\x00")
		seen[key] = struct{}{}
	}
	for _, deployment := range deployments {
		imageRef := strings.TrimSpace(stringFromMap(deployment, "image"))
		if imageRef == "" {
			continue
		}
		userID := strings.TrimSpace(stringFromMap(deployment, "user_id"))
		namespace := strings.TrimSpace(stringFromMap(deployment, "namespace"))
		name := strings.TrimSpace(stringFromMap(deployment, "name"))
		target := namespace + "/" + name
		createdAt, _ := timeFromMap(deployment, "created_at")
		if !matchesOperationsFilter(filter, createdAt, userID, stringFromMap(deployment, "created_by"), namespace, name, imageRef, name, target) {
			continue
		}
		key := strings.Join([]string{userID, namespace, imageRef, target}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, platformImageActivity{
			UserID:           userID,
			Namespace:        namespace,
			ImageRef:         imageRef,
			ServerName:       name,
			DeploymentTarget: target,
			Action:           "deployment_current",
			Status:           "active",
			Source:           "kubernetes",
			CreatedAt:        createdAt,
		})
		if len(items) >= limit {
			break
		}
	}
	return sanitizeImageActivity(items)
}

func filterDeploymentsForOperations(deployments []map[string]any, filter adminOperationsFilter) []map[string]any {
	limit := operationsLimit(filter)
	out := make([]map[string]any, 0, min(len(deployments), limit))
	for _, deployment := range deployments {
		namespace := strings.TrimSpace(stringFromMap(deployment, "namespace"))
		name := strings.TrimSpace(stringFromMap(deployment, "name"))
		imageRef := strings.TrimSpace(stringFromMap(deployment, "image"))
		target := namespace + "/" + name
		createdAt, _ := timeFromMap(deployment, "created_at")
		if !matchesOperationsFilter(filter, createdAt, stringFromMap(deployment, "user_id"), stringFromMap(deployment, "created_by"), namespace, name, imageRef, name, target) {
			continue
		}
		out = append(out, deployment)
		if len(out) >= limit {
			break
		}
	}
	return sanitizeDeploymentSummaries(out)
}

func matchesOperationsFilter(filter adminOperationsFilter, timestamp time.Time, values ...string) bool {
	if !filter.Since.IsZero() && !timestamp.IsZero() && timestamp.Before(filter.Since) {
		return false
	}
	if !filter.Until.IsZero() && !timestamp.IsZero() && timestamp.After(filter.Until) {
		return false
	}
	user := adminOperationsUserSearch(filter)
	if user == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(strings.TrimSpace(value)), user) {
			return true
		}
	}
	return false
}

func operationsLimit(filter adminOperationsFilter) int {
	if filter.Limit <= 0 {
		return 50
	}
	return filter.Limit
}

func sanitizeImageActivity(items []platformImageActivity) []platformImageActivity {
	out := make([]platformImageActivity, 0, len(items))
	for _, item := range items {
		item.ImageRef = metadata.DisplayImageReference(item.ImageRef)
		item.SourceImage = metadata.DisplayImageReference(item.SourceImage)
		out = append(out, item)
	}
	return out
}

func sanitizeDeploymentSummaries(items []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		cloned := make(map[string]any, len(item))
		for k, v := range item {
			cloned[k] = v
		}
		if imageRef := strings.TrimSpace(stringFromMap(cloned, "image")); imageRef != "" {
			cloned["image"] = metadata.DisplayImageReference(imageRef)
		}
		out = append(out, cloned)
	}
	return out
}

func stringFromMap(values map[string]any, key string) string {
	switch v := values[key].(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func timeFromMap(values map[string]any, key string) (time.Time, bool) {
	switch v := values[key].(type) {
	case time.Time:
		return v, true
	case string:
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
