package main

import (
	"context"
	"dagger/gpt/internal/dagger"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/openai/openai-go"
	"go.opentelemetry.io/otel/codes"
)

func New(
	ctx context.Context,
	// OpenAI API token
	token *dagger.Secret,
	// OpenAI model
	// +optional
	// +default="gpt-4o"
	model ModelName,
	// A builtin knowledge library, made of text files.
	// First paragraph is the description. The rest is the contents.
	// +optional
	// +defaultPath=./knowledge
	knowledgeDir *dagger.Directory,
	// A system prompt to inject into the GPT context
	// +optional
	// +defaultPath="./system-prompt.txt"
	systemPrompt *dagger.File,
) (Gpt, error) {
	gpt := Gpt{
		Token:   token,
		Model:   model,
		Workdir: dag.Directory(),
	}
	prompt, err := systemPrompt.Contents(ctx)
	if err != nil {
		return gpt, err
	}
	return gpt.
		WithSystemPrompt(ctx, prompt).
		WithKnowledgeDir(ctx, knowledgeDir)
}

type Gpt struct {
	Model         ModelName      // +private
	Token         *dagger.Secret // +private
	HistoryJSON   string         // +private
	Log           []string
	ShellHistory  []Command   // +private
	LastReply     string      // +private
	KnowledgeBase []Knowledge // +private
	Workdir       *dagger.Directory
}

// An OpenAI model name
type ModelName = string

// Add knowledge by reading text files from a directory
// Any .txt or .md file will be read.
// - The first paragraph is the description
// - The rest of the file is the contents
func (gpt Gpt) WithKnowledgeDir(ctx context.Context, dir *dagger.Directory) (Gpt, error) {
	txtPaths, err := dir.Glob(ctx, "**/*.txt")
	if err != nil {
		return gpt, err
	}
	mdPaths, err := dir.Glob(ctx, "**/*.md")
	if err != nil {
		return gpt, err
	}
	paths := append(txtPaths, mdPaths...)
	toolnameRE := regexp.MustCompile("[^a-zA-Z0-9_-]")
	for _, p := range paths {
		doc, err := dir.File(p).Contents(ctx)
		if err != nil {
			return gpt, err
		}
		// Use regex to split paragraphs, allowing for any amount of whitespace or newlines
		re := regexp.MustCompile(`(?m)^\s*$`)
		parts := re.Split(doc, 2)
		description := strings.TrimSpace(parts[0])
		contents := ""
		if len(parts) > 1 {
			contents = strings.TrimSpace(parts[1])
		}
		// Scrub filename
		p = p[:len(p)-len(filepath.Ext(p))]
		name := toolnameRE.ReplaceAllString(p, "")
		gpt = gpt.WithKnowledge(name, description, contents)
	}
	return gpt, nil
}

// An individual piece of knowledge
type Knowledge struct {
	Name        string
	Description string
	Contents    string
}

// Inject knowledge
func (m Gpt) WithKnowledge(
	// Unique name. Not semantically meaningful
	name,
	// Description for the knowledge. Keep it short, like the cover of a book.
	// The model uses it to decide which book to read
	description,
	// Contents of the knowledge. This is like the contents of the book.
	// It will only be read by the model if it decides to lookup based on the description.
	contents string,
) Gpt {
	m.KnowledgeBase = append(m.KnowledgeBase, Knowledge{
		Name:        name,
		Description: description,
		Contents:    contents,
	})
	return m
}

// Lookup a piece of knowledge by name
func (m Gpt) Knowledge(name string) (*Knowledge, error) {
	for _, knowledge := range m.KnowledgeBase {
		if knowledge.Name == name {
			return &knowledge, nil
		}
	}
	return nil, fmt.Errorf("no such knowledge: %s", name)
}

func (m Gpt) withReply(ctx context.Context, message openai.ChatCompletionMessage) Gpt {
	if len(message.Content) != 0 {
		log := "🤖: " + message.Content
		_, span := Tracer().Start(ctx, log)
		span.End()
		m.Log = append(m.Log, log)
	}
	hist := m.loadHistory()
	hist = append(hist, message)
	m.LastReply = message.Content
	return m.saveHistory(hist)
}

func (m Gpt) WithToolOutput(callId, content string) Gpt {
	// Remove all ANSI escape codes (eg. part of raw interactive shell output), to avoid json marshalling failing
	re := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	content = re.ReplaceAllString(content, "")
	hist := m.loadHistory()
	hist = append(hist, openai.ToolMessage(callId, content))
	return m.saveHistory(hist)
}

func (m Gpt) WithPrompt(ctx context.Context, prompt string) Gpt {
	log := "🧑: " + prompt
	ctx, span := Tracer().Start(ctx, log)
	span.End()
	hist := m.loadHistory()
	hist = append(hist, openai.UserMessage(prompt))
	m.Log = append(m.Log, log)
	return m.saveHistory(hist)
}

