// Package retrievalgate implements the retrieval eligibility gates and
// candidate provenance layer for Milestone 21's "Pre-Work Retrieval Gate"
// and "Optional Vector Candidate Discovery" sections (TODOS.md M21-068
// through M21-072, M21-077, M21-136 through M21-138, M21-144, M21-145).
//
// AGENTS.md is explicit about this package's one job: "Vector similarity may
// discover candidates; it never establishes compatibility, validity,
// assurance, or permission." docs/plan.md §0 Dependency Invariants declares
// "vector similarity -> applicability or authority" and "memory item ->
// self-validation" PROHIBITED. This package is what makes both
// dependencies structurally impossible rather than merely documented:
//
//   - DiscoveredCandidate (provenance.go) is the only type in this package
//     that may carry a similarity score or name/alias discovery channel. It
//     mirrors the existing atomname.NameMatch pattern: "a pure discovery
//     result... deliberately carries no applicability... field."
//   - EligibilityCandidate (eligibility.go) is the only input
//     EvaluateEligibility and the gate predicates it composes can see. It
//     has no field capable of holding a similarity score, a discovery
//     channel, or a documentation payload -- so a candidate's retrieval
//     rank, similarity score, or documentation richness cannot type-check
//     its way into an eligibility decision. See structural_test.go for a
//     reflection-based proof that stays red if this property regresses.
//   - CandidateProvenance (provenance.go) is assembled only AFTER an
//     EligibilityDecision already exists; it records how a candidate was
//     found and what happened to it, but nothing in this package ever
//     feeds a DiscoveredCandidate or a CandidateProvenance back into
//     EvaluateEligibility.
//
// Phase separation (M21-136): discovery (locating candidates by exact
// identity, structured fields, or vector similarity) and eligibility
// (deciding whether a discovered candidate may influence work) are
// different phases with different inputs and different types. Nothing in
// this package can collapse them into one step: EvaluateEligibility takes
// an EligibilityCandidate, never a DiscoveredCandidate, so a caller cannot
// even accidentally wire a discovery result directly into an eligibility
// decision -- the parameter type refuses it at compile time.
//
// This package is pure and storage-independent, matching the layering
// internal/fingerprint, internal/atomdoc, and internal/atomname already
// use: it depends only on internal/domain and internal/fingerprint, never
// on internal/storage. Its DiscoveryChannel and RejectionReason
// enumerations mirror (by string value, not by import) the vocabulary
// already declared in internal/storage/memory_retrieval_repository.go and
// internal/storage/memory_applicability_repository.go, following the same
// "mirrors... wire vocabulary... without importing that package" pattern
// internal/fingerprint.AuthorityClass already uses for
// internal/executor.AuthorityClass. A later planning lane calls this
// package's pure functions and then persists the result through the
// existing storage.CreateMemoryRetrievalQuery /
// CreateMemoryRetrievalCandidate / CreateMemoryRetrievalDecision
// repositories; this package performs no I/O and owns no schema.
//
// Nothing here performs embedding generation, similarity search, or
// ranking (all still closed at the docs/plan.md §0 branch gate); a
// similarity score is accepted only as an opaque, non-authoritative INPUT
// on DiscoveredCandidate, exactly to prove it cannot confer eligibility.
package retrievalgate
