package runtimeapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"mcp-runtime/pkg/controlplane"
)

const (
	defaultActiveServerLimit              = 1
	defaultPushCooldown                   = 6 * time.Hour
	defaultPublishRateLimitWindow         = 6 * time.Hour
	envActiveServerLimit                  = "PLATFORM_MCP_ACTIVE_SERVER_LIMIT"
	envPushCooldown                       = "PLATFORM_MCP_PUSH_COOLDOWN"
	envPublishRateLimitWindow             = "PLATFORM_MCP_PUBLISH_RATE_LIMIT_WINDOW"
	envPublishIdentitySalt                = "PLATFORM_MCP_PUBLISH_IDENTITY_SALT"
	clientFingerprintHeader               = "X-MCP-Client-Fingerprint"
	platformLastPushAtAnnotation          = "platform.mcpruntime.org/last-push-at"
	platformLastPushByAnnotation          = "platform.mcpruntime.org/last-push-by"
	platformPublisherIPHashLabel          = "platform.mcpruntime.org/publisher-ip-hash"
	platformPublisherFingerprintHashLabel = "platform.mcpruntime.org/publisher-fingerprint-hash"
	maxPublishAttemptLimiterEntries       = 10000
)

type serverPublishPolicy struct {
	activeServerLimit      int
	pushCooldown           time.Duration
	publishRateLimitWindow time.Duration
}

type serverPublishPolicyStatus struct {
	ActiveServerLimit        int    `json:"active_server_limit"`
	ActiveServerCount        int    `json:"active_server_count"`
	ActiveServerLimitEnabled bool   `json:"active_server_limit_enabled"`
	CanPublish               bool   `json:"can_publish"`
	PushCooldown             string `json:"push_cooldown"`
	PushCooldownSeconds      int64  `json:"push_cooldown_seconds"`
	PushCooldownEnabled      bool   `json:"push_cooldown_enabled"`
	PublishRateLimit         string `json:"publish_rate_limit"`
	PublishRateLimitSeconds  int64  `json:"publish_rate_limit_seconds"`
	PublishRateLimitEnabled  bool   `json:"publish_rate_limit_enabled"`
}

type serverPublishPolicyRejection struct {
	status        int
	code          string
	message       string
	nextAllowedAt time.Time
	limit         int
	count         int
}

type publishIdentity struct {
	UserID          string
	IPHash          string
	FingerprintHash string
}

type serverPublishAttemptLimiter struct {
	mu          sync.Mutex
	nextAllowed map[string]time.Time
}

var publishIdentitySaltCache struct {
	sync.Once
	value string
}

func newServerPublishAttemptLimiter() *serverPublishAttemptLimiter {
	return &serverPublishAttemptLimiter{nextAllowed: map[string]time.Time{}}
}

