package retrieval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/retrievalgate"
	"codeflux.dev/codeflux/internal/storage"
)

// MaximumPresentedItems bounds how many eligible candidates
// PreWorkGateResult.Eligible ever carries (M21-073: "Return a typed,
// bounded result the frontend can render"). Candidates beyond this bound
// are still discovered, evaluated, and logged (Considered still counts
// them); they are simply not surfaced for presentation.
const MaximumPresentedItems = 20

// store is the narrow slice of *storage.Repositories this package needs,
// named and shaped like internal/coordinator's taskPreflightStore
// convention. It exists so tests can substitute a fake without spinning up
// a real database for pure orchestration logic, and so this package's
// storage dependency stays explicit and auditable.
type store interface {
	CreateMemoryRetrievalQuery(context.Context, storage.CreateMemoryRetrievalQuery) error
	CreateMemoryRetrievalCandidate(context.Context, storage.CreateMemoryRetrievalCandidate) error
	CreateMemoryRetrievalDecision(context.Context, storage.CreateMemoryRetrievalDecision) error
	CreateMemoryRetrievalFallback(context.Context, storage.CreateMemoryRetrievalFallback) (storage.MemoryRetrievalFallback, error)

	ListEpisodesByFingerprintHash(context.Context, domain.ProjectID, string) ([]storage.Episode, error)
	ListMemoryArtifactsSupportedByEpisode(context.Context, domain.EpisodeID) ([]domain.MemoryArtifactID, error)
	MemoryArtifactIsDeleted(context.Context, domain.MemoryArtifactID) (bool, error)
	GetLatestMemoryArtifactRevision(context.Context, domain.MemoryArtifactID) (storage.MemoryArtifactRevisionRecord, error)
	ListMemoryArtifactRevisionsByRepositoryAndKind(context.Context, domain.ProjectID, domain.RepositoryID, domain.MemoryArtifactKind) ([]storage.MemoryArtifactRevisionRecord, error)

	ListMemoryArtifactSupportingEvidence(context.Context, domain.MemoryArtifactRevisionID) ([]storage.MemoryArtifactSupportingEvidence, error)
	GetEvidenceAssuranceLevel(context.Context, domain.EvidenceID) (domain.AssuranceLevel, error)
	ListMemoryArtifactApplicabilityPredicates(context.Context, domain.MemoryArtifactRevisionID) ([]storage.MemoryArtifactApplicabilityPredicate, error)

	// ListAtomNamesByProject and ListActiveAtomNameAliasesByProject back
	// discoverByAtomName (atom_name_discovery.go, M21-167): searching every
	// currently valid canonical name, normalized phrase, and active alias in
	// the task's project against the task's affected-symbol hints, before
	// any vector-similarity channel could ever run.
	ListAtomNamesByProject(context.Context, domain.ProjectID) ([]storage.AtomNameRevision, error)
	ListActiveAtomNameAliasesByProject(context.Context, domain.ProjectID) ([]storage.AtomNameAlias, error)

	// ListAtomDocumentationDiscoveryTextByProject backs
	// discoverByAtomDocumentation (atom_documentation_discovery.go,
	// M21-G07): the retrieval-bearing prose of every admitted atom
	// documentation revision, so rich comments improve what is discovered.
	// It returns no assurance, maturity, or applicability field, so it
	// cannot influence what is eligible.
	ListAtomDocumentationDiscoveryTextByProject(context.Context, domain.ProjectID) ([]storage.AtomDocumentationDiscoveryText, error)
}

// Service is the pre-work retrieval gate (M21-064..076). Construct one with
// NewService over a *storage.Repositories.
type Service struct {
	store store

	// similarityHook is an unexported, always-nil-in-production seam for a
	// future vector-similarity discovery phase (docs/plan.md §31 "vector
	// similarity for candidate recall"; still closed at the docs/plan.md §0
	// branch gate -- "do not implement similarity"). NewService never sets
	// it, no other production code in this repository can reach an
	// unexported field to set it, and this package's own tests are the only
	// place it is ever assigned (see
	// TestDiscoverCandidatesOnlyAddsSimilarityAfterNameAndStructuredChannels
	// in atom_name_discovery_test.go). When discoverCandidates runs, this
	// hook is invoked, if non-nil, strictly AFTER every exact-identity,
	// structured-field, and name/alias channel above -- proving, at the
	// call-order level, that a similarity channel could only ever ADD to
	// the candidate set already discovered, never precede, reorder, or
	// replace it (M21-167).
	similarityHook func(ctx context.Context, input PreWorkGateInput) ([]similarityDiscovery, error)
}

