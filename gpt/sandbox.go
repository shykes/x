package main

import (
	"context"
	"dagger/gpt/internal/dagger"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/codes"
)

func NewSandbox() Sandbox {
	return Sandbox{
		Home: dag.Directory(),
		Base: dag.Container().From("docker.io/library/alpine:latest@sha256:21dc6063fd678b478f57c0e13f47560d0ea4eeba26dfc947b2a4f81f686b9f45"),
		// FIXME: disable building dagger CLI from source, because of annoying cache misses in our CLI build
		// DaggerCli: dag.DaggerCli().Binary(),
		DaggerCli: dag.
			Container().
			From("registry.dagger.io/engine:main@sha256:50d03804e9c78dcded9f015816a7e7ffbb8b132c675647d64c69cfd19e1cc171").
			File("/usr/local/bin/dagger"),
		Username: "👤",
	}
}

type Sandbox struct {
	// The sandbox's home directory
	Home *dagger.Directory
	// Base image for the sandbox host computer
	Base *dagger.Container
	// User name (for traces and logs)
	// +private
	Username string
	// Runs of script execution
	Runs []Run
	// Instruction manuals for the user of the sandbox
	Manuals      []Manual
	DaggerCli    *dagger.File // +private
	History      []string
	RemoteModule string            // +private
	LocalModule  *dagger.Directory // +private
}

// The host container for the sandbox
func (s Sandbox) Host() *dagger.Container {
	return s.Base.
		WithMountedFile("/bin/dagger", s.DaggerCli).
		WithEnvVariable("HOME", "/sandbox").
		WithDirectory("$HOME", s.Home, dagger.ContainerWithDirectoryOpts{Expand: true}).
		WithWorkdir("$HOME", dagger.ContainerWithWorkdirOpts{Expand: true}).
		WithDefaultTerminalCmd([]string{"/bin/sh"}, dagger.ContainerWithDefaultTerminalCmdOpts{
			ExperimentalPrivilegedNesting: true,
		}).
		WithFile("/bin/sandbox-entrypoint", s.sandboxEntrypoint())
}

func (s Sandbox) sandboxEntrypoint() *dagger.File {
	var script string
	if s.LocalModule != nil {
		script = "exec dagger shell -s -m /module"
	} else if s.RemoteModule != "" {
		// FIXME properly shell-escpape module name
		script = fmt.Sprintf("exec dagger shell -s -m '%s'", s.RemoteModule)
	} else {
		script = "exec dagger shell -s"
	}
	return dag.Directory().
		WithNewFile("sandbox-entrypoint", "#!/bin/sh\n"+script, dagger.DirectoryWithNewFileOpts{Permissions: 0700}).
		File("sandbox-entrypoint")
}

// Configure a remote module as context for the sandbox
func (s Sandbox) WithRemoteModule(address string) Sandbox {
	s.LocalModule = nil
	s.RemoteModule = address
	return s
}

// Configure a local module as context for the sandbox
func (s Sandbox) WithLocalModule(module *dagger.Directory) Sandbox {
	s.RemoteModule = ""
	s.LocalModule = module
	return s
}

// All filesystem changes made to the host sandbox so far
func (s Sandbox) Changes() *dagger.Directory {
	changes := dag.Directory()
	for _, run := range s.Runs {
		// FIXME: are deletions tracked by Directory.Diff? If so, how to handle them properly?
		changes = changes.WithDirectory("/", run.Changes())
	}
	return changes
}

func (s Sandbox) WithUsername(username string) Sandbox {
	s.Username = username
	return s
}

func (s Sandbox) WithSecret(name string, value *dagger.Secret) Sandbox {
	s.Base = s.Base.WithSecretVariable(name, value)
	return s
}

// Configure the sandbox's home directory
func (s Sandbox) WithHome(home *dagger.Directory) Sandbox {
	s.Home = home
	return s
}

// Lookup a manual and return its contents.
func (s Sandbox) ReadManual(ctx context.Context, key string) (string, error) {
	manual, err := s.Manual(ctx, key)
	if err != nil {
		return "", err
	}
	return manual.Contents, nil
}

// Lookup a manual
func (s Sandbox) Manual(ctx context.Context, key string) (*Manual, error) {
	for _, man := range s.Manuals {
		if man.Key == key {
			event := fmt.Sprintf("[%s] 📖 %s", s.Username, man.Description)
			span, _ := Tracer().Start(ctx, event)
			span.Done()
			s.History = append(s.History, event)
			return &man, nil
		}
	}
	return nil, fmt.Errorf("no such manual: %s", key)
}

// Add a note to the sandbox history on behalf of the sandbox user
func (s Sandbox) WithNote(ctx context.Context,
	note string,
	// The name of the user leaving the note. Default to the sandbox username
	// +optional
	username string,
) Sandbox {
	if username == "" {
		username = s.Username
	}
	event := fmt.Sprintf("[%s] %s", username, note)
	ctx, span := Tracer().Start(ctx, event)
	span.End()
	s.History = append(s.History, event)
	return s
}

