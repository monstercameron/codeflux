package review

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

const MaximumRiskSignals = 32

var ErrInvalidRiskClassification = errors.New("invalid risk classification")

type RiskValidationError struct {
	Field  string
	Reason string
}

func (failure *RiskValidationError) Error() string {
	return fmt.Sprintf("%s is invalid: %s", failure.Field, failure.Reason)
}

func (failure *RiskValidationError) Unwrap() error { return ErrInvalidRiskClassification }

// RiskPolicyVersion identifies the immutable classification rules used for a
// decision. It is independent from application and validation-profile versions.
type RiskPolicyVersion string

const (
	RiskPolicyVersionV1      RiskPolicyVersion = "risk-classification-v1"
	CurrentRiskPolicyVersion                   = RiskPolicyVersionV1
)

func (version RiskPolicyVersion) IsValid() bool { return version == RiskPolicyVersionV1 }

// RiskSignal is one typed, independently explainable observation about a
// proposed or discovered change. Signal discovery remains outside this pure
// policy; callers must not infer safety from missing discovery data.
type RiskSignal string

const (
	RiskSignalNarrowScopedChange RiskSignal = "narrow-scoped-change"
	RiskSignalTestAdditionOnly   RiskSignal = "test-addition-only"
	RiskSignalDocumentationOnly  RiskSignal = "documentation-only"

	RiskSignalBroadChange      RiskSignal = "broad-change"
	RiskSignalGeneratedCode    RiskSignal = "generated-code"
	RiskSignalDependencyChange RiskSignal = "dependency-change"
	RiskSignalConfiguration    RiskSignal = "configuration-change"

	RiskSignalAuthentication    RiskSignal = "authentication"
	RiskSignalAuthorization     RiskSignal = "authorization"
	RiskSignalPayment           RiskSignal = "payment"
	RiskSignalMigration         RiskSignal = "migration"
	RiskSignalCredential        RiskSignal = "credential"
	RiskSignalConcurrency       RiskSignal = "concurrency"
	RiskSignalExternalEffect    RiskSignal = "external-effect"
	RiskSignalTestRemoval       RiskSignal = "test-removal"
	RiskSignalUnknownOrConflict RiskSignal = "unknown-or-conflicting"
)

func AllRiskSignals() []RiskSignal {
	return []RiskSignal{
		RiskSignalNarrowScopedChange,
		RiskSignalTestAdditionOnly,
		RiskSignalDocumentationOnly,
		RiskSignalBroadChange,
		RiskSignalGeneratedCode,
		RiskSignalDependencyChange,
		RiskSignalConfiguration,
		RiskSignalAuthentication,
		RiskSignalAuthorization,
		RiskSignalPayment,
		RiskSignalMigration,
		RiskSignalCredential,
		RiskSignalConcurrency,
		RiskSignalExternalEffect,
		RiskSignalTestRemoval,
		RiskSignalUnknownOrConflict,
	}
}

func RoutineRiskSignals() []RiskSignal {
	return []RiskSignal{
		RiskSignalNarrowScopedChange,
		RiskSignalTestAdditionOnly,
		RiskSignalDocumentationOnly,
	}
}

func ElevatedRiskSignals() []RiskSignal {
	return []RiskSignal{
		RiskSignalBroadChange,
		RiskSignalGeneratedCode,
		RiskSignalDependencyChange,
		RiskSignalConfiguration,
	}
}

func ProtectedRiskSignals() []RiskSignal {
	return []RiskSignal{
		RiskSignalAuthentication,
		RiskSignalAuthorization,
		RiskSignalPayment,
		RiskSignalMigration,
		RiskSignalCredential,
		RiskSignalConcurrency,
		RiskSignalExternalEffect,
		RiskSignalTestRemoval,
		RiskSignalUnknownOrConflict,
	}
}

func (signal RiskSignal) IsValid() bool {
	return slices.Contains(AllRiskSignals(), signal)
}

func (signal RiskSignal) Floor() domain.RiskLevel {
	switch {
	case slices.Contains(ProtectedRiskSignals(), signal):
		return domain.RiskLevelProtected
	case slices.Contains(ElevatedRiskSignals(), signal):
		return domain.RiskLevelElevated
	case slices.Contains(RoutineRiskSignals(), signal):
		return domain.RiskLevelRoutine
	default:
		return ""
	}
}

type RiskReasonCode string

