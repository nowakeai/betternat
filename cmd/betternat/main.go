package main

import (
	"context"
	"fmt"
	"os"

	"github.com/betternat/betternat/internal/cli"
)

func main() {
	if err := cli.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "betternat: %v\n", err)
		os.Exit(1)
	}
}
