// Package fingerprint computes the versioned task fingerprint that project
// memory retrieval uses as its exact-match identity and eligibility key (see
// docs/plan.md §5 "Task Fingerprint and Retrieval" and §31 "Versioned Task
// Fingerprints"; AGENTS.md "Vector similarity may discover candidates; it
// never establishes compatibility, validity, assurance, or permission").
//
// It owns:
//
//   - the fingerprint schema version and explicit, non-silent detection of a
//     schema mismatch (M21-051);
//   - the exact fields that participate in gate eligibility and the
//     canonical hash: project/repository identity, base revision or
//     compatibility range, normalized task class, affected package/symbol/
//     path hints, language/toolchain/dependency bindings, risk and
//     validation requirements, and requested effect/authority class
//     (M21-052..058);
//   - the structural separation between those exact fields and free-form
//     descriptive retrieval text, so descriptive text has no code path into
//     identity (M21-059);
//   - deterministic normalization and canonical serialization of the exact
//     fields, independent of caller-supplied ordering, map iteration order,
//     locale, float formatting, and time zone (M21-060);
//   - the SHA-256 hash of the canonical exact fingerprint (M21-061).
//
// It does not own:
//
//   - SQLite persistence, migrations, or the fingerprint schema-version
//     table (internal/storage owns M21-023; this package returns typed Go
//     values and lets that lane persist them);
//   - the retrieval/pre-work gate, applicability-predicate evaluation, or
//     any eligibility decision built on top of a fingerprint (M21-064..077);
//   - embedding generation or vector similarity over descriptive text
//     (M21-078 and later, behind the branch gate);
//   - episodes, memory-artifact content, or atom documentation, which
//     remain internal/domain's memory.go and internal/atomdoc.
package fingerprint
