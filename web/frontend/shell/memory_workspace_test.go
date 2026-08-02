package shell_test

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/shell"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func memoryRows() []shell.MemoryArtifactRow {
	return []shell.MemoryArtifactRow{
		{
			ArtifactID: "memory-artifact-1", RevisionID: "memory-artifact-revision-1",
			Kind: domain.MemoryArtifactKindRepositoryFact, Maturity: domain.MaturityStateValidated,
			Summary: "The test suite is run with go test ./...", RevisionNumber: 2,
			CreatedAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), CreatedFromCorrection: true,
		},
		{
			ArtifactID: "memory-artifact-2", RevisionID: "memory-artifact-revision-2",
			Kind: domain.MemoryArtifactKindReviewedCommand, Maturity: domain.MaturityStateCandidate,
			Summary: "go build ./...", RevisionNumber: 1,
		},
	}
}

// TestMemoryWorkspaceListsWhatTheProjectLearned checks the surface reports the
// claim, its kind, and how far it has been governed -- the three facts a
// reader needs before trusting anything the agent does with it.
func TestMemoryWorkspaceListsWhatTheProjectLearned(t *testing.T) {
	markup := render(t, ui.CreateElement(shell.MemoryWorkspaceShell, shell.MemoryWorkspaceProps{
		Tokens: tokens(t), State: shell.SurfaceReady, Rows: memoryRows(),
		OnSelect: func(string) {},
	}))
	for _, want := range []string{
		`data-component="memory-shell"`,
		`data-state="ready"`,
		`data-artifact-count="2"`,
		`data-component="memory-row"`,
		`data-artifact-id="memory-artifact-1"`,
		`data-maturity="validated"`,
		"The test suite is run with go test ./...",
		"user-corrected",
		"2 artifacts",
		"1 carry authority",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("memory markup missing %q", want)
		}
	}
}

// TestMemoryWorkspaceSeparatesEmptyFromUnreadable keeps "this project has
// learned nothing yet" distinct from "memory could not be read": only one of
// those is a problem, and an empty list would say both.
func TestMemoryWorkspaceSeparatesEmptyFromUnreadable(t *testing.T) {
	empty := render(t, ui.CreateElement(shell.MemoryWorkspaceShell, shell.MemoryWorkspaceProps{
		Tokens: tokens(t), State: shell.SurfaceReady,
	}))
	if !strings.Contains(empty, "Nothing learned yet") {
		t.Errorf("empty memory markup = %s", empty)
	}
	failed := render(t, ui.CreateElement(shell.MemoryWorkspaceShell, shell.MemoryWorkspaceProps{
		Tokens: tokens(t), State: shell.SurfaceFailed,
		ErrorMessage: "the coordinator refused the read", OnRetry: func() {},
	}))
	if !strings.Contains(failed, "the coordinator refused the read") ||
		!strings.Contains(failed, "Try again") ||
		strings.Contains(failed, "Nothing learned yet") {
		t.Errorf("failed memory markup = %s", failed)
	}
}

// TestMemoryWorkspaceDetailReportsUnresolvedProvenanceAsUnresolved keeps an
// undetermined origin from reading as "no origin": those are different claims
// and only one of them is true.
func TestMemoryWorkspaceDetailReportsUnresolvedProvenanceAsUnresolved(t *testing.T) {
	rows := memoryRows()
	detail := shell.MemoryArtifactDetail{
		Row: rows[0],
		Fields: []shell.MemoryDetailField{
			{Label: "Statement", Value: "The test suite is run with go test ./..."},
		},
		OriginUnknownReason:  "the derivation chain has not been walked",
		SupersedesRevisionID: "memory-artifact-revision-0",
		BindingState:         "Bound to revision abc123",
	}
	markup := render(t, ui.CreateElement(shell.MemoryWorkspaceShell, shell.MemoryWorkspaceProps{
		Tokens: tokens(t), State: shell.SurfaceReady, Rows: rows,
		SelectedID: rows[0].ArtifactID, DetailState: shell.SurfaceReady, Detail: &detail,
		OnSelect: func(string) {},
	}))
	for _, want := range []string{
		`data-component="memory-detail"`,
		"Not yet determined: the derivation chain has not been walked",
		"Bound to revision abc123",
		"memory-artifact-revision-0",
		"A person&#39;s correction",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("detail markup missing %q: %s", want, markup)
		}
	}
}

// TestMemoryWorkspaceDoesNotOfferGovernanceItCannotPerform holds the surface
// to what the coordinator actually exposes: reading memory. A correct-looking
// delete control that no service backs would be a lie the reader can click.
func TestMemoryWorkspaceDoesNotOfferGovernanceItCannotPerform(t *testing.T) {
	rows := memoryRows()
	detail := shell.MemoryArtifactDetail{Row: rows[0]}
	markup := render(t, ui.CreateElement(shell.MemoryWorkspaceShell, shell.MemoryWorkspaceProps{
		Tokens: tokens(t), State: shell.SurfaceReady, Rows: rows,
		SelectedID: rows[0].ArtifactID, DetailState: shell.SurfaceReady, Detail: &detail,
		OnSelect: func(string) {},
	}))
	for _, forbidden := range []string{"Quarantine", "Invalidate", "Delete artifact"} {
		if strings.Contains(markup, forbidden) {
			t.Errorf("memory markup offers unbacked action %q", forbidden)
		}
	}
	if !strings.Contains(markup, "is not available from this console yet") {
		t.Errorf("memory markup does not say governance is unavailable: %s", markup)
	}
}
