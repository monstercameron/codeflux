package domain

import "strings"

// Chronological Episodes (Milestone 21, M21-029..039)
//
// An episode is the immutable chronological record of one completed or
// terminated task (docs/plan.md §0 "Episode"): intent, plan, actions,
// interventions, validation, decision, time, cost, and outcome. Per §31
// "Chronological Episodes", it records the versioned task fingerprint,
// starting and ending revisions, proposed and accepted changes, compiler/
// evaluator/verifier/test/runtime results, retrieved artifacts, reuse and
// rejection decisions, human decisions, and cost and timing evidence.
//
// M21-029 (BLOCKER) fixes the episode boundary everything else hangs off:
// an episode opens the moment its task is received and its starting
// revision is selected (NewEpisode, always status Open), and it closes
// exactly once, the moment a terminal user decision is recorded (accepted,
// rejected, abandoned, or unresolved -- EpisodeClosure, M21-037). Closing is
// a one-way transition (episodeTransitions has no edge out of Closed): once
// closed, the episode is frozen (M21-038; see
// ValidateEpisodeStatusTransition and the storage-layer mirror,
// episodes_immutable_after_close, in the M21-029..039 migration). Later
// correction never mutates a closed episode; it is expressed only as an
// invalidation overlay layered on top (M21-039; EpisodeInvalidationOverlay,
// ValidateEpisodeInvalidationOverlay), per AGENTS.md "Preserve immutable
// historical records; represent later correction through revisions or
// invalidation overlays."
//
// Per §31, "Agent explanations are stored only as agent_self_report with
// evidence_strength: none. They are not treated as causal accounts." This
// package intentionally defines no separate policy for that rule inside
// episode.go: SupportingEvidenceRecord.Validate (memory.go) already pins
// agent-self-report evidence to EvidenceStrengthNone everywhere it is used,
// including any evidence an episode's facts reference.

// -----------------------------------------------------------------------
// Episode status and outcome (M21-029, M21-037, M21-038)
// -----------------------------------------------------------------------

// EpisodeStatus describes whether an episode is still accumulating facts
// (Open) or has reached its terminal, frozen state (Closed).
type EpisodeStatus string

const (
	EpisodeStatusOpen   EpisodeStatus = "open"
	EpisodeStatusClosed EpisodeStatus = "closed"
)

var allEpisodeStatuses = []EpisodeStatus{EpisodeStatusOpen, EpisodeStatusClosed}

// IsValid reports whether the episode status is declared.
func (value EpisodeStatus) IsValid() bool {
	return containsState(allEpisodeStatuses, value)
}

// episodeTransitions declares the entire episode lifecycle: Open -> Closed
// and nothing else. There is deliberately no edge out of Closed (M21-038):
// once an episode reaches its terminal user decision, no further status
// transition -- not even back to Open -- is ever valid again.
var episodeTransitions = transitions(
	[2]EpisodeStatus{EpisodeStatusOpen, EpisodeStatusClosed},
)

// ValidateEpisodeStatusTransition validates one episode status transition.
// It is the domain-level mirror of the storage-layer freeze trigger
// (episodes_immutable_after_close): both refuse every transition out of
// Closed.
func ValidateEpisodeStatusTransition(from, to EpisodeStatus) error {
	return validateTransition("episode", from, to, EpisodeStatus.IsValid, episodeTransitions)
}

// EpisodeOutcome classifies the terminal user decision an episode closed
// with (M21-037). It is required on every closed episode and forbidden on
// every open one (see EpisodeClosure.Validate).
type EpisodeOutcome string

const (
	EpisodeOutcomeAccepted   EpisodeOutcome = "accepted"
	EpisodeOutcomeRejected   EpisodeOutcome = "rejected"
	EpisodeOutcomeAbandoned  EpisodeOutcome = "abandoned"
	EpisodeOutcomeUnresolved EpisodeOutcome = "unresolved"
)

var allEpisodeOutcomes = []EpisodeOutcome{
	EpisodeOutcomeAccepted, EpisodeOutcomeRejected, EpisodeOutcomeAbandoned, EpisodeOutcomeUnresolved,
}

// AllEpisodeOutcomes returns the complete declared outcome set.
func AllEpisodeOutcomes() []EpisodeOutcome {
	return append([]EpisodeOutcome(nil), allEpisodeOutcomes...)
}

// IsValid reports whether the episode outcome is declared.
func (value EpisodeOutcome) IsValid() bool {
	return containsState(allEpisodeOutcomes, value)
}

