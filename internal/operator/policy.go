package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"mcp-runtime/pkg/policy"
)

func (r *MCPServerReconciler) reconcilePolicyConfigMap(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	name := gatewayPolicyConfigMapName(mcpServer.Name)
	existing := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: name, Namespace: mcpServer.Namespace}

	if !gatewayEnabled(mcpServer) {
		if err := r.Get(ctx, key, existing); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return r.Delete(ctx, existing)
	}

	doc, err := r.renderGatewayPolicy(ctx, mcpServer)
	if err != nil {
		return err
	}
	// Reject an invalid rendered policy before it can replace the ConfigMap
	// contents, so the gateway never reloads a malformed last-known-good policy.
	if err := policy.Validate(doc); err != nil {
		return fmt.Errorf("rendered gateway policy for %s/%s is invalid: %w", mcpServer.Namespace, mcpServer.Name, err)
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: mcpServer.Namespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		configMap.Labels = map[string]string{
			"app":                          mcpServer.Name,
			"app.kubernetes.io/managed-by": "mcp-runtime",
		}
		rendered, err := renderPolicyConfigMapData(configMap.Data[gatewayPolicyFileName], doc)
		if err != nil {
			return err
		}
		configMap.Data = map[string]string{
			gatewayPolicyFileName: rendered,
		}
		return ctrl.SetControllerReference(mcpServer, configMap, r.Scheme)
	})
	return err
}

