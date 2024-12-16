package main

import (
	"context"
	"dagger/gpt/internal/dagger"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const systemPrompt = `
<knowledge name="terminal">
The terminal is your ONLY tool for accomplishing tasks. It runs the dagger shell, which features:

- a bash-compatible syntax,
- backed by a container engine with a declarative API.
- instead of text flowing through unix commands, typed artifacts flow through containerized functions
- artifacts are immutable, content-addressed, and cached

Guidelines:
- Everything is typed and documented. Use .doc aggressively
- Everything is immutable and contextual. Most functions have no side effects.

Example commands (one per line):

.help
.doc
.doc container
container | .doc
container | from alpine | with-exec apk add openssh git | .doc publish
container | from alpine | with-exec apk add openssh git | publish ttl.sh/my-image
directory | with-new-file goreleaser-readme.md $(git https://github.com/goreleaser/goreleaser | head | tree | file README.md)
directory | with-new-file goreleaser-readme.md $(git https://github.com/goreleaser/goreleaser | tags | tree | file README.md | contents)
http https://news.ycombinator.com | contents
directory | with-new-file hello.txt "hello world" | file hello.txt | .doc
directory | with-new-file hello.txt "hello world" | file hello.txt | contents
container | from index.docker.io/golang | with-directory /src $(.git https://github.com/goreleaser/goreleaser | head | tree) | with-workdir /src | with-exec go build ./... | directory ./bin
.doc github.com/dagger/dagger/modules/go
github.com/dagger/dagger/modules/go $(git https://github.com/goreleaser/goreleaser | head | tree) | .doc
.doc github.com/dagger/dagger/cmd/dagger
github.com/dagger/dagger/cmd/dagger | binary --platform=darwin/arm64
.doc github.com/cubzh/cubzh

# Load module directly from address:
github.com/cubzh/cubzh | .doc

# Load module directly from address, inspect its contents, then build a pipeline
github.com/shykes/x/termcast | .doc
github.com/shykes/x/termcast | exec 'ls -l' | exec 'curl https://lemonde.fr' | gif
git https://github.com/kpenfound/dagger-modules | head | tree | glob '**'

github.com/shykes/x | .deps
github.com/shykes/x | wolfi | .doc
github.com/shykes/x | python | .doc
github.com/shykes/x | svix | .doc
github.com/shykes/x | kafka | .doc

# Bash syntax means the usual quoting rules apply. Be careful to use single quotes when writing shell scripts to a file, or the env variables may be expanded by the dagger shell instead
foo=bar; directory | with-new-file joke.txt "two programmers meet in a $foo" | with-new-file script.sh 'echo "my user is $USER"'

# with-exec has args within args. use -- judiciously:
container | from alpine | with-exec ls -- -l

# most dockerfile commands have an equivalent, but not always named the same. explore!
container | .doc
container | with-default-args bash -- -l

# ephemeral services are great for containerizing test environments
container | from alpine | with-service-binding www $(container | from nginx | with-exposed-port 80) | with-exec curl www | stdout

</knowledge>
`

func New(
	// OpenAI API token
	token *dagger.Secret,
) Gpt {
	return Gpt{
		Token: token,
	}
}

type Gpt struct {
	Token        *dagger.Secret // +private
	HistoryJSON  string         // +private
	DebugLog     []string
	ShellHistory []Command
	//	history     []openai.ChatCompletionMessageParamUnion
	LastReply string
}

func (m Gpt) History() string {
	return m.HistoryJSON
}

type Message struct {
	Role    string      `json:"role", required`
	Content interface{} `json:"content", required`
	//Content    []map[string]interface{} `json:"content", required`
	ToolCallID string `json:"tool_call_id"`
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
	fmt.Printf("loadHistory(%s)...\n", m.HistoryJSON)
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
			fmt.Printf("loading history entry: USER: %v\n", msg)
			//var text string
			//if len(msg.Content) > 0 {
			//	if v, ok := msg.Content[0]["text"]; ok {
			//		text = v.(string)
			//	}
			//}
			text, err := msg.Text()
			if err != nil {
				panic(err)
			}
			history = append(history, openai.UserMessage(text))
			fmt.Printf("USER: %v\n", msg)
		case "tool":
			// history = append(history, openai.ToolMessage(msg.ToolCallID, msg.Content[0]["text"].(string)))
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
		default:
			fmt.Printf("OTHER: %v\n", msg)
		}
	}
	fmt.Printf("loadHistory(%s) -> %v\n", m.HistoryJSON, history)
	return history
}

