package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"mcp-runtime/internal/agentadapter"
)

func main() {
	cfg, err := agentadapter.LoadProxyConfigFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "mcp-runtime-agent-proxy listening on %s -> %s\n", cfg.ListenAddr, cfg.RuntimeURL.String())
	if err := agentadapter.RunHTTPProxy(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
