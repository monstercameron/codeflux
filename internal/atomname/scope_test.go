package atomname

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/domain"
)

func mustProjectID(t *testing.T) domain.ProjectID {
	t.Helper()
	id, err := domain.NewProjectID()
	if err != nil {
		t.Fatalf("domain.NewProjectID failed: %v", err)
	}
	return id
}

func mustAtomVersionID(t *testing.T) atomdoc.AtomVersionID {
	t.Helper()
	atomID, err := domain.NewAtomID()
	if err != nil {
		t.Fatalf("domain.NewAtomID failed: %v", err)
	}
	version, err := atomdoc.NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatalf("atomdoc.NewAtomVersionID failed: %v", err)
	}
	return version
}

func mustNameRecord(t *testing.T, scope NamingScope, canonical string) AtomNameRecord {
	t.Helper()
	name, err := NewCanonicalName(canonical)
	if err != nil {
		t.Fatalf("NewCanonicalName(%q) failed: %v", canonical, err)
	}
	rationale, err := NewNamingRationale("nearest confusing alternative name", "distinguishing qualifier text")
	if err != nil {
		t.Fatalf("NewNamingRationale failed: %v", err)
	}
	version := mustAtomVersionID(t)
	record, err := NewAtomNameRecord(version.AtomID(), version, scope, name, rationale, 1, AtomNameRevisionID{}, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewAtomNameRecord failed: %v", err)
	}
	return record
}

// TestDetectCanonicalNameCollisionWithinOneSemanticScope is M21-170: two
// different atoms in the same project and semantic scope must not be able
// to claim the same normalized canonical name.
func TestDetectCanonicalNameCollisionWithinOneSemanticScope(t *testing.T) {
	project := mustProjectID(t)
	scope, err := NewNamingScope(project, "internal/payments")
	if err != nil {
		t.Fatalf("NewNamingScope failed: %v", err)
	}

	existing := mustNameRecord(t, scope, "ReserveAccountFundsUntilAuthorizationExpires")
	candidate := DeriveNormalizedPhrase(existing.CanonicalName)

	otherAtom, err := domain.NewAtomID()
	if err != nil {
		t.Fatalf("domain.NewAtomID failed: %v", err)
	}

	collision, with, err := DetectCanonicalNameCollision(scope, []AtomNameRecord{existing}, candidate, otherAtom)
	if err != nil {
		t.Fatalf("DetectCanonicalNameCollision failed: %v", err)
	}
	if !collision {
		t.Fatal("expected a collision for a second, different atom claiming the same normalized name in the same scope")
	}
	if with == nil || with.AtomID != existing.AtomID {
		t.Fatalf("expected the collision to reference the existing record's atom, got %+v", with)
	}

	// The same atom re-validating its own current name must not collide
	// with itself.
	collision, _, err = DetectCanonicalNameCollision(scope, []AtomNameRecord{existing}, candidate, existing.AtomID)
	if err != nil {
		t.Fatalf("DetectCanonicalNameCollision failed: %v", err)
	}
	if collision {
		t.Error("an atom must not collide with its own existing name record")
	}

	// An invalidated (Valid == false) record must not participate.
	invalidated := existing
	invalidated.Valid = false
	collision, _, err = DetectCanonicalNameCollision(scope, []AtomNameRecord{invalidated}, candidate, otherAtom)
	if err != nil {
		t.Fatalf("DetectCanonicalNameCollision failed: %v", err)
	}
	if collision {
		t.Error("an invalidated record must not cause a collision")
	}
}

// TestEquivalentNamesInSeparateProjectScopesRemainIsolated is M21-171:
// identical normalized canonical names in two different project scopes
// must not collide.
func TestEquivalentNamesInSeparateProjectScopesRemainIsolated(t *testing.T) {
	scopeA, err := NewNamingScope(mustProjectID(t), "internal/payments")
	if err != nil {
		t.Fatalf("NewNamingScope failed: %v", err)
	}
	scopeB, err := NewNamingScope(mustProjectID(t), "internal/payments")
	if err != nil {
		t.Fatalf("NewNamingScope failed: %v", err)
	}

	recordInScopeA := mustNameRecord(t, scopeA, "ReserveAccountFundsUntilAuthorizationExpires")
	candidate := DeriveNormalizedPhrase(recordInScopeA.CanonicalName)

	otherAtom, err := domain.NewAtomID()
	if err != nil {
		t.Fatalf("domain.NewAtomID failed: %v", err)
	}

	collision, _, err := DetectCanonicalNameCollision(scopeB, []AtomNameRecord{recordInScopeA}, candidate, otherAtom)
	if err != nil {
		t.Fatalf("DetectCanonicalNameCollision failed: %v", err)
	}
	if collision {
		t.Error("equivalent names in separate project scopes must remain isolated from each other")
	}

	// Same project, different semantic scope must also remain isolated.
	scopeC, err := NewNamingScope(scopeA.ProjectID, "internal/billing")
	if err != nil {
		t.Fatalf("NewNamingScope failed: %v", err)
	}
	collision, _, err = DetectCanonicalNameCollision(scopeC, []AtomNameRecord{recordInScopeA}, candidate, otherAtom)
	if err != nil {
		t.Fatalf("DetectCanonicalNameCollision failed: %v", err)
	}
	if collision {
		t.Error("equivalent names in separate semantic scopes of the same project must remain isolated")
	}
}

