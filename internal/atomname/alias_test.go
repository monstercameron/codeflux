package atomname

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestNewAliasFromPriorCanonicalNameAndInvalidate(t *testing.T) {
	atomID, err := domain.NewAtomID()
	if err != nil {
		t.Fatalf("domain.NewAtomID failed: %v", err)
	}
	priorName, err := NewCanonicalName("ReserveAccountFunds")
	if err != nil {
		t.Fatalf("NewCanonicalName failed: %v", err)
	}
	createdAt := time.Now().UTC()

	alias, err := NewAliasFromPriorCanonicalName(atomID, priorName, createdAt)
	if err != nil {
		t.Fatalf("NewAliasFromPriorCanonicalName failed: %v", err)
	}
	if !alias.Active() {
		t.Fatal("a freshly created alias must be active")
	}
	if alias.Source != AtomNameAliasSourceRename {
		t.Errorf("expected AtomNameAliasSourceRename, got %q", alias.Source)
	}
	// normalizeAliasText must route a spaceless Go identifier through the
	// same word-split tokenization DeriveNormalizedPhrase uses (defect
	// DEFECT-3), not a bare lowercase+whitespace-collapse pass: otherwise
	// "reserveaccountfunds" (alias) can never equal "reserve account funds"
	// (canonical phrase) for the same identifier, and the
	// ComposeNameEmbeddingInput dedup that depends on that equality can
	// never fire for a rename-created alias.
	if want := DeriveNormalizedPhrase(priorName).String(); alias.NormalizedAliasText != want {
		t.Errorf("unexpected normalized alias text %q, want %q (matching DeriveNormalizedPhrase's tokenization)", alias.NormalizedAliasText, want)
	}
	if alias.NormalizedAliasText != "reserve account funds" {
		t.Errorf("unexpected normalized alias text %q", alias.NormalizedAliasText)
	}

	invalidatedAt := createdAt.Add(time.Hour)
	invalidated, err := alias.Invalidate("superseded by a later rename", invalidatedAt)
	if err != nil {
		t.Fatalf("Invalidate failed: %v", err)
	}
	if invalidated.Active() {
		t.Error("an invalidated alias must no longer be active")
	}
	if invalidated.InvalidatedReason == "" {
		t.Error("expected a non-empty invalidation reason")
	}
	// Invalidate must not mutate the original alias value (immutability).
	if !alias.Active() {
		t.Error("Invalidate must not mutate its receiver")
	}

	if _, err := invalidated.Invalidate("again", invalidatedAt.Add(time.Hour)); err == nil {
		t.Error("expected invalidating an already-invalidated alias to fail")
	}
	if _, err := alias.Invalidate("", invalidatedAt); err == nil {
		t.Error("expected an empty invalidation reason to be rejected")
	}
}

func TestNewAliasFromPriorCanonicalNameRejectsZeroInputs(t *testing.T) {
	atomID, err := domain.NewAtomID()
	if err != nil {
		t.Fatalf("domain.NewAtomID failed: %v", err)
	}
	priorName, err := NewCanonicalName("ReserveAccountFunds")
	if err != nil {
		t.Fatalf("NewCanonicalName failed: %v", err)
	}
	if _, err := NewAliasFromPriorCanonicalName(domain.AtomID{}, priorName, time.Now()); err == nil {
		t.Error("expected a zero atom identity to be rejected")
	}
	if _, err := NewAliasFromPriorCanonicalName(atomID, CanonicalName{}, time.Now()); err == nil {
		t.Error("expected a zero prior canonical name to be rejected")
	}
	if _, err := NewAliasFromPriorCanonicalName(atomID, priorName, time.Time{}); err == nil {
		t.Error("expected a zero created-at timestamp to be rejected")
	}
}
