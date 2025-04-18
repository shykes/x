package main

import (
	"context"
	"dagger/mcp-sdk/internal/dagger"
	"fmt"
)

func New(
	// +optional
	// +defaultPath="."
	source *dagger.Directory,
	// +default="mcp-runtime"
	toolName string,
	// +defaultPath="./stdio"
	stdioTool *dagger.File,
) *McpSdk {
	return &McpSdk{Source: source, ToolName: toolName, StdioTool: stdioTool}
}

type McpSdk struct {
	Source    *dagger.Directory
	ToolName  string
	StdioTool *dagger.File
}

func (m *McpSdk) Codegen(modSource *dagger.ModuleSource, introspectionJSON *dagger.File) (*dagger.GeneratedCode, error) {
	return dag.GeneratedCode(dag.Directory()), nil
}

func (m *McpSdk) ModuleRuntime(ctx context.Context, modSource *dagger.ModuleSource, introspectionJSON *dagger.File) (*dagger.Container, error) {
	modRoot, err := moduleRoot(ctx, modSource)
	if err != nil {
		return nil, err
	}
	return dag.Container().
		From("docker.io/library/alpine:latest@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c").
		WithFile("/bin/"+m.ToolName, m.bin()).
		WithFile("/bin/stdio", m.StdioTool).
		WithMountedDirectory("/mod", modRoot).
		WithEnvVariable("MODULE_ROOT", "/mod").
		WithEntrypoint([]string{"/bin/" + m.ToolName}), nil
}

func (m *McpSdk) bin() *dagger.File {
	return dag.Container().
		From("docker.io/library/golang:1.23.6-alpine@sha256:f8113c4b13e2a8b3a168dceaee88ac27743cc84e959f43b9dbd2291e9c3f57a0").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("gomodcache")).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("gobuildcache")).
		WithDirectory("/src", m.Source).
		WithWorkdir("/src").
		WithExec([]string{"go", "build", "-o", "./bin/", "./..."}).
		Directory("bin").
		File(m.ToolName)
}

// Return the root directory of a dagger module
func moduleRoot(ctx context.Context, mod *dagger.ModuleSource) (*dagger.Directory, error) {
	// The path of the module root in its git context
	rootPath, err := mod.SourceRootSubpath(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get module path: %w", err)
	}
	return mod.ContextDirectory().Directory(rootPath), nil
}
