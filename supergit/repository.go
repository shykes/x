package main

import (
	"context"
	"strings"

	"supergit/internal/dagger"
)

// A new git repository
func (r *Supergit) Repository() *Repository {
	// We need to initialize these fields in a constructor,
	// because we can't hide them behind an accessor
	// (private fields are not persisted in between dagger function calls)
	return &Repository{
		State: container().
			WithDirectory(gitStatePath, dag.Directory()).
			WithExec([]string{
				"git", "--git-dir=" + gitStatePath,
				"init", "-q", "--bare",
			}).
			Directory(gitStatePath),
		Worktree: dag.Directory(),
	}
}

// A git repository
type Repository struct {
	State    *dagger.Directory
	Worktree *dagger.Directory
}

// Execute a git command in the repository
func (r *Repository) WithGitCommand(args []string) *Repository {
	return r.GitCommand(args).Output()
}

// A Git command executed from the current repository state
func (r *Repository) GitCommand(args []string) *GitCommand {
	return &GitCommand{
		Args:  args,
		Input: r,
	}
}

// A Git command
type GitCommand struct {
	Args  []string
	Input *Repository
}

func (cmd *GitCommand) container() *dagger.Container {
	prefix := []string{"git", "--git-dir=" + gitStatePath, "--work-tree=" + gitWorktreePath}
	execArgs := append(prefix, cmd.Args...)
	return container().
		WithDirectory(gitStatePath, cmd.Input.State).
		WithDirectory(gitWorktreePath, cmd.Input.Worktree).
		WithExec(execArgs)
}

func (cmd *GitCommand) Stdout(ctx context.Context) (string, error) {
	return cmd.container().Stdout(ctx)
}

func (cmd *GitCommand) Stderr(ctx context.Context) (string, error) {
	return cmd.container().Stderr(ctx)
}

func (cmd *GitCommand) Sync(ctx context.Context) (*GitCommand, error) {
	_, err := cmd.container().Sync(ctx)
	return cmd, err
}

func (cmd *GitCommand) Output() *Repository {
	container := cmd.container()
	return &Repository{
		State:    container.Directory(gitStatePath),
		Worktree: container.Directory(gitWorktreePath),
	}
}

func (r *Repository) WithRemote(name, url string) *Repository {
	return r.WithGitCommand([]string{"remote", "add", name, url})
}

func (r *Repository) Tag(name string) *Tag {
	return &Tag{
		Repository: r,
		Name:       name,
	}
}

func (t *Tag) FullName() string {
	if strings.HasPrefix(t.Name, "refs/tags/") {
		return t.Name
	}
	if strings.HasPrefix(t.Name, "tags/") {
		return "refs/" + t.Name
	}
	return "refs/tags/" + t.Name
}

type Tag struct {
	Repository *Repository
	Name       string
}

func (t *Tag) Tree() *dagger.Directory {
	return t.Repository.WithGitCommand([]string{"checkout", t.Name}).Worktree
}

func (r *Repository) Commit(digest string) *Commit {
	return &Commit{
		Repository: r,
		Digest:     digest,
	}
}

type Commit struct {
	Digest     string
	Repository *Repository
}

func (c *Commit) Tree() *dagger.Directory {
	return c.Repository.
		WithGitCommand([]string{"checkout", c.Digest}).
		Worktree
}
