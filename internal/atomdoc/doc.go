// Package atomdoc parses, validates, scrubs, hashes, and generates Codeflux
// atom-documentation content for schema version 1 (AGENTS.md "Atom
// Documentation Style"; docs/plan.md §7 "Atom Documentation as Retrieval
// Material").
//
// It owns: the versioned structured-documentation schema and field policy;
// AST-based extraction of source-authored atom doc comments using go/ast,
// go/parser, and go/token rather than regular-expression scanning alone;
// whitespace normalization that preserves list structure, punctuation,
// domain terms, and negative examples; keyword-stuffing and repeated-
// boilerplate quality flags; scrubbing atom documentation through the shared
// internal/redact pipeline before admission and rejecting comments carrying
// known-sensitive content; the two distinct content hashes (the exact
// source-comment hash computed before normalization, and the normalized-
// input hash computed after normalization and scrubbing); the atom-version
// and documentation-revision identity types, each kept distinct in the type
// system from atom identity (domain.AtomID); which atom-tier categories are
// source-authored, SQLite-authored, or generated projections; generating Go
// comment text from a structured documentation revision for SQLite-authored
// atoms; and a typed admission result describing success or the exact
// rejection reason.
//
// It does not own: SQLite schema, migrations, or repositories (the schema
// lane owns M21-103..107 and is the sole migration writer; this package
// returns typed results for that lane to persist); embedding generation,
// vector storage, or similarity search; atom-naming validation, aliasing, or
// rename classification; or memory boundary types, episodes, fact
// extraction, fingerprints, retrieval ranking, or UI.
//
// Every extracted comment is untrusted descriptive input until this
// package's syntax, schema-policy, security, and hash/contract-binding
// checks pass. Passing those checks makes the comment usable as retrieval
// material; it never substitutes for the atom's typed contract, executable
// applicability predicate, tests, or evidence.
package atomdoc
