package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

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
	//paramsJSON, err := params.MarshalJSON()
	//if err != nil {
	//	return nil, err
	//}
	//fmt.Printf("Sending openai request:\n----\n%s\n----\n", paramsJSON)
	return client.Chat.Completions.New(ctx, params)
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
