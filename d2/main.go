package main

import (
	"dagger/d-2/internal/dagger"
)

type D2 struct{}

func (m *D2) Svg(source *dagger.File) *dagger.Directory {
	return dag.Container().
		From("alpine").
		WithExec([]string{"apk", "add", "go"}).
		WithExec([]string{"go", "install", "oss.terrastruct.com/d2@latest"}).
		WithEnvVariable("PATH", "$PATH:/root/go/bin", dagger.ContainerWithEnvVariableOpts{Expand: true}).
		WithWorkdir("/d2").
		WithMountedFile("./source.d2", source).
		WithWorkdir("./output").
		Terminal().
		WithExec([]string{"d2", "../source.d2", "."}).
		Directory(".")
}
