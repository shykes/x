package main

import (
	"context"
	"dagger/llm/internal/dagger"

	"fmt"
)

var providers = []LlmProvider{
	// Insert LLM implementations here
	OpenAI{},
}

func New(
	ctx context.Context,
	// LLM model name
	// +optional
	// +default="gpt-4o"
	model string,
	// A builtin knowledge library, made of text files.
	// First paragraph is the description. The rest is the contents.
	// +optional
	// +defaultPath=./knowledge
	knowledgeDir *dagger.Directory,
	// A system prompt to inject into the LLM context
	// +optional
	// +defaultPath="./system-prompt.txt"
	systemPrompt *dagger.File,
) (Llm, error) {
	llm := Llm{
		Model: model,
	}
	sandbox, err := NewSandbox().WithUsername("🤖").ImportManuals(ctx, knowledgeDir)
	if err != nil {
		return llm, err
	}
	llm.Sandbox = sandbox
	prompt, err := systemPrompt.Contents(ctx)
	if err != nil {
		return llm, err
	}
	return llm.WithSystemPrompt(ctx, prompt)
}

type Llm struct {
	// LLM model name
	Model string // +private
	// Optional API token
	Token *dagger.Secret // +private
	// Optional API endpoint (eg. for local models)
	Endpoint *dagger.Service // +private
	// Opaque LLM state
	State   string // +private
	Sandbox Sandbox
}

type LlmProvider interface {
	// Return a list of supported models
	Models() []string
	// Initialize a new llm state
	New() LlmState
	// Load state from provider-specific serialized format
	Load(string) (LlmState, error)
}

type LlmState interface {
	// Serialize the state to an opaque provider-specific string
	Save() (string, error)
	// Append a user prompt to the state, without sending
	WithPrompt(string) LlmState
	// Append a system prompt to the state, without sending
	WithSystemPrompt(string) LlmState
	// Send a single API query, and process replies and tool calls
	Query(
		ctx context.Context,
		model string,
		endpoint *dagger.Service,
		token *dagger.Secret,
		shells []ShellTool,
		manuals []ManualTool,
	) (string, LlmState, error)
}

type ShellTool interface {
	Name() string
	Description() string
	Run(context.Context, string) (string, error)
}

type RunResult interface {
	Output() string
	Error() string
	Success() bool
	ToJSON() (string, error)
}

type ManualTool interface {
	Name() string
	Description() string
	Contents(context.Context) (string, error)
}

// Configure an API token to authenticate against the LLM provider
func (m Llm) WithToken(token *dagger.Secret) Llm {
	m.Token = token
	return m
}

// Configure an API endpoint to send LLM requests
// Use this for local models, or hosted models that support multiple endpoints
func (m Llm) WithEndpoint(endpoint *dagger.Service) Llm {
	m.Endpoint = endpoint
	return m
}

func (m Llm) WithSandbox(sandbox Sandbox) Llm {
	m.Sandbox = sandbox
	return m
}

func (m Llm) WithSecret(name string, value *dagger.Secret) Llm {
	m.Sandbox = m.Sandbox.WithSecret(name, value)
	return m
}

func (m Llm) WithDirectory(dir *dagger.Directory) Llm {
	m.Sandbox = m.Sandbox.WithHome(m.Sandbox.Home.WithDirectory(".", dir))
	return m
}

// Configure a remote module as context for the sandbox
func (m Llm) WithRemoteModule(address string) Llm {
	m.Sandbox = m.Sandbox.WithRemoteModule(address)
	return m
}

// Configure a local module as context for the sandbox
func (m Llm) WithLocalModule(module *dagger.Directory) Llm {
	m.Sandbox = m.Sandbox.WithLocalModule(module)
	return m
}

func (m Llm) History() []string {
	return m.Sandbox.History
}

func (m Llm) WithPrompt(ctx context.Context, prompt string) (Llm, error) {
	m.Sandbox = m.Sandbox.WithNote(ctx, prompt, "🧑")
	st, err := m.llmState()
	if err != nil {
		return m, err
	}
	st = st.WithPrompt(prompt)
	return m.withLlmState(st)
}

func (m Llm) WithSystemPrompt(ctx context.Context, prompt string) (Llm, error) {
	m.Sandbox = m.Sandbox.WithNote(ctx, prompt, "🧬")
	st, err := m.llmState()
	if err != nil {
		return m, err
	}
	return m.withLlmState(st.WithSystemPrompt(prompt))
}

func (m Llm) Ask(
	ctx context.Context,
	// The message to send the model
	prompt string,
) (out Llm, rerr error) {
	m, err := m.WithPrompt(ctx, prompt)
	if err != nil {
		return m, err
	}
	st, err := m.llmState()
	if err != nil {
		return m, err
	}
	var reply string
	for {
		// Each query gets a tool server instance with its own call counter.
		tools := m.toolServer()
		reply, st, err = st.Query(ctx, m.Model, m.Endpoint, m.Token, tools.Shells(), tools.Manuals())
		if err != nil {
			return m, err
		}
		if len(reply) != 0 {
			tools.sandbox = tools.sandbox.WithNote(ctx, reply, "")
		}
		m.Sandbox = tools.sandbox
		if tools.Count() == 0 {
			break
		}
	}
	return m, nil
}

func (m Llm) toolServer() toolServer {
	return toolServer{
		sandbox: m.Sandbox,
	}
}

type toolServer struct {
	sandbox Sandbox
	count   int
}

func (ts *toolServer) Count() int {
	return ts.count
}

func (ts *toolServer) Shells() []ShellTool {
	return []ShellTool{ts.daggerShell()}
}

func (ts *toolServer) daggerShell() ShellTool {
	return &daggerShell{ts}
}

type daggerShell struct {
	*toolServer
}

func (ds *daggerShell) Name() string {
	return "dagger"
}

func (dsh *daggerShell) Description() string {
	return "Execute a dagger script. <prerequisite>read the dagger manual</prerequisite>"
}

func (dsh *daggerShell) Run(ctx context.Context, script string) (string, error) {
	dsh.count += 1
	var err error
	dsh.sandbox, err = dsh.sandbox.Run(ctx, script)
	if err != nil {
		return "", err
	}
	result, err := dsh.sandbox.LastRun()
	if err != nil {
		return "", err
	}
	return result.ToJSON()
}

func (ts *toolServer) Manuals() []ManualTool {
	var manuals []ManualTool
	for _, manual := range ts.sandbox.Manuals {
		manuals = append(manuals, manualTool{
			toolServer:  ts,
			name:        manual.Name,
			description: manual.Description,
		})
	}
	return manuals
}

type manualTool struct {
	*toolServer
	name        string
	description string
}

func (m manualTool) Name() string {
	return m.name
}

func (m manualTool) Description() string {
	return m.description
}

func (m manualTool) Contents(ctx context.Context) (string, error) {
	// FIXME: move otel custom span to here
	m.count += 1
	return m.sandbox.ReadManual(ctx, m.name)
}

func (m Llm) llmState() (LlmState, error) {
	provider, err := m.llmProvider()
	if err != nil {
		return nil, err
	}
	return provider.Load(m.State)
}

func (m Llm) withLlmState(st LlmState) (Llm, error) {
	var err error
	m.State, err = st.Save()
	return m, err
}

func (m Llm) llmProvider() (LlmProvider, error) {
	for _, provider := range providers {
		for _, model := range provider.Models() {
			if model == m.Model {
				return provider, nil
			}
		}
	}
	return nil, fmt.Errorf("no provider for model: %s", m.Model)
}
