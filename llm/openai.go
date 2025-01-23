package main

import (
	"context"
	"dagger/llm/internal/dagger"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"go.opentelemetry.io/otel/codes"
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
		"llama3.1",                           // ModelNameLlama3_1
		"llama3.2",                           // ModelNameLlama3_2
		"llama3.3",                           // ModelNameLlama3_3
		"mistral",                            // ModelNameMistral
	}
}

func (oai OpenAI) New() LlmState {
	return OpenAIState{}
}

// State of an OpenAI session, safely serializable
type OpenAIState struct {
	history []openai.ChatCompletionMessageParamUnion
}

// Append a user message (prompt) to the message history
func (st OpenAIState) WithPrompt(prompt string) LlmState {
	st.history = append(st.history, openai.UserMessage(prompt))
	return st
}

// Append a system prompt message to the history
func (st OpenAIState) WithSystemPrompt(prompt string) LlmState {
	st.history = append(st.history, openai.SystemMessage(prompt))
	return st
}

// Send a chat completion API query, process tool calls, and return the reply text
func (s OpenAIState) Query(
	ctx context.Context,
	model string,
	endpoint *dagger.Service,
	token *dagger.Secret,
	tools []Tool,
) (string, LlmState, error) {
	res, err := s.sendQuery(ctx, model, endpoint, token, tools)
	if err != nil {
		return "", s, err
	}
	reply := res.Choices[0].Message
	// Add the model reply to the history
	s.history = append(s.history, reply)
	// Handle tool calls
	calls := res.Choices[0].Message.ToolCalls
	for _, call := range calls {
		result, err := s.callTool(ctx, call.Function.Name, call.Function.Arguments, tools)
		if err != nil {
			return "", s, err
		}
		s.history = append(s.history, openai.ToolMessage(call.ID, result))
	}
	return reply.Content, s, nil
}

func (s OpenAIState) sendQuery(
	ctx context.Context,
	model string,
	endpoint *dagger.Service,
	token *dagger.Secret,
	tools []Tool,
) (res *openai.ChatCompletion, rerr error) {
	ctx, span := Tracer().Start(ctx, "[🤖] 💭")
	defer func() {
		if rerr != nil {
			span.SetStatus(codes.Error, rerr.Error())
		}
		span.End()
	}()
	var toolParams []openai.ChatCompletionToolParam
	for _, tool := range tools {
		toolParams = append(toolParams, openai.ChatCompletionToolParam{
			Type: openai.F(openai.ChatCompletionToolTypeFunction),
			Function: openai.F(openai.FunctionDefinitionParam{
				Name:        openai.String(tool.Name()),
				Description: openai.String(tool.Description()),
				Parameters:  openai.F(openai.FunctionParameters(tool.InputSchema())),
			}),
		})
	}
	opts := []option.RequestOption{option.WithHeader("Content-Type", "application/json")}
	if token != nil {
		key, err := token.Plaintext(ctx)
		if err != nil {
			return nil, err
		}
		opts = append(opts, option.WithAPIKey(key))
	}
	if endpoint != nil {
		endpoint, err := endpoint.Start(ctx)
		if err != nil {
			return nil, err
		}
		url, err := endpoint.Endpoint(ctx, dagger.ServiceEndpointOpts{Scheme: "http"})
		if err != nil {
			return nil, err
		}
		// The following is required for Ollama - OpenAI compatibility
		// Set Host to satisfy routing to local Ollama
		// Note setting Host header does not work with go http.Client
		opts = append(opts, option.WithMiddleware(
			option.Middleware(func(req *http.Request, cb option.MiddlewareNext) (*http.Response, error) {
				req.Host = "localhost:11434"
				return cb(req)
			}),
		))
		url += "/v1/" // This is required for Ollama OpenAI compat
		opts = append(opts, option.WithBaseURL(url))
	}
	return openai.NewClient(opts...).Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Seed:     openai.Int(0),
		Model:    openai.F(openai.ChatModel(model)),
		Messages: openai.F(s.history),
		Tools:    openai.F(toolParams),
	})
}

func (s OpenAIState) callTool(ctx context.Context, name, input string, tools []Tool) (result string, rerr error) {
	for _, tool := range tools {
		if tool.Name() == name {
			return tool.Call(ctx, input)
		}
	}
	return "", fmt.Errorf("tool not available: %s", name)
}

func (oai OpenAI) Load(data string) (LlmState, error) {
	st := new(OpenAIState)
	if data == "" {
		return st, nil
	}
	var raw []openAIMessage
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
			st.history = append(st.history, openai.UserMessage(text))
		case "tool":
			text, err := msg.Text()
			if err != nil {
				return st, err
			}
			st.history = append(st.history, openai.ToolMessage(msg.ToolCallID, text))
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
			st.history = append(st.history, openai.ChatCompletionMessage{
				Role:      "assistant",
				Content:   text,
				ToolCalls: calls,
			})
		}
	}
	return st, nil
}

type openAIMessage struct {
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

func (msg openAIMessage) Text() (string, error) {
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

func (s OpenAIState) Save() (string, error) {
	data, err := json.Marshal(s.history)
	return string(data), err
}
