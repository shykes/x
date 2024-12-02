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

// Daggy is an AI agent that knows how to drive Dagger
// It is powered by OpenAI
type Daggy struct{}

// Tell Daggy to do something
func (m *Daggy) Ask(
	ctx context.Context,
	// A prompt telling Daggy what to do
	prompt string,
	// OpenAI API key
	token *dagger.Secret,
	// Custom base container
	// +optional
	base *dagger.Container,
) (string, error) {
	// Initialize the OpenAI client
	key, err := token.Plaintext(ctx)
	if err != nil {
		return "", err
	}
	client := openai.NewClient(
		option.WithAPIKey(key),
		option.WithHeader("Content-Type", "application/json"),
	)
	params := openai.ChatCompletionNewParams{
		Messages: openai.F([]openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(systemPrompt + prompt),
		}),
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
		Seed:  openai.Int(0),
		Model: openai.F(openai.ChatModelGPT4o),
	}
	var lastMessage string
	for {
		// DEBUG
		// jsonBody, err := json.Marshal(params)
		// if err != nil {
		// 	return "", fmt.Errorf("error marshalling params: %w", err)
		// }
		// fmt.Println("Request JSON:", string(jsonBody))
		// Make initial chat completion request

		// Send the next request
		completion, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", err
		}
		lastMessage = completion.Choices[0].Message.Content
		toolCalls := completion.Choices[0].Message.ToolCalls
		// Abort early if there are no tool calls
		if len(toolCalls) == 0 {
			break
		}
		// If there is a was a function call, continue the conversation
		params.Messages.Value = append(params.Messages.Value, completion.Choices[0].Message)
		for _, toolCall := range toolCalls {
			// Extract the command from the function call arguments
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				return "", err
			}
			fmt.Printf("--> [CALL] [%s] %w\n", toolCall.Function.Name, args)
			switch toolCall.Function.Name {
			case "give-up":
				return "", fmt.Errorf("I give up: %s", args["comment"])
			case "success":
				return args["comment"].(string), nil
			case "run":
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
					return "", err
				}
				command := args["command"].(string)
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
					return "", err
				}
				stderr, err := cmd.Stderr(ctx)
				if err != nil {
					return "", err
				}
				var output struct {
					Stdout string
					Stderr string
				}
				output.Stdout = stdout
				output.Stderr = stderr
				outputJson, err := json.Marshal(output)
				if err != nil {
					return "", err
				}
				// output := fmt.Sprintf("<stdout>%s</stdout><stderr>%s</stderr>", stdout, stderr)
				params.Messages.Value = append(params.Messages.Value, openai.ToolMessage(toolCall.ID, string(outputJson)))
				fmt.Printf("<-- [RESULT] %w\n", openai.ToolMessage(toolCall.ID, string(outputJson)))
			}
		}
	}
	return lastMessage, nil
}
