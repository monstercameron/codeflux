package recallkey

import "testing"

// TestPIPE051_ContractHashMatchesOnlyWhenTheFullContractAgrees covers
// PIPE-051.
//
// The defect it replaces, `strings.Contains(content, "func "+name+"(")`, has
// two failure modes: it never finds a renamed atom (the substring is built
// from the new identifier, which never appeared in the old text), and it
// always "finds" a same-named atom whose contract has actually changed
// (nothing about a name match compares signatures). ComputeContractHash must
// discriminate both.
func TestPIPE051_ContractHashMatchesOnlyWhenTheFullContractAgrees(t *testing.T) {
	base := NormalizedContract{
		Parameters:   []string{"int"},
		Results:      []string{"int", "error"},
		ReturnsError: true,
		Effects:      []string{"reaches outside its arguments"},
	}

	// A rename: identical contract, identical hash. The old substring match
	// could never make this claim -- it depends on the literal identifier.
	renamed := base
	if ComputeContractHash(base) != ComputeContractHash(renamed) {
		t.Fatal("two identical contracts hashed differently; a rename with " +
			"the same contract would never be found")
	}

	// A same-named function whose contract actually differs must NOT match.
	// This is the case the old check always got wrong: identical name,
	// different signature, and the substring search reported it reusable
	// anyway.
	differentReturnsError := base
	differentReturnsError.ReturnsError = false
	differentReturnsError.Results = []string{"int"}
	if ComputeContractHash(base) == ComputeContractHash(differentReturnsError) {
		t.Fatal("contracts differing in return-error handling hashed the " +
			"same; a same-named different atom would be matched")
	}

	differentEffects := base
	differentEffects.Effects = nil
	if ComputeContractHash(base) == ComputeContractHash(differentEffects) {
		t.Fatal("contracts differing in declared effects hashed the same; " +
			"an impure function would be matched against a pure one")
	}

	differentParameters := base
	differentParameters.Parameters = []string{"string"}
	if ComputeContractHash(base) == ComputeContractHash(differentParameters) {
		t.Fatal("contracts differing in parameter type hashed the same")
	}

	// Effect ordering is not part of the contract: the same set, written
	// down in a different order, must still hash the same.
	twoEffects := base
	twoEffects.Effects = []string{"a", "b"}
	swapped := base
	swapped.Effects = []string{"b", "a"}
	if ComputeContractHash(twoEffects) != ComputeContractHash(swapped) {
		t.Fatal("the same effect set in a different order hashed differently")
	}
}

// TestPIPE051_ContractHashIsValid proves ComputeContractHash always produces
// a hash ParseContractHash accepts, which is what lets it be used as an index
// key and stored without a second validation pass.
func TestPIPE051_ContractHashIsValid(t *testing.T) {
	hash := ComputeContractHash(NormalizedContract{})
	if hash.IsZero() {
		t.Fatal("ComputeContractHash produced a zero hash for a valid, if empty, contract")
	}
	if len(hash.String()) != 64 {
		t.Fatalf("contract hash %q is not 64 characters", hash.String())
	}
}

// TestPIPE051a_ShapeNarrowsCandidatesButNeverDecides covers PIPE-051a.
//
// The signature-shape key must group two contracts that share a type vector
// even when they are not the same contract, and it must separate two
// contracts that share nothing about their types even if everything else
// matches. Shape alone must never agree exactly where ContractHash disagrees
// in a way that lets a caller treat a shape match as a verdict.
func TestPIPE051a_ShapeNarrowsCandidatesButNeverDecides(t *testing.T) {
	pure := NormalizedContract{
		Parameters: []string{"int"}, Results: []string{"int"},
	}
	impure := NormalizedContract{
		Parameters: []string{"int"}, Results: []string{"int"},
		Effects: []string{"reaches outside its arguments"},
	}
	if pure.Shape() != impure.Shape() {
		t.Fatal("two contracts with identical parameter and result types " +
			"produced different shapes; shape must ignore effects")
	}
	if ComputeContractHash(pure) == ComputeContractHash(impure) {
		t.Fatal("a pure and an impure contract sharing a shape hashed the " +
			"same; shape must never be sufficient for a match")
	}

	differentShape := NormalizedContract{
		Parameters: []string{"string"}, Results: []string{"int"},
	}
	if pure.Shape() == differentShape.Shape() {
		t.Fatal("contracts with different parameter types produced the same shape")
	}
}
