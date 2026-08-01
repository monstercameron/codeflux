package atomname

import (
	"testing"
	"time"
)

func mustEmbeddingTestRecord(t *testing.T, canonical string) AtomNameRecord {
	t.Helper()
	scope, err := NewNamingScope(mustProjectID(t), "internal/payments")
	if err != nil {
		t.Fatalf("NewNamingScope failed: %v", err)
	}
	return mustNameRecord(t, scope, canonical)
}

// TestComposeNameEmbeddingInputIncludesCanonicalAndPhraseExactlyOnce is
// M21-161.
func TestComposeNameEmbeddingInputIncludesCanonicalAndPhraseExactlyOnce(t *testing.T) {
	record := mustEmbeddingTestRecord(t, "ReserveAccountFundsUntilAuthorizationExpires")
	input, err := ComposeNameEmbeddingInput(record, nil)
	if err != nil {
		t.Fatalf("ComposeNameEmbeddingInput failed: %v", err)
	}

	// AGENTS.md "Atom Naming Style" and docs/plan.md §7 require embedding
	// "the canonical name and its word-split normalized phrase" — two
	// textually distinct fields, not the same phrase twice under different
	// role labels. The canonical-name segment must therefore carry the
	// actual Go identifier (record.CanonicalName), not record.DisplayName:
	// DisplayName and NormalizedPhrase differ only by case
	// ("Reserve Account..." vs "reserve account..."), so emitting
	// DisplayName here would restate the normalized phrase a second time
	// under a different label instead of contributing new information.
	canonicalCount, phraseCount := 0, 0
	for _, segment := range input.Segments {
		switch segment.Role {
		case EmbeddingSegmentRoleCanonicalName:
			canonicalCount++
			if segment.Text != record.CanonicalName.String() {
				t.Errorf("canonical segment text = %q, want %q", segment.Text, record.CanonicalName.String())
			}
		case EmbeddingSegmentRoleNormalizedPhrase:
			phraseCount++
			if segment.Text != record.NormalizedPhrase.String() {
				t.Errorf("normalized-phrase segment text = %q, want %q", segment.Text, record.NormalizedPhrase.String())
			}
		}
	}
	if canonicalCount != 1 {
		t.Errorf("expected exactly one canonical-name segment, got %d", canonicalCount)
	}
	if phraseCount != 1 {
		t.Errorf("expected exactly one normalized-phrase segment, got %d", phraseCount)
	}
}

// TestComposeNameEmbeddingInputAliasesAreLowWeightAndNotDuplicated is
// M21-162. The "duplicate" alias is produced through the real
// rename-creates-alias path (AuthorizeCanonicalRename ->
// NewAliasFromPriorCanonicalName), not manufactured by feeding
// already-normalized phrase text straight into NewReviewedManualAlias: the
// realistic source of a phrase-duplicating alias is a formatting-only
// rename (a prior spelling of the exact same canonical name, differing only
// in an initialism's casing), and the dedup only means anything if it can
// catch that case (DEFECT-3: normalizeAliasText must tokenize like
// DeriveNormalizedPhrase for this rename-created alias to normalize
// identically to the canonical phrase in the first place).
func TestComposeNameEmbeddingInputAliasesAreLowWeightAndNotDuplicated(t *testing.T) {
	record := mustEmbeddingTestRecord(t, "ValidateHTTPRequestPayload")
	createdAt := time.Now().UTC()

	distinctAlias, err := NewReviewedManualAlias(record.AtomID, "check inbound web payload", createdAt)
	if err != nil {
		t.Fatalf("NewReviewedManualAlias failed: %v", err)
	}

	renameOutcome, err := AuthorizeCanonicalRename(
		RenameProposal{
			OldName:         mustCanonicalName(t, "ValidateHttpRequestPayload"),
			NewName:         record.CanonicalName,
			OldContractHash: mustContractHash(t, contractHashA),
			NewContractHash: mustContractHash(t, contractHashA),
		},
		mustRenameAcceptance(t, "qa-reviewer"),
		record.AtomVersion,
		record.AtomVersion,
		createdAt,
	)
	if err != nil {
		t.Fatalf("AuthorizeCanonicalRename failed: %v", err)
	}
	if renameOutcome.CreatedAlias == nil {
		t.Fatal("expected the rename to create a prior-name alias")
	}
	duplicateAlias := *renameOutcome.CreatedAlias

	// The rename-created alias's normalized text must actually collide with
	// the canonical phrase for this test to exercise what it claims to;
	// this is the direct, real-path evidence for DEFECT-3's fix.
	if duplicateAlias.NormalizedAliasText != record.NormalizedPhrase.String() {
		t.Fatalf("test fixture does not reproduce a real phrase-duplicating alias: alias normalized to %q, canonical phrase is %q",
			duplicateAlias.NormalizedAliasText, record.NormalizedPhrase.String())
	}

	input, err := ComposeNameEmbeddingInput(record, []AtomNameAliasRecord{distinctAlias, duplicateAlias})
	if err != nil {
		t.Fatalf("ComposeNameEmbeddingInput failed: %v", err)
	}

	var aliasSegments int
	for _, segment := range input.Segments {
		if segment.Role != EmbeddingSegmentRoleAliasDiscovery {
			continue
		}
		aliasSegments++
		if !segment.LowWeight {
			t.Errorf("expected alias segment %q to be marked low-weight", segment.Text)
		}
		if segment.Text == duplicateAlias.AliasText {
			t.Errorf("expected the canonical-phrase-duplicating rename alias to be excluded, found segment %q", segment.Text)
		}
	}
	if aliasSegments != 1 {
		t.Fatalf("expected exactly one non-duplicate alias segment, got %d", aliasSegments)
	}
	if input.ExcludedAliasCount != 1 {
		t.Errorf("expected ExcludedAliasCount == 1 for the duplicate alias, got %d", input.ExcludedAliasCount)
	}
}

