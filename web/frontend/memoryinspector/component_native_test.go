//go:build !js

package memoryinspector

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestActivateCorrectionDispatchesOnlyDomainValidatedDrafts(t *testing.T) {
	fx := fullFixture(t)
	props := fx.props.Selected.Correction

	var submittedRevision domain.MemoryArtifactRevisionID
	var submittedContent domain.MemoryArtifactContent
	props.OnSubmit = func(revision domain.MemoryArtifactRevisionID, content domain.MemoryArtifactContent) {
		submittedRevision, submittedContent = revision, content
	}
	if !ActivateCorrection(props) {
		t.Fatal("valid correction was not dispatched")
	}
	if submittedRevision != props.CandidateRevisionID {
		t.Fatalf("submitted revision = %s, want %s", submittedRevision, props.CandidateRevisionID)
	}
	if err := submittedContent.Validate(); err != nil {
		t.Fatalf("submitted content invalid: %v", err)
	}

	busy := props
	busy.Busy = true
	if ActivateCorrection(busy) {
		t.Fatal("busy correction dispatched")
	}

	blankDraft := props
	blankDraft.Draft = "   "
	blankDraft.OnSubmit = func(domain.MemoryArtifactRevisionID, domain.MemoryArtifactContent) {
		t.Fatal("blank draft must never dispatch")
	}
	if ActivateCorrection(blankDraft) {
		t.Fatal("blank draft correction dispatched")
	}
}

func TestActivateQuarantineRequiresConfirmationAndLegalTransition(t *testing.T) {
	fx := fullFixture(t)
	props := fx.props.Selected.Quarantine

	unconfirmed := props
	unconfirmed.Confirmed = false
	unconfirmed.OnQuarantine = func(domain.MemoryArtifactID, domain.MemoryArtifactRevisionID) {
		t.Fatal("unconfirmed quarantine must never dispatch")
	}
	if ActivateQuarantine(unconfirmed) {
		t.Fatal("unconfirmed quarantine dispatched")
	}

	var quarantinedArtifact domain.MemoryArtifactID
	var quarantinedRevision domain.MemoryArtifactRevisionID
	confirmed := props
	confirmed.Confirmed = true
	confirmed.OnQuarantine = func(artifact domain.MemoryArtifactID, revision domain.MemoryArtifactRevisionID) {
		quarantinedArtifact, quarantinedRevision = artifact, revision
	}
	if !ActivateQuarantine(confirmed) {
		t.Fatal("confirmed, legal quarantine was not dispatched")
	}
	if quarantinedArtifact != props.Revision.ArtifactID || quarantinedRevision != props.Revision.RevisionID {
		t.Fatalf("quarantine dispatched wrong identity: %s / %s", quarantinedArtifact, quarantinedRevision)
	}

	// Quarantine is authority-terminal: an already-quarantined revision has no
	// transition back into Quarantined in the declared table, so a second
	// quarantine attempt on the same maturity must never dispatch.
	alreadyQuarantined := props
	alreadyQuarantined.Revision.Maturity = domain.MaturityStateQuarantined
	alreadyQuarantined.Confirmed = true
	alreadyQuarantined.OnQuarantine = func(domain.MemoryArtifactID, domain.MemoryArtifactRevisionID) {
		t.Fatal("re-quarantining an already-quarantined revision must never dispatch")
	}
	if ActivateQuarantine(alreadyQuarantined) {
		t.Fatal("already-quarantined revision dispatched quarantine again")
	}

	// Retired is terminal with no outgoing transitions at all.
	retired := props
	retired.Revision.Maturity = domain.MaturityStateRetired
	retired.Confirmed = true
	retired.OnQuarantine = func(domain.MemoryArtifactID, domain.MemoryArtifactRevisionID) {
		t.Fatal("retired revision must never dispatch quarantine")
	}
	if ActivateQuarantine(retired) {
		t.Fatal("retired revision dispatched quarantine")
	}
}

func TestActivateInvalidationRequiresNonEmptyReasonAndLegalTransition(t *testing.T) {
	fx := fullFixture(t)
	props := fx.props.Selected.Invalidation

	blank := props
	blank.Reason = "   "
	blank.OnInvalidate = func(domain.MemoryArtifactID, domain.MemoryArtifactRevisionID, string) {
		t.Fatal("blank reason must never dispatch")
	}
	if ActivateInvalidation(blank) {
		t.Fatal("blank-reason invalidation dispatched")
	}

	var reportedReason string
	valid := props
	valid.Reason = "  superseded by a corrected build command  "
	valid.OnInvalidate = func(_ domain.MemoryArtifactID, _ domain.MemoryArtifactRevisionID, reason string) {
		reportedReason = reason
	}
	if !ActivateInvalidation(valid) {
		t.Fatal("valid invalidation was not dispatched")
	}
	if reportedReason != "superseded by a corrected build command" {
		t.Fatalf("dispatched reason = %q, want trimmed reason", reportedReason)
	}
}

