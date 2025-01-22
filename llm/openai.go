package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"go.opentelemetry.io/otel/codes"
)

// An OpenAI model name
type ModelName = string

const ()

const (
	shellToolPrefix  = "run-"
	manualToolPrefix = "man-"
)

type OpenAI struct{}

func (oai OpenAI) Models() []string {
	return []string{
		"o1-preview",                         // ModelNameO1Preview
		"o1-preview-2024-09-12",              // ModelNameO1Preview2024_09_12
		"o1-mini",                            // ModelNameO1Mini
		"o1-mini-2024-09-12",                 // ModelNameO1Mini2024_09_12
		"gpt-4o",                             // ModelNameGPT4o
		"gpt-4o-2024-11-20",                  // ModelNameGPT4o2024_11_20
		"gpt-4o-2024-08-06",                  // ModelNameGPT4o2024_08_06
		"gpt-4o-2024-05-13",                  // ModelNameGPT4o2024_05_13
		"gpt-4o-realtime-preview",            // ModelNameGPT4oRealtimePreview
		"gpt-4o-realtime-preview-2024-10-01", // ModelNameGPT4oRealtimePreview2024_10_01
		"gpt-4o-audio-preview",               // ModelNameGPT4oAudioPreview
		"gpt-4o-audio-preview-2024-10-01",    // ModelNameGPT4oAudioPreview2024_10_01
		"chatgpt-4o-latest",                  // ModelNameChatgpt4oLatest
		"gpt-4o-mini",                        // ModelNameGPT4oMini
		"gpt-4o-mini-2024-07-18",             // ModelNameGPT4oMini2024_07_18
		"gpt-4-turbo",                        // ModelNameGPT4Turbo
		"gpt-4-turbo-2024-04-09",             // ModelNameGPT4Turbo2024_04_09
		"gpt-4-0125-preview",                 // ModelNameGPT4_0125Preview
		"gpt-4-turbo-preview",                // ModelNameGPT4TurboPreview
		"gpt-4-1106-preview",                 // ModelNameGPT4_1106Preview
		"gpt-4-vision-preview",               // ModelNameGPT4VisionPreview
		"gpt-4",                              // ModelNameGPT4
		"gpt-4-0314",                         // ModelNameGPT4_0314
		"gpt-4-0613",                         // ModelNameGPT4_0613
		"gpt-4-32k",                          // ModelNameGPT4_32k
		"gpt-4-32k-0314",                     // ModelNameGPT4_32k0314
		"gpt-4-32k-0613",                     // ModelNameGPT4_32k0613
		"gpt-3.5-turbo",                      // ModelNameGPT3_5Turbo
		"gpt-3.5-turbo-16k",                  // ModelNameGPT3_5Turbo16k
		"gpt-3.5-turbo-0301",                 // ModelNameGPT3_5Turbo0301
		"gpt-3.5-turbo-0613",                 // ModelNameGPT3_5Turbo0613
		"gpt-3.5-turbo-1106",                 // ModelNameGPT3_5Turbo1106
		"gpt-3.5-turbo-0125",                 // ModelNameGPT3_5Turbo0125
		"gpt-3.5-turbo-16k-0613",             // ModelNameGPT3_5Turbo16k0613
	}
}

func (oai OpenAI) New() LlmState {
	return OpenAIState{}
}

// State of an OpenAI session, safely serializable
type OpenAIState struct {
	History []openai.ChatCompletionMessageParamUnion
}

