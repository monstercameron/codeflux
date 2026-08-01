// Package memoryinspector renders the project-memory Inspection and
// Correction UI (M21-090..099): a bounded list of governed memory artifacts,
// their lineage and history, and typed callback seams for correction,
// quarantine, invalidation, deletion, and secret-free export. It never mutates
// domain state itself and never talks to transport; every mutation is a
// caller-owned callback validated against internal/domain first.
package memoryinspector

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/domain"
	frontendi18n "codeflux.dev/codeflux/web/frontend/i18n"
	"codeflux.dev/codeflux/web/frontend/primitives"
)

const (
	// MaximumListEntries bounds the project-memory list before virtualization
	// even considers rendering it, per AGENTS.md "Avoid unbounded ... graph
	// queries, and database reads" applied to the frontend list contract.
	MaximumListEntries = 2000
	// MaximumSummaryBytes bounds every short descriptive text field rendered
	// in this package (list-row summaries, unknown reasons, error messages).
	MaximumSummaryBytes = 512
	// MaximumIdentityListItems bounds lineage ancestor lookups and supporting
	// episode lists.
	MaximumIdentityListItems = 64
	// MaximumEpisodeSummaryBytes bounds one supporting-episode's descriptive text.
	MaximumEpisodeSummaryBytes = 512
	// MaximumRetrievalHistoryItems bounds the retrieval/influence history list.
	MaximumRetrievalHistoryItems = 256
	// MaximumCorrectionDraftBytes bounds the single free-text correction field.
	MaximumCorrectionDraftBytes = 4096
	// MaximumInvalidationReasonBytes bounds the required invalidation reason.
	MaximumInvalidationReasonBytes = 1024
	// MaximumExportCandidates bounds one export batch; export is a deliberate,
	// scoped action, not a full-corpus dump.
	MaximumExportCandidates = 200
	// MaximumFilterKinds and MaximumFilterMaturities bound the multi-select
	// filter sets to the declared domain enum sizes.
	MaximumFilterKinds      = 8
	MaximumFilterMaturities = 6

	// DefaultListHeight and DefaultListRowHeight size the virtualized list
	// when a caller supplies no explicit viewport geometry.
	DefaultListHeight    = 480
	DefaultListRowHeight = 56

	// correctableListLineDelimiter joins/splits the one-per-line editing
	// surface for list-shaped correctable fields (Argv, TestPaths). Unlike
	// the historical "; " delimiter, a newline cannot appear inside a single
	// Argv or TestPaths element (both are single-line shell/path values), so
	// joining and splitting on it can never silently fuse or split a value
	// that legitimately contains "; ".
	correctableListLineDelimiter = "\n"

	// MaximumLineageIndexEntries bounds DeletionProps.CurrentLineageIndex,
	// per AGENTS.md "Avoid unbounded ... database reads" applied to the
	// caller-supplied lineage index a deletion is validated against.
	MaximumLineageIndexEntries = MaximumListEntries
)

// ErrInvalidMemoryInspectorProps classifies any structurally invalid prop
// tree passed to this package's render functions.
var ErrInvalidMemoryInspectorProps = errors.New("invalid project memory inspector props")

// ErrUnsupportedCorrectionKind classifies an artifact kind without a defined
// primary correctable field.
var ErrUnsupportedCorrectionKind = errors.New("memory inspector: artifact kind has no primary correctable field")

func fieldError(field, reason string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidMemoryInspectorProps, field, reason)
}

func validateRequiredText(field, value string, maximum int) error {
	if strings.TrimSpace(value) == "" {
		return fieldError(field, "must not be empty")
	}
	return validateText(field, value, maximum)
}

func validateOptionalText(field, value string, maximum int) error {
	if value == "" {
		return nil
	}
	return validateText(field, value, maximum)
}

func validateText(field, value string, maximum int) error {
	if strings.TrimSpace(value) != value || !utf8.ValidString(value) || len(value) > maximum {
		return fieldError(field, "must be trimmed, valid UTF-8, and bounded")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fieldError(field, "must not contain control characters")
		}
	}
	return nil
}

// -----------------------------------------------------------------------
// List (M21-090, M21-091)
// -----------------------------------------------------------------------

// LastConfirmationAttribution is the Known/UnknownReason-disciplined record of
// when a memory artifact's authority was last confirmed by evidence. No
// domain field for this exists yet (the storage lane is still landing), so
// this projection is owned by the presentation layer until it does.
type LastConfirmationAttribution struct {
	Known         bool
	At            time.Time
	UnknownReason string
}

// Validate checks internal consistency of the known/unknown attribution.
func (value LastConfirmationAttribution) Validate() error {
	if !value.Known {
		if !value.At.IsZero() {
			return fieldError("last_confirmation", "unknown attribution must not carry a timestamp")
		}
		return validateOptionalText("last_confirmation.unknown_reason", value.UnknownReason, MaximumSummaryBytes)
	}
	if value.UnknownReason != "" {
		return fieldError("last_confirmation.unknown_reason", "must be empty when known")
	}
	if value.At.IsZero() {
		return fieldError("last_confirmation", "known attribution requires a non-zero timestamp")
	}
	return nil
}

// ListEntry is one summarized row in the project-memory list.
type ListEntry struct {
	ArtifactID            domain.MemoryArtifactID
	RevisionID            domain.MemoryArtifactRevisionID
	Scope                 domain.MemoryProjectScope
	Kind                  domain.MemoryArtifactKind
	Maturity              domain.MaturityState
	Summary               string
	LastConfirmation      LastConfirmationAttribution
	CreatedFromCorrection bool
}

// Validate checks required identity, declared enums, scope, and bounded text.
func (entry ListEntry) Validate() error {
	switch {
	case entry.ArtifactID.IsZero():
		return fieldError("list_entry.artifact_id", "must not be empty")
	case entry.RevisionID.IsZero():
		return fieldError("list_entry.revision_id", "must not be empty")
	case !entry.Kind.IsValid():
		return fieldError("list_entry.kind", "is not declared")
	case !entry.Maturity.IsValid():
		return fieldError("list_entry.maturity", "is not declared")
	}
	if err := entry.Scope.Validate(); err != nil {
		return err
	}
	if err := validateRequiredText("list_entry.summary", entry.Summary, MaximumSummaryBytes); err != nil {
		return err
	}
	return entry.LastConfirmation.Validate()
}

