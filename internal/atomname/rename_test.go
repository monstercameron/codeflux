package atomname

import (
	"errors"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/domain"
)

var (
	contractHashA = strings.Repeat("1", 64)
	contractHashB = strings.Repeat("2", 64)
)

func mustRenameAcceptance(t *testing.T, reviewerIdentity string) RenameAcceptance {
	t.Helper()
	acceptance, err := NewRenameAcceptance(reviewerIdentity)
	if err != nil {
		t.Fatalf("NewRenameAcceptance(%q) failed: %v", reviewerIdentity, err)
	}
	return acceptance
}

func mustContractHash(t *testing.T, raw string) atomdoc.ContractHash {
	t.Helper()
	hash, err := atomdoc.ParseContractHash(raw)
	if err != nil {
		t.Fatalf("atomdoc.ParseContractHash(%q) failed: %v", raw, err)
	}
	return hash
}

// TestClassifyRenameFormattingOnly is part of M21-157: identical words,
// unchanged contract, is formatting-only.
func TestClassifyRenameFormattingOnly(t *testing.T) {
	// A formatting-only rename splits into the exact same words
	// case-insensitively (here, only the capitalization of the HTTP
	// initialism differs) while the pinned contract is unchanged.
	proposal := RenameProposal{
		OldName:         mustCanonicalName(t, "DeriveHTTPRequestID"),
		NewName:         mustCanonicalName(t, "DeriveHttpRequestID"),
		OldContractHash: mustContractHash(t, contractHashA),
		NewContractHash: mustContractHash(t, contractHashA),
	}

	classification, err := ClassifyRename(proposal)
	if err != nil {
		t.Fatalf("ClassifyRename failed: %v", err)
	}
	if classification != RenameClassificationFormattingOnly {
		t.Errorf("expected formatting-only classification, got %q", classification)
	}
}

func TestClassifyRenameSemanticPreserving(t *testing.T) {
	proposal := RenameProposal{
		OldName:         mustCanonicalName(t, "ReserveAccountFunds"),
		NewName:         mustCanonicalName(t, "ReserveAccountFundsUntilAuthorizationExpires"),
		OldContractHash: mustContractHash(t, contractHashA),
		NewContractHash: mustContractHash(t, contractHashA),
	}
	classification, err := ClassifyRename(proposal)
	if err != nil {
		t.Fatalf("ClassifyRename failed: %v", err)
	}
	if classification != RenameClassificationSemanticPreserving {
		t.Errorf("expected semantic-preserving classification, got %q", classification)
	}
}

func TestClassifyRenameSemanticBreakingOnContractChange(t *testing.T) {
	proposal := RenameProposal{
		OldName:         mustCanonicalName(t, "ReserveAccountFunds"),
		NewName:         mustCanonicalName(t, "ReserveAccountFundsUntilAuthorizationExpires"),
		OldContractHash: mustContractHash(t, contractHashA),
		NewContractHash: mustContractHash(t, contractHashB),
	}
	classification, err := ClassifyRename(proposal)
	if err != nil {
		t.Fatalf("ClassifyRename failed: %v", err)
	}
	if classification != RenameClassificationSemanticBreaking {
		t.Errorf("expected semantic-breaking classification, got %q", classification)
	}
}

func TestClassifyRenameSemanticBreakingOnAssertedChange(t *testing.T) {
	proposal := RenameProposal{
		OldName:                mustCanonicalName(t, "DeriveHTTPRequestID"),
		NewName:                mustCanonicalName(t, "DeriveHttpRequestID"),
		OldContractHash:        mustContractHash(t, contractHashA),
		NewContractHash:        mustContractHash(t, contractHashA),
		SemanticChangeAsserted: true,
	}
	classification, err := ClassifyRename(proposal)
	if err != nil {
		t.Fatalf("ClassifyRename failed: %v", err)
	}
	if classification != RenameClassificationSemanticBreaking {
		t.Errorf("an asserted semantic change must classify as semantic-breaking even with identical words, got %q", classification)
	}
}

