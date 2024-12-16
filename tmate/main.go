package main

import "tmate/internal/dagger"

type Tmate struct{}

// Run tmate in a container
func (t *Tmate) Tmate(
	// +optional
	base *dagger.Container,
	// +optional
	version string,
) *dagger.Container {
	return t.Release(version).Container(base, "")
}
