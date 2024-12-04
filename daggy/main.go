// Daggy is an AI agent that knows how to call Dagger functions.
// It is powered by OpenAI and GPTScript
package main

import (
	"context"
	"daggy/internal/dagger"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const systemPrompt = `
You will be given a task, and access to a terminal.

Don't respond to the request. Simply accomplish the task using the provided tools.
If you can't accomplish the task, say so in a terse and concise way.

The terminal is running a new kind of shell, called a "container shell". It has the following features:

- A container engine with all common operations I might find in a dockerfile or docker-compose file.
- Container operations can be chained in pipelines, using the familiar bash syntax. Instead of text flowing between unix commands, it's artifacts flowing between containerized functions.
- The shell syntax is just a frontend to a declarative API which is fully typed.
- Artifacts are typed objets. They define functions which themselves are typed.
- The shell starts in an initial scope. Available builtin commands can be listed with '.help'. Documentation of the current object can be printed with '.doc'.
- A special builtin .core loads a special object with lots of available functions.

Example commands:

.help
.container | .doc
.container | .doc from
.container | from alpine | with-exec apk,add,git,openssh | with-exec git,version | stdout
.container | from index.docker.io/golang | with-directory /src $(.git https://github.com/goreleaser/goreleaser | head | tree) | with-workdir /src | with-exec go,build,./... | directory ./bin
.git | head | tree | file README.md
.directory | with-new-file hello.txt "Well hello there"
.load github.com/dagger/dagger | .doc
.load github.com/goreleaser/goreleaser | .doc

Take your time to explore the terminal. You can run as many commands as you want. Make ample use of the interactive documentation.
Do not give up easily.

TASK:
`

func New(
	// OpenAI API token
	token *dagger.Secret,
) Daggy {
	return Daggy{
		Token: token,
	}
}

// Daggy is an AI agent that knows how to drive Dagger
// It is powered by OpenAI
type Daggy struct {
	Token       *dagger.Secret // +private
	HistoryJSON string
	//	history     []openai.ChatCompletionMessageParamUnion
	LastReply string
}

type Message map[string]interface{}

func (msg Message) implementsChatCompletionMessageParamUnion() {}

func (m Daggy) loadHistory() []openai.ChatCompletionMessageParamUnion {
	if m.HistoryJSON == "" {
		return nil
	}
	var raw []interface{}
	err := json.Unmarshal([]byte(m.HistoryJSON), &raw)
	if err != nil {
		panic(err)
	}
	var history []openai.ChatCompletionMessageParamUnion
	for _, msg := range raw {
		msgJson, err := json.Marshal(msg)
		if err != nil {
			panic(err)
		}
		fmt.Printf("checking re-marshalled value: '%s'\n", msgJson)
		var userMessage openai.ChatCompletionUserMessageParam
		if err := json.Unmarshal(msgJson, &userMessage); err == nil {
			history = append(history, userMessage)
			continue
		} else {
			fmt.Printf("no match: %s\n", err)
		}
		panic(fmt.Sprintf("unsupported message: %v", msg))
	}
	//for _, msg := range raw {
	//	switch v := msg.(type) {
	//	case openai.ChatCompletionUserMessageParam:
	//		history = append(history, v)
	//	default:
	//		panic(fmt.Sprintf("unsupported message type: %v", msg))
	//	}
	//}
	return history
}

func (m Daggy) saveHistory(history []openai.ChatCompletionMessageParamUnion) Daggy {
	data, err := json.Marshal(history)
	if err != nil {
		panic(err)
	}
	m.HistoryJSON = string(data)
	return m
}

func (m Daggy) withAgentMessage(message openai.ChatCompletionMessage) Daggy {
	hist := m.loadHistory()
	hist = append(hist, message)
	return m.saveHistory(hist)
}

func (m Daggy) WithToolMessage(callId, content string) Daggy {
	hist := m.loadHistory()
	hist = append(hist, openai.ToolMessage(callId, content))
	return m.saveHistory(hist)
}

func (m Daggy) WithUserMessage(message string) Daggy {
	hist := m.loadHistory()
	hist = append(hist, openai.UserMessage(message))
	return m.saveHistory(hist)
}

// func (m Daggy) Do(
// 	ctx context.Context,
// 	// A prompt telling Daggy what to do
// 	prompt string,
// ) (Daggy, error) {
// 	return m.Prompt(ctx, systemPrompt+"\n"+prompt)
// }

func (m Daggy) Fake(
	ctx context.Context,
	// A prompt telling Daggy what to do
	prompt string,
) Daggy {
	return m.WithUserMessage(prompt)
}

func (m Daggy) Prompt(
	ctx context.Context,
	// A prompt telling Daggy what to do
	prompt string,
) (out Daggy, rerr error) {
	m = m.WithUserMessage(prompt)
	for {
		q, err := m.oaiQuery(ctx)
		if err != nil {
			return m, err
		}
		// Add the model reply to the history
		m = m.withAgentMessage(q.Choices[0].Message)
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
			fmt.Printf("--> [CALL] [%s] %w\n", call.Function.Name, args)
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
				m = m.WithToolMessage(call.ID, string(resultJson))
			}
		}
	}
	return m, nil
}

func (m Daggy) oaiQuery(ctx context.Context) (*openai.ChatCompletion, error) {
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

type toolRunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func (m Daggy) toolRun(ctx context.Context, command string) (*toolRunResult, error) {
	// Execute the command
	cmd := dag.Container().
		From("alpine").
		WithFile("/bin/dagger", dag.DaggerCli().Binary()).
		WithExec(
			[]string{"dagger", "shell", "-s", "--no-load", "-c", command},
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
