package validation

import (
	"errors"
	"slices"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/workspace"
)

func TestValidationProfileFloorsAreVersionedAndCannotWeaken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		risk       domain.RiskLevel
		profile    ProfileName
		required   []RequirementKind
		advisory   []RequirementKind
		prohibited []RequirementKind
	}{
		{
			name: "routine", risk: domain.RiskLevelRoutine, profile: ProfileRoutine,
			required:   []RequirementKind{RequirementFormatter, RequirementTargetedTests, RequirementStaticAnalysis, RequirementDiffReview},
			advisory:   []RequirementKind{RequirementBroadTests, RequirementBuild},
			prohibited: []RequirementKind{RequirementIndependentReview, RequirementAcceptanceSuite},
		},
		{
			name: "elevated", risk: domain.RiskLevelElevated, profile: ProfileElevated,
			required: []RequirementKind{
				RequirementFormatter, RequirementTargetedTests, RequirementBroadTests,
				RequirementBuild, RequirementStaticAnalysis, RequirementDiffReview,
				RequirementRegressionAnalysis, RequirementIndependentReview,
			},
			prohibited: []RequirementKind{RequirementAcceptanceSuite, RequirementSecurityReview},
		},
		{
			name: "protected", risk: domain.RiskLevelProtected, profile: ProfileProtected,
			required: []RequirementKind{
				RequirementFormatter, RequirementTargetedTests, RequirementBroadTests,
				RequirementBuild, RequirementStaticAnalysis, RequirementDiffReview,
				RequirementRegressionAnalysis, RequirementIndependentReview,
				RequirementAcceptanceSuite, RequirementSecurityReview, RequirementDomainReview,
				RequirementProofObligations, RequirementExternalAssumptionResolution,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			profile, err := SelectProfile(test.risk, nil)
			if err != nil {
				t.Fatalf("SelectProfile() error = %v", err)
			}
			if profile.PolicyVersion != PolicyVersion || profile.Version != ProfileVersionV1 || profile.Name != test.profile {
				t.Fatalf("profile identity = %#v, want %q/%q/%q", profile, PolicyVersion, ProfileVersionV1, test.profile)
			}
			for _, kind := range test.required {
				if got := requirementStrength(profile, kind); got != RequirementRequired {
					t.Errorf("requirement %q = %q, want required", kind, got)
				}
			}
			for _, kind := range test.advisory {
				if got := requirementStrength(profile, kind); got != RequirementAdvisory {
					t.Errorf("requirement %q = %q, want advisory", kind, got)
				}
			}
			for _, kind := range test.prohibited {
				if got := requirementStrength(profile, kind); got != "" {
					t.Errorf("unexpected requirement %q = %q", kind, got)
				}
			}

			digest, err := profile.Digest()
			if err != nil || len(digest) != 64 {
				t.Fatalf("Digest() = %q, %v", digest, err)
			}
			again, err := profile.Digest()
			if err != nil || again != digest {
				t.Fatalf("second Digest() = %q, %v; want %q", again, err, digest)
			}

			weakened := profile
			weakened.Requirements = slices.Clone(profile.Requirements)
			weakened.Requirements[0].Strength = RequirementAdvisory
			if err := weakened.Validate(); !errors.Is(err, ErrInvalidProfile) {
				t.Fatalf("weakened profile Validate() error = %v, want ErrInvalidProfile", err)
			}
		})
	}
}

