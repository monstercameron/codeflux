package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Memory Boundary (Milestone 21, M21-001..013)
//
// Raw task history is fact: an EpisodeID names an immutable chronological
// record of what happened and is never revoked. Project-memory authority is
// a different thing: a governed, revocable claim that a MemoryArtifactID's
// current revision may influence future work. An episode existing, or even
// being named as a SupportingEpisodes entry on a MemoryArtifactLineage,
// never by itself grants authority. Authority is granted only through
// AuthorizeMemoryArtifactMaturityGrant, which accepts the full recorded
// evidence set, excludes agent-self-report evidence from ever independently
// justifying a grant (it can never satisfy the corroborated requirement; see
// M21-011 below), and binds the resulting proof to the exact revision it was
// earned for so it cannot be replayed against an unrelated artifact or
// revision. Authority is also always revocable and, once quarantined,
// invalidated, or retired, never regained by the same artifact revision or
// identity: ReviseMemoryArtifactWithUserCorrection refuses to correct a
// revision whose maturity can no longer reach an authority-bearing state,
// per §31 "An independently derived successor receives a new identity" (see
// MaturityState.CanReachAuthority).

// ErrCrossProjectMemoryAccess classifies an attempt to let one project's
// memory artifact satisfy another project's query boundary.
var ErrCrossProjectMemoryAccess = errors.New("cross-project memory access")

// ErrAuthorityProofRequired classifies an attempt to move a memory artifact
// into an authority-bearing maturity state without a proof obtained from
// AuthorizeMemoryArtifactMaturityGrant.
var ErrAuthorityProofRequired = errors.New("memory artifact maturity requires an authorized evidence proof")

// ErrAuthorityProofMismatch classifies an attempt to move a memory artifact
// into an authority-bearing maturity state using an authorityProof that was
// legitimately earned from another revision's evidence. A proof obtained
// from AuthorizeMemoryArtifactMaturityGrant for revision A can never
// authorize revision B: see MaturityTransitionRequest.Revision.
var ErrAuthorityProofMismatch = errors.New("memory artifact maturity authority proof does not match the revision it is applied to")

// ErrMemoryArtifactCorrectionRequiresNewIdentity classifies an attempt to
// correct (ReviseMemoryArtifactWithUserCorrection) a memory-artifact
// revision whose maturity can no longer reach an authority-bearing state
// (quarantined, invalidated, or retired; see MaturityState.CanReachAuthority).
// Per §31 Artifact Failure Protocol, "Quarantine is terminal for the exposed
// lesson version. A failed lesson is not repaired in place or restored with
// inherited authority. An independently derived successor receives a new
// identity, no inherited confidence, the original counterexample, and fresh
// admission requirements." A correction under the same MemoryArtifactID
// would repair the failed artifact in place and let it regain a path to
// authority under its original identity, exactly what §31 forbids. Callers
// hitting this error must create a new memory artifact identity (a fresh
// MemoryArtifactID via NewMemoryArtifactRevision) instead of correcting the
// terminal-for-authority revision.
var ErrMemoryArtifactCorrectionRequiresNewIdentity = errors.New("memory artifact correction requires a new artifact identity")

// -----------------------------------------------------------------------
// Evidence strength and source (M21-011 structural enforcement)
// -----------------------------------------------------------------------

// EvidenceSourceKind classifies where supporting evidence for a memory
// artifact came from. AgentSelfReport is always carried at
// EvidenceStrengthNone (enforced by SupportingEvidenceRecord.Validate), so
// it can never itself satisfy AuthorizeMemoryArtifactMaturityGrant's
// corroborated-evidence requirement; its presence in an evidence set is
// otherwise inert (excluded, not poisoning), matching §31: "Agent
// explanations are stored only as agent_self_report with evidence_strength:
// none. They are not treated as causal accounts," and the Dependency
// Invariant "agent self-report -> accepted outcome" is prohibited, not
// "agent self-report present -> reject everything."
type EvidenceSourceKind string

const (
	EvidenceSourceKindAgentSelfReport             EvidenceSourceKind = "agent-self-report"
	EvidenceSourceKindAutomatedValidationRun      EvidenceSourceKind = "automated-validation-run"
	EvidenceSourceKindUserApproval                EvidenceSourceKind = "user-approval"
	EvidenceSourceKindIndependentReplayComparison EvidenceSourceKind = "independent-replay-comparison"
	EvidenceSourceKindMechanicalRuleCheck         EvidenceSourceKind = "mechanical-rule-check"
	EvidenceSourceKindRegressionOracleRun         EvidenceSourceKind = "regression-oracle-run"
)

// IsValid reports whether the evidence source is declared.
func (value EvidenceSourceKind) IsValid() bool {
	switch value {
	case EvidenceSourceKindAgentSelfReport,
		EvidenceSourceKindAutomatedValidationRun,
		EvidenceSourceKindUserApproval,
		EvidenceSourceKindIndependentReplayComparison,
		EvidenceSourceKindMechanicalRuleCheck,
		EvidenceSourceKindRegressionOracleRun:
		return true
	default:
		return false
	}
}

// EvidenceStrength grades whether evidence is strong enough to influence
// maturity. Per §31 and the M21-009/M21-011 tasks, "unknown"/self-reported
// support is never zero, empty, or false-confident: it is the explicit
// EvidenceStrengthNone value, never simply an absent field.
type EvidenceStrength string

const (
	EvidenceStrengthNone         EvidenceStrength = "none"
	EvidenceStrengthCorroborated EvidenceStrength = "corroborated"
)

// IsValid reports whether the evidence strength is declared.
func (value EvidenceStrength) IsValid() bool {
	switch value {
	case EvidenceStrengthNone, EvidenceStrengthCorroborated:
		return true
	default:
		return false
	}
}

// SupportingEvidenceRecord binds one piece of evidence to a memory-artifact
// maturity claim. Validate is the first structural barrier for M21-011: an
// agent_self_report record can never be constructed with a non-none
// strength, regardless of caller intent.
type SupportingEvidenceRecord struct {
	Evidence EvidenceID         `json:"evidence"`
	Source   EvidenceSourceKind `json:"source"`
	Strength EvidenceStrength   `json:"strength"`
}

// Validate checks the record is well-formed and, specifically, that
// agent-self-report evidence never claims corroborated strength.
func (value SupportingEvidenceRecord) Validate() error {
	switch {
	case value.Evidence.IsZero():
		return valueError("memory.supporting_evidence.evidence", "must not be empty")
	case !value.Source.IsValid():
		return valueError("memory.supporting_evidence.source", "is not declared")
	case !value.Strength.IsValid():
		return valueError("memory.supporting_evidence.strength", "is not declared")
	case value.Source == EvidenceSourceKindAgentSelfReport && value.Strength != EvidenceStrengthNone:
		return valueError("memory.supporting_evidence.strength", "agent self-report evidence must always carry evidence_strength none")
	default:
		return nil
	}
}

// authorityProof is an unexported capability token proving that a set of
// supporting evidence is eligible to authorize an authority-bearing maturity
// transition (Validated or PreferredForExperiment) for exactly one memory
// artifact revision. It can only be produced by
// AuthorizeMemoryArtifactMaturityGrant. Because the struct and its fields
// are unexported, no package outside internal/domain can construct a
// non-zero authorityProof by any means other than calling that function.
// Binding the proof to Revision (M21-011 plus the anti-replay fix) means a
// proof legitimately earned from one artifact revision's evidence can never
// be presented to promote a different, unrelated revision:
// ValidateMemoryArtifactMaturityTransition checks the binding, not merely
// that some proof exists. Within that binding, agent-self-report evidence
// can never independently satisfy the corroborated-evidence requirement
// (SupportingEvidenceRecord.Validate pins it to EvidenceStrengthNone), so
// self-report cannot create validated status, not by convention but because
// the only door into an authority-bearing state requires a token that
// self-report evidence cannot unlock on its own.
type authorityProof struct {
	revision MemoryArtifactRevisionID
	evidence []SupportingEvidenceRecord
}

// isAuthorized reports whether proof carries a real, non-empty grant for
// some revision (irrespective of which one).
func (proof authorityProof) isAuthorized() bool {
	return !proof.revision.IsZero() && len(proof.evidence) > 0
}

