package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mcp-platform-api/internal/httperrors"
	"mcp-platform-api/internal/platformstore"
	"mcp-runtime/pkg/metadata"
)

type Dependencies struct {
	Platform  *platformstore.Store
	WriteJSON func(http.ResponseWriter, int, any)
}

func HandleNamespaces(w http.ResponseWriter, r *http.Request, deps Dependencies) {
	if deps.Platform == nil {
		httperrors.PlatformUnavailable(w)
		return
	}
	if r.Method != http.MethodGet {
		httperrors.MethodNotAllowed(w, "GET")
		return
	}
	namespaces, err := deps.Platform.ListNamespaces(r.Context())
	if err != nil {
		httperrors.QueryFailed(w, "failed to list namespaces")
		return
	}
	deps.WriteJSON(w, http.StatusOK, map[string]any{"namespaces": namespaces})
}

func HandleAudit(w http.ResponseWriter, r *http.Request, deps Dependencies) {
	if deps.Platform == nil {
		httperrors.PlatformUnavailable(w)
		return
	}
	if r.Method != http.MethodGet {
		httperrors.MethodNotAllowed(w, "GET")
		return
	}
	filter, err := adminOperationsFilterFromRequest(r)
	if err != nil {
		httperrors.InvalidQuery(w, err.Error())
		return
	}
	auditLogs, err := deps.Platform.ListAuditLogs(r.Context(), filter)
	if err != nil {
		httperrors.QueryFailed(w, "failed to list audit logs")
		return
	}
	deps.WriteJSON(w, http.StatusOK, map[string]any{"audit_logs": auditLogs})
}

func HandleOperations(w http.ResponseWriter, r *http.Request, deps Dependencies) {
	if deps.Platform == nil {
		httperrors.PlatformUnavailable(w)
		return
	}
	if r.Method != http.MethodGet {
		httperrors.MethodNotAllowed(w, "GET")
		return
	}
	filter, err := adminOperationsFilterFromRequest(r)
	if err != nil {
		httperrors.InvalidQuery(w, err.Error())
		return
	}

	users, err := deps.Platform.ListUserActivity(r.Context(), filter)
	if err != nil {
		httperrors.QueryFailed(w, "failed to list users")
		return
	}
	auditLogs, err := deps.Platform.ListAuditLogs(r.Context(), filter)
	if err != nil {
		httperrors.QueryFailed(w, "failed to list audit logs")
		return
	}
	images, err := deps.Platform.ListImageActivity(r.Context(), filter)
	if err != nil {
		httperrors.QueryFailed(w, "failed to list image activity")
		return
	}

	deps.WriteJSON(w, http.StatusOK, map[string]any{
		"filters":     adminOperationsFilterResponseFor(filter),
		"users":       users,
		"audit_logs":  auditLogs,
		"images":      images,
		"deployments": []map[string]any{},
	})
}

func adminOperationsFilterFromRequest(r *http.Request) (platformstore.OperationsFilter, error) {
	user := strings.TrimSpace(r.URL.Query().Get("user"))
	filter := platformstore.OperationsFilter{
		User:       user,
		UserSearch: strings.ToLower(user),
		Limit:      clampInt(queryInt(r, "limit", 50), 1, 200),
	}
	since, err := parseOptionalTimeQuery(r, "since", false)
	if err != nil {
		return platformstore.OperationsFilter{}, err
	}
	until, err := parseOptionalTimeQuery(r, "until", true)
	if err != nil {
		return platformstore.OperationsFilter{}, err
	}
	filter.Since = since
	filter.Until = until
	if !filter.Since.IsZero() && !filter.Until.IsZero() && filter.Since.After(filter.Until) {
		return platformstore.OperationsFilter{}, errInvalidTimeRange
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

func adminOperationsFilterResponseFor(filter platformstore.OperationsFilter) platformstore.OperationsFilterResponse {
	out := platformstore.OperationsFilterResponse{
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

func mergeDeploymentImageActivity(items []platformstore.ImageActivity, deployments []map[string]any, filter platformstore.OperationsFilter) []platformstore.ImageActivity {
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
		items = append(items, platformstore.ImageActivity{
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

func filterDeploymentsForOperations(deployments []map[string]any, filter platformstore.OperationsFilter) []map[string]any {
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

func matchesOperationsFilter(filter platformstore.OperationsFilter, timestamp time.Time, values ...string) bool {
	if !filter.Since.IsZero() && !timestamp.IsZero() && timestamp.Before(filter.Since) {
		return false
	}
	if !filter.Until.IsZero() && !timestamp.IsZero() && timestamp.After(filter.Until) {
		return false
	}
	user := platformstore.AdminOperationsUserSearch(filter)
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

func operationsLimit(filter platformstore.OperationsFilter) int {
	if filter.Limit <= 0 {
		return 50
	}
	return filter.Limit
}

func sanitizeImageActivity(items []platformstore.ImageActivity) []platformstore.ImageActivity {
	out := make([]platformstore.ImageActivity, 0, len(items))
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
		copyItem := make(map[string]any, len(item))
		for k, v := range item {
			copyItem[k] = v
		}
		if imageRef, ok := copyItem["image"].(string); ok {
			copyItem["image"] = metadata.DisplayImageReference(imageRef)
		}
		out = append(out, copyItem)
	}
	return out
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	if raw, ok := values[key]; ok && raw != nil {
		switch v := raw.(type) {
		case string:
			return v
		case fmt.Stringer:
			return v.String()
		default:
			return fmt.Sprint(v)
		}
	}
	return ""
}

func timeFromMap(values map[string]any, key string) (time.Time, bool) {
	if values == nil {
		return time.Time{}, false
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return time.Time{}, false
	}
	switch v := raw.(type) {
	case time.Time:
		return v, true
	case string:
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func queryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func clampInt(n, minValue, maxValue int) int {
	if n < minValue {
		return minValue
	}
	if n > maxValue {
		return maxValue
	}
	return n
}
