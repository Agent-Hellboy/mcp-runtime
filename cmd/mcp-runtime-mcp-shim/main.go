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
	cfg, err := agentadapter.LoadShimConfigFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := agentadapter.RunStdioShim(ctx, cfg, agentadapter.StdioOptions{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