func (m Gpt) WithSystemPrompt(ctx context.Context, prompt string) Gpt {
	log := "🧬: " + prompt
	ctx, span := Tracer().Start(ctx, log)
	span.End()
	hist := m.loadHistory()
	hist = append(hist, openai.SystemMessage(prompt))
	m.Log = append(m.Log, log)
	return m.saveHistory(hist)
}

// Attach a new workdir for the AI's shell
func (gpt Gpt) WithWorkdir(workdir *dagger.Directory) Gpt {
	gpt.Workdir = workdir
	return gpt
}

func (m Gpt) Ask(
	ctx context.Context,
	// The message to send the model
	prompt string,
) (out Gpt, rerr error) {
	m = m.WithPrompt(ctx, prompt)
	for {
		q, err := m.oaiQuery(ctx)
		if err != nil {
			return m, err
		}
		// Add the model reply to the history
		m = m.withReply(ctx, q.Choices[0].Message)
		// Handle tool calls
		calls := q.Choices[0].Message.ToolCalls
		if len(calls) == 0 {
			break
		}
		for _, call := range calls {
			// Extract the command from the function call arguments
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
				return m, err
			}
			switch call.Function.Name {
			case "run":
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
					return m, err
				}
				log := fmt.Sprintf("🤖💻 " + args["command"].(string))
				m.Log = append(m.Log, log)
				ctx, span := Tracer().Start(ctx, log)
				result, err := m.toolRun(ctx, args["command"].(string))
				if err != nil {
					return m, err
				}
				m.Workdir = m.Workdir.WithDirectory(".", result.Workdir)
				resultJson, err := json.Marshal(result)
				if err != nil {
					return m, err
				}
				m = m.WithToolOutput(call.ID, string(resultJson))
				cmd := Command{
					Command: args["command"].(string),
				}
				if result.ExitCode == 0 {
					cmd.Success = true
					cmd.Result = result.Stdout
				} else {
					cmd.Success = false
					cmd.Error = result.Stderr
					span.SetStatus(codes.Error, cmd.Error)
				}
				span.End()
				m.ShellHistory = append(m.ShellHistory, cmd)

			default:
				knowledge, err := m.Knowledge(call.Function.Name)
				if err != nil {
					return m, err
				}
				log := "🤖📖 \"" + knowledge.Description + "\""
				m.Log = append(m.Log, log)
				_, span := Tracer().Start(ctx, log)
				span.End()
				m = m.WithToolOutput(call.ID, knowledge.Contents)
			}
		}
	}
	return m, nil
}

type Command struct {
	Command string
	Success bool
	Result  string
	Error   string
}

func (gpt Gpt) HistoryFile() *dagger.File {
	var snippets []string
	for _, cmd := range gpt.ShellHistory {
		snippet := fmt.Sprintf("<cmd>\n%s\n</cmd>\n<success>%t</success>\n<result>\n%s\n</result>\n<error>\n%s\n</error>",
			cmd.Command, cmd.Success, cmd.Result, cmd.Error)
		snippets = append(snippets, snippet)
	}
	return dag.Directory().WithNewFile("commands", strings.Join(snippets, "\n")).File("commands")
}

type toolRunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Workdir  *dagger.Directory `json:"-"`
}

func (m Gpt) ToolEnv() *dagger.Container {
	return dag.Container().
		From("docker.io/library/alpine:latest@sha256:21dc6063fd678b478f57c0e13f47560d0ea4eeba26dfc947b2a4f81f686b9f45").
		WithFile("/bin/dagger", dag.DaggerCli().Binary()).
		WithWorkdir("/gpt/workdir").
		WithDirectory(".", m.Workdir).
		WithDefaultTerminalCmd([]string{"/bin/sh"}, dagger.ContainerWithDefaultTerminalCmdOpts{
			ExperimentalPrivilegedNesting: true,
		})
}

func (m Gpt) toolRun(ctx context.Context, command string) (*toolRunResult, error) {
	// Execute the command
	cmd := m.ToolEnv().WithExec(
		[]string{"dagger", "shell", "-s"},
		dagger.ContainerWithExecOpts{
			ExperimentalPrivilegedNesting: true,
			Expect:                        dagger.ReturnTypeAny,
			Stdin:                         command,
		},
	)
	stdout, err := cmd.Stdout(ctx)
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.Stderr(ctx)
	if err != nil {
		return nil, err
	}
	exitCode, err := cmd.ExitCode(ctx)
	if err != nil {
		return nil, err
	}
	return &toolRunResult{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
		Workdir:  cmd.Directory("/gpt/workdir"),
	}, nil
}
