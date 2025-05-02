package main

import (
	"context"
	"fmt"
	"os"

	"mcp-runtime/internal/dagger"
)

var (
	ctx context.Context
	dag *dagger.Client
)

func init() {
	ctx = context.Background()
	if c, err := dagger.Connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "open dagger session: %s", err.Error())
		os.Exit(1)
	} else {
		dag = c
	}
}
