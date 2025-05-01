package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"

	"dagger.io/dagger"
)

func init() { rand.Seed(time.Now().UnixNano()) }

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
	mcpServer, err := r.initMcpServer(ctx)
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
		fn, err := tool.Function()
		if err != nil {
			return nil, err
		}
		root = root.WithFunction(fn)
	}
	// Return all types
	return []*dagger.TypeDef{root}, nil
}

func (r *Runtime) initMcpServer(ctx context.Context) (*MCPServer, error) {
	src := moduleSource()
	fmt.Printf("Building mcp container from Dockerfile...\n")
	ctr, err := src.DockerBuild().Sync(ctx)
	if err != nil {
		return nil, err
	}
	fmt.Printf("extracting command info from smithery.yaml..\n")
	mcpCommand, err := ParseSmitheryCommand(ctx, src.File("smithery.yaml"))
	if err != nil {
		return nil, err
	}
	if t := strings.ToLower(mcpCommand.Type); t != "stdio" {
		return nil, fmt.Errorf("unsupported mcp command type: %q. Only 'stdio' is supported.", t)
	}
	for k, v := range mcpCommand.Env {
		ctr = ctr.WithEnvVariable(k, v)
	}
	var args []string
	if cmd := mcpCommand.Command; cmd != "" {
		args = append(args, cmd)
	}
	args = append(args, mcpCommand.Args...)
	srv, err := NewStdioServer(ctr, args, dag.Host().File("/bin/stdio"))
	if err != nil {
		return nil, err
	}
	return &MCPServer{srv}, nil
}

type MCPServer struct {
	*StdioServer
}

func (mcp *MCPServer) ListTools(ctx context.Context) ([]Tool, error) {
	// Start the mcp server
	_, err := mcp.Start(ctx)
	if err != nil {
		return nil, err
	}
	// Start the proxt
	conn, err := mcp.StdioServer.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return mcp.listTools(ctx, conn, conn)
}

// ListTools sends a `tools/list` request and returns all tools the server exposes.
//
// * ctx stops the operation early (handy for time-outs).
// * r / w are the already-established MCP transport (stdio, TCP, …).
func (mcp *MCPServer) listTools(ctx context.Context, r io.Reader, w io.Writer) ([]Tool, error) {
	enc := json.NewEncoder(w)
	scn := bufio.NewScanner(r)
	scn.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // allow big tool lists

	var all []Tool
	var cursor any
	for {
		// Every page uses a fresh request id to avoid collisions.
		reqID := rand.Int63()

		req := map[string]any{
			"jsonrpc": "2.0",
			"id":      reqID,
			"method":  "tools/list",
			"params":  map[string]any{},
		}
		if cursor != nil {
			req["params"].(map[string]any)["cursor"] = cursor
		}
		if err := enc.Encode(req); err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}

		// Read messages until we get the matching response.
		for scn.Scan() {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			rawLine := scn.Bytes()

			// Quick pre-parse: figure out whether this message has an "id".
			var hdr struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"` // notifications have no id
			}
			if err := json.Unmarshal(rawLine, &hdr); err != nil {
				continue // malformed noise – ignore
			}

			// Notifications (pings, progress, list_changed, …) ⇒ ignore gracefully.
			if len(hdr.ID) == 0 {
				continue
			}

			var lineID int64
			if err := json.Unmarshal(hdr.ID, &lineID); err != nil || lineID != reqID {
				continue // response to some other request
			}

			// This *is* the response we’re waiting for.
			var resp struct {
				Result struct {
					Tools      []Tool `json:"tools"`
					NextCursor any    `json:"nextCursor"`
				} `json:"result"`
				Error *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error,omitempty"`
			}
			if err := json.Unmarshal(rawLine, &resp); err != nil {
				return nil, fmt.Errorf("decode tools/list response: %w", err)
			}
			if resp.Error != nil {
				return nil, errors.New(resp.Error.Message)
			}

			all = append(all, resp.Result.Tools...)
			cursor = resp.Result.NextCursor
			break
		}
		if err := scn.Err(); err != nil {
			return nil, fmt.Errorf("read from server: %w", err)
		}
		if cursor == nil {
			return all, nil // no more pages
		}
	}
}

