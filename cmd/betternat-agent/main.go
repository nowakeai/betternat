package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nowakeai/betternat/internal/agent"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := agent.Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "betternat-agent: %v\n", err)
		os.Exit(1)
	}
}