// similarityDiscovery is the payload shape a future vector-similarity
// discovery phase would report for one candidate: which revision, and its
// similarity score. It exists solely so similarityHook has something to
// return; nothing in this package computes one.
type similarityDiscovery struct {
	Revision storage.MemoryArtifactRevisionRecord
	Score    float64
}

// NewService builds a Service over repositories. repositories must not be
// nil.
func NewService(repositories *storage.Repositories) (*Service, error) {
	if repositories == nil {
		return nil, errors.New("retrieval service requires repositories")
	}
	return &Service{store: repositories}, nil
}

// PreWorkGateInput is everything RunPreWorkGate needs to consult project
// memory before a task's planning flow generates a solution from scratch.
type PreWorkGateInput struct {
	// QueryID is the caller-supplied, idempotent identity for this
	// retrieval pass (storage.CreateMemoryRetrievalQuery.ID). Re-running
	// RunPreWorkGate with the same QueryID for a query already logged will
	// fail on the query-uniqueness constraint; callers mint a fresh QueryID
	// per pre-work retrieval attempt.
	QueryID string
	// ProjectID is the project this retrieval is scoped to. It must equal
	// Boundary.Project.
	ProjectID domain.ProjectID
	// TaskID is the task this retrieval is being run for, when one already
	// exists. It is optional (the zero value omits the query's task
	// linkage) because a caller may want to retrieve before a task record
	// is durably created.
	TaskID domain.TaskID
	// Boundary is the explicit project-isolation predicate every retrieved
	// candidate is checked against (AGENTS.md "Add explicit project-
	// boundary predicates to memory, graph, vector, and retrieval
	// queries").
	Boundary domain.MemoryQueryProjectBoundary
	// Task is the current task's own exact fingerprint: identity, the
	// exact-match lookup key (M21-064), and every eligibility input
	// (M21-065..067, and internal/retrievalgate's toolchain/dependency/
	// applicability/evidence/assurance gates).
	Task fingerprint.ExactFingerprint
}

// InfluentialMemoryItem is one ELIGIBLE retrieval candidate, presented for
// the user/agent to see before planning (M21-073). Eligible is not yet
// influential: RunPreWorkGate never records a decision for it, and
// RecordInfluence (M21-074, M21-075) is the separate, later fact of what
// the agent actually did with it.
type InfluentialMemoryItem struct {
	CandidateID string
	RevisionID  domain.MemoryArtifactRevisionID
	Kind        domain.MemoryArtifactKind
	Channels    []storage.MemoryRetrievalCandidateSource

	// MatchedText carries, for each channel in Channels that was discovered
	// by matching a specific atom canonical name, normalized phrase, or
	// active alias against the task's affected-symbol hints (M21-168), the
	// exact text that matched -- so a reviewer can see WHY this candidate
	// entered the set, not merely which channel found it. A channel with no
	// single matched text to report (the M21-064 fingerprint-hash exact
	// match, or a bare repository+kind structured filter with no name
	// involved) has no entry here. Durable persistence of this attribution
	// is currently limited to this in-process result: the storage schema
	// (migration 000028's memory_retrieval_candidate_channels) records the
	// channel set losslessly but has no column for the matched text itself;
	// see this package's doc comment and the M21-168 report for the exact
	// additive column that would be needed to persist it durably.
	MatchedText map[storage.MemoryRetrievalCandidateSource]string

	// decision is the already-computed EligibilityDecision that made this
	// item eligible. It is unexported: a caller cannot construct or alter
	// it, only round-trip the exact InfluentialMemoryItem RunPreWorkGate
	// returned into RecordInfluence, so RecordInfluence can never be
	// tricked into recording influence for a decision it did not itself
	// compute.
	decision retrievalgate.EligibilityDecision
}

// PreWorkGateResult is RunPreWorkGate's terminal, typed result.
type PreWorkGateResult struct {
	QueryID string
	// Considered is the total number of distinct candidates discovered and
	// run through the eligibility gates, whether or not they were
	// eligible or made the bounded Eligible list below.
	Considered int
	// Eligible is the bounded, typed list of candidates presented for use
	// (M21-073), capped at MaximumPresentedItems.
	Eligible []InfluentialMemoryItem
	// FellBack is true when zero candidates were eligible: the caller must
	// proceed with ordinary planning from scratch (M21-076). This is a
	// normal outcome, never an error, and is durably recorded via
	// storage.CreateMemoryRetrievalFallback before RunPreWorkGate returns.
	FellBack bool
}

