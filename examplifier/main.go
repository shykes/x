package main

import (
	"dagger/examplifier/internal/dagger"
	"fmt"
)

func New(
	token *dagger.Secret,
	// +optional
	// +defaultPath="system-prompt.txt"
	systemPrompt *dagger.File,
	// +optional
	// +defaultPath="./knowledge"
	knowledgeDir *dagger.Directory,
) Examplifier {
	return Examplifier{
		Gpt: dag.
			Gpt(token, dagger.GptOpts{SystemPrompt: systemPrompt}).
			WithKnowledgeDir(knowledgeDir),
	}
}

type Examplifier struct {
	Gpt *dagger.Gpt // +private
}

func (m *Examplifier) Examplify(
	mod *dagger.Directory,
	// Number of examples
	// +optional
	// +default=5
	n int,
) *dagger.Directory {
	return m.Gpt.
		WithWorkdir(mod).
		Ask(
			fmt.Sprintf(`
There is a local module at the path ".". You can load it with '.doc .', '. | .doc', etc.

Follow these steps carefully. Be attentive to detail:

1. Inspect this module thoroughly, including its functions and arguments.
2. Think of %d example pipelines that would help someone understand how to use it. Maximize coverage of available functions and arguments. (the EXAMPLES)
3. Run each pipeline to make sure it works
4. Write a description of the module and its purpose, along with your working example pipelines, to a new file
5. Export the file to ./examplify/README.md . Be careful to export the file, and not the enclosing directory
`, n)).Workdir()
}
