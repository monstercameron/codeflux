package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"codeflux.dev/codeflux/internal/storage"
)

// localRepositoryScope is the repository a started coordinator is pointed at.
type localRepositoryScope struct {
	storage.LocalBootstrap
	Path string
	// Revision is the head the scope was bound to. It is carried out of here
	// rather than re-read by the caller because anything a started coordinator
	// records against this project has to name the same revision the bootstrap
	// used, and a second `git rev-parse` can answer differently.
	Revision string
}

// localRepositoryStore is the narrow surface opening a repository needs.
type localRepositoryStore interface {
	EnsureLocalBootstrap(
		ctx context.Context, canonicalPath, gitIdentity, projectName string,
	) (storage.LocalBootstrap, error)
}

// ensureLocalRepository points the coordinator at one repository.
//
// The path defaults to the working directory, because the overwhelmingly
// common case is running the command inside the repository you mean. It is
// resolved and verified before anything is recorded: a coordinator that
// recorded a path which is not a Git repository would produce a workspace
// whose every task fails at its first command.
func ensureLocalRepository(
	ctx context.Context,
	store localRepositoryStore,
	requested string,
) (localRepositoryScope, error) {
	if store == nil {
		return localRepositoryScope{}, errors.New("the coordinator exposed no durable store")
	}
	path := strings.TrimSpace(requested)
	if path == "" {
		working, err := os.Getwd()
		if err != nil {
			return localRepositoryScope{}, errors.New(
				"no repository was named and the working directory is unavailable; " +
					"pass --repository PATH")
		}
		path = working
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return localRepositoryScope{}, fmt.Errorf("resolve repository path %q: %w", path, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return localRepositoryScope{}, fmt.Errorf(
			"no directory at %s; pass --repository PATH", absolute)
	}
	if !info.IsDir() {
		return localRepositoryScope{}, fmt.Errorf("%s is not a directory", absolute)
	}

	root, identity, err := resolveGitRepository(ctx, absolute)
	if err != nil {
		return localRepositoryScope{}, err
	}
	bootstrap, err := store.EnsureLocalBootstrap(ctx, root, identity, filepath.Base(root))
	if err != nil {
		return localRepositoryScope{}, fmt.Errorf("open %s: %w", root, err)
	}
	return localRepositoryScope{LocalBootstrap: bootstrap, Path: root, Revision: identity}, nil
}

// resolveGitRepository returns the repository root and its head revision.
//
// Both come from Git rather than from the path the user typed. Somebody who
// runs the command three directories down means the repository, and a head
// revision the coordinator invented would not identify anything.
func resolveGitRepository(ctx context.Context, directory string) (string, string, error) {
	root, err := gitOutput(ctx, directory, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", fmt.Errorf(
			"%s is not inside a Git repository; codeflux binds every change to a base "+
				"revision, so it needs one", directory)
	}
	absoluteRoot, err := filepath.Abs(filepath.FromSlash(root))
	if err != nil {
		return "", "", fmt.Errorf("resolve repository root %q: %w", root, err)
	}
	identity, err := gitOutput(ctx, absoluteRoot, "rev-parse", "HEAD")
	if err != nil {
		// A repository with no commit has nothing to bind a change to. Saying
		// so here is better than recording it and failing at the first task.
		return "", "", fmt.Errorf(
			"%s has no commits yet; codeflux binds every change to a base revision, "+
				"so make one commit first", absoluteRoot)
	}
	return absoluteRoot, identity, nil
}

// gitOutput runs one read-only Git command.
func gitOutput(ctx context.Context, directory string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", errors.New("git returned nothing")
	}
	return value, nil
}