// TestComposeNameEmbeddingInputExcludesObsoleteAliasesButRetainsLineage is
// M21-163.
func TestComposeNameEmbeddingInputExcludesObsoleteAliasesButRetainsLineage(t *testing.T) {
	record := mustEmbeddingTestRecord(t, "ReserveAccountFundsUntilAuthorizationExpires")
	createdAt := time.Now().UTC()

	active, err := NewReviewedManualAlias(record.AtomID, "hold funds pending authorization", createdAt)
	if err != nil {
		t.Fatalf("NewReviewedManualAlias failed: %v", err)
	}
	obsolete, err := NewReviewedManualAlias(record.AtomID, "old discovery phrase", createdAt)
	if err != nil {
		t.Fatalf("NewReviewedManualAlias failed: %v", err)
	}
	obsolete, err = obsolete.Invalidate("superseded by a clearer alias", createdAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("Invalidate failed: %v", err)
	}

	fullAliasSet := []AtomNameAliasRecord{active, obsolete}
	input, err := ComposeNameEmbeddingInput(record, fullAliasSet)
	if err != nil {
		t.Fatalf("ComposeNameEmbeddingInput failed: %v", err)
	}

	for _, segment := range input.Segments {
		if segment.Text == obsolete.AliasText {
			t.Errorf("expected the obsolete alias to be excluded from active candidate generation")
		}
	}
	if input.ExcludedAliasCount != 1 {
		t.Errorf("expected ExcludedAliasCount == 1 for the obsolete alias, got %d", input.ExcludedAliasCount)
	}

	// Lineage retention means the caller's full alias slice — the thing the
	// storage lane actually persists — is untouched by composition.
	if len(fullAliasSet) != 2 || fullAliasSet[1].AliasText != obsolete.AliasText {
		t.Fatalf("ComposeNameEmbeddingInput must not mutate or drop entries from the caller's alias lineage slice")
	}
}

func TestComposeNameEmbeddingInputRejectsInvalidRecord(t *testing.T) {
	record := mustEmbeddingTestRecord(t, "ReserveAccountFundsUntilAuthorizationExpires")
	record.Valid = false
	if _, err := ComposeNameEmbeddingInput(record, nil); err == nil {
		t.Error("expected composing embedding input from a non-current record to be rejected")
	}
}

func TestDetermineEmbeddingInvalidationTriggers(t *testing.T) {
	recordBefore := mustEmbeddingTestRecord(t, "ReserveAccountFunds")
	recordAfter := mustEmbeddingTestRecord(t, "ReserveAccountFundsUntilAuthorizationExpires")

	before, err := ComposeNameEmbeddingInput(recordBefore, nil)
	if err != nil {
		t.Fatalf("ComposeNameEmbeddingInput failed: %v", err)
	}
	after, err := ComposeNameEmbeddingInput(recordAfter, nil)
	if err != nil {
		t.Fatalf("ComposeNameEmbeddingInput failed: %v", err)
	}

	triggers := DetermineEmbeddingInvalidationTriggers(before, after)
	if len(triggers) != 1 || triggers[0] != EmbeddingInvalidationTriggerCanonicalContentChanged {
		t.Errorf("expected exactly the canonical-content-changed trigger, got %v", triggers)
	}

	if triggers := DetermineEmbeddingInvalidationTriggers(before, before); len(triggers) != 0 {
		t.Errorf("expected no triggers for two identical snapshots, got %v", triggers)
	}
}
