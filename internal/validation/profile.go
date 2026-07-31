// Package validation defines deterministic validation-profile policy before
// command selection, execution, persistence, or review presentation.
package validation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/workspace"
)

const (
	PolicyVersion    = "validation-profile-policy-v1"
	ProfileVersionV1 = "v1"

	MaximumDiscoveredCommands = 64
	MaximumCommandArguments   = 32
)

var (
	ErrInvalidProfile           = errors.New("invalid validation profile")
	ErrInvalidDiscoveredCommand = errors.New("invalid discovered validation command")
	ErrFirstRunApprovalRequired = errors.New("validation command first-run approval required")
	ErrInvalidWaiverAuthority   = errors.New("invalid validation waiver authority")
)

type ProfileName string

const (
	ProfileRoutine   ProfileName = "routine-v1"
	ProfileElevated  ProfileName = "elevated-v1"
	ProfileProtected ProfileName = "protected-v1"
)

func (name ProfileName) IsValid() bool {
	return name == ProfileRoutine || name == ProfileElevated || name == ProfileProtected
}

type RequirementStrength string

const (
	RequirementRequired RequirementStrength = "required"
	RequirementAdvisory RequirementStrength = "advisory"
)

func (strength RequirementStrength) IsValid() bool {
	return strength == RequirementRequired || strength == RequirementAdvisory
}

type RequirementKind string

const (
	RequirementFormatter                    RequirementKind = "formatter"
	RequirementTargetedTests                RequirementKind = "targeted-tests"
	RequirementBroadTests                   RequirementKind = "broad-tests"
	RequirementBuild                        RequirementKind = "build"
	RequirementStaticAnalysis               RequirementKind = "static-analysis"
	RequirementDiffReview                   RequirementKind = "diff-review"
	RequirementRegressionAnalysis           RequirementKind = "regression-analysis"
	RequirementIndependentReview            RequirementKind = "independent-review"
	RequirementAcceptanceSuite              RequirementKind = "acceptance-suite"
	RequirementSecurityReview               RequirementKind = "security-review"
	RequirementDomainReview                 RequirementKind = "domain-review"
	RequirementProofObligations             RequirementKind = "proof-obligations"
	RequirementExternalAssumptionResolution RequirementKind = "external-assumption-resolution"
)

func (kind RequirementKind) IsValid() bool {
	switch kind {
	case RequirementFormatter, RequirementTargetedTests, RequirementBroadTests,
		RequirementBuild, RequirementStaticAnalysis, RequirementDiffReview,
		RequirementRegressionAnalysis, RequirementIndependentReview,
		RequirementAcceptanceSuite, RequirementSecurityReview,
		RequirementDomainReview, RequirementProofObligations,
		RequirementExternalAssumptionResolution:
		return true
	default:
		return false
	}
}

type Requirement struct {
	Kind     RequirementKind     `json:"kind"`
	Strength RequirementStrength `json:"strength"`
}

type CheckClass string

const (
	CheckFormatter      CheckClass = "formatter"
	CheckTargetedTest   CheckClass = "targeted-test"
	CheckBroadTest      CheckClass = "broad-test"
	CheckBuild          CheckClass = "build"
	CheckStaticAnalysis CheckClass = "static-analysis"
)

func (class CheckClass) IsValid() bool {
	switch class {
	case CheckFormatter, CheckTargetedTest, CheckBroadTest, CheckBuild,
		CheckStaticAnalysis:
		return true
	default:
		return false
	}
}

type RetryFailure string

const (
	RetryFailureInfrastructureUnavailable RetryFailure = "infrastructure-unavailable"
)

type RetryPolicy struct {
	MaximumAttempts uint8          `json:"maximum_attempts"`
	RetryOn         []RetryFailure `json:"retry_on"`
}

type Check struct {
	ID                       string              `json:"id"`
	Class                    CheckClass          `json:"class"`
	Strength                 RequirementStrength `json:"strength"`
	Arguments                []string            `json:"arguments"`
	Source                   string              `json:"source"`
	CommandFingerprint       string              `json:"command_fingerprint"`
	RequiresFirstRunApproval bool                `json:"requires_first_run_approval"`
	Timeout                  time.Duration       `json:"timeout"`
	Retry                    RetryPolicy         `json:"retry"`
}

type Profile struct {
	PolicyVersion string           `json:"policy_version"`
	Name          ProfileName      `json:"name"`
	Version       string           `json:"version"`
	RiskFloor     domain.RiskLevel `json:"risk_floor"`
	Requirements  []Requirement    `json:"requirements"`
	Checks        []Check          `json:"checks"`
}

