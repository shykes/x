package main

import (
	"context"

	"slim/internal/dagger"
)

type Slim struct{}

// Slim down a container
func (s *Slim) Slim(ctx context.Context, container *dagger.Container) (*dagger.Container, error) {
	// Start an ephemeral dockerd
	dockerd := dag.Docker().Engine()
	docker := dag.Docker().Cli(dagger.DockerCliOpts{
		Engine: dockerd,
	})
	// Load the input container into the dockerd
	imgRef, err := docker.Import(container).Ref(ctx)
	if err != nil {
		return container, err
	}
	// Setup the slim container, attached to the dockerd
	slim := dag.
		Container().
		// FIXME: choose image based on default architecture
		From("index.docker.io/dslim/slim-arm").
		WithServiceBinding("dockerd", dockerd).
		WithEnvVariable("DOCKER_HOST", "tcp://dockerd:2375").
		WithExec([]string{
			"build",
			"--tag", "slim-output:latest",
			"--target", imgRef,
			// "--show-clogs",
		})
	// Force execution of the slim command
	slim, err = slim.Sync(ctx)
	if err != nil {
		return container, err
	}
	// Extract the resulting image back into a container
	return docker.Image(dagger.DockerCliImageOpts{
		Repository: "slim-output",
		Tag:        "latest",
	}).Export(), nil
}

func (s *Slim) Compare(ctx context.Context, container *dagger.Container) (*dagger.Container, error) {
	slimmed, err := s.Slim(ctx, container)
	if err != nil {
		return nil, err
	}
	debug := dag.
		Container().
		From("alpine").
		WithMountedDirectory("before", slimmed.Rootfs()).
		WithMountedDirectory("after", container.Rootfs())
	return debug, nil
}
