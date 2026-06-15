package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mcp-runtime-control/internal/platformclient"
	"mcp-runtime-control/internal/runtimeapi"
	"mcp-runtime/pkg/metadata"
)

type Dependencies struct {
	Platform  *platformclient.Client
	Runtime   *runtimeapi.RuntimeServer
	WriteJSON func(http.ResponseWriter, int, any)
}

func HandleOperations(w http.ResponseWriter, r *http.Request, deps Dependencies) {
	if deps.Platform == nil || !deps.Platform.Configured() {
		deps.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity client not configured"})
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("allow", "GET")
		deps.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	filter, err := adminOperationsFilterFromRequest(r)
	if err != nil {
		deps.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	snapshot, err := deps.Platform.OperationsSnapshot(r.Context(), filter)
	if err != nil {
		deps.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load operations snapshot"})
		return
	}

	deployments := []map[string]any{}
	if deps.Runtime != nil && deps.Runtime.KubernetesAvailable() {
		deployments, err = deps.Runtime.ListAdminDeploymentSummaries(r.Context(), "")
		if err != nil {
			deps.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list deployments"})
			return
		}
		deployments = filterDeploymentsForOperations(deployments, filter)
		snapshot.Images = mergeDeploymentImageActivity(snapshot.Images, deployments, filter)
	}

	deps.WriteJSON(w, http.StatusOK, map[string]any{
		"filters":     adminOperationsFilterResponseFor(filter),
		"users":       snapshot.Users,
		"audit_logs":  snapshot.AuditLogs,
		"images":      snapshot.Images,
		"deployments": deployments,
	})
}

func adminOperationsFilterFromRequest(r *http.Request) (platformclient.OperationsFilter, error) {
	user := strings.TrimSpace(r.URL.Query().Get("user"))
	filter := platformclient.OperationsFilter{
		User:       user,
		UserSearch: strings.ToLower(user),
		Limit:      clampInt(queryInt(r, "limit", 50), 1, 200),
	}
	since, err := parseOptionalTimeQuery(r, "since", false)
	if err != nil {
		return platformclient.OperationsFilter{}, err
	}
	until, err := parseOptionalTimeQuery(r, "until", true)
	if err != nil {
		return platformclient.OperationsFilter{}, err
	}
	filter.Since = since
	filter.Until = until
	if !filter.Since.IsZero() && !filter.Until.IsZero() && filter.Since.After(filter.Until) {
		return platformclient.OperationsFilter{}, errInvalidTimeRange
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

func adminOperationsFilterResponseFor(filter platformclient.OperationsFilter) platformclient.OperationsFilterResponse {
	out := platformclient.OperationsFilterResponse{
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

func mergeDeploymentImageActivity(items []platformclient.ImageActivity, deployments []map[string]any, filter platformclient.OperationsFilter) []platformclient.ImageActivity {
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
		items = append(items, platformclient.ImageActivity{
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

func filterDeploymentsForOperations(deployments []map[string]any, filter platformclient.OperationsFilter) []map[string]any {
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

func matchesOperationsFilter(filter platformclient.OperationsFilter, timestamp time.Time, values ...string) bool {
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

func adminOperationsUserSearch(filter platformclient.OperationsFilter) string {
	if filter.UserSearch != "" {
		return filter.UserSearch
	}
	return strings.ToLower(strings.TrimSpace(filter.User))
}

func operationsLimit(filter platformclient.OperationsFilter) int {
	if filter.Limit <= 0 {
		return 50
	}
	return filter.Limit
}

func sanitizeImageActivity(items []platformclient.ImageActivity) []platformclient.ImageActivity {
	out := make([]platformclient.ImageActivity, 0, len(items))
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
