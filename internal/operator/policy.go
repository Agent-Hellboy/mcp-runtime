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
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
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
		configMap.Data = map[string]string{
			gatewayPolicyFileName: string(data),
		}
		return ctrl.SetControllerReference(mcpServer, configMap, r.Scheme)
	})
	return err
}

func (r *MCPServerReconciler) renderGatewayPolicy(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) (*policy.Document, error) {
	serverTeamID := strings.TrimSpace(mcpServer.Spec.TeamID)
	doc := &policy.Document{
		Server: policy.Server{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
			TeamID:    serverTeamID,
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
				Name:          tool.Name,
				Description:   tool.Description,
				RequiredTrust: string(tool.RequiredTrust),
				SideEffect:    string(tool.SideEffect),
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
		subjectTeamID, ok := subjectTeamIDForServer(serverTeamID, grant.Spec.Subject.TeamID)
		if !ok {
			continue
		}
		rendered := policy.Grant{
			Name:          grant.Name,
			HumanID:       grant.Spec.Subject.HumanID,
			AgentID:       grant.Spec.Subject.AgentID,
			TeamID:        subjectTeamID,
			MaxTrust:      string(defaultTrust(grant.Spec.MaxTrust)),
			PolicyVersion: grant.Spec.PolicyVersion,
			Disabled:      grant.Spec.Disabled,
		}
		for _, sideEffect := range grant.Spec.AllowedSideEffects {
			rendered.AllowedSideEffects = append(rendered.AllowedSideEffects, string(sideEffect))
		}
		for _, rule := range grant.Spec.ToolRules {
			rendered.ToolRules = append(rendered.ToolRules, policy.ToolAccess{
				Name:          rule.Name,
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
		subjectTeamID, ok := subjectTeamIDForServer(serverTeamID, session.Spec.Subject.TeamID)
		if !ok {
			continue
		}
		rendered := policy.Binding{
			Name:           session.Name,
			HumanID:        session.Spec.Subject.HumanID,
			AgentID:        session.Spec.Subject.AgentID,
			TeamID:         subjectTeamID,
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

	return doc, nil
}

func subjectTeamIDForServer(serverTeamID, subjectTeamID string) (string, bool) {
	serverTeamID = strings.TrimSpace(serverTeamID)
	subjectTeamID = strings.TrimSpace(subjectTeamID)
	if serverTeamID == "" {
		return subjectTeamID, true
	}
	if subjectTeamID == "" {
		return serverTeamID, true
	}
	return subjectTeamID, subjectTeamID == serverTeamID
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
