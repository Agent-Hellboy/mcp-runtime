package manifest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type traefikDynamicConfigMap struct {
	Data map[string]string `yaml:"data"`
}

type traefikDynamicConfig struct {
	HTTP struct {
		Middlewares map[string]struct {
			ForwardAuth *struct {
				Address            string `yaml:"address"`
				TrustForwardHeader *bool  `yaml:"trustForwardHeader"`
			} `yaml:"forwardAuth"`
		} `yaml:"middlewares"`
	} `yaml:"http"`
}

func TestRegistryForwardAuthDoesNotTrustClientForwardedHeaders(t *testing.T) {
	for _, rel := range []string{
		filepath.Join("config", "ingress", "base", "dynamic-config.yaml"),
		filepath.Join("config", "ingress", "overlays", "http", "dynamic-config.yaml"),
	} {
		t.Run(rel, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", rel))
			if err != nil {
				t.Fatalf("read %s: %v", rel, err)
			}

			var configMap traefikDynamicConfigMap
			if err := yaml.Unmarshal(raw, &configMap); err != nil {
				t.Fatalf("decode %s config map: %v", rel, err)
			}
			dynamicYAML := strings.TrimSpace(configMap.Data["dynamic.yml"])
			if dynamicYAML == "" {
				t.Fatalf("%s missing data.dynamic.yml", rel)
			}

			var dynamic traefikDynamicConfig
			if err := yaml.Unmarshal([]byte(dynamicYAML), &dynamic); err != nil {
				t.Fatalf("decode %s dynamic.yml: %v", rel, err)
			}
			middleware, ok := dynamic.HTTP.Middlewares["registry-admin-auth"]
			if !ok {
				t.Fatalf("%s missing registry-admin-auth middleware", rel)
			}
			if middleware.ForwardAuth == nil {
				t.Fatalf("%s registry-admin-auth missing forwardAuth", rel)
			}
			if !strings.HasSuffix(middleware.ForwardAuth.Address, "/api/registry/authz") {
				t.Fatalf("%s registry authz address = %q, want /api/registry/authz", rel, middleware.ForwardAuth.Address)
			}
			if middleware.ForwardAuth.TrustForwardHeader != nil && *middleware.ForwardAuth.TrustForwardHeader {
				t.Fatalf("%s registry-admin-auth must not trust client-supplied forwarded headers", rel)
			}
		})
	}
}
