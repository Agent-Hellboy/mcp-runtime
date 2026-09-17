package platform

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/kube"
	"mcp-runtime/internal/cli/setup/assetpath"
)

func deployMCPAuthServer(image, configuredIssuer, tlsSecret, connectorsFile, connector string, testMode bool, deps SetupDeps) error {
	path, err := assetpath.ResolveRepoAssetPath("k8s/23-mcp-auth-server.yaml")
	if err != nil {
		return err
	}
	raw, err := kube.ReadFileAtPath(path)
	if err != nil {
		return err
	}
	issuer := strings.TrimRight(strings.TrimSpace(configuredIssuer), "/")
	if issuer == "" {
		issuer = strings.TrimRight(strings.TrimSpace(os.Getenv("OAUTH_ISSUER_URL")), "/")
	}
	if issuer == "" && testMode {
		issuer = "http://localhost:18080/mcp-auth"
	}
	if !testMode {
		parsed, err := url.Parse(issuer)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("production mcp-auth deployment requires an absolute HTTPS issuer URL")
		}
		if strings.TrimSpace(connectorsFile) == "" || strings.TrimSpace(connector) == "" {
			return fmt.Errorf("production mcp-auth deployment requires a connector file and selected connector")
		}
		if strings.TrimSpace(tlsSecret) == "" {
			return fmt.Errorf("production mcp-auth deployment requires a TLS Secret")
		}
	}
	resource := strings.TrimSuffix(issuer, "/mcp-auth") + "/mcp-auth-example/mcp"
	manifest := strings.ReplaceAll(string(raw), "image: docker.io/princekrroshan01/mcp-auth-server:latest", "image: "+image)
	manifest = strings.ReplaceAll(manifest, "MCP_AUTH_ISSUER_VALUE", issuer)
	manifest = strings.ReplaceAll(manifest, "MCP_AUTH_RESOURCE_VALUE", resource)
	manifest = strings.ReplaceAll(manifest, "MCP_AUTH_LOCAL_DEVELOPMENT_VALUE", strconv.FormatBool(testMode))
	manifest = strings.ReplaceAll(manifest, "MCP_AUTH_LOCAL_TOKEN_EXCHANGE_VALUE", strconv.FormatBool(testMode))
	manifest = strings.ReplaceAll(manifest, "MCP_AUTH_REQUIRE_HTTPS_VALUE", strconv.FormatBool(!testMode))
	parsedIssuer, _ := url.Parse(issuer)
	manifest = strings.ReplaceAll(manifest, "MCP_AUTH_INGRESS_HOST_VALUE", parsedIssuer.Host)
	if testMode {
		manifest = strings.ReplaceAll(manifest, "MCP_AUTH_INGRESS_TLS_BLOCK", "")
	} else {
		manifest = strings.ReplaceAll(manifest, "MCP_AUTH_INGRESS_TLS_BLOCK", "  tls:\n    - hosts: ["+strconv.Quote(parsedIssuer.Host)+"]\n      secretName: "+tlsSecret)
	}
	if strings.TrimSpace(connectorsFile) != "" {
		connectorJSON, secretValues, err := readMCPAuthConnector(connectorsFile, connector)
		if err != nil {
			return err
		}
		configMap := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: mcp-auth-connectors\n  namespace: mcp-sentinel\ndata:\n  connectors.json: |\n"
		for _, line := range strings.Split(strings.TrimRight(string(connectorJSON), "\n"), "\n") {
			configMap += "    " + line + "\n"
		}
		if err := applyManifestYAML(configMap, "", os.Stdout); err != nil {
			return fmt.Errorf("apply mcp-auth connector config: %w", err)
		}
		secret := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: mcp-auth-connector-secrets\n  namespace: mcp-sentinel\nstringData:\n"
		for name, value := range secretValues {
			encoded, _ := json.Marshal(value)
			secret += "  " + name + ": " + string(encoded) + "\n"
		}
		if len(secretValues) > 0 {
			if err := applyManifestYAML(secret, "", os.Stdout); err != nil {
				return fmt.Errorf("apply mcp-auth connector secrets: %w", err)
			}
		}
		manifest = strings.ReplaceAll(manifest, "MCP_AUTH_CONNECTOR_ENV_BLOCK", "- {name: MCP_AUTH_CONNECTORS_FILE, value: /etc/mcp-auth/connectors.json}\n            - {name: MCP_AUTH_CONNECTOR, value: "+strconv.Quote(connector)+"}\n            - {name: MCP_AUTH_LOCAL_DEVELOPMENT, value: \"false\"}")
		manifest = strings.ReplaceAll(manifest, "MCP_AUTH_CONNECTOR_ENVFROM_BLOCK", "- secretRef: {name: mcp-auth-connector-secrets}")
		manifest = strings.ReplaceAll(manifest, "MCP_AUTH_CONNECTOR_VOLUME_MOUNT_BLOCK", "- name: connectors\n              mountPath: /etc/mcp-auth\n              readOnly: true")
		manifest = strings.ReplaceAll(manifest, "MCP_AUTH_CONNECTOR_POD_VOLUME_BLOCK", "- name: connectors\n          configMap: {name: mcp-auth-connectors}\n        - name: connector-secrets\n          secret: {secretName: mcp-auth-connector-secrets}")
	} else {
		manifest = strings.ReplaceAll(manifest, "MCP_AUTH_CONNECTOR_ENV_BLOCK", "")
		manifest = strings.ReplaceAll(manifest, "MCP_AUTH_CONNECTOR_ENVFROM_BLOCK", "[]")
		manifest = strings.ReplaceAll(manifest, "MCP_AUTH_CONNECTOR_VOLUME_MOUNT_BLOCK", "[]")
		manifest = strings.ReplaceAll(manifest, "MCP_AUTH_CONNECTOR_POD_VOLUME_BLOCK", "[]")
	}
	core.Info("Applying optional bundled mcp-auth authorization server")
	if err := applyManifestYAML(manifest, "", os.Stdout); err != nil {
		return fmt.Errorf("apply mcp-auth authorization server: %w", err)
	}
	internalIssuer := "http://mcp-auth-server.mcp-sentinel.svc.cluster.local:8080"
	cmd, err := core.DefaultKubectlClient().CommandArgs([]string{
		"set", "env", "deployment/mcp-runtime-operator-controller-manager",
		"-n", core.NamespaceMCPRuntime,
		"OAUTH_INTERNAL_ISSUER_URL=" + internalIssuer,
	})
	if err != nil {
		return fmt.Errorf("prepare operator OAuth issuer update: %w", err)
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("configure operator OAuth issuer: %w", err)
	}
	if err := deps.RestartDeployment("mcp-runtime-operator-controller-manager", core.NamespaceMCPRuntime); err != nil {
		return fmt.Errorf("restart operator after OAuth issuer update: %w", err)
	}
	return nil
}

func readMCPAuthConnector(path, selected string) ([]byte, map[string]string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- explicit setup flag path.
	if err != nil {
		return nil, nil, fmt.Errorf("read --mcp-auth-connectors-file: %w", err)
	}
	var connectors map[string]map[string]any
	if err := json.Unmarshal(data, &connectors); err != nil {
		return nil, nil, fmt.Errorf("parse --mcp-auth-connectors-file: %w", err)
	}
	if strings.TrimSpace(selected) == "" {
		return nil, nil, fmt.Errorf("--mcp-auth-connector is required when a connector file is provided")
	}
	config, ok := connectors[selected]
	if !ok {
		return nil, nil, fmt.Errorf("connector %q is not defined in %s", selected, path)
	}
	secrets := map[string]string{}
	for _, key := range []string{"client_secret_env", "client_id_env"} {
		if name, ok := config[key].(string); ok && strings.TrimSpace(name) != "" {
			if value, exists := os.LookupEnv(name); exists {
				secrets[name] = value
			} else {
				return nil, nil, fmt.Errorf("connector %q references unset environment variable %s", selected, name)
			}
		}
	}
	return data, secrets, nil
}