// SelectProfile selects the immutable profile floor for an already-classified
// risk and maps only typed validation commands discovered from the repository.
// Unknown repository recipes remain outside the validation profile until a
// later selection step classifies them explicitly.
func SelectProfile(risk domain.RiskLevel, commands []workspace.SuggestedCommand) (Profile, error) {
	if !risk.IsValid() || len(commands) > MaximumDiscoveredCommands {
		return Profile{}, ErrInvalidProfile
	}
	profile := profileDefinition(risk)
	for _, command := range commands {
		mapped, applicable, err := MapDiscoveredCommand(profile, command)
		if err != nil {
			return Profile{}, err
		}
		if applicable {
			profile.Checks = append(profile.Checks, mapped)
		}
	}
	slices.SortFunc(profile.Checks, compareChecks)
	if err := profile.Validate(); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// MapDiscoveredCommand maps recognized formatter, test, build, and static
// analysis recipes without treating arbitrary repository recipes as trusted.
func MapDiscoveredCommand(profile Profile, command workspace.SuggestedCommand) (Check, bool, error) {
	class, applicable := classifyCommand(command)
	if !applicable {
		return Check{}, false, nil
	}
	if err := validateDiscoveredCommand(command); err != nil {
		return Check{}, false, err
	}
	strength, ok := strengthForCheck(profile.Requirements, class)
	if !ok {
		return Check{}, false, fmt.Errorf("%w: profile lacks command requirement", ErrInvalidProfile)
	}
	fingerprint := discoveredCommandFingerprint(command, class)
	timeout, retry := executionPolicy(class)
	return Check{
		ID: string(class) + "-" + fingerprint[:16], Class: class,
		Strength: strength, Arguments: slices.Clone(command.Arguments),
		Source: command.Source, CommandFingerprint: fingerprint,
		RequiresFirstRunApproval: command.RequiresApproval,
		Timeout:                  timeout, Retry: retry,
	}, true, nil
}

func (profile Profile) Validate() error {
	if profile.PolicyVersion != PolicyVersion || profile.Version != ProfileVersionV1 ||
		!profile.Name.IsValid() || !profile.RiskFloor.IsValid() {
		return ErrInvalidProfile
	}
	expected := profileDefinition(profile.RiskFloor)
	if profile.Name != expected.Name || !slices.Equal(profile.Requirements, expected.Requirements) ||
		len(profile.Checks) > MaximumDiscoveredCommands {
		return fmt.Errorf("%w: validation floor was altered", ErrInvalidProfile)
	}
	seenIDs := make(map[string]struct{}, len(profile.Checks))
	seenFingerprints := make(map[string]struct{}, len(profile.Checks))
	for index, check := range profile.Checks {
		if err := validateCheck(profile.Requirements, check); err != nil {
			return err
		}
		if index > 0 && compareChecks(profile.Checks[index-1], check) >= 0 {
			return fmt.Errorf("%w: checks are not in canonical order", ErrInvalidProfile)
		}
		if _, duplicate := seenIDs[check.ID]; duplicate {
			return fmt.Errorf("%w: duplicate check identity", ErrInvalidProfile)
		}
		seenIDs[check.ID] = struct{}{}
		if _, duplicate := seenFingerprints[check.CommandFingerprint]; duplicate {
			return fmt.Errorf("%w: duplicate discovered command", ErrInvalidProfile)
		}
		seenFingerprints[check.CommandFingerprint] = struct{}{}
	}
	return nil
}

func (profile Profile) CanonicalJSON() ([]byte, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		return nil, fmt.Errorf("%w: encode profile: %v", ErrInvalidProfile, err)
	}
	return encoded, nil
}

func (profile Profile) Digest() (string, error) {
	encoded, err := profile.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func profileDefinition(risk domain.RiskLevel) Profile {
	profile := Profile{PolicyVersion: PolicyVersion, Version: ProfileVersionV1, RiskFloor: risk}
	switch risk {
	case domain.RiskLevelProtected:
		profile.Name = ProfileProtected
		profile.Requirements = []Requirement{
			required(RequirementFormatter), required(RequirementTargetedTests),
			required(RequirementBroadTests), required(RequirementBuild),
			required(RequirementStaticAnalysis), required(RequirementDiffReview),
			required(RequirementRegressionAnalysis), required(RequirementIndependentReview),
			required(RequirementAcceptanceSuite), required(RequirementSecurityReview),
			required(RequirementDomainReview), required(RequirementProofObligations),
			required(RequirementExternalAssumptionResolution),
		}
	case domain.RiskLevelElevated:
		profile.Name = ProfileElevated
		profile.Requirements = []Requirement{
			required(RequirementFormatter), required(RequirementTargetedTests),
			required(RequirementBroadTests), required(RequirementBuild),
			required(RequirementStaticAnalysis), required(RequirementDiffReview),
			required(RequirementRegressionAnalysis), required(RequirementIndependentReview),
		}
	default:
		profile.Name = ProfileRoutine
		profile.Requirements = []Requirement{
			required(RequirementFormatter), required(RequirementTargetedTests),
			advisory(RequirementBroadTests), advisory(RequirementBuild),
			required(RequirementStaticAnalysis), required(RequirementDiffReview),
		}
	}
	return profile
}

func required(kind RequirementKind) Requirement {
	return Requirement{Kind: kind, Strength: RequirementRequired}
}
func advisory(kind RequirementKind) Requirement {
	return Requirement{Kind: kind, Strength: RequirementAdvisory}
}

func classifyCommand(command workspace.SuggestedCommand) (CheckClass, bool) {
	switch command.Kind {
	case "format", "formatter":
		return CheckFormatter, true
	case "test":
		if broadTestArguments(command.Arguments) {
			return CheckBroadTest, true
		}
		return CheckTargetedTest, true
	case "build":
		return CheckBuild, true
	case "lint", "static-analysis":
		return CheckStaticAnalysis, true
	default:
		return "", false
	}
}

func broadTestArguments(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "./..." || argument == "..." || strings.HasSuffix(argument, "/...") {
			return true
		}
	}
	return false
}

