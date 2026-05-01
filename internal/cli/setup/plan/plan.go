// Package plan contains pure setup planning types and default resolution.
package plan

import "mcp-runtime/internal/cli/cluster"

const (
	StorageModeDynamic  = "dynamic"
	StorageModeHostpath = "hostpath"
)

// Input captures the raw CLI inputs for setup.
type Input struct {
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
	// Let's Encrypt (HTTP-01 via cert-manager). If empty, other TLS modes apply; mutually exclusive with TLSClusterIssuer.
	ACMEmail    string
	ACMEStaging bool
	// TLSClusterIssuer is a pre-existing cert-manager.io ClusterIssuer (e.g. org internal CA / Vault / ADCS). Mutually exclusive with ACMEmail.
	TLSClusterIssuer   string
	InstallCertManager bool
}

// Plan captures the resolved setup decisions.
type Plan struct {
	Kubeconfig          string
	Context             string
	RegistryType        string
	RegistryStorageSize string
	StorageMode         string
	Ingress             cluster.IngressOptions
	RegistryManifest    string
	TLSEnabled          bool
	TestMode            bool
	StrictProd          bool
	DeployAnalytics     bool
	OperatorArgs        []string
	ACMEmail            string
	ACMEStaging         bool
	TLSClusterIssuer    string
	InstallCertManager  bool
}

// Build resolves CLI inputs into a concrete setup plan.
func Build(input Input) Plan {
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

	return Plan{
		Kubeconfig:          input.Kubeconfig,
		Context:             input.Context,
		RegistryType:        input.RegistryType,
		RegistryStorageSize: input.RegistryStorageSize,
		StorageMode:         input.StorageMode,
		Ingress: cluster.IngressOptions{
			Mode:     input.IngressMode,
			Manifest: manifestPath,
			Force:    input.ForceIngressInstall,
		},
		RegistryManifest:   registryManifest,
		TLSEnabled:         input.TLSEnabled,
		TestMode:           input.TestMode,
		StrictProd:         input.StrictProd,
		DeployAnalytics:    input.DeployAnalytics,
		OperatorArgs:       input.OperatorArgs,
		ACMEmail:           input.ACMEmail,
		ACMEStaging:        input.ACMEStaging,
		InstallCertManager: input.InstallCertManager,
		TLSClusterIssuer:   input.TLSClusterIssuer,
	}
}
