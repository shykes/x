// Record and replay interactive terminal sessions
//
// The termcast module provides a complete API for simulating interactive terminal sessions,
// and sharing them as GIFs.
// It can also replay recordings live in the caller's terminal.
//
// Termcast can simulate human keystrokes; execute commands in containers;
// ask an AI to imagine a scenario; and more.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"termcast/internal/dagger"
)

var (
	asciinemaDigest    = "sha256:dc5fed074250b307758362f0b3045eb26de59ca8f6747b1d36f665c1f5dcc7bd"
	asciinemaContainer = dag.
				Container().
				From("ghcr.io/asciinema/asciinema@" + asciinemaDigest).
				WithoutEntrypoint()

	aggGitCommit     = "84ef0590c9deb61d21469f2669ede31725103173"
	defaultContainer = dag.Wolfi().Container(dagger.WolfiContainerOpts{Packages: []string{"dagger"}})
	defaultShell     = []string{"/bin/sh"}
	defaultPrompt    = "$ "
	defaultWidth     = 80
	defaultHeight    = 24
)

func New(
	// Terminal width
	// +optional
	width int,
	// Terminal height
	// +optional
	height int,
	// Containerized environment for executing commands
	// +optional
	container *dagger.Container,
	// Shell to use when executing commands
	// +optional
	shell []string,
	// The prompt shown by the interactive shell
	// +optional
	prompt string,
) *Termcast {
	if container == nil {
		container = defaultContainer
	}
	if shell == nil {
		shell = defaultShell
	}
	if prompt == "" {
		prompt = defaultPrompt
	}
	if width == 0 {
		width = defaultWidth
	}
	if height == 0 {
		height = defaultHeight
	}
	return &Termcast{
		Height:    height,
		Width:     width,
		Container: container,
		Shell:     shell,
		Prompt:    prompt,
	}
}

type Termcast struct {
	// The height of the terminal
	Height int
	// The width of the terminal
	Width int
	// +private
	Events []*Event
	// Time elapsed since beginning of the session, in milliseconds
	Clock int
	// The containerized environment where commands are executed
	// See Exec()
	Container *dagger.Container
	// +private
	Shell []string
	// +private
	Prompt string
}

// Customize the container used by 'exec'
func (m *Termcast) WithContainer(ctr *dagger.Container) *Termcast {
	m.Container = ctr
	return m
}

type Event struct {
	Time int // milliseconds
	Code string
	Data string
}

func (e *Event) Encode() (string, error) {
	out, err := json.Marshal([3]interface{}{float64(e.Time) / 1000, e.Code, e.Data})
	return string(out), err
}

// Simulate data being printed to the terminal, all at once
func (m *Termcast) Print(data string) *Termcast {
	m.Events = append(m.Events, &Event{
		Time: m.Clock,
		Code: "o",
		Data: strings.Replace(data, "\n", "\r\n", -1),
	})
	return m
}

// Append a recording to the end of this recording
func (m *Termcast) Append(other *Termcast) *Termcast {
	for _, e := range other.Events {
		newEvent := &Event{
			Time: m.Clock + e.Time,
			Code: e.Code,
			Data: e.Data,
		}
		m.Events = append(m.Events, newEvent)
		if newEvent.Time > m.Clock {
			m.Clock = newEvent.Time
		}
	}
	return m
}

// Simulate the user waiting for a random amount of time, with no input or output
func (m *Termcast) WaitRandom(
	// The minimum wait time, in milliseconds
	min int,
	// The maximum wait time, in milliseconds
	max int,
) *Termcast {
	rand.Seed(time.Now().UnixNano())
	return m.Wait(min + rand.Intn(max-min))
}

// Simulate waiting for a certain amount of time, with no input or output on the temrinal
func (m *Termcast) Wait(
	// wait time, in milliseconds. Must be an integer.
	ms int,
) *Termcast {
	m.Clock += ms
	return m
}

