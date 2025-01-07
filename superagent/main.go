package main

import (
	"dagger/superagent/internal/dagger"
)

type Superagent struct{}

// Generate shell examples of a given dagger module
func (m *Superagent) ShellExamples(address string, token *dagger.Secret) *dagger.Directory {
	return dag.Gpt(token).
		Ask(`load the dagger module at ` + address + `, explore its API, including types and functions. Then think of 10 example pipelines using it, that really show its purpose. Run each pipeline to make sure it works.`).
		Ask("make sure you run all the examples to make sure they work").
		Ask("write each example pipelie to a file with the .dag extension, and share them back to me. make sure to export to a local path, like ./examples").
		Workdir()
}
