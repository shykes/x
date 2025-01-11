package main

import (
	"context"
	"dagger/sandbox/internal/dagger"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/codes"
)

func New() Sandbox {
	return Sandbox{
		Home:      dag.Directory(),
		Base:      dag.Wolfi().Container(),
		DaggerCli: dag.DaggerCli().Binary(),
		Username:  "👤",
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
	// History of script execution
	History []Run
	// Instruction manuals for the user of the sandbox
	Manuals   []Manual
	DaggerCli *dagger.File // +private
}

// The host container for the sandbox
func (s Sandbox) Host() *dagger.Container {
	return s.Base.
		WithMountedFile("/bin/dagger", s.DaggerCli).
		WithEnvVariable("HOME", "/sandbox").
		WithDirectory("$HOME", s.Home, dagger.ContainerWithDirectoryOpts{Expand: true}).
		WithWorkdir("$HOME", dagger.ContainerWithWorkdirOpts{Expand: true})
}

// All filesystem changes made to the host sandbox so far
func (s Sandbox) Changes() *dagger.Directory {
	changes := dag.Directory()
	for _, run := range s.History {
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
	for _, man := range s.Manuals {
		if man.Key == key {
			span, _ := Tracer().Start(ctx, fmt.Sprintf("[%s] 📖 %s", s.Username, man.Description))
			span.Done()
			return man.Contents, nil
		}
	}
	return "", fmt.Errorf("no such manual: %s", key)
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
			[]string{"dagger", "shell", "-s"},
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
	s.History = append(s.History, Run{
		Username:   s.Username,
		HostBefore: hostBefore,
		Script:     script,
		Stdout:     stdout,
		Stderr:     stderr,
		ExitCode:   exitCode,
		HostAfter:  hostAfter,
	})
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