// TestAuthorizeCanonicalRenameSemanticPreservingRequiresReview is M21-158.
func TestAuthorizeCanonicalRenameSemanticPreservingRequiresReview(t *testing.T) {
	version := mustAtomVersionID(t)
	proposal := RenameProposal{
		OldName:         mustCanonicalName(t, "ReserveAccountFunds"),
		NewName:         mustCanonicalName(t, "ReserveAccountFundsUntilAuthorizationExpires"),
		OldContractHash: mustContractHash(t, contractHashA),
		NewContractHash: mustContractHash(t, contractHashA),
	}

	_, err := AuthorizeCanonicalRename(proposal, RenameAcceptance{}, version, version, time.Now().UTC())
	if !errors.Is(err, ErrRenameReviewRequired) {
		t.Fatalf("expected ErrRenameReviewRequired without review approval, got %v", err)
	}

	outcome, err := AuthorizeCanonicalRename(proposal, mustRenameAcceptance(t, "qa-reviewer"), version, version, time.Now().UTC())
	if err != nil {
		t.Fatalf("AuthorizeCanonicalRename unexpectedly failed with review approval: %v", err)
	}
	if outcome.Classification != RenameClassificationSemanticPreserving {
		t.Fatalf("expected semantic-preserving classification, got %q", outcome.Classification)
	}
	if outcome.AtomID != version.AtomID() {
		t.Errorf("expected the atom ID to be retained across a semantic-preserving rename")
	}
	if outcome.EffectiveAtomVersion != version {
		t.Errorf("expected the atom version to be retained across a semantic-preserving rename")
	}
	if outcome.CreatedAlias == nil {
		t.Fatal("expected a semantic-preserving rename to create a prior-name alias")
	}
	if outcome.CreatedAlias.AliasText != proposal.OldName.String() {
		t.Errorf("expected the created alias to carry the old canonical name, got %q", outcome.CreatedAlias.AliasText)
	}
	if !outcome.RequiresNewDocumentationRevision {
		t.Error("expected an accepted rename to require a new documentation revision (M21-160)")
	}
}

// TestAuthorizeCanonicalRenameSemanticBreakingCannotRetainOldIdentity is
// M21-159 and M21-173: a semantic-breaking rename must not silently keep
// the exact same atom version.
func TestAuthorizeCanonicalRenameSemanticBreakingCannotRetainOldIdentity(t *testing.T) {
	version := mustAtomVersionID(t)
	proposal := RenameProposal{
		OldName:         mustCanonicalName(t, "ReserveAccountFunds"),
		NewName:         mustCanonicalName(t, "CaptureAccountFunds"),
		OldContractHash: mustContractHash(t, contractHashA),
		NewContractHash: mustContractHash(t, contractHashB),
	}

	// Attempting to pass the exact same atom-version identity through for a
	// semantic-breaking rename must fail loudly, not silently succeed.
	_, err := AuthorizeCanonicalRename(proposal, RenameAcceptance{}, version, version, time.Now().UTC())
	if !errors.Is(err, ErrRenameRequiresNewIdentity) {
		t.Fatalf("expected ErrRenameRequiresNewIdentity when old and new atom versions are identical, got %v", err)
	}

	// A bumped version number on the same atom identity is accepted.
	bumped, err := atomdoc.NewAtomVersionID(version.AtomID(), version.Number()+1)
	if err != nil {
		t.Fatalf("atomdoc.NewAtomVersionID failed: %v", err)
	}
	outcome, err := AuthorizeCanonicalRename(proposal, RenameAcceptance{}, version, bumped, time.Now().UTC())
	if err != nil {
		t.Fatalf("AuthorizeCanonicalRename unexpectedly failed with a bumped atom version: %v", err)
	}
	if outcome.Classification != RenameClassificationSemanticBreaking {
		t.Fatalf("expected semantic-breaking classification, got %q", outcome.Classification)
	}
	if outcome.EffectiveAtomVersion != bumped {
		t.Errorf("expected the effective atom version to be the bumped version")
	}
	if outcome.CreatedAlias != nil {
		t.Error("a semantic-breaking rename must not automatically alias the old name to the new, semantically different atom")
	}

	// A brand-new atom identity is also accepted.
	newAtomID, err := domain.NewAtomID()
	if err != nil {
		t.Fatalf("domain.NewAtomID failed: %v", err)
	}
	newIdentity, err := atomdoc.NewAtomVersionID(newAtomID, 1)
	if err != nil {
		t.Fatalf("atomdoc.NewAtomVersionID failed: %v", err)
	}
	outcome, err = AuthorizeCanonicalRename(proposal, RenameAcceptance{}, version, newIdentity, time.Now().UTC())
	if err != nil {
		t.Fatalf("AuthorizeCanonicalRename unexpectedly failed with a new atom identity: %v", err)
	}
	if outcome.AtomID != newAtomID {
		t.Errorf("expected the outcome atom ID to be the new atom identity")
	}
}

