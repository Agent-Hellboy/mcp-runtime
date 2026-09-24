package operator

import (
	"context"
	"net/url"
	"path"
	"reflect"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
)

const (
	bundledOAuthNamespace  = "mcp-sentinel"
	bundledOAuthDeployment = "mcp-auth-server"
)

// The list is delivered as Deployment env, so each change to the set of
// audiences rolls the mcp-auth-server pod; adding or removing an OAuth server
// therefore interrupts logins in flight. Moving the list to a watched
// ConfigMap needs mcp-auth support for reloading it.
//
// reconcileBundledOAuthResources keeps the optional bundled authorization
// server's accepted resource list aligned with the audiences advertised by
// OAuth MCPServers. It runs for every MCPServer event, including a deleted
// object's final reconcile, so additions, changes, and removals are reflected.
func (r *MCPServerReconciler) reconcileBundledOAuthResources(ctx context.Context) error {
	if strings.TrimSpace(r.OAuthIssuerURL) == "" {
		return nil
	}

	var servers mcpv1alpha1.MCPServerList
	if err := r.List(ctx, &servers); err != nil {
		return err
	}
	resources := make([]string, 0, len(servers.Items))
	seen := make(map[string]struct{}, len(servers.Items))
	for i := range servers.Items {
		server := r.defaultedMCPServerForReconcile(&servers.Items[i])
		if server.Spec.Auth == nil || server.Spec.Auth.Mode != mcpv1alpha1.AuthModeOAuth {
			continue
		}
		if strings.TrimRight(strings.TrimSpace(server.Spec.Auth.IssuerURL), "/") != strings.TrimRight(strings.TrimSpace(r.OAuthIssuerURL), "/") {
			continue
		}
		audience := strings.TrimSpace(server.Spec.Auth.Audience)
		if audience == "" || !r.audienceServedByPlatform(server, audience) {
			continue
		}
		if _, ok := seen[audience]; ok {
			continue
		}
		seen[audience] = struct{}{}
		resources = append(resources, audience)
	}
	sort.Strings(resources)

	deployment := &appsv1.Deployment{}
	key := types.NamespacedName{Name: bundledOAuthDeployment, Namespace: bundledOAuthNamespace}
	if err := r.Get(ctx, key, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return nil
	}

	before := deployment.DeepCopy()
	envs := deployment.Spec.Template.Spec.Containers[0].Env
	upsertEnv := func(name, value string) {
		for i := range envs {
			if envs[i].Name == name {
				envs[i].Value = value
				envs[i].ValueFrom = nil
				return
			}
		}
		envs = append(envs, corev1.EnvVar{Name: name, Value: value})
	}
	removeEnv := func(name string) {
		filtered := envs[:0]
		for _, env := range envs {
			if env.Name != name {
				filtered = append(filtered, env)
			}
		}
		envs = filtered
	}
	upsertEnv("MCP_AUTH_RESOURCES", strings.Join(resources, ","))
	if len(resources) == 0 {
		removeEnv("MCP_AUTH_RESOURCE")
	} else {
		upsertEnv("MCP_AUTH_RESOURCE", resources[0])
	}
	deployment.Spec.Template.Spec.Containers[0].Env = envs
	if reflect.DeepEqual(before.Spec.Template.Spec.Containers[0].Env, envs) {
		return nil
	}
	return r.Patch(ctx, deployment, client.MergeFrom(before))
}

// audienceServedByPlatform reports whether an MCPServer's audience names the
// route that server is actually served on. MCPServer objects are tenant
// input, and the bundled authorization server mints tokens for every listed
// resource, so an audience is only published when its path is the server's
// own public path and its host is one the platform serves it on: the
// server's ingress host, the operator default MCP host, or, when neither is
// known (local test mode), the issuer's host. A server cannot enlist another
// host, an external URL, or a different route.
func (r *MCPServerReconciler) audienceServedByPlatform(server *mcpv1alpha1.MCPServer, audience string) bool {
	parsed, err := url.Parse(audience)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return false
	}
	publicPath := strings.TrimSpace(server.EffectivePublicPath())
	if publicPath == "" || path.Clean("/"+strings.TrimLeft(parsed.Path, "/")) != path.Clean("/"+strings.TrimLeft(publicPath, "/")) {
		return false
	}
	hosts := []string{strings.TrimSpace(server.Spec.IngressHost), strings.TrimSpace(r.DefaultIngressHost)}
	if hosts[0] == "" && hosts[1] == "" {
		if issuer, err := url.Parse(strings.TrimSpace(r.OAuthIssuerURL)); err == nil {
			hosts = append(hosts, issuer.Host)
		}
	}
	for _, host := range hosts {
		if host != "" && strings.EqualFold(host, parsed.Host) {
			return true
		}
	}
	return false
}
