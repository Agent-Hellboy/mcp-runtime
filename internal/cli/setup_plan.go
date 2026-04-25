package cli

// This file defines the setup planning types and logic.
// SetupPlanInput captures raw CLI inputs, and BuildSetupPlan resolves them into a concrete SetupPlan
// that determines which manifests and configurations to use during setup.

const (
	StorageModeDynamic  = "dynamic"
	StorageModeHostpath = "hostpath"
)

// SetupPlanInput captures the raw CLI inputs for setup.
type SetupPlanInput struct {
	Kubeconfig             string
	Context                string
	RegistryType           string
	RegistryStorageSize    string
	StorageMode            string
	IngressMode            string
	IngressManifest        string
	IngressManifestChanged bool
	ForceIngressInstall    bool
	TLSEnabled             bool
	TestMode               bool
	StrictProd             bool
	DeployAnalytics        bool
	OperatorArgs           []string
}

// SetupPlan captures the resolved setup decisions.
type SetupPlan struct {
	Kubeconfig          string
	Context             string
	RegistryType        string
	RegistryStorageSize string
	StorageMode         string
	Ingress             ingressOptions
	RegistryManifest    string
	TLSEnabled          bool
	TestMode            bool
	StrictProd          bool
	DeployAnalytics     bool
	OperatorArgs        []string
}

// BuildSetupPlan resolves CLI inputs into a concrete setup plan.
func BuildSetupPlan(input SetupPlanInput) SetupPlan {
	if input.StorageMode == "" {
		input.StorageMode = StorageModeDynamic
	}

	manifestPath := input.IngressManifest
	if !input.IngressManifestChanged {
		if input.TLSEnabled {
			manifestPath = "config/ingress/overlays/prod"
		} else {
			manifestPath = "config/ingress/overlays/http"
		}
	}

	registryManifest := "config/registry"
	if input.StorageMode == StorageModeHostpath {
		if input.TLSEnabled {
			registryManifest = "config/registry/overlays/hostpath-tls"
		} else {
			registryManifest = "config/registry/overlays/hostpath"
		}
	} else if input.TLSEnabled {
		registryManifest = "config/registry/overlays/tls"
	}

	return SetupPlan{
		Kubeconfig:          input.Kubeconfig,
		Context:             input.Context,
		RegistryType:        input.RegistryType,
		RegistryStorageSize: input.RegistryStorageSize,
		StorageMode:         input.StorageMode,
		Ingress: ingressOptions{
			mode:     input.IngressMode,
			manifest: manifestPath,
			force:    input.ForceIngressInstall,
		},
		RegistryManifest: registryManifest,
		TLSEnabled:       input.TLSEnabled,
		TestMode:         input.TestMode,
		StrictProd:       input.StrictProd,
		DeployAnalytics:  input.DeployAnalytics,
		OperatorArgs:     input.OperatorArgs,
	}
}
