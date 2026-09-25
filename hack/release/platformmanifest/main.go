// Command platformmanifest prints the platform release component manifest
// (platform-manifest.json) for a release version, generated from the CLI's
// component catalog so `mcp-runtime update` and releases stay in sync.
//
//	go run ./hack/release/platformmanifest -version v0.5.0 [-crd-change]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"mcp-runtime/internal/platformrelease"
)

func main() {
	version := flag.String("version", "", "release version (semver, for example v0.5.0)")
	crdChange := flag.Bool("crd-change", false, "mark the release as changing CRDs (update refuses; use setup)")
	flag.Parse()

	m, err := platformrelease.GenerateManifest(*version, *crdChange)
	if err != nil {
		fmt.Fprintf(os.Stderr, "platformmanifest: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		fmt.Fprintf(os.Stderr, "platformmanifest: %v\n", err)
		os.Exit(1)
	}
}
