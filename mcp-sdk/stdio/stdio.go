package stdio

import (
	"context"
	"fmt"
	"net"
	"time"

	"dagger.io/dagger"
)

type StdioServer struct {
	ctr       *dagger.Container
	stdioTool *dagger.File
	args      []string
}

func NewStdioServer(ctr *dagger.Container, args []string, stdioTool *dagger.File) (*StdioServer, error) {
	return &StdioServer{
		ctr:       ctr,
		stdioTool: stdioTool,
		args:      args,
	}, nil
}

func (srv *StdioServer) Service() *dagger.Service {
	var args []string
	args = append(args, "stdio")
	args = append(args, srv.args...)
	return srv.ctr.
		WithFile("/bin/stdio", srv.stdioTool).
		WithExposedPort(4242).
		WithEnvVariable("PORT", "4242").
		AsService(dagger.ContainerAsServiceOpts{
			Args: args,
		})
}

func (srv *StdioServer) Connect(ctx context.Context) (net.Conn, error) {
	go func() {
		if err := srv.Service().Up(ctx); err != nil {
			panic(err) // FIXME
		}
	}()
	fmt.Printf("waiting before connecting...\n")
	time.Sleep(1 * time.Second)
	fmt.Printf("connecting...\n")
	return net.Dial("tcp", "localhost:4242")
}
