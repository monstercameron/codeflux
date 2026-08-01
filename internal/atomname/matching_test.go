package atomname

import (
	"testing"
	"time"
)

// TestMatchAtomNamesAndAliasesFindsCanonicalNormalizedAndAliasChannels
// supports M21-167 and M21-168: matches are found through the canonical
// name, normalized phrase, and active-alias channels, with the channel and
// matched text recorded for attribution.
func TestMatchAtomNamesAndAliasesFindsCanonicalNormalizedAndAliasChannels(t *testing.T) {
	scope, err := NewNamingScope(mustProjectID(t), "internal/payments")
	if err != nil {
		t.Fatalf("NewNamingScope failed: %v", err)
	}
	record := mustNameRecord(t, scope, "ReserveAccountFundsUntilAuthorizationExpires")

	byCanonical := MatchAtomNamesAndAliases("ReserveAccountFundsUntilAuthorizationExpires", []AtomNameRecord{record}, nil)
	if len(byCanonical) != 1 || byCanonical[0].Channel != NameMatchChannelCanonicalName {
		t.Fatalf("expected one canonical-name match, got %v", byCanonical)
	}

	byPhrase := MatchAtomNamesAndAliases("reserve account funds until authorization expires", []AtomNameRecord{record}, nil)
	if len(byPhrase) != 1 || byPhrase[0].Channel != NameMatchChannelNormalizedPhrase {
		t.Fatalf("expected one normalized-phrase match, got %v", byPhrase)
	}

	alias, err := NewReviewedManualAlias(record.AtomID, "hold funds pending authorization", time.Now().UTC())
	if err != nil {
		t.Fatalf("NewReviewedManualAlias failed: %v", err)
	}
	byAlias := MatchAtomNamesAndAliases("hold funds pending authorization", nil, []AtomNameAliasRecord{alias})
	if len(byAlias) != 1 || byAlias[0].Channel != NameMatchChannelActiveAlias || byAlias[0].AtomID != record.AtomID {
		t.Fatalf("expected one active-alias match for the correct atom, got %v", byAlias)
	}
}

// TestOldAliasFindsRenamedAtomButDoesNotBypassApplicability is M21-174: an
// alias created from a rename still resolves the renamed atom through the
// alias channel, and the returned NameMatch carries no applicability,
// compatibility, or assurance signal — only identity and channel.
func TestOldAliasFindsRenamedAtomButDoesNotBypassApplicability(t *testing.T) {
	scope, err := NewNamingScope(mustProjectID(t), "internal/payments")
	if err != nil {
		t.Fatalf("NewNamingScope failed: %v", err)
	}
	oldName := mustCanonicalName(t, "ReserveAccountFunds")
	newRecord := mustNameRecord(t, scope, "ReserveAccountFundsUntilAuthorizationExpires")

	outcome, err := AuthorizeCanonicalRename(
		RenameProposal{
			OldName:         oldName,
			NewName:         newRecord.CanonicalName,
			OldContractHash: mustContractHash(t, contractHashA),
			NewContractHash: mustContractHash(t, contractHashA),
		},
		mustRenameAcceptance(t, "qa-reviewer"),
		newRecord.AtomVersion,
		newRecord.AtomVersion,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("AuthorizeCanonicalRename failed: %v", err)
	}
	if outcome.CreatedAlias == nil {
		t.Fatal("expected the rename to create a prior-name alias")
	}

	matches := MatchAtomNamesAndAliases(oldName.String(), nil, []AtomNameAliasRecord{*outcome.CreatedAlias})
	if len(matches) != 1 {
		t.Fatalf("expected the old name to resolve the renamed atom through its alias, got %v", matches)
	}
	match := matches[0]
	if match.AtomID != newRecord.AtomID {
		t.Errorf("expected the alias match to resolve to the renamed atom's identity")
	}
	if match.Channel != NameMatchChannelActiveAlias {
		t.Errorf("expected the alias channel to be recorded, got %q", match.Channel)
	}

	// NameMatch has exactly three fields — AtomID, Channel, and
	// MatchedText (see matching.go) — so this discovery result
	// structurally cannot itself grant applicability, compatibility,
	// evidence, or assurance. A retrieval gate must run those checks
	// separately on match.AtomID before using it.
}
