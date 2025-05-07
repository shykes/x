package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	//"dagger.io/dagger"
	"mcp-runtime/internal/dagger"

	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	if err := Serve(ctx, &Runtime{}); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %s", err.Error())
		os.Exit(2)
	}
}

type Runtime struct {
	Container  *dagger.Container
	FifoVolume *dagger.CacheVolume
	Functions  map[string]*dagger.Function
}

func (r *Runtime) Dispatch(ctx context.Context, call *Call) (any, error) {
	if call.IsInit() {
		objects, err := r.DispatchInit(ctx, call)
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
	if call.IsConstructor() {
		return r.DispatchConstructor(ctx, call)
	}
	if call.IsMainObject() {
		switch call.Name {
		case "container":
			return r.DispatchContainer(ctx, call)
		default:
			return r.DispatchMCPTool(ctx, call)
		}
	}
	return nil, fmt.Errorf("no such object: %q", call.ParentName)
}

func (r *Runtime) DispatchContainer(ctx context.Context, call *Call) (*dagger.Container, error) {
	return r.Container, nil
}

func (r *Runtime) DispatchMCPTool(ctx context.Context, call *Call) (any, error) {
	_, found := r.Functions[call.Name]
	if !found {
		return nil, fmt.Errorf("function not found: %q", call.Name)
	}
	return "FIXME", nil
}

func (r *Runtime) DispatchConstructor(ctx context.Context, call *Call) (map[string]any, error) {
	return nil, nil
	// FIXME: create the mcp servert and return it, so that it gets persisted as a field
}

// Return a list of the module's types
func (r *Runtime) DispatchInit(ctx context.Context, call *Call) ([]*dagger.TypeDef, error) {
	// 1. BUILTIN TYPES AND FUNCTIONS
	modName := call.ModuleName
	root := dag.TypeDef().
		WithObject(modName).
		WithField("mcpServer", dag.TypeDef().WithObject("Service"))
	// Add constructor
	constructor := dag.Function("", root)
	root = root.WithConstructor(constructor)
	// container()
	root = root.WithFunction(
		dag.Function("container", dag.TypeDef().WithObject("container")).WithDescription("Build the MCP server into a container"),
	)
	// 2. DYNAMIC TYPES AND FUNCTIONS (mapped from mcp schema)

	// Inspect MCP tools
	toolsJSON, err := dag.Host().File("/mcp/tools.json").Contents(ctx)
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal([]byte(toolsJSON), &manifest); err != nil {
		return nil, err
	}
	for _, tool := range manifest.Tools {
		fn, err := toolToFunction(tool)
		if err != nil {
			return nil, err
		}
		root = root.WithFunction(fn)
	}
	return []*dagger.TypeDef{root}, nil
}

func print(msg string, err error) {
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\n", msg)
}

func toolToFunction(tool mcp.Tool) (*dagger.Function, error) {
	fn := dag.Function(
		tool.Name,
		dag.TypeDef().WithKind(dagger.TypeDefKindStringKind),
	).WithDescription(tool.Description)

	// required set
	req := map[string]struct{}{}
	for _, v := range tool.InputSchema.Required {
		req[v] = struct{}{}
	}
	for name, raw := range tool.InputSchema.Properties {
		schema, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid property definition: not an object")
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
	return fn, nil
}

func formatTypeName(name string) string {
	return strings.ToUpper(name[0:1]) + strings.ToLower(name[1:])
}

func schemaToTypeDef(field string, s map[string]any) (*dagger.TypeDef, error) {
	switch s["type"] {
	case "string":
		if enum, ok := s["enum"].([]any); ok {
			td := dag.TypeDef().WithEnum(formatTypeName(field))
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
