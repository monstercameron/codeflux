package memoryinspector

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// -----------------------------------------------------------------------
// Fixture builders
// -----------------------------------------------------------------------

func mustProjectID(t *testing.T) domain.ProjectID {
	t.Helper()
	value, err := domain.NewProjectID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustRepositoryID(t *testing.T) domain.RepositoryID {
	t.Helper()
	value, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustMemoryArtifactID(t *testing.T) domain.MemoryArtifactID {
	t.Helper()
	value, err := domain.NewMemoryArtifactID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustMemoryArtifactRevisionID(t *testing.T) domain.MemoryArtifactRevisionID {
	t.Helper()
	value, err := domain.NewMemoryArtifactRevisionID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustEpisodeID(t *testing.T) domain.EpisodeID {
	t.Helper()
	value, err := domain.NewEpisodeID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustEvidenceID(t *testing.T) domain.EvidenceID {
	t.Helper()
	value, err := domain.NewEvidenceID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustAtomID(t *testing.T) domain.AtomID {
	t.Helper()
	value, err := domain.NewAtomID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func validRevisionHex() string { return strings.Repeat("a", 40) }

func repositoryFactContent(t *testing.T, repo domain.RepositoryID) domain.MemoryArtifactContent {
	t.Helper()
	content, err := domain.NewRepositoryFactMemoryContent(domain.RepositoryFactContent{
		Repository: repo, Category: domain.RepositoryFactCategoryBuildCommand,
		Statement: "go build ./...",
		Binding:   domain.RevisionBinding{Known: true, ExactRevision: validRevisionHex()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func reviewedCommandContent(t *testing.T, repo domain.RepositoryID) domain.MemoryArtifactContent {
	t.Helper()
	content, err := domain.NewReviewedCommandMemoryContent(domain.ReviewedCommandContent{
		Repository: repo, Purpose: domain.CommandPurposeTest, Argv: []string{"go", "test", "./..."},
		Binding: domain.RevisionBinding{Known: true, ExactRevision: validRevisionHex()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func fileToTestMappingContent(t *testing.T, repo domain.RepositoryID) domain.MemoryArtifactContent {
	t.Helper()
	content, err := domain.NewFileToTestMappingMemoryContent(domain.FileToTestMappingContent{
		Repository: repo, SourcePath: "internal/domain/memory.go", TestPaths: []string{"internal/domain/memory_test.go"},
		Binding: domain.RevisionBinding{Known: true, ExactRevision: validRevisionHex()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func repositoryConventionContent(t *testing.T, repo domain.RepositoryID) domain.MemoryArtifactContent {
	t.Helper()
	content, err := domain.NewRepositoryConventionMemoryContent(domain.RepositoryConventionContent{
		Repository: repo, Scope: "web/frontend", Statement: "Every component takes one Props struct.",
		Binding: domain.RevisionBinding{Known: true, ExactRevision: validRevisionHex()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func acceptedRegressionCaseContent(t *testing.T, repo domain.RepositoryID) domain.MemoryArtifactContent {
	t.Helper()
	content, err := domain.NewAcceptedRegressionCaseMemoryContent(domain.AcceptedRegressionCaseContent{
		Repository: repo, Classification: domain.RegressionClassificationSemantic,
		ReproducibleInput: "task with empty budget", Oracle: "budget validator",
		DemonstratedFailure: mustEvidenceID(t), ExpectedOutcome: "reject with ErrBudgetRequired",
		Binding: domain.RevisionBinding{Known: true, ExactRevision: validRevisionHex()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func executionRecipeContent(t *testing.T, repo domain.RepositoryID) domain.MemoryArtifactContent {
	t.Helper()
	content, err := domain.NewExecutionRecipeMemoryContent(domain.ExecutionRecipeContent{
		Repository: repo, ApplicabilityStatement: "Applies to Go build-command tasks in this repository.",
		PlanSummary: "Run go build then go test ./...", RequiredValidationProfile: "fast",
	})
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func executableAtomReferenceContent(t *testing.T, repo domain.RepositoryID) domain.MemoryArtifactContent {
	t.Helper()
	content, err := domain.NewExecutableAtomReferenceMemoryContent(domain.ExecutableAtomReferenceContent{
		Repository: repo, Atom: mustAtomID(t), CanonicalName: "ValidateTaskBudgetBeforeModelRequest",
		Binding: domain.RevisionBinding{Known: true, ExactRevision: validRevisionHex()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func observationHypothesisContent(t *testing.T, repo domain.RepositoryID) domain.MemoryArtifactContent {
	t.Helper()
	content, err := domain.NewObservationHypothesisMemoryContent(domain.ObservationHypothesisContent{
		Repository: repo, Statement: "Large diffs may correlate with slower review.", Strength: domain.EvidenceStrengthNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	return content
}

// fixture bundles a fully valid, internally consistent Ready-state Props tree
// together with the identities used to build it, so individual tests can
// mutate a shallow copy for one specific behavior.
type fixture struct {
	project    domain.ProjectID
	repository domain.RepositoryID
	artifact   domain.MemoryArtifactID
	revision   domain.MemoryArtifactRevisionID
	ancestorA  domain.MemoryArtifactID
	ancestorB  domain.MemoryArtifactID
	props      Props
}

func fullFixture(t *testing.T) fixture {
	t.Helper()
	project := mustProjectID(t)
	repository := mustRepositoryID(t)
	artifact := mustMemoryArtifactID(t)
	revisionID := mustMemoryArtifactRevisionID(t)
	scope := domain.MemoryProjectScope{Project: project, Repository: repository}

	revision, err := domain.NewMemoryArtifactRevision(artifact, revisionID, scope, repositoryFactContent(t, repository))
	if err != nil {
		t.Fatal(err)
	}

	ancestorA := mustMemoryArtifactID(t)
	ancestorB := mustMemoryArtifactID(t)
	episode := mustEpisodeID(t)
	lineage := domain.MemoryArtifactLineage{
		ArtifactID: artifact, OriginKnown: true, OriginArtifactID: ancestorA,
		DerivedFrom: []domain.MemoryArtifactID{ancestorA}, InfluencedBy: []domain.MemoryArtifactID{ancestorB},
		SupportingEpisodes: []domain.EpisodeID{episode}, SupportingEpisodesKnown: true,
		LineageRootsKnown: true, LineageRootIDs: []domain.MemoryArtifactID{ancestorA},
	}
	if err := lineage.Validate(); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	detail := DetailProps{
		Revision: revision, Lineage: lineage,
		Ancestors: map[domain.MemoryArtifactID]AncestorSummary{
			ancestorA: {ArtifactID: ancestorA, Known: true, Kind: domain.MemoryArtifactKindReviewedCommand, Maturity: domain.MaturityStateValidated},
			ancestorB: {ArtifactID: ancestorB, Known: false, UnknownReason: "ancestor record not migrated"},
		},
		SupportingEpisodes: []EpisodeSummary{
			{Episode: episode, Known: true, OccurredAt: now.Add(-time.Hour), Summary: "Task validated the build command."},
		},
		RetrievalHistory: []RetrievalRecord{
			{Episode: mustEpisodeID(t), Outcome: RetrievalOutcomeInfluencedOutcome, Known: true, OccurredAt: now.Add(-2 * time.Hour), Note: "Used as the build command."},
			{Episode: mustEpisodeID(t), Outcome: RetrievalOutcomeRetrievedOnly, Known: true, OccurredAt: now.Add(-3 * time.Hour), Note: "Surfaced but a different recipe was chosen."},
			{Episode: mustEpisodeID(t), Outcome: RetrievalOutcomeRetrievedAndRejected, Known: true, OccurredAt: now.Add(-4 * time.Hour), Note: "Rejected: stale binding."},
		},
	}
	if err := detail.Validate(); err != nil {
		t.Fatal(err)
	}

	correction := CorrectionProps{
		Prior: revision, CandidateRevisionID: mustMemoryArtifactRevisionID(t), Draft: "go build ./cmd/...",
		OnDraftChange: func(string) {}, OnSubmit: func(domain.MemoryArtifactRevisionID, domain.MemoryArtifactContent) {},
	}
	quarantine := QuarantineProps{
		Revision: revision, Confirmed: false,
		OnConfirmChange: func(bool) {}, OnQuarantine: func(domain.MemoryArtifactID, domain.MemoryArtifactRevisionID) {},
	}
	invalidation := InvalidationProps{
		Revision: revision, Reason: "",
		OnReasonChange: func(string) {}, OnInvalidate: func(domain.MemoryArtifactID, domain.MemoryArtifactRevisionID, string) {},
	}
	// currentLineageIndex is the caller-supplied (server-side) lineage index
	// domain.ValidateMemoryArtifactDeletion recomputes the fresh preview
	// against; the UI never builds this itself. ancestorA.DerivedFrom =
	// [artifact] makes ancestorA a direct semantic dependent of artifact (the
	// deletion target); ancestorB.InfluencedBy = [artifact] makes ancestorB
	// a contextually influenced descendant -- together reproducing exactly
	// the Preview below via domain.PreviewMemoryArtifactDeletion.
	currentLineageIndex := map[domain.MemoryArtifactID]domain.MemoryArtifactLineage{
		artifact: lineage,
		ancestorA: {
			ArtifactID: ancestorA, DerivedFrom: []domain.MemoryArtifactID{artifact},
			OriginKnown: true, OriginArtifactID: artifact,
			SupportingEpisodesKnown: true,
			LineageRootsKnown:       true, LineageRootIDs: []domain.MemoryArtifactID{artifact},
		},
		ancestorB: {
			ArtifactID: ancestorB, InfluencedBy: []domain.MemoryArtifactID{artifact},
			OriginKnown: true, LineageRootsKnown: true, SupportingEpisodesKnown: true,
		},
	}
	deletion := DeletionProps{
		Target: artifact, PreviewState: DeletionPreviewReady,
		Preview: domain.MemoryArtifactDeletionPreview{
			Target: artifact, DirectDependents: []domain.MemoryArtifactID{ancestorA}, ContextuallyInfluenced: []domain.MemoryArtifactID{ancestorB},
		},
		CurrentLineageIndex: currentLineageIndex,
		OnAcknowledgeChange: func([]domain.MemoryArtifactID) {}, OnDelete: func(domain.MemoryArtifactDeletionRequest) {},
	}

	exportRecord, err := domain.NewMemoryArtifactExportRecord(revision, lineage)
	if err != nil {
		t.Fatal(err)
	}

	entryA := ListEntry{
		ArtifactID: artifact, RevisionID: revisionID, Scope: scope,
		Kind: domain.MemoryArtifactKindRepositoryFact, Maturity: domain.MaturityStateCandidate,
		Summary:          "Build command for the module.",
		LastConfirmation: LastConfirmationAttribution{Known: true, At: now.Add(-2 * 24 * time.Hour)},
	}
	entryB := ListEntry{
		ArtifactID: ancestorA, RevisionID: mustMemoryArtifactRevisionID(t), Scope: scope,
		Kind: domain.MemoryArtifactKindReviewedCommand, Maturity: domain.MaturityStateValidated,
		Summary:          "Reviewed test command.",
		LastConfirmation: LastConfirmationAttribution{Known: false, UnknownReason: "never confirmed by evidence"},
	}

	props := Props{
		Mode: primitives.Mode{},
		List: ListProps{
			Entries: []ListEntry{entryA, entryB}, Filter: ListFilter{}, ActiveID: artifact.String(),
			OnSelect: func(domain.MemoryArtifactID) {}, OnFilterChange: func(ListFilter) {},
		},
		Selected: SelectedArtifactProps{
			State: SelectedStateReady, Detail: detail, Correction: correction,
			Quarantine: quarantine, Invalidation: invalidation, Deletion: deletion,
		},
		Export: ExportProps{
			Candidates:       []ExportCandidate{{Record: exportRecord, Selected: false}},
			OnToggleSelect:   func(domain.MemoryArtifactID, bool) {},
			OnExportSelected: func([]domain.MemoryArtifactExportRecord) {},
		},
	}
	if err := props.Validate(); err != nil {
		t.Fatal(err)
	}
	return fixture{project: project, repository: repository, artifact: artifact, revision: revisionID, ancestorA: ancestorA, ancestorB: ancestorB, props: props}
}

func render(t *testing.T, props Props) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.CreateElement(Component, props))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// -----------------------------------------------------------------------
// Rendering tests
// -----------------------------------------------------------------------

func TestComponentRendersListFilterDetailGovernanceAndExportSections(t *testing.T) {
	fx := fullFixture(t)
	markup := render(t, fx.props)

	for _, want := range []string{
		`data-component="project-memory-inspector"`,
		`data-component="project-memory-list-section"`,
		`data-component="project-memory-filters"`,
		`data-component="virtual-list"`,
		`data-artifact-id="` + fx.artifact.String() + `"`,
		"Repository fact", "Reviewed command",
		"Candidate", "Validated",
		`data-component="project-memory-selected"`, `data-state="ready"`,
		"Identity and maturity", "Lineage",
		"Derived from (semantic dependency)", "Influenced by (contextual exposure)",
		fx.ancestorA.String(), fx.ancestorB.String(),
		"Unknown — ancestor artifact record is unavailable: ancestor record not migrated",
		"Supporting episodes", "Task validated the build command.",
		"Bindings", "Exact revision", validRevisionHex(),
		"Applicability predicate", "Unknown — applicability predicate is not modeled for this artifact kind.",
		"Retrieval and influence history",
		"Influenced accepted outcome", "Used as the build command.",
		"Retrieved (not necessarily used)", "Surfaced but a different recipe was chosen.",
		"Retrieved and rejected", "Rejected: stale binding.",
		"Correct this artifact", "A correction always creates a new candidate revision",
		"Quarantine", "terminal for this revision",
		"Invalidate", "Reason for invalidation",
		"Delete", fx.ancestorA.String(),
		"I acknowledge these dependents will be affected",
		"Export selected records", "secret-free by construction",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("markup missing %q\n%s", want, markup)
		}
	}

	// Defect 2 regression guard: a positive-substring check alone cannot
	// catch "unexplained blank" rendering, because the fixture's lineage
	// happens to carry a known, non-empty origin and root set, so the
	// historical bug (a bare "—" for a zero OriginArtifactID, and a
	// textless empty <ul> for empty LineageRootIDs) never fired against this
	// fixture in the first place. Assert the specific blank markup shape the
	// old code emitted is absent.
	for _, unwanted := range []string{
		">—<", // old lineageSection's bare-dash origin placeholder
		`data-inspector-list="roots" data-count="0"></ul>`, // old textless empty roots <ul>
	} {
		if strings.Contains(markup, unwanted) {
			t.Errorf("markup rendered an unexplained blank (%q present):\n%s", unwanted, markup)
		}
	}
}

// TestLineageSectionRendersExplicitCopyForUnknownOriginAndEmptyKnownRoots
// exercises the two Defect 2 cases the composite fixture's known/non-empty
// lineage cannot: OriginKnown == false (must render "Unknown — <reason>",
// never a bare "—") and LineageRootsKnown == true with an empty
// LineageRootIDs (must render explicit "this artifact is the only root"
// copy, never a silently empty list). Mirrors ancestorList's existing
// Known/UnknownReason discipline three lines above it in component.go,
// which AGENTS.md's "Never display unknown cost as zero" requires here too.
func TestLineageSectionRendersExplicitCopyForUnknownOriginAndEmptyKnownRoots(t *testing.T) {
	fx := fullFixture(t)
	props := fx.props
	lineage := props.Selected.Detail.Lineage
	lineage.OriginKnown = false
	lineage.OriginArtifactID = domain.MemoryArtifactID{}
	lineage.OriginUnknownReason = "origin has not been computed for this artifact yet"
	lineage.LineageRootsKnown = true
	lineage.LineageRootIDs = nil
	props.Selected.Detail.Lineage = lineage

	markup := render(t, props)

	for _, want := range []string{
		"Unknown — origin has not been computed for this artifact yet",
		"This artifact is itself the only lineage root.",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("markup missing explicit unknown/empty-state copy %q\n%s", want, markup)
		}
	}
	for _, unwanted := range []string{
		">—<",
		`data-inspector-list="roots" data-count="0"></ul>`,
	} {
		if strings.Contains(markup, unwanted) {
			t.Errorf("markup rendered an unexplained blank (%q present):\n%s", unwanted, markup)
		}
	}
}

func TestComponentRejectsInvalidPropsWithInlineAlertNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("component panicked on invalid props: %v", r)
		}
	}()
	markup := render(t, Props{})
	if !strings.Contains(markup, `data-component="inline-alert"`) || !strings.Contains(markup, `data-tone="failure"`) {
		t.Fatalf("invalid props did not render a failure inline alert:\n%s", markup)
	}
}

func TestSelectedStateNoneLoadingAndUnreadyStatesRenderExplicitCopy(t *testing.T) {
	fx := fullFixture(t)

	none := fx.props
	none.Selected = SelectedArtifactProps{State: SelectedStateNone}
	markup := render(t, none)
	if !strings.Contains(markup, `data-state="none"`) || !strings.Contains(markup, "No artifact selected") {
		t.Fatalf("none state markup missing expected copy:\n%s", markup)
	}

	loading := fx.props
	loading.Selected = SelectedArtifactProps{State: SelectedStateLoading}
	markup = render(t, loading)
	if !strings.Contains(markup, `data-state="loading"`) || !strings.Contains(markup, `data-component="skeleton"`) {
		t.Fatalf("loading state markup missing skeleton:\n%s", markup)
	}

	for _, state := range []SelectedState{
		SelectedStateError, SelectedStateDenied, SelectedStateIncompatible, SelectedStateDisconnected,
	} {
		unready := fx.props
		unready.Selected = SelectedArtifactProps{State: state, ErrorMessage: "diagnostic detail", OnRetry: func() {}}
		markup = render(t, unready)
		if !strings.Contains(markup, `data-state="`+string(state)+`"`) || !strings.Contains(markup, "diagnostic detail") {
			t.Fatalf("state %s markup missing expected copy:\n%s", state, markup)
		}
	}
}

func TestListStatesRenderLoadingErrorAndDisconnected(t *testing.T) {
	fx := fullFixture(t)

	loading := fx.props
	loading.List.State = primitives.VirtualListLoading
	markup := render(t, loading)
	if !strings.Contains(markup, `data-state="loading"`) {
		t.Fatalf("loading list markup missing state marker:\n%s", markup)
	}

	errored := fx.props
	errored.List.State = primitives.VirtualListError
	errored.List.OnRetry = func() {}
	markup = render(t, errored)
	if !strings.Contains(markup, "Could not load memory artifacts") {
		t.Fatalf("error list markup missing expected copy:\n%s", markup)
	}

	disconnected := fx.props
	disconnected.List.State = primitives.VirtualListDisconnected
	disconnected.List.OnRetry = func() {}
	markup = render(t, disconnected)
	if !strings.Contains(markup, "Memory list is offline") {
		t.Fatalf("disconnected list markup missing expected copy:\n%s", markup)
	}
}

func TestDeletionPreviewMustLoadBeforeDeleteControlAppears(t *testing.T) {
	fx := fullFixture(t)

	loading := fx.props
	loading.Selected.Deletion = DeletionProps{Target: fx.artifact, PreviewState: DeletionPreviewLoading}
	markup := render(t, loading)
	if strings.Contains(markup, `id="memory-deletion-submit"`) {
		t.Fatalf("delete control rendered before the preview loaded:\n%s", markup)
	}
	if !strings.Contains(markup, "Loading affected-descendant preview") {
		t.Fatalf("deletion loading copy missing:\n%s", markup)
	}

	failed := fx.props
	failed.Selected.Deletion = DeletionProps{
		Target: fx.artifact, PreviewState: DeletionPreviewError, ErrorMessage: "network timeout", OnRetryPreview: func() {},
	}
	markup = render(t, failed)
	if strings.Contains(markup, `id="memory-deletion-submit"`) {
		t.Fatalf("delete control rendered after a failed preview:\n%s", markup)
	}
	if !strings.Contains(markup, "network timeout") {
		t.Fatalf("deletion error copy missing:\n%s", markup)
	}

	markup = render(t, fx.props)
	if !strings.Contains(markup, `id="memory-deletion-submit"`) {
		t.Fatalf("delete control missing once the preview is ready:\n%s", markup)
	}
}

func TestQuarantineCopyStatesTerminalConsequenceExplicitly(t *testing.T) {
	fx := fullFixture(t)
	markup := render(t, fx.props)
	if !strings.Contains(markup, "terminal for this revision") ||
		!strings.Contains(markup, "cannot be un-quarantined or restored with inherited authority") {
		t.Fatalf("quarantine copy does not explicitly state the terminal consequence:\n%s", markup)
	}

	already := fx.props
	already.Selected.Quarantine.Revision.Maturity = domain.MaturityStateQuarantined
	markup = render(t, already)
	if !strings.Contains(markup, "Quarantine is unavailable") {
		t.Fatalf("an already-quarantined revision did not surface unavailability copy:\n%s", markup)
	}
}

func TestExportSectionExplainsSecretFreeRedactionIsUpstream(t *testing.T) {
	fx := fullFixture(t)
	markup := render(t, fx.props)
	if !strings.Contains(markup, "secret-free by construction") || !strings.Contains(markup, "Redaction happens upstream") {
		t.Fatalf("export section did not state secret-free/upstream-redaction copy:\n%s", markup)
	}

	empty := fx.props
	empty.Export = ExportProps{}
	markup = render(t, empty)
	if !strings.Contains(markup, "No exportable records are available.") {
		t.Fatalf("empty export state missing expected copy:\n%s", markup)
	}
}

// -----------------------------------------------------------------------
// Pure filter tests (M21-091)
// -----------------------------------------------------------------------

func TestApplyListFilterByKindMaturityValidityAndLastConfirmation(t *testing.T) {
	now := time.Now()
	entries := []ListEntry{
		{
			ArtifactID: mustMemoryArtifactID(t), RevisionID: mustMemoryArtifactRevisionID(t),
			Scope: domain.MemoryProjectScope{Project: mustProjectID(t)},
			Kind:  domain.MemoryArtifactKindRepositoryFact, Maturity: domain.MaturityStateValidated,
			Summary: "validated fact", LastConfirmation: LastConfirmationAttribution{Known: true, At: now.Add(-3 * 24 * time.Hour)},
		},
		{
			ArtifactID: mustMemoryArtifactID(t), RevisionID: mustMemoryArtifactRevisionID(t),
			Scope: domain.MemoryProjectScope{Project: mustProjectID(t)},
			Kind:  domain.MemoryArtifactKindReviewedCommand, Maturity: domain.MaturityStateCandidate,
			Summary: "candidate command", LastConfirmation: LastConfirmationAttribution{Known: false, UnknownReason: "never confirmed"},
		},
		{
			ArtifactID: mustMemoryArtifactID(t), RevisionID: mustMemoryArtifactRevisionID(t),
			Scope: domain.MemoryProjectScope{Project: mustProjectID(t)},
			Kind:  domain.MemoryArtifactKindObservationHypothesis, Maturity: domain.MaturityStateQuarantined,
			Summary: "quarantined observation", LastConfirmation: LastConfirmationAttribution{Known: true, At: now.Add(-45 * 24 * time.Hour)},
		},
	}

	byKind := ApplyListFilter(entries, ListFilter{Kinds: []domain.MemoryArtifactKind{domain.MemoryArtifactKindRepositoryFact}}, now)
	if len(byKind) != 1 || byKind[0].Kind != domain.MemoryArtifactKindRepositoryFact {
		t.Fatalf("kind filter = %+v", byKind)
	}

	byMaturity := ApplyListFilter(entries, ListFilter{Maturities: []domain.MaturityState{domain.MaturityStateCandidate}}, now)
	if len(byMaturity) != 1 || byMaturity[0].Maturity != domain.MaturityStateCandidate {
		t.Fatalf("maturity filter = %+v", byMaturity)
	}

	currentlyValid := ApplyListFilter(entries, ListFilter{Validity: ValidityFilterCurrentlyValid}, now)
	if len(currentlyValid) != 1 || currentlyValid[0].Maturity != domain.MaturityStateValidated {
		t.Fatalf("currently-valid filter = %+v", currentlyValid)
	}
	noLongerValid := ApplyListFilter(entries, ListFilter{Validity: ValidityFilterNoLongerValid}, now)
	if len(noLongerValid) != 1 || noLongerValid[0].Maturity != domain.MaturityStateQuarantined {
		t.Fatalf("no-longer-valid filter = %+v", noLongerValid)
	}

	neverConfirmed := ApplyListFilter(entries, ListFilter{LastConfirmation: LastConfirmationFilterNeverConfirmed}, now)
	if len(neverConfirmed) != 1 || neverConfirmed[0].LastConfirmation.Known {
		t.Fatalf("never-confirmed filter = %+v", neverConfirmed)
	}
	olderThan30 := ApplyListFilter(entries, ListFilter{LastConfirmation: LastConfirmationFilterOlderThan30Days}, now)
	if len(olderThan30) != 1 || olderThan30[0].Maturity != domain.MaturityStateQuarantined {
		t.Fatalf("older-than-30-days filter = %+v", olderThan30)
	}
	within7 := ApplyListFilter(entries, ListFilter{LastConfirmation: LastConfirmationFilterWithin7Days}, now)
	if len(within7) != 1 || within7[0].Maturity != domain.MaturityStateValidated {
		t.Fatalf("within-7-days filter = %+v", within7)
	}

	unrestricted := ApplyListFilter(entries, ListFilter{}, now)
	if len(unrestricted) != len(entries) {
		t.Fatalf("zero-value filter should not restrict entries, got %d of %d", len(unrestricted), len(entries))
	}
}

func TestListFilterValidateRejectsUnboundedOrUndeclaredValues(t *testing.T) {
	tooManyKinds := make([]domain.MemoryArtifactKind, MaximumFilterKinds+1)
	for index := range tooManyKinds {
		tooManyKinds[index] = domain.MemoryArtifactKindRepositoryFact
	}
	if err := (ListFilter{Kinds: []domain.MemoryArtifactKind{"not-a-kind"}}).Validate(); !errors.Is(err, ErrInvalidMemoryInspectorProps) {
		t.Fatalf("undeclared kind error = %v", err)
	}
	if err := (ListFilter{Validity: "not-a-validity"}).Validate(); !errors.Is(err, ErrInvalidMemoryInspectorProps) {
		t.Fatalf("undeclared validity error = %v", err)
	}
	if err := (ListFilter{LastConfirmation: "not-a-bucket"}).Validate(); !errors.Is(err, ErrInvalidMemoryInspectorProps) {
		t.Fatalf("undeclared last-confirmation error = %v", err)
	}
}

// -----------------------------------------------------------------------
// Correctable field and revision-binding pure logic tests
// -----------------------------------------------------------------------

func TestCorrectableFieldRoundTripsForEveryArtifactKind(t *testing.T) {
	repo := mustRepositoryID(t)
	contents := map[domain.MemoryArtifactKind]domain.MemoryArtifactContent{
		domain.MemoryArtifactKindRepositoryFact:          repositoryFactContent(t, repo),
		domain.MemoryArtifactKindReviewedCommand:         reviewedCommandContent(t, repo),
		domain.MemoryArtifactKindFileToTestMapping:       fileToTestMappingContent(t, repo),
		domain.MemoryArtifactKindRepositoryConvention:    repositoryConventionContent(t, repo),
		domain.MemoryArtifactKindAcceptedRegressionCase:  acceptedRegressionCaseContent(t, repo),
		domain.MemoryArtifactKindExecutionRecipe:         executionRecipeContent(t, repo),
		domain.MemoryArtifactKindExecutableAtomReference: executableAtomReferenceContent(t, repo),
		domain.MemoryArtifactKindObservationHypothesis:   observationHypothesisContent(t, repo),
	}
	for kind, content := range contents {
		if _, err := CorrectableFieldName(kind); err != nil {
			t.Fatalf("%s: field name = %v", kind, err)
		}
		value, err := CorrectableFieldValue(content)
		if err != nil {
			t.Fatalf("%s: field value = %v", kind, err)
		}
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s: field value was empty", kind)
		}
		corrected, err := ApplyCorrectableField(content, value+" (revised)")
		if err != nil {
			t.Fatalf("%s: apply = %v", kind, err)
		}
		if err := corrected.Validate(); err != nil {
			t.Fatalf("%s: corrected content invalid: %v", kind, err)
		}
		nextValue, err := CorrectableFieldValue(corrected)
		if err != nil {
			t.Fatalf("%s: corrected field value = %v", kind, err)
		}
		if !strings.Contains(nextValue, "(revised)") {
			t.Fatalf("%s: correction did not apply, got %q", kind, nextValue)
		}
	}
}

// TestCorrectableListFieldRoundTripsDelimiterContainingValuesByteForByte
// reproduces the historical Defect 1 (HIGH, silent data corruption):
// CorrectableFieldValue joined Argv/TestPaths with the literal "; " and
// splitCorrectableList split on the same string, so an element that itself
// contained "; " (e.g. Argv = ["--message", "fix bug; retry logic",
// "--dry-run"]) displayed as "--message; fix bug; retry logic; --dry-run"
// and split back into FOUR elements on an unchanged round trip -- an
// argument silently divided in two. TestCorrectableFieldRoundTripsForEvery-
// ArtifactKind above could never catch this: it appends " (revised)" to the
// joined string and never constructs a value containing the delimiter. This
// test asserts the exact opposite of a change: an UNCHANGED draft must round
// trip byte-for-byte back to the original Argv/TestPaths slice.
func TestCorrectableListFieldRoundTripsDelimiterContainingValuesByteForByte(t *testing.T) {
	repo := mustRepositoryID(t)

	reviewedCommand, err := domain.NewReviewedCommandMemoryContent(domain.ReviewedCommandContent{
		Repository: repo, Purpose: domain.CommandPurposeTest,
		Argv:    []string{"--message", "fix bug; retry logic", "--dry-run"},
		Binding: domain.RevisionBinding{Known: true, ExactRevision: validRevisionHex()},
	})
	if err != nil {
		t.Fatal(err)
	}
	fileToTestMapping, err := domain.NewFileToTestMappingMemoryContent(domain.FileToTestMappingContent{
		Repository: repo, SourcePath: "internal/domain/memory.go",
		TestPaths: []string{"internal/domain/memory_test.go", "internal/domain/pkg; legacy_test.go"},
		Binding:   domain.RevisionBinding{Known: true, ExactRevision: validRevisionHex()},
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		content domain.MemoryArtifactContent
		want    []string
		got     func(domain.MemoryArtifactContent) []string
	}{
		{
			name: "reviewed-command-argv", content: reviewedCommand,
			want: []string{"--message", "fix bug; retry logic", "--dry-run"},
			got:  func(content domain.MemoryArtifactContent) []string { return content.ReviewedCommand.Argv },
		},
		{
			name: "file-to-test-mapping-test-paths", content: fileToTestMapping,
			want: []string{"internal/domain/memory_test.go", "internal/domain/pkg; legacy_test.go"},
			got:  func(content domain.MemoryArtifactContent) []string { return content.FileToTestMapping.TestPaths },
		},
	}
	for _, testCase := range cases {
		value, err := CorrectableFieldValue(testCase.content)
		if err != nil {
			t.Fatalf("%s: field value = %v", testCase.name, err)
		}
		// The draft is passed through completely unchanged: this is the
		// round trip a user performs by opening and saving the correction
		// without editing anything.
		corrected, err := ApplyCorrectableField(testCase.content, value)
		if err != nil {
			t.Fatalf("%s: apply unchanged draft = %v", testCase.name, err)
		}
		got := testCase.got(corrected)
		if !slices.Equal(got, testCase.want) {
			t.Fatalf("%s: round trip corrupted a delimiter-containing value: got %#v, want %#v", testCase.name, got, testCase.want)
		}
	}
}

func TestRevisionBindingForAndApplicabilityStatementForByKind(t *testing.T) {
	repo := mustRepositoryID(t)
	if _, ok := RevisionBindingFor(repositoryFactContent(t, repo)); !ok {
		t.Fatal("repository fact should carry a revision binding")
	}
	if _, ok := RevisionBindingFor(executionRecipeContent(t, repo)); ok {
		t.Fatal("execution recipe should not carry a revision binding at this layer")
	}
	if _, ok := RevisionBindingFor(observationHypothesisContent(t, repo)); ok {
		t.Fatal("observation/hypothesis should not carry a revision binding")
	}
	statement, ok := ApplicabilityStatementFor(executionRecipeContent(t, repo))
	if !ok || strings.TrimSpace(statement) == "" {
		t.Fatal("execution recipe should carry an applicability statement")
	}
	if _, ok := ApplicabilityStatementFor(repositoryFactContent(t, repo)); ok {
		t.Fatal("repository fact should not carry an applicability statement")
	}
}

func TestPreviewCorrectionRejectsInvalidDraftAndAcceptsValid(t *testing.T) {
	fx := fullFixture(t)
	props := fx.props.Selected.Correction

	if _, err := PreviewCorrection(props); err != nil {
		t.Fatalf("valid draft preview = %v", err)
	}

	blank := props
	blank.Draft = "   "
	if _, err := PreviewCorrection(blank); err == nil {
		t.Fatal("blank draft should not preview as a valid correction")
	}

	sameRevision := props
	sameRevision.CandidateRevisionID = props.Prior.RevisionID
	if _, err := PreviewCorrection(sameRevision); !errors.Is(err, ErrInvalidMemoryInspectorProps) {
		t.Fatalf("same-revision candidate error = %v", err)
	}
}

func TestListEntryCountSurfacesInMarkup(t *testing.T) {
	fx := fullFixture(t)
	markup := render(t, fx.props)
	if !strings.Contains(markup, `data-result-count="`+strconv.Itoa(len(fx.props.List.Entries))+`"`) {
		t.Fatalf("result count marker missing:\n%s", markup)
	}
}
