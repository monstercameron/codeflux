package main

import (
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
)

func TestTheTopBarSaysWhatTheWorkingTreeSays(t *testing.T) {
	// The top bar used to read "codeflux / main / uncommitted changes" from a
	// fixture: right by coincidence on one machine, wrong everywhere else, and
	// still right-looking with the coordinator disconnected.
	summary := projectWorkspaceSummary(&codefluxv1.InspectRepositoryResponse{
		Repository: &codefluxv1.RepositorySummary{
			DisplayName: &codefluxv1.RedactedText{Value: "orders-service"},
			Git: &codefluxv1.GitStateView{
				Branch: "release/4.2", Dirty: true, ChangedPathCount: 4,
			},
		},
	})
	if summary.Repository != "orders-service" || summary.Branch != "release/4.2" {
		t.Errorf("summary = %+v, want the coordinator's answer", summary)
	}
	// The count is the point: one dirty file and four hundred are different
	// decisions, and "uncommitted changes" is the same sentence for both.
	if summary.WorktreeStatus != "4 uncommitted changes" {
		t.Errorf("worktree status = %q, want the count", summary.WorktreeStatus)
	}
}

func TestTheWorktreeStatusIsCountedInPlainWords(t *testing.T) {
	for name, expected := range map[string]struct {
		git  *codefluxv1.GitStateView
		want string
	}{
		"clean":           {&codefluxv1.GitStateView{}, "clean"},
		"one change":      {&codefluxv1.GitStateView{Dirty: true, ChangedPathCount: 1}, "1 uncommitted change"},
		"several changes": {&codefluxv1.GitStateView{Dirty: true, ChangedPathCount: 12}, "12 uncommitted changes"},
		"dirty but uncounted": {
			&codefluxv1.GitStateView{Dirty: true}, "uncommitted changes",
		},
		"no answer at all": {nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := worktreeStatusLabel(expected.git); got != expected.want {
				t.Errorf("status = %q, want %q", got, expected.want)
			}
		})
	}
}

func TestADetachedHeadIsNamedRatherThanBlanked(t *testing.T) {
	// A detached head is a real state with a real consequence — work committed
	// there is easy to lose — so it is said rather than left as an empty branch.
	summary := projectWorkspaceSummary(&codefluxv1.InspectRepositoryResponse{
		Repository: &codefluxv1.RepositorySummary{
			Git: &codefluxv1.GitStateView{
				Detached: true, HeadRevision: strings.Repeat("f", 40),
			},
		},
	})
	if !strings.Contains(summary.Branch, "detached") {
		t.Errorf("branch = %q, want it to say the head is detached", summary.Branch)
	}
}

func TestAnUnansweredFieldDoesNotOverwriteWhatIsKnown(t *testing.T) {
	// A field Git declined to answer must not blank a field that was answered,
	// or a momentary failure would erase the top bar.
	known := frontendstate.TopBarView{
		Repository: "orders-service", Branch: "main", WorktreeStatus: "clean",
	}
	after := applyWorkspaceSummary(known, workspaceSummary{})
	if after != known {
		t.Errorf("an empty summary changed the top bar:\nbefore %+v\nafter  %+v", known, after)
	}
}
