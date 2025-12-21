// Package cli provides CLI commands for the mcp-runtime.
package cli

import (
	"errors"
	"sync"

	"go.uber.org/zap"

	"mcp-runtime/pkg/errx"
)

var (
	debugMode   bool
	debugModeMu sync.RWMutex
)

// SetDebugMode sets the global debug mode flag.
// When enabled, logStructuredError will output structured error logs to terminal.
func SetDebugMode(enabled bool) {
	debugModeMu.Lock()
	defer debugModeMu.Unlock()
	debugMode = enabled
}

// IsDebugMode returns whether debug mode is enabled.
func IsDebugMode() bool {
	debugModeMu.RLock()
	defer debugModeMu.RUnlock()
	return debugMode
}

type errorSpec struct {
	code        string
	description string
}

// defineError creates a sentinel error and registers it in errorSpecs in one step.
// This eliminates redundancy between error definitions and errorSpecs mapping.
func defineError(msg string, code, description string) error {
	err := errors.New(msg)
	errorSpecs[err] = errorSpec{code: code, description: description}
	return err
}

// lookupSpec provides a lookup function for errx.FromSentinel.
func lookupSpec(sentinel error) (code, description string) {
	spec := specFor(sentinel)
	return spec.code, spec.description
}

// newUserError creates a new error using the appropriate errx category helper.
// The base error is used to determine the category, and the message provides context.
func newUserError(base error, msg string) error {
	if base == nil {
		return errx.CreateByCode(errx.CodeCLI, errx.DescCLI, msg, nil)
	}
	return errx.FromSentinel(base, lookupSpec, msg, nil)
}

// wrapUserError wraps a cause error using the appropriate errx category helper.
// The base error is used to determine the category, and the message provides context.
func wrapUserError(base, cause error, msg string) error {
	if base == nil {
		return errx.CreateByCode(errx.CodeCLI, errx.DescCLI, msg, cause)
	}
	return errx.FromSentinel(base, lookupSpec, msg, cause)
}

// wrapUserErrorWithContext wraps an error with additional structured context.
// This is useful for adding debugging information like namespace, resource names, etc.
func wrapUserErrorWithContext(base, cause error, msg string, context map[string]any) error {
	err := wrapUserError(base, cause, msg)
	if errxErr, ok := err.(*errx.Error); ok && len(context) > 0 {
		return errxErr.WithContextMap(context)
	}
	return err
}

