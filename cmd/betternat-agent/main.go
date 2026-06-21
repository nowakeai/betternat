package main

import (
	"context"
	"fmt"
	"os"

	"github.com/betternat/betternat/internal/agent"
)

func main() {
	if err := agent.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "betternat-agent: %v\n", err)
		os.Exit(1)
	}
}