func (oai OpenAI) Load(data string) (LlmState, error) {
	st := new(OpenAIState)
	if data == "" {
		return st, nil
	}
	var raw []Message
	err := json.Unmarshal([]byte(data), &raw)
	if err != nil {
		return st, err
	}
	for _, msg := range raw {
		switch msg.Role {
		case "user":
			text, err := msg.Text()
			if err != nil {
				return st, err
			}
			st.History = append(st.History, openai.UserMessage(text))
		case "tool":
			text, err := msg.Text()
			if err != nil {
				return st, err
			}
			st.History = append(st.History, openai.ToolMessage(msg.ToolCallID, text))
		case "assistant":
			text, err := msg.Text()
			if err != nil {
				return st, err
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
			st.History = append(st.History, openai.ChatCompletionMessage{
				Role:      "assistant",
				Content:   text,
				ToolCalls: calls,
			})
		}
	}
	return st, nil
}

func (s OpenAIState) Save() (string, error) {
	data, err := json.Marshal(s.History)
	return string(data), err
}

// Append a user message (prompt) to the message history
func (st OpenAIState) WithPrompt(prompt string) LlmState {
	st.History = append(st.History, openai.UserMessage(prompt))
	return st
}

// Append a system prompt message to the history
func (st OpenAIState) WithSystemPrompt(prompt string) LlmState {
	st.History = append(st.History, openai.SystemMessage(prompt))
	return st
}

// Send a chat completion API query, and update the state with the result
func (s OpenAIState) Query(
	ctx context.Context,
	model,
	endpoint string,
	key string,
	shells []ShellTool,
	manuals []ManualTool,
) (string, LlmState, error) {
	res, err := s.sendQuery(ctx, model, endpoint, key, shells, manuals)
	if err != nil {
		return "", s, err
	}
	reply := res.Choices[0].Message
	// Add the model reply to the history
	s.History = append(s.History, reply)
	// Handle tool calls
	calls := res.Choices[0].Message.ToolCalls
	for _, call := range calls {
		result, err := s.callTool(ctx, call.Function.Name, call.Function.Arguments, shells, manuals)
		if err != nil {
			return "", s, err
		}
		s.History = append(s.History, openai.ToolMessage(call.ID, result))
	}
	return reply.Content, s, nil
}

func (s OpenAIState) sendQuery(
	ctx context.Context,
	model,
	endpoint string,
	key string,
	shells []ShellTool,
	manuals []ManualTool,
) (res *openai.ChatCompletion, rerr error) {
	ctx, span := Tracer().Start(ctx, "[🤖] 💭")
	defer func() {
		if rerr != nil {
			span.SetStatus(codes.Error, rerr.Error())
		}
		span.End()
	}()
	var tools []openai.ChatCompletionToolParam
	for _, shell := range shells {
		tools = append(tools, openai.ChatCompletionToolParam{
			Type: openai.F(openai.ChatCompletionToolTypeFunction),
			Function: openai.F(openai.FunctionDefinitionParam{
				Name:        openai.String(shellToolPrefix + shell.Name()),
				Description: openai.String(shell.Description()),
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
		})
	}
	for _, man := range manuals {
		tools = append(tools, openai.ChatCompletionToolParam{
			Type: openai.F(openai.ChatCompletionToolTypeFunction),
			Function: openai.F(openai.FunctionDefinitionParam{
				Name:        openai.String(manualToolPrefix + man.Name()),
				Description: openai.String(man.Description()),
			}),
		})
	}
	return openai.NewClient(
		option.WithAPIKey(key),
		option.WithHeader("Content-Type", "application/json"),
	).Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Seed:     openai.Int(0),
		Model:    openai.F(openai.ChatModel(model)),
		Messages: openai.F(s.History),
		Tools:    openai.F(tools),
	})
}

func (s OpenAIState) callTool(ctx context.Context, name, args string, shells []ShellTool, manuals []ManualTool) (result string, rerr error) {
	// 1. Are we calling a shell tool?
	if strings.HasPrefix(name, shellToolPrefix) {
		shellName := strings.TrimPrefix(name, shellToolPrefix)
		for _, shell := range shells {
			if shellName == shell.Name() {
				// Execute the command with the specified shell
				var shellArgs struct {
					Command string `json:"command"`
				}
				if err := json.Unmarshal([]byte(args), &shellArgs); err != nil {
					return "", err
				}
				return shell.Run(ctx, shellArgs.Command)
			}
		}
		return "", fmt.Errorf("tool not available: %s", name)
	}
	// 2. Are we calling a manual tool?
	if strings.HasPrefix(name, manualToolPrefix) {
		manualName := strings.TrimPrefix(name, manualToolPrefix)
		for _, manual := range manuals {
			if manualName == manual.Name() {
				return manual.Contents(ctx)
			}
		}
		return "", fmt.Errorf("tool not available: %s", name)
	}
	// 3. Are we calling an unknown tool?
	return "", fmt.Errorf("tool not available: %s", name)
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
