package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"mcp-runtime/internal/dagger"
)

var (
	ctx context.Context
	dag *dagger.Client
)

func init() {
	os.Chdir("/context")
	ctx = context.Background()
	if c, err := dagger.Connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "open dagger session: %s", err.Error())
		os.Exit(1)
	} else {
		dag = c
	}
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	entries, err := dag.Host().Directory(".").Entries(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("workdir=%s\n----\n%s\n----\n", wd, strings.Join(entries, "\n"))
}