// discoveredEntry accumulates every discovery channel that found the same
// memory-artifact revision in one retrieval pass (M21-137: "several
// channels").
type discoveredEntry struct {
	revision        storage.MemoryArtifactRevisionRecord
	channels        map[storage.MemoryRetrievalCandidateSource]struct{}
	similarityScore *float64
	// matchedText records, per channel, the exact atom canonical name,
	// normalized phrase, or active alias text that caused this candidate to
	// be discovered through that channel (M21-168), when the channel came
	// from atom name/alias matching (see discoverByAtomName in
	// atom_name_discovery.go). Channels produced by every other discovery
	// phase (the M21-064 fingerprint-hash lookup, or a bare repository+kind
	// structured filter) have no entry here: there is no single matched
	// text to report for those.
	matchedText map[storage.MemoryRetrievalCandidateSource]string
}

// RunPreWorkGate is the pre-work retrieval gate's entry point (M21-064
// BLOCKER): "Run exact identity/fingerprint lookup before planning from
// scratch." It must be called before a task's planning flow generates a
// solution, not as an afterthought. See the package doc for the full
// discovery-then-eligibility-then-logging sequence.
func (service *Service) RunPreWorkGate(ctx context.Context, input PreWorkGateInput) (PreWorkGateResult, error) {
	if service == nil || service.store == nil {
		return PreWorkGateResult{}, errors.New("retrieval service is unavailable")
	}
	if input.QueryID == "" {
		return PreWorkGateResult{}, errors.New("retrieval query ID must not be empty")
	}
	if input.ProjectID.IsZero() {
		return PreWorkGateResult{}, errors.New("project ID must not be empty")
	}
	if err := input.Boundary.Validate(); err != nil {
		return PreWorkGateResult{}, err
	}
	if input.Boundary.Project != input.ProjectID {
		return PreWorkGateResult{}, errors.New("project boundary must match the retrieval query's project")
	}
	if err := input.Task.Validate(); err != nil {
		return PreWorkGateResult{}, err
	}

	order, index, err := service.discoverCandidates(ctx, input)
	if err != nil {
		return PreWorkGateResult{}, err
	}

	queryKind := storage.RetrievalQueryApplicabilityFilter
	for _, revisionID := range order {
		if _, ok := index[revisionID].channels[storage.RetrievalCandidateExactMatch]; ok {
			queryKind = storage.RetrievalQueryExactIdentity
			break
		}
	}
	var taskID *domain.TaskID
	if !input.TaskID.IsZero() {
		id := input.TaskID
		taskID = &id
	}
	if err := service.store.CreateMemoryRetrievalQuery(ctx, storage.CreateMemoryRetrievalQuery{
		ID: input.QueryID, ProjectID: input.ProjectID, TaskID: taskID,
		FingerprintSchemaVersion: int(input.Task.SchemaVersion), QueryKind: queryKind,
	}); err != nil {
		return PreWorkGateResult{}, err
	}

	result := PreWorkGateResult{QueryID: input.QueryID}
	rank := 0
	for _, revisionID := range order {
		rank++
		entry := index[revisionID]
		channels := sortedChannels(entry.channels)
		candidateID := deriveCandidateID(input.QueryID, revisionID)

		candidate, err := service.buildEligibilityCandidate(ctx, entry.revision, input.ProjectID)
		if err != nil {
			return PreWorkGateResult{}, err
		}
		decision, err := retrievalgate.EvaluateEligibility(candidate, input.Boundary, input.Task)
		if err != nil {
			return PreWorkGateResult{}, err
		}

		discovery := retrievalgate.DiscoveredCandidate{
			RevisionID: revisionID, Channels: toGateChannels(channels), SimilarityScore: entry.similarityScore,
		}
		if err := discovery.Validate(); err != nil {
			return PreWorkGateResult{}, err
		}

		if err := service.store.CreateMemoryRetrievalCandidate(ctx, storage.CreateMemoryRetrievalCandidate{
			ID: candidateID, QueryID: input.QueryID, RevisionID: revisionID, Rank: rank,
			Channels: channels, SimilarityScore: entry.similarityScore,
		}); err != nil {
			return PreWorkGateResult{}, err
		}
		result.Considered++

		if !decision.Eligible {
			detail := boundedReasonDetail(decision.Detail)
			if err := service.store.CreateMemoryRetrievalDecision(ctx, storage.CreateMemoryRetrievalDecision{
				ID: deriveDecisionID(candidateID), CandidateID: candidateID,
				Decision: "rejected", ReasonKind: storage.MemoryRetrievalDecisionReason(decision.Reason),
				ReasonDetailRedacted: &detail,
			}); err != nil {
				return PreWorkGateResult{}, err
			}
			continue
		}

		if len(result.Eligible) < MaximumPresentedItems {
			result.Eligible = append(result.Eligible, InfluentialMemoryItem{
				CandidateID: candidateID, RevisionID: revisionID, Kind: entry.revision.Kind,
				Channels: channels, MatchedText: entry.matchedText, decision: decision,
			})
		}
	}

	if len(result.Eligible) == 0 {
		if _, err := service.store.CreateMemoryRetrievalFallback(ctx, storage.CreateMemoryRetrievalFallback{
			ID: deriveFallbackID(input.QueryID), QueryID: input.QueryID, CandidatesConsidered: result.Considered,
		}); err != nil {
			return PreWorkGateResult{}, err
		}
		result.FellBack = true
	}
	return result, nil
}

