package shell_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/shell"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func repositoryRows() []shell.RepositoryRow {
	return []shell.RepositoryRow{
		{
			RepositoryID: "repo_current", Name: "demo", RecordedRevision: "adc7b5c",
			Revision: 3, Current: true, Accessible: true,
			ThreadPath: "/workspace/repo_current/thread/thread_one",
		},
		{
			RepositoryID: "repo_other", Name: "archive", RecordedRevision: "",
			Revision: 1, Accessible: true,
		},
	}
}

// TestRepositoryWorkspaceNamesWhichRepositoryIsOpen checks the page answers the
// question it exists for: which repositories the coordinator recorded, and
// which one this session is actually working in.
func TestRepositoryWorkspaceNamesWhichRepositoryIsOpen(t *testing.T) {
	markup := render(t, ui.CreateElement(shell.RepositoryWorkspaceShell, shell.RepositoryWorkspaceProps{
		Tokens: tokens(t), State: shell.SurfaceReady, Rows: repositoryRows(),
		SelectedID: "repo_current", OnSelect: func(string) {},
	}))
	for _, want := range []string{
		`data-component="repository-chooser-shell"`,
		`data-repository-count="2"`,
		`data-component="repository-row"`,
		`data-repository-id="repo_current"`,
		"2 repositories · demo is open",
		"In this session",
		"recorded at adc7b5c",
		"Entry revision",
		"no recorded revision",
		"No thread",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("repository markup missing %q", want)
		}
	}
}

// TestRepositoryWorkspaceReportsAnUnreadTreeAsUnknown keeps "Git did not
// answer" from rendering as a clean tree. Clean is the claim somebody starts an
// agent on the strength of, so it may never be a default.
func TestRepositoryWorkspaceReportsAnUnreadTreeAsUnknown(t *testing.T) {
	unknown := render(t, ui.CreateElement(shell.RepositoryWorkspaceShell, shell.RepositoryWorkspaceProps{
		Tokens: tokens(t), State: shell.SurfaceReady, Rows: repositoryRows(),
		SelectedID: "repo_current", InspectionState: shell.SurfaceReady,
		Inspection: &shell.RepositoryInspection{}, OnSelect: func(string) {},
	}))
	if !strings.Contains(unknown, "unknown rather than clean") || strings.Contains(unknown, ">Clean<") {
		t.Errorf("unknown working tree markup = %s", unknown)
	}
	dirty := render(t, ui.CreateElement(shell.RepositoryWorkspaceShell, shell.RepositoryWorkspaceProps{
		Tokens: tokens(t), State: shell.SurfaceReady, Rows: repositoryRows(),
		SelectedID: "repo_current", InspectionState: shell.SurfaceReady,
		Inspection: &shell.RepositoryInspection{
			WorkingTreeKnown: true, Branch: "master", HeadRevision: "adc7b5c1122",
			Dirty: true, ChangedPathCount: 4,
			Warnings: []string{"This repository has no recorded Git identity."},
		},
		OnSelect: func(string) {},
	}))
	for _, want := range []string{"master", "4 uncommitted changes", "no recorded Git identity"} {
		if !strings.Contains(dirty, want) {
			t.Errorf("dirty working tree markup missing %q", want)
		}
	}
}

// TestRepositoryWorkspaceOffersNoEntryWithoutAThread holds the page to actions
// that work: a repository with no open thread has no workspace to enter, and a
// link that leads nowhere invites a click and answers with silence.
func TestRepositoryWorkspaceOffersNoEntryWithoutAThread(t *testing.T) {
	markup := render(t, ui.CreateElement(shell.RepositoryWorkspaceShell, shell.RepositoryWorkspaceProps{
		Tokens: tokens(t), State: shell.SurfaceReady, Rows: repositoryRows(),
		SelectedID: "repo_other", InspectionState: shell.SurfaceReady,
		Inspection: &shell.RepositoryInspection{WorkingTreeKnown: true, Branch: "main"},
		OnSelect:   func(string) {},
	}))
	if strings.Contains(markup, "Open the workspace") {
		t.Errorf("markup offers entry into a repository with no thread: %s", markup)
	}
	if !strings.Contains(markup, "no workspace to enter yet") {
		t.Errorf("markup does not explain why there is no entry: %s", markup)
	}
	if !strings.Contains(markup, "codeflux start --repository") {
		t.Errorf("markup does not say how a repository is recorded: %s", markup)
	}
}

// TestRepositoryWorkspaceSeparatesEmptyFromUnreachable keeps a coordinator
// with no repository distinct from one that could not be asked.
func TestRepositoryWorkspaceSeparatesEmptyFromUnreachable(t *testing.T) {
	empty := render(t, ui.CreateElement(shell.RepositoryWorkspaceShell, shell.RepositoryWorkspaceProps{
		Tokens: tokens(t), State: shell.SurfaceReady,
	}))
	if !strings.Contains(empty, "No repository recorded") {
		t.Errorf("empty markup = %s", empty)
	}
	failed := render(t, ui.CreateElement(shell.RepositoryWorkspaceShell, shell.RepositoryWorkspaceProps{
		Tokens: tokens(t), State: shell.SurfaceFailed,
		ErrorMessage: "the coordinator closed the connection", OnRetry: func() {},
	}))
	if !strings.Contains(failed, "the coordinator closed the connection") ||
		strings.Contains(failed, "No repository recorded") {
		t.Errorf("failed markup = %s", failed)
	}
}
