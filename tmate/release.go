package main

import (
	"bufio"
	"context"
	"path"
	"strings"

	"tmate/internal/dagger"
)

const (
	defaultVersion = "2.4.0"
	defaultBinPath = "/usr/bin"
)

// A release of the Tmate software
func (t *Tmate) Release(
	// +optional
	version string,
) *Release {
	if version == "" {
		version = defaultVersion
	}
	return &Release{
		Version: version,
	}
}

// A release of the Tmate software
type Release struct {
	// Version number of this release
	Version string
}

// The source code for this release
func (r *Release) Source() *dagger.Directory {
	return dag.
		Git("https://github.com/tmate-io/tmate.git").
		Tag(r.Version).
		Tree()
}

// A static build of Tmate
func (r *Release) StaticBinary() *dagger.File {
	// FIXME: replace Dockerfile with pure Go
	// FIXME: platform argument
	return r.Source().DockerBuild().File("tmate")
}

// A container with tmate installed.
//
// Arguments:
//   - `base` (optional): custom base container
//   - `binPath` the path where the tmate static binary will be installed. Default: /usr/bin
func (r *Release) Container(
	// +optional
	base *dagger.Container,
	// +optional
	// +default="/usr/bin"
	binPath string,
) *dagger.Container {
	var ctr *dagger.Container
	if base != nil {
		path := binPath
		if path == "" {
			path = defaultBinPath
		}
		path += "/tmate"
		ctr = base.WithFile(path, r.StaticBinary())
	} else {
		ctr = r.dynamicBuild()
	}
	return ctr.WithDefaultArgs([]string{"tmate"})
}

// Build the dynamic tmate binary, and return the whole build environmnent,
// with the tmate source as working directory.
func (r *Release) dynamicBuild() *dagger.Container {
	preBuild := dag.
		Container().
		From("ubuntu").
		WithEnvVariable("DEBIAN_FRONTEND", "noninteractive").
		WithExec([]string{"apt-get", "update"}).
		WithExec([]string{"apt-get", "install", "-y", "git-core"}).
		WithExec([]string{"apt-get", "install", "-y", "build-essential"}).
		WithExec([]string{"apt-get", "install", "-y", "pkg-config"}).
		WithExec([]string{"apt-get", "install", "-y", "libtool"}).
		WithExec([]string{"apt-get", "install", "-y", "libevent-dev"}).
		WithExec([]string{"apt-get", "install", "-y", "libncurses-dev"}).
		WithExec([]string{"apt-get", "install", "-y", "zlib1g-dev"}).
		WithExec([]string{"apt-get", "install", "-y", "automake"}).
		WithExec([]string{"apt-get", "install", "-y", "libssh-dev"}).
		WithExec([]string{"apt-get", "install", "-y", "libmsgpack-dev"}).
		WithExec([]string{"apt-get", "install", "-y", "autoconf"}).
		WithExec([]string{"apt-get", "install", "-y", "libssl-dev"}).
		WithMountedDirectory("/src", r.Source()).
		WithWorkdir("/src")
	postBuild := preBuild.
		WithExec([]string{"autoupdate"}).
		WithExec([]string{"./autogen.sh"}).
		WithExec([]string{"./configure"}).
		WithExec([]string{"make"}).
		WithExec([]string{"make", "install"})
	return postBuild
}

// A build of tmate as a dynamically linked binary + required libraries
func (r *Release) Dynamic(ctx context.Context) (*dagger.Directory, error) {
	// Execute the build and keep the full build environment
	buildEnv := r.dynamicBuild()
	// Extract dynamic libraries
	libs, err := dynLibs(ctx, buildEnv, "tmate")
	if err != nil {
		return nil, err
	}
	// Extract dynamic executable
	exe := buildEnv.File("tmate")
	// Bundle executable + libs in a directory
	bundle := dag.
		Directory().
		WithFile("/bin/tmate", exe).
		WithDirectory("/lib", libs)
	return bundle, nil
}

// A utility that extracts dynamic libraries required by a binary
// Note: the container must have the `ldd` utility installed
func dynLibs(ctx context.Context, ctr *dagger.Container, binary string) (*dagger.Directory, error) {
	// FIXME: inspect the binary contents in pure Go instead of shelling out to ldd
	ldd, err := ctr.WithExec([]string{"ldd", binary}).Stdout(ctx)
	if err != nil {
		return nil, err
	}
	libs := dag.Directory()
	// Parse the output of ldd
	for scanner := bufio.NewScanner(strings.NewReader(ldd)); scanner.Scan(); {
		line := scanner.Text()
		fields := strings.Fields(line) // Split line by whitespace
		if len(fields) != 4 {
			continue
		}
		libPath := fields[2]
		libName := path.Base(libPath)
		lib := ctr.File(libPath)
		libs = libs.WithFile(libName, lib)
	}
	return libs, nil
}
