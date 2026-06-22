package runtimeapi

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"mcp-runtime/pkg/controlplane"
)

const (
	defaultActiveServerLimit     = 5
	envActiveServerLimit         = "PLATFORM_MCP_ACTIVE_SERVER_LIMIT"
	envPushCooldown              = "PLATFORM_MCP_PUSH_COOLDOWN"
	platformLastPushAtAnnotation = "platform.mcpruntime.org/last-push-at"
	platformLastPushByAnnotation = "platform.mcpruntime.org/last-push-by"
)

type serverPublishPolicy struct {
	activeServerLimit int
	pushCooldown      time.Duration
}

type serverPublishPolicyStatus struct {
	ActiveServerLimit        int    `json:"active_server_limit"`
	ActiveServerCount        int    `json:"active_server_count"`
	ActiveServerLimitEnabled bool   `json:"active_server_limit_enabled"`
	CanPublish               bool   `json:"can_publish"`
	PushCooldown             string `json:"push_cooldown"`
	PushCooldownSeconds      int64  `json:"push_cooldown_seconds"`
	PushCooldownEnabled      bool   `json:"push_cooldown_enabled"`
}

type serverPublishPolicyRejection struct {
	status        int
	code          string
	message       string
	nextAllowedAt time.Time
	limit         int
	count         int
}

func currentServerPublishPolicy() serverPublishPolicy {
	return serverPublishPolicy{
		activeServerLimit: activeServerLimitFromEnv(),
		pushCooldown:      durationFromEnv(envPushCooldown, 0),
	}
}

