package main

import (
	"context"
	"dagger/mcp-sdk/internal/dagger"
)

func New(
	// +optional
	// +defaultPath="."
	source *dagger.Directory,
	// +default="mcp-runtime"
	toolName string,
) *McpSdk {
	return &McpSdk{Source: source, ToolName: toolName}
}

type McpSdk struct {
	Source   *dagger.Directory
	ToolName string
}

func (m *McpSdk) Codegen(modSource *dagger.ModuleSource, introspectionJSON *dagger.File) (*dagger.GeneratedCode, error) {
	return dag.GeneratedCode(dag.Directory()), nil
}

func (m *McpSdk) ModuleRuntime(ctx context.Context, modSource *dagger.ModuleSource, introspectionJSON *dagger.File) (*dagger.Container, error) {
	modRootPath, err := modSource.SourceRootSubpath(ctx)
	if err != nil {
		return nil, err
	}
	return dag.Container().
		From("docker.io/library/alpine:latest@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c").
		WithFile("/bin/"+m.ToolName, m.bin()).
		WithMountedDirectory("/context", modSource.ContextDirectory()).
		WithEnvVariable("MODULE_ROOT", "/context/"+modRootPath).
		WithEntrypoint([]string{"/bin/" + m.ToolName}).
		Terminal(dagger.ContainerTerminalOpts{
			Cmd:                           []string{"/bin/sh"},
			ExperimentalPrivilegedNesting: true,
		}), nil
}

func (m *McpSdk) bin() *dagger.File {
	return dag.Container().
		From("docker.io/library/golang:1.23.6-alpine@sha256:f8113c4b13e2a8b3a168dceaee88ac27743cc84e959f43b9dbd2291e9c3f57a0").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("gomodcache")).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("gobuildcache")).
		WithDirectory("/src", m.Source).
		WithWorkdir("/src/cmd/mcp-runtime").
		WithExec([]string{"go", "build", "-o", "./bin/", "./..."}).
		Directory("bin").
		File(m.ToolName)
}
