// Package cli provides CLI commands for the mcp-runtime.
package cli

import "errors"

// Sentinel errors for CLI operations.
var (
	// ErrRegistryNotReady indicates the registry deployment is not ready.
	ErrRegistryNotReady = errors.New("registry not ready")

	// ErrRegistryNotFound indicates the registry deployment was not found.
	ErrRegistryNotFound = errors.New("registry not found")

	// ErrOperatorNotFound indicates the operator deployment was not found.
	ErrOperatorNotFound = errors.New("operator not found")

	// ErrOperatorNotReady indicates the operator deployment is not ready.
	ErrOperatorNotReady = errors.New("operator not ready")

	// ErrCRDNotInstalled indicates the MCPServer CRD is not installed.
	ErrCRDNotInstalled = errors.New("MCPServer CRD not installed")

	// ErrClusterNotAccessible indicates the Kubernetes cluster is not accessible.
	ErrClusterNotAccessible = errors.New("cluster not accessible")

	// ErrCertManagerNotInstalled indicates cert-manager is not installed.
	ErrCertManagerNotInstalled = errors.New("cert-manager not installed")

	// ErrCASecretNotFound indicates the CA secret for TLS is not found.
	ErrCASecretNotFound = errors.New("CA secret not found")

	// ErrImageRequired indicates an image parameter is required but not provided.
	ErrImageRequired = errors.New("image is required")

	// ErrInvalidServerName indicates the server name is invalid.
	ErrInvalidServerName = errors.New("invalid server name: must be lowercase alphanumeric with optional hyphens")

	// ErrNamespaceNotFound indicates a namespace was not found.
	ErrNamespaceNotFound = errors.New("namespace not found")

	// ErrDeploymentTimeout indicates a deployment did not become ready in time.
	ErrDeploymentTimeout = errors.New("deployment timed out waiting for readiness")
)
