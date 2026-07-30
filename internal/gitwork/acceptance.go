package gitwork

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// AcceptanceMode controls whether acceptance commits the task branch, applies
// the patch to the primary worktree, or performs both explicit actions.
type AcceptanceMode string

const (
	AcceptanceCommit AcceptanceMode = "commit"
	AcceptancePatch  AcceptanceMode = "patch"
	AcceptanceBoth   AcceptanceMode = "both"
)

// AcceptTaskChangeInput binds acceptance to the exact reviewed diff.
type AcceptTaskChangeInput struct {
	TaskID               domain.TaskID
	RepositoryPath       string
	ExpectedDiffIdentity string
	Mode                 AcceptanceMode
	AuthorName           string
	AuthorEmail          string
	CommitMessage        string
}

// AcceptanceResult preserves base/task/diff attribution for review.
type AcceptanceResult struct {
	TaskID         domain.TaskID
	BaseRevision   string
	DiffIdentity   string
	BranchName     string
	CommitRevision string
	PatchApplied   bool
}

type inputCommandRunner interface {
	RunInput(
		context.Context,
		string,
		string,
		[]byte,
		...string,
	) (CommandResult, error)
}

// AcceptTaskChange validates the exact review identity before any commit or
// primary-worktree write.
func (service *Service) AcceptTaskChange(
	ctx context.Context,
	input AcceptTaskChangeInput,
) (AcceptanceResult, error) {
	if input.TaskID.IsZero() || len(input.ExpectedDiffIdentity) != 64 {
		return AcceptanceResult{}, errors.New("task and reviewed diff identity are required")
	}
	if input.Mode != AcceptanceCommit &&
		input.Mode != AcceptancePatch &&
		input.Mode != AcceptanceBoth {
		return AcceptanceResult{}, errors.New("acceptance mode is invalid")
	}
	if input.Mode == AcceptanceCommit || input.Mode == AcceptanceBoth {
		if err := validateCommitAttribution(
			input.AuthorName,
			input.AuthorEmail,
			input.CommitMessage,
		); err != nil {
			return AcceptanceResult{}, err
		}
	}
	diff, err := service.GetTaskDiff(ctx, TaskDiffQuery{TaskID: input.TaskID})
	if err != nil {
		return AcceptanceResult{}, err
	}
	if diff.Identity != input.ExpectedDiffIdentity {
		return AcceptanceResult{}, ErrEditConflict
	}
	if diff.FilesChanged == 0 {
		return AcceptanceResult{}, errors.New("accepted task diff is empty")
	}
	binding, err := service.bindings.GetWorktreeBinding(ctx, input.TaskID)
	if err != nil {
		return AcceptanceResult{}, err
	}
	result := AcceptanceResult{
		TaskID: input.TaskID, BaseRevision: binding.BaseRevision,
		DiffIdentity: diff.Identity, BranchName: binding.BranchName,
		CommitRevision: binding.HeadRevision,
	}
	if input.Mode == AcceptanceCommit || input.Mode == AcceptanceBoth {
		binding, err = service.commitAcceptedChange(ctx, binding, input)
		if err != nil {
			return AcceptanceResult{}, err
		}
		result.CommitRevision = binding.HeadRevision
		committedDiff, err := service.GetTaskDiff(
			ctx,
			TaskDiffQuery{TaskID: input.TaskID},
		)
		if err != nil {
			return AcceptanceResult{}, err
		}
		diff = committedDiff
		result.DiffIdentity = diff.Identity
	}
	if input.Mode == AcceptancePatch || input.Mode == AcceptanceBoth {
		if err := service.applyAcceptedPatch(
			ctx,
			input.RepositoryPath,
			binding.BaseRevision,
			[]byte(diff.UnifiedDiff),
		); err != nil {
			return AcceptanceResult{}, err
		}
		result.PatchApplied = true
	}
	return result, nil
}

