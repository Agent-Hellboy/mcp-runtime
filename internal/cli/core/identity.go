package core

import (
	"errors"
	"strings"
)

// ResolveEmailAlias returns the single account email represented by --email and
// the deprecated/alias --username flag.
func ResolveEmailAlias(email, username string) (string, error) {
	email = strings.TrimSpace(email)
	username = strings.TrimSpace(username)
	if email != "" && username != "" && !strings.EqualFold(email, username) {
		return "", errors.New("--email and --username must match when both are set")
	}
	if email != "" {
		return email, nil
	}
	return username, nil
}
