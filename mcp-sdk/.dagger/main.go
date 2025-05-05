package main

import (
	"context"
	"dagger/mcp-sdk/internal/dagger"
	"fmt"
	"strings"
)

func New(
	// +optional
	// +defaultPath="."
	source *dagger.Directory,
	// +default="mcp-runtime"
	binName string,
) *McpSdk {
	return &McpSdk{Source: source, BinName: binName}
}

type McpSdk struct {
	Source       *dagger.Directory
	BinName      string
	TargetModule *dagger.ModuleSource
}

func (m *McpSdk) ModuleRuntime(ctx context.Context, modSource *dagger.ModuleSource, introspectionJSON *dagger.File) (*dagger.Container, error) {
	return m.WithTargetModule(modSource).Runtime(ctx)
}

func (m *McpSdk) TargetModuleRoot(ctx context.Context) (*dagger.Directory, error) {
	if m.TargetModule == nil {
		return nil, fmt.Errorf("no target module")
	}
	rootPath, err := m.TargetModule.SourceRootSubpath(ctx)
	if err != nil {
		return nil, err
	}
	return m.TargetModule.ContextDirectory().Directory(rootPath), nil
}

func (m *McpSdk) Runtime(ctx context.Context) (*dagger.Container, error) {
	mcpServer, err := m.McpFrontend(ctx)
	if err != nil {
		return nil, err
	}
	mcpTools, err := m.McpTools(ctx)
	if err != nil {
		return nil, err
	}
	return dag.Container().
			From("docker.io/library/alpine:latest@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c").
			WithFile("/bin/"+m.BinName, m.bin()).
			WithEntrypoint([]string{"/bin/" + m.BinName}).
			WithServiceBinding("mcp", mcpServer).
			WithFile("/mcp/tools.json", mcpTools).
			Terminal(),
		nil
}

func (m *McpSdk) McpTools(ctx context.Context) (*dagger.File, error) {
	mcpClient, err := m.McpClient(ctx)
	if err != nil {
		return nil, err
	}
	return mcpClient.
			WithExec([]string{"mcptools", "--format", "json", "tools", "http://mcp:4242/sse"}, dagger.ContainerWithExecOpts{
				RedirectStdout: "tools.json",
			}).
			File("tools.json"),
		nil
}

func (m *McpSdk) WithTargetModule(mod *dagger.ModuleSource) *McpSdk {
	m.TargetModule = mod
	return m
}

func (m *McpSdk) McpClientBin() *dagger.File {
	return m.golang().
		WithMountedDirectory(".", dag.Git("https://github.com/f/mcptools").Head().Tree()).
		WithEnvVariable("CGO_ENABLED", "0").
		WithExec([]string{"go", "build", "-ldflags", "-s -w", "-o", "./bin/", "./..."}).
		File("./bin/mcptools")
}

func (m *McpSdk) McpClient(ctx context.Context) (*dagger.Container, error) {
	mcpServer, err := m.McpFrontend(ctx)
	if err != nil {
		return nil, err
	}
	return dag.Container().
			From("alpine").
			WithFiles("/bin/", []*dagger.File{m.McpClientBin()}).
			WithServiceBinding("mcp", mcpServer),
		nil
}

func (m *McpSdk) McpFrontend(ctx context.Context) (*dagger.Service, error) {
	backend, err := m.McpBackend(ctx)
	if err != nil {
		return nil, err
	}
	return dag.Stdio().Proxy(backend), nil
}

func (m *McpSdk) McpBackend(ctx context.Context) (*dagger.Container, error) {
	source, err := m.TargetModuleRoot(ctx)
	if err != nil {
		return nil, err
	}
	fmt.Printf("extracting command info from smithery.yaml..\n")
	mcpCommand, err := ParseSmitheryCommand(ctx, source.File("smithery.yaml"))
	if err != nil {
		return nil, err
	}
	if t := strings.ToLower(mcpCommand.Type); t != "stdio" {
		return nil, fmt.Errorf("unsupported mcp command type: %q. Only 'stdio' is supported.", t)
	}
	fmt.Printf("Building mcp container from Dockerfile...\n")
	return source.DockerBuild().
			With(func(c *dagger.Container) *dagger.Container {
				for k, v := range mcpCommand.Env {
					c = c.WithEnvVariable(k, v)
				}
				return c
			}).
			With(func(c *dagger.Container) *dagger.Container {
				var args []string
				if cmd := mcpCommand.Command; cmd != "" {
					args = append(args, cmd)
				}
				args = append(args, mcpCommand.Args...)
				return c.WithDefaultArgs(args)
			}),
		nil
}

func (m *McpSdk) golang() *dagger.Container {
	return dag.Container().
		From("docker.io/library/golang:alpine@sha256:7772cb5322baa875edd74705556d08f0eeca7b9c4b5367754ce3f2f00041ccee").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("gomodcache")).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("gobuildcache")).
		WithWorkdir("/src")
}

func (m *McpSdk) bin() *dagger.File {
	return m.golang().
		WithDirectory(".", m.Source).
		WithWorkdir("/src/cmd/mcp-runtime").
		WithExec([]string{"go", "build", "-o", "./bin/", "./..."}).
		Directory("bin").
		File(m.BinName)
}

// no-op to implement the Dagger SDK interface
func (m *McpSdk) Codegen(modSource *dagger.ModuleSource, introspectionJSON *dagger.File) (*dagger.GeneratedCode, error) {
	return dag.GeneratedCode(dag.Directory()), nil
}
