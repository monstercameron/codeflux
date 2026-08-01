// Package atomname implements the pure, deterministic logic for Codeflux
// atom naming identity, normalization, aliasing, and rename classification
// (AGENTS.md "Atom Naming Style"; docs/plan.md §7 "Atom Naming and Retrieval
// Identity"; TODOS.md M21-146 through M21-175).
//
// It owns: atom naming-schema version 1, defined independently from
// internal/atomdoc's documentation schema version and from the separate,
// not-yet-implemented atom embedding-input schema (M21-146, M21-127);
// canonical Go-identifier validation with no short semantic length limit
// (M21-147); deterministic derivation of the display name and normalized
// word-split phrase from the canonical identifier, preserving multi-letter
// initialisms such as HTTP, ID, or URL (M21-148, M21-149, M21-150); the
// established-abbreviation allowlist and its project extension mechanism,
// plus the recognized-action-verb, known-provider-name, and
// guarantee-claim-word lexicons that a reviewed
// "//codeflux:atom-name-exception <kind>: <reason>" directive can bypass
// (M21-151); the structured naming rationale requiring the nearest confusing
// alternative and the distinguishing qualifier (M21-152); the typed
// atom-name and atom-name-alias records for the storage lane to persist
// (supporting M21-153..156, which remain DATA tasks owned by that lane);
// classification of a proposed rename as formatting-only,
// semantic-preserving, or semantic-breaking, and the enforcement that a
// semantic-preserving rename requires explicit review while a
// semantic-breaking rename requires a new atom version or atom identity
// (M21-157..160); composition of the canonical-name and normalized-phrase
// embedding-input segments and low-weight alias discovery text, excluding
// obsolete or invalidated aliases from active candidate generation while
// retaining their lineage (M21-161..163); the typed vector
// re-embedding-invalidation trigger signal (M21-164, naming trigger only); a
// pure graph-node label truncation function that never mutates the stored
// canonical or display name (supporting M21-165, M21-166); and pure
// canonical-name/normalized-phrase/alias matching primitives for a retrieval
// gate to call before vector similarity (supporting M21-167, M21-168).
//
// It does not own: SQLite schema, migrations, or repositories (the storage
// lane is the sole migration writer for M21-153..156; this package only
// returns typed records for that lane to persist); embedding generation,
// vector storage, or similarity search, which sit behind the vector-runtime
// branch gate in docs/plan.md §0 (this package defines only the typed
// invalidation-trigger signal, never regeneration); graph-node visual
// rendering or the decision of when to truncate (this package provides the
// pure truncation function; the frontend graph lane in web/frontend owns
// calling it); the retrieval gate's project, compatibility, applicability,
// evidence, and assurance checks (this package's matches are discovery
// signals only and carry no notion of approval or applicability); source
// location or comment-group extraction for the naming-exception directive
// (internal/atomdoc's AST extraction owns locating declarations; this
// package only parses and validates one already-isolated directive body);
// or atom-documentation parsing, hashing, or admission, which remain
// internal/atomdoc's responsibility. This package reuses
// atomdoc.AtomVersionID, atomdoc.DocumentationRevisionID, and
// atomdoc.ContractHash rather than defining competing types.
package atomname