func TestValidationProfileMapsDiscoveredCommandsAndExecutionPolicy(t *testing.T) {
	t.Parallel()

	commands := []workspace.SuggestedCommand{
		{Kind: "format", Arguments: []string{"gofmt", "-w", "."}, Source: "go.work"},
		{Kind: "test", Arguments: []string{"go", "test", "./internal/validation"}, Source: "go.work"},
		{Kind: "test", Arguments: []string{"go", "test", "./..."}, Source: "go.work"},
		{Kind: "build", Arguments: []string{"go", "build", "./..."}, Source: "go.work"},
		{Kind: "lint", Arguments: []string{"golangci-lint", "run"}, Source: ".golangci.yml", RequiresApproval: true},
		{Kind: "repository-suggested", Arguments: []string{"make", "dangerous"}, Source: "Makefile", RequiresApproval: true},
	}

	for _, test := range []struct {
		name          string
		risk          domain.RiskLevel
		broadStrength RequirementStrength
	}{
		{name: "routine", risk: domain.RiskLevelRoutine, broadStrength: RequirementAdvisory},
		{name: "protected", risk: domain.RiskLevelProtected, broadStrength: RequirementRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			profile, err := SelectProfile(test.risk, commands)
			if err != nil {
				t.Fatalf("SelectProfile() error = %v", err)
			}
			if len(profile.Checks) != 5 {
				t.Fatalf("len(Checks) = %d, want 5 (unknown recipe must not be trusted)", len(profile.Checks))
			}

			checks := checksByClass(profile.Checks)
			assertCheckPolicy(t, checks[CheckFormatter], RequirementRequired, 2*time.Minute, 1, nil)
			assertCheckPolicy(t, checks[CheckTargetedTest], RequirementRequired, 5*time.Minute, 2, []RetryFailure{RetryFailureInfrastructureUnavailable})
			assertCheckPolicy(t, checks[CheckBroadTest], test.broadStrength, 15*time.Minute, 1, nil)
			assertCheckPolicy(t, checks[CheckBuild], test.broadStrength, 10*time.Minute, 1, nil)
			assertCheckPolicy(t, checks[CheckStaticAnalysis], RequirementRequired, 10*time.Minute, 1, nil)
			if !checks[CheckStaticAnalysis].RequiresFirstRunApproval {
				t.Error("static analysis check did not preserve first-run approval policy")
			}
		})
	}
}

func TestValidationProfileRejectsMalformedOrDuplicateCommands(t *testing.T) {
	t.Parallel()

	bad := []workspace.SuggestedCommand{
		{Kind: "test", Source: "go.work"},
		{Kind: "test", Arguments: []string{" go", "test"}, Source: "go.work"},
		{Kind: "test", Arguments: []string{"go", "test"}, Source: " go.work"},
	}
	for _, command := range bad {
		if _, err := SelectProfile(domain.RiskLevelRoutine, []workspace.SuggestedCommand{command}); !errors.Is(err, ErrInvalidDiscoveredCommand) {
			t.Errorf("SelectProfile(%#v) error = %v, want ErrInvalidDiscoveredCommand", command, err)
		}
	}

	command := workspace.SuggestedCommand{Kind: "test", Arguments: []string{"go", "test", "./..."}, Source: "go.work"}
	if _, err := SelectProfile(domain.RiskLevelRoutine, []workspace.SuggestedCommand{command, command}); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("duplicate command error = %v, want ErrInvalidProfile", err)
	}
}

func TestValidationProfileRejectsForgedCheckPolicy(t *testing.T) {
	t.Parallel()

	profile, err := SelectProfile(domain.RiskLevelProtected, []workspace.SuggestedCommand{{
		Kind: "test", Arguments: []string{"go", "test", "./internal/validation"}, Source: "go.work",
	}})
	if err != nil {
		t.Fatal(err)
	}
	forged := profile
	forged.Checks = slices.Clone(profile.Checks)
	forged.Checks[0].Retry.MaximumAttempts = 9
	if err := forged.Validate(); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("forged retry Validate() error = %v, want ErrInvalidProfile", err)
	}
}

func requirementStrength(profile Profile, kind RequirementKind) RequirementStrength {
	for _, requirement := range profile.Requirements {
		if requirement.Kind == kind {
			return requirement.Strength
		}
	}
	return ""
}

func checksByClass(checks []Check) map[CheckClass]Check {
	byClass := make(map[CheckClass]Check, len(checks))
	for _, check := range checks {
		byClass[check.Class] = check
	}
	return byClass
}

func assertCheckPolicy(t *testing.T, check Check, strength RequirementStrength, timeout time.Duration, attempts uint8, retryOn []RetryFailure) {
	t.Helper()
	if check.Class == "" {
		t.Fatal("mapped check missing")
	}
	if check.Strength != strength || check.Timeout != timeout || check.Retry.MaximumAttempts != attempts || !slices.Equal(check.Retry.RetryOn, retryOn) {
		t.Fatalf("check policy = %#v, want strength=%q timeout=%s attempts=%d retry_on=%v", check, strength, timeout, attempts, retryOn)
	}
}