const (
	RiskReasonSignalFloor          RiskReasonCode = "signal-floor"
	RiskReasonConservativeDefault  RiskReasonCode = "conservative-default"
	RiskReasonUserOverrideRaised   RiskReasonCode = "user-override-raised"
	RiskReasonUserOverrideRetained RiskReasonCode = "user-override-retained-floor"
)

func (code RiskReasonCode) IsValid() bool {
	switch code {
	case RiskReasonSignalFloor, RiskReasonConservativeDefault,
		RiskReasonUserOverrideRaised, RiskReasonUserOverrideRetained:
		return true
	default:
		return false
	}
}

// RiskReason is one deterministic explanation fact suitable for later
// structured persistence. It contains no model-authored free text.
type RiskReason struct {
	Code   RiskReasonCode
	Signal RiskSignal
	Floor  domain.RiskLevel
}

func (reason RiskReason) Validate() error {
	if !reason.Code.IsValid() || !reason.Floor.IsValid() {
		return riskInvalid("reason", "code and floor are required")
	}
	if reason.Code == RiskReasonSignalFloor && !reason.Signal.IsValid() {
		return riskInvalid("reason.signal", "is required for a signal floor")
	}
	if reason.Code != RiskReasonSignalFloor && reason.Signal != "" {
		return riskInvalid("reason.signal", "must be empty for a non-signal reason")
	}
	return nil
}

// RiskClassification is an immutable, policy-versioned decision. The signal
// and reason accessors return clones so later persistence cannot observe caller
// mutation.
type RiskClassification struct {
	policyVersion RiskPolicyVersion
	signals       []RiskSignal
	selected      domain.RiskLevel
	userOverride  domain.RiskLevel
	reasons       []RiskReason
}

func (classification RiskClassification) PolicyVersion() RiskPolicyVersion {
	return classification.policyVersion
}
func (classification RiskClassification) Signals() []RiskSignal {
	return slices.Clone(classification.signals)
}
func (classification RiskClassification) SelectedRisk() domain.RiskLevel {
	return classification.selected
}
func (classification RiskClassification) UserOverride() (domain.RiskLevel, bool) {
	return classification.userOverride, classification.userOverride.IsValid()
}
func (classification RiskClassification) Reasons() []RiskReason {
	return slices.Clone(classification.reasons)
}

func (classification RiskClassification) Validate() error {
	if !classification.policyVersion.IsValid() {
		return riskInvalid("policy_version", "is not supported")
	}
	if !classification.selected.IsValid() {
		return riskInvalid("selected_risk", "is required")
	}
	if classification.userOverride != "" && !classification.userOverride.IsValid() {
		return riskInvalid("user_override", "is not supported")
	}
	if len(classification.signals) > MaximumRiskSignals {
		return riskInvalid("signals", "exceeds the bounded signal limit")
	}
	for index, signal := range classification.signals {
		if !signal.IsValid() {
			return riskInvalid("signals", "contains an unsupported signal")
		}
		if index > 0 && riskSignalOrder(classification.signals[index-1]) >= riskSignalOrder(signal) {
			return riskInvalid("signals", "must be unique and canonical")
		}
	}
	for _, reason := range classification.reasons {
		if err := reason.Validate(); err != nil {
			return err
		}
	}
	recomputed, err := classifyChangeRisk(classification.signals, classification.userOverride)
	if err != nil {
		return err
	}
	if recomputed.selected != classification.selected || !slices.Equal(recomputed.reasons, classification.reasons) {
		return riskInvalid("classification", "does not match its policy inputs")
	}
	return nil
}

// ClassifyChangeRisk selects the strongest immutable floor from structured
// signals and an optional user override. An override may raise risk but cannot
// lower a signal-derived or conservative floor.
func ClassifyChangeRisk(signals []RiskSignal, userOverride domain.RiskLevel) (RiskClassification, error) {
	classification, err := classifyChangeRisk(signals, userOverride)
	if err != nil {
		return RiskClassification{}, err
	}
	if err := classification.Validate(); err != nil {
		return RiskClassification{}, err
	}
	return classification, nil
}

