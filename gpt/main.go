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
	"github.com/openai/openai-go/option"
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

type Message struct {
	Role       string      `json:"role", required`
	Content    interface{} `json:"content", required`
	ToolCallID string      `json:"tool_call_id"`
	ToolCalls  []struct {
		// The ID of the tool call.
		ID string `json:"id"`
		// The function that the model called.
		Function struct {
			Arguments string `json:"arguments"`
			// The name of the function to call.
			Name string `json:"name"`
		} `json:"function"`
		// The type of the tool. Currently, only `function` is supported.
		Type openai.ChatCompletionMessageToolCallType `json:"type"`
	} `json:"tool_calls"`
}

func (msg Message) Text() (string, error) {
	contentJson, err := json.Marshal(msg.Content)
	if err != nil {
		return "", err
	}
	switch msg.Role {
	case "user", "tool":
		var content []struct {
			Text string `json:"text", required`
		}
		if err := json.Unmarshal(contentJson, &content); err != nil {
			return "", fmt.Errorf("malformatted user or tool message: %s", err.Error())
		}
		if len(content) == 0 {
			return "", nil
		}
		return content[0].Text, nil
	case "assistant":
		var content string
		if err := json.Unmarshal(contentJson, &content); err != nil {
			return "", fmt.Errorf("malformatted assistant message: %#v", content)
		}
		return content, nil
	}
	return "", fmt.Errorf("unsupported message role: %s", msg.Role)
}

func (m Gpt) loadHistory() []openai.ChatCompletionMessageParamUnion {
	if m.HistoryJSON == "" {
		return nil
	}
	var raw []Message
	err := json.Unmarshal([]byte(m.HistoryJSON), &raw)
	if err != nil {
		panic(err)
	}
	var history []openai.ChatCompletionMessageParamUnion
	for _, msg := range raw {
		switch msg.Role {
		case "user":
			text, err := msg.Text()
			if err != nil {
				panic(err)
			}
			history = append(history, openai.UserMessage(text))
		case "tool":
			text, err := msg.Text()
			if err != nil {
				panic(err)
			}
			history = append(history, openai.ToolMessage(msg.ToolCallID, text))
		case "assistant":
			text, err := msg.Text()
			if err != nil {
				panic(err)
			}
			var calls []openai.ChatCompletionMessageToolCall
			for _, call := range msg.ToolCalls {
				calls = append(calls, openai.ChatCompletionMessageToolCall{
					ID: call.ID,
					Function: openai.ChatCompletionMessageToolCallFunction{
						Arguments: call.Function.Arguments,
						Name:      call.Function.Name,
					},
					Type: call.Type,
				})
			}
			history = append(history, openai.ChatCompletionMessage{
				Role:      "assistant",
				Content:   text,
				ToolCalls: calls,
			})
		}
	}
	return history
}

func (m Gpt) saveHistory(history []openai.ChatCompletionMessageParamUnion) Gpt {
	data, err := json.Marshal(history)
	if err != nil {
		panic(err)
	}
	m.HistoryJSON = string(data)
	return m
}

func (m Gpt) withReply(ctx context.Context, message openai.ChatCompletionMessage) Gpt {
	if len(message.Content) != 0 {
		log := "🤖: " + message.Content
		_, span := Tracer().Start(ctx, log)
		span.SetStatus(codes.Ok, "")
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
					span.SetStatus(codes.Ok, cmd.Result)
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
				span.SetStatus(codes.Ok, knowledge.Contents)
				span.End()
				m = m.WithToolOutput(call.ID, knowledge.Contents)
			}
		}
	}
	return m, nil
}

