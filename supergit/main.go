package main

import "supergit/internal/dagger"

const (
	gitStatePath    = "/git/state"
	gitWorktreePath = "/git/worktree"
)

type Supergit struct{}

func (s *Supergit) Container() *dagger.Container {
	return container()
}

func container() *dagger.Container {
	return dag.
		Container().
		From("cgr.dev/chainguard/wolfi-base").
		WithExec([]string{"apk", "add", "git"})
}
