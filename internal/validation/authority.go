package validation

import (
	"errors"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

type FirstRunAuthority struct {
	CheckID            string
	CommandFingerprint string
	Actor              string
	AuthorityReference string
}

// ValidateFirstRunAuthority requires an exact attributable grant only when
// repository discovery marked the command as approval-gated.
func ValidateFirstRunAuthority(profile Profile, check Check, authority *FirstRunAuthority) error {
	if err := validateProfileCheck(profile, check); err != nil {
		return ErrFirstRunApprovalRequired
	}
	if !check.RequiresFirstRunApproval {
		return nil
	}
	if authority == nil {
		return ErrFirstRunApprovalRequired
	}
	if authority.CheckID != check.ID || authority.CommandFingerprint != check.CommandFingerprint ||
		!boundedAuthorityText(authority.Actor) || !boundedAuthorityText(authority.AuthorityReference) {
		return ErrFirstRunApprovalRequired
	}
	return nil
}

type SkipReason string

const (
	SkipNotApplicable          SkipReason = "not-applicable"
	SkipCommandUnavailable     SkipReason = "command-unavailable"
	SkipEnvironmentUnavailable SkipReason = "environment-unavailable"
	SkipUnsupportedPlatform    SkipReason = "unsupported-platform"
	SkipPrerequisiteFailed     SkipReason = "prerequisite-failed"
	SkipSupersededEquivalent   SkipReason = "superseded-by-equivalent-check"
)

func (reason SkipReason) IsValid() bool {
	switch reason {
	case SkipNotApplicable, SkipCommandUnavailable, SkipEnvironmentUnavailable,
		SkipUnsupportedPlatform, SkipPrerequisiteFailed, SkipSupersededEquivalent:
		return true
	default:
		return false
	}
}

type WaiverAuthority struct {
	ProfileName        ProfileName
	ProfileVersion     string
	CheckID            string
	CommandFingerprint string
	Actor              string
	AuthorityReference string
	Reason             string
}

type CheckOutcome struct {
	ProfileName    ProfileName            `json:"profile_name"`
	ProfileVersion string                 `json:"profile_version"`
	CheckID        string                 `json:"check_id"`
	Strength       RequirementStrength    `json:"strength"`
	State          domain.ValidationState `json:"state"`
	SkipReason     SkipReason             `json:"skip_reason,omitempty"`
	Explanation    string                 `json:"explanation,omitempty"`
	Waiver         *WaiverAuthority       `json:"waiver,omitempty"`
}

func RecordSkippedCheck(profile Profile, check Check, reason SkipReason, explanation string) (CheckOutcome, error) {
	if err := validateProfileCheck(profile, check); err != nil || !reason.IsValid() || !boundedExplanation(explanation) {
		return CheckOutcome{}, ErrInvalidProfile
	}
	return CheckOutcome{ProfileName: profile.Name, ProfileVersion: profile.Version, CheckID: check.ID, Strength: check.Strength, State: domain.ValidationStateSkipped, SkipReason: reason, Explanation: explanation}, nil
}

// WaiveRequiredCheck records explicit user authority as waived. It never
// converts the outcome to passed or creates passing evidence.
func WaiveRequiredCheck(profile Profile, check Check, authority WaiverAuthority) (CheckOutcome, error) {
	if err := validateProfileCheck(profile, check); err != nil || check.Strength != RequirementRequired ||
		authority.ProfileName != profile.Name || authority.ProfileVersion != profile.Version ||
		authority.CheckID != check.ID || authority.CommandFingerprint != check.CommandFingerprint ||
		!boundedAuthorityText(authority.Actor) || !boundedAuthorityText(authority.AuthorityReference) ||
		!boundedExplanation(authority.Reason) {
		return CheckOutcome{}, ErrInvalidWaiverAuthority
	}
	copy := authority
	return CheckOutcome{ProfileName: profile.Name, ProfileVersion: profile.Version, CheckID: check.ID, Strength: check.Strength, State: domain.ValidationStateWaived, Explanation: authority.Reason, Waiver: &copy}, nil
}

func (outcome CheckOutcome) Passed() bool {
	return outcome.State == domain.ValidationStatePassed
}

func validateProfileCheck(profile Profile, check Check) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	if err := validateCheck(profile.Requirements, check); err != nil {
		return err
	}
	for _, candidate := range profile.Checks {
		if candidate.ID == check.ID && candidate.CommandFingerprint == check.CommandFingerprint {
			return nil
		}
	}
	return errors.New("check is not part of validation profile")
}

func boundedAuthorityText(value string) bool {
	return strings.TrimSpace(value) == value && value != "" && len(value) <= 512
}

func boundedExplanation(value string) bool {
	return strings.TrimSpace(value) == value && value != "" && len(value) <= 2048
}
