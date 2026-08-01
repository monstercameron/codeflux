package atomname

import "testing"

// TestGraphTruncationNeverChangesStoredCanonicalOrAccessibleNames is
// M21-175: truncating the visual graph-node label must never alter the
// stored AtomNameRecord's canonical name, display name, or normalized
// phrase — those remain the full, accessible names for tooltip, inspector,
// search result, and accessibility label use.
func TestGraphTruncationNeverChangesStoredCanonicalOrAccessibleNames(t *testing.T) {
	scope, err := NewNamingScope(mustProjectID(t), "internal/payments")
	if err != nil {
		t.Fatalf("NewNamingScope failed: %v", err)
	}
	record := mustNameRecord(t, scope, "ReserveAccountFundsUntilAuthorizationExpires")

	originalCanonical := record.CanonicalName.String()
	originalDisplay := record.DisplayName.String()
	originalNormalized := record.NormalizedPhrase.String()

	label := TruncateGraphNodeLabel(record.DisplayName, 12)
	if !label.Truncated {
		t.Fatal("expected a display name longer than the budget to be truncated")
	}
	if len([]rune(label.Text)) > 12 {
		t.Errorf("truncated label %q exceeds the requested rune budget", label.Text)
	}

	if record.CanonicalName.String() != originalCanonical {
		t.Error("truncation must not change the stored canonical name")
	}
	if record.DisplayName.String() != originalDisplay {
		t.Error("truncation must not change the stored display name")
	}
	if record.NormalizedPhrase.String() != originalNormalized {
		t.Error("truncation must not change the stored normalized phrase")
	}

	// The full, untruncated names remain independently accessible for
	// tooltip, inspector, search-result, and accessibility-label use.
	if record.DisplayName.String() == label.Text {
		t.Fatal("test fixture did not actually exercise truncation")
	}
}

func TestTruncateGraphNodeLabelNoTruncationWhenLabelFits(t *testing.T) {
	name, err := NewCanonicalName("DeriveKey")
	if err != nil {
		t.Fatalf("NewCanonicalName failed: %v", err)
	}
	display := DeriveDisplayName(name)
	label := TruncateGraphNodeLabel(display, 100)
	if label.Truncated {
		t.Error("expected no truncation when the label fits within the budget")
	}
	if label.Text != display.String() {
		t.Errorf("expected the untruncated text to equal the display name, got %q", label.Text)
	}
}

func TestTruncateGraphNodeLabelNonPositiveBudgetMeansNoTruncation(t *testing.T) {
	name, err := NewCanonicalName("ReserveAccountFundsUntilAuthorizationExpires")
	if err != nil {
		t.Fatalf("NewCanonicalName failed: %v", err)
	}
	display := DeriveDisplayName(name)
	label := TruncateGraphNodeLabel(display, 0)
	if label.Truncated || label.Text != display.String() {
		t.Errorf("expected a non-positive budget to skip truncation, got %+v", label)
	}
}

func TestTruncateGraphNodeLabelExtremelyNarrowBudget(t *testing.T) {
	name, err := NewCanonicalName("ReserveAccountFundsUntilAuthorizationExpires")
	if err != nil {
		t.Fatalf("NewCanonicalName failed: %v", err)
	}
	display := DeriveDisplayName(name)
	label := TruncateGraphNodeLabel(display, 1)
	if !label.Truncated {
		t.Fatal("expected truncation for a budget narrower than the ellipsis")
	}
	if len([]rune(label.Text)) != 1 {
		t.Errorf("expected exactly one rune of output for a budget of 1, got %q", label.Text)
	}
}
