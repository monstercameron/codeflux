package main

import (
	"strconv"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
)

// workspaceSummary is what the top bar knows about the repository being worked
// in.
//
// It carries only what the coordinator answered. The top bar previously read
// "codeflux / main / uncommitted changes" from a fixture, which was right by
// coincidence on one machine and wrong everywhere else, and stayed right-looking
// even when the coordinator was not connected at all.
type workspaceSummary struct {
	Repository     string
	Branch         string
	WorktreeStatus string
}

// projectWorkspaceSummary reads a repository inspection into the top bar.
//
// A field Git declined to answer is left empty rather than filled with a
// plausible default: the top bar renders an unanswered field as unknown, and a
// branch invented here would be indistinguishable from one that was read.
func projectWorkspaceSummary(
	response *codefluxv1.InspectRepositoryResponse,
) workspaceSummary {
	repository := response.GetRepository()
	git := repository.GetGit()
	summary := workspaceSummary{
		Repository: repository.GetDisplayName().GetValue(),
		Branch:     git.GetBranch(),
	}
	if summary.Branch == "" && git.GetDetached() {
		// A detached head is a real state with a real consequence — work
		// committed there is easy to lose — so it is named rather than blanked.
		summary.Branch = "detached at " + shortRevision(git.GetHeadRevision())
	}
	summary.WorktreeStatus = worktreeStatusLabel(git)
	return summary
}

// worktreeStatusLabel says what is uncommitted, with a count.
//
// The count is the point: "uncommitted changes" is the same sentence whether
// one file or four hundred are dirty, and those are different decisions.
func worktreeStatusLabel(git *codefluxv1.GitStateView) string {
	if git == nil {
		return ""
	}
	if !git.GetDirty() {
		return "clean"
	}
	count := git.GetChangedPathCount()
	if count == 0 {
		return "uncommitted changes"
	}
	if count == 1 {
		return "1 uncommitted change"
	}
	return strconv.FormatUint(uint64(count), 10) + " uncommitted changes"
}

// applyWorkspaceSummary writes the summary onto a top bar.
func applyWorkspaceSummary(
	bar frontendstate.TopBarView,
	summary workspaceSummary,
) frontendstate.TopBarView {
	if summary.Repository != "" {
		bar.Repository = summary.Repository
	}
	if summary.Branch != "" {
		bar.Branch = summary.Branch
	}
	if summary.WorktreeStatus != "" {
		bar.WorktreeStatus = summary.WorktreeStatus
	}
	return bar
}
