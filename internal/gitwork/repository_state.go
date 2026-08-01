package gitwork

import "context"

// RepositoryState is what a person needs to know about a repository before
// they act on it.
//
// It is read from Git rather than from the database, because the database
// records what was true when a repository was opened and a working tree
// changes between one glance at the interface and the next. A top bar that
// says "main / uncommitted changes" while the tree is on another branch is
// worse than one that says nothing.
type RepositoryState struct {
	Branch           string
	HeadRevision     string
	Detached         bool
	Dirty            bool
	ChangedPathCount uint32
}

// ReadRepositoryState reports the current branch, head, and cleanliness.
//
// Every field is optional in the sense that Git may decline to answer: a
// repository mid-rebase has no branch name, and a bare one has no working
// tree. A field Git will not answer for is left empty rather than guessed, so
// a caller can say "Unknown" instead of something false.
func (service *Service) ReadRepositoryState(
	ctx context.Context,
	repositoryPath string,
) (RepositoryState, error) {
	directory, err := canonicalDirectory(repositoryPath)
	if err != nil {
		return RepositoryState{}, err
	}
	state := RepositoryState{}
	// --abbrev-ref answers "HEAD" for a detached head rather than failing, so
	// that answer is translated here instead of being shown to a person as a
	// branch literally named HEAD.
	if branch, branchErr := service.gitText(
		ctx, directory, "rev-parse", "--abbrev-ref", "HEAD",
	); branchErr == nil {
		if branch == "HEAD" {
			state.Detached = true
		} else {
			state.Branch = branch
		}
	}
	if head, headErr := service.gitText(
		ctx, directory, "rev-parse", "--verify", "HEAD",
	); headErr == nil {
		state.HeadRevision = head
	}
	status, statusErr := service.gitText(
		ctx, directory, "status", "--porcelain=v1", "--untracked-files=normal",
	)
	if statusErr != nil {
		// A repository whose status cannot be read is reported as clean rather
		// than as dirty: claiming uncommitted work that may not exist would
		// stop a person from starting a task for no reason.
		return state, nil
	}
	changed := parsePorcelainPaths(status)
	state.ChangedPathCount = uint32(len(changed))
	state.Dirty = len(changed) > 0
	return state, nil
}
