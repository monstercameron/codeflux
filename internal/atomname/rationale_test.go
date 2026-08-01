package atomname

import "testing"

// TestNewNamingRationaleRequiresNearestAlternativeAndQualifier proves
// M21-152: a naming rationale must name the nearest confusing alternative
// and the distinguishing qualifier, and rejects a trivial or empty field.
func TestNewNamingRationaleRequiresNearestAlternativeAndQualifier(t *testing.T) {
	rationale, err := NewNamingRationale(
		"ReserveAccountFunds, which only holds funds without an expiry",
		"adds an authorization-expiry deadline that releases the hold automatically",
	)
	if err != nil {
		t.Fatalf("NewNamingRationale unexpectedly failed: %v", err)
	}
	if rationale.NearestConfusingAlternative() == "" || rationale.DistinguishingQualifier() == "" {
		t.Fatalf("expected both rationale fields to be populated: %+v", rationale)
	}

	if _, err := NewNamingRationale("", "some qualifier text"); err == nil {
		t.Error("expected an empty nearest-alternative field to be rejected")
	}
	if _, err := NewNamingRationale("some alternative text", ""); err == nil {
		t.Error("expected an empty distinguishing-qualifier field to be rejected")
	}
	if _, err := NewNamingRationale("X", "Y"); err == nil {
		t.Error("expected a non-substantive single-word field to be rejected")
	}
}

func TestNamingRationaleIsZero(t *testing.T) {
	var rationale NamingRationale
	if !rationale.IsZero() {
		t.Error("expected the zero-value NamingRationale to report IsZero")
	}
	built, err := NewNamingRationale("nearest alternative name", "distinguishing qualifier text")
	if err != nil {
		t.Fatalf("NewNamingRationale failed: %v", err)
	}
	if built.IsZero() {
		t.Error("expected a constructed NamingRationale to not report IsZero")
	}
}
