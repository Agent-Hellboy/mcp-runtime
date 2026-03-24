package cli

// This file defines error handling utilities for the CLI, including:
//   - Sentinel errors for different error categories (CLI, Cluster, Registry, etc.)
//   - Error wrapping functions that integrate with the errx error system
//   - Structured error logging with context
//   - Debug mode management for error output

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

// newSentinelError creates a sentinel error and registers it in errorSpecs in one step.
// This eliminates redundancy between error definitions and errorSpecs mapping.
func newSentinelError(msg string, code, description string) error {
	err := errors.New(msg)
	errorSpecs[err] = errorSpec{code: code, description: description}
	return err
}

// errorSpecs maps sentinel errors to their error codes and descriptions.
// Populated automatically by newSentinelError() during variable initialization.
// Must be declared before sentinel errors to ensure proper initialization order.
var errorSpecs = make(map[error]errorSpec)

// lookupSpec provides a lookup function for errx.FromSentinel.
func lookupSpec(sentinel error) (code, description string) {
	spec := specFor(sentinel)
	return spec.code, spec.description
}

// newWithSentinel creates a new error using the appropriate errx category helper.
// The base error (sentinel) is used to determine the category, and the message provides context.
func newWithSentinel(base error, msg string) error {
	if base == nil {
		return errx.CreateByCode(errx.CodeCLI, errx.DescCLI, msg, nil)
	}
	return errx.FromSentinel(base, lookupSpec, msg, nil)
}

// wrapWithSentinel wraps a cause error using the appropriate errx category helper.
// The base error (sentinel) is used to determine the category, and the message provides context.
func wrapWithSentinel(base, cause error, msg string) error {
	if base == nil {
		return errx.CreateByCode(errx.CodeCLI, errx.DescCLI, msg, cause)
	}
	return errx.FromSentinel(base, lookupSpec, msg, cause)
}

// wrapWithSentinelAndContext wraps an error with additional structured context.
// This is useful for adding debugging information like namespace, resource names, etc.
func wrapWithSentinelAndContext(base, cause error, msg string, context map[string]any) error {
	err := wrapWithSentinel(base, cause, msg)
	if errxErr, ok := err.(*errx.Error); ok && len(context) > 0 {
		return errxErr.WithContextMap(context)
	}
	return err
}

