package validation

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/workspace"
)

func TestFirstRunApprovalRequiresExactAttributableAuthority(t *testing.T) {
	t.Parallel()

	profile, err := SelectProfile(domain.RiskLevelProtected, []workspace.SuggestedCommand{{
		Kind: "lint", Arguments: []string{"golangci-lint", "run"}, Source: ".golangci.yml", RequiresApproval: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	check := profile.Checks[0]
	if err := ValidateFirstRunAuthority(profile, check, nil); !errors.Is(err, ErrFirstRunApprovalRequired) {
		t.Fatalf("nil authority error = %v, want ErrFirstRunApprovalRequired", err)
	}

	invalid := []FirstRunAuthority{
		{CheckID: "wrong", CommandFingerprint: check.CommandFingerprint, Actor: "user-1", AuthorityReference: "approval-1"},
		{CheckID: check.ID, CommandFingerprint: "wrong", Actor: "user-1", AuthorityReference: "approval-1"},
		{CheckID: check.ID, CommandFingerprint: check.CommandFingerprint, AuthorityReference: "approval-1"},
		{CheckID: check.ID, CommandFingerprint: check.CommandFingerprint, Actor: "user-1"},
	}
	for _, authority := range invalid {
		if err := ValidateFirstRunAuthority(profile, check, &authority); !errors.Is(err, ErrFirstRunApprovalRequired) {
			t.Errorf("authority %#v error = %v, want ErrFirstRunApprovalRequired", authority, err)
		}
	}
	exact := FirstRunAuthority{CheckID: check.ID, CommandFingerprint: check.CommandFingerprint, Actor: "user-1", AuthorityReference: "approval-1"}
	if err := ValidateFirstRunAuthority(profile, check, &exact); err != nil {
		t.Fatalf("exact authority error = %v", err)
	}

	check.RequiresFirstRunApproval = false
	if err := ValidateFirstRunAuthority(profile, check, nil); !errors.Is(err, ErrFirstRunApprovalRequired) {
		t.Fatalf("forged non-gated check error = %v, want ErrFirstRunApprovalRequired", err)
	}

	ungatedProfile, err := SelectProfile(domain.RiskLevelProtected, []workspace.SuggestedCommand{{
		Kind: "build", Arguments: []string{"go", "build", "./..."}, Source: "go.work",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFirstRunAuthority(ungatedProfile, ungatedProfile.Checks[0], nil); err != nil {
		t.Fatalf("non-gated profile check required authority: %v", err)
	}
}

func TestSkipAndWaiverOutcomesRemainHonest(t *testing.T) {
	t.Parallel()

	profile, err := SelectProfile(domain.RiskLevelProtected, []workspace.SuggestedCommand{{
		Kind: "test", Arguments: []string{"go", "test", "./internal/validation"}, Source: "go.work",
	}})
	if err != nil {
		t.Fatal(err)
	}
	check := profile.Checks[0]

	skipped, err := RecordSkippedCheck(profile, check, SkipEnvironmentUnavailable, "test service was unavailable")
	if err != nil {
		t.Fatalf("RecordSkippedCheck() error = %v", err)
	}
	if skipped.State != domain.ValidationStateSkipped || skipped.SkipReason != SkipEnvironmentUnavailable || skipped.Passed() {
		t.Fatalf("skipped outcome = %#v", skipped)
	}
	if _, err := RecordSkippedCheck(profile, check, "not-a-reason", "why"); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("invalid skip reason error = %v, want ErrInvalidProfile", err)
	}
	if _, err := RecordSkippedCheck(profile, check, SkipEnvironmentUnavailable, ""); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("empty skip explanation error = %v, want ErrInvalidProfile", err)
	}

	authority := WaiverAuthority{
		ProfileName: profile.Name, ProfileVersion: profile.Version, CheckID: check.ID,
		CommandFingerprint: check.CommandFingerprint, Actor: "user-1",
		AuthorityReference: "waiver-1", Reason: "explicitly accepted unavailable validation",
	}
	wrong := authority
	wrong.CommandFingerprint = "wrong"
	if _, err := WaiveRequiredCheck(profile, check, wrong); !errors.Is(err, ErrInvalidWaiverAuthority) {
		t.Fatalf("wrong waiver error = %v, want ErrInvalidWaiverAuthority", err)
	}

	waived, err := WaiveRequiredCheck(profile, check, authority)
	if err != nil {
		t.Fatalf("WaiveRequiredCheck() error = %v", err)
	}
	if waived.State != domain.ValidationStateWaived || waived.Waiver == nil || waived.Passed() {
		t.Fatalf("waived outcome = %#v; waived must never pass", waived)
	}
	if !(CheckOutcome{State: domain.ValidationStatePassed}).Passed() {
		t.Fatal("passed outcome was not recognized")
	}
}

func TestAdvisoryCheckCannotUseRequiredCheckWaiver(t *testing.T) {
	t.Parallel()

	profile, err := SelectProfile(domain.RiskLevelRoutine, []workspace.SuggestedCommand{{
		Kind: "build", Arguments: []string{"go", "build", "./..."}, Source: "go.work",
	}})
	if err != nil {
		t.Fatal(err)
	}
	check := profile.Checks[0]
	authority := WaiverAuthority{
		ProfileName: profile.Name, ProfileVersion: profile.Version, CheckID: check.ID,
		CommandFingerprint: check.CommandFingerprint, Actor: "user-1",
		AuthorityReference: "waiver-1", Reason: "not needed",
	}
	if _, err := WaiveRequiredCheck(profile, check, authority); !errors.Is(err, ErrInvalidWaiverAuthority) {
		t.Fatalf("advisory waiver error = %v, want ErrInvalidWaiverAuthority", err)
	}
}