// RecordInfluence persists what the agent/planning loop actually did with
// one eligible candidate RunPreWorkGate returned: used it as-is, adapted
// it, or rejected it (M21-074). Calling this is the separate, later fact
// that turns "eligible" into "influenced (or was deliberately excluded
// from influencing) the outcome" (M21-075); a candidate RunPreWorkGate
// never marked eligible can never reach this call successfully, because
// retrievalgate.RecordAgentInfluence structurally refuses to build an
// AgentInfluenceRecord from a rejected decision, and item.decision is the
// exact decision RunPreWorkGate itself computed for this candidate.
func (service *Service) RecordInfluence(
	ctx context.Context,
	item InfluentialMemoryItem,
	action retrievalgate.AgentInfluenceAction,
	justification string,
) error {
	if service == nil || service.store == nil {
		return errors.New("retrieval service is unavailable")
	}
	record, err := retrievalgate.RecordAgentInfluence(item.decision, action, justification)
	if err != nil {
		return err
	}
	decisionValue := "rejected"
	if record.Accepted() {
		decisionValue = "accepted"
	}
	detail := boundedReasonDetail(record.Justification)
	return service.store.CreateMemoryRetrievalDecision(ctx, storage.CreateMemoryRetrievalDecision{
		ID: deriveDecisionID(item.CandidateID), CandidateID: item.CandidateID,
		Decision: decisionValue, ReasonKind: storage.MemoryRetrievalDecisionReason(record.Reason()),
		ReasonDetailRedacted: &detail,
	})
}

