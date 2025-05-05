package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	//"dagger.io/dagger"
	"mcp-runtime/internal/dagger"

	"github.com/ThinkInAIXYZ/go-mcp/client"
	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/transport"
)

func main() {
	if err := Serve(ctx, &Runtime{}); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %s", err.Error())
		os.Exit(2)
	}
}

type Runtime struct {
	Container  *dagger.Container
	McpCommand *CommandSpec
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
	// Build & run MCP server
	mcpServer, err := r.McpServer(ctx)
	if err != nil {
		return nil, err
	}
	// Inspect MCP tools
	tools, err := mcpServer.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	// Expose each tool as a function
	for _, tool := range tools {
		fn, err := toolToFunction(tool)
		if err != nil {
			return nil, err
		}
		root = root.WithFunction(fn)
	}
	// Return all types
	return []*dagger.TypeDef{root}, nil
}

func (r *Runtime) McpServer(ctx context.Context) (*MCPServer, error) {
	src := moduleSource()
	fmt.Printf("extracting command info from smithery.yaml..\n")
	mcpCommand, err := ParseSmitheryCommand(ctx, src.File("smithery.yaml"))
	if err != nil {
		return nil, err
	}
	if t := strings.ToLower(mcpCommand.Type); t != "stdio" {
		return nil, fmt.Errorf("unsupported mcp command type: %q. Only 'stdio' is supported.", t)
	}
	fmt.Printf("Building mcp container from Dockerfile...\n")
	ctr := src.DockerBuild().
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
		})
	return &MCPServer{
		Container: ctr,
	}, nil
}

type MCPServer struct {
	Container *dagger.Container
}

func (mcp *MCPServer) ListTools(ctx context.Context) ([]*protocol.Tool, error) {
	// Connect to the mcp server
	conn, err := mcp.Connect(ctx)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Listing tools..\n")
	return mcp.listTools(ctx, conn)
}

func (mcp *MCPServer) Proxy() *dagger.Service {
	return dag.Stdio().Proxy(mcp.Container)
}

func (mcp *MCPServer) Connect(ctx context.Context) (net.Conn, error) {
	proxy, err := mcp.Proxy().Start(ctx)
	if err != nil {
		return nil, err
	}
	go proxy.Up(ctx)
	time.Sleep(1 * time.Second)
	// DEBUG
	resp, err := dag.Container().
		From("alpine:latest").
		WithExec([]string{"apk", "add", "--no-cache", "netcat-openbsd"}).
		WithServiceBinding("mcp", proxy).
		WithExec([]string{
			"/usr/bin/nc", "-q", "1", "mcp", "4242",
		}).
		Stdout(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("tools:", resp)
	panic(fmt.Sprintf("tools: <<<%s>>>", resp))

	return net.Dial("tcp", "localhost:4242")
}

func (mcp *MCPServer) listTools(ctx context.Context, conn net.Conn) ([]*protocol.Tool, error) {
	// wrap the raw conn as a ClientTransport
	t := transport.NewMockClientTransport(conn, conn)
	// create the MCP client over that transport
	mc, err := client.NewClient(t)
	if err != nil {
		return nil, err
	}
	defer mc.Close()

	// list and return tools
	res, err := mc.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	return res.Tools, nil
}

func print(msg string, err error) {
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\n", msg)
}

func toolToFunction(tool *protocol.Tool) (*dagger.Function, error) {
	fn := dag.Function(
		tool.Name,
		dag.TypeDef().WithKind(dagger.TypeDefKindStringKind),
	).WithDescription(tool.Description)

	// required set
	req := map[string]struct{}{}
	for _, v := range tool.InputSchema.Required {
		req[v] = struct{}{}
	}

	var inputSchema struct {
		Properties map[string]any
	}
	if err := json.Unmarshal(tool.RawInputSchema, &inputSchema); err != nil {
		return nil, err
	}
	// args
	for name, raw := range inputSchema.Properties {
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
