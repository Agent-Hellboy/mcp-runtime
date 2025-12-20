package operator

import (
	"testing"
)

func TestHasProvisionedRegistry(t *testing.T) {
	tests := []struct {
		name string
		cfg  *OperatorConfig
		want bool
	}{
		{
			name: "has_provisioned_registry",
			cfg: &OperatorConfig{
				ProvisionedRegistryURL: "registry.example.com:5000",
			},
			want: true,
		},
		{
			name: "no_provisioned_registry",
			cfg:  &OperatorConfig{},
			want: false,
		},
		{
			name: "empty_url",
			cfg: &OperatorConfig{
				ProvisionedRegistryURL: "",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.HasProvisionedRegistry(); got != tt.want {
				t.Errorf("HasProvisionedRegistry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToRegistryConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *OperatorConfig
		wantNil bool
		wantURL string
	}{
		{
			name: "converts_when_registry_configured",
			cfg: &OperatorConfig{
				ProvisionedRegistryURL:        "registry.example.com:5000",
				ProvisionedRegistryUsername:   "user",
				ProvisionedRegistryPassword:   "pass",
				ProvisionedRegistrySecretName: "my-secret",
			},
			wantNil: false,
			wantURL: "registry.example.com:5000",
		},
		{
			name:    "returns_nil_when_no_registry",
			cfg:     &OperatorConfig{},
			wantNil: true,
		},
		{
			name: "returns_nil_when_empty_url",
			cfg: &OperatorConfig{
				ProvisionedRegistryURL: "",
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.ToRegistryConfig()
			if tt.wantNil {
				if got != nil {
					t.Errorf("ToRegistryConfig() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("ToRegistryConfig() = nil, want non-nil")
			}
			if got.URL != tt.wantURL {
				t.Errorf("ToRegistryConfig().URL = %v, want %v", got.URL, tt.wantURL)
			}
			if got.Username != tt.cfg.ProvisionedRegistryUsername {
				t.Errorf("ToRegistryConfig().Username = %v, want %v", got.Username, tt.cfg.ProvisionedRegistryUsername)
			}
			if got.Password != tt.cfg.ProvisionedRegistryPassword {
				t.Errorf("ToRegistryConfig().Password = %v, want %v", got.Password, tt.cfg.ProvisionedRegistryPassword)
			}
			if got.SecretName != tt.cfg.ProvisionedRegistrySecretName {
				t.Errorf("ToRegistryConfig().SecretName = %v, want %v", got.SecretName, tt.cfg.ProvisionedRegistrySecretName)
			}
		})
	}
}

func TestGetEnvOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		setEnv       bool
		want         string
	}{
		{
			name:         "returns_env_value_when_set",
			key:          "TEST_ENV_VAR_1",
			defaultValue: "default",
			envValue:     "custom",
			setEnv:       true,
			want:         "custom",
		},
		{
			name:         "returns_default_when_not_set",
			key:          "TEST_ENV_VAR_2",
			defaultValue: "default",
			setEnv:       false,
			want:         "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(tt.key, tt.envValue)
			}
			if got := getEnvOrDefault(tt.key, tt.defaultValue); got != tt.want {
				t.Errorf("getEnvOrDefault() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnvIntOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue int
		envValue     string
		setEnv       bool
		want         int
	}{
		{
			name:         "returns_env_int_when_valid",
			key:          "TEST_INT_VAR_1",
			defaultValue: 10,
			envValue:     "42",
			setEnv:       true,
			want:         42,
		},
		{
			name:         "returns_default_when_not_set",
			key:          "TEST_INT_VAR_2",
			defaultValue: 10,
			setEnv:       false,
			want:         10,
		},
		{
			name:         "returns_default_when_invalid_int",
			key:          "TEST_INT_VAR_3",
			defaultValue: 10,
			envValue:     "not-a-number",
			setEnv:       true,
			want:         10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(tt.key, tt.envValue)
			}
			if got := getEnvIntOrDefault(tt.key, tt.defaultValue); got != tt.want {
				t.Errorf("getEnvIntOrDefault() = %v, want %v", got, tt.want)
			}
		})
	}
}
