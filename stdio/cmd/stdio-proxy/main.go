// mcp-single-endpoint-proxy.go
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------
//  ONE long-running stdio back-end
// ---------------------------------------------------------------------

type backend struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	writeMu  sync.Mutex
	subsMu   sync.RWMutex
	subs     map[int]chan []byte
	nextSub  int
	shutdown chan struct{}
}

func newBackend(path string, args []string) (*backend, error) {
	b := &backend{
		cmd:      exec.Command(path, args...),
		subs:     make(map[int]chan []byte),
		shutdown: make(chan struct{}),
	}
	var err error
	if b.stdin, err = b.cmd.StdinPipe(); err != nil {
		return nil, err
	}
	if b.stdout, err = b.cmd.StdoutPipe(); err != nil {
		return nil, err
	}
	b.cmd.Stderr = os.Stderr

	if err := b.cmd.Start(); err != nil {
		return nil, err
	}
	go b.fanOut()
	return b, nil
}

func (b *backend) fanOut() {
	r := bufio.NewReader(b.stdout)
	for {
		line, err := r.ReadBytes('\n') // assume each JSON-RPC message ends in '\n'
		if len(line) > 0 {
			b.subsMu.RLock()
			for _, ch := range b.subs {
				select {
				case ch <- append([]byte(nil), line...):
				default:
				}
			}
			b.subsMu.RUnlock()
		}
		if err != nil {
			// backend exited; close everything
			close(b.shutdown)
			b.subsMu.RLock()
			for _, ch := range b.subs {
				close(ch)
			}
			b.subsMu.RUnlock()
			return
		}
	}
}

func (b *backend) write(p []byte) error {
	b.writeMu.Lock()
	_, err := b.stdin.Write(p)
	b.writeMu.Unlock()
	return err
}

func (b *backend) addSub() (int, chan []byte) {
	b.subsMu.Lock()
	id := b.nextSub
	b.nextSub++
	ch := make(chan []byte, 32)
	b.subs[id] = ch
	b.subsMu.Unlock()
	return id, ch
}

func (b *backend) removeSub(id int) {
	b.subsMu.Lock()
	if ch, ok := b.subs[id]; ok {
		close(ch)
		delete(b.subs, id)
	}
	b.subsMu.Unlock()
}

// ---------------------------------------------------------------------
//  HTTP layer
// ---------------------------------------------------------------------

var (
	be          *backend
	port        = 4242
	backendCmd  string
	backendArgs []string
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s BACKEND_CMD [ARGS…]", os.Args[0])
	}
	backendCmd, backendArgs = os.Args[1], os.Args[2:]
	if p := os.Getenv("PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}

	var err error
	if be, err = newBackend(backendCmd, backendArgs); err != nil {
		log.Fatalf("starting backend failed: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/sse", sseHandler)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
		// only header read timeout; the stream itself is infinite
		ReadHeaderTimeout: 0,
		WriteTimeout:      0,
		IdleTimeout:       0,
	}

	log.Printf("proxy listening on :%d → %s %v", port, backendCmd, backendArgs)
	log.Fatal(server.ListenAndServe())
}

func sseHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// subscribe
		id, ch := be.addSub()
		defer be.removeSub(id)

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Accel-Buffering", "no") // for nginx

		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", 500)
			return
		}

		// send headers + handshake
		w.WriteHeader(200)
		fmt.Fprint(w, ":\n\n") // ping
		fmt.Fprint(w, "event: messageEndpoint\n")
		fmt.Fprint(w, "data: /sse\n\n") // tell mcptools to POST here
		fl.Flush()

		// fan-out backend lines as SSE events
		for {
			select {
			case raw, ok := <-ch:
				if !ok {
					return
				}
				if _, err := w.Write(toSSE(raw)); err != nil {
					return
				}
				fl.Flush()
			case <-be.shutdown:
				return
			case <-r.Context().Done():
				return
			}
		}

	case http.MethodPost:
		// forward RPC bytes to backend stdin
		if _, err := io.Copy(&stdinWriter{be}, r.Body); err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		w.WriteHeader(204)
		return

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func toSSE(raw []byte) []byte {
	// raw ends in \n; strip it and wrap as event: message
	raw = bytes.TrimRight(raw, "\r\n")
	parts := strings.Split(string(raw), "\n")

	var b strings.Builder
	b.WriteString("event: message\n")
	for _, l := range parts {
		b.WriteString("data: ")
		b.WriteString(l)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	return []byte(b.String())
}

type stdinWriter struct{ *backend }

func (w *stdinWriter) Write(p []byte) (int, error) {
	if err := w.backend.write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}