// ValidityFilter buckets a maturity state by governed authority, derived from
// domain.MaturityState.IsAuthorityBearing rather than an independently
// invented notion of validity.
type ValidityFilter string

const (
	ValidityFilterAny             ValidityFilter = "any"
	ValidityFilterCurrentlyValid  ValidityFilter = "currently-valid"
	ValidityFilterNotYetValidated ValidityFilter = "not-yet-validated"
	ValidityFilterNoLongerValid   ValidityFilter = "no-longer-valid"
)

// IsValid reports whether the validity filter is declared.
func (filter ValidityFilter) IsValid() bool {
	switch filter {
	case "", ValidityFilterAny, ValidityFilterCurrentlyValid, ValidityFilterNotYetValidated, ValidityFilterNoLongerValid:
		return true
	default:
		return false
	}
}

func (filter ValidityFilter) normalized() ValidityFilter {
	if filter == "" {
		return ValidityFilterAny
	}
	return filter
}

func (filter ValidityFilter) matches(maturity domain.MaturityState) bool {
	switch filter.normalized() {
	case ValidityFilterAny:
		return true
	case ValidityFilterCurrentlyValid:
		return maturity.IsAuthorityBearing()
	case ValidityFilterNotYetValidated:
		return maturity == domain.MaturityStateCandidate
	case ValidityFilterNoLongerValid:
		return maturity == domain.MaturityStateQuarantined ||
			maturity == domain.MaturityStateInvalidated ||
			maturity == domain.MaturityStateRetired
	default:
		return false
	}
}

// LastConfirmationFilter buckets entries by LastConfirmationAttribution.
type LastConfirmationFilter string

const (
	LastConfirmationFilterAny             LastConfirmationFilter = "any"
	LastConfirmationFilterWithin7Days     LastConfirmationFilter = "within-7-days"
	LastConfirmationFilterWithin30Days    LastConfirmationFilter = "within-30-days"
	LastConfirmationFilterOlderThan30Days LastConfirmationFilter = "older-than-30-days"
	LastConfirmationFilterNeverConfirmed  LastConfirmationFilter = "never-confirmed"
)

// IsValid reports whether the last-confirmation filter is declared.
func (filter LastConfirmationFilter) IsValid() bool {
	switch filter {
	case "", LastConfirmationFilterAny, LastConfirmationFilterWithin7Days, LastConfirmationFilterWithin30Days,
		LastConfirmationFilterOlderThan30Days, LastConfirmationFilterNeverConfirmed:
		return true
	default:
		return false
	}
}

func (filter LastConfirmationFilter) normalized() LastConfirmationFilter {
	if filter == "" {
		return LastConfirmationFilterAny
	}
	return filter
}

func (filter LastConfirmationFilter) matches(attribution LastConfirmationAttribution, now time.Time) bool {
	switch filter.normalized() {
	case LastConfirmationFilterAny:
		return true
	case LastConfirmationFilterNeverConfirmed:
		return !attribution.Known
	case LastConfirmationFilterWithin7Days, LastConfirmationFilterWithin30Days, LastConfirmationFilterOlderThan30Days:
		if !attribution.Known {
			return false
		}
		elapsed := now.Sub(attribution.At)
		if elapsed < 0 {
			elapsed = 0
		}
		switch filter.normalized() {
		case LastConfirmationFilterWithin7Days:
			return elapsed <= 7*24*time.Hour
		case LastConfirmationFilterWithin30Days:
			return elapsed <= 30*24*time.Hour
		default:
			return elapsed > 30*24*time.Hour
		}
	default:
		return false
	}
}

// ListFilter is the pure M21-091 filter over Entries: type, maturity,
// validity, and last confirmation. Empty Kinds or Maturities mean "no
// restriction," matching the rest of the package's Known/empty conventions.
type ListFilter struct {
	Kinds            []domain.MemoryArtifactKind
	Maturities       []domain.MaturityState
	Validity         ValidityFilter
	LastConfirmation LastConfirmationFilter
}

// Validate checks bounded, declared, deduplicated filter values.
func (filter ListFilter) Validate() error {
	if len(filter.Kinds) > MaximumFilterKinds {
		return fieldError("filter.kinds", "exceeds the bounded filter set")
	}
	seenKinds := make(map[domain.MemoryArtifactKind]struct{}, len(filter.Kinds))
	for _, kind := range filter.Kinds {
		if !kind.IsValid() {
			return fieldError("filter.kinds", "contains an undeclared kind")
		}
		if _, duplicate := seenKinds[kind]; duplicate {
			return fieldError("filter.kinds", "contains a duplicate kind")
		}
		seenKinds[kind] = struct{}{}
	}
	if len(filter.Maturities) > MaximumFilterMaturities {
		return fieldError("filter.maturities", "exceeds the bounded filter set")
	}
	seenMaturities := make(map[domain.MaturityState]struct{}, len(filter.Maturities))
	for _, maturity := range filter.Maturities {
		if !maturity.IsValid() {
			return fieldError("filter.maturities", "contains an undeclared maturity")
		}
		if _, duplicate := seenMaturities[maturity]; duplicate {
			return fieldError("filter.maturities", "contains a duplicate maturity")
		}
		seenMaturities[maturity] = struct{}{}
	}
	if !filter.Validity.IsValid() {
		return fieldError("filter.validity", "is not declared")
	}
	if !filter.LastConfirmation.IsValid() {
		return fieldError("filter.last_confirmation", "is not declared")
	}
	return nil
}

