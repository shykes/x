package main

import (
	"context"
	"dagger/llm/internal/dagger"
	"encoding/json"
	"regexp"

	"github.com/openai/openai-go"
)

func New(
	ctx context.Context,
	// OpenAI API token
	openaiToken *dagger.Secret,
	// OpenAI model
	// +optional
	// +default="gpt-4o"
	model ModelName,
	// A builtin knowledge library, made of text files.
	// First paragraph is the description. The rest is the contents.
	// +optional
	// +defaultPath=./knowledge
	knowledgeDir *dagger.Directory,
	// A system prompt to inject into the LLM context
	// +optional
	// +defaultPath="./system-prompt.txt"
	systemPrompt *dagger.File,
) (Llm, error) {
	llm := Llm{
		OpenAIToken: openaiToken,
		Model:       model,
	}
	sandbox, err := NewSandbox().WithUsername("🤖").ImportManuals(ctx, knowledgeDir)
	if err != nil {
		return llm, err
	}
	llm.Sandbox = sandbox
	prompt, err := systemPrompt.Contents(ctx)
	if err != nil {
		return llm, err
	}
	return llm.WithSystemPrompt(ctx, prompt), nil
}

type Llm struct {
	Model       ModelName      // +private
	OpenAIToken *dagger.Secret // +private
	HistoryJSON string         // +private
	Sandbox     Sandbox
}

func (m Llm) WithSandbox(sandbox Sandbox) Llm {
	m.Sandbox = sandbox
	return m
}

func (m Llm) WithSecret(name string, value *dagger.Secret) Llm {
	m.Sandbox = m.Sandbox.WithSecret(name, value)
	return m
}

func (m Llm) WithDirectory(dir *dagger.Directory) Llm {
	m.Sandbox = m.Sandbox.WithHome(m.Sandbox.Home.WithDirectory(".", dir))
	return m
}

// Configure a remote module as context for the sandbox
func (m Llm) WithRemoteModule(address string) Llm {
	m.Sandbox = m.Sandbox.WithRemoteModule(address)
	return m
}

// Configure a local module as context for the sandbox
func (m Llm) WithLocalModule(module *dagger.Directory) Llm {
	m.Sandbox = m.Sandbox.WithLocalModule(module)
	return m
}

func (m Llm) History() []string {
	return m.Sandbox.History
}

func (m Llm) withReply(ctx context.Context, message openai.ChatCompletionMessage) Llm {
	if len(message.Content) != 0 {
		m.Sandbox = m.Sandbox.WithNote(ctx, message.Content, "")
	}
	hist := m.loadHistory(ctx)
	hist = append(hist, message)
	return m.saveHistory(hist)
}

func (m Llm) WithToolOutput(ctx context.Context, callId, content string) Llm {
	// Remove all ANSI escape codes (eg. part of raw interactive shell output), to avoid json marshalling failing
	re := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	content = re.ReplaceAllString(content, "")
	hist := m.loadHistory(ctx)
	hist = append(hist, openai.ToolMessage(callId, content))
	return m.saveHistory(hist)
}

func (m Llm) WithPrompt(ctx context.Context, prompt string) Llm {
	m.Sandbox = m.Sandbox.WithNote(ctx, prompt, "🧑")
	hist := m.loadHistory(ctx)
	hist = append(hist, openai.UserMessage(prompt))
	return m.saveHistory(hist)
}

func (m Llm) WithSystemPrompt(ctx context.Context, prompt string) Llm {
	m.Sandbox = m.Sandbox.WithNote(ctx, prompt, "🧬")
	hist := m.loadHistory(ctx)
	hist = append(hist, openai.SystemMessage(prompt))
	return m.saveHistory(hist)
}

func (m Llm) Ask(
	ctx context.Context,
	// The message to send the model
	prompt string,
) (out Llm, rerr error) {
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
			case "dagger":
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
					return m, err
				}
				m.Sandbox, err = m.Sandbox.Run(ctx, args["command"].(string))
				if err != nil {
					return m, err
				}
				run, err := m.Sandbox.LastRun()
				if err != nil {
					return m, err
				}
				result, err := run.ResultJSON()
				if err != nil {
					return m, err
				}
				m = m.WithToolOutput(ctx, call.ID, result)
			default:
				manual, err := m.Sandbox.Manual(ctx, call.Function.Name)
				if err != nil {
					return m, err
				}
				m = m.WithToolOutput(ctx, call.ID, manual.Contents)
			}
		}
	}
	return m, nil
}