// authorizesRevision reports whether proof was earned for exactly revision,
// the anti-replay check that prevents evidence legitimately gathered for one
// memory-artifact revision from promoting an unrelated one.
func (proof authorityProof) authorizesRevision(revision MemoryArtifactRevisionID) bool {
	return proof.isAuthorized() && proof.revision == revision
}

// AuthorizeMemoryArtifactMaturityGrant validates the full recorded
// supporting-evidence set for revision and, only if it contains at least one
// corroborated record, returns a proof usable with
// ValidateMemoryArtifactMaturityTransition and TransitionMemoryArtifactMaturity
// to move exactly that revision to Validated or PreferredForExperiment. The
// proof is bound to revision: ValidateMemoryArtifactMaturityTransition
// rejects it for any other revision (see ErrAuthorityProofMismatch).
//
// Callers pass the complete evidence set, including any agent-self-report
// records: this function excludes them from the corroborated-evidence check
// internally rather than requiring (or trusting) a caller to pre-filter
// them, and rather than letting their mere presence poison an otherwise
// valid grant. Per §31, "Agent explanations are stored only as
// agent_self_report with evidence_strength: none. They are not treated as
// causal accounts" -- so self-report contributes nothing toward
// authorization, but does not veto real evidence either
// (M21-011: "prohibit self-report from CREATING validated status", not from
// coexisting with the evidence that does). An evidence set containing only
// self-report records, or no corroborated non-self-report record at all,
// still never produces a usable proof.
func AuthorizeMemoryArtifactMaturityGrant(revision MemoryArtifactRevisionID, evidence []SupportingEvidenceRecord) (authorityProof, error) {
	if revision.IsZero() {
		return authorityProof{}, valueError("memory.maturity.evidence.revision", "an authority grant requires the revision identity it authorizes")
	}
	if len(evidence) == 0 {
		return authorityProof{}, valueError("memory.maturity.evidence", "an authority grant requires at least one supporting evidence record")
	}
	hasCorroborated := false
	for _, record := range evidence {
		if err := record.Validate(); err != nil {
			return authorityProof{}, err
		}
		// Agent self-report is always EvidenceStrengthNone (enforced by
		// Validate above), so it can never itself flip hasCorroborated. It is
		// still recorded in the returned proof's evidence for provenance.
		if record.Strength == EvidenceStrengthCorroborated {
			hasCorroborated = true
		}
	}
	if !hasCorroborated {
		return authorityProof{}, valueError("memory.maturity.evidence", "an authority grant requires at least one corroborated, non-self-report evidence record")
	}
	return authorityProof{revision: revision, evidence: append([]SupportingEvidenceRecord(nil), evidence...)}, nil
}

// -----------------------------------------------------------------------
// Maturity states (M21-010)
// -----------------------------------------------------------------------

// MaturityState describes the governed, revocable authority of one memory
// artifact revision. Reaching Validated or PreferredForExperiment always
// requires an authorityProof (see ValidateMemoryArtifactMaturityTransition).
// Reaching Quarantined is authority-terminal: per §31 Artifact Failure
// Protocol, "Quarantine is terminal for the exposed lesson version. A failed
// lesson is not repaired in place or restored with inherited authority," so
// the transition table below contains no path from Quarantined back to
// Candidate, Validated, or PreferredForExperiment.
type MaturityState string

const (
	MaturityStateCandidate              MaturityState = "candidate"
	MaturityStateValidated              MaturityState = "validated"
	MaturityStatePreferredForExperiment MaturityState = "preferred-for-experiment"
	MaturityStateQuarantined            MaturityState = "quarantined"
	MaturityStateInvalidated            MaturityState = "invalidated"
	MaturityStateRetired                MaturityState = "retired"
)

var maturityStates = []MaturityState{
	MaturityStateCandidate,
	MaturityStateValidated,
	MaturityStatePreferredForExperiment,
	MaturityStateQuarantined,
	MaturityStateInvalidated,
	MaturityStateRetired,
}

var maturityTransitions = transitions(
	[2]MaturityState{MaturityStateCandidate, MaturityStateValidated},
	[2]MaturityState{MaturityStateCandidate, MaturityStatePreferredForExperiment},
	[2]MaturityState{MaturityStateCandidate, MaturityStateQuarantined},
	[2]MaturityState{MaturityStateCandidate, MaturityStateInvalidated},
	[2]MaturityState{MaturityStateCandidate, MaturityStateRetired},
	[2]MaturityState{MaturityStateValidated, MaturityStatePreferredForExperiment},
	[2]MaturityState{MaturityStateValidated, MaturityStateQuarantined},
	[2]MaturityState{MaturityStateValidated, MaturityStateInvalidated},
	[2]MaturityState{MaturityStateValidated, MaturityStateRetired},
	[2]MaturityState{MaturityStatePreferredForExperiment, MaturityStateValidated},
	[2]MaturityState{MaturityStatePreferredForExperiment, MaturityStateQuarantined},
	[2]MaturityState{MaturityStatePreferredForExperiment, MaturityStateInvalidated},
	[2]MaturityState{MaturityStatePreferredForExperiment, MaturityStateRetired},
	[2]MaturityState{MaturityStateQuarantined, MaturityStateInvalidated},
	[2]MaturityState{MaturityStateQuarantined, MaturityStateRetired},
)

// AllMaturityStates returns the complete declared maturity-state set.
func AllMaturityStates() []MaturityState {
	return append([]MaturityState(nil), maturityStates...)
}

// IsValid reports whether the maturity state is declared.
func (state MaturityState) IsValid() bool {
	return containsState(maturityStates, state)
}

// IsTerminal reports whether the state has no outgoing transition at all.
func (state MaturityState) IsTerminal() bool {
	switch state {
	case MaturityStateInvalidated, MaturityStateRetired:
		return true
	default:
		return false
	}
}

// IsAuthorityBearing reports whether the state lets a memory artifact
// influence future work.
func (state MaturityState) IsAuthorityBearing() bool {
	switch state {
	case MaturityStateValidated, MaturityStatePreferredForExperiment:
		return true
	default:
		return false
	}
}

// CanReachAuthority reports whether the declared transition table still
// contains a path from this state directly into an authority-bearing state.
// It is derived from maturityTransitions rather than hard-coded so that any
// future edit to the table is checked against the same rule it documents:
// once a state is Quarantined, Invalidated, or Retired, CanReachAuthority is
// false and stays false.
func (state MaturityState) CanReachAuthority() bool {
	for _, target := range []MaturityState{MaturityStateValidated, MaturityStatePreferredForExperiment} {
		if _, allowed := maturityTransitions[state][target]; allowed {
			return true
		}
	}
	return false
}

// MaturityTransitionRequest carries the evidence proof needed to validate a
// memory-artifact maturity change. Revision identifies which memory-artifact
// revision this transition applies to; when To is authority-bearing, Proof
// must have been earned (via AuthorizeMemoryArtifactMaturityGrant) for
// exactly this Revision, not merely be some non-empty proof, so evidence
// legitimately gathered for one revision can never promote another.
type MaturityTransitionRequest struct {
	From     MaturityState
	To       MaturityState
	Revision MemoryArtifactRevisionID
	Proof    authorityProof
}

// ValidateMemoryArtifactMaturityTransition validates one maturity transition.
// Moving into Validated or PreferredForExperiment additionally requires a
// Proof obtained from AuthorizeMemoryArtifactMaturityGrant for exactly
// Revision; a zero-value Proof (the default when a caller never calls that
// function) is rejected with ErrAuthorityProofRequired, and a Proof
// authorized for a different revision is rejected with
// ErrAuthorityProofMismatch.
func ValidateMemoryArtifactMaturityTransition(request MaturityTransitionRequest) error {
	if err := validateTransition(
		"memory artifact maturity",
		request.From,
		request.To,
		MaturityState.IsValid,
		maturityTransitions,
	); err != nil {
		return err
	}
	if request.To.IsAuthorityBearing() {
		if !request.Proof.isAuthorized() {
			return &TransitionError{
				Machine: "memory artifact maturity",
				From:    string(request.From),
				To:      string(request.To),
				Cause:   ErrAuthorityProofRequired,
			}
		}
		if !request.Proof.authorizesRevision(request.Revision) {
			return &TransitionError{
				Machine: "memory artifact maturity",
				From:    string(request.From),
				To:      string(request.To),
				Cause:   ErrAuthorityProofMismatch,
			}
		}
	}
	return nil
}