func activeServerLimitFromEnv() int {
	raw := strings.TrimSpace(os.Getenv(envActiveServerLimit))
	if raw == "" {
		return defaultActiveServerLimit
	}
	switch strings.ToLower(raw) {
	case "disabled", "off", "false", "none":
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return defaultActiveServerLimit
	}
	if limit < 0 {
		return 0
	}
	return limit
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	switch strings.ToLower(raw) {
	case "disabled", "off", "false", "none", "0":
		return 0
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func (s *DeploymentService) publishPolicyStatusForPrincipal(ctx context.Context, p principal) serverPublishPolicyStatus {
	policy := currentServerPublishPolicy()
	status := serverPublishPolicyStatus{
		ActiveServerLimit:        policy.activeServerLimit,
		ActiveServerLimitEnabled: policy.activeServerLimit > 0,
		CanPublish:               true,
		PushCooldown:             policy.pushCooldown.String(),
		PushCooldownSeconds:      int64(policy.pushCooldown.Seconds()),
		PushCooldownEnabled:      policy.pushCooldown > 0,
	}
	if p.Role == roleAdmin || strings.TrimSpace(p.UserID()) == "" || policy.activeServerLimit <= 0 {
		return status
	}
	count, err := s.countPrincipalActiveServers(ctx, p)
	if err != nil {
		status.CanPublish = false
		return status
	}
	status.ActiveServerCount = count
	status.CanPublish = count < policy.activeServerLimit
	return status
}

func (s *DeploymentService) evaluateServerPublishPolicy(ctx context.Context, p principal, namespace, name string, current *mcpv1alpha1.MCPServer, now time.Time) (*serverPublishPolicyRejection, error) {
	policy := currentServerPublishPolicy()
	if policy.pushCooldown > 0 && current != nil {
		if lastPushedAt, ok := lastServerPushAt(current); ok {
			nextAllowed := lastPushedAt.Add(policy.pushCooldown)
			if now.Before(nextAllowed) {
				return &serverPublishPolicyRejection{
					status:        http.StatusTooManyRequests,
					code:          "server_push_cooldown_active",
					message:       fmt.Sprintf("server %q has already been pushed within the configured rate-limit window; next allowed push at %s", name, nextAllowed.UTC().Format(time.RFC3339)),
					nextAllowedAt: nextAllowed,
				}, nil
			}
		}
	}

	if p.Role == roleAdmin || current != nil || policy.activeServerLimit <= 0 {
		return nil, nil
	}
	count, err := s.countPrincipalActiveServers(ctx, p)
	if err != nil {
		return nil, err
	}
	if count >= policy.activeServerLimit {
		return &serverPublishPolicyRejection{
			status:  http.StatusTooManyRequests,
			code:    "active_server_limit_reached",
			message: fmt.Sprintf("active MCPServer limit %d reached; retire an existing server before publishing another", policy.activeServerLimit),
			limit:   policy.activeServerLimit,
			count:   count,
		}, nil
	}
	return nil, nil
}

func lastServerPushAt(server *mcpv1alpha1.MCPServer) (time.Time, bool) {
	if server == nil || server.Annotations == nil {
		return time.Time{}, false
	}
	raw := strings.TrimSpace(server.Annotations[platformLastPushAtAnnotation])
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func (s *DeploymentService) countPrincipalActiveServers(ctx context.Context, p principal) (int, error) {
	control := s.controlPlane()
	if control == nil {
		return 0, fmt.Errorf("kubernetes not available")
	}
	userID := strings.TrimSpace(p.UserID())
	if userID == "" {
		return 0, nil
	}
	ownerSelector, canUseOwnerSelector := serverOwnerLabelSelector(userID)
	count := 0
	for _, namespace := range publishNamespacesForPrincipal(p) {
		opts := controlplane.ListServersOptions{SkipDeploymentStatus: true}
		if canUseOwnerSelector && !principalOwnsNamespace(p, namespace) {
			opts.LabelSelector = ownerSelector
		}
		result, err := control.ListServersWithOptions(ctx, namespace, opts)
		if err != nil {
			return 0, err
		}
		for _, server := range result.Servers {
			if serverInfoOwnedByPrincipal(server, p) {
				count++
			}
		}
	}
	return count, nil
}

func serverInfoOwnedByPrincipal(server controlplane.ServerInfo, p principal) bool {
	return serverLabelsOwnedByPrincipal(server.Namespace, server.Labels, p)
}

func serverWritableByPrincipal(server mcpv1alpha1.MCPServer, p principal) bool {
	return serverLabelsOwnedByPrincipal(server.Namespace, server.Labels, p)
}

func serverLabelsOwnedByPrincipal(namespace string, serverLabels map[string]string, p principal) bool {
	userID := strings.TrimSpace(p.UserID())
	if userID == "" {
		return false
	}
	owner := strings.TrimSpace(serverLabels[platformUserIDLabel])
	if owner == userID {
		return true
	}
	if owner != "" {
		return false
	}
	if principalOwnsNamespace(p, namespace) {
		return true
	}
	return sharedCatalogWritableForUsers() && isModeCatalogNamespace(namespace) && principalCanPublishNamespace(p, namespace)
}

func serverOwnerLabelSelector(userID string) (string, bool) {
	req, err := labels.NewRequirement(platformUserIDLabel, selection.Equals, []string{strings.TrimSpace(userID)})
	if err != nil {
		return "", false
	}
	return req.String(), true
}

func (r *serverPublishPolicyRejection) retryAfterHeader() string {
	if r == nil || r.nextAllowedAt.IsZero() {
		return ""
	}
	seconds := int64(time.Until(r.nextAllowedAt).Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return strconv.FormatInt(seconds, 10)
}

func (r *serverPublishPolicyRejection) payload() map[string]any {
	payload := map[string]any{
		"error":   r.code,
		"code":    r.code,
		"message": r.message,
	}
	if !r.nextAllowedAt.IsZero() {
		payload["next_allowed_at"] = r.nextAllowedAt.UTC().Format(time.RFC3339)
	}
	if r.limit > 0 {
		payload["active_server_limit"] = r.limit
		payload["active_server_count"] = r.count
	}
	return payload
}

func serverPublishAuditEvent(r *http.Request, p principal, action, status, name, namespace, image, message string) auditEvent {
	target := strings.Trim(strings.TrimSpace(namespace)+"/"+strings.TrimSpace(name), "/")
	return auditEvent{
		UserID:           p.UserID(),
		Action:           action,
		Resource:         strings.TrimSpace(name),
		Namespace:        strings.TrimSpace(namespace),
		Status:           status,
		Message:          strings.TrimSpace(message),
		ActorIP:          requestIP(r),
		Source:           auditSource(r, p),
		AuthIdentity:     auditIdentityLabel(p),
		ImageRef:         strings.TrimSpace(image),
		ServerName:       strings.TrimSpace(name),
		DeploymentTarget: target,
	}
}
