package cli

// SetupPlanInput captures the raw CLI inputs for setup.
type SetupPlanInput struct {
	RegistryType           string
	RegistryStorageSize    string
	IngressMode            string
	IngressManifest        string
	IngressManifestChanged bool
	ForceIngressInstall    bool
	TLSEnabled             bool
	TestMode               bool
}

// SetupPlan captures the resolved setup decisions.
type SetupPlan struct {
	RegistryType        string
	RegistryStorageSize string
	Ingress             ingressOptions
	RegistryManifest    string
	TLSEnabled          bool
	TestMode            bool
}

// BuildSetupPlan resolves CLI inputs into a concrete setup plan.
func BuildSetupPlan(input SetupPlanInput) SetupPlan {
	manifestPath := input.IngressManifest
	if !input.IngressManifestChanged {
		if input.TLSEnabled {
			manifestPath = "config/ingress/overlays/prod"
		} else {
			manifestPath = "config/ingress/overlays/http"
		}
	}

	registryManifest := "config/registry"
	if input.TLSEnabled {
		registryManifest = "config/registry/overlays/tls"
	}

	return SetupPlan{
		RegistryType:        input.RegistryType,
		RegistryStorageSize: input.RegistryStorageSize,
		Ingress: ingressOptions{
			mode:     input.IngressMode,
			manifest: manifestPath,
			force:    input.ForceIngressInstall,
		},
		RegistryManifest: registryManifest,
		TLSEnabled:       input.TLSEnabled,
		TestMode:         input.TestMode,
	}
}