// -----------------------------------------------------------------------
// Project ownership and cross-project isolation (M21-012)
// -----------------------------------------------------------------------

// MemoryProjectScope identifies the project, and optionally the repository,
// that owns a memory artifact.
type MemoryProjectScope struct {
	Project    ProjectID    `json:"project"`
	Repository RepositoryID `json:"repository"`
}

// Validate checks that the mandatory project boundary is present.
func (value MemoryProjectScope) Validate() error {
	if value.Project.IsZero() {
		return valueError("memory.project_scope.project", "must not be empty")
	}
	return nil
}

// MemoryQueryProjectBoundary is the explicit project-isolation predicate
// every memory query must declare, per AGENTS.md "Add explicit
// project-boundary predicates to memory, graph, vector, and retrieval
// queries." A query with a zero Project is invalid and must be rejected
// before it reaches storage.
type MemoryQueryProjectBoundary struct {
	Project ProjectID `json:"project"`
	// IncludeRepositories, when non-empty, further restricts the query to
	// specific repositories within Project. An empty slice means every
	// repository owned by Project is in scope.
	IncludeRepositories []RepositoryID `json:"include_repositories"`
}

// Validate checks the boundary carries a real project and no blank
// repository entries.
func (value MemoryQueryProjectBoundary) Validate() error {
	if value.Project.IsZero() {
		return valueError("memory.query.project", "must not be empty")
	}
	for _, repository := range value.IncludeRepositories {
		if repository.IsZero() {
			return valueError("memory.query.project.include_repositories", "must not contain empty identities")
		}
	}
	return nil
}

// Allows reports whether a memory artifact owned by scope may be returned by
// a query declaring this boundary. Cross-project isolation is absolute: a
// scope from a different project never satisfies the boundary regardless of
// maturity, evidence, or lineage. It fails closed on either side being the
// zero ProjectID: two unset projects comparing equal to each other must
// never be mistaken for a real, matching project boundary.
func (value MemoryQueryProjectBoundary) Allows(scope MemoryProjectScope) bool {
	if value.Project.IsZero() || scope.Project.IsZero() {
		return false
	}
	if value.Project != scope.Project {
		return false
	}
	if len(value.IncludeRepositories) == 0 {
		return true
	}
	for _, repository := range value.IncludeRepositories {
		if repository == scope.Repository {
			return true
		}
	}
	return false
}

// RequireMemoryQueryProjectBoundary checks boundary and scope, returning a
// typed error the transport boundary can map safely whenever scope falls
// outside boundary. This is the one call every memory-query implementation
// should route through to enforce M21-012 cross-project isolation.
func RequireMemoryQueryProjectBoundary(boundary MemoryQueryProjectBoundary, scope MemoryProjectScope) error {
	if err := boundary.Validate(); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if !boundary.Allows(scope) {
		return fmt.Errorf("%w: artifact project %q is outside query boundary project %q", ErrCrossProjectMemoryAccess, scope.Project, boundary.Project)
	}
	return nil
}

// -----------------------------------------------------------------------
// Revision binding shared by fact-shaped artifact kinds
// -----------------------------------------------------------------------

// RevisionBinding ties a fact to the repository revision(s) at which it
// holds. Per the package-wide Known/UnknownReason rule, an unknown binding
// is never represented by empty strings; it must explain itself.
type RevisionBinding struct {
	Known              bool   `json:"known"`
	UnknownReason      string `json:"unknown_reason"`
	ExactRevision      string `json:"exact_revision"`
	ValidFromRevision  string `json:"valid_from_revision"`
	ValidUntilRevision string `json:"valid_until_revision"`
}

// MaximumRevisionFieldBytes bounds every free-form revision-identifier
// string on RevisionBinding (ExactRevision, ValidFromRevision,
// ValidUntilRevision) and its UnknownReason explanation. Every peer identity
// or binding-shaped field elsewhere in the codebase is explicitly capped
// (e.g. internal/fingerprint.MaximumBindingFieldLength, the "repository
// revision" bound in internal/forecast), so an uncapped revision string here
// was an outlier: without this bound, an arbitrarily large caller-supplied
// value is accepted, canonicalized, and hashed into every persisted revision
// and content digest that carries it.
const MaximumRevisionFieldBytes = 512

// Validate checks internal consistency of the known/unknown binding and
// bounds every free-form string field at MaximumRevisionFieldBytes.
func (value RevisionBinding) Validate() error {
	if !value.Known {
		if strings.TrimSpace(value.UnknownReason) == "" {
			return valueError("memory.revision_binding.unknown_reason", "must explain why the binding is unknown")
		}
		if len(value.UnknownReason) > MaximumRevisionFieldBytes {
			return valueError("memory.revision_binding.unknown_reason", "must not exceed 512 bytes")
		}
		if value.ExactRevision != "" || value.ValidFromRevision != "" || value.ValidUntilRevision != "" {
			return valueError("memory.revision_binding", "unknown binding must not carry revision values")
		}
		return nil
	}
	if strings.TrimSpace(value.UnknownReason) != "" {
		return valueError("memory.revision_binding.unknown_reason", "known binding must not carry an unknown reason")
	}
	if strings.TrimSpace(value.ExactRevision) == "" && strings.TrimSpace(value.ValidFromRevision) == "" {
		return valueError("memory.revision_binding", "known binding requires an exact revision or a validity start")
	}
	switch {
	case len(value.ExactRevision) > MaximumRevisionFieldBytes:
		return valueError("memory.revision_binding.exact_revision", "must not exceed 512 bytes")
	case len(value.ValidFromRevision) > MaximumRevisionFieldBytes:
		return valueError("memory.revision_binding.valid_from_revision", "must not exceed 512 bytes")
	case len(value.ValidUntilRevision) > MaximumRevisionFieldBytes:
		return valueError("memory.revision_binding.valid_until_revision", "must not exceed 512 bytes")
	}
	return nil
}

// -----------------------------------------------------------------------
// Artifact kinds (M21-002..009)
// -----------------------------------------------------------------------

// MemoryArtifactKind discriminates which typed payload a MemoryArtifactContent carries.
type MemoryArtifactKind string

const (
	MemoryArtifactKindRepositoryFact          MemoryArtifactKind = "repository-fact"
	MemoryArtifactKindReviewedCommand         MemoryArtifactKind = "reviewed-command"
	MemoryArtifactKindFileToTestMapping       MemoryArtifactKind = "file-to-test-mapping"
	MemoryArtifactKindRepositoryConvention    MemoryArtifactKind = "repository-convention"
	MemoryArtifactKindAcceptedRegressionCase  MemoryArtifactKind = "accepted-regression-case"
	MemoryArtifactKindExecutionRecipe         MemoryArtifactKind = "execution-recipe"
	MemoryArtifactKindExecutableAtomReference MemoryArtifactKind = "executable-atom-reference"
	MemoryArtifactKindObservationHypothesis   MemoryArtifactKind = "observation-hypothesis"
)

// IsValid reports whether the artifact kind is declared.
func (value MemoryArtifactKind) IsValid() bool {
	switch value {
	case MemoryArtifactKindRepositoryFact,
		MemoryArtifactKindReviewedCommand,
		MemoryArtifactKindFileToTestMapping,
		MemoryArtifactKindRepositoryConvention,
		MemoryArtifactKindAcceptedRegressionCase,
		MemoryArtifactKindExecutionRecipe,
		MemoryArtifactKindExecutableAtomReference,
		MemoryArtifactKindObservationHypothesis:
		return true
	default:
		return false
	}
}

// RepositoryFactCategory classifies a factual repository fact (M21-002).
type RepositoryFactCategory string

