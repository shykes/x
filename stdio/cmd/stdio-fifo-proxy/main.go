// fifo_wrap.go
package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
)

func ensureFIFO(p string) {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		if err := syscall.Mkfifo(p, 0666); err != nil {
			log.Fatalf("mkfifo %s: %v", p, err)
		}
	} else if err != nil {
		log.Fatalf("stat %s: %v", p, err)
	}
}

func paths() (inPath, outPath string) {
	prefix := os.Getenv("FIFO_PREFIX")
	if prefix == "" {
		prefix = "."
	}
	return filepath.Join(prefix, "in"), filepath.Join(prefix, "out")
}

func serverMode() {
	inPath, outPath := paths()
	ensureFIFO(inPath)
	ensureFIFO(outPath)

	inF, err := os.OpenFile(inPath, os.O_RDONLY, 0)
	if err != nil {
		log.Fatalf("open %s: %v", inPath, err)
	}
	defer inF.Close()

	outF, err := os.OpenFile(outPath, os.O_WRONLY, 0)
	if err != nil {
		log.Fatalf("open %s: %v", outPath, err)
	}
	defer outF.Close()

	cmd := exec.Command(os.Args[1], os.Args[2:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inF, outF, os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("server exit: %v", err)
	}
}

func clientMode() {
	inPath, outPath := paths()
	ensureFIFO(inPath)
	ensureFIFO(outPath)

	// client writes to in, reads from out
	inW, err := os.OpenFile(inPath, os.O_WRONLY, 0)
	if err != nil {
		log.Fatalf("open %s: %v", inPath, err)
	}
	defer inW.Close()

	outR, err := os.OpenFile(outPath, os.O_RDONLY, 0)
	if err != nil {
		log.Fatalf("open %s: %v", outPath, err)
	}
	defer outR.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		io.Copy(os.Stdout, outR)
		wg.Done()
	}()
	io.Copy(inW, os.Stdin)
	inW.Close() // signal EOF to server
	wg.Wait()
}

func main() {
	if len(os.Args) < 2 {
		clientMode()
	} else {
		serverMode()
	}
}