func (filter ListFilter) matchesKind(kind domain.MemoryArtifactKind) bool {
	if len(filter.Kinds) == 0 {
		return true
	}
	for _, candidate := range filter.Kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

func (filter ListFilter) matchesMaturity(maturity domain.MaturityState) bool {
	if len(filter.Maturities) == 0 {
		return true
	}
	for _, candidate := range filter.Maturities {
		if candidate == maturity {
			return true
		}
	}
	return false
}

// ApplyListFilter returns the entries matching filter at now, preserving
// input order. It is a pure function so filtering behavior is unit-testable
// independent of rendering.
func ApplyListFilter(entries []ListEntry, filter ListFilter, now time.Time) []ListEntry {
	result := make([]ListEntry, 0, len(entries))
	for _, entry := range entries {
		if !filter.matchesKind(entry.Kind) {
			continue
		}
		if !filter.matchesMaturity(entry.Maturity) {
			continue
		}
		if !filter.Validity.matches(entry.Maturity) {
			continue
		}
		if !filter.LastConfirmation.matches(entry.LastConfirmation, now) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

// ListProps configures the M21-090/M21-091 project-memory list.
type ListProps struct {
	State     primitives.VirtualListState
	Entries   []ListEntry
	Filter    ListFilter
	ActiveID  string
	Height    float64
	RowHeight float64

	OnFilterChange func(ListFilter)
	OnSelect       func(domain.MemoryArtifactID)
	OnRetry        func()
}

// Validate checks bounded, unique, individually valid entries, a declared
// virtual-list state, a valid filter, and a selection callback whenever
// entries are present to select.
func (props ListProps) Validate() error {
	switch props.State {
	case "", primitives.VirtualListReady, primitives.VirtualListLoading,
		primitives.VirtualListError, primitives.VirtualListDisconnected:
	default:
		return fieldError("list.state", "is not declared")
	}
	if len(props.Entries) > MaximumListEntries {
		return fieldError("list.entries", "exceeds the bounded list limit")
	}
	seen := make(map[domain.MemoryArtifactID]struct{}, len(props.Entries))
	for _, entry := range props.Entries {
		if err := entry.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[entry.ArtifactID]; duplicate {
			return fieldError("list.entries", "contains a duplicate artifact identity")
		}
		seen[entry.ArtifactID] = struct{}{}
	}
	if err := props.Filter.Validate(); err != nil {
		return err
	}
	if len(props.Entries) > 0 && props.OnSelect == nil {
		return fieldError("list.on_select", "is required when entries are present")
	}
	if (props.State == primitives.VirtualListError || props.State == primitives.VirtualListDisconnected) && props.OnRetry == nil {
		return fieldError("list.on_retry", "is required for a recoverable list state")
	}
	return nil
}

// -----------------------------------------------------------------------
// Lineage, supporting episodes, bindings, and retrieval/influence history
// (M21-092, M21-093, M21-094)
// -----------------------------------------------------------------------

// AncestorSummary enriches one lineage ancestor identity for display.
type AncestorSummary struct {
	ArtifactID    domain.MemoryArtifactID
	Known         bool
	UnknownReason string
	Kind          domain.MemoryArtifactKind
	Maturity      domain.MaturityState
}

// Validate checks the known/unknown discipline and declared enums.
func (value AncestorSummary) Validate() error {
	if value.ArtifactID.IsZero() {
		return fieldError("ancestor.artifact_id", "must not be empty")
	}
	if !value.Known {
		if value.Kind != "" || value.Maturity != "" {
			return fieldError("ancestor", "unknown ancestor must not carry kind or maturity")
		}
		return validateOptionalText("ancestor.unknown_reason", value.UnknownReason, MaximumSummaryBytes)
	}
	if value.UnknownReason != "" {
		return fieldError("ancestor.unknown_reason", "must be empty when known")
	}
	if !value.Kind.IsValid() {
		return fieldError("ancestor.kind", "is not declared")
	}
	if !value.Maturity.IsValid() {
		return fieldError("ancestor.maturity", "is not declared")
	}
	return nil
}

// EpisodeSummary enriches one supporting episode identity for display.
type EpisodeSummary struct {
	Episode       domain.EpisodeID
	Known         bool
	UnknownReason string
	OccurredAt    time.Time
	Summary       string
}

// Validate checks the known/unknown discipline and bounded summary text.
func (value EpisodeSummary) Validate() error {
	if value.Episode.IsZero() {
		return fieldError("episode.episode_id", "must not be empty")
	}
	if !value.Known {
		if !value.OccurredAt.IsZero() || value.Summary != "" {
			return fieldError("episode", "unknown episode must not carry occurrence or summary")
		}
		return validateOptionalText("episode.unknown_reason", value.UnknownReason, MaximumSummaryBytes)
	}
	if value.UnknownReason != "" {
		return fieldError("episode.unknown_reason", "must be empty when known")
	}
	if value.OccurredAt.IsZero() {
		return fieldError("episode.occurred_at", "known episode requires a non-zero timestamp")
	}
	return validateRequiredText("episode.summary", value.Summary, MaximumEpisodeSummaryBytes)
}

// RetrievalOutcome distinguishes "was retrieved" from "actually influenced
// the accepted outcome," per §31: causation is investigated, not assumed from
// retrieval alone. No domain type for this exists yet (the retrieval/vector
// lane, M21-070s, has not landed structured records), so this projection is
// owned by the presentation layer until it does.
type RetrievalOutcome string

const (
	RetrievalOutcomeInfluencedOutcome    RetrievalOutcome = "influenced-outcome"
	RetrievalOutcomeRetrievedOnly        RetrievalOutcome = "retrieved-only"
	RetrievalOutcomeRetrievedAndRejected RetrievalOutcome = "retrieved-and-rejected"
)

// IsValid reports whether the retrieval outcome is declared.
func (outcome RetrievalOutcome) IsValid() bool {
	switch outcome {
	case RetrievalOutcomeInfluencedOutcome, RetrievalOutcomeRetrievedOnly, RetrievalOutcomeRetrievedAndRejected:
		return true
	default:
		return false
	}
}

// RetrievalRecord is one chronological retrieval/influence event referencing
// the supporting episode it occurred in.
type RetrievalRecord struct {
	Episode       domain.EpisodeID
	Outcome       RetrievalOutcome
	Known         bool
	OccurredAt    time.Time
	UnknownReason string
	Note          string
}

// Validate checks the known/unknown discipline, declared outcome, and
// bounded note text.
func (value RetrievalRecord) Validate() error {
	if value.Episode.IsZero() {
		return fieldError("retrieval.episode_id", "must not be empty")
	}
	if !value.Outcome.IsValid() {
		return fieldError("retrieval.outcome", "is not declared")
	}
	if !value.Known {
		if !value.OccurredAt.IsZero() || value.Note != "" {
			return fieldError("retrieval", "unknown retrieval must not carry occurrence or note")
		}
		return validateOptionalText("retrieval.unknown_reason", value.UnknownReason, MaximumSummaryBytes)
	}
	if value.UnknownReason != "" {
		return fieldError("retrieval.unknown_reason", "must be empty when known")
	}
	if value.OccurredAt.IsZero() {
		return fieldError("retrieval.occurred_at", "known retrieval requires a non-zero timestamp")
	}
	return validateOptionalText("retrieval.note", value.Note, MaximumSummaryBytes)
}

// DetailProps configures the M21-092/M21-093/M21-094 detail surface for one
// selected artifact revision.
type DetailProps struct {
	Revision           domain.MemoryArtifactRevision
	Lineage            domain.MemoryArtifactLineage
	Ancestors          map[domain.MemoryArtifactID]AncestorSummary
	SupportingEpisodes []EpisodeSummary
	RetrievalHistory   []RetrievalRecord
}

// Validate checks the revision, that lineage and revision describe the same
// artifact, bounded ancestor and episode lookups consistent with Lineage, and
// a bounded retrieval history.
func (props DetailProps) Validate() error {
	if props.Revision.ArtifactID.IsZero() || props.Revision.RevisionID.IsZero() {
		return fieldError("detail.revision", "must reference a real artifact revision")
	}
	if err := props.Revision.Scope.Validate(); err != nil {
		return err
	}
	if !props.Revision.Maturity.IsValid() {
		return fieldError("detail.revision.maturity", "is not declared")
	}
	if err := props.Revision.Content.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMemoryInspectorProps, err)
	}
	if props.Lineage.ArtifactID != props.Revision.ArtifactID {
		return fieldError("detail.lineage", "must describe the same artifact as revision")
	}
	if err := props.Lineage.Validate(); err != nil {
		return err
	}
	if len(props.Ancestors) > MaximumIdentityListItems*2 {
		return fieldError("detail.ancestors", "exceeds the bounded lookup size")
	}
	for id, summary := range props.Ancestors {
		if summary.ArtifactID != id {
			return fieldError("detail.ancestors", "lookup key must match ancestor artifact id")
		}
		if err := summary.Validate(); err != nil {
			return err
		}
	}
	if len(props.SupportingEpisodes) > MaximumIdentityListItems {
		return fieldError("detail.supporting_episodes", "exceeds the bounded list limit")
	}
	supporting := make(map[domain.EpisodeID]struct{}, len(props.SupportingEpisodes))
	for _, episode := range props.SupportingEpisodes {
		if err := episode.Validate(); err != nil {
			return err
		}
		supporting[episode.Episode] = struct{}{}
	}
	if len(supporting) != len(props.Lineage.SupportingEpisodes) {
		return fieldError("detail.supporting_episodes", "must describe exactly the lineage's supporting episodes")
	}
	for _, id := range props.Lineage.SupportingEpisodes {
		if _, ok := supporting[id]; !ok {
			return fieldError("detail.supporting_episodes", "must describe exactly the lineage's supporting episodes")
		}
	}
	if len(props.RetrievalHistory) > MaximumRetrievalHistoryItems {
		return fieldError("detail.retrieval_history", "exceeds the bounded list limit")
	}
	for _, record := range props.RetrievalHistory {
		if err := record.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// RevisionBindingFor returns the exact revision binding carried by content's
// fact-shaped payload. ExecutionRecipeContent and ObservationHypothesisContent
// carry no RevisionBinding field at this typed-value layer, so ok is false
// for those two kinds.
func RevisionBindingFor(content domain.MemoryArtifactContent) (binding domain.RevisionBinding, ok bool) {
	switch content.Kind {
	case domain.MemoryArtifactKindRepositoryFact:
		if content.RepositoryFact == nil {
			return domain.RevisionBinding{}, false
		}
		return content.RepositoryFact.Binding, true
	case domain.MemoryArtifactKindReviewedCommand:
		if content.ReviewedCommand == nil {
			return domain.RevisionBinding{}, false
		}
		return content.ReviewedCommand.Binding, true
	case domain.MemoryArtifactKindFileToTestMapping:
		if content.FileToTestMapping == nil {
			return domain.RevisionBinding{}, false
		}
		return content.FileToTestMapping.Binding, true
	case domain.MemoryArtifactKindRepositoryConvention:
		if content.RepositoryConvention == nil {
			return domain.RevisionBinding{}, false
		}
		return content.RepositoryConvention.Binding, true
	case domain.MemoryArtifactKindAcceptedRegressionCase:
		if content.AcceptedRegressionCase == nil {
			return domain.RevisionBinding{}, false
		}
		return content.AcceptedRegressionCase.Binding, true
	case domain.MemoryArtifactKindExecutableAtomReference:
		if content.AtomReference == nil {
			return domain.RevisionBinding{}, false
		}
		return content.AtomReference.Binding, true
	default:
		return domain.RevisionBinding{}, false
	}
}

// ApplicabilityStatementFor returns the descriptive applicability statement
// carried only by ExecutionRecipeContent at this typed-value layer.
// Structured, machine-checkable applicability predicates are a later lane
// (§31, M21-022); every other artifact kind carries none, so ok is false.
func ApplicabilityStatementFor(content domain.MemoryArtifactContent) (statement string, ok bool) {
	if content.Kind == domain.MemoryArtifactKindExecutionRecipe && content.ExecutionRecipe != nil {
		return content.ExecutionRecipe.ApplicabilityStatement, true
	}
	return "", false
}

// -----------------------------------------------------------------------
// Correction by new revision (M21-095)
// -----------------------------------------------------------------------

// CorrectableFieldName identifies which field of content's typed payload is
// exposed as the one free-text field user correction can target at this
// presentation layer. Building a full multi-field editor for all eight kinds
// is deferred until a real correction need is measured; today's UI corrects
// the single field most likely to need a human fix per kind.
func CorrectableFieldName(kind domain.MemoryArtifactKind) (string, error) {
	switch kind {
	case domain.MemoryArtifactKindRepositoryFact:
		return "statement", nil
	case domain.MemoryArtifactKindReviewedCommand:
		return "argv", nil
	case domain.MemoryArtifactKindFileToTestMapping:
		return "test_paths", nil
	case domain.MemoryArtifactKindRepositoryConvention:
		return "statement", nil
	case domain.MemoryArtifactKindAcceptedRegressionCase:
		return "expected_outcome", nil
	case domain.MemoryArtifactKindExecutionRecipe:
		return "plan_summary", nil
	case domain.MemoryArtifactKindExecutableAtomReference:
		return "canonical_name", nil
	case domain.MemoryArtifactKindObservationHypothesis:
		return "statement", nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedCorrectionKind, kind)
	}
}

// correctableFieldIsList reports whether kind's one correctable field
// (CorrectableFieldName) is list-shaped (Argv, TestPaths) rather than a
// single scalar string. List-shaped fields are edited one value per line
// rather than in a single-line text field, per the fix for the historical
// "; "-join round-trip defect: joining/splitting on a fixed delimiter
// corrupted any element that itself contained that delimiter.
func correctableFieldIsList(kind domain.MemoryArtifactKind) bool {
	switch kind {
	case domain.MemoryArtifactKindReviewedCommand, domain.MemoryArtifactKindFileToTestMapping:
		return true
	default:
		return false
	}
}

// CorrectableFieldValue returns the current text of content's one
// correctable field, joining list-shaped fields (Argv, TestPaths) one value
// per line (see correctableListLineDelimiter) so the round trip through
// ApplyCorrectableField/splitCorrectableList can never corrupt a value that
// contains the historical "; " delimiter: a newline cannot appear inside a
// single Argv or TestPaths element, so no escaping rule is needed.
func CorrectableFieldValue(content domain.MemoryArtifactContent) (string, error) {
	switch content.Kind {
	case domain.MemoryArtifactKindRepositoryFact:
		if content.RepositoryFact != nil {
			return content.RepositoryFact.Statement, nil
		}
	case domain.MemoryArtifactKindReviewedCommand:
		if content.ReviewedCommand != nil {
			return strings.Join(content.ReviewedCommand.Argv, correctableListLineDelimiter), nil
		}
	case domain.MemoryArtifactKindFileToTestMapping:
		if content.FileToTestMapping != nil {
			return strings.Join(content.FileToTestMapping.TestPaths, correctableListLineDelimiter), nil
		}
	case domain.MemoryArtifactKindRepositoryConvention:
		if content.RepositoryConvention != nil {
			return content.RepositoryConvention.Statement, nil
		}
	case domain.MemoryArtifactKindAcceptedRegressionCase:
		if content.AcceptedRegressionCase != nil {
			return content.AcceptedRegressionCase.ExpectedOutcome, nil
		}
	case domain.MemoryArtifactKindExecutionRecipe:
		if content.ExecutionRecipe != nil {
			return content.ExecutionRecipe.PlanSummary, nil
		}
	case domain.MemoryArtifactKindExecutableAtomReference:
		if content.AtomReference != nil {
			return content.AtomReference.CanonicalName, nil
		}
	case domain.MemoryArtifactKindObservationHypothesis:
		if content.ObservationHypothesis != nil {
			return content.ObservationHypothesis.Statement, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnsupportedCorrectionKind, content.Kind)
}

// ApplyCorrectableField clones content's populated payload, replaces its one
// correctable field with newValue, and revalidates through the matching
// New*MemoryContent constructor so an invalid correction can never reach
// domain.ReviseMemoryArtifactWithUserCorrection.
func ApplyCorrectableField(content domain.MemoryArtifactContent, newValue string) (domain.MemoryArtifactContent, error) {
	trimmed := strings.TrimSpace(newValue)
	switch content.Kind {
	case domain.MemoryArtifactKindRepositoryFact:
		if content.RepositoryFact != nil {
			payload := *content.RepositoryFact
			payload.Statement = trimmed
			return domain.NewRepositoryFactMemoryContent(payload)
		}
	case domain.MemoryArtifactKindReviewedCommand:
		if content.ReviewedCommand != nil {
			payload := *content.ReviewedCommand
			payload.Argv = splitCorrectableList(trimmed)
			return domain.NewReviewedCommandMemoryContent(payload)
		}
	case domain.MemoryArtifactKindFileToTestMapping:
		if content.FileToTestMapping != nil {
			payload := *content.FileToTestMapping
			payload.TestPaths = splitCorrectableList(trimmed)
			return domain.NewFileToTestMappingMemoryContent(payload)
		}
	case domain.MemoryArtifactKindRepositoryConvention:
		if content.RepositoryConvention != nil {
			payload := *content.RepositoryConvention
			payload.Statement = trimmed
			return domain.NewRepositoryConventionMemoryContent(payload)
		}
	case domain.MemoryArtifactKindAcceptedRegressionCase:
		if content.AcceptedRegressionCase != nil {
			payload := *content.AcceptedRegressionCase
			payload.ExpectedOutcome = trimmed
			return domain.NewAcceptedRegressionCaseMemoryContent(payload)
		}
	case domain.MemoryArtifactKindExecutionRecipe:
		if content.ExecutionRecipe != nil {
			payload := *content.ExecutionRecipe
			payload.PlanSummary = trimmed
			return domain.NewExecutionRecipeMemoryContent(payload)
		}
	case domain.MemoryArtifactKindExecutableAtomReference:
		if content.AtomReference != nil {
			payload := *content.AtomReference
			payload.CanonicalName = trimmed
			return domain.NewExecutableAtomReferenceMemoryContent(payload)
		}
	case domain.MemoryArtifactKindObservationHypothesis:
		if content.ObservationHypothesis != nil {
			payload := *content.ObservationHypothesis
			payload.Statement = trimmed
			return domain.NewObservationHypothesisMemoryContent(payload)
		}
	}
	return domain.MemoryArtifactContent{}, fmt.Errorf("%w: %q", ErrUnsupportedCorrectionKind, content.Kind)
}

// splitCorrectableList reverses CorrectableFieldValue's one-per-line join.
// Splitting on a literal newline (rather than the historical "; ") means no
// value the field can legitimately hold -- including one containing "; " --
// can ever be mistaken for a delimiter and split into extra elements.
func splitCorrectableList(joined string) []string {
	lines := strings.Split(joined, correctableListLineDelimiter)
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// CorrectionProps configures the M21-095 correction control. Submission
// always creates a new candidate revision; it never mutates Prior.
type CorrectionProps struct {
	Prior               domain.MemoryArtifactRevision
	CandidateRevisionID domain.MemoryArtifactRevisionID
	Draft               string
	Busy                bool

	OnDraftChange func(string)
	OnSubmit      func(domain.MemoryArtifactRevisionID, domain.MemoryArtifactContent)
}

// Validate checks the prior revision, a distinct candidate identity, bounded
// draft text, and required callbacks.
func (props CorrectionProps) Validate() error {
	if props.Prior.ArtifactID.IsZero() || props.Prior.RevisionID.IsZero() {
		return fieldError("correction.prior", "must reference a real prior revision")
	}
	if props.CandidateRevisionID.IsZero() || props.CandidateRevisionID == props.Prior.RevisionID {
		return fieldError("correction.candidate_revision_id", "must be a new, distinct revision identity")
	}
	if err := validateOptionalText("correction.draft", props.Draft, MaximumCorrectionDraftBytes); err != nil {
		return err
	}
	if props.OnDraftChange == nil {
		return fieldError("correction.on_draft_change", "is required")
	}
	if props.OnSubmit == nil {
		return fieldError("correction.on_submit", "is required")
	}
	return nil
}

// PreviewCorrection validates props.Draft against props.Prior's content
// through ApplyCorrectableField and the same
// domain.ReviseMemoryArtifactWithUserCorrection contract submission uses, so
// a control surface can disable submission before any callback fires.
func PreviewCorrection(props CorrectionProps) (domain.MemoryArtifactContent, error) {
	if err := props.Validate(); err != nil {
		return domain.MemoryArtifactContent{}, err
	}
	corrected, err := ApplyCorrectableField(props.Prior.Content, props.Draft)
	if err != nil {
		return domain.MemoryArtifactContent{}, err
	}
	if _, err := domain.ReviseMemoryArtifactWithUserCorrection(props.Prior, props.CandidateRevisionID, corrected); err != nil {
		return domain.MemoryArtifactContent{}, err
	}
	return corrected, nil
}

// ActivateCorrection dispatches OnSubmit only once the draft passes
// domain.ReviseMemoryArtifactWithUserCorrection, so a caller can never
// receive a correction the domain layer would reject.
func ActivateCorrection(props CorrectionProps) bool {
	if props.Busy {
		return false
	}
	corrected, err := PreviewCorrection(props)
	if err != nil {
		return false
	}
	props.OnSubmit(props.CandidateRevisionID, corrected)
	return true
}

// -----------------------------------------------------------------------
// Quarantine and invalidation (M21-096, M21-097)
// -----------------------------------------------------------------------

// TransitionAvailability reports whether from can mechanically reach to under
// domain.ValidateMemoryArtifactMaturityTransition. Quarantine, invalidation,
// and retirement are never authority-bearing destinations, so a zero-value
// transition request (no proof) is always sufficient to check reachability;
// this package never constructs or needs an authority proof.
func TransitionAvailability(from, to domain.MaturityState) error {
	return domain.ValidateMemoryArtifactMaturityTransition(domain.MaturityTransitionRequest{From: from, To: to})
}

// QuarantineProps configures the M21-096 quarantine control. Quarantine is
// authority-terminal for Revision: the UI never implies it can later be
// un-quarantined or restored with inherited authority.
type QuarantineProps struct {
	Revision  domain.MemoryArtifactRevision
	Confirmed bool
	Busy      bool

	OnConfirmChange func(bool)
	OnQuarantine    func(domain.MemoryArtifactID, domain.MemoryArtifactRevisionID)
}

// Validate checks the revision identity, declared maturity, and required callbacks.
func (props QuarantineProps) Validate() error {
	if props.Revision.ArtifactID.IsZero() || props.Revision.RevisionID.IsZero() {
		return fieldError("quarantine.revision", "must reference a real artifact revision")
	}
	if !props.Revision.Maturity.IsValid() {
		return fieldError("quarantine.revision.maturity", "is not declared")
	}
	if props.OnConfirmChange == nil {
		return fieldError("quarantine.on_confirm_change", "is required")
	}
	if props.OnQuarantine == nil {
		return fieldError("quarantine.on_quarantine", "is required")
	}
	return nil
}

// ActivateQuarantine dispatches OnQuarantine only once the transition is
// mechanically legal and the user has explicitly confirmed the terminal
// consequence.
func ActivateQuarantine(props QuarantineProps) bool {
	if props.Busy || !props.Confirmed {
		return false
	}
	if err := TransitionAvailability(props.Revision.Maturity, domain.MaturityStateQuarantined); err != nil {
		return false
	}
	props.OnQuarantine(props.Revision.ArtifactID, props.Revision.RevisionID)
	return true
}

// InvalidationProps configures the M21-097 invalidation control. Invalidation
// always requires an explicit, non-empty reason.
type InvalidationProps struct {
	Revision domain.MemoryArtifactRevision
	Reason   string
	Busy     bool

	OnReasonChange func(string)
	OnInvalidate   func(domain.MemoryArtifactID, domain.MemoryArtifactRevisionID, string)
}

// Validate checks the revision identity, declared maturity, bounded reason
// text, and required callbacks.
func (props InvalidationProps) Validate() error {
	if props.Revision.ArtifactID.IsZero() || props.Revision.RevisionID.IsZero() {
		return fieldError("invalidation.revision", "must reference a real artifact revision")
	}
	if !props.Revision.Maturity.IsValid() {
		return fieldError("invalidation.revision.maturity", "is not declared")
	}
	if err := validateOptionalText("invalidation.reason", props.Reason, MaximumInvalidationReasonBytes); err != nil {
		return err
	}
	if props.OnReasonChange == nil {
		return fieldError("invalidation.on_reason_change", "is required")
	}
	if props.OnInvalidate == nil {
		return fieldError("invalidation.on_invalidate", "is required")
	}
	return nil
}

// ActivateInvalidation dispatches OnInvalidate only once the transition is
// mechanically legal and a non-empty reason has been supplied.
func ActivateInvalidation(props InvalidationProps) bool {
	if props.Busy {
		return false
	}
	reason := strings.TrimSpace(props.Reason)
	if reason == "" {
		return false
	}
	if err := TransitionAvailability(props.Revision.Maturity, domain.MaturityStateInvalidated); err != nil {
		return false
	}
	props.OnInvalidate(props.Revision.ArtifactID, props.Revision.RevisionID, reason)
	return true
}

// -----------------------------------------------------------------------
// Deletion with affected-descendant preview (M21-098)
// -----------------------------------------------------------------------

// DeletionPreviewState identifies whether the affected-descendant preview
// required by domain.PreviewMemoryArtifactDeletion has loaded.
type DeletionPreviewState string

const (
	DeletionPreviewLoading DeletionPreviewState = "loading"
	DeletionPreviewReady   DeletionPreviewState = "ready"
	DeletionPreviewError   DeletionPreviewState = "error"
)

// IsValid reports whether the deletion preview state is declared.
func (state DeletionPreviewState) IsValid() bool {
	switch state {
	case DeletionPreviewLoading, DeletionPreviewReady, DeletionPreviewError:
		return true
	default:
		return false
	}
}

// DeletionProps configures the M21-098 deletion control. The delete action is
// only ever offered once PreviewState is Ready: this struct's Validate makes
// that the only state that requires or permits OnDelete.
//
// AcknowledgedDependents names exactly the dependent identities the user has
// acknowledged, matching domain.MemoryArtifactDeletionRequest -- it is no
// longer a bare bool, so an acknowledgement can never be satisfied by a
// stale or forged smaller set silently passing as "true".
//
// CurrentLineageIndex is the caller-supplied (server-side) lineage index that
// domain.ValidateMemoryArtifactDeletion recomputes the fresh preview from.
// Per the package rule that "the UI never computes the preview," this
// package never derives, caches, or trusts a self-built index here; it only
// forwards exactly what the caller supplied to the domain check.
type DeletionProps struct {
	Target                 domain.MemoryArtifactID
	PreviewState           DeletionPreviewState
	Preview                domain.MemoryArtifactDeletionPreview
	ErrorMessage           string
	AcknowledgedDependents []domain.MemoryArtifactID
	CurrentLineageIndex    map[domain.MemoryArtifactID]domain.MemoryArtifactLineage
	Busy                   bool

	OnAcknowledgeChange func([]domain.MemoryArtifactID)
	OnRetryPreview      func()
	OnDelete            func(domain.MemoryArtifactDeletionRequest)
}

// Validate checks the target identity and the callbacks required by each
// declared preview state.
func (props DeletionProps) Validate() error {
	if props.Target.IsZero() {
		return fieldError("deletion.target", "must not be empty")
	}
	if !props.PreviewState.IsValid() {
		return fieldError("deletion.preview_state", "is not declared")
	}
	switch props.PreviewState {
	case DeletionPreviewLoading:
		return nil
	case DeletionPreviewReady:
		if props.Preview.Target != props.Target {
			return fieldError("deletion.preview", "must describe the same target as Target")
		}
		if len(props.CurrentLineageIndex) > MaximumLineageIndexEntries {
			return fieldError("deletion.current_lineage_index", "exceeds the bounded lookup size")
		}
		for id, lineage := range props.CurrentLineageIndex {
			if lineage.ArtifactID != id {
				return fieldError("deletion.current_lineage_index", "lookup key must match lineage artifact id")
			}
			if err := lineage.Validate(); err != nil {
				return err
			}
		}
		if props.OnAcknowledgeChange == nil {
			return fieldError("deletion.on_acknowledge_change", "is required once the preview is ready")
		}
		if props.OnDelete == nil {
			return fieldError("deletion.on_delete", "is required once the preview is ready")
		}
		return nil
	case DeletionPreviewError:
		if err := validateRequiredText("deletion.error_message", props.ErrorMessage, MaximumSummaryBytes); err != nil {
			return err
		}
		if props.OnRetryPreview == nil {
			return fieldError("deletion.on_retry_preview", "is required when the preview failed")
		}
		return nil
	default:
		return fieldError("deletion.preview_state", "is not declared")
	}
}

// acknowledgesAllDirectDependents reports whether props.AcknowledgedDependents
// names exactly the identities in props.Preview.DirectDependents (order and
// duplicates ignored), matching the set equality
// domain.ValidateMemoryArtifactDeletion itself requires. Used to drive the
// acknowledgement control's pressed state and the submit button's
// availability without duplicating the domain's exact-match semantics.
func acknowledgesAllDirectDependents(props DeletionProps) bool {
	return sameArtifactIDSet(props.AcknowledgedDependents, props.Preview.DirectDependents)
}

// sameArtifactIDSet reports whether a and b contain exactly the same
// domain.MemoryArtifactID values, ignoring order and duplicate entries.
func sameArtifactIDSet(a, b []domain.MemoryArtifactID) bool {
	toSet := func(values []domain.MemoryArtifactID) map[domain.MemoryArtifactID]struct{} {
		set := make(map[domain.MemoryArtifactID]struct{}, len(values))
		for _, id := range values {
			set[id] = struct{}{}
		}
		return set
	}
	setA, setB := toSet(a), toSet(b)
	if len(setA) != len(setB) {
		return false
	}
	for id := range setA {
		if _, ok := setB[id]; !ok {
			return false
		}
	}
	return true
}

// ActivateDeletion dispatches OnDelete only once the preview has loaded and
// domain.ValidateMemoryArtifactDeletion accepts the constructed request
// against props.CurrentLineageIndex. That domain check recomputes the
// preview from the caller-supplied index itself, so a stale or forged
// Preview -- or an AcknowledgedDependents set that does not name exactly the
// freshly recomputed dependents -- is rejected here, not merely trusted.
func ActivateDeletion(props DeletionProps) bool {
	if props.Busy || props.PreviewState != DeletionPreviewReady {
		return false
	}
	request := domain.MemoryArtifactDeletionRequest{
		Target: props.Target, Preview: props.Preview,
		AcknowledgedDependents: append([]domain.MemoryArtifactID(nil), props.AcknowledgedDependents...),
	}
	if err := domain.ValidateMemoryArtifactDeletion(request, props.CurrentLineageIndex); err != nil {
		return false
	}
	props.OnDelete(request)
	return true
}

// -----------------------------------------------------------------------
// Selected-artifact composition (wraps M21-092..098 for one artifact)
// -----------------------------------------------------------------------

// SelectedState is the explicit data-owning state of the single-artifact
// detail and governance surface.
type SelectedState string

const (
	SelectedStateNone         SelectedState = "none"
	SelectedStateLoading      SelectedState = "loading"
	SelectedStateReady        SelectedState = "ready"
	SelectedStateError        SelectedState = "error"
	SelectedStateDenied       SelectedState = "denied"
	SelectedStateIncompatible SelectedState = "incompatible"
	SelectedStateDisconnected SelectedState = "disconnected"
)

// IsValid reports whether the selected-artifact state is declared.
func (state SelectedState) IsValid() bool {
	switch state {
	case SelectedStateNone, SelectedStateLoading, SelectedStateReady,
		SelectedStateError, SelectedStateDenied, SelectedStateIncompatible, SelectedStateDisconnected:
		return true
	default:
		return false
	}
}

// SelectedArtifactProps composes detail (M21-092..094) and governance
// (M21-095..098) for exactly one selected artifact.
type SelectedArtifactProps struct {
	State        SelectedState
	ErrorMessage string
	OnRetry      func()

	Detail       DetailProps
	Correction   CorrectionProps
	Quarantine   QuarantineProps
	Invalidation InvalidationProps
	Deletion     DeletionProps
}

// Validate checks the declared state and, for Ready, that every nested
// governance prop targets the same artifact as Detail.
func (props SelectedArtifactProps) Validate() error {
	if !props.State.IsValid() {
		return fieldError("selected.state", "is not declared")
	}
	switch props.State {
	case SelectedStateNone, SelectedStateLoading:
		return nil
	case SelectedStateReady:
		if err := props.Detail.Validate(); err != nil {
			return err
		}
		artifact := props.Detail.Revision.ArtifactID
		if props.Correction.Prior.ArtifactID != artifact {
			return fieldError("selected.correction", "must target the same artifact as detail")
		}
		if err := props.Correction.Validate(); err != nil {
			return err
		}
		if props.Quarantine.Revision.ArtifactID != artifact {
			return fieldError("selected.quarantine", "must target the same artifact as detail")
		}
		if err := props.Quarantine.Validate(); err != nil {
			return err
		}
		if props.Invalidation.Revision.ArtifactID != artifact {
			return fieldError("selected.invalidation", "must target the same artifact as detail")
		}
		if err := props.Invalidation.Validate(); err != nil {
			return err
		}
		if props.Deletion.Target != artifact {
			return fieldError("selected.deletion", "must target the same artifact as detail")
		}
		return props.Deletion.Validate()
	case SelectedStateError, SelectedStateDenied, SelectedStateIncompatible, SelectedStateDisconnected:
		if err := validateRequiredText("selected.error_message", props.ErrorMessage, MaximumSummaryBytes); err != nil {
			return err
		}
		if props.OnRetry == nil {
			return fieldError("selected.on_retry", "is required for a recoverable unready state")
		}
		return nil
	default:
		return fieldError("selected.state", "is not declared")
	}
}

// -----------------------------------------------------------------------
// Export selected structured records without secrets (M21-099)
// -----------------------------------------------------------------------

// ExportCandidate pairs one already-fetched, typed, secret-free
// domain.MemoryArtifactExportRecord with whether the user has selected it for
// export. This package never builds its own redaction: it only surfaces the
// export record as produced upstream.
type ExportCandidate struct {
	Record   domain.MemoryArtifactExportRecord
	Selected bool
}

// Validate checks the record's identity and lineage consistency.
func (candidate ExportCandidate) Validate() error {
	if candidate.Record.ArtifactID.IsZero() || candidate.Record.RevisionID.IsZero() {
		return fieldError("export.candidate.record", "must reference a real artifact revision")
	}
	if err := candidate.Record.Content.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMemoryInspectorProps, err)
	}
	if err := candidate.Record.Lineage.Validate(); err != nil {
		return err
	}
	if candidate.Record.Lineage.ArtifactID != candidate.Record.ArtifactID {
		return fieldError("export.candidate.lineage", "must describe the same artifact as the record")
	}
	return nil
}

// ExportProps configures the M21-099 export control.
type ExportProps struct {
	Candidates []ExportCandidate
	Busy       bool

	OnToggleSelect   func(domain.MemoryArtifactID, bool)
	OnExportSelected func([]domain.MemoryArtifactExportRecord)
}

// Validate checks bounded, unique, individually valid candidates and
// required callbacks.
func (props ExportProps) Validate() error {
	if len(props.Candidates) > MaximumExportCandidates {
		return fieldError("export.candidates", "exceeds the bounded export batch")
	}
	seen := make(map[domain.MemoryArtifactID]struct{}, len(props.Candidates))
	for _, candidate := range props.Candidates {
		if err := candidate.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[candidate.Record.ArtifactID]; duplicate {
			return fieldError("export.candidates", "contains a duplicate artifact identity")
		}
		seen[candidate.Record.ArtifactID] = struct{}{}
	}
	if len(props.Candidates) > 0 {
		if props.OnToggleSelect == nil {
			return fieldError("export.on_toggle_select", "is required when candidates are present")
		}
		if props.OnExportSelected == nil {
			return fieldError("export.on_export_selected", "is required when candidates are present")
		}
	}
	return nil
}

// SelectedExportRecords returns the records currently marked Selected, in
// candidate order, without mutating props.
func SelectedExportRecords(props ExportProps) []domain.MemoryArtifactExportRecord {
	selected := make([]domain.MemoryArtifactExportRecord, 0, len(props.Candidates))
	for _, candidate := range props.Candidates {
		if candidate.Selected {
			selected = append(selected, candidate.Record)
		}
	}
	return selected
}

// ActivateExport dispatches OnExportSelected only when at least one candidate
// is selected: this control exports selected records, never the full batch
// implicitly.
func ActivateExport(props ExportProps) bool {
	if props.Busy {
		return false
	}
	selected := SelectedExportRecords(props)
	if len(selected) == 0 || props.OnExportSelected == nil {
		return false
	}
	props.OnExportSelected(selected)
	return true
}

// -----------------------------------------------------------------------
// Root props
// -----------------------------------------------------------------------

// Props is the top-level Component input: the list (M21-090/091), the
// selected artifact's detail and governance controls (M21-092..098), and the
// export batch (M21-099).
type Props struct {
	Mode       primitives.Mode
	Translator frontendi18n.Translator
	List       ListProps
	Selected   SelectedArtifactProps
	Export     ExportProps
}

// Validate checks every nested section.
func (props Props) Validate() error {
	if err := props.List.Validate(); err != nil {
		return err
	}
	if err := props.Selected.Validate(); err != nil {
		return err
	}
	return props.Export.Validate()
}
