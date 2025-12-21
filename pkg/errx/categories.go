package errx

// CategoryFunc represents a pair of category-specific error creation functions.
type CategoryFunc struct {
	Create func(string) *Error
	Wrap   func(string, error) *Error
}

// CategoryMap maps error codes to their category-specific functions.
var CategoryMap = map[string]CategoryFunc{
	CodeCLI:      {Create: CLI, Wrap: WrapCLI},
	CodeCluster:  {Create: Cluster, Wrap: WrapCluster},
	CodeRegistry: {Create: Registry, Wrap: WrapRegistry},
	CodeOperator: {Create: Operator, Wrap: WrapOperator},
	CodePipeline: {Create: Pipeline, Wrap: WrapPipeline},
	CodeBuild:    {Create: Build, Wrap: WrapBuild},
	CodeServer:   {Create: Server, Wrap: WrapServer},
	CodeCert:     {Create: Cert, Wrap: WrapCert},
	CodeSetup:    {Create: Setup, Wrap: WrapSetup},
	CodeConfig:   {Create: Config, Wrap: WrapConfig},
}

// CreateByCode creates an Error using the appropriate category helper function.
// If the code is not in CategoryMap, it falls back to generic error creation.
func CreateByCode(code, description, message string, cause error) *Error {
	if cat, ok := CategoryMap[code]; ok {
		if cause != nil {
			return cat.Wrap(message, cause)
		}
		return cat.Create(message)
	}
	// Fallback to generic error creation for unknown codes
	if cause != nil {
		return Wrap(code, description, message, cause)
	}
	return New(code, description, message)
}

// FromSentinel creates an Error from a sentinel error and optional message/cause.
// This is useful when you have a sentinel error and want to create an errx.Error
// with the same category. The sentinel is used to determine the category via a lookup function.
func FromSentinel(sentinel error, lookup func(error) (code, description string), message string, cause error) *Error {
	code, desc := lookup(sentinel)
	if code == "" {
		code = CodeCLI
		desc = DescCLI
	}
	return CreateByCode(code, desc, message, cause).WithBase(sentinel)
}

// CLI creates a CLI/argument validation error with code 70000.
// Use this for errors related to command-line argument validation,
// invalid user input, or CLI-specific issues.
func CLI(message string) *Error {
	return New(CodeCLI, DescCLI, message)
}

// WrapCLI wraps a cause with a CLI/argument validation error.
// Use this when a CLI error is caused by another error that should be preserved.
func WrapCLI(message string, cause error) *Error {
	return Wrap(CodeCLI, DescCLI, message, cause)
}

// Cluster creates a cluster/provisioning error.
func Cluster(message string) *Error {
	return New(CodeCluster, DescCluster, message)
}

// WrapCluster wraps a cause with a cluster/provisioning error.
func WrapCluster(message string, cause error) *Error {
	return Wrap(CodeCluster, DescCluster, message, cause)
}

// Registry creates a registry error.
func Registry(message string) *Error {
	return New(CodeRegistry, DescRegistry, message)
}

// WrapRegistry wraps a cause with a registry error.
func WrapRegistry(message string, cause error) *Error {
	return Wrap(CodeRegistry, DescRegistry, message, cause)
}

// Operator creates an operator error.
func Operator(message string) *Error {
	return New(CodeOperator, DescOperator, message)
}

// WrapOperator wraps a cause with an operator error.
func WrapOperator(message string, cause error) *Error {
	return Wrap(CodeOperator, DescOperator, message, cause)
}

// Pipeline creates a pipeline error.
func Pipeline(message string) *Error {
	return New(CodePipeline, DescPipeline, message)
}

// WrapPipeline wraps a cause with a pipeline error.
func WrapPipeline(message string, cause error) *Error {
	return Wrap(CodePipeline, DescPipeline, message, cause)
}

// Build creates a build error.
func Build(message string) *Error {
	return New(CodeBuild, DescBuild, message)
}

// WrapBuild wraps a cause with a build error.
func WrapBuild(message string, cause error) *Error {
	return Wrap(CodeBuild, DescBuild, message, cause)
}

// Server creates a server definition error.
func Server(message string) *Error {
	return New(CodeServer, DescServer, message)
}

// WrapServer wraps a cause with a server definition error.
func WrapServer(message string, cause error) *Error {
	return Wrap(CodeServer, DescServer, message, cause)
}

// Cert creates a certificate/TLS error.
func Cert(message string) *Error {
	return New(CodeCert, DescCert, message)
}

// WrapCert wraps a cause with a certificate/TLS error.
func WrapCert(message string, cause error) *Error {
	return Wrap(CodeCert, DescCert, message, cause)
}

// Setup creates a setup/installation error.
func Setup(message string) *Error {
	return New(CodeSetup, DescSetup, message)
}

// WrapSetup wraps a cause with a setup/installation error.
func WrapSetup(message string, cause error) *Error {
	return Wrap(CodeSetup, DescSetup, message, cause)
}

// Config creates a configuration error.
func Config(message string) *Error {
	return New(CodeConfig, DescConfig, message)
}

// WrapConfig wraps a cause with a configuration error.
func WrapConfig(message string, cause error) *Error {
	return Wrap(CodeConfig, DescConfig, message, cause)
}
