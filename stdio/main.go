package main

import (
	"dagger/stdio/internal/dagger"
	"fmt"
)

func New(
	// +optional
	// +defaultPath="."
	// +ignore=["*", "!go.sum", "!go.mod", "!cmd/stdio-proxy"]
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
func (srv *Stdio) Proxy(ctr *dagger.Container,
	// +optional
	// +default=4242
	port int,
) *dagger.Service {
	ctr = ctr.WithEntrypoint([]string{"stdio-proxy"}, dagger.ContainerWithEntrypointOpts{
		KeepDefaultArgs: true,
	})
	return ctr.
		WithFile("/bin/stdio-proxy", srv.Binary()).
		WithExposedPort(port).
		WithEnvVariable("PORT", fmt.Sprintf("%d", port)).
		AsService(dagger.ContainerAsServiceOpts{
			UseEntrypoint: true,
		})
}

// Build the proxy static binary
func (src *Stdio) Binary() *dagger.File {
	// FIXME:
	// go build -a
	// go build -installsuffix cgo
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
			"-o", "bin/stdio-proxy",
			"./cmd/stdio-proxy",
		}).
		File("bin/stdio-proxy")
}