func asciinemaBinary() *dagger.Directory {
	ctr := dag.
		Wolfi().
		Container(dagger.WolfiContainerOpts{
			Packages: []string{"rust", "build-base", "libgcc"},
		}).
		WithMountedDirectory(
			"/src",
			dag.Git("https://github.com/asciinema/asciinema").Branch("develop").Tree(),
		).
		WithWorkdir("/src").
		WithExec([]string{"cargo", "build", "--release"})
	return dag.
		Directory().
		WithFile("/usr/local/bin/asciinema", ctr.File("target/release/asciinema")).
		WithFile("/usr/lib/libgcc_s.so.1", ctr.File("/usr/lib/libgcc_s.so.1"))
}

// Simulate a human running an interactive command in a container
func (m *Termcast) Exec(
	ctx context.Context,
	// The command to execute
	cmd string,
	// Toggle simple mode.
	// Enabling simple mode is faster and more reliable, but also less realistic: the recording will print
	//  the final output all at once, without preserving timing information.
	// Disabling mode is more realistic: the timing of each byte is preserved in the recording;
	//  this requires building a binary (which is slow) and injecting it into the target container (which could break).
	// +optional
	// +default=false
	simple bool,
	// Make simulated keystrokes faster
	// +optional
	fast bool,
) (*Termcast, error) {
	m = m.
		Print(m.Prompt).
		Keystrokes(cmd, fast).
		Enter()
	if simple {
		return m.execSimple(ctx, cmd, 100)
	}
	return m.execFull(ctx, cmd)
}

// Build the execution environment for debugging purposes
func (m *Termcast) ExecEnv() *dagger.Container {
	return m.Container.
		WithDirectory("/", asciinemaBinary())
}

func (m *Termcast) execFull(ctx context.Context, cmd string) (*Termcast, error) {
	recording := m.ExecEnv().
		WithExec(
			[]string{"/usr/local/bin/asciinema", "rec", "-c", cmd, "./term.cast"},
			dagger.ContainerWithExecOpts{ExperimentalPrivilegedNesting: true},
		).
		File("./term.cast")
	return m.DecodeFile(ctx, recording, true)
}

func (m *Termcast) execSimple(
	ctx context.Context,
	// The command to execute
	cmd string,
	// How long to wait before showing the command output, in milliseconds
	// +default=100
	delay int,
) (*Termcast, error) {
	m.Container = m.Container.WithExec(m.Shell, dagger.ContainerWithExecOpts{
		Stdin:                         cmd,
		RedirectStdout:                "/tmp/output",
		RedirectStderr:                "/tmp/output",
		ExperimentalPrivilegedNesting: true, // for dagger-in-dagger
	})
	m = m.Wait(delay)
	output, err := m.Container.File("/tmp/output").Contents(ctx)
	if err != nil {
		return m, err
	}
	return m.Print(output), nil
}

// Simulate a human typing text
func (m *Termcast) Keystrokes(
	// Data to input as keystrokes
	data string,
	// Make the simulating typing faster
	// +optional
	fast bool,
) *Termcast {
	for _, c := range data {
		m = m.keystroke(string(c), fast)
	}
	return m
}

// Simulate a human entering a single keystroke
func (m *Termcast) keystroke(
	// Data to input as keystrokes
	data string,
	// Make the simulated keystroke faster
	fast bool,
) *Termcast {
	var (
		min = 5
		max = 200
	)
	if fast {
		min = 1
		max = 80
	}
	return m.WaitRandom(min, max).Print(data)
}

// Simulate pressing the enter key
func (m *Termcast) Enter() *Termcast {
	return m.keystroke("\r\n", false)
}

// Simulate pressing the backspace key
func (m *Termcast) Backspace(
	// Number of backspaces
	// +default=1
	repeat int,
	// Make simulated keystrokes faster
	// +optional
	fast bool,
) *Termcast {
	for i := 0; i < repeat; i += 1 {
		m = m.keystroke("\b \b", fast)
	}
	return m
}

// Encode the recording to a string in the asciicast v2 format
func (m *Termcast) Encode() (*dagger.File, error) {
	var out strings.Builder
	if err := json.NewEncoder(&out).Encode(map[string]interface{}{
		"version": 2,
		"width":   m.Width,
		"height":  m.Height,
	}); err != nil {
		return nil, err
	}
	for _, e := range m.Events {
		line, err := e.Encode()
		if err != nil {
			return nil, err
		}
		out.Write([]byte(line + "\n"))
	}
	return newFile("rec.cast", out.String()), nil
}