// -----------------------------------------------------------------------
// Episode identity and boundary (M21-029)
// -----------------------------------------------------------------------

// MaximumEpisodeRevisionFieldBytes bounds the free-form starting/ending
// revision identifier strings, matching the sibling bound already declared
// for memory-artifact revision bindings (RevisionBinding's
// MaximumRevisionFieldBytes) and internal/fingerprint's own binding-field
// bound: an uncapped revision string would be an outlier among identity
// fields that are otherwise all explicitly capped.
const MaximumEpisodeRevisionFieldBytes = 255

// NewEpisode carries the facts fixed at episode start (M21-029): the task
// this episode covers, its versioned task fingerprint (SchemaVersion/Hash
// from internal/fingerprint.ExactFingerprint.Hash -- this package does not
// import internal/fingerprint itself, so the schema version and hash arrive
// as the caller's already-computed plain values, matching how
// memory_artifact_revisions carries a plain content_sha256 without this
// package depending on the hashing package that produced it), and the
// repository revision the episode started from. One episode covers exactly
// one completed or terminated task (docs/plan.md §0 "Episode"): Task
// therefore identifies the episode as much as its own EpisodeID does.
type NewEpisode struct {
	ID                       EpisodeID
	Project                  ProjectID
	Repository               RepositoryID
	Task                     TaskID
	FingerprintSchemaVersion uint32
	FingerprintHash          string
	StartingRevision         string
}

// Validate checks that every fact fixed at episode start is present and
// well-formed.
func (value NewEpisode) Validate() error {
	switch {
	case value.ID.IsZero():
		return valueError("episode.id", "must not be empty")
	case value.Project.IsZero():
		return valueError("episode.project", "must not be empty")
	case value.Repository.IsZero():
		return valueError("episode.repository", "must not be empty")
	case value.Task.IsZero():
		return valueError("episode.task", "must not be empty")
	case value.FingerprintSchemaVersion == 0:
		return valueError("episode.fingerprint_schema_version", "must not be zero")
	case !isLowercaseHexSHA256(value.FingerprintHash):
		return valueError("episode.fingerprint_hash", "must be a lowercase hex sha256 digest")
	case strings.TrimSpace(value.StartingRevision) != value.StartingRevision || value.StartingRevision == "":
		return valueError("episode.starting_revision", "must not be empty")
	case len(value.StartingRevision) > MaximumEpisodeRevisionFieldBytes:
		return valueError("episode.starting_revision", "must not exceed 255 bytes")
	default:
		return nil
	}
}

// EpisodeClosure carries the facts fixed at an episode's terminal user
// decision (M21-034, M21-037): the ending repository revision and the
// accepted/rejected/abandoned/unresolved outcome. Closing an episode is
// what freezes it (M21-038); see ValidateEpisodeStatusTransition.
type EpisodeClosure struct {
	EndingRevision string
	Outcome        EpisodeOutcome
}

// Validate checks that every fact required to close an episode is present
// and well-formed.
func (value EpisodeClosure) Validate() error {
	switch {
	case strings.TrimSpace(value.EndingRevision) != value.EndingRevision || value.EndingRevision == "":
		return valueError("episode.closure.ending_revision", "must not be empty")
	case len(value.EndingRevision) > MaximumEpisodeRevisionFieldBytes:
		return valueError("episode.closure.ending_revision", "must not exceed 255 bytes")
	case !value.Outcome.IsValid():
		return valueError("episode.closure.outcome", "is not declared")
	default:
		return nil
	}
}

func isLowercaseHexSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// -----------------------------------------------------------------------
// Episode fact references (M21-030..036)
// -----------------------------------------------------------------------

// EpisodeFactKind classifies one fact an episode references. Per §31 and
// the M21-032 direction that generalizes to every fact an episode records,
// an episode never copies another table's payload; it records only a
// reference to the already-durable row that carries it (a revision number
// on a (task_id, revision)-keyed table, an opaque row ID, or both).
type EpisodeFactKind string