func TestNewAtomNameRecordRevisionSequencing(t *testing.T) {
	scope, err := NewNamingScope(mustProjectID(t), "internal/payments")
	if err != nil {
		t.Fatalf("NewNamingScope failed: %v", err)
	}
	name, err := NewCanonicalName("ReserveAccountFundsUntilAuthorizationExpires")
	if err != nil {
		t.Fatalf("NewCanonicalName failed: %v", err)
	}
	rationale, err := NewNamingRationale("nearest confusing alternative name", "distinguishing qualifier text")
	if err != nil {
		t.Fatalf("NewNamingRationale failed: %v", err)
	}
	version := mustAtomVersionID(t)

	if _, err := NewAtomNameRecord(version.AtomID(), version, scope, name, rationale, 2, AtomNameRevisionID{}, time.Now().UTC()); err == nil {
		t.Error("expected revision 2 without a supersedes identity to be rejected")
	}
	if _, err := NewAtomNameRecord(version.AtomID(), version, scope, name, rationale, 1, AtomNameRevisionID{value: "anr_deadbeef"}, time.Now().UTC()); err == nil {
		t.Error("expected the first revision naming a supersedes identity to be rejected")
	}
}

// TestNamingScopeIsZero is the reproduction for the adversarial-review
// zero-value-scope defect: an uninitialized NamingScope{} (and a
// partially-malformed hand-constructed scope missing one field) must report
// itself as zero, the same guard every other identity type in this package
// (CanonicalName, ContractHash, AtomVersionID, NamingRationale) already
// enforces at construction.
func TestNamingScopeIsZero(t *testing.T) {
	if !(NamingScope{}).IsZero() {
		t.Error("expected an uninitialized NamingScope{} to report IsZero() == true")
	}
	if !(NamingScope{SemanticScope: "internal/payments"}).IsZero() {
		t.Error("expected a scope missing its project identity to report IsZero() == true")
	}
	if !(NamingScope{ProjectID: mustProjectID(t)}).IsZero() {
		t.Error("expected a scope missing its semantic-scope label to report IsZero() == true")
	}

	valid, err := NewNamingScope(mustProjectID(t), "internal/payments")
	if err != nil {
		t.Fatalf("NewNamingScope failed: %v", err)
	}
	if valid.IsZero() {
		t.Error("expected a validly constructed NamingScope to report IsZero() == false")
	}
}

// TestNewAtomNameRecordRejectsZeroScope is the reproduction for the
// "uninitialized NamingScope{} silently produces a Valid == true record"
// half of the defect: NewAtomNameRecord must validate its scope argument
// instead of accepting a zero value.
func TestNewAtomNameRecordRejectsZeroScope(t *testing.T) {
	name, err := NewCanonicalName("ReserveAccountFundsUntilAuthorizationExpires")
	if err != nil {
		t.Fatalf("NewCanonicalName failed: %v", err)
	}
	rationale, err := NewNamingRationale("nearest confusing alternative name", "distinguishing qualifier text")
	if err != nil {
		t.Fatalf("NewNamingRationale failed: %v", err)
	}
	version := mustAtomVersionID(t)

	record, err := NewAtomNameRecord(version.AtomID(), version, NamingScope{}, name, rationale, 1, AtomNameRevisionID{}, time.Now().UTC())
	if err == nil {
		t.Fatalf("expected a zero NamingScope to be rejected, got a Valid == %v record", record.Valid)
	}
}

// TestDetectCanonicalNameCollisionRejectsZeroScope is the reproduction for
// the "DetectCanonicalNameCollision with a zero scope silently finds
// nothing — no error, fail-open" half of the defect.
func TestDetectCanonicalNameCollisionRejectsZeroScope(t *testing.T) {
	scope, err := NewNamingScope(mustProjectID(t), "internal/payments")
	if err != nil {
		t.Fatalf("NewNamingScope failed: %v", err)
	}
	existing := mustNameRecord(t, scope, "ReserveAccountFundsUntilAuthorizationExpires")
	candidate := DeriveNormalizedPhrase(existing.CanonicalName)

	otherAtom, err := domain.NewAtomID()
	if err != nil {
		t.Fatalf("domain.NewAtomID failed: %v", err)
	}

	if _, _, err := DetectCanonicalNameCollision(NamingScope{}, []AtomNameRecord{existing}, candidate, otherAtom); err == nil {
		t.Fatal("expected a zero query scope to be rejected with an explicit error instead of silently reporting no collision")
	}
}
