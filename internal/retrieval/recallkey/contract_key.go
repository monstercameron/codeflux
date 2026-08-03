// Package recallkey implements the deterministic recall keys TODOS.md's
// "Registration and Reuse" section requires before a contract this run needs
// may be matched against an atom the project already has (PIPE-051,
// PIPE-051a).
//
// It is a separate package from its sibling internal/retrieval, and not a
// file inside it, because the two guard different boundaries.
// internal/retrieval is the M21 pre-work discovery/eligibility gate, and
// internal/retrieval/gate_evidence_g07_test.go enforces that no production
// file in that exact package imports internal/atomdoc: discovery there reads
// documentation as plain text only, through
// storage.AtomDocumentationDiscoveryText, specifically so a richly written
// comment can never reason its own way past an eligibility gate. This
// package's job is unrelated to that gate -- it computes the PIPE pipeline
// recall stage's own matching keys, which legitimately need
// atomdoc.ContractHash's validated type -- so it lives beside internal/
// retrieval rather than inside it, keeping the M21 guard meaningful instead
// of papering over it.
//
// Two keys are implemented here:
//
//   - ContractHash (PIPE-051): an exact match on the normalised signature
//     together with its return-error and effect classification, answered
//     from an index with no model request. It exists to replace a substring
//     search over stored source text (the defect PIPE-051 names:
//     `strings.Contains(content, "func "+name+"(")`), which finds a renamed
//     atom never -- the substring is built from the literal new identifier,
//     which never appeared in the old text -- and a same-named different
//     atom always, because nothing about a name match compares what either
//     function actually promises.
//   - SignatureShape (PIPE-051a): the parameter and result type vector with
//     names erased, used only to bound a *candidate set* for a caller's own
//     use (for example, explaining why a same-shaped function was still
//     rejected). AGENTS.md's product boundary is binding here: "Vector
//     similarity may discover candidates; it never establishes
//     compatibility, validity, assurance, or permission." A shape match
//     narrows what is worth looking at; only ContractHash ever decides that
//     two contracts are the same one.
//
// PIPE-051b, the documentation-embedding key over the shape candidates, is
// deliberately not implemented in this package. AUDIT-026 closed the vector
// branch until DeterministicRetrievalRecallMeasurement can be run over
// reviewed fallbacks from real runs, and cmd/codeflux-dev/vector_branch_test.go
// fails the build if any production file calls a vector writer. That
// precondition has not changed since AUDIT-026 was recorded, so PIPE-051b
// stays blocked; see this repository's PIPE-050/051/051a completion report
// for the exact reasoning.
package recallkey

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"codeflux.dev/codeflux/internal/atomdoc"
)

// NormalizedContract is everything the pipeline can derive about one
// function's contract without a model request: its name-erased parameter and
// result types, whether it can fail by returning an error, and the effects it
// declares.
//
// The flow's own gate for the contracts stage asks for "preconditions,
// postconditions, and effects" (internal/pipeline.StageContracts). Nothing in
// the pipeline today raises a separately-declared precondition or
// postcondition for produced code -- the contracts stage derives its
// contract from the implementation rather than from an agreed declaration
// (PIPE-137, still open) -- so this type carries the strongest proxy for
// those two fields the pipeline actually has today: ReturnsError (the one
// postcondition every Go signature can state without inference) and Effects
// (what the contracts stage's own evidence already names). A future,
// stronger contract representation is expected to extend the fields this
// type hashes over, not replace ComputeContractHash's approach.
type NormalizedContract struct {
	// Parameters is the ordered, name-erased declared type of every
	// parameter -- "string", "*Account", "[]byte" -- because a caller cannot
	// see parameter names, and a signature differing only in naming offers
	// the same contract.
	Parameters []string
	// Results is the ordered, name-erased declared type of every result.
	Results []string
	// ReturnsError is whether failure is carried as a value the caller must
	// handle.
	ReturnsError bool
	// Effects names what the function does beyond returning a value. Nil or
	// empty means the function is pure.
	Effects []string
}

// SignatureShape is the parameter and result type vector with names erased
// (PIPE-051a), rendered as a stable, comparable string safe to use as a map
// key.
//
// It says nothing about failure behaviour or effects, which is deliberate:
// it identifies a candidate set, never a match. Two contracts can share a
// Shape and still be different contracts -- one returns an error and the
// other cannot fail, one mutates state and the other is pure -- and only
// ComputeContractHash, over the full NormalizedContract, may decide they are
// the same one.
type SignatureShape string

// Shape derives the signature-shape key from a contract's type vector.
func (contract NormalizedContract) Shape() SignatureShape {
	return SignatureShape(
		"(" + strings.Join(contract.Parameters, ",") + ")->(" +
			strings.Join(contract.Results, ",") + ")")
}

// ComputeContractHash derives the contract-hash key (PIPE-051).
//
// Two contracts hash identically only when their shape, error behaviour, and
// effects all agree; any one of those differing -- a parameter reordered, an
// effect added, error handling introduced or removed -- changes the hash, so
// the key can never quietly match a semantically different atom the way a
// bare name or a substring search over source text can.
//
// Effects are sorted and deduplicated before hashing so that the same set of
// effects, written down in a different order, still hashes the same; nothing
// about the order Effects happens to be authored in is part of the contract.
func ComputeContractHash(contract NormalizedContract) atomdoc.ContractHash {
	effects := append([]string(nil), contract.Effects...)
	sort.Strings(effects)
	deduped := effects[:0]
	var previous string
	for index, effect := range effects {
		if index > 0 && effect == previous {
			continue
		}
		deduped = append(deduped, effect)
		previous = effect
	}

	hasher := sha256.New()
	hasher.Write([]byte("codeflux-contract-hash-v1\x1fshape="))
	hasher.Write([]byte(string(contract.Shape())))
	if contract.ReturnsError {
		hasher.Write([]byte("\x1freturns-error=true\x1feffects="))
	} else {
		hasher.Write([]byte("\x1freturns-error=false\x1feffects="))
	}
	hasher.Write([]byte(strings.Join(deduped, ",")))

	digest := hex.EncodeToString(hasher.Sum(nil))
	// sha256's own output is exactly 32 bytes, so hex-encoding it is always
	// exactly 64 lowercase hexadecimal characters -- precisely what
	// ParseContractHash validates. The error cannot occur for any input this
	// function can produce, so it is discarded here rather than propagated
	// through every caller for a case that would be a programming error in
	// this function, not a runtime one.
	hash, _ := atomdoc.ParseContractHash(digest)
	return hash
}
