package main

import (
	"context"
	"dagger/gpt/internal/dagger"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"go.opentelemetry.io/otel/codes"
)

func New(
	// OpenAI API token
	token *dagger.Secret,
	// OpenAI model
	// +optional
	// +default="gpt-4o"
	model ModelName,
) Gpt {
	return Gpt{
		Token: token,
		Model: model,
	}.WithKnowledge(
		"terminal",
		"basic information about using the terminal",
		`The terminal is your ONLY tool for accomplishing tasks. It runs the dagger shell, which features:

- a bash-compatible syntax,
- backed by a container engine with a declarative API.
- instead of text flowing through unix commands, typed artifacts flow through containerized functions
- artifacts are immutable, content-addressed, and cached

Guidelines:
- Everything is typed and documented. Use .doc anytime you're not sure how to achieve something. See examples below.
- Everything is immutable and contextual. Most functions have no side effects.
`).
		WithKnowledge(
			"shell-exploration",
			"how to explore the shell, find available commands",
			`
The builtin '.help' shows available builtins

The builtin '.doc' prints available commands (functions) in the current scope.

.doc can be inserted in a pipeline, to see what's available at that stage of the pipeline.

'.doc FOO' will print detailed information about the function FOO.
Use this to list available arguments and their type.

Objects are typed. .doc includes the name of the current object in scope.

Examples (one command per line):

.help
.doc
.doc container
container | .doc
container | from alpine
`).
		WithKnowledge(
			"shell-git",
			"how to interact with git repositories in the dagger shell",
			`
The core function 'git' can interact with git remotes.
Use the usual shell exploration techniques to discover the exact API.

Examples:

git https://github.com/goreleaser/goreleaser | head | tree
git https://github.com/dagger/dagger | tags
git https://github.com/cubzh/cubzh | branch main | commit
git https://github.com/kpenfound/dagger-modules | head | tree | glob '**'
`).
		WithKnowledge(
			"shell-sub-pipelines",
			"how to compose several pipelines in the dagger shell, use the output of one pipeline as argument to another",
			`
For sub-pipelines, dagger shell uses the usual bash syntax for sub-commands: $()

Of course this is Dagger, so instead of raw text streams flowing through the pipelines, it's typed objects.

Examples:

container | from index.docker.io/golang | with-directory /src $(.git https://github.com/goreleaser/goreleaser | head | tree) | with-workdir /src | with-exec go build ./... | directory ./bin
directory | with-new-file goreleaser-readme.md $(git https://github.com/goreleaser/goreleaser | tags | tree | file README.md | contents)
`).
		WithKnowledge("shell-errors", "getting useful errors in the shell", `
Sometimes the dagger shell produces errors that are not super useful. Here are some tips for managing that:

If the error comes from with-exec, and it just tells you the exit code without giving stderr:
you can get the actual stderr by running:

<YOUR PIPELINE> | with-exec YOUR COMMAND --expect=ANY | stderr

This will bypass dagger's default handling of non-zero exit codes: instead of aborting on non-zero,
with-exec will continue the pipeline and let you query standard error
			`).
		WithKnowledge(
			"using-modules",
			"how to use modules to extend dagger shell with more functions and types",
			`
To extend your shell with new capabilities, use modules.

A module is just a source directory (local or remote via git) that dagger knows how to load
functions and types from.

To use a module, simply use its address as a command. For example:

<example>
# Assuming a module is at the local path ./foo
./foo | .doc
./foo | my-func MYARG --MYFLAG=VALUE
</example>

<example>
github.com/dagger/dagger/modules/go $(git https://github.com/dagger/dagger) | build ./cmd/dagger
</example>

<example>
github.com/shykes/hello | .doc
github.com/shykes/hello | hello --name=alice --greeting=hi
</example>

Modules can be composed in the same pipeline:

<example>
github.com/dagger/dagger/modules/wolfi | container | with-file $(github.com/dagger/dagger/cmd/dagger | binary) | with-exec dagger version | stdout
</example>

A module is loaded by a constructor function, which returns an object type.
That object can itself have more functions, etc.

To inspect the module's constructor, use .doc

<example>
.doc github.com/dagger/dagger/modules/go
</example>

`).
		WithKnowledge(
			"shell-examples",
			"Various examples of using the dagger shell",
			`

.help
.doc
.doc container
directory | .doc
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
`)
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

type Gpt struct {
	Model         ModelName
	Token         *dagger.Secret // +private
	HistoryJSON   string         // +private
	Log           []string
	ShellHistory  []Command
	LastReply     string
	KnowledgeBase []Knowledge
}

type Knowledge struct {
	Name        string
	Description string
	Contents    string
}

func (m Gpt) WithKnowledge(name, description, contents string) Gpt {
	m.KnowledgeBase = append(m.KnowledgeBase, Knowledge{
		Name:        name,
		Description: description,
		Contents:    contents,
	})
	return m
}

func (m Gpt) Knowledge(name string) (*Knowledge, error) {
	for _, knowledge := range m.KnowledgeBase {
		if knowledge.Name == name {
			return &knowledge, nil
		}
	}
	return nil, fmt.Errorf("no such knowledge: %s", name)
}

func (m Gpt) History() string {
	return m.HistoryJSON
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

func (m Gpt) withReply(message openai.ChatCompletionMessage) Gpt {
	hist := m.loadHistory()
	hist = append(hist, message)
	m.Log = append(m.Log, fmt.Sprintf("ASSISTANT: %s", message.Content))
	m.LastReply = message.Content
	return m.saveHistory(hist)
}

func (m Gpt) WithToolOutput(callId, content string) Gpt {
	hist := m.loadHistory()
	hist = append(hist, openai.ToolMessage(callId, content))
	m.Log = append(m.Log, fmt.Sprintf("TOOL CALL: #%s: %s", callId, content))
	return m.saveHistory(hist)
}

func (m Gpt) WithPrompt(prompt string) Gpt {
	hist := m.loadHistory()
	hist = append(hist, openai.UserMessage(prompt))
	m.Log = append(m.Log, "USER: "+prompt)
	return m.saveHistory(hist)
}

func (m Gpt) Ask(
	ctx context.Context,
	// The message to send the model
	prompt string,
) (out Gpt, rerr error) {
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
			m.Log = append(m.Log, fmt.Sprintf("TOOL CALL: %s(%s) #%s", call.Function.Name, args, call.ID))
			switch call.Function.Name {
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
			default:
				knowledge, err := m.Knowledge(call.Function.Name)
				if err != nil {
					return m, err
				}
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
