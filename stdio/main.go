package main

import (
	"dagger/stdio/internal/dagger"
	"fmt"
	"math/rand"
	"time"
)

func New(
	// +optional
	// +defaultPath="."
	// +ignore=["*", "!go.sum", "!go.mod", "!cmd/rstdio"]
	proxySource *dagger.Directory,
) (*Stdio, error) {
	return &Stdio{
		ProxySource: proxySource,
	}, nil
}

type Stdio struct {
	ProxySource *dagger.Directory // +private
}

// Execute the given container as a stdio server, and expose it as a TCP service
func (srv *Stdio) Server(
	ctr *dagger.Container,
	// +optional
	// +default=8000
	port int,
) *dagger.Service {
	return ctr.
		WithFile("/bin/rstdio", srv.Binary()).
		WithEntrypoint([]string{"rstdio"}, dagger.ContainerWithEntrypointOpts{
			KeepDefaultArgs: true,
		}).
		WithExposedPort(port).
		WithEnvVariable("PORT", fmt.Sprintf("%d", port)).
		AsService(dagger.ContainerAsServiceOpts{
			UseEntrypoint: true,
		})
}

func (srv *Stdio) Client(ctr *dagger.Container) *dagger.Container {
	return ctr.
		WithFile("/bin/rstdio", srv.Binary())
}

// Build the proxy static binary
func (src *Stdio) Binary() *dagger.File {
	return dag.Container().
		From("golang:alpine").
		WithWorkdir("/app").
		WithMountedDirectory(".", src.ProxySource).
		WithEnvVariable("CGO_ENABLED", "0").
		WithExec([]string{
			"go", "build",
			"-a",
			"-installsuffix", "cgo",
			"-ldflags", `-s -w`,
			"-o", "./bin/",
			"./...",
		}).
		File("bin/rstdio")
}

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func RandString(n int) string {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