// renderPolicyConfigMapData serializes the policy document for the ConfigMap.
// When the rendered policy content is unchanged (same deterministic revision),
// the prior payload is preserved verbatim so that refreshing the informational
// generated_at timestamp does not churn the ConfigMap or trigger needless
// gateway reloads.
func renderPolicyConfigMapData(existing string, doc *policy.Document) (string, error) {
	if existing != "" {
		var prev policy.Document
		if err := json.Unmarshal([]byte(existing), &prev); err == nil && prev.Revision != "" && prev.Revision == doc.Revision {
			recomputed, computeErr := policy.ComputeRevision(&prev)
			if computeErr == nil && recomputed == prev.Revision {
				return existing, nil
			}
		}
	}
	doc.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (r *MCPServerReconciler) renderGatewayPolicy(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) (*policy.Document, error) {
	serverTeamID := strings.TrimSpace(mcpServer.Spec.TeamID)
	doc := &policy.Document{
		Server: policy.Server{
			Name:      policy.ServerName(mcpServer.Name),
			Namespace: policy.Namespace(mcpServer.Namespace),
			TeamID:    policy.TeamID(serverTeamID),
			Cluster:   strings.TrimSpace(r.ClusterName),
		},
	}

	if mcpServer.Spec.Auth != nil {
		doc.Auth = &policy.Auth{
			Mode:            string(mcpServer.Spec.Auth.Mode),
			HumanIDHeader:   mcpServer.Spec.Auth.HumanIDHeader,
			AgentIDHeader:   mcpServer.Spec.Auth.AgentIDHeader,
			TeamIDHeader:    mcpServer.Spec.Auth.TeamIDHeader,
			SessionIDHeader: mcpServer.Spec.Auth.SessionIDHeader,
			TokenHeader:     mcpServer.Spec.Auth.TokenHeader,
			IssuerURL:       mcpServer.Spec.Auth.IssuerURL,
			Audience:        mcpServer.Spec.Auth.Audience,
			TrustDomain:     mcpServer.Spec.Auth.TrustDomain,
		}
	}
	if mcpServer.Spec.Policy != nil {
		doc.Policy = &policy.Config{
			Mode:            string(mcpServer.Spec.Policy.Mode),
			DefaultDecision: string(mcpServer.Spec.Policy.DefaultDecision),
			EnforceOn:       mcpServer.Spec.Policy.EnforceOn,
			PolicyVersion:   mcpServer.Spec.Policy.PolicyVersion,
		}
	}
	if mcpServer.Spec.Session != nil {
		doc.Session = &policy.Session{
			Required:            mcpServer.Spec.Session.Required,
			Store:               mcpServer.Spec.Session.Store,
			HeaderName:          mcpServer.Spec.Session.HeaderName,
			MaxLifetime:         mcpServer.Spec.Session.MaxLifetime,
			IdleTimeout:         mcpServer.Spec.Session.IdleTimeout,
			UpstreamTokenHeader: mcpServer.Spec.Session.UpstreamTokenHeader,
		}
	}
	if len(mcpServer.Spec.Tools) > 0 {
		doc.Tools = make([]policy.Tool, 0, len(mcpServer.Spec.Tools))
		for _, tool := range mcpServer.Spec.Tools {
			rendered := policy.Tool{
				Name:          policy.ToolName(tool.Name),
				Description:   tool.Description,
				RequiredTrust: string(tool.RequiredTrust),
				SideEffect:    string(tool.SideEffect),
				RiskLevel:     string(tool.RiskLevel),
			}
			if len(tool.Labels) > 0 {
				rendered.Labels = make(map[string]string, len(tool.Labels))
				for k, v := range tool.Labels {
					rendered.Labels[k] = v
				}
			}
			doc.Tools = append(doc.Tools, rendered)
		}
	}

	var grants mcpv1alpha1.MCPAccessGrantList
	if err := r.List(ctx, &grants); err != nil {
		return nil, err
	}
	for _, grant := range grants.Items {
		if !serverReferenceMatches(grant.Namespace, grant.Spec.ServerRef, mcpServer) {
			continue
		}
		subjectTeamID := subjectTeamIDForServer(serverTeamID, grant.Spec.Subject.TeamID)
		rendered := policy.Grant{
			Name:          grant.Name,
			Namespace:     policy.Namespace(grant.Namespace),
			HumanID:       policy.HumanID(grant.Spec.Subject.HumanID),
			AgentID:       policy.AgentID(grant.Spec.Subject.AgentID),
			TeamID:        policy.TeamID(subjectTeamID),
			MaxTrust:      string(defaultTrust(grant.Spec.MaxTrust)),
			PolicyVersion: grant.Spec.PolicyVersion,
			Disabled:      grant.Spec.Disabled,
		}
		for _, sideEffect := range grant.Spec.AllowedSideEffects {
			rendered.AllowedSideEffects = append(rendered.AllowedSideEffects, string(sideEffect))
		}
		for _, rule := range grant.Spec.ToolRules {
			rendered.ToolRules = append(rendered.ToolRules, policy.ToolAccess{
				Name:          policy.ToolName(rule.Name),
				Decision:      string(defaultDecision(rule.Decision)),
				RequiredTrust: string(defaultTrust(rule.RequiredTrust)),
			})
		}
		doc.Grants = append(doc.Grants, rendered)
	}

	var sessions mcpv1alpha1.MCPAgentSessionList
	if err := r.List(ctx, &sessions); err != nil {
		return nil, err
	}
	for _, session := range sessions.Items {
		if !serverReferenceMatches(session.Namespace, session.Spec.ServerRef, mcpServer) {
			continue
		}
		subjectTeamID := subjectTeamIDForServer(serverTeamID, session.Spec.Subject.TeamID)
		rendered := policy.Binding{
			Name:           policy.SessionID(session.Name),
			Namespace:      policy.Namespace(session.Namespace),
			HumanID:        policy.HumanID(session.Spec.Subject.HumanID),
			AgentID:        policy.AgentID(session.Spec.Subject.AgentID),
			TeamID:         policy.TeamID(subjectTeamID),
			ConsentedTrust: string(defaultTrust(session.Spec.ConsentedTrust)),
			Revoked:        session.Spec.Revoked,
			PolicyVersion:  session.Spec.PolicyVersion,
		}
		if session.Spec.ExpiresAt != nil {
			rendered.ExpiresAt = session.Spec.ExpiresAt.UTC().Format(time.RFC3339)
		}
		if session.Spec.UpstreamTokenSecretRef != nil {
			rendered.UpstreamTokenRef = fmt.Sprintf("%s/%s", session.Spec.UpstreamTokenSecretRef.Name, session.Spec.UpstreamTokenSecretRef.Key)
		}
		doc.Sessions = append(doc.Sessions, rendered)
	}

	// Stamp document-level metadata (schema version + deterministic revision).
	// generated_at is left empty here and set at write time so it cannot affect
	// the revision; see renderPolicyConfigMapData.
	if err := policy.Stamp(doc, ""); err != nil {
		return nil, err
	}
	return doc, nil
}

func subjectTeamIDForServer(serverTeamID, subjectTeamID string) string {
	serverTeamID = strings.TrimSpace(serverTeamID)
	subjectTeamID = strings.TrimSpace(subjectTeamID)
	if subjectTeamID == "" {
		return serverTeamID
	}
	return subjectTeamID
}

func serverReferenceMatches(objectNamespace string, ref mcpv1alpha1.ServerReference, server *mcpv1alpha1.MCPServer) bool {
	namespace := strings.TrimSpace(ref.Namespace)
	if namespace == "" {
		namespace = objectNamespace
	}
	return ref.Name == server.Name && namespace == server.Namespace
}

func gatewayPolicyConfigMapName(serverName string) string {
	return serverName + "-gateway-policy"
}

func defaultTrust(trust mcpv1alpha1.TrustLevel) mcpv1alpha1.TrustLevel {
	if trust == "" {
		return mcpv1alpha1.TrustLevelLow
	}
	return trust
}

func defaultDecision(decision mcpv1alpha1.PolicyDecision) mcpv1alpha1.PolicyDecision {
	if decision == "" {
		return mcpv1alpha1.PolicyDecisionAllow
	}
	return decision
}
