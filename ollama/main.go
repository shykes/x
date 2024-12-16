package main

import (
	"context"
	"dagger/ollama/internal/dagger"
)

type Ollama struct{}

func (o *Ollama) Llama2(ctx context.Context, prompt string) (string, error) {
	return o.Client(ctx, "llama2").
		WithExec([]string{"ollama", "pull", "llama2"}).
		WithExec([]string{"ollama", "run", "llama2", prompt}).
		Stdout(ctx)
}

func (o *Ollama) Client(ctx context.Context, model string) *dagger.Container {
	return o.Base(ctx).
		WithServiceBinding("server", o.Server(ctx, model).AsService()).
		WithEnvVariable("OLLAMA_HOST", "server")
}

func (o *Ollama) Base(ctx context.Context) *dagger.Container {
	return dag.Container().
		From("index.docker.io/ollama/ollama").
		WithoutEntrypoint()
}

func (o *Ollama) Server(ctx context.Context, model string) *dagger.Container {
	return o.Base(ctx).
		WithExec([]string{"sh", "-c", "ollama serve & ollama pull " + model}).
		WithEnvVariable("OLLAMA_HOST", "0.0.0.0").
		WithDefaultArgs([]string{"ollama", "serve"}).
		WithExposedPort(11434)
}
