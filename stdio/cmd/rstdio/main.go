// rstdio - simple stdio over TCP
// Usage:
//
//	rstdio <host|host:port|tcp://host:port>        # client mode
//	rstdio <command> [args...]                     # server mode, listens on :3333
//
// In server mode, each new TCP connection spawns the command and connects its
// stdin/stdout to the socket. Stderr goes to the parent's stderr.
package main

import (
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

var port = "8000"

func main() {
	args := os.Args[1:]

	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	switch {
	case len(args) == 1 && looksLikeAddress(args[0]):
		runClient(args[0])
	case len(args) >= 1:
		runServer(args)
	default:
		log.Fatalf("usage: %s <host|host:port|tcp://host:port> | <command> [args...]", os.Args[0])
	}
}

func looksLikeAddress(a string) bool {
	if strings.HasPrefix(a, "tcp://") {
		return true
	}
	if strings.Contains(a, ":") {
		return true
	}
	// bare hostname/IP
	return true
}

func normalizeAddr(a string) string {
	if strings.HasPrefix(a, "tcp://") {
		u, err := url.Parse(a)
		if err == nil {
			a = u.Host
		}
	}
	if !strings.Contains(a, ":") {
		a = net.JoinHostPort(a, port)
	}
	return a
}

func runClient(a string) {
	addr := normalizeAddr(a)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()

	done := make(chan struct{})
	go func() {
		io.Copy(conn, os.Stdin)
		conn.(*net.TCPConn).CloseWrite()
		done <- struct{}{}
	}()
	io.Copy(os.Stdout, conn)
	<-done
}

func runServer(cmdArgs []string) {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("listening on :%s", port)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Print(err)
			continue
		}
		go handleConn(conn, cmdArgs)
	}
}

func handleConn(conn net.Conn, cmdArgs []string) {
	defer conn.Close()

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdin = conn
	cmd.Stdout = conn
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Printf("start %v: %v", cmdArgs[0], err)
		return
	}
	cmd.Wait()
}