const (
	RepositoryFactCategoryTopology               RepositoryFactCategory = "topology"
	RepositoryFactCategoryOwnership              RepositoryFactCategory = "ownership"
	RepositoryFactCategoryBuildCommand           RepositoryFactCategory = "build-command"
	RepositoryFactCategoryTestCommand            RepositoryFactCategory = "test-command"
	RepositoryFactCategoryDependency             RepositoryFactCategory = "dependency"
	RepositoryFactCategoryGeneratedFilePolicy    RepositoryFactCategory = "generated-file-policy"
	RepositoryFactCategoryEnvironmentRequirement RepositoryFactCategory = "environment-requirement"
	RepositoryFactCategoryFailureSignature       RepositoryFactCategory = "failure-signature"
)

// IsValid reports whether the fact category is declared.
func (value RepositoryFactCategory) IsValid() bool {
	switch value {
	case RepositoryFactCategoryTopology,
		RepositoryFactCategoryOwnership,
		RepositoryFactCategoryBuildCommand,
		RepositoryFactCategoryTestCommand,
		RepositoryFactCategoryDependency,
		RepositoryFactCategoryGeneratedFilePolicy,
		RepositoryFactCategoryEnvironmentRequirement,
		RepositoryFactCategoryFailureSignature:
		return true
	default:
		return false
	}
}

// RepositoryFactContent is a factual repository fact (M21-002): topology,
// ownership, build/test commands, dependencies, generated-file policy,
// environment requirements, or a known failure signature, bound to the
// repository revision(s) at which it holds.
type RepositoryFactContent struct {
	Repository RepositoryID           `json:"repository"`
	Category   RepositoryFactCategory `json:"category"`
	Statement  string                 `json:"statement"`
	Binding    RevisionBinding        `json:"binding"`
}

// Validate checks required fields and the revision binding.
func (value RepositoryFactContent) Validate() error {
	switch {
	case value.Repository.IsZero():
		return valueError("memory.repository_fact.repository", "must not be empty")
	case !value.Category.IsValid():
		return valueError("memory.repository_fact.category", "is not declared")
	case strings.TrimSpace(value.Statement) == "":
		return valueError("memory.repository_fact.statement", "must not be empty")
	default:
		return value.Binding.Validate()
	}
}

// CommandPurpose classifies what a reviewed command accomplishes.
type CommandPurpose string

const (
	CommandPurposeBuild    CommandPurpose = "build"
	CommandPurposeTest     CommandPurpose = "test"
	CommandPurposeLint     CommandPurpose = "lint"
	CommandPurposeGenerate CommandPurpose = "generate"
)

// IsValid reports whether the command purpose is declared.
func (value CommandPurpose) IsValid() bool {
	switch value {
	case CommandPurposeBuild, CommandPurposeTest, CommandPurposeLint, CommandPurposeGenerate:
		return true
	default:
		return false
	}
}

// ReviewedCommandContent is a reviewed command (M21-003): an argument array,
// never an interpolated shell string, bound to the repository revision(s) at
// which it succeeded.
type ReviewedCommandContent struct {
	Repository       RepositoryID    `json:"repository"`
	Purpose          CommandPurpose  `json:"purpose"`
	Argv             []string        `json:"argv"`
	WorkingDirectory string          `json:"working_directory"`
	Binding          RevisionBinding `json:"binding"`
}

// Validate checks required fields, argument well-formedness, and the
// revision binding.
func (value ReviewedCommandContent) Validate() error {
	switch {
	case value.Repository.IsZero():
		return valueError("memory.reviewed_command.repository", "must not be empty")
	case !value.Purpose.IsValid():
		return valueError("memory.reviewed_command.purpose", "is not declared")
	case len(value.Argv) == 0:
		return valueError("memory.reviewed_command.argv", "must contain at least one argument")
	}
	for _, argument := range value.Argv {
		if strings.TrimSpace(argument) == "" {
			return valueError("memory.reviewed_command.argv", "must not contain blank arguments")
		}
	}
	return value.Binding.Validate()
}

// FileToTestMappingContent is a file-to-test mapping (M21-004) observed from
// a successful validation.
type FileToTestMappingContent struct {
	Repository RepositoryID    `json:"repository"`
	SourcePath string          `json:"source_path"`
	TestPaths  []string        `json:"test_paths"`
	Binding    RevisionBinding `json:"binding"`
}

// Validate checks required fields, path well-formedness, and the revision binding.
func (value FileToTestMappingContent) Validate() error {
	switch {
	case value.Repository.IsZero():
		return valueError("memory.file_to_test_mapping.repository", "must not be empty")
	case strings.TrimSpace(value.SourcePath) == "":
		return valueError("memory.file_to_test_mapping.source_path", "must not be empty")
	case len(value.TestPaths) == 0:
		return valueError("memory.file_to_test_mapping.test_paths", "must contain at least one test path")
	}
	for _, path := range value.TestPaths {
		if strings.TrimSpace(path) == "" {
			return valueError("memory.file_to_test_mapping.test_paths", "must not contain blank paths")
		}
	}
	return value.Binding.Validate()
}

// RepositoryConventionContent is a repository convention (M21-005), such as
// a formatting or lint rule, scoped to a path or package and bound to the
// revision(s) at which it was accepted.
type RepositoryConventionContent struct {
	Repository RepositoryID    `json:"repository"`
	Scope      string          `json:"scope"`
	Statement  string          `json:"statement"`
	Binding    RevisionBinding `json:"binding"`
}

// Validate checks required fields and the revision binding.
func (value RepositoryConventionContent) Validate() error {
	switch {
	case value.Repository.IsZero():
		return valueError("memory.repository_convention.repository", "must not be empty")
	case strings.TrimSpace(value.Scope) == "":
		return valueError("memory.repository_convention.scope", "must not be empty")
	case strings.TrimSpace(value.Statement) == "":
		return valueError("memory.repository_convention.statement", "must not be empty")
	default:
		return value.Binding.Validate()
	}
}

// RegressionClassification classifies an accepted regression case.
type RegressionClassification string

const (
	RegressionClassificationSemantic                  RegressionClassification = "semantic"
	RegressionClassificationSafety                    RegressionClassification = "safety"
	RegressionClassificationVersionedSpecification    RegressionClassification = "versioned-specification"
	RegressionClassificationVersionedExternalContract RegressionClassification = "versioned-external-contract"
)

// IsValid reports whether the regression classification is declared.
func (value RegressionClassification) IsValid() bool {
	switch value {
	case RegressionClassificationSemantic,
		RegressionClassificationSafety,
		RegressionClassificationVersionedSpecification,
		RegressionClassificationVersionedExternalContract:
		return true
	default:
		return false
	}
}

// AcceptedRegressionCaseContent is an accepted regression case (M21-006).
// Per §31 Regression Cases, it requires a reproducible input, a stable or
// versioned oracle, a demonstrated failure (referenced by EvidenceID, not by
// prose), and an expected outcome; "an alleged anti-pattern without a
// reproducible failing case is stored only as an observation" (see
// ObservationHypothesisContent instead).
type AcceptedRegressionCaseContent struct {
	Repository          RepositoryID             `json:"repository"`
	Classification      RegressionClassification `json:"classification"`
	ReproducibleInput   string                   `json:"reproducible_input"`
	Oracle              string                   `json:"oracle"`
	DemonstratedFailure EvidenceID               `json:"demonstrated_failure"`
	ExpectedOutcome     string                   `json:"expected_outcome"`
	Binding             RevisionBinding          `json:"binding"`
}

// Validate checks required fields, the demonstrated-failure evidence link,
// and the revision binding.
func (value AcceptedRegressionCaseContent) Validate() error {
	switch {
	case value.Repository.IsZero():
		return valueError("memory.accepted_regression_case.repository", "must not be empty")
	case !value.Classification.IsValid():
		return valueError("memory.accepted_regression_case.classification", "is not declared")
	case strings.TrimSpace(value.ReproducibleInput) == "":
		return valueError("memory.accepted_regression_case.reproducible_input", "must not be empty")
	case strings.TrimSpace(value.Oracle) == "":
		return valueError("memory.accepted_regression_case.oracle", "must not be empty")
	case value.DemonstratedFailure.IsZero():
		return valueError("memory.accepted_regression_case.demonstrated_failure", "must reference the evidence that demonstrated the failure")
	case strings.TrimSpace(value.ExpectedOutcome) == "":
		return valueError("memory.accepted_regression_case.expected_outcome", "must not be empty")
	default:
		return value.Binding.Validate()
	}
}