func (service *Service) commitAcceptedChange(
	ctx context.Context,
	binding storage.WorktreeBinding,
	input AcceptTaskChangeInput,
) (storage.WorktreeBinding, error) {
	verification, err := service.VerifyTaskWorktree(ctx, binding.TaskID)
	if err != nil {
		return storage.WorktreeBinding{}, err
	}
	if !verification.Dirty {
		if binding.HeadRevision == binding.BaseRevision {
			return storage.WorktreeBinding{}, errors.New("task branch has no accepted commit or pending changes")
		}
	} else {
		if _, err := service.runner.Run(
			ctx,
			binding.WorktreePath,
			"git",
			"add",
			"-A",
			"--",
		); err != nil {
			return storage.WorktreeBinding{}, fmt.Errorf("stage accepted task change: %w", err)
		}
	}
	author := input.AuthorName + " <" + input.AuthorEmail + ">"
	if _, err := service.runner.Run(
		ctx,
		binding.WorktreePath,
		"git",
		"-c",
		"core.hooksPath="+service.disabledHooksPath(),
		"-c",
		"commit.gpgsign=false",
		"-c",
		"user.name="+input.AuthorName,
		"-c",
		"user.email="+input.AuthorEmail,
		"commit",
		"--no-verify",
		"--allow-empty",
		"--author",
		author,
		"-m",
		input.CommitMessage,
	); err != nil {
		_, _ = service.runner.Run(
			context.Background(),
			binding.WorktreePath,
			"git",
			"reset",
			"--mixed",
			binding.HeadRevision,
		)
		return storage.WorktreeBinding{}, fmt.Errorf("commit accepted task change: %w", err)
	}
	head, err := service.gitText(ctx, binding.WorktreePath, "rev-parse", "HEAD")
	if err != nil {
		return storage.WorktreeBinding{}, err
	}
	advanced, err := service.bindings.AdvanceWorktreeBinding(
		ctx,
		storage.AdvanceWorktreeBinding{
			TaskID: binding.TaskID, ExpectedRevision: binding.Revision,
			ExpectedHead: binding.HeadRevision, HeadRevision: head,
		},
	)
	if err != nil {
		_, _ = service.runner.Run(
			context.Background(),
			binding.WorktreePath,
			"git",
			"reset",
			"--mixed",
			binding.HeadRevision,
		)
		return storage.WorktreeBinding{}, err
	}
	return advanced, nil
}

func (service *Service) applyAcceptedPatch(
	ctx context.Context,
	repositoryPath string,
	baseRevision string,
	patch []byte,
) error {
	repository, err := canonicalDirectory(repositoryPath)
	if err != nil {
		return err
	}
	if pathWithin(repository, service.root) || pathWithin(service.root, repository) {
		return errors.New("primary repository and worktree root overlap")
	}
	head, err := service.gitText(ctx, repository, "rev-parse", "--verify", "HEAD")
	if err != nil || head != baseRevision {
		return errors.New("primary repository no longer matches the accepted base revision")
	}
	runner, ok := service.runner.(inputCommandRunner)
	if !ok {
		return errors.New("command runner does not support patch input")
	}
	if _, err := runner.RunInput(
		ctx,
		repository,
		"git",
		patch,
		"apply",
		"--check",
		"--binary",
		"--whitespace=nowarn",
		"-",
	); err != nil {
		return fmt.Errorf("check accepted patch against primary worktree: %w", err)
	}
	if _, err := runner.RunInput(
		ctx,
		repository,
		"git",
		patch,
		"apply",
		"--binary",
		"--whitespace=nowarn",
		"-",
	); err != nil {
		return fmt.Errorf("apply accepted patch to primary worktree: %w", err)
	}
	return nil
}

func validateCommitAttribution(name, email, message string) error {
	if name == "" || name != strings.TrimSpace(name) || len(name) > 255 ||
		strings.ContainsAny(name, "\r\n<>") {
		return errors.New("commit author name is invalid")
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 320 ||
		strings.ContainsAny(email, "\r\n") {
		return errors.New("commit author email is invalid")
	}
	if message == "" || message != strings.TrimSpace(message) ||
		len(message) > 4096 || strings.ContainsAny(message, "\r\n") {
		return errors.New("commit message must be one bounded line")
	}
	return nil
}
