package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"dagger.io/dagger"
)

func main() {
	runtime, err := InitRuntime(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %s", err.Error())
		os.Exit(1)
	}
	if err := Serve(ctx, runtime); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %s", err.Error())
		os.Exit(2)
	}
}

func moduleSource() *dagger.Directory {
	fmt.Printf("MODULE_ROOT=%s\n", os.Getenv("MODULE_ROOT"))
	src := dag.Host().Directory(os.Getenv("MODULE_ROOT"))
	if contents, err := src.Glob(ctx, "**"); err == nil {
		fmt.Printf("----\n%s\n---\n", contents)
	}
	if contents, err := src.File("dagger.json").Contents(ctx); err == nil {
		fmt.Println(contents)
	}
	return src
}

func print(msg string, err error) {
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\n", msg)
}

func InitRuntime(ctx context.Context) (*Runtime, error) {
	src := moduleSource()
	fmt.Printf("Building mcp container from Dockerfile...\n")
	ctr, err := src.
		DockerBuild().
		WithFile("/bin/stdio", dag.Host().File("/bin/stdio")).
		Sync(ctx)
	if err != nil {
		return nil, err
	}
	fmt.Printf("extracting command info from smithery.yaml..\n")
	mcpCommand, err := ParseSmitheryCommand(ctx, src.File("smithery.yaml"))
	if err != nil {
		return nil, err
	}
	fmt.Printf("---- MCP COMMAND ----\n%#v\n--------", mcpCommand)
	if t := strings.ToLower(mcpCommand.Type); t != "stdio" {
		return nil, fmt.Errorf("unsupported mcp command type: %q. Only 'stdio' is supported.", t)
	}
	for k, v := range mcpCommand.Env {
		ctr = ctr.WithEnvVariable(k, v)
	}
	args := []string{"stdio"}
	if cmd := mcpCommand.Command; cmd != "" {
		args = append(args, cmd)
	}
	args = append(args, mcpCommand.Args...)
	ctr = ctr.WithExec(args, dagger.ContainerWithExecOpts{

		//		Stdin: `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{},"clientInfo":{"name":"goose","version":"1.0.18"},"protocolVersion":"2025-03-26"}}
		//{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
		//`,
	})
	stdout, err := ctr.Stdout(ctx)
	if err != nil {
		return nil, err
	}
	stderr, err := ctr.Stderr(ctx)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		Container:   ctr,
		McpCommand:  mcpCommand,
		ProbeStdout: stdout,
		ProbeStderr: stderr,
	}, nil
}

type Runtime struct {
	Container   *dagger.Container
	McpCommand  *CommandSpec
	ProbeStdout string
	ProbeStderr string
}

func (r *Runtime) Dispatch(ctx context.Context, call *Call) (any, error) {
	if call.IsConstructor() {
		objects, err := r.DispatchConstructor(ctx, call)
		if err != nil {
			return nil, err
		}
		// FIXME: move boilerplate to utils
		mod := dag.Module()
		for _, obj := range objects {
			mod = mod.WithObject(obj)
		}
		return mod, nil
	}
	if call.IsMainObject() {
		switch call.Name {
		case "debug":
			return r.DispatchDebug(ctx, call)
		default:
			return nil, fmt.Errorf("object %q has no function %q", call.ParentName, call.Name)
		}
	}
	return nil, fmt.Errorf("no such object: %q", call.ParentName)
}

func (r *Runtime) DispatchConstructor(ctx context.Context, call *Call) ([]*dagger.TypeDef, error) {
	root := dag.TypeDef().WithObject(call.ModuleName)
	// Debug function
	fDebug := dag.Function("debug", dag.TypeDef().WithKind(dagger.TypeDefKindStringKind))
	root = root.WithFunction(fDebug)
	return []*dagger.TypeDef{
		root,
	}, nil
}

func (r *Runtime) DispatchDebug(ctx context.Context, call *Call) (string, error) {
	return fmt.Sprintf("# STDOUT\n\n%s\n\n# STDERR\n\n%s\n", r.ProbeStdout, r.ProbeStderr), nil
}