func (m Gpt) oaiQuery(ctx context.Context) (*openai.ChatCompletion, error) {
	// Initialize the OpenAI client
	key, err := m.Token.Plaintext(ctx)
	if err != nil {
		return nil, err
	}
	client := openai.NewClient(
		option.WithAPIKey(key),
		option.WithHeader("Content-Type", "application/json"),
	)
	runTool := openai.ChatCompletionToolParam{
		Type: openai.F(openai.ChatCompletionToolTypeFunction),
		Function: openai.F(openai.FunctionDefinitionParam{
			Name:        openai.String("run"),
			Description: openai.String("Execute a dagger shell command in the terminal. This is your primary way to accomplish tasks. The syntax is bash-compatible but the backend is different. It requires specialized knowledge to use"),
			Parameters: openai.F(openai.FunctionParameters{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]string{
						"type": "string",
					},
				},
				"required": []string{"command"},
			}),
		}),
	}
	tools := []openai.ChatCompletionToolParam{runTool}
	for _, knowledge := range m.KnowledgeBase {
		tools = append(tools, openai.ChatCompletionToolParam{
			Type: openai.F(openai.ChatCompletionToolTypeFunction),
			Function: openai.F(openai.FunctionDefinitionParam{
				Name:        openai.String(knowledge.Name),
				Description: openai.String(knowledge.Description),
			}),
		})
	}
	params := openai.ChatCompletionNewParams{
		Seed:     openai.Int(0),
		Model:    openai.F(openai.ChatModel(m.Model)),
		Messages: openai.F(m.loadHistory()),
		Tools:    openai.F(tools),
	}
	paramsJSON, err := params.MarshalJSON()
	if err != nil {
		return nil, err
	}
	fmt.Printf("Sending openai request:\n----\n%s\n----\n", paramsJSON)
	return client.Chat.Completions.New(ctx, params)
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
		[]string{"dagger", "shell", "-s", "-c", command},
		dagger.ContainerWithExecOpts{ExperimentalPrivilegedNesting: true, Expect: dagger.ReturnTypeAny},
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

type ModelName = string

const (
	ModelNameO1Preview                      ModelName = "o1-preview"
	ModelNameO1Preview2024_09_12            ModelName = "o1-preview-2024-09-12"
	ModelNameO1Mini                         ModelName = "o1-mini"
	ModelNameO1Mini2024_09_12               ModelName = "o1-mini-2024-09-12"
	ModelNameGPT4o                          ModelName = "gpt-4o"
	ModelNameGPT4o2024_11_20                ModelName = "gpt-4o-2024-11-20"
	ModelNameGPT4o2024_08_06                ModelName = "gpt-4o-2024-08-06"
	ModelNameGPT4o2024_05_13                ModelName = "gpt-4o-2024-05-13"
	ModelNameGPT4oRealtimePreview           ModelName = "gpt-4o-realtime-preview"
	ModelNameGPT4oRealtimePreview2024_10_01 ModelName = "gpt-4o-realtime-preview-2024-10-01"
	ModelNameGPT4oAudioPreview              ModelName = "gpt-4o-audio-preview"
	ModelNameGPT4oAudioPreview2024_10_01    ModelName = "gpt-4o-audio-preview-2024-10-01"
	ModelNameChatgpt4oLatest                ModelName = "chatgpt-4o-latest"
	ModelNameGPT4oMini                      ModelName = "gpt-4o-mini"
	ModelNameGPT4oMini2024_07_18            ModelName = "gpt-4o-mini-2024-07-18"
	ModelNameGPT4Turbo                      ModelName = "gpt-4-turbo"
	ModelNameGPT4Turbo2024_04_09            ModelName = "gpt-4-turbo-2024-04-09"
	ModelNameGPT4_0125Preview               ModelName = "gpt-4-0125-preview"
	ModelNameGPT4TurboPreview               ModelName = "gpt-4-turbo-preview"
	ModelNameGPT4_1106Preview               ModelName = "gpt-4-1106-preview"
	ModelNameGPT4VisionPreview              ModelName = "gpt-4-vision-preview"
	ModelNameGPT4                           ModelName = "gpt-4"
	ModelNameGPT4_0314                      ModelName = "gpt-4-0314"
	ModelNameGPT4_0613                      ModelName = "gpt-4-0613"
	ModelNameGPT4_32k                       ModelName = "gpt-4-32k"
	ModelNameGPT4_32k0314                   ModelName = "gpt-4-32k-0314"
	ModelNameGPT4_32k0613                   ModelName = "gpt-4-32k-0613"
	ModelNameGPT3_5Turbo                    ModelName = "gpt-3.5-turbo"
	ModelNameGPT3_5Turbo16k                 ModelName = "gpt-3.5-turbo-16k"
	ModelNameGPT3_5Turbo0301                ModelName = "gpt-3.5-turbo-0301"
	ModelNameGPT3_5Turbo0613                ModelName = "gpt-3.5-turbo-0613"
	ModelNameGPT3_5Turbo1106                ModelName = "gpt-3.5-turbo-1106"
	ModelNameGPT3_5Turbo0125                ModelName = "gpt-3.5-turbo-0125"
	ModelNameGPT3_5Turbo16k0613             ModelName = "gpt-3.5-turbo-16k-0613"
)