// ExecutionRecipeContent is an execution recipe (M21-007). Structured,
// machine-checkable applicability predicates are M21-022's concern
// (applicability-predicate records); at this typed-value layer,
// ApplicabilityStatement is descriptive text that a later applicability-gate
// lane compiles or checks. ObservedPerformance reuses ForecastRange because
// a recipe's observed cost and latency, aggregated over its supporting
// episodes, are naturally the same P50/P90 known/unknown-flagged shape as a
// forecast.
type ExecutionRecipeContent struct {
	Repository                RepositoryID  `json:"repository"`
	ApplicabilityStatement    string        `json:"applicability_statement"`
	PlanSummary               string        `json:"plan_summary"`
	RequiredCommands          []string      `json:"required_commands"`
	ExpectedFailureSignals    []string      `json:"expected_failure_signals"`
	RequiredValidationProfile string        `json:"required_validation_profile"`
	ObservedPerformance       ForecastRange `json:"observed_performance"`
}

// Validate checks required fields and the observed-performance forecast shape.
func (value ExecutionRecipeContent) Validate() error {
	switch {
	case value.Repository.IsZero():
		return valueError("memory.execution_recipe.repository", "must not be empty")
	case strings.TrimSpace(value.ApplicabilityStatement) == "":
		return valueError("memory.execution_recipe.applicability_statement", "must not be empty")
	case strings.TrimSpace(value.PlanSummary) == "":
		return valueError("memory.execution_recipe.plan_summary", "must not be empty")
	case strings.TrimSpace(value.RequiredValidationProfile) == "":
		return valueError("memory.execution_recipe.required_validation_profile", "must not be empty")
	default:
		return value.ObservedPerformance.Validate()
	}
}

// ExecutableAtomReferenceContent is an executable atom reference (M21-008):
// an opaque, revision-bound pointer at an atom's stable identity and
// canonical name for provenance and source mapping, without requiring or
// depending on the deep semantic atom machinery gated behind §§6-20.
type ExecutableAtomReferenceContent struct {
	Repository    RepositoryID    `json:"repository"`
	Atom          AtomID          `json:"atom"`
	CanonicalName string          `json:"canonical_name"`
	Binding       RevisionBinding `json:"binding"`
}

// Validate checks required fields and the revision binding.
func (value ExecutableAtomReferenceContent) Validate() error {
	switch {
	case value.Repository.IsZero():
		return valueError("memory.executable_atom_reference.repository", "must not be empty")
	case value.Atom.IsZero():
		return valueError("memory.executable_atom_reference.atom", "must not be empty")
	case strings.TrimSpace(value.CanonicalName) == "":
		return valueError("memory.executable_atom_reference.canonical_name", "must not be empty")
	default:
		return value.Binding.Validate()
	}
}

// ObservationHypothesisContent is an observation/hypothesis (M21-009).
// Strength is required to equal EvidenceStrengthNone: per §31, "Unproven
// findings may be retained for offline research. They are not shown to
// working agents and do not affect generation," and an alleged anti-pattern
// without a reproducible failing case can only ever be stored here, never as
// an AcceptedRegressionCaseContent.
type ObservationHypothesisContent struct {
	Repository RepositoryID     `json:"repository"`
	Statement  string           `json:"statement"`
	Strength   EvidenceStrength `json:"strength"`
}

// NewObservationHypothesisContent builds an observation/hypothesis with its
// evidence strength fixed at EvidenceStrengthNone, regardless of caller
// intent, so that constructing one through the normal path can never claim
// stronger support than the plan allows.
func NewObservationHypothesisContent(repository RepositoryID, statement string) (ObservationHypothesisContent, error) {
	value := ObservationHypothesisContent{
		Repository: repository,
		Statement:  statement,
		Strength:   EvidenceStrengthNone,
	}
	if err := value.Validate(); err != nil {
		return ObservationHypothesisContent{}, err
	}
	return value, nil
}

// Validate checks required fields and that Strength is exactly EvidenceStrengthNone.
func (value ObservationHypothesisContent) Validate() error {
	switch {
	case strings.TrimSpace(value.Statement) == "":
		return valueError("memory.observation_hypothesis.statement", "must not be empty")
	case value.Strength != EvidenceStrengthNone:
		return valueError("memory.observation_hypothesis.strength", "an observation or hypothesis must always carry evidence_strength none")
	default:
		return nil
	}
}

// MemoryArtifactContent is a closed tagged union over the eight artifact
// kinds. Exactly one payload field is populated, matching Kind; use one of
// the New*MemoryContent constructors to build a consistent value.
type MemoryArtifactContent struct {
	Kind                   MemoryArtifactKind              `json:"kind"`
	RepositoryFact         *RepositoryFactContent          `json:"repository_fact,omitempty"`
	ReviewedCommand        *ReviewedCommandContent         `json:"reviewed_command,omitempty"`
	FileToTestMapping      *FileToTestMappingContent       `json:"file_to_test_mapping,omitempty"`
	RepositoryConvention   *RepositoryConventionContent    `json:"repository_convention,omitempty"`
	AcceptedRegressionCase *AcceptedRegressionCaseContent  `json:"accepted_regression_case,omitempty"`
	ExecutionRecipe        *ExecutionRecipeContent         `json:"execution_recipe,omitempty"`
	AtomReference          *ExecutableAtomReferenceContent `json:"atom_reference,omitempty"`
	ObservationHypothesis  *ObservationHypothesisContent   `json:"observation_hypothesis,omitempty"`
}

// Validate checks that Kind is declared, that exactly the matching payload
// field is populated, and that the populated payload itself is valid.
func (value MemoryArtifactContent) Validate() error {
	populated := 0
	if value.RepositoryFact != nil {
		populated++
	}
	if value.ReviewedCommand != nil {
		populated++
	}
	if value.FileToTestMapping != nil {
		populated++
	}
	if value.RepositoryConvention != nil {
		populated++
	}
	if value.AcceptedRegressionCase != nil {
		populated++
	}
	if value.ExecutionRecipe != nil {
		populated++
	}
	if value.AtomReference != nil {
		populated++
	}
	if value.ObservationHypothesis != nil {
		populated++
	}
	if populated != 1 {
		return valueError("memory.content", "must populate exactly one artifact-kind payload")
	}
	switch value.Kind {
	case MemoryArtifactKindRepositoryFact:
		if value.RepositoryFact == nil {
			return valueError("memory.content.kind", "repository-fact kind requires the repository_fact payload")
		}
		return value.RepositoryFact.Validate()
	case MemoryArtifactKindReviewedCommand:
		if value.ReviewedCommand == nil {
			return valueError("memory.content.kind", "reviewed-command kind requires the reviewed_command payload")
		}
		return value.ReviewedCommand.Validate()
	case MemoryArtifactKindFileToTestMapping:
		if value.FileToTestMapping == nil {
			return valueError("memory.content.kind", "file-to-test-mapping kind requires the file_to_test_mapping payload")
		}
		return value.FileToTestMapping.Validate()
	case MemoryArtifactKindRepositoryConvention:
		if value.RepositoryConvention == nil {
			return valueError("memory.content.kind", "repository-convention kind requires the repository_convention payload")
		}
		return value.RepositoryConvention.Validate()
	case MemoryArtifactKindAcceptedRegressionCase:
		if value.AcceptedRegressionCase == nil {
			return valueError("memory.content.kind", "accepted-regression-case kind requires the accepted_regression_case payload")
		}
		return value.AcceptedRegressionCase.Validate()
	case MemoryArtifactKindExecutionRecipe:
		if value.ExecutionRecipe == nil {
			return valueError("memory.content.kind", "execution-recipe kind requires the execution_recipe payload")
		}
		return value.ExecutionRecipe.Validate()
	case MemoryArtifactKindExecutableAtomReference:
		if value.AtomReference == nil {
			return valueError("memory.content.kind", "executable-atom-reference kind requires the atom_reference payload")
		}
		return value.AtomReference.Validate()
	case MemoryArtifactKindObservationHypothesis:
		if value.ObservationHypothesis == nil {
			return valueError("memory.content.kind", "observation-hypothesis kind requires the observation_hypothesis payload")
		}
		return value.ObservationHypothesis.Validate()
	default:
		return valueError("memory.content.kind", "is not declared")
	}
}

