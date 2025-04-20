package main

import (
	"context"
	"encoding/json"
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
	ctr = ctr.WithExec(args)
	stdout, err := ctr.Stdout(ctx)
	if err != nil {
		return nil, err
	}
	stderr, err := ctr.Stderr(ctx)
	if err != nil {
		return nil, err
	}
	tools, err := parseMCPTools(stdout)
	if err != nil {
		return nil, err
	}
	functions := map[string]*dagger.Function{}
	for _, tool := range tools {
		fn, err := tool.Function()
		if err != nil {
			return nil, err
		}
		functions[tool.Name] = fn
	}
	return &Runtime{
		Container:   ctr,
		McpCommand:  mcpCommand,
		ProbeStdout: stdout,
		ProbeStderr: stderr,
		Functions:   functions,
	}, nil
}

func parseMCPTools(raw string) ([]Tool, error) {
	var resp struct {
		Result struct {
			Tools []Tool
		} `json:"result"`
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, err
	}
	return resp.Result.Tools, nil
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Args        map[string]any `json:"inputSchema"`
}

type MCP struct {
	Name        string
	Description string
	Args        map[string]any
}

type Runtime struct {
	Container   *dagger.Container
	McpCommand  *CommandSpec
	ProbeStdout string
	ProbeStderr string
	Functions   map[string]*dagger.Function
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
			return r.DispatchMCPTool(ctx, call)
		}
	}
	return nil, fmt.Errorf("no such object: %q", call.ParentName)
}

func (r *Runtime) DispatchMCPTool(ctx context.Context, call *Call) (any, error) {
	_, found := r.Functions[call.Name]
	if !found {
		return nil, fmt.Errorf("function not found: %q", call.Name)
	}
	return "FIXME", nil
}

func (r *Runtime) DispatchConstructor(ctx context.Context, call *Call) ([]*dagger.TypeDef, error) {
	root := dag.TypeDef().WithObject(call.ModuleName)
	// Debug function
	fDebug := dag.Function("debug", dag.TypeDef().WithKind(dagger.TypeDefKindStringKind)).WithDescription("Debug the internals of the MCP server")
	root = root.WithFunction(fDebug)
	for _, fn := range r.Functions {
		root = root.WithFunction(fn)
	}
	return []*dagger.TypeDef{
		root,
	}, nil
}

func (r *Runtime) DispatchDebug(ctx context.Context, call *Call) (string, error) {
	return fmt.Sprintf("# STDOUT\n\n%s\n\n# STDERR\n\n%s\n", r.ProbeStdout, r.ProbeStderr), nil
}

func (tool Tool) Function() (*dagger.Function, error) {
	fn := dag.Function(
		tool.Name,
		dag.TypeDef().WithKind(dagger.TypeDefKindStringKind),
	).WithDescription(tool.Description)

	// required set
	req := map[string]struct{}{}
	if r, ok := tool.Args["required"].([]any); ok {
		for _, v := range r {
			if s, ok := v.(string); ok {
				req[s] = struct{}{}
			}
		}
	}

	// args
	if props, ok := tool.Args["properties"].(map[string]any); ok {
		for name, raw := range props {
			schema, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			td, err := schemaToTypeDef(name, schema)
			if err != nil {
				return nil, err
			}
			if _, isReq := req[name]; !isReq {
				td = td.WithOptional(true)
			}
			fn = fn.WithArg(name, td)
		}
	}
	return fn, nil
}

func schemaToTypeDef(field string, s map[string]any) (*dagger.TypeDef, error) {
	switch s["type"] {
	case "string":
		if enum, ok := s["enum"].([]any); ok {
			td := dag.TypeDef().WithEnum(strings.Title(field))
			for _, v := range enum {
				if sv, ok := v.(string); ok {
					td = td.WithEnumValue(sv)
				}
			}
			return td, nil
		}
		return dag.TypeDef().WithKind(dagger.TypeDefKindStringKind), nil
	case "integer":
		return dag.TypeDef().WithKind(dagger.TypeDefKindIntegerKind), nil
	case "number":
		return dag.TypeDef().WithKind(dagger.TypeDefKindFloatKind), nil
	case "boolean":
		return dag.TypeDef().WithKind(dagger.TypeDefKindBooleanKind), nil
	case "array":
		items, ok := s["items"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("array %s lacks items", field)
		}
		elem, err := schemaToTypeDef(field+"Item", items)
		if err != nil {
			return nil, err
		}
		return dag.TypeDef().WithListOf(elem), nil
	default:
		return nil, fmt.Errorf("unsupported json type %v for %s", s["type"], field)
	}
}