func TestAuthorizeCanonicalRenameFormattingOnlyMustKeepSameVersion(t *testing.T) {
	version := mustAtomVersionID(t)
	other, err := atomdoc.NewAtomVersionID(version.AtomID(), version.Number()+1)
	if err != nil {
		t.Fatalf("atomdoc.NewAtomVersionID failed: %v", err)
	}
	proposal := RenameProposal{
		OldName:         mustCanonicalName(t, "DeriveHTTPRequestID"),
		NewName:         mustCanonicalName(t, "DeriveHttpRequestID"),
		OldContractHash: mustContractHash(t, contractHashA),
		NewContractHash: mustContractHash(t, contractHashA),
	}
	if _, err := AuthorizeCanonicalRename(proposal, RenameAcceptance{}, version, other, time.Now().UTC()); err == nil {
		t.Error("expected a formatting-only rename with a changed atom version to be rejected")
	}
}

// TestNewRenameAcceptanceRequiresReviewerIdentity covers the structural
// tightening of RenameAcceptance: a bare exported bool a caller could set to
// true with no evidence is replaced by an unexported field only settable
// through a constructor that requires a non-empty reviewer identity. This
// does not make review real (a caller can still fabricate any string), but
// it does mean a rename can no longer be "approved" by an accidental or
// default-valued struct literal — RenameAcceptance{} (the zero value)
// reports Approved() == false.
func TestNewRenameAcceptanceRequiresReviewerIdentity(t *testing.T) {
	if (RenameAcceptance{}).Approved() {
		t.Error("expected the zero-value RenameAcceptance to report Approved() == false")
	}
	if _, err := NewRenameAcceptance(""); err == nil {
		t.Error("expected an empty reviewer identity to be rejected")
	}
	if _, err := NewRenameAcceptance("   "); err == nil {
		t.Error("expected a whitespace-only reviewer identity to be rejected")
	}

	acceptance, err := NewRenameAcceptance("qa-reviewer@example.internal")
	if err != nil {
		t.Fatalf("NewRenameAcceptance failed: %v", err)
	}
	if !acceptance.Approved() {
		t.Error("expected a constructed RenameAcceptance to report Approved() == true")
	}
	if acceptance.ReviewerIdentity() != "qa-reviewer@example.internal" {
		t.Errorf("unexpected reviewer identity %q", acceptance.ReviewerIdentity())
	}
}

func TestClassifyRenameRejectsInvalidProposals(t *testing.T) {
	name := mustCanonicalName(t, "ReserveAccountFunds")
	if _, err := ClassifyRename(RenameProposal{OldName: name, NewName: name, OldContractHash: mustContractHash(t, contractHashA), NewContractHash: mustContractHash(t, contractHashA)}); err == nil {
		t.Error("expected identical old and new names to be rejected")
	}
	if _, err := ClassifyRename(RenameProposal{OldName: name, NewName: mustCanonicalName(t, "CaptureAccountFunds")}); err == nil {
		t.Error("expected missing contract hashes to be rejected")
	}
}