// NewRepositoryFactMemoryContent builds a consistent repository-fact content value (M21-002).
func NewRepositoryFactMemoryContent(payload RepositoryFactContent) (MemoryArtifactContent, error) {
	value := MemoryArtifactContent{Kind: MemoryArtifactKindRepositoryFact, RepositoryFact: &payload}
	return value, value.Validate()
}

// NewReviewedCommandMemoryContent builds a consistent reviewed-command content value (M21-003).
func NewReviewedCommandMemoryContent(payload ReviewedCommandContent) (MemoryArtifactContent, error) {
	value := MemoryArtifactContent{Kind: MemoryArtifactKindReviewedCommand, ReviewedCommand: &payload}
	return value, value.Validate()
}

// NewFileToTestMappingMemoryContent builds a consistent file-to-test-mapping content value (M21-004).
func NewFileToTestMappingMemoryContent(payload FileToTestMappingContent) (MemoryArtifactContent, error) {
	value := MemoryArtifactContent{Kind: MemoryArtifactKindFileToTestMapping, FileToTestMapping: &payload}
	return value, value.Validate()
}

// NewRepositoryConventionMemoryContent builds a consistent repository-convention content value (M21-005).
func NewRepositoryConventionMemoryContent(payload RepositoryConventionContent) (MemoryArtifactContent, error) {
	value := MemoryArtifactContent{Kind: MemoryArtifactKindRepositoryConvention, RepositoryConvention: &payload}
	return value, value.Validate()
}

// NewAcceptedRegressionCaseMemoryContent builds a consistent accepted-regression-case content value (M21-006).
func NewAcceptedRegressionCaseMemoryContent(payload AcceptedRegressionCaseContent) (MemoryArtifactContent, error) {
	value := MemoryArtifactContent{Kind: MemoryArtifactKindAcceptedRegressionCase, AcceptedRegressionCase: &payload}
	return value, value.Validate()
}

// NewExecutionRecipeMemoryContent builds a consistent execution-recipe content value (M21-007).
func NewExecutionRecipeMemoryContent(payload ExecutionRecipeContent) (MemoryArtifactContent, error) {
	value := MemoryArtifactContent{Kind: MemoryArtifactKindExecutionRecipe, ExecutionRecipe: &payload}
	return value, value.Validate()
}

// NewExecutableAtomReferenceMemoryContent builds a consistent executable-atom-reference content value (M21-008).
func NewExecutableAtomReferenceMemoryContent(payload ExecutableAtomReferenceContent) (MemoryArtifactContent, error) {
	value := MemoryArtifactContent{Kind: MemoryArtifactKindExecutableAtomReference, AtomReference: &payload}
	return value, value.Validate()
}

// NewObservationHypothesisMemoryContent builds a consistent observation/hypothesis content value (M21-009).
func NewObservationHypothesisMemoryContent(payload ObservationHypothesisContent) (MemoryArtifactContent, error) {
	value := MemoryArtifactContent{Kind: MemoryArtifactKindObservationHypothesis, ObservationHypothesis: &payload}
	return value, value.Validate()
}

// -----------------------------------------------------------------------
// Memory artifact revision (M21-001, M21-010, M21-013)
// -----------------------------------------------------------------------

// MemoryArtifactRevision is one immutable revision of a memory artifact: its
// stable identity, project ownership, current governed maturity, and typed
// content. A user correction (see ReviseMemoryArtifactWithUserCorrection)
// always produces a new MemoryArtifactRevision with a new RevisionID; it
// never mutates this one.
type MemoryArtifactRevision struct {
	ArtifactID            MemoryArtifactID         `json:"artifact_id"`
	RevisionID            MemoryArtifactRevisionID `json:"revision_id"`
	Scope                 MemoryProjectScope       `json:"scope"`
	Maturity              MaturityState            `json:"maturity"`
	Content               MemoryArtifactContent    `json:"content"`
	SupersedesRevision    MemoryArtifactRevisionID `json:"supersedes_revision"`
	CreatedFromCorrection bool                     `json:"created_from_correction"`
}

// NewMemoryArtifactRevision creates the first revision of a memory artifact.
// It always starts at MaturityStateCandidate: authority is earned
// afterward, never assumed at creation.
func NewMemoryArtifactRevision(
	artifact MemoryArtifactID,
	revision MemoryArtifactRevisionID,
	scope MemoryProjectScope,
	content MemoryArtifactContent,
) (MemoryArtifactRevision, error) {
	switch {
	case artifact.IsZero():
		return MemoryArtifactRevision{}, valueError("memory.artifact.artifact_id", "must not be empty")
	case revision.IsZero():
		return MemoryArtifactRevision{}, valueError("memory.artifact.revision_id", "must not be empty")
	}
	if err := scope.Validate(); err != nil {
		return MemoryArtifactRevision{}, err
	}
	if err := content.Validate(); err != nil {
		return MemoryArtifactRevision{}, err
	}
	return MemoryArtifactRevision{
		ArtifactID: artifact,
		RevisionID: revision,
		Scope:      scope,
		Maturity:   MaturityStateCandidate,
		Content:    content,
	}, nil
}

// TransitionMemoryArtifactMaturity moves revision to a new maturity state,
// requiring proof whenever the destination is authority-bearing.
func TransitionMemoryArtifactMaturity(
	revision MemoryArtifactRevision,
	to MaturityState,
	proof authorityProof,
) (MemoryArtifactRevision, error) {
	if err := ValidateMemoryArtifactMaturityTransition(MaturityTransitionRequest{
		From:     revision.Maturity,
		To:       to,
		Revision: revision.RevisionID,
		Proof:    proof,
	}); err != nil {
		return MemoryArtifactRevision{}, err
	}
	updated := revision
	updated.Maturity = to
	return updated, nil
}

// ReviseMemoryArtifactWithUserCorrection creates a new immutable revision
// that supersedes prior without mutating it, per AGENTS.md "Preserve
// immutable historical records; represent later correction through
// revisions or invalidation overlays" and M21-013's requirement that
// correction creates a new revision rather than mutating history. The new
// revision always starts back at MaturityStateCandidate: corrected content
// has not yet earned the evidence that supported the prior revision's
// maturity, so authority is never silently carried forward.
//
// Per §31 Artifact Failure Protocol, "Quarantine is terminal for the exposed
// lesson version. A failed lesson is not repaired in place or restored with
// inherited authority. An independently derived successor receives a new
// identity, no inherited confidence, the original counterexample, and fresh
// admission requirements." Correction is a repair-in-place operation under
// the SAME MemoryArtifactID, so it is refused whenever prior's maturity can
// no longer reach an authority-bearing state -- Quarantined, Invalidated, or
// Retired (see MaturityState.CanReachAuthority) -- with
// ErrMemoryArtifactCorrectionRequiresNewIdentity. This is deliberately a
// hard rejection rather than, say, silently minting a new MemoryArtifactID
// on the caller's behalf: §31 requires a successor to be "independently
// derived" with "fresh admission requirements," which this function cannot
// establish on its own; the caller must go through NewMemoryArtifactRevision
// with a new identity instead.
func ReviseMemoryArtifactWithUserCorrection(
	prior MemoryArtifactRevision,
	newRevisionID MemoryArtifactRevisionID,
	correctedContent MemoryArtifactContent,
) (MemoryArtifactRevision, error) {
	switch {
	case prior.ArtifactID.IsZero() || prior.RevisionID.IsZero():
		return MemoryArtifactRevision{}, valueError("memory.correction.prior", "must reference a real prior revision")
	case newRevisionID.IsZero():
		return MemoryArtifactRevision{}, valueError("memory.correction.revision_id", "must not be empty")
	case newRevisionID == prior.RevisionID:
		return MemoryArtifactRevision{}, valueError("memory.correction.revision_id", "must be a new, distinct revision identity")
	case !prior.Maturity.IsValid():
		return MemoryArtifactRevision{}, valueError("memory.correction.prior.maturity", "must be a declared maturity state")
	case !prior.Maturity.CanReachAuthority():
		return MemoryArtifactRevision{}, fmt.Errorf(
			"%w: prior revision %s has maturity %q, which cannot reach an authority-bearing state; create a new memory artifact identity instead of correcting artifact %s in place",
			ErrMemoryArtifactCorrectionRequiresNewIdentity, prior.RevisionID, prior.Maturity, prior.ArtifactID,
		)
	}
	if err := correctedContent.Validate(); err != nil {
		return MemoryArtifactRevision{}, err
	}
	revised := prior
	revised.RevisionID = newRevisionID
	revised.Content = correctedContent
	revised.SupersedesRevision = prior.RevisionID
	revised.CreatedFromCorrection = true
	revised.Maturity = MaturityStateCandidate
	return revised, nil
}