const (
	// EpisodeFactKindRequirementRevision references a
	// task_requirement_revisions row (M21-030 requirement).
	EpisodeFactKindRequirementRevision EpisodeFactKind = "requirement-revision"
	// EpisodeFactKindPlanRevisionProposed references an agent_plan_revisions
	// row that was proposed (M21-030 plan).
	EpisodeFactKindPlanRevisionProposed EpisodeFactKind = "plan-revision-proposed"
	// EpisodeFactKindPlanRevisionAccepted references an agent_plan_revisions
	// row that was accepted (M21-030 plan).
	EpisodeFactKindPlanRevisionAccepted EpisodeFactKind = "plan-revision-accepted"
	// EpisodeFactKindRepositoryContextRevision references the repository
	// revision observed at one point in the episode (M21-031).
	EpisodeFactKindRepositoryContextRevision EpisodeFactKind = "repository-context-revision"
	// EpisodeFactKindUserIntervention references a redirect, correction, or
	// other non-approval user intervention (M21-033).
	EpisodeFactKindUserIntervention EpisodeFactKind = "user-intervention"
	// EpisodeFactKindUserApproval references an approvals row (M21-033).
	EpisodeFactKindUserApproval EpisodeFactKind = "user-approval"
	// EpisodeFactKindValidationResult references a validation_run_results (or
	// equivalent) row (M21-034).
	EpisodeFactKindValidationResult EpisodeFactKind = "validation-result"
	// EpisodeFactKindFinalDecision references an acceptance_decisions row
	// (M21-034).
	EpisodeFactKindFinalDecision EpisodeFactKind = "final-decision"
	// EpisodeFactKindForecast references an effort_forecast_revisions row
	// (M21-035).
	EpisodeFactKindForecast EpisodeFactKind = "forecast"
	// EpisodeFactKindActualMetrics references a forecast_outcomes row
	// (M21-035).
	EpisodeFactKindActualMetrics EpisodeFactKind = "actual-metrics"
	// EpisodeFactKindPreRepairFailure references the failed validation that
	// preceded a repair (M21-036). Per the task direction, "the pre-repair
	// failure is the evidence; do not let a later repair erase it": this
	// fact must be recorded before any EpisodeFactKindRepairAttempt fact for
	// the same episode (see the storage-layer
	// episode_fact_references_repair_requires_prior_failure trigger).
	EpisodeFactKindPreRepairFailure EpisodeFactKind = "pre-repair-failure"
	// EpisodeFactKindRepairAttempt references a repair_attempts row
	// (M21-036).
	EpisodeFactKindRepairAttempt EpisodeFactKind = "repair-attempt"
)

var allEpisodeFactKinds = []EpisodeFactKind{
	EpisodeFactKindRequirementRevision,
	EpisodeFactKindPlanRevisionProposed,
	EpisodeFactKindPlanRevisionAccepted,
	EpisodeFactKindRepositoryContextRevision,
	EpisodeFactKindUserIntervention,
	EpisodeFactKindUserApproval,
	EpisodeFactKindValidationResult,
	EpisodeFactKindFinalDecision,
	EpisodeFactKindForecast,
	EpisodeFactKindActualMetrics,
	EpisodeFactKindPreRepairFailure,
	EpisodeFactKindRepairAttempt,
}

// AllEpisodeFactKinds returns the complete declared fact-kind set.
func AllEpisodeFactKinds() []EpisodeFactKind {
	return append([]EpisodeFactKind(nil), allEpisodeFactKinds...)
}

// IsValid reports whether the fact kind is declared.
func (value EpisodeFactKind) IsValid() bool {
	return containsState(allEpisodeFactKinds, value)
}

// EpisodeFactReference is one reference from an episode to an already
// durable fact recorded elsewhere: a revision number on a
// (task_id, revision)-keyed table (Revision), an opaque row identity
// (ReferenceID), or both. At least one of Revision/ReferenceID must be
// present; NewEpisodeFactReference never accepts a bare kind with nothing
// to point at, so an episode can never claim to reference a fact without
// actually naming which row.
type EpisodeFactReference struct {
	Kind        EpisodeFactKind
	Revision    *uint64
	ReferenceID string
}

// Validate checks that the fact reference names a declared kind and points
// at a real row identity.
func (value EpisodeFactReference) Validate() error {
	if !value.Kind.IsValid() {
		return valueError("episode.fact_reference.kind", "is not declared")
	}
	if value.Revision == nil && strings.TrimSpace(value.ReferenceID) == "" {
		return valueError("episode.fact_reference", "must reference a revision number, a row identity, or both")
	}
	if value.Revision != nil && *value.Revision == 0 {
		return valueError("episode.fact_reference.revision", "must not be zero")
	}
	if value.ReferenceID != "" {
		if strings.TrimSpace(value.ReferenceID) != value.ReferenceID {
			return valueError("episode.fact_reference.reference_id", "must not have leading or trailing whitespace")
		}
		if len(value.ReferenceID) > 255 {
			return valueError("episode.fact_reference.reference_id", "must not exceed 255 bytes")
		}
	}
	return nil
}

