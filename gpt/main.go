package main

import (
	"context"
	"dagger/gpt/internal/dagger"
	"encoding/json"
	"regexp"

	"github.com/openai/openai-go"
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
		Token: token,
		Model: model,
	}
	sandbox, err := NewSandbox().WithUsername("🤖").ImportManuals(ctx, knowledgeDir)
	if err != nil {
		return gpt, err
	}
	gpt.Sandbox = sandbox
	prompt, err := systemPrompt.Contents(ctx)
	if err != nil {
		return gpt, err
	}
	return gpt.WithSystemPrompt(ctx, prompt), nil
}

type Gpt struct {
	Model       ModelName      // +private
	Token       *dagger.Secret // +private
	HistoryJSON string         // +private
	Sandbox     Sandbox
}

func (m Gpt) WithSandbox(sandbox Sandbox) Gpt {
	m.Sandbox = sandbox
	return m
}

func (m Gpt) WithSecret(name string, value *dagger.Secret) Gpt {
	m.Sandbox = m.Sandbox.WithSecret(name, value)
	return m
}

func (m Gpt) WithDirectory(dir *dagger.Directory) Gpt {
	m.Sandbox = m.Sandbox.WithHome(m.Sandbox.Home.WithDirectory(".", dir))
	return m
}

// Configure a remote module as context for the sandbox
func (m Gpt) WithRemoteModule(address string) Gpt {
	m.Sandbox = m.Sandbox.WithRemoteModule(address)
	return m
}

// Configure a local module as context for the sandbox
func (m Gpt) WithLocalModule(module *dagger.Directory) Gpt {
	m.Sandbox = m.Sandbox.WithLocalModule(module)
	return m
}

func (m Gpt) History() []string {
	return m.Sandbox.History
}

func (m Gpt) withReply(ctx context.Context, message openai.ChatCompletionMessage) Gpt {
	if len(message.Content) != 0 {
		m.Sandbox = m.Sandbox.WithNote(ctx, message.Content, "")
	}
	hist := m.loadHistory(ctx)
	hist = append(hist, message)
	return m.saveHistory(hist)
}

func (m Gpt) WithToolOutput(ctx context.Context, callId, content string) Gpt {
	// Remove all ANSI escape codes (eg. part of raw interactive shell output), to avoid json marshalling failing
	re := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	content = re.ReplaceAllString(content, "")
	hist := m.loadHistory(ctx)
	hist = append(hist, openai.ToolMessage(callId, content))
	return m.saveHistory(hist)
}

func (m Gpt) WithPrompt(ctx context.Context, prompt string) Gpt {
	m.Sandbox = m.Sandbox.WithNote(ctx, prompt, "🧑")
	hist := m.loadHistory(ctx)
	hist = append(hist, openai.UserMessage(prompt))
	return m.saveHistory(hist)
}

func (m Gpt) WithSystemPrompt(ctx context.Context, prompt string) Gpt {
	m.Sandbox = m.Sandbox.WithNote(ctx, prompt, "🧬")
	hist := m.loadHistory(ctx)
	hist = append(hist, openai.SystemMessage(prompt))
	return m.saveHistory(hist)
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