// -----------------------------------------------------------------------
// Influence lineage (§31 Influence Lineage, Descendant Contamination)
// -----------------------------------------------------------------------

// MemoryArtifactLineage records how one memory-artifact revision relates to
// its evidence ancestry: derived_from is a semantic dependency, while
// influenced_by is contextual exposure without a mechanical dependency. An
// artifact cannot claim both relationships to the same ancestor, and it
// cannot list itself as its own ancestor.
//
// OriginArtifactID and LineageRootIDs each carry a Known/UnknownReason
// companion, matching the package-wide rule already applied to
// RevisionBinding: an absent origin or root set is never represented by a
// bare empty value with no explanation attached, because "empty" is
// ambiguous between "known: this artifact has no origin/root ancestor
// (it is one itself)" and "we have not determined the origin/roots yet."
// AGENTS.md Frontend Rules: "Never display unknown cost as zero" applies
// here by the same logic -- a caller (e.g. web/frontend/memoryinspector)
// must be able to tell the two cases apart and say so, rather than
// rendering a bare dash or a silently empty list.
type MemoryArtifactLineage struct {
	ArtifactID   MemoryArtifactID   `json:"artifact_id"`
	DerivedFrom  []MemoryArtifactID `json:"derived_from"`
	InfluencedBy []MemoryArtifactID `json:"influenced_by"`

	// OriginArtifactID, when OriginKnown is true, names the ultimate
	// ancestor at the root of this artifact's derived_from chain (the zero
	// MemoryArtifactID legitimately means "this artifact is its own
	// origin: it has no derived_from ancestor"). When OriginKnown is
	// false, OriginArtifactID must be zero and OriginUnknownReason must
	// explain why the origin has not been determined.
	OriginArtifactID    MemoryArtifactID `json:"origin_artifact_id"`
	OriginKnown         bool             `json:"origin_known"`
	OriginUnknownReason string           `json:"origin_unknown_reason"`

	SupportingEpisodes []EpisodeID `json:"supporting_episode_ids"`

	// LineageRootIDs, when LineageRootsKnown is true, names every artifact
	// at the root of this artifact's derived_from/influenced_by ancestry
	// (an empty slice legitimately means "this artifact is itself the only
	// root"). When LineageRootsKnown is false, LineageRootIDs must be
	// empty and LineageRootsUnknownReason must explain why the roots have
	// not been determined.
	LineageRootIDs            []MemoryArtifactID `json:"lineage_root_ids"`
	LineageRootsKnown         bool               `json:"lineage_roots_known"`
	LineageRootsUnknownReason string             `json:"lineage_roots_unknown_reason"`
}

// Validate checks the lineage is internally consistent.
func (value MemoryArtifactLineage) Validate() error {
	if value.ArtifactID.IsZero() {
		return valueError("memory.lineage.artifact_id", "must not be empty")
	}
	derivedFrom := map[MemoryArtifactID]bool{}
	for _, id := range value.DerivedFrom {
		if id.IsZero() {
			return valueError("memory.lineage.derived_from", "must not contain empty identities")
		}
		if id == value.ArtifactID {
			return valueError("memory.lineage.derived_from", "must not reference itself")
		}
		derivedFrom[id] = true
	}
	for _, id := range value.InfluencedBy {
		if id.IsZero() {
			return valueError("memory.lineage.influenced_by", "must not contain empty identities")
		}
		if id == value.ArtifactID {
			return valueError("memory.lineage.influenced_by", "must not reference itself")
		}
		if derivedFrom[id] {
			return valueError("memory.lineage", "an artifact cannot be both derived_from and influenced_by the same ancestor")
		}
	}
	for _, id := range value.SupportingEpisodes {
		if id.IsZero() {
			return valueError("memory.lineage.supporting_episode_ids", "must not contain empty identities")
		}
	}
	if value.OriginKnown {
		if strings.TrimSpace(value.OriginUnknownReason) != "" {
			return valueError("memory.lineage.origin_unknown_reason", "known origin must not carry an unknown reason")
		}
	} else if strings.TrimSpace(value.OriginUnknownReason) == "" {
		return valueError("memory.lineage.origin_unknown_reason", "must explain why the origin artifact is unknown")
	} else if !value.OriginArtifactID.IsZero() {
		return valueError("memory.lineage.origin_artifact_id", "unknown origin must not carry an origin artifact identity")
	}
	if value.LineageRootsKnown {
		if strings.TrimSpace(value.LineageRootsUnknownReason) != "" {
			return valueError("memory.lineage.lineage_roots_unknown_reason", "known lineage roots must not carry an unknown reason")
		}
	} else if strings.TrimSpace(value.LineageRootsUnknownReason) == "" {
		return valueError("memory.lineage.lineage_roots_unknown_reason", "must explain why the lineage roots are unknown")
	} else if len(value.LineageRootIDs) != 0 {
		return valueError("memory.lineage.lineage_root_ids", "unknown lineage roots must not carry root identities")
	}
	return nil
}

// EvidenceFamily returns value's artifact ID together with every ancestor
// reachable through DerivedFrom or InfluencedBy edges, transitively, using
// index to look up each ancestor's own lineage. Per §31, "Artifacts and
// episodes from the same influence lineage count as one evidence family."
func (value MemoryArtifactLineage) EvidenceFamily(index map[MemoryArtifactID]MemoryArtifactLineage) map[MemoryArtifactID]struct{} {
	family := map[MemoryArtifactID]struct{}{value.ArtifactID: {}}
	queue := append([]MemoryArtifactID(nil), value.DerivedFrom...)
	queue = append(queue, value.InfluencedBy...)
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if _, seen := family[next]; seen {
			continue
		}
		family[next] = struct{}{}
		if related, ok := index[next]; ok {
			queue = append(queue, related.DerivedFrom...)
			queue = append(queue, related.InfluencedBy...)
		}
	}
	return family
}

// ConfirmsMemoryArtifactIndependently reports whether candidateEpisodes can
// serve as independent confirmation of ancestor. Per §31, "Descendants of a
// pattern do not independently confirm their ancestor. Independent
// confirmation must come from episodes never exposed to that lineage."
// candidateEpisodes confirms independently only if it contains at least one
// episode that never supported any artifact in ancestor's evidence family
// (ancestor's own DerivedFrom/InfluencedBy closure).
func ConfirmsMemoryArtifactIndependently(
	ancestor MemoryArtifactID,
	index map[MemoryArtifactID]MemoryArtifactLineage,
	candidateEpisodes []EpisodeID,
) (bool, error) {
	lineage, ok := index[ancestor]
	if !ok {
		return false, valueError("memory.lineage.ancestor", "must be present in the lineage index")
	}
	family := lineage.EvidenceFamily(index)
	exposed := map[EpisodeID]struct{}{}
	for id := range family {
		if related, ok := index[id]; ok {
			for _, episode := range related.SupportingEpisodes {
				exposed[episode] = struct{}{}
			}
		}
	}
	for _, episode := range candidateEpisodes {
		if episode.IsZero() {
			return false, valueError("memory.lineage.candidate_episodes", "must not contain empty identities")
		}
		if _, isExposed := exposed[episode]; !isExposed {
			return true, nil
		}
	}
	return false, nil
}

// -----------------------------------------------------------------------
// Inspection, correction, export, and deletion semantics (M21-013)
// -----------------------------------------------------------------------