func (s Sandbox) WithManual(
	// Unique key for the manual
	key,
	// Description for the knowledge. Keep it short, like the cover of a book.
	description,
	// Contents of the manual
	contents string,
) Sandbox {
	s.Manuals = append(s.Manuals, Manual{
		Key:         key,
		Description: description,
		Contents:    contents,
	})
	return s
}

// Import manuals from a directory into the sandbox
// Any .txt or .md file will be read.
// - The filename (minus the extension) is the key
// - The first paragraph is the description
// - The rest of the file is the contents
func (s Sandbox) ImportManuals(ctx context.Context, dir *dagger.Directory) (Sandbox, error) {
	txtPaths, err := dir.Glob(ctx, "**/*.txt")
	if err != nil {
		return s, err
	}
	mdPaths, err := dir.Glob(ctx, "**/*.md")
	if err != nil {
		return s, err
	}
	paths := append(txtPaths, mdPaths...)
	toolnameRE := regexp.MustCompile("[^a-zA-Z0-9_-]")
	for _, p := range paths {
		doc, err := dir.File(p).Contents(ctx)
		if err != nil {
			return s, err
		}
		// Use regex to split paragraphs, allowing for any amount of whitespace or newlines
		re := regexp.MustCompile(`(?m)^\s*$`)
		parts := re.Split(doc, 2)
		description := strings.TrimSpace(parts[0])
		contents := ""
		if len(parts) > 1 {
			contents = strings.TrimSpace(parts[1])
		}
		// Scrub filename
		p = p[:len(p)-len(filepath.Ext(p))]
		key := toolnameRE.ReplaceAllString(p, "")
		s = s.WithManual(key, description, contents)
	}
	return s, nil
}

// An instruction manual for the user of the sandbox
type Manual struct {
	Key         string
	Description string
	Contents    string
}

func (s Sandbox) LastRun() (*Run, error) {
	if len(s.Runs) == 0 {
		return nil, fmt.Errorf("no run in the history")
	}
	return &s.Runs[len(s.Runs)-1], nil
}

// Open an interactive terminal session
func (s Sandbox) Terminal(ctx context.Context) (Sandbox, error) {
	_, err := s.Host().Terminal(dagger.ContainerTerminalOpts{
		Cmd:                           []string{"/bin/sandbox-entrypoint"},
		ExperimentalPrivilegedNesting: true,
	}).Sync(ctx)
	return s, err
}

// Run a script in the sandbox
func (s Sandbox) Run(
	ctx context.Context,
	script string,
) (rs Sandbox, rerr error) {
	ctx, span := Tracer().Start(ctx, fmt.Sprintf("[%s] 💻 %s\n", s.Username, script))
	defer func() {
		if rerr != nil {
			span.SetStatus(codes.Error, rerr.Error())
		}
		span.End()
	}()
	hostBefore := s.Host()
	hostAfter := hostBefore.
		WithExec(
			[]string{"/bin/sandbox-entrypoint"},
			dagger.ContainerWithExecOpts{
				ExperimentalPrivilegedNesting: true,
				Expect:                        dagger.ReturnTypeAny,
				Stdin:                         script,
			},
		)
	stdout, err := hostAfter.Stdout(ctx)
	if err != nil {
		return s, err
	}
	stderr, err := hostAfter.Stderr(ctx)
	if err != nil {
		return s, err
	}
	exitCode, err := hostAfter.ExitCode(ctx)
	if err != nil {
		return s, err
	}
	if exitCode != 0 {
		span.SetStatus(codes.Error, stderr)
	}
	run := Run{
		Username:   s.Username,
		HostBefore: hostBefore,
		Script:     script,
		Stdout:     stdout,
		Stderr:     stderr,
		ExitCode:   exitCode,
		HostAfter:  hostAfter,
	}
	s.Runs = append(s.Runs, run)
	s.History = append(s.History, run.Short())
	s.Base = hostAfter.WithoutDirectory("$HOME", dagger.ContainerWithoutDirectoryOpts{Expand: true})
	s.Home = hostAfter.Directory("$HOME", dagger.ContainerDirectoryOpts{Expand: true})
	return s, nil
}

type Run struct {
	Username   string
	Script     string
	HostBefore *dagger.Container
	HostAfter  *dagger.Container
	Stdout     string
	Stderr     string
	ExitCode   int
}

func (r Run) Short() string {
	var emoji string
	if r.ExitCode == 0 {
		emoji = "✅"
	} else {
		emoji = "☠️"
	}
	return fmt.Sprintf("[%s] 💻%s %s", r.Username, emoji, r.Script)
}

// All filesystem changes made by the run
func (r Run) Changes() *dagger.Directory {
	return r.HostBefore.Rootfs().Diff(r.HostAfter.Rootfs())
}

// Encode the run result as JSON
func (r Run) ResultJSON() (string, error) {
	var res struct {
		Stdout   string
		Stderr   string
		ExitCode int
	}
	res.Stdout = r.Stdout
	res.Stderr = r.Stderr
	res.ExitCode = r.ExitCode
	b, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