func currentServerPublishPolicy() serverPublishPolicy {
	return serverPublishPolicy{
		activeServerLimit:      activeServerLimitFromEnv(),
		pushCooldown:           durationFromEnv(envPushCooldown, defaultPushCooldown),
		publishRateLimitWindow: durationFromEnv(envPublishRateLimitWindow, defaultPublishRateLimitWindow),
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

func (s *RuntimeServer) publishPolicyStatusForPrincipal(ctx context.Context, p principal, identity publishIdentity) serverPublishPolicyStatus {
	policy := currentServerPublishPolicy()
	status := serverPublishPolicyStatus{
		ActiveServerLimit:        policy.activeServerLimit,
		ActiveServerLimitEnabled: policy.activeServerLimit > 0,
		CanPublish:               true,
		PushCooldown:             policy.pushCooldown.String(),
		PushCooldownSeconds:      int64(policy.pushCooldown.Seconds()),
		PushCooldownEnabled:      policy.pushCooldown > 0,
		PublishRateLimit:         policy.publishRateLimitWindow.String(),
		PublishRateLimitSeconds:  int64(policy.publishRateLimitWindow.Seconds()),
		PublishRateLimitEnabled:  policy.publishRateLimitWindow > 0,
	}
	if p.Role == roleAdmin || strings.TrimSpace(p.UserID()) == "" || policy.activeServerLimit <= 0 {
		return status
	}
	count, err := s.countPublishIdentityActiveServers(ctx, p, identity)
	if err != nil {
		status.CanPublish = false
		return status
	}
	status.ActiveServerCount = count
	status.CanPublish = count < policy.activeServerLimit
	return status
}

func (s *RuntimeServer) evaluateServerPublishPolicy(ctx context.Context, p principal, namespace, name string, current *mcpv1alpha1.MCPServer, identity publishIdentity, now time.Time) (*serverPublishPolicyRejection, error) {
	policy := currentServerPublishPolicy()
	if p.Role != roleAdmin && policy.publishRateLimitWindow > 0 {
		nextAllowed, limited := s.publishAttemptLimiter().checkAndRecord(publishRateLimitKeys(identity), now, policy.publishRateLimitWindow)
		if limited {
			return &serverPublishPolicyRejection{
				status:        http.StatusTooManyRequests,
				code:          "publish_rate_limited",
				message:       fmt.Sprintf("publishing is rate limited; next allowed publish at %s", nextAllowed.UTC().Format(time.RFC3339)),
				nextAllowedAt: nextAllowed,
			}, nil
		}
	}

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
	count, err := s.countPublishIdentityActiveServers(ctx, p, identity)
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

func (s *RuntimeServer) countPublishIdentityActiveServers(ctx context.Context, p principal, identity publishIdentity) (int, error) {
	control := s.controlPlane()
	if control == nil {
		return 0, fmt.Errorf("kubernetes not available")
	}
	userID := strings.TrimSpace(p.UserID())
	if userID == "" {
		return 0, nil
	}
	selectors := publishIdentityLabelSelectors(userID, identity)
	if len(selectors) == 0 {
		return 0, nil
	}
	count := 0
	seen := map[string]struct{}{}
	for _, namespace := range publishNamespacesForPrincipal(p) {
		namespaceOwned := principalOwnsNamespace(p, namespace)
		namespaceSelectors := selectors
		if namespaceOwned {
			namespaceSelectors = []string{""}
		}
		for _, selector := range namespaceSelectors {
			result, err := control.ListServersWithOptions(ctx, namespace, controlplane.ListServersOptions{
				LabelSelector:        selector,
				SkipDeploymentStatus: true,
			})
			if err != nil {
				return 0, err
			}
			for _, server := range result.Servers {
				key := serverInfoKey(server)
				if _, ok := seen[key]; ok {
					continue
				}
				if serverInfoMatchesPublishIdentity(server, p, identity) {
					seen[key] = struct{}{}
					count++
				}
			}
		}
	}
	return count, nil
}

func serverInfoKey(server controlplane.ServerInfo) string {
	if server.UID != "" {
		return server.Namespace + "/" + server.UID
	}
	return server.Namespace + "/" + server.Name
}

func serverInfoMatchesPublishIdentity(server controlplane.ServerInfo, p principal, identity publishIdentity) bool {
	return serverLabelsMatchPublishIdentity(server.Namespace, server.Labels, p, identity)
}

func serverWritableByPrincipal(server mcpv1alpha1.MCPServer, p principal) bool {
	return serverLabelsOwnedByPrincipal(server.Namespace, server.Labels, p)
}

func serverLabelsMatchPublishIdentity(namespace string, serverLabels map[string]string, p principal, identity publishIdentity) bool {
	if serverLabelsCountAgainstPrincipal(namespace, serverLabels, p) {
		return true
	}
	if identity.IPHash != "" && strings.TrimSpace(serverLabels[platformPublisherIPHashLabel]) == identity.IPHash {
		return true
	}
	if identity.FingerprintHash != "" && strings.TrimSpace(serverLabels[platformPublisherFingerprintHashLabel]) == identity.FingerprintHash {
		return true
	}
	return false
}

func serverLabelsCountAgainstPrincipal(namespace string, serverLabels map[string]string, p principal) bool {
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
	return principalOwnsNamespace(p, namespace)
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

func publishIdentityLabelSelectors(userID string, identity publishIdentity) []string {
	selectors := make([]string, 0, 3)
	if selector, ok := labelEqualsSelector(platformUserIDLabel, userID); ok {
		selectors = append(selectors, selector)
	}
	if selector, ok := labelEqualsSelector(platformPublisherIPHashLabel, identity.IPHash); ok {
		selectors = append(selectors, selector)
	}
	if selector, ok := labelEqualsSelector(platformPublisherFingerprintHashLabel, identity.FingerprintHash); ok {
		selectors = append(selectors, selector)
	}
	return selectors
}

func labelEqualsSelector(key, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	req, err := labels.NewRequirement(key, selection.Equals, []string{value})
	if err != nil {
		return "", false
	}
	return req.String(), true
}

func (s *RuntimeServer) publishAttemptLimiter() *serverPublishAttemptLimiter {
	s.publishAttemptsMu.Lock()
	defer s.publishAttemptsMu.Unlock()
	if s.publishAttempts == nil {
		s.publishAttempts = newServerPublishAttemptLimiter()
	}
	return s.publishAttempts
}

func publishIdentityFromRequest(r *http.Request, p principal) publishIdentity {
	identity := publishIdentity{UserID: strings.TrimSpace(p.UserID())}
	if ip := requestIP(r); ip != "" {
		identity.IPHash = publishSignalHash("ip", ip)
	}
	if fingerprint := requestClientFingerprint(r); fingerprint != "" {
		identity.FingerprintHash = publishSignalHash("fingerprint", fingerprint)
	}
	return identity
}

func requestClientFingerprint(r *http.Request) string {
	if r == nil {
		return ""
	}
	value := strings.TrimSpace(r.Header.Get(clientFingerprintHeader))
	if value == "" {
		return ""
	}
	if len(value) > 512 {
		value = value[:512]
	}
	return value
}

func publishSignalHash(kind, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(kind + "\x00" + publishIdentitySalt() + "\x00" + value))
	return hex.EncodeToString(sum[:16])
}

func publishIdentitySalt() string {
	publishIdentitySaltCache.Do(func() {
		publishIdentitySaltCache.value = strings.TrimSpace(os.Getenv(envPublishIdentitySalt))
	})
	return publishIdentitySaltCache.value
}

func applyPublishIdentityLabels(labels map[string]string, current *mcpv1alpha1.MCPServer, identity publishIdentity) {
	if labels == nil {
		return
	}
	if identity.IPHash != "" {
		labels[platformPublisherIPHashLabel] = identity.IPHash
	}
	if identity.FingerprintHash != "" {
		labels[platformPublisherFingerprintHashLabel] = identity.FingerprintHash
	}
	if current == nil {
		return
	}
	preserveCurrentLabel(labels, current.Labels, platformPublisherIPHashLabel)
	preserveCurrentLabel(labels, current.Labels, platformPublisherFingerprintHashLabel)
}

func publishRateLimitKeys(identity publishIdentity) []string {
	keys := make([]string, 0, 3)
	if identity.UserID != "" {
		keys = append(keys, "user:"+identity.UserID)
	}
	if identity.IPHash != "" {
		keys = append(keys, "ip:"+identity.IPHash)
	}
	if identity.FingerprintHash != "" {
		keys = append(keys, "fingerprint:"+identity.FingerprintHash)
	}
	return keys
}

func (l *serverPublishAttemptLimiter) checkAndRecord(keys []string, now time.Time, window time.Duration) (time.Time, bool) {
	if l == nil || len(keys) == 0 || window <= 0 {
		return time.Time{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	for _, key := range keys {
		if nextAllowed := l.nextAllowed[key]; nextAllowed.After(now) {
			return nextAllowed, true
		}
	}
	nextAllowed := now.Add(window)
	for _, key := range keys {
		l.nextAllowed[key] = nextAllowed
	}
	l.prune(now)
	return time.Time{}, false
}

func (l *serverPublishAttemptLimiter) prune(now time.Time) {
	for key, nextAllowed := range l.nextAllowed {
		if !nextAllowed.After(now) {
			delete(l.nextAllowed, key)
		}
	}
	if len(l.nextAllowed) <= maxPublishAttemptLimiterEntries {
		return
	}
	entries := make([]serverPublishAttemptLimiterEntry, 0, len(l.nextAllowed))
	for key, nextAllowed := range l.nextAllowed {
		entries = append(entries, serverPublishAttemptLimiterEntry{key: key, nextAllowed: nextAllowed})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].nextAllowed.Before(entries[j].nextAllowed)
	})
	for _, entry := range entries {
		if len(l.nextAllowed) <= maxPublishAttemptLimiterEntries {
			return
		}
		delete(l.nextAllowed, entry.key)
	}
}

type serverPublishAttemptLimiterEntry struct {
	key         string
	nextAllowed time.Time
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
