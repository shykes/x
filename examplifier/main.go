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
	address string,
	// +optional
	context *dagger.Directory,
	// Number of examples
	// +optional
	// +default=5
	n int,
) *dagger.Directory {
	if context == nil {
		context = dag.Directory()
	}
	return m.Gpt.
		WithWorkdir(context).
		Ask(
			fmt.Sprintf(`
1. Load the dagger module at the address "%s".
2. Inspect this module thoroughly, including its functions and arguments.
3. Think of %d example pipelines that would help someone understand how to use it. Maximize coverage of available functions and arguments. (the EXAMPLES)
4. Run each pipeline to make sure it works
5. Write the pipelines to a markdown file, along with a summary of the module's features and purpose
6. Export the file to ./examplify/README.md. Be careful to export the file, and not the enclosing directory
`, address, n)).Workdir()
}
