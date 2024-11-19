package main

import (
	"dagger/singularity/internal/dagger"
)

type Singularity struct{}

// Import a dagger container into a Singularity SIF file
func (m *Singularity) Import(source *dagger.Container) *dagger.File {
	return dag.Container().
		From("singularityware/singularity").
		WithFile("img.tar", source.AsTarball()).
		WithExec([]string{"singularity", "build", "img.sif", "oci-archive://img.tar"}).
		File("img.sif")
}