// EscalateChangeRisk incorporates newly discovered signals and an optional
// user-selected floor without discarding any accepted input. Risk therefore
// remains monotonic for the lifetime of a task: new evidence can raise the
// decision, while a later narrower observation cannot demote it.
func EscalateChangeRisk(
	current RiskClassification,
	newSignals []RiskSignal,
	userOverride domain.RiskLevel,
) (RiskClassification, error) {
	if err := current.Validate(); err != nil {
		return RiskClassification{}, err
	}
	if userOverride != "" && !userOverride.IsValid() {
		return RiskClassification{}, riskInvalid("user_override", "is not supported")
	}
	allSignals := append(current.Signals(), newSignals...)
	effectiveOverride, hasOverride := current.UserOverride()
	if userOverride.IsValid() && (!hasOverride || riskOrdinal(userOverride) > riskOrdinal(effectiveOverride)) {
		effectiveOverride = userOverride
	}
	next, err := ClassifyChangeRisk(allSignals, effectiveOverride)
	if err != nil {
		return RiskClassification{}, err
	}
	if riskOrdinal(next.SelectedRisk()) < riskOrdinal(current.SelectedRisk()) {
		return RiskClassification{}, riskInvalid("selected_risk", "automatic risk demotion is prohibited")
	}
	return next, nil
}

func classifyChangeRisk(signals []RiskSignal, userOverride domain.RiskLevel) (RiskClassification, error) {
	if len(signals) > MaximumRiskSignals {
		return RiskClassification{}, riskInvalid("signals", "exceeds the bounded signal limit")
	}
	if userOverride != "" && !userOverride.IsValid() {
		return RiskClassification{}, riskInvalid("user_override", "is not supported")
	}
	seen := make(map[RiskSignal]struct{}, len(signals))
	canonical := make([]RiskSignal, 0, len(signals))
	for _, signal := range signals {
		if !signal.IsValid() {
			return RiskClassification{}, riskInvalid("signals", "contains an unsupported signal")
		}
		if _, duplicate := seen[signal]; duplicate {
			continue
		}
		seen[signal] = struct{}{}
		canonical = append(canonical, signal)
	}
	slices.SortFunc(canonical, func(left, right RiskSignal) int {
		return riskSignalOrder(left) - riskSignalOrder(right)
	})

	selected := domain.RiskLevelProtected
	reasons := make([]RiskReason, 0, len(canonical)+1)
	if len(canonical) == 0 {
		reasons = append(reasons, RiskReason{Code: RiskReasonConservativeDefault, Floor: selected})
	} else {
		selected = domain.RiskLevelRoutine
		for _, signal := range canonical {
			floor := signal.Floor()
			reasons = append(reasons, RiskReason{Code: RiskReasonSignalFloor, Signal: signal, Floor: floor})
			selected = maximumRisk(selected, floor)
		}
	}
	if userOverride.IsValid() {
		if riskOrdinal(userOverride) > riskOrdinal(selected) {
			selected = userOverride
			reasons = append(reasons, RiskReason{Code: RiskReasonUserOverrideRaised, Floor: selected})
		} else {
			reasons = append(reasons, RiskReason{Code: RiskReasonUserOverrideRetained, Floor: selected})
		}
	}
	return RiskClassification{
		policyVersion: CurrentRiskPolicyVersion,
		signals:       canonical, selected: selected, userOverride: userOverride, reasons: reasons,
	}, nil
}

func (classification RiskClassification) Explanation() string {
	parts := make([]string, 0, len(classification.reasons))
	for _, reason := range classification.reasons {
		switch reason.Code {
		case RiskReasonSignalFloor:
			parts = append(parts, string(reason.Signal)+" requires "+string(reason.Floor))
		case RiskReasonConservativeDefault:
			parts = append(parts, "missing structured signals requires protected")
		case RiskReasonUserOverrideRaised:
			parts = append(parts, "user override raises risk to "+string(reason.Floor))
		case RiskReasonUserOverrideRetained:
			parts = append(parts, "user override cannot lower the "+string(reason.Floor)+" floor")
		}
	}
	return strings.Join(parts, "; ")
}

func riskSignalOrder(signal RiskSignal) int {
	return slices.Index(AllRiskSignals(), signal)
}

func maximumRisk(left, right domain.RiskLevel) domain.RiskLevel {
	if riskOrdinal(right) > riskOrdinal(left) {
		return right
	}
	return left
}

func riskOrdinal(risk domain.RiskLevel) int {
	switch risk {
	case domain.RiskLevelRoutine:
		return 1
	case domain.RiskLevelElevated:
		return 2
	case domain.RiskLevelProtected:
		return 3
	default:
		return 0
	}
}

func riskInvalid(field, reason string) error {
	return &RiskValidationError{Field: field, Reason: reason}
}
