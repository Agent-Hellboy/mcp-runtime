package access

import (
	"fmt"
	"regexp"
)

// dns1123SubdomainRegexp matches a Kubernetes-style DNS-1123 subdomain
// (metadata.name / namespace: lowercase, digits, '.', '-'; max 253 per convention).
var dns1123SubdomainRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

const k8sNameMaxLength = 253

// ValidateResourceName returns nil if name is a valid Kubernetes object name
// (DNS-1123 subdomain, <= 253 characters). The field argument is used in the error
// message so callers can distinguish, e.g., "name" from "serverRef.name".
func ValidateResourceName(field, name string) error {
	if name == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(name) > k8sNameMaxLength {
		return fmt.Errorf("%s %q exceeds %d characters", field, name, k8sNameMaxLength)
	}
	if !dns1123SubdomainRegexp.MatchString(name) {
		return fmt.Errorf("%s %q must be a valid DNS-1123 name (lowercase alphanumeric, dots, hyphens)", field, name)
	}
	return nil
}

// ValidateOptionalResourceName is ValidateResourceName but treats empty as valid.
// Use for optional fields like serverRef.namespace.
func ValidateOptionalResourceName(field, name string) error {
	if name == "" {
		return nil
	}
	return ValidateResourceName(field, name)
}