// Sentinel errors for CLI operations.
// Errors are defined and registered in one step using defineError to eliminate redundancy.
var (
	// CLI errors.
	ErrImageRequired             = defineError("image is required", errx.CodeCLI, errx.DescCLI)
	ErrInvalidServerName         = defineError("invalid server name", errx.CodeCLI, errx.DescCLI)
	ErrGetWorkingDirectoryFailed = defineError("get working directory", errx.CodeCLI, errx.DescCLI)
	ErrControlCharsNotAllowed    = defineError("value must not contain control characters", errx.CodeCLI, errx.DescCLI)
	ErrFieldRequired             = defineError("field is required", errx.CodeCLI, errx.DescCLI)
	ErrGetHomeDirectoryFailed    = defineError("failed to get home directory", errx.CodeCLI, errx.DescCLI)
	ErrUnknownRegistryMode       = defineError("unknown registry mode", errx.CodeCLI, errx.DescCLI)

	// Pipeline errors.
	ErrLoadMetadataFailed      = defineError("failed to load metadata", errx.CodePipeline, errx.DescPipeline)
	ErrNoServersInMetadata     = defineError("no servers found in metadata", errx.CodePipeline, errx.DescPipeline)
	ErrGenerateCRDsFailed      = defineError("failed to generate CRDs", errx.CodePipeline, errx.DescPipeline)
	ErrListManifestFilesFailed = defineError("failed to list manifest files", errx.CodePipeline, errx.DescPipeline)
	ErrNoManifestFilesFound    = defineError("no manifest files found", errx.CodePipeline, errx.DescPipeline)
	ErrApplyManifestFailed     = defineError("failed to apply manifest", errx.CodePipeline, errx.DescPipeline)

	// Operator errors.
	ErrOperatorNotFound = defineError("operator not found", errx.CodeOperator, errx.DescOperator)
	ErrOperatorNotReady = defineError("operator not ready", errx.CodeOperator, errx.DescOperator)

	// Setup errors.
	ErrClusterInitFailed                  = defineError("failed to initialize cluster", errx.CodeSetup, errx.DescSetup)
	ErrClusterConfigFailed                = defineError("cluster configuration failed", errx.CodeSetup, errx.DescSetup)
	ErrTLSSetupFailed                     = defineError("TLS setup failed", errx.CodeSetup, errx.DescSetup)
	ErrDeployRegistryFailed               = defineError("failed to deploy registry", errx.CodeSetup, errx.DescSetup)
	ErrOperatorImageBuildFailed           = defineError("operator image build failed", errx.CodeSetup, errx.DescSetup)
	ErrEnsureRegistryNamespaceFailed      = defineError("failed to ensure registry namespace", errx.CodeSetup, errx.DescSetup)
	ErrPushOperatorImageInternalFailed    = defineError("failed to push operator image to internal registry", errx.CodeSetup, errx.DescSetup)
	ErrOperatorDeploymentFailed           = defineError("operator deployment failed", errx.CodeSetup, errx.DescSetup)
	ErrConfigureExternalRegistryEnvFailed = defineError("failed to configure external registry env on operator", errx.CodeSetup, errx.DescSetup)
	ErrRestartOperatorDeploymentFailed    = defineError("failed to restart operator deployment after registry env update", errx.CodeSetup, errx.DescSetup)
	ErrCRDCheckFailed                     = defineError("CRD check failed", errx.CodeSetup, errx.DescSetup)
	ErrRenderSecretManifestFailed         = defineError("render secret manifest", errx.CodeSetup, errx.DescSetup)
	ErrApplySecretManifestFailed          = defineError("apply secret manifest", errx.CodeSetup, errx.DescSetup)
	ErrMarshalDockerConfigFailed          = defineError("marshal docker config", errx.CodeSetup, errx.DescSetup)
	ErrApplyImagePullSecretFailed         = defineError("apply imagePullSecret", errx.CodeSetup, errx.DescSetup)
	ErrPushImageInClusterFailed           = defineError("failed to push image in-cluster", errx.CodeSetup, errx.DescSetup)
	ErrSetupStepFailed                    = defineError("setup step failed", errx.CodeSetup, errx.DescSetup)
	ErrApplyCRDFailed                     = defineError("failed to apply CRD", errx.CodeSetup, errx.DescSetup)
	ErrEnsureOperatorNamespaceFailed      = defineError("failed to ensure operator namespace", errx.CodeSetup, errx.DescSetup)
	ErrApplyRBACFailed                    = defineError("failed to apply RBAC", errx.CodeSetup, errx.DescSetup)
	ErrReadManagerYAMLFailed              = defineError("failed to read manager.yaml", errx.CodeSetup, errx.DescSetup)
	ErrCreateTempFileFailed               = defineError("failed to create temp file", errx.CodeSetup, errx.DescSetup)
	ErrCloseTempFileFailed                = defineError("failed to close temp file", errx.CodeSetup, errx.DescSetup)
	ErrWriteTempFileFailed                = defineError("failed to write temp file", errx.CodeSetup, errx.DescSetup)
	ErrApplyManagerDeploymentFailed       = defineError("failed to apply manager deployment", errx.CodeSetup, errx.DescSetup)
	ErrClusterIssuerApplyFailed           = defineError("failed to apply ClusterIssuer", errx.CodeSetup, errx.DescSetup)
	ErrCreateRegistryNamespaceFailed      = defineError("failed to create registry namespace", errx.CodeSetup, errx.DescSetup)
	ErrApplyCertificateFailed             = defineError("failed to apply Certificate", errx.CodeSetup, errx.DescSetup)

	// Cert errors.
	ErrCertManagerNotInstalled     = defineError("cert-manager not installed", errx.CodeCert, errx.DescCert)
	ErrCASecretNotFound            = defineError("CA secret not found", errx.CodeCert, errx.DescCert)
	ErrCertificateNotReady         = defineError("certificate not ready", errx.CodeCert, errx.DescCert)
	ErrClusterIssuerNotFound       = defineError("ClusterIssuer not found", errx.CodeCert, errx.DescCert)
	ErrRegistryCertificateNotFound = defineError("registry Certificate not found", errx.CodeCert, errx.DescCert)

	// Cluster errors.
	ErrCRDNotInstalled                = defineError("MCPServer CRD not installed", errx.CodeCluster, errx.DescCluster)
	ErrClusterNotAccessible           = defineError("cluster not accessible", errx.CodeCluster, errx.DescCluster)
	ErrNamespaceNotFound              = defineError("namespace not found", errx.CodeCluster, errx.DescCluster)
	ErrDeploymentTimeout              = defineError("deployment timed out waiting for readiness", errx.CodeCluster, errx.DescCluster)
	ErrInstallCRDFailed               = defineError("failed to install CRD", errx.CodeCluster, errx.DescCluster)
	ErrEnsureRuntimeNamespaceFailed   = defineError("failed to ensure mcp-runtime namespace", errx.CodeCluster, errx.DescCluster)
	ErrEnsureServersNamespaceFailed   = defineError("failed to ensure mcp-servers namespace", errx.CodeCluster, errx.DescCluster)
	ErrKubeconfigNotReadable          = defineError("kubeconfig not found or not readable", errx.CodeCluster, errx.DescCluster)
	ErrSetKubeconfigFailed            = defineError("failed to set KUBECONFIG", errx.CodeCluster, errx.DescCluster)
	ErrSetContextFailed               = defineError("failed to set context", errx.CodeCluster, errx.DescCluster)
	ErrAKSKubeconfigNotImplemented    = defineError("AKS kubeconfig not yet implemented", errx.CodeCluster, errx.DescCluster)
	ErrGKEKubeconfigNotImplemented    = defineError("GKE kubeconfig not yet implemented", errx.CodeCluster, errx.DescCluster)
	ErrUnsupportedProvider            = defineError("unsupported provider", errx.CodeCluster, errx.DescCluster)
	ErrUnsupportedIngressController   = defineError("unsupported ingress controller", errx.CodeCluster, errx.DescCluster)
	ErrInstallIngressControllerFailed = defineError("failed to install ingress controller", errx.CodeCluster, errx.DescCluster)
	ErrCreateKindConfigFailed         = defineError("failed to create temp kind config", errx.CodeCluster, errx.DescCluster)
	ErrCloseKindConfigFailed          = defineError("failed to close kind config", errx.CodeCluster, errx.DescCluster)
	ErrWriteKindConfigFailed          = defineError("failed to write kind config", errx.CodeCluster, errx.DescCluster)
	ErrCreateKindClusterFailed        = defineError("failed to create kind cluster", errx.CodeCluster, errx.DescCluster)
	ErrGKEProvisioningNotImplemented  = defineError("GKE provisioning not yet implemented", errx.CodeCluster, errx.DescCluster)
	ErrProvisionEKSFailed             = defineError("failed to provision EKS cluster", errx.CodeCluster, errx.DescCluster)
	ErrAKSProvisioningNotImplemented  = defineError("AKS provisioning not yet implemented", errx.CodeCluster, errx.DescCluster)

	// Registry errors.
	ErrRegistryNotReady            = defineError("registry not ready", errx.CodeRegistry, errx.DescRegistry)
	ErrRegistryNotFound            = defineError("registry not found", errx.CodeRegistry, errx.DescRegistry)
	ErrBuildOperatorImageFailed    = defineError("failed to build operator image", errx.CodeRegistry, errx.DescRegistry)
	ErrPushOperatorImageFailed     = defineError("failed to push operator image", errx.CodeRegistry, errx.DescRegistry)
	ErrUnsupportedRegistryType     = defineError("unsupported registry type", errx.CodeRegistry, errx.DescRegistry)
	ErrEnsureNamespaceFailed       = defineError("failed to ensure namespace", errx.CodeRegistry, errx.DescRegistry)
	ErrReadRegistryStorageFailed   = defineError("failed to read current registry storage size", errx.CodeRegistry, errx.DescRegistry)
	ErrUpdateRegistryStorageFailed = defineError("failed to update registry storage size", errx.CodeRegistry, errx.DescRegistry)
	ErrRegistryLoginFailed         = defineError("failed to login to registry", errx.CodeRegistry, errx.DescRegistry)
	ErrTagImageFailed              = defineError("failed to tag image", errx.CodeRegistry, errx.DescRegistry)
	ErrPushImageFailed             = defineError("failed to push image", errx.CodeRegistry, errx.DescRegistry)
	ErrHelperNamespaceNotFound     = defineError("helper namespace not found", errx.CodeRegistry, errx.DescRegistry)
	ErrSaveImageFailed             = defineError("failed to save image", errx.CodeRegistry, errx.DescRegistry)
	ErrStartHelperPodFailed        = defineError("failed to start helper pod", errx.CodeRegistry, errx.DescRegistry)
	ErrHelperPodNotReady           = defineError("helper pod not ready", errx.CodeRegistry, errx.DescRegistry)
	ErrCopyImageToHelperFailed     = defineError("failed to copy image tar to helper pod", errx.CodeRegistry, errx.DescRegistry)
	ErrPushImageFromHelperFailed   = defineError("failed to push image from helper pod", errx.CodeRegistry, errx.DescRegistry)

	// Config errors.
	ErrRegistryURLRequired           = defineError("registry url is required", errx.CodeConfig, errx.DescConfig)
	ErrRegistryURLMissingInConfig    = defineError("registry url missing in config", errx.CodeConfig, errx.DescConfig)
	ErrSaveRegistryConfigFailed      = defineError("failed to save registry config", errx.CodeConfig, errx.DescConfig)
	ErrReadRegistryConfigFailed      = defineError("failed to read registry config", errx.CodeConfig, errx.DescConfig)
	ErrUnmarshalRegistryConfigFailed = defineError("failed to unmarshal registry config", errx.CodeConfig, errx.DescConfig)

	// Build errors.
	ErrBuildImageFailed         = defineError("failed to build image", errx.CodeBuild, errx.DescBuild)
	ErrMetadataFileNotFound     = defineError("metadata file not found", errx.CodeBuild, errx.DescBuild)
	ErrServerNotFoundInMetadata = defineError("server not found in metadata", errx.CodeBuild, errx.DescBuild)
	ErrMarshalMetadataFailed    = defineError("failed to marshal metadata", errx.CodeBuild, errx.DescBuild)
	ErrWriteMetadataFailed      = defineError("failed to write metadata", errx.CodeBuild, errx.DescBuild)

	// Server errors.
	ErrMarshalManifestFailed = defineError("failed to marshal manifest", errx.CodeServer, errx.DescServer)
	ErrWriteManifestFailed   = defineError("failed to write manifest", errx.CodeServer, errx.DescServer)
	ErrInvalidFilePath       = defineError("invalid file path", errx.CodeServer, errx.DescServer)
	ErrFileNotAccessible     = defineError("cannot access file", errx.CodeServer, errx.DescServer)
	ErrFileIsDirectory       = defineError("path is a directory, not a file", errx.CodeServer, errx.DescServer)
	ErrGetMCPServerFailed    = defineError("kubectl get mcpserver failed", errx.CodeServer, errx.DescServer)
	ErrListServersFailed     = defineError("failed to list servers", errx.CodeServer, errx.DescServer)
	ErrCreateServerFailed    = defineError("failed to create server", errx.CodeServer, errx.DescServer)
	ErrDeleteServerFailed    = defineError("failed to delete server", errx.CodeServer, errx.DescServer)
	ErrViewServerLogsFailed  = defineError("failed to view server logs", errx.CodeServer, errx.DescServer)
)