// Return an interactive terminal that will play the recording, read-only.
func (m *Termcast) Play(ctx context.Context) error {
	file, err := m.Encode()
	if err != nil {
		return err
	}
	_, err = asciinemaContainer.
		WithFile("term.cast", file).
		Terminal(dagger.ContainerTerminalOpts{
			Cmd: []string{"asciinema", "play", "term.cast"},
		}).
		Sync(ctx)
	return err
}

// Encode the recording into an animated GIF files
func (m *Termcast) Gif() (*dagger.File, error) {
	agg := dag.
		Git("https://github.com/asciinema/agg").
		Tag(aggGitCommit).
		// Tag("v1.4.3").
		Tree().
		DockerBuild().
		WithoutEntrypoint()
	file, err := m.Encode()
	if err != nil {
		return nil, err
	}
	gif := agg.
		WithMountedFile("term.cast", file).
		WithExec([]string{"agg", "term.cast", "cast.gif"}).
		File("cast.gif")
	return gif, nil
}

// Decode an asciicast v2 file, and add its contents to the end of the recording.
//
//	See https://docs.asciinema.org/manual/asciicast/v2/
func (m *Termcast) DecodeFile(
	ctx context.Context,
	// The data to decode, in asciicast format
	data *dagger.File,
	// Indicate whether the decoder should expect an asciicast header.
	// If true, the decoder will parse (and discrd) the header, the load the events
	// If false, the decoder will look for events directly
	// +default=true
	expectHeader bool,
) (*Termcast, error) {
	contents, err := data.Contents(ctx)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(contents))
	if expectHeader {
		// Parse and discard header (we already have our own)
		var header map[string]interface{}
		if err := dec.Decode(&header); err != nil {
			return nil, fmt.Errorf("decode asciicast v2 header: %s", err)
		}
	}
	for dec.More() {
		var o [3]interface{}
		if err := dec.Decode(&o); err != nil {
			return nil, fmt.Errorf("decode asciicast v2 event: %s", err)
		}
		seconds, ok := o[0].(float64)
		if !ok {
			return nil, fmt.Errorf("invalid format")
		}
		milliseconds := int(seconds * 1000)
		code, ok := o[1].(string)
		if !ok {
			return nil, fmt.Errorf("invalid format")
		}
		data, ok := o[2].(string)
		if !ok {
			return nil, fmt.Errorf("invalid format")
		}
		e := &Event{
			Time: milliseconds,
			Code: code,
			Data: data,
		}
		m.Events = append(m.Events, e)
		if e.Time > m.Clock {
			m.Clock = e.Time
		}
	}
	return m, nil
}

// Add one event to the recording, in raw asciicast format
// Add events to the recording from raw asciinema v2 contents
func (m *Termcast) Decode(
	ctx context.Context,
	// The raw events, in asciicast v2 format
	// - exactly one event per line
	// - Do not include te asciicast header
	// - For special characters, use "\u" not "\x"
	// <example>
	// [1.000000, "o", "$ "]
	// [1.500000, "o", "l"]
	// [1.600000, "o", "s"]
	// [1.700000, "o", " "]
	// [1.800000, "o", "-"]
	// [1.900000, "o", "l"]
	// [2.000000, "o", "\r\n"]
	// [2.100000, "o", "total 32\r\n"]
	// [2.200000, "o", "-rw-r--r--  1 user  staff  1024 Mar  7 10:00 file1.txt\r\n"]
	// </example>
	contents string,
	// Indicate whether the decoder should expect an asciicast header.
	// If true, the decoder will parse (and discrd) the header, the load the events
	// If false, the decoder will look for events directly
	// +optional
	// +default=true
	expectHeader bool,
) (*Termcast, error) {
	return m.DecodeFile(
		ctx,
		dag.Directory().WithNewFile("cast", contents).File("cast"),
		expectHeader,
	)
}

func newFile(name, contents string) *dagger.File {
	return dag.Directory().WithNewFile(name, contents).File(name)
}