// Sentinel errors for CLI operations.
// Errors are defined and registered in one step using newSentinelError to eliminate redundancy.
var (
	// CLI errors.
	ErrImageRequired             = newSentinelError("image is required", errx.CodeCLI, errx.DescCLI)
	ErrInvalidServerName         = newSentinelError("invalid server name", errx.CodeCLI, errx.DescCLI)
	ErrGetWorkingDirectoryFailed = newSentinelError("get working directory", errx.CodeCLI, errx.DescCLI)
	ErrControlCharsNotAllowed    = newSentinelError("value must not contain control characters", errx.CodeCLI, errx.DescCLI)
	ErrFieldRequired             = newSentinelError("field is required", errx.CodeCLI, errx.DescCLI)
	ErrGetHomeDirectoryFailed    = newSentinelError("failed to get home directory", errx.CodeCLI, errx.DescCLI)
	ErrUnknownRegistryMode       = newSentinelError("unknown registry mode", errx.CodeCLI, errx.DescCLI)

	// Pipeline errors.
	ErrLoadMetadataFailed      = newSentinelError("failed to load metadata", errx.CodePipeline, errx.DescPipeline)
	ErrNoServersInMetadata     = newSentinelError("no servers found in metadata", errx.CodePipeline, errx.DescPipeline)
	ErrGenerateCRDsFailed      = newSentinelError("failed to generate CRDs", errx.CodePipeline, errx.DescPipeline)
	ErrListManifestFilesFailed = newSentinelError("failed to list manifest files", errx.CodePipeline, errx.DescPipeline)
	ErrNoManifestFilesFound    = newSentinelError("no manifest files found", errx.CodePipeline, errx.DescPipeline)
	ErrApplyManifestFailed     = newSentinelError("failed to apply manifest", errx.CodePipeline, errx.DescPipeline)

	// Operator errors.
	ErrOperatorNotFound = newSentinelError("operator not found", errx.CodeOperator, errx.DescOperator)
	ErrOperatorNotReady = newSentinelError("operator not ready", errx.CodeOperator, errx.DescOperator)

	// Setup errors.
	ErrClusterInitFailed                  = newSentinelError("failed to initialize cluster", errx.CodeSetup, errx.DescSetup)
	ErrClusterConfigFailed                = newSentinelError("cluster configuration failed", errx.CodeSetup, errx.DescSetup)
	ErrTLSSetupFailed                     = newSentinelError("TLS setup failed", errx.CodeSetup, errx.DescSetup)
	ErrDeployRegistryFailed               = newSentinelError("failed to deploy registry", errx.CodeSetup, errx.DescSetup)
	ErrOperatorImageBuildFailed           = newSentinelError("operator image build failed", errx.CodeSetup, errx.DescSetup)
	ErrEnsureRegistryNamespaceFailed      = newSentinelError("failed to ensure registry namespace", errx.CodeSetup, errx.DescSetup)
	ErrPushOperatorImageInternalFailed    = newSentinelError("failed to push operator image to internal registry", errx.CodeSetup, errx.DescSetup)
	ErrOperatorDeploymentFailed           = newSentinelError("operator deployment failed", errx.CodeSetup, errx.DescSetup)
	ErrConfigureExternalRegistryEnvFailed = newSentinelError("failed to configure external registry env on operator", errx.CodeSetup, errx.DescSetup)
	ErrRestartOperatorDeploymentFailed    = newSentinelError("failed to restart operator deployment after registry env update", errx.CodeSetup, errx.DescSetup)
	ErrCRDCheckFailed                     = newSentinelError("CRD check failed", errx.CodeSetup, errx.DescSetup)
	ErrRenderSecretManifestFailed         = newSentinelError("render secret manifest", errx.CodeSetup, errx.DescSetup)
	ErrApplySecretManifestFailed          = newSentinelError("apply secret manifest", errx.CodeSetup, errx.DescSetup)
	ErrMarshalDockerConfigFailed          = newSentinelError("marshal docker config", errx.CodeSetup, errx.DescSetup)
	ErrApplyImagePullSecretFailed         = newSentinelError("apply imagePullSecret", errx.CodeSetup, errx.DescSetup)
	ErrPushImageInClusterFailed           = newSentinelError("failed to push image in-cluster", errx.CodeSetup, errx.DescSetup)
	ErrSetupStepFailed                    = newSentinelError("setup step failed", errx.CodeSetup, errx.DescSetup)
	ErrApplyCRDFailed                     = newSentinelError("failed to apply CRD", errx.CodeSetup, errx.DescSetup)
	ErrEnsureOperatorNamespaceFailed      = newSentinelError("failed to ensure operator namespace", errx.CodeSetup, errx.DescSetup)
	ErrApplyRBACFailed                    = newSentinelError("failed to apply RBAC", errx.CodeSetup, errx.DescSetup)
	ErrReadManagerYAMLFailed              = newSentinelError("failed to read manager.yaml", errx.CodeSetup, errx.DescSetup)
	ErrCreateTempFileFailed               = newSentinelError("failed to create temp file", errx.CodeSetup, errx.DescSetup)
	ErrCloseTempFileFailed                = newSentinelError("failed to close temp file", errx.CodeSetup, errx.DescSetup)
	ErrWriteTempFileFailed                = newSentinelError("failed to write temp file", errx.CodeSetup, errx.DescSetup)
	ErrApplyManagerDeploymentFailed       = newSentinelError("failed to apply manager deployment", errx.CodeSetup, errx.DescSetup)
	ErrClusterIssuerApplyFailed           = newSentinelError("failed to apply ClusterIssuer", errx.CodeSetup, errx.DescSetup)
	ErrCreateRegistryNamespaceFailed      = newSentinelError("failed to create registry namespace", errx.CodeSetup, errx.DescSetup)
	ErrApplyCertificateFailed             = newSentinelError("failed to apply Certificate", errx.CodeSetup, errx.DescSetup)

	// Cert errors.
	ErrCertManagerNotInstalled     = newSentinelError("cert-manager not installed", errx.CodeCert, errx.DescCert)
	ErrCASecretNotFound            = newSentinelError("CA secret not found", errx.CodeCert, errx.DescCert)
	ErrCertificateNotReady         = newSentinelError("certificate not ready", errx.CodeCert, errx.DescCert)
	ErrClusterIssuerNotFound       = newSentinelError("ClusterIssuer not found", errx.CodeCert, errx.DescCert)
	ErrRegistryCertificateNotFound = newSentinelError("registry Certificate not found", errx.CodeCert, errx.DescCert)

	// Cluster errors.
	ErrCRDNotInstalled                = newSentinelError("MCPServer CRD not installed", errx.CodeCluster, errx.DescCluster)
	ErrClusterNotAccessible           = newSentinelError("cluster not accessible", errx.CodeCluster, errx.DescCluster)
	ErrNamespaceNotFound              = newSentinelError("namespace not found", errx.CodeCluster, errx.DescCluster)
	ErrDeploymentTimeout              = newSentinelError("deployment timed out waiting for readiness", errx.CodeCluster, errx.DescCluster)
	ErrInstallCRDFailed               = newSentinelError("failed to install CRD", errx.CodeCluster, errx.DescCluster)
	ErrEnsureRuntimeNamespaceFailed   = newSentinelError("failed to ensure mcp-runtime namespace", errx.CodeCluster, errx.DescCluster)
	ErrEnsureServersNamespaceFailed   = newSentinelError("failed to ensure mcp-servers namespace", errx.CodeCluster, errx.DescCluster)
	ErrKubeconfigNotReadable          = newSentinelError("kubeconfig not found or not readable", errx.CodeCluster, errx.DescCluster)
	ErrSetKubeconfigFailed            = newSentinelError("failed to set KUBECONFIG", errx.CodeCluster, errx.DescCluster)
	ErrSetContextFailed               = newSentinelError("failed to set context", errx.CodeCluster, errx.DescCluster)
	ErrAKSKubeconfigNotImplemented    = newSentinelError("AKS kubeconfig not yet implemented", errx.CodeCluster, errx.DescCluster)
	ErrGKEKubeconfigNotImplemented    = newSentinelError("GKE kubeconfig not yet implemented", errx.CodeCluster, errx.DescCluster)
	ErrUnsupportedProvider            = newSentinelError("unsupported provider", errx.CodeCluster, errx.DescCluster)
	ErrUnsupportedIngressController   = newSentinelError("unsupported ingress controller", errx.CodeCluster, errx.DescCluster)
	ErrInstallIngressControllerFailed = newSentinelError("failed to install ingress controller", errx.CodeCluster, errx.DescCluster)
	ErrCreateKindConfigFailed         = newSentinelError("failed to create temp kind config", errx.CodeCluster, errx.DescCluster)
	ErrCloseKindConfigFailed          = newSentinelError("failed to close kind config", errx.CodeCluster, errx.DescCluster)
	ErrWriteKindConfigFailed          = newSentinelError("failed to write kind config", errx.CodeCluster, errx.DescCluster)
	ErrCreateKindClusterFailed        = newSentinelError("failed to create kind cluster", errx.CodeCluster, errx.DescCluster)
	ErrGKEProvisioningNotImplemented  = newSentinelError("GKE provisioning not yet implemented", errx.CodeCluster, errx.DescCluster)
	ErrProvisionEKSFailed             = newSentinelError("failed to provision EKS cluster", errx.CodeCluster, errx.DescCluster)
	ErrAKSProvisioningNotImplemented  = newSentinelError("AKS provisioning not yet implemented", errx.CodeCluster, errx.DescCluster)

	// Registry errors.
	ErrRegistryNotReady            = newSentinelError("registry not ready", errx.CodeRegistry, errx.DescRegistry)
	ErrRegistryNotFound            = newSentinelError("registry not found", errx.CodeRegistry, errx.DescRegistry)
	ErrBuildOperatorImageFailed    = newSentinelError("failed to build operator image", errx.CodeRegistry, errx.DescRegistry)
	ErrPushOperatorImageFailed     = newSentinelError("failed to push operator image", errx.CodeRegistry, errx.DescRegistry)
	ErrUnsupportedRegistryType     = newSentinelError("unsupported registry type", errx.CodeRegistry, errx.DescRegistry)
	ErrEnsureNamespaceFailed       = newSentinelError("failed to ensure namespace", errx.CodeRegistry, errx.DescRegistry)
	ErrReadRegistryStorageFailed   = newSentinelError("failed to read current registry storage size", errx.CodeRegistry, errx.DescRegistry)
	ErrUpdateRegistryStorageFailed = newSentinelError("failed to update registry storage size", errx.CodeRegistry, errx.DescRegistry)
	ErrRegistryLoginFailed         = newSentinelError("failed to login to registry", errx.CodeRegistry, errx.DescRegistry)
	ErrTagImageFailed              = newSentinelError("failed to tag image", errx.CodeRegistry, errx.DescRegistry)
	ErrPushImageFailed             = newSentinelError("failed to push image", errx.CodeRegistry, errx.DescRegistry)
	ErrHelperNamespaceNotFound     = newSentinelError("helper namespace not found", errx.CodeRegistry, errx.DescRegistry)
	ErrSaveImageFailed             = newSentinelError("failed to save image", errx.CodeRegistry, errx.DescRegistry)
	ErrStartHelperPodFailed        = newSentinelError("failed to start helper pod", errx.CodeRegistry, errx.DescRegistry)
	ErrHelperPodNotReady           = newSentinelError("helper pod not ready", errx.CodeRegistry, errx.DescRegistry)
	ErrCopyImageToHelperFailed     = newSentinelError("failed to copy image tar to helper pod", errx.CodeRegistry, errx.DescRegistry)
	ErrPushImageFromHelperFailed   = newSentinelError("failed to push image from helper pod", errx.CodeRegistry, errx.DescRegistry)

	// Config errors.
	ErrRegistryURLRequired           = newSentinelError("registry url is required", errx.CodeConfig, errx.DescConfig)
	ErrRegistryURLMissingInConfig    = newSentinelError("registry url missing in config", errx.CodeConfig, errx.DescConfig)
	ErrSaveRegistryConfigFailed      = newSentinelError("failed to save registry config", errx.CodeConfig, errx.DescConfig)
	ErrReadRegistryConfigFailed      = newSentinelError("failed to read registry config", errx.CodeConfig, errx.DescConfig)
	ErrUnmarshalRegistryConfigFailed = newSentinelError("failed to unmarshal registry config", errx.CodeConfig, errx.DescConfig)

	// Build errors.
	ErrBuildImageFailed         = newSentinelError("failed to build image", errx.CodeBuild, errx.DescBuild)
	ErrMetadataFileNotFound     = newSentinelError("metadata file not found", errx.CodeBuild, errx.DescBuild)
	ErrServerNotFoundInMetadata = newSentinelError("server not found in metadata", errx.CodeBuild, errx.DescBuild)
	ErrMarshalMetadataFailed    = newSentinelError("failed to marshal metadata", errx.CodeBuild, errx.DescBuild)
	ErrWriteMetadataFailed      = newSentinelError("failed to write metadata", errx.CodeBuild, errx.DescBuild)

	// Server errors.
	ErrMarshalManifestFailed = newSentinelError("failed to marshal manifest", errx.CodeServer, errx.DescServer)
	ErrWriteManifestFailed   = newSentinelError("failed to write manifest", errx.CodeServer, errx.DescServer)
	ErrInvalidFilePath       = newSentinelError("invalid file path", errx.CodeServer, errx.DescServer)
	ErrFileNotAccessible     = newSentinelError("cannot access file", errx.CodeServer, errx.DescServer)
	ErrFileIsDirectory       = newSentinelError("path is a directory, not a file", errx.CodeServer, errx.DescServer)
	ErrGetMCPServerFailed    = newSentinelError("kubectl get mcpserver failed", errx.CodeServer, errx.DescServer)
	ErrListServersFailed     = newSentinelError("failed to list servers", errx.CodeServer, errx.DescServer)
	ErrCreateServerFailed    = newSentinelError("failed to create server", errx.CodeServer, errx.DescServer)
	ErrDeleteServerFailed    = newSentinelError("failed to delete server", errx.CodeServer, errx.DescServer)
	ErrViewServerLogsFailed  = newSentinelError("failed to view server logs", errx.CodeServer, errx.DescServer)
)

func specFor(base error) errorSpec {
	spec, ok := errorSpecs[base]
	if ok {
		return spec
	}
	return errorSpec{code: errx.CodeCLI, description: errx.DescCLI}
}

// TODO: Consider moving this to pkg/errx as a generic logging utility for errx.Error.
// The structured logging logic could be useful for other components beyond the CLI.
// Note: Moving this would require mocking zap.Logger dependencies in tests,
// which suggests it might be better suited as a library function with a testable interface.

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

		// Add cause if present (use distinct field name to avoid duplicate "error" field)
		if cause := errxErr.Cause(); cause != nil {
			fields = append(fields, zap.NamedError("error.cause", cause))
		}

		logger.Error(msg, fields...)
	} else {
		// Fallback for non-errx errors
		logger.Error(msg, zap.Error(err))
	}
}
