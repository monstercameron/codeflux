package atomdoc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// validFixtureSource is a synthetic, stable schema-v1 atom comment used
// across tests. It intentionally mirrors the AGENTS.md "ReserveFunds"
// example shape but with fabricated, non-sensitive domain content so tests
// never depend on real credentials, customer data, or private URLs.
const validFixtureSource = `package fixture

// ReserveWidgetInventoryUntilCheckoutExpires reserves a count of widget
// inventory against a checkout session without committing the sale.
//
//codeflux:atom
// Codeflux atom documentation (schema v1):
//   Purpose:
//     Hold scarce widget inventory for one checkout session so two shoppers
//     cannot both complete a sale for the same physical unit.
//   Use when:
//     A checkout session has a stable session identity and the caller needs
//     a short-lived hold before payment capture completes.
//   Do not use when:
//     The caller wants a permanent stock decrement; use CommitWidgetSale for
//     that outcome instead, since a reservation alone never decrements stock.
//   Semantics:
//     Reserves the requested count atomically against available inventory
//     and returns a hold identity; a hold that is never captured or released
//     expires automatically after its configured lifetime.
//   Inputs:
//     - SessionID identifies the checkout session; it must be a previously
//       issued, non-expired session identity.
//     - Count is the number of physical units requested; it must be a
//       positive integer bounded by the catalog's per-order maximum.
//   Outputs:
//     - HoldID identifies the created reservation and is required by the
//       matching release or capture call.
//     - InsufficientInventory indicates the requested count exceeds the
//       currently available count; no hold is created in this case.
//   Preconditions:
//     - The checkout session must exist and must not already hold a
//       reservation for this catalog item.
//   Postconditions:
//     - On success, the reserved count is subtracted from available
//       inventory until the hold is released, captured, or expires.
//   Effects:
//     - Writes one inventory hold row scoped to the checkout session with a
//       single logical reservation identity per session and catalog item.
//   Failure semantics:
//     - InsufficientInventory is a safe, retryable outcome; a storage
//       failure before the hold is durably written is also safe to retry
//       using the same idempotency key.
//   Determinism:
//     The reservation count is deterministic given the same inventory state;
//     the generated hold identity is not deterministic across retries.
//   Idempotency and retry:
//     Logical operation identity is the pair of session identity and catalog
//     item; a retry with the same pair returns the existing hold rather than
//     creating a second one for its configured key lifetime.
//   Reconciliation and compensation:
//     An expired, uncaptured hold is reconciled by an automatic release job;
//     no manual compensation step exists for this atom.
//   Security and privacy:
//     The session identity is treated as a capability-scoped reference and
//     is never logged alongside catalog pricing details.
//   Dependencies and bindings:
//     Depends on the inventory ledger's compare-and-set primitive; behavior
//     assumes the ledger enforces per-item serializable writes.
//   Complexity and limits:
//     Bounded to the catalog's configured per-order maximum count and a
//     fixed maximum hold lifetime measured in minutes.
//   Examples:
//     - A shopper reserving three units of one catalog item during checkout
//       is the representative use.
//     - Requesting zero units is a non-example; the caller must request at
//       least one unit.
//   Verification:
//     Covered by a real-storage integration test asserting exactly one hold
//     row per session and item under concurrent reservation attempts.
//   Retrieval concepts:
//     Inventory hold, checkout reservation, stock lock, oversell prevention.
func ReserveWidgetInventoryUntilCheckoutExpires() {}
`

func parseFixtureFile(t *testing.T, source string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse fixture source: %v", err)
	}
	return fset, file
}

func mustLocateSingleCandidate(t *testing.T, source string) SourceCandidate {
	t.Helper()
	fset, file := parseFixtureFile(t, source)
	candidates, err := LocateAtomDeclarationCandidates(fset, file)
	if err != nil {
		t.Fatalf("locate candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected exactly one candidate, got %d", len(candidates))
	}
	return candidates[0]
}
