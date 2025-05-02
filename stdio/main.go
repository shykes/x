package main

import (
	"dagger/stdio/internal/dagger"
)

func New(
	// +optional
	// +defaultPath="stdio.sh"
	stdioTool *dagger.File,
) (*Stdio, error) {
	return &Stdio{
		StdioTool: stdioTool,
	}, nil
}

type Stdio struct {
	StdioTool *dagger.File // +private
}

// Execute the given container as a stdio server, and expose it as a TCP service
func (srv *Stdio) Proxy(ctr *dagger.Container) *dagger.Service {
	ctr = ctr.WithEntrypoint([]string{"stdio"}, dagger.ContainerWithEntrypointOpts{
		KeepDefaultArgs: true,
	})
	return ctr.
		WithFile("/bin/stdio", srv.StdioTool).
		WithExposedPort(4242).
		WithEnvVariable("PORT", "4242").
		AsService(dagger.ContainerAsServiceOpts{
			UseEntrypoint: true,
		})
}
