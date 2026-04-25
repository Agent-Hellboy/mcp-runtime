package access

import (
	"fmt"
	"regexp"
)

// rfc1123LabelRegexp matches a Kubernetes RFC 1123 label / resource name:
// lowercase alphanumeric and hyphens, must start and end with alphanumeric.
// Length is bounded separately; the regexp itself does not enforce 63 chars.
var rfc1123LabelRegexp = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

const rfc1123MaxLength = 63

// ValidateResourceName returns nil if name is a valid Kubernetes resource name
// (RFC 1123 label, <= 63 chars). The field argument is used in the error message
// so callers can distinguish, e.g., "name" from "serverRef.name".
func ValidateResourceName(field, name string) error {
	if name == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(name) > rfc1123MaxLength {
		return fmt.Errorf("%s %q exceeds %d characters", field, name, rfc1123MaxLength)
	}
	if !rfc1123LabelRegexp.MatchString(name) {
		return fmt.Errorf("%s %q must be lowercase alphanumeric with optional hyphens", field, name)
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
