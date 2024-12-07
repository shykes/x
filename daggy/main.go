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
<knowledge name="terminal">
The terminal is your primary tool for accomplishing tasks. It runs the dagger shell, which features:

- a bash-compatible syntax,
- backed by a container engine with a declarative API.
- instead of text flowing through unix commands, typed artifacts flow through containerized functions
- artifacts are immutable, content-addressed, and cached

Example commands:
- Show builtins: .help
- Show available functions: .doc
- Show arguments and return type of a function: .doc FUNC
- Initialize a container, then show available functions in the returned Container: container | .doc
- A simple container build: container | from alpine | with-exec apk,add,openssh,git | publish ttl.sh/my-image
- Sub-pipelines: directory | with-file goreleaser-readme.md $(git https://github.com/goreleaser/goreleaser | head | tree | file README.md)
- More sub-pipelines: container | from index.docker.io/golang | with-directory /src $(.git https://github.com/goreleaser/goreleaser | head | tree) | with-workdir /src | with-exec go,build,./... | directory ./bin

Some directories can be executed by Dagger as functions. They are called modules. Examples:
- .doc github.com/dagger/dagger/cmd/dagger
- github.com/dagger/dagger/cmd/dagger | binary --platform=darwin/arm64
- .doc github.com/cubzh/cubzh
- github.com/cubzh/cubzh | .doc

The shell can "navigate" to a module. All subsequent commands start from that module's context.

- .use github.com/dagger/dagger; .doc; .use github.com/cubzh/cubzh; .doc
</knowledge>
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
						[]string{"dagger", "shell", "-s", "-c", command},
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
