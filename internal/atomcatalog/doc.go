// Package atomcatalog declares the starter control-flow catalog described in
// docs/go-program-graph.md §5.9 and returns typed records for the storage lane
// to persist, so the entries are retrievable through the same path as any
// other atom documentation.
//
// It owns: the fifteen control-flow entries and their schema-v1
// atom-documentation content; derivation of each entry's atom identity,
// version, and documentation revision through internal/atomdoc; and a seeding
// function that writes them through storage.CreateAtomDocumentationRevision.
//
// It does not own: SQLite schema or migrations (the storage lane is the sole
// migration writer; this package only supplies records for it to persist);
// embedding generation or similarity search; atom-naming validation, which is
// internal/atomname's; or any evaluator, kernel, or lowering behaviour.
//
// # This catalog is a proposal, not a kernel declaration
//
// POST-002 in internal/deferred forbids claiming "that the kernel's scope is
// known". Every entry here records itself as a proposal from
// docs/go-program-graph.md §5.9 and carries no assertion that the set is
// complete, frozen, minimal, or authorised. Seeding retrievable documentation
// is the shipped M21 retrieval lane; it neither implements nor scopes a
// kernel, and no reader may take it as doing so.
//
// Retrieval consequences follow the same rule internal/atomdoc states for all
// documentation: an entry here is a discovery candidate. It is not a contract,
// carries no applicability predicate, and confers no assurance.
package atomcatalog
