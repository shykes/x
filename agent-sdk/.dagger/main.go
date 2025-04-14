package main

import (
	"context"
	"dagger/agent-sdk/internal/dagger"
	"fmt"
)

func New(
	// +optional
	// +defaultPath="."
	source *dagger.Directory,
) *AgentSdk {
	return &AgentSdk{Source: source}
}

type AgentSdk struct {
	Source *dagger.Directory
}

func (m *AgentSdk) Codegen(modSource *dagger.ModuleSource, introspectionJSON *dagger.File) (*dagger.GeneratedCode, error) {
	return dag.GeneratedCode(dag.Directory()), nil
}

func (m *AgentSdk) ModuleRuntime(ctx context.Context, modSource *dagger.ModuleSource, introspectionJSON *dagger.File) (*dagger.Container, error) {
	agent, err := moduleRoot(ctx, modSource)
	if err != nil {
		return nil, err
	}
	return dag.Container().
		From("alpine").
		WithFile("/bin/runagent", m.runtimeBin()).
		WithDirectory("/agent", agent).
		WithWorkdir("/agent").
		WithEnvVariable("RUNTIME_WORKDIR", "/agent").
		WithEntrypoint([]string{"/bin/runagent"}), nil
}

func (m *AgentSdk) runtimeBin() *dagger.File {
	return dag.Container().
		From("golang:1.23.6-alpine").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("gomodcache")).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("gobuildcache")).
		WithDirectory("/src", m.Source).
		WithWorkdir("/src").
		WithExec([]string{"go", "build", "-o", "/bin/runagent", "./cmd/runagent"}).
		File("/bin/runagent")
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
