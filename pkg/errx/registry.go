package errx

// RegistryEntry describes a registered error code.
type RegistryEntry struct {
	Code        string
	Description string
}

// Error codes follow a stable 5-digit scheme where the first two digits are the
// domain and the last three digits are reserved for subcodes.
const (
	CodeCLI      = "70000"
	CodeCluster  = "71000"
	CodeRegistry = "72000"
	CodeOperator = "73000"
	CodePipeline = "74000"
	CodeBuild    = "75000"
	CodeServer   = "76000"
	CodeCert     = "77000"
	CodeSetup    = "78000"
	CodeConfig   = "79000"
)

const (
	DescCLI      = "CLI/argument validation error"
	DescCluster  = "Cluster/provisioning error"
	DescRegistry = "Registry error"
	DescOperator = "Operator error"
	DescPipeline = "Pipeline error"
	DescBuild    = "Build error"
	DescServer   = "Server definition error"
	DescCert     = "Certificate/TLS error"
	DescSetup    = "Setup/installation error"
	DescConfig   = "Configuration error"
)

var registryEntries = []RegistryEntry{
	{Code: CodeCLI, Description: DescCLI},
	{Code: CodeCluster, Description: DescCluster},
	{Code: CodeRegistry, Description: DescRegistry},
	{Code: CodeOperator, Description: DescOperator},
	{Code: CodePipeline, Description: DescPipeline},
	{Code: CodeBuild, Description: DescBuild},
	{Code: CodeServer, Description: DescServer},
	{Code: CodeCert, Description: DescCert},
	{Code: CodeSetup, Description: DescSetup},
	{Code: CodeConfig, Description: DescConfig},
}

var registryMap = map[string]string{
	CodeCLI:      DescCLI,
	CodeCluster:  DescCluster,
	CodeRegistry: DescRegistry,
	CodeOperator: DescOperator,
	CodePipeline: DescPipeline,
	CodeBuild:    DescBuild,
	CodeServer:   DescServer,
	CodeCert:     DescCert,
	CodeSetup:    DescSetup,
	CodeConfig:   DescConfig,
}

// ErrorRegistry returns the error registry in deterministic order.
// This provides a list of all registered error codes and their descriptions.
func ErrorRegistry() []RegistryEntry {
	entries := make([]RegistryEntry, len(registryEntries))
	copy(entries, registryEntries)
	return entries
}

// DescriptionFor returns the registry description for a code.
func DescriptionFor(code string) (string, bool) {
	desc, ok := registryMap[code]
	return desc, ok
}

// IsValidCode checks if the given error code is registered.
func IsValidCode(code string) bool {
	_, ok := registryMap[code]
	return ok
}
