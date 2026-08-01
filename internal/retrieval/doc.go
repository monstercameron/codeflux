// Package retrieval implements the pre-work retrieval gate (TODOS.md
// M21-064 through M21-076) and closes the M21-137 storage gap's remaining
// write-path wiring: the entry point that makes a task's planning flow
// actually consult project memory before generating a solution from
// scratch, per docs/plan.md §31 "Retrieval and Pre-Work Gate":
//
//	Before generating a solution, the agent:
//	1. constructs the task fingerprint;
//	2. loads version-compatible workspace facts and execution recipes;
//	...
//	9. records every retrieval, routing, reuse, rejection, escalation, and
//	   outcome.
//
// This package is the caller that wires internal/retrievalgate's already-
// landed, structurally pure eligibility gates to real storage:
//
//   - Service.RunPreWorkGate (M21-064 BLOCKER) is the entry point. It runs
//     an exact identity/fingerprint lookup FIRST (a prior closed episode
//     matching the task's exact fingerprint hash, and the memory artifacts
//     that episode is known to have supported), then discovers reviewed
//     project facts (M21-065), compatible commands and file-to-test
//     mappings (M21-066), and exact reusable recipes/atoms (M21-067) --
//     all through deterministic, structured-field lookups, never
//     similarity. Every discovered candidate, regardless of which
//     channel(s) found it, is run through the exact same
//     retrievalgate.EvaluateEligibility gate sequence; M21-067's
//     applicability requirement therefore falls out of that shared gate
//     rather than needing its own special case. Every candidate --
//     eligible or not -- is durably logged (M21-072); every REJECTED
//     candidate's decision is recorded immediately, because a gate
//     rejection is already a complete, final fact. An ELIGIBLE candidate's
//     decision is deliberately NOT recorded yet: eligibility and influence
//     are different facts (docs/plan.md §31), and only the agent's later
//     RecordInfluence call can supply the second one.
//   - RunPreWorkGate's result (PreWorkGateResult) is a typed, bounded list
//     of eligible items for presentation (M21-073); this package builds no
//     UI.
//   - Service.RecordInfluence (M21-074, M21-075) is what the agent/planning
//     loop calls after using, adapting, or rejecting one eligible item.
//     Calling it is what turns "eligible" into "actually influenced (or was
//     deliberately not used for) the outcome" -- retrievalgate.
//     RecordAgentInfluence structurally refuses to accept an ineligible
//     candidate here, so a rejected candidate can never be logged as having
//     influenced anything.
//   - When RunPreWorkGate finds zero eligible candidates, it records that
//     fact once, as a normal outcome, via
//     storage.CreateMemoryRetrievalFallback (M21-076) and returns
//     PreWorkGateResult.FellBack = true; the caller then proceeds with
//     ordinary planning from scratch. This is never reported as an error.
//
// No embedding generation, similarity search, or ranking happens anywhere
// in this package (still closed at the docs/plan.md §0 branch gate): every
// discovery channel used today is DiscoveryChannelExactIdentity or
// DiscoveryChannelStructuredFields. DiscoveryChannelVectorSimilarity and
// DiscoveredCandidate.SimilarityScore remain fully wired through (a typed
// seam), but nothing in this package ever produces a similarity score in
// production, and -- structurally, via retrievalgate's own types -- nothing
// here could let one influence an eligibility decision even if it did.
//
//   - atom_name_discovery.go (M21-167, M21-168) adds the last discovery
//     phase RunPreWorkGate runs today: searching every currently valid atom
//     canonical name, normalized phrase, and active alias in the task's
//     project against the task's affected-symbol hints
//     (internal/atomname.MatchAtomNamesAndAliases -- a pure exact-text
//     match, never similarity), and recording which specific name or alias
//     text caused the match (InfluentialMemoryItem.MatchedText). Service's
//     unexported similarityHook field is a test-only seam (nil in every
//     production Service) that exists solely to prove, structurally, that a
//     future similarity channel could only ever run after this phase and
//     add to what it already found -- never precede, reorder, or replace
//     it; see atom_name_discovery_test.go.
package retrieval