// errorSpecs maps sentinel errors to their error codes and descriptions.
// Populated automatically by defineError() during variable initialization.
var errorSpecs = make(map[error]errorSpec)

func specFor(base error) errorSpec {
	spec, ok := errorSpecs[base]
	if ok {
		return spec
	}
	return errorSpec{code: errx.CodeCLI, description: errx.DescCLI}
}

// logStructuredError logs an error with structured fields to terminal.
// Only logs when debug mode is enabled (via --debug flag).
// The zap logger is configured with console encoding, so structured fields
// are displayed in a human-readable format in the terminal.
//
// This extracts all context from errx.Error and logs it with structured fields:
// - error.code: "SETUP_CLUSTER_INIT_FAILED"
// - error.category: "Setup"
// - error.context.namespace: "registry"
// - error.context.image: "my-image:latest"
// - error.context.component: "registry" | "operator" | "server"
//
// Note: The Kubernetes operator (which runs in-cluster) uses controller-runtime's
// zap logger for structured logging that can be collected by log aggregation systems.
func logStructuredError(logger *zap.Logger, err error, msg string) {
	if logger == nil || err == nil || !IsDebugMode() {
		return
	}

	var errxErr *errx.Error
	if errors.As(err, &errxErr) {
		fields := []zap.Field{
			zap.String("error.code", errxErr.Code()),
			zap.String("error.category", errxErr.Description()),
			zap.String("error.message", errxErr.Message()),
			zap.Error(err),
		}

		// Add all context fields as individual zap fields for structured output
		if ctx := errxErr.Context(); ctx != nil {
			for key, value := range ctx {
				fields = append(fields, zap.Any("error.context."+key, value))
			}
		}

		// Add cause if present
		if cause := errxErr.Cause(); cause != nil {
			fields = append(fields, zap.Error(cause))
		}

		logger.Error(msg, fields...)
	} else {
		// Fallback for non-errx errors
		logger.Error(msg, zap.Error(err))
	}
}