func TestActivateDeletionRequiresReadyPreviewAndAcknowledgedDependents(t *testing.T) {
	fx := fullFixture(t)
	props := fx.props.Selected.Deletion // has DirectDependents populated

	notAcknowledged := props
	notAcknowledged.AcknowledgedDependents = nil
	notAcknowledged.OnDelete = func(domain.MemoryArtifactDeletionRequest) {
		t.Fatal("unacknowledged direct dependents must never dispatch deletion")
	}
	if ActivateDeletion(notAcknowledged) {
		t.Fatal("deletion dispatched without acknowledging direct dependents")
	}

	// Acknowledging a set that names fewer or different identities than the
	// freshly recomputed dependents must never dispatch: acknowledgement is
	// bound to the exact identities the domain layer recomputes, not a bare
	// "yes" a caller could set regardless of what was actually shown.
	partiallyAcknowledged := props
	partiallyAcknowledged.AcknowledgedDependents = []domain.MemoryArtifactID{}
	partiallyAcknowledged.OnDelete = func(domain.MemoryArtifactDeletionRequest) {
		t.Fatal("a partial/empty acknowledgement must never dispatch deletion")
	}
	if ActivateDeletion(partiallyAcknowledged) {
		t.Fatal("deletion dispatched with a partial acknowledgement")
	}

	loading := props
	loading.PreviewState = DeletionPreviewLoading
	loading.AcknowledgedDependents = append([]domain.MemoryArtifactID(nil), props.Preview.DirectDependents...)
	loading.OnDelete = func(domain.MemoryArtifactDeletionRequest) {
		t.Fatal("deletion must never dispatch before the preview is ready")
	}
	if ActivateDeletion(loading) {
		t.Fatal("deletion dispatched while the preview was still loading")
	}

	var dispatched domain.MemoryArtifactDeletionRequest
	ready := props
	ready.AcknowledgedDependents = append([]domain.MemoryArtifactID(nil), props.Preview.DirectDependents...)
	ready.OnDelete = func(request domain.MemoryArtifactDeletionRequest) { dispatched = request }
	if !ActivateDeletion(ready) {
		t.Fatal("acknowledged, ready deletion was not dispatched")
	}
	if dispatched.Target != props.Target || !sameArtifactIDSet(dispatched.AcknowledgedDependents, props.Preview.DirectDependents) {
		t.Fatalf("dispatched deletion request = %+v", dispatched)
	}
}

// TestActivateDeletionRejectsStalePreviewAgainstCurrentLineageIndex
// reproduces the staleness gap ValidateMemoryArtifactDeletion(request,
// currentIndex) closes: domain.MemoryArtifactDeletionRequest.Preview is
// caller-supplied and could be stale (fetched before the lineage graph
// changed) or forged (hand-built to under-report dependents). It never
// constructs a mismatch between Preview and what CurrentLineageIndex would
// currently recompute, so this test never exercised that rejection path
// before -- it dispatches deletion with a Preview claiming no dependents at
// all while CurrentLineageIndex (fx.props' fixture graph) still shows
// ancestorA/ancestorB related to the target, and asserts ActivateDeletion
// refuses to call OnDelete.
func TestActivateDeletionRejectsStalePreviewAgainstCurrentLineageIndex(t *testing.T) {
	fx := fullFixture(t)
	props := fx.props.Selected.Deletion

	stale := props
	stale.Preview = domain.MemoryArtifactDeletionPreview{Target: props.Target} // stale/forged: claims zero dependents
	stale.AcknowledgedDependents = nil                                         // matches the (wrong) empty preview shown
	stale.OnDelete = func(domain.MemoryArtifactDeletionRequest) {
		t.Fatal("a preview that mismatches the current lineage index must never dispatch deletion")
	}
	if ActivateDeletion(stale) {
		t.Fatal("stale preview deletion dispatched despite mismatching the current lineage index")
	}
}

func TestActivateExportDispatchesOnlySelectedRecords(t *testing.T) {
	fx := fullFixture(t)
	props := fx.props.Export

	none := props
	none.OnExportSelected = func([]domain.MemoryArtifactExportRecord) {
		t.Fatal("export must never dispatch with nothing selected")
	}
	if ActivateExport(none) {
		t.Fatal("export dispatched with no selection")
	}

	selected := props
	selected.Candidates = append([]ExportCandidate(nil), props.Candidates...)
	selected.Candidates[0].Selected = true
	var exported []domain.MemoryArtifactExportRecord
	selected.OnExportSelected = func(records []domain.MemoryArtifactExportRecord) { exported = records }
	if !ActivateExport(selected) {
		t.Fatal("export with a selection was not dispatched")
	}
	if len(exported) != 1 || exported[0].ArtifactID != selected.Candidates[0].Record.ArtifactID {
		t.Fatalf("exported records = %+v", exported)
	}
}

func TestTransitionAvailabilityMatchesDeclaredMaturityTable(t *testing.T) {
	if err := TransitionAvailability(domain.MaturityStateCandidate, domain.MaturityStateQuarantined); err != nil {
		t.Fatalf("candidate -> quarantined should be legal: %v", err)
	}
	if err := TransitionAvailability(domain.MaturityStateQuarantined, domain.MaturityStateCandidate); err == nil {
		t.Fatal("quarantined -> candidate must be illegal: quarantine is authority-terminal")
	}
	if err := TransitionAvailability(domain.MaturityStateQuarantined, domain.MaturityStateValidated); err == nil {
		t.Fatal("quarantined -> validated must be illegal")
	}
	if err := TransitionAvailability(domain.MaturityStateRetired, domain.MaturityStateInvalidated); err == nil {
		t.Fatal("retired is terminal and must have no outgoing transition")
	}
}