type StdioServer struct {
	ctr        *dagger.Container
	fifoVolume *dagger.CacheVolume
	stdioTool  *dagger.File
	args       []string
}

func NewStdioServer(ctr *dagger.Container, args []string, stdioTool *dagger.File) (*StdioServer, error) {
	// Setup fifo
	fifoVolume := tempVolume()
	_, err := dag.Container().
		From("alpine").
		WithMountedCache("/fifo", fifoVolume).
		WithWorkdir("/fifo").
		With(func(c *dagger.Container) *dagger.Container {
			// Cache buster
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			cachebuster := fmt.Sprintf("%08x%08x", r.Uint32(), r.Uint32())
			return c.WithEnvVariable("CACHE_BUSTER", cachebuster)
		}).
		WithExec([]string{"mkfifo", "in", "out", "err"}).
		Sync(ctx)
	if err != nil {
		return nil, err
	}
	return &StdioServer{
		ctr:        ctr,
		fifoVolume: fifoVolume,
		stdioTool:  stdioTool,
		args:       args,
	}, nil
}

func (srv *StdioServer) Start(ctx context.Context) (*dagger.Service, error) {
	var args []string
	args = append(args, "stdio")
	args = append(args, srv.args...)
	return srv.ctr.
		WithFile("/bin/stdio", srv.stdioTool).
		WithEnvVariable("FIFO_PREFIX", "/fifo").
		WithMountedCache("/fifo", srv.fifoVolume).
		AsService(dagger.ContainerAsServiceOpts{
			Args: args,
		}).
		Start(ctx)
}

func (srv *StdioServer) Connect(ctx context.Context) (io.ReadWriter, error) {
	proxy, err := dag.Container().
		From("alpine/socat").
		WithMountedCache("/fifo", srv.fifoVolume).
		WithExposedPort(4242).
		AsService(dagger.ContainerAsServiceOpts{
			//Args: []string{"socat", "TCP-LISTEN:4242,reuseaddr,fork", "OPEN:/fifo/in,rdonly!OPEN:/fifo/out,wronly"},
			//	Args: []string{"socat", "UNIX-LISTEN:/var/run/stdio-proxy.sock,reuseaddr,fork", "OPEN:/fifo/in,rdonly", "OPEN:/fifo/out,wronly"},
			Args: []string{"sh", "-c", "ls -l /fifo; while true; do nc -l -p 4242 </fifo/out >/fifo/in; done"},
			//Args: []string{"sh", "-c", "while true; do echo BOO | nc -l -p 4242; done"},
		}).Start(ctx)
	if err != nil {
		return nil, err
	}
	go func() {
		if err := proxy.Up(ctx); err != nil {
			panic(err) // FIXME
		}
	}()
	fmt.Printf("waiting before connecting...\n")
	time.Sleep(1 * time.Second)
	fmt.Printf("connecting...\n")
	return net.Dial("tcp", "localhost:4242")
}

func print(msg string, err error) {
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\n", msg)
}

func tempVolume() *dagger.CacheVolume {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	id := fmt.Sprintf("%08x%08x", r.Uint32(), r.Uint32())
	return dag.CacheVolume(id)
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

// MCP tool spec
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema map[string]any  `json:"inputSchema"`
	Annotations json.RawMessage `json:"annotations,omitempty"`
}

func (tool Tool) Function() (*dagger.Function, error) {
	fn := dag.Function(
		tool.Name,
		dag.TypeDef().WithKind(dagger.TypeDefKindStringKind),
	).WithDescription(tool.Description)

	// required set
	req := map[string]struct{}{}
	if r, ok := tool.InputSchema["required"].([]any); ok {
		for _, v := range r {
			if s, ok := v.(string); ok {
				req[s] = struct{}{}
			}
		}
	}

	// args
	if props, ok := tool.InputSchema["properties"].(map[string]any); ok {
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