func (m Gpt) saveHistory(history []openai.ChatCompletionMessageParamUnion) Gpt {
	data, err := json.Marshal(history)
	if err != nil {
		panic(err)
	}
	m.HistoryJSON = string(data)
	fmt.Printf("saveHistory(%v) -> %v\n", history, m.HistoryJSON)
	return m
}

func (m Gpt) withReply(message openai.ChatCompletionMessage) Gpt {
	hist := m.loadHistory()
	hist = append(hist, message)
	m.DebugLog = append(m.DebugLog, fmt.Sprintf("ASSISTANT: %v", message))
	m.LastReply = message.Content
	return m.saveHistory(hist)
}

func (m Gpt) WithToolOutput(callId, content string) Gpt {
	hist := m.loadHistory()
	hist = append(hist, openai.ToolMessage(callId, content))
	m.DebugLog = append(m.DebugLog, "TOOL "+callId+": "+content)
	return m.saveHistory(hist)
}

func (m Gpt) WithPrompt(prompt string) Gpt {
	fmt.Printf("WithPrompt(%v)\n", prompt)
	hist := m.loadHistory()
	hist = append(hist, openai.UserMessage(prompt))
	m.DebugLog = append(m.DebugLog, "USER: "+prompt)
	return m.saveHistory(hist)
}

func (m Gpt) Ask(
	ctx context.Context,
	// A prompt telling Daggy what to do
	prompt string,
	// +optional
	// +default=true
	knowledge bool,
) (out Gpt, rerr error) {
	if knowledge {
		m = m.WithPrompt(systemPrompt)
	}
	m = m.WithPrompt(prompt)
	for {
		q, err := m.oaiQuery(ctx)
		if err != nil {
			return m, err
		}
		// Add the model reply to the history
		m = m.withReply(q.Choices[0].Message)
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
			fmt.Printf("--> %s(%s)\n", call.Function.Name, args)
			switch call.Function.Name {
			case "give-up":
				return m, nil
			case "success":
				return m, nil
			case "run":
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
					return m, err
				}
				result, err := m.toolRun(ctx, args["command"].(string))
				if err != nil {
					return m, err
				}
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
				}
				m.ShellHistory = append(m.ShellHistory, cmd)
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
	params := openai.ChatCompletionNewParams{
		Seed:     openai.Int(0),
		Model:    openai.F(openai.ChatModelGPT4o),
		Messages: openai.F(m.loadHistory()),
		Tools: openai.F([]openai.ChatCompletionToolParam{
			{
				Type: openai.F(openai.ChatCompletionToolTypeFunction),
				Function: openai.F(openai.FunctionDefinitionParam{
					Name:        openai.String("success"),
					Description: openai.String("Declare that you have succeeded in accomplishing the task"),
					Parameters: openai.F(openai.FunctionParameters{
						"type": "object",
						"properties": map[string]interface{}{
							"comment": map[string]string{
								"type": "string",
							},
						},
					}),
				}),
			},
			{
				Type: openai.F(openai.ChatCompletionToolTypeFunction),
				Function: openai.F(openai.FunctionDefinitionParam{
					Name:        openai.String("give-up"),
					Description: openai.String("Declare that you have giving up on accomplishing the task"),
					Parameters: openai.F(openai.FunctionParameters{
						"type": "object",
						"properties": map[string]interface{}{
							"comment": map[string]string{
								"type": "string",
							},
						},
					}),
				}),
			},
			{
				Type: openai.F(openai.ChatCompletionToolTypeFunction),
				Function: openai.F(openai.FunctionDefinitionParam{
					Name:        openai.String("run"),
					Description: openai.String("Execute a command in the terminal"),
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
			},
		}),
	}
	return client.Chat.Completions.New(ctx, params)
}

type Command struct {
	Command string
	Success bool
	Result  string
	Error   string
}

type toolRunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func (m Gpt) toolRun(ctx context.Context, command string) (*toolRunResult, error) {
	// Execute the command
	cmd := dag.Container().
		From("alpine").
		WithFile("/bin/dagger", dag.DaggerCli().Binary()).
		WithExec(
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
	}, nil
}
