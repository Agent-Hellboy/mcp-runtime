// Package cli contains shared CLI infrastructure used by command packages.
//
// Command-specific behavior belongs in internal/cli/<command>; this package is
// limited to config, constants, errors, runtime composition, process execution,
// kubectl clients, terminal output, and test doubles.
package core
