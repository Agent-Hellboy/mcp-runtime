package errx_test

import (
	"errors"
	"fmt"

	"mcp-runtime/internal/cli"
	"mcp-runtime/pkg/errx"
)

func Example() {
	kubeErr := errors.New("kubectl apply failed")

	err := errx.WrapRegistry("failed to apply registry manifests", kubeErr).
		WithBase(cli.ErrRegistryNotReady).
		WithContext("namespace", "mcp-system").
		WithContext("resource", "registry")

	if errors.Is(err, cli.ErrRegistryNotReady) {
		fmt.Println("registry not ready")
	}

	fmt.Println(errx.UserString(err))
	_ = errx.DebugString(err)
}