// MemoryArtifactExportRecord is the structured, secret-free projection of
// one memory-artifact revision suitable for user export. Every field is an
// ordinary domain value; memory-artifact content never carries a credential
// field per AGENTS.md "Never add a credential column," so this allowlist
// projection requires no additional redaction pass.
type MemoryArtifactExportRecord struct {
	ArtifactID MemoryArtifactID         `json:"artifact_id"`
	RevisionID MemoryArtifactRevisionID `json:"revision_id"`
	Scope      MemoryProjectScope       `json:"scope"`
	Maturity   MaturityState            `json:"maturity"`
	Content    MemoryArtifactContent    `json:"content"`
	Lineage    MemoryArtifactLineage    `json:"lineage"`
}

// NewMemoryArtifactExportRecord builds an export record after checking that
// revision and lineage describe the same artifact.
func NewMemoryArtifactExportRecord(
	revision MemoryArtifactRevision,
	lineage MemoryArtifactLineage,
) (MemoryArtifactExportRecord, error) {
	if revision.ArtifactID.IsZero() {
		return MemoryArtifactExportRecord{}, valueError("memory.export.artifact_id", "must not be empty")
	}
	if lineage.ArtifactID != revision.ArtifactID {
		return MemoryArtifactExportRecord{}, valueError("memory.export.lineage", "must describe the same artifact as revision")
	}
	if err := lineage.Validate(); err != nil {
		return MemoryArtifactExportRecord{}, err
	}
	return MemoryArtifactExportRecord{
		ArtifactID: revision.ArtifactID,
		RevisionID: revision.RevisionID,
		Scope:      revision.Scope,
		Maturity:   revision.Maturity,
		Content:    revision.Content,
		Lineage:    lineage,
	}, nil
}

// MemoryArtifactDeletionPreview summarizes what deleting target would
// affect, computed without mutating anything. Per M21-013, "deletion must be
// able to preview affected descendants" before it happens. DirectDependents
// is the full transitive derived_from closure (every artifact reachable by
// repeatedly following "X derived_from target-or-an-already-found-
// dependent"), matching what internal/storage's cascadeMemoryArtifactDerivedFrom
// actually deletes -- not merely artifacts one hop away. The field name is
// kept for API/wire continuity with existing callers; the semantics were
// widened from one-hop to transitive by this fix, so any caller assuming
// "one hop only" was relying on a defect, not a guarantee.
type MemoryArtifactDeletionPreview struct {
	Target                 MemoryArtifactID   `json:"target"`
	DirectDependents       []MemoryArtifactID `json:"direct_dependents"`
	ContextuallyInfluenced []MemoryArtifactID `json:"contextually_influenced"`
}

// PreviewMemoryArtifactDeletion computes target's full transitive semantic
// dependents (every artifact reachable by following derived_from edges back
// to target, however many hops away) and target's directly contextually
// influenced descendants (influenced_by == target) from index, in
// deterministic identity order. Per §31 Descendant Contamination, direct
// semantic dependents need explicit handling before deletion -- and because
// cascade deletion (internal/storage's cascadeMemoryArtifactDerivedFrom)
// follows derived_from transitively, this preview must too, or a caller
// could be shown and asked to acknowledge only the first hop while deeper
// dependents are silently deleted underneath them. influenced_by
// descendants remain direct-only: per §31 they are "only flagged, not
// automatically removed," so unlike derived_from they are never cascaded
// and a transitive influenced_by walk would overstate what deletion
// actually touches. The traversal is cycle-safe (a visited set bounds it to
// at most len(index) descendants regardless of any derived_from cycle
// present in index).
func PreviewMemoryArtifactDeletion(
	target MemoryArtifactID,
	index map[MemoryArtifactID]MemoryArtifactLineage,
) (MemoryArtifactDeletionPreview, error) {
	if target.IsZero() {
		return MemoryArtifactDeletionPreview{}, valueError("memory.deletion.target", "must not be empty")
	}
	ids := make([]MemoryArtifactID, 0, len(index))
	for id := range index {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })

	// dependentsOf[ancestor] lists every artifact whose lineage names
	// ancestor directly in DerivedFrom, built once so the transitive walk
	// below never re-scans the whole index per hop.
	dependentsOf := map[MemoryArtifactID][]MemoryArtifactID{}
	for _, id := range ids {
		for _, ancestor := range index[id].DerivedFrom {
			dependentsOf[ancestor] = append(dependentsOf[ancestor], id)
		}
	}

	visited := map[MemoryArtifactID]struct{}{target: {}}
	var transitiveDependents []MemoryArtifactID
	queue := []MemoryArtifactID{target}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, dependent := range dependentsOf[current] {
			if _, seen := visited[dependent]; seen {
				continue
			}
			visited[dependent] = struct{}{}
			transitiveDependents = append(transitiveDependents, dependent)
			queue = append(queue, dependent)
		}
	}
	sort.Slice(transitiveDependents, func(i, j int) bool {
		return transitiveDependents[i].String() < transitiveDependents[j].String()
	})

	preview := MemoryArtifactDeletionPreview{Target: target, DirectDependents: transitiveDependents}
	for _, id := range ids {
		for _, influence := range index[id].InfluencedBy {
			if influence == target {
				preview.ContextuallyInfluenced = append(preview.ContextuallyInfluenced, id)
				break
			}
		}
	}
	return preview, nil
}

// MemoryArtifactDeletionRequest carries a user's deletion intent together
// with explicit acknowledgement of the previewed impact. AcknowledgedDependents
// names exactly the dependent identities the caller acknowledged, rather
// than a bare bool: ValidateMemoryArtifactDeletion requires this set to
// equal the freshly recomputed DirectDependents, so an acknowledgement can
// never be satisfied by a stale or forged smaller set silently passing as
// "true".
type MemoryArtifactDeletionRequest struct {
	Target                 MemoryArtifactID              `json:"target"`
	Preview                MemoryArtifactDeletionPreview `json:"preview"`
	AcknowledgedDependents []MemoryArtifactID            `json:"acknowledged_dependents"`
}

// ValidateMemoryArtifactDeletion checks that request describes a real
// target, recomputes the deletion preview from currentIndex, and rejects
// the request unless request.Preview exactly matches that fresh
// recomputation -- the freshness/derivation binding that closes both the
// "forgeable" and "stale" gaps in a caller-supplied Preview: a
// hand-built or previously-fetched Preview claiming fewer dependents than
// currentIndex actually shows is rejected, it is never merely trusted. It
// also requires AcknowledgedDependents to name exactly the (freshly
// recomputed) DirectDependents set, so acknowledgement is bound to the real
// identities the caller was shown, not a bare bool a caller could set
// regardless of what they actually saw.
//
// Storage callers: pass the same lineage index used to compute (or that
// would currently compute) the preview for target, loaded within the same
// transaction as the deletion itself, so currentIndex reflects the state
// the deletion is about to act on.
func ValidateMemoryArtifactDeletion(
	request MemoryArtifactDeletionRequest,
	currentIndex map[MemoryArtifactID]MemoryArtifactLineage,
) error {
	if request.Target.IsZero() {
		return valueError("memory.deletion.target", "must not be empty")
	}
	if request.Preview.Target != request.Target {
		return valueError("memory.deletion.preview", "must describe the same target as the request")
	}
	current, err := PreviewMemoryArtifactDeletion(request.Target, currentIndex)
	if err != nil {
		return err
	}
	if !sameMemoryArtifactIDSet(current.DirectDependents, request.Preview.DirectDependents) ||
		!sameMemoryArtifactIDSet(current.ContextuallyInfluenced, request.Preview.ContextuallyInfluenced) {
		return valueError("memory.deletion.preview", "does not match the current lineage; recompute the preview before deleting")
	}
	if len(current.DirectDependents) > 0 && !sameMemoryArtifactIDSet(request.AcknowledgedDependents, current.DirectDependents) {
		return valueError("memory.deletion.dependents", "direct semantic dependents must be previewed and acknowledged, by identity, before deletion")
	}
	return nil
}

// sameMemoryArtifactIDSet reports whether a and b contain exactly the same
// MemoryArtifactID values, ignoring order and duplicate entries.
func sameMemoryArtifactIDSet(a, b []MemoryArtifactID) bool {
	toSet := func(values []MemoryArtifactID) map[MemoryArtifactID]struct{} {
		set := make(map[MemoryArtifactID]struct{}, len(values))
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