// discoverCandidates runs the M21-064..067 discovery phases in the order
// docs/plan.md §31 declares (exact identity first), merging any revision
// discovered by more than one phase into a single entry with the union of
// channels (M21-137), rather than logging it more than once.
func (service *Service) discoverCandidates(
	ctx context.Context,
	input PreWorkGateInput,
) ([]domain.MemoryArtifactRevisionID, map[domain.MemoryArtifactRevisionID]*discoveredEntry, error) {
	var order []domain.MemoryArtifactRevisionID
	index := map[domain.MemoryArtifactRevisionID]*discoveredEntry{}

	add := func(revision storage.MemoryArtifactRevisionRecord, channel storage.MemoryRetrievalCandidateSource, matchedText string) {
		entry, ok := index[revision.RevisionID]
		if !ok {
			entry = &discoveredEntry{revision: revision, channels: map[storage.MemoryRetrievalCandidateSource]struct{}{}}
			index[revision.RevisionID] = entry
			order = append(order, revision.RevisionID)
		}
		entry.channels[channel] = struct{}{}
		if matchedText != "" {
			if entry.matchedText == nil {
				entry.matchedText = map[storage.MemoryRetrievalCandidateSource]string{}
			}
			if _, exists := entry.matchedText[channel]; !exists {
				entry.matchedText[channel] = matchedText
			}
		}
	}

	// M21-064 BLOCKER: exact identity/fingerprint lookup, before anything
	// else. A prior CLOSED episode matching the task's own exact
	// fingerprint hash is the strongest possible discovery signal: the
	// literal same task was already attempted.
	hash, err := input.Task.Hash()
	if err != nil {
		return nil, nil, err
	}
	episodes, err := service.store.ListEpisodesByFingerprintHash(ctx, input.ProjectID, hash)
	if err != nil {
		return nil, nil, err
	}
	for _, episode := range episodes {
		artifactIDs, err := service.store.ListMemoryArtifactsSupportedByEpisode(ctx, episode.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, artifactID := range artifactIDs {
			deleted, err := service.store.MemoryArtifactIsDeleted(ctx, artifactID)
			if err != nil {
				return nil, nil, err
			}
			if deleted {
				continue
			}
			revision, err := service.store.GetLatestMemoryArtifactRevision(ctx, artifactID)
			if err != nil {
				return nil, nil, err
			}
			add(revision, storage.RetrievalCandidateExactMatch, "")
		}
	}

	// M21-065: reviewed project facts relevant to the selected (task
	// repository) context.
	if _, err := service.discoverByKind(ctx, input, domain.MemoryArtifactKindRepositoryFact, add); err != nil {
		return nil, nil, err
	}
	// M21-066: compatible commands and file-to-test mappings.
	if _, err := service.discoverByKind(ctx, input, domain.MemoryArtifactKindReviewedCommand, add); err != nil {
		return nil, nil, err
	}
	if _, err := service.discoverByKind(ctx, input, domain.MemoryArtifactKindFileToTestMapping, add); err != nil {
		return nil, nil, err
	}
	// M21-067: exact reusable atoms or recipes. Applicability is enforced
	// uniformly by EvaluateEligibility's shared applicability gate below,
	// never by a similarity threshold.
	if _, err := service.discoverByKind(ctx, input, domain.MemoryArtifactKindExecutionRecipe, add); err != nil {
		return nil, nil, err
	}
	atomReferenceRevisions, err := service.discoverByKind(ctx, input, domain.MemoryArtifactKindExecutableAtomReference, add)
	if err != nil {
		return nil, nil, err
	}

	// M21-167: search canonical name, normalized phrase, and active alias
	// against the task's affected-symbol hints. This is the last discovery
	// phase this function runs today, and it is a deterministic exact-text
	// match (internal/atomname.MatchAtomNamesAndAliases) -- never a
	// similarity search.
	if err := service.discoverByAtomName(ctx, input, atomReferenceRevisions, add); err != nil {
		return nil, nil, err
	}

	// M21-G07: atom documentation text is the last structured channel, still
	// ahead of any similarity seam. Rich comments make an atom easier to
	// FIND; every candidate found here goes through the same eligibility
	// chain, so richer prose can never make one easier to TRUST.
	if err := service.discoverByAtomDocumentation(ctx, input, atomReferenceRevisions, add); err != nil {
		return nil, nil, err
	}

	// Vector-similarity discovery seam (M21-167 ordering proof; see
	// similarityHook's doc comment on the Service type). similarityHook is
	// nil in every production Service, so nothing below ever executes
	// outside this package's own tests: no embedding generation, similarity
	// search, or ranking happens anywhere in production. When set (by a
	// test only), it is invoked strictly HERE -- after every exact-identity,
	// structured-field, and name/alias channel above has already fully run
	// and merged into order/index -- so it can only ADD a new candidate at
	// the end of order, or add a vector-similarity channel to a revision
	// already discovered above; it can never precede, reorder, or replace
	// what the channels above already found.
	if service.similarityHook != nil {
		discovered, err := service.similarityHook(ctx, input)
		if err != nil {
			return nil, nil, err
		}
		for _, candidate := range discovered {
			add(candidate.Revision, storage.RetrievalCandidateVectorSimilarity, "")
			score := candidate.Score
			index[candidate.Revision.RevisionID].similarityScore = &score
		}
	}

	return order, index, nil
}

// discoverByKind discovers every candidate of kind bound to the task's own
// repository through a deterministic structured-field filter (never
// similarity), logging it under storage.RetrievalCandidateApplicabilityPass
// (see internal/retrievalgate.DiscoveryChannelStructuredFields's doc: "found
// through a deterministic structured-field filter... never through
// similarity"). It returns the discovered revisions so a caller (
// discoverByAtomName, for MemoryArtifactKindExecutableAtomReference) can
// reuse the same fetch rather than re-querying storage.
func (service *Service) discoverByKind(
	ctx context.Context,
	input PreWorkGateInput,
	kind domain.MemoryArtifactKind,
	add func(storage.MemoryArtifactRevisionRecord, storage.MemoryRetrievalCandidateSource, string),
) ([]storage.MemoryArtifactRevisionRecord, error) {
	revisions, err := service.store.ListMemoryArtifactRevisionsByRepositoryAndKind(ctx, input.ProjectID, input.Task.Repository, kind)
	if err != nil {
		return nil, err
	}
	for _, revision := range revisions {
		add(revision, storage.RetrievalCandidateApplicabilityPass, "")
	}
	return revisions, nil
}

// buildEligibilityCandidate loads every eligibility input for one
// discovered revision, straight from durable storage -- never from
// documentation or descriptive text (M21-145's structural guarantee that
// EligibilityCandidate cannot even carry such a field is what this
// function's return type enforces).
func (service *Service) buildEligibilityCandidate(
	ctx context.Context,
	revision storage.MemoryArtifactRevisionRecord,
	projectID domain.ProjectID,
) (retrievalgate.EligibilityCandidate, error) {
	scopeRepository := revision.ContentRepositoryID
	if revision.ScopeRepositoryID != nil {
		scopeRepository = *revision.ScopeRepositoryID
	}

	evidenceLinks, err := service.store.ListMemoryArtifactSupportingEvidence(ctx, revision.RevisionID)
	if err != nil {
		return retrievalgate.EligibilityCandidate{}, err
	}
	assuranceLevels := make([]domain.AssuranceLevel, 0, len(evidenceLinks))
	for _, link := range evidenceLinks {
		level, err := service.store.GetEvidenceAssuranceLevel(ctx, link.EvidenceID)
		if err != nil {
			return retrievalgate.EligibilityCandidate{}, err
		}
		assuranceLevels = append(assuranceLevels, level)
	}

	predicateRecords, err := service.store.ListMemoryArtifactApplicabilityPredicates(ctx, revision.RevisionID)
	if err != nil {
		return retrievalgate.EligibilityCandidate{}, err
	}
	predicates := make([]retrievalgate.ApplicabilityPredicate, 0, len(predicateRecords))
	for _, record := range predicateRecords {
		predicate, err := decodeApplicabilityPredicate(record)
		if err != nil {
			return retrievalgate.EligibilityCandidate{}, err
		}
		predicates = append(predicates, predicate)
	}

	return retrievalgate.EligibilityCandidate{
		RevisionID: revision.RevisionID,
		Scope:      domain.MemoryProjectScope{Project: projectID, Repository: scopeRepository},
		// No memory-artifact kind currently declares its own top-level
		// toolchain/dependency requirement outside the M21-022
		// applicability-predicate mechanism decoded into Applicability
		// below (ApplicabilityPredicateKindDependencyBinding); these two
		// fields are therefore always empty today, which
		// EvaluateToolchainCompatibility/EvaluateDependencyCompatibility
		// treat as trivially satisfied by design (an empty `required` list
		// is vacuously satisfied). Toolchain/dependency compatibility for
		// retrieved candidates is enforced through the applicability
		// predicate gate instead (M21-067).
		RequiredToolchainBindings:   nil,
		RequiredDependencyBindings:  nil,
		Applicability:               predicates,
		Maturity:                    revision.Maturity,
		SupportingEvidenceAssurance: assuranceLevels,
	}, nil
}

func sortedChannels(channels map[storage.MemoryRetrievalCandidateSource]struct{}) []storage.MemoryRetrievalCandidateSource {
	result := make([]storage.MemoryRetrievalCandidateSource, 0, len(channels))
	for channel := range channels {
		result = append(result, channel)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func toGateChannels(channels []storage.MemoryRetrievalCandidateSource) []retrievalgate.DiscoveryChannel {
	result := make([]retrievalgate.DiscoveryChannel, len(channels))
	for i, channel := range channels {
		result[i] = retrievalgate.DiscoveryChannel(channel)
	}
	return result
}

// boundedReasonDetail defensively bounds detail at storage's
// reason_detail_redacted column limit (1..2048 bytes) and guarantees
// non-emptiness, even though every retrievalgate-produced detail string
// already satisfies both in practice.
func boundedReasonDetail(detail string) string {
	if detail == "" {
		detail = "no additional detail recorded"
	}
	const maximumBytes = 2048
	if len(detail) > maximumBytes {
		detail = detail[:maximumBytes]
	}
	return detail
}

func deriveCandidateID(queryID string, revisionID domain.MemoryArtifactRevisionID) string {
	return hashID(queryID + "|candidate|" + revisionID.String())
}

func deriveDecisionID(candidateID string) string {
	return hashID(candidateID + "|decision")
}

func deriveFallbackID(queryID string) string {
	return hashID(queryID + "|fallback")
}

func hashID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}