// -----------------------------------------------------------------------
// Invalidation overlays (M21-039)
// -----------------------------------------------------------------------

// EpisodeInvalidationOverlayKind classifies what aspect of a closed
// episode's history a later overlay concerns.
type EpisodeInvalidationOverlayKind string

const (
	EpisodeInvalidationOverlayKindOutcomeInvalidated    EpisodeInvalidationOverlayKind = "outcome-invalidated"
	EpisodeInvalidationOverlayKindEvidenceInvalidated   EpisodeInvalidationOverlayKind = "evidence-invalidated"
	EpisodeInvalidationOverlayKindFingerprintSuperseded EpisodeInvalidationOverlayKind = "fingerprint-superseded"
	EpisodeInvalidationOverlayKindOther                 EpisodeInvalidationOverlayKind = "other"
)

var allEpisodeInvalidationOverlayKinds = []EpisodeInvalidationOverlayKind{
	EpisodeInvalidationOverlayKindOutcomeInvalidated,
	EpisodeInvalidationOverlayKindEvidenceInvalidated,
	EpisodeInvalidationOverlayKindFingerprintSuperseded,
	EpisodeInvalidationOverlayKindOther,
}

// IsValid reports whether the overlay kind is declared.
func (value EpisodeInvalidationOverlayKind) IsValid() bool {
	return containsState(allEpisodeInvalidationOverlayKinds, value)
}

// EpisodeCurrentAssessment is the mutable-overlay "what is currently known"
// judgment layered on top of an episode's immutable historical facts, per
// §31 "Historical Claim Invalidation": "Immutable revisions retain what was
// asserted at the time, but a mutable claim-status overlay records what is
// currently known."
type EpisodeCurrentAssessment string

const (
	EpisodeCurrentAssessmentReliable   EpisodeCurrentAssessment = "reliable"
	EpisodeCurrentAssessmentUnreliable EpisodeCurrentAssessment = "unreliable"
	EpisodeCurrentAssessmentSuperseded EpisodeCurrentAssessment = "superseded"
)

var allEpisodeCurrentAssessments = []EpisodeCurrentAssessment{
	EpisodeCurrentAssessmentReliable, EpisodeCurrentAssessmentUnreliable, EpisodeCurrentAssessmentSuperseded,
}

// IsValid reports whether the current assessment is declared.
func (value EpisodeCurrentAssessment) IsValid() bool {
	return containsState(allEpisodeCurrentAssessments, value)
}

// EpisodeInvalidationOverlay is one append-only correction layered on top of
// a closed episode (M21-039). It never mutates the episode's frozen row; it
// is always a new fact recorded alongside it. ReplacementEpisode, when
// non-zero, names an independently derived successor episode, mirroring
// §31 Artifact Failure Protocol: "An independently derived successor
// receives a new identity ... and fresh admission requirements."
type EpisodeInvalidationOverlay struct {
	Kind               EpisodeInvalidationOverlayKind
	Assessment         EpisodeCurrentAssessment
	ReasonRedacted     string
	ReplacementEpisode EpisodeID
}

// Validate checks that the overlay is well-formed.
func (value EpisodeInvalidationOverlay) Validate() error {
	switch {
	case !value.Kind.IsValid():
		return valueError("episode.invalidation_overlay.kind", "is not declared")
	case !value.Assessment.IsValid():
		return valueError("episode.invalidation_overlay.assessment", "is not declared")
	case strings.TrimSpace(value.ReasonRedacted) == "":
		return valueError("episode.invalidation_overlay.reason_redacted", "must not be empty")
	case len(value.ReasonRedacted) > 8192:
		return valueError("episode.invalidation_overlay.reason_redacted", "must not exceed 8192 bytes")
	default:
		return nil
	}
}

// ValidateEpisodeInvalidationOverlay checks that an overlay may be recorded
// against an episode currently in status. Per M21-039, an overlay only ever
// corrects a closed (frozen) episode's history -- there is nothing yet to
// invalidate about one still open -- so this refuses every status other
// than Closed, mirroring the storage-layer
// episode_invalidation_overlays_requires_closed_episode trigger.
func ValidateEpisodeInvalidationOverlay(status EpisodeStatus, overlay EpisodeInvalidationOverlay) error {
	if !status.IsValid() {
		return valueError("episode.status", "is not declared")
	}
	if status != EpisodeStatusClosed {
		return &TransitionError{
			Machine: "episode invalidation overlay",
			From:    string(status),
			To:      "overlaid",
			Cause:   ErrUnknownState,
		}
	}
	return overlay.Validate()
}