func validateDiscoveredCommand(command workspace.SuggestedCommand) error {
	if strings.TrimSpace(command.Kind) != command.Kind || command.Kind == "" || len(command.Kind) > 64 ||
		strings.TrimSpace(command.Source) != command.Source || command.Source == "" || len(command.Source) > 4096 ||
		len(command.Arguments) == 0 || len(command.Arguments) > MaximumCommandArguments {
		return ErrInvalidDiscoveredCommand
	}
	for _, argument := range command.Arguments {
		if strings.TrimSpace(argument) != argument || argument == "" || len(argument) > 4096 || strings.ContainsRune(argument, '\x00') {
			return ErrInvalidDiscoveredCommand
		}
	}
	return nil
}

func discoveredCommandFingerprint(command workspace.SuggestedCommand, class CheckClass) string {
	hash := sha256.New()
	writeFingerprint(hash, string(class))
	writeFingerprint(hash, command.Source)
	writeFingerprint(hash, fmt.Sprintf("%t", command.RequiresApproval))
	for _, argument := range command.Arguments {
		writeFingerprint(hash, argument)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

type byteWriter interface{ Write([]byte) (int, error) }

func writeFingerprint(writer byteWriter, value string) {
	_, _ = writer.Write([]byte(fmt.Sprintf("%d:", len(value))))
	_, _ = writer.Write([]byte(value))
}

func strengthForCheck(requirements []Requirement, class CheckClass) (RequirementStrength, bool) {
	kind := map[CheckClass]RequirementKind{
		CheckFormatter: RequirementFormatter, CheckTargetedTest: RequirementTargetedTests,
		CheckBroadTest: RequirementBroadTests, CheckBuild: RequirementBuild,
		CheckStaticAnalysis: RequirementStaticAnalysis,
	}[class]
	for _, requirement := range requirements {
		if requirement.Kind == kind {
			return requirement.Strength, true
		}
	}
	return "", false
}

func executionPolicy(class CheckClass) (time.Duration, RetryPolicy) {
	switch class {
	case CheckFormatter:
		return 2 * time.Minute, RetryPolicy{MaximumAttempts: 1}
	case CheckTargetedTest:
		return 5 * time.Minute, RetryPolicy{MaximumAttempts: 2, RetryOn: []RetryFailure{RetryFailureInfrastructureUnavailable}}
	case CheckBroadTest:
		return 15 * time.Minute, RetryPolicy{MaximumAttempts: 1}
	case CheckBuild, CheckStaticAnalysis:
		return 10 * time.Minute, RetryPolicy{MaximumAttempts: 1}
	default:
		return 0, RetryPolicy{}
	}
}

func validateCheck(requirements []Requirement, check Check) error {
	strength, exists := strengthForCheck(requirements, check.Class)
	expectedTimeout, expectedRetry := executionPolicy(check.Class)
	if !check.Class.IsValid() || !exists || check.Strength != strength ||
		check.ID != string(check.Class)+"-"+check.CommandFingerprint[:min(16, len(check.CommandFingerprint))] ||
		len(check.CommandFingerprint) != 64 || !lowerHex(check.CommandFingerprint) ||
		check.Timeout != expectedTimeout || !slices.Equal(check.Retry.RetryOn, expectedRetry.RetryOn) ||
		check.Retry.MaximumAttempts != expectedRetry.MaximumAttempts {
		return fmt.Errorf("%w: check policy differs from profile floor", ErrInvalidProfile)
	}
	command := workspace.SuggestedCommand{Kind: commandKindForClass(check.Class), Arguments: check.Arguments, Source: check.Source, RequiresApproval: check.RequiresFirstRunApproval}
	if err := validateDiscoveredCommand(command); err != nil {
		return err
	}
	if discoveredCommandFingerprint(command, check.Class) != check.CommandFingerprint {
		return fmt.Errorf("%w: command fingerprint differs", ErrInvalidProfile)
	}
	return nil
}

func commandKindForClass(class CheckClass) string {
	switch class {
	case CheckFormatter:
		return "format"
	case CheckTargetedTest, CheckBroadTest:
		return "test"
	case CheckBuild:
		return "build"
	case CheckStaticAnalysis:
		return "static-analysis"
	default:
		return ""
	}
}

func compareChecks(left, right Check) int {
	if compared := strings.Compare(string(left.Class), string(right.Class)); compared != 0 {
		return compared
	}
	return strings.Compare(left.CommandFingerprint, right.CommandFingerprint)
}

func lowerHex(value string) bool {
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
