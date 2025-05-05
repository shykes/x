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
	"time"
)

/* ------------------------------------------------------------------------- */
/*  ONE  long-running stdio back-end                                         */
/* ------------------------------------------------------------------------- */

type backend struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	writeMu   sync.Mutex          // serialise writes to stdin
	subsMu    sync.RWMutex        // protect subscribers map
	subs      map[int]chan []byte // each sub gets raw JSON lines
	nextSubID int
	shutdown  chan struct{}
}

func newBackend(cmd string, args []string) (*backend, error) {
	b := &backend{
		cmd:      exec.Command(cmd, args...),
		subs:     make(map[int]chan []byte),
		shutdown: make(chan struct{}),
	}
	var err error
	b.stdin, err = b.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	b.stdout, err = b.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	b.cmd.Stderr = os.Stderr
	if err := b.cmd.Start(); err != nil {
		return nil, err
	}

	go b.fanOut()
	return b, nil
}

/*  fan-out every JSON line the server writes to *all* subscribers          */
func (b *backend) fanOut() {
	r := bufio.NewReader(b.stdout)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			b.subsMu.RLock()
			for _, ch := range b.subs {
				select { // drop message when subscriber buffer is full
				case ch <- append([]byte(nil), line...):
				default:
				}
			}
			b.subsMu.RUnlock()
		}
		if err != nil { // EOF or backend exited – close every client
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

/* safe write to backend stdin (one POST at a time)                         */
func (b *backend) write(p []byte) error {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	_, err := b.stdin.Write(p)
	return err
}

/* subscribe -- returns (id, channel)                                       */
func (b *backend) addSub() (int, chan []byte) {
	b.subsMu.Lock()
	defer b.subsMu.Unlock()
	id := b.nextSubID
	b.nextSubID++
	ch := make(chan []byte, 32) // small buffer for bursts
	b.subs[id] = ch
	return id, ch
}

func (b *backend) removeSub(id int) {
	b.subsMu.Lock()
	defer b.subsMu.Unlock()
	if ch, ok := b.subs[id]; ok {
		close(ch)
		delete(b.subs, id)
	}
}

/* ------------------------------------------------------------------------- */
/*  HTTP server                                                              */
/* ------------------------------------------------------------------------- */

var (
	be          *backend
	port        = 8000
	backendCmd  string
	backendArgs []string
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s BACKEND_CMD [ARG...]", os.Args[0])
	}
	backendCmd, backendArgs = os.Args[1], os.Args[2:]

	if p := os.Getenv("PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}

	var err error
	be, err = newBackend(backendCmd, backendArgs)
	if err != nil {
		log.Fatalf("failed to start backend: %v", err)
	}

	http.HandleFunc("/", handler)
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      0, // stream forever
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("proxy listening on :%d  (backend: %s %v)", port, backendCmd, backendArgs)
	log.Fatal(srv.ListenAndServe())
}

func handler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		streamHandler(w, r)
	case http.MethodPost:
		postHandler(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

/* -------------  GET  → subscribe to stdout as SSE  ---------------------- */

func streamHandler(w http.ResponseWriter, r *http.Request) {
	id, ch := be.addSub()
	defer be.removeSub(id)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // nginx: disable response buffering

	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// initial ping so impatient LBs / clients don’t abort
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ":\n\n")
	fl.Flush()

	// forward every JSON line as a proper SSE event
	for {
		select {
		case raw, ok := <-ch:
			if !ok {
				return
			} // backend died
			ss := toSSE(raw)
			if _, err := w.Write(ss); err != nil {
				return
			}
			fl.Flush()
		case <-be.shutdown:
			return
		case <-r.Context().Done():
			return
		}
	}
}

/* convert a backend JSON line into an SSE frame                           */
func toSSE(raw []byte) []byte {
	raw = bytes.TrimRight(raw, "\r\n")
	// if the line itself contains LF, prefix every sub-line with "data: "
	parts := strings.Split(string(raw), "\n")
	var b strings.Builder
	for _, p := range parts {
		b.WriteString("data: ")
		b.WriteString(p)
		b.WriteByte('\n')
	}
	b.WriteByte('\n') // <blank line> terminator
	return []byte(b.String())
}

/* -------------  POST  → write body to backend stdin  -------------------- */

func postHandler(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}
	if _, err := io.Copy(&stdinWriter{be}, r.Body); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type stdinWriter struct{ *backend }

func (w *stdinWriter) Write(p []byte) (int, error) {
	if err := w.backend.write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}
