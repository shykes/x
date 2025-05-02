package main

import (
	"bufio"
	"context"
	"dagger/test/internal/dagger"
	"fmt"
	"net"
	"time"
)

type Test struct{}

func (m *Test) Test(ctx context.Context) (string, error) {
	srv := dag.Container().
		From("alpine").
		WithExposedPort(4242).
		AsService(dagger.ContainerAsServiceOpts{
			Args: []string{"sh", "-c", "while true; do nc -lk -p 4242 -e echo fuuuuuu; done"},
		})
		//	if _, err := srv.Start(ctx); err != nil {
		//		return "", err
		//	}
	go func() {
		if err := srv.Up(ctx, dagger.ServiceUpOpts{}); err != nil {
			panic(err)
		}
	}()
	//dag.Container().From("alpine").WithServiceBinding("fu", srv).Terminal().Sync(ctx)
	fmt.Printf("sleeping\n")
	time.Sleep(2 * time.Second)
	fmt.Printf("connecting\n")
	conn, err := net.Dial("tcp", "localhost:4242")
	if err != nil {
		return "", err
	}
	buf := bufio.NewReader(conn)
	line, err := buf.ReadString('\n')
	if err != nil {
		return "", err
	}
	return string(line), nil
}
