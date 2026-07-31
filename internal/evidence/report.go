// Package evidence defines immutable, claim-level final evidence reports.
// Reports are structured domain data; Markdown is never authoritative.
package evidence

import (
	"errors"
	"fmt"
	"math"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	SchemaVersion             uint32 = 1
	MaximumChangedFiles              = 4096
	MaximumValidations               = 1024
	MaximumApprovals                 = 256
	MaximumVersions                  = 128
	MaximumClaims                    = 1024
	MaximumLinksPerClaim             = 128
	MaximumNarratives                = 256
	MaximumTextBytes                 = 8192
	MaximumApprovalScopeBytes        = 1024
)

var ErrInvalidReport = errors.New("invalid final evidence report")

type ChangedFileStatus string

const (
	FileAdded    ChangedFileStatus = "added"
	FileModified ChangedFileStatus = "modified"
	FileDeleted  ChangedFileStatus = "deleted"
	FileRenamed  ChangedFileStatus = "renamed"
)

func (status ChangedFileStatus) valid() bool {
	return status == FileAdded || status == FileModified || status == FileDeleted || status == FileRenamed
}

type ValidationStatus string

const (
	ValidationPassed      ValidationStatus = "passed"
	ValidationFailed      ValidationStatus = "failed"
	ValidationWaived      ValidationStatus = "waived"
	ValidationSkipped     ValidationStatus = "skipped"
	ValidationUnavailable ValidationStatus = "unavailable"
	ValidationCancelled   ValidationStatus = "cancelled"
	ValidationInvalidated ValidationStatus = "invalidated"
)

func (status ValidationStatus) valid() bool {
	switch status {
	case ValidationPassed, ValidationFailed, ValidationWaived, ValidationSkipped,
		ValidationUnavailable, ValidationCancelled, ValidationInvalidated:
		return true
	default:
		return false
	}
}

type VersionKind string

const (
	VersionModel    VersionKind = "model"
	VersionProvider VersionKind = "provider"
	VersionTool     VersionKind = "tool"
	VersionPolicy   VersionKind = "policy"
)

func (kind VersionKind) valid() bool {
	return kind == VersionModel || kind == VersionProvider || kind == VersionTool || kind == VersionPolicy
}

type ClaimBoundary string

const (
	BoundaryInternal ClaimBoundary = "internal"
	BoundaryExternal ClaimBoundary = "external-system"
)

type ChangedFile struct {
	Path       string
	PriorPath  string
	Status     ChangedFileStatus
	Insertions uint64
	Deletions  uint64
	Generated  bool
}

type ValidationCheck struct {
	CheckID         string
	ValidationRunID string
	Required        bool
	Status          ValidationStatus
	Summary         string
	StatusReason    string
	CommandDigest   string
	DiffIdentity    string
}

type ApprovalUse struct {
	ApprovalID    domain.ApprovalID
	State         domain.ApprovalRequestState
	Scope         string
	AuthorityUsed string
}

type VersionBinding struct {
	Kind          VersionKind
	Name          string
	Known         bool
	Version       string
	UnknownReason string
}

type ForecastActual struct {
	ForecastDurationKnown         bool
	ForecastP50                   time.Duration
	ForecastP90                   time.Duration
	ForecastDurationUnknownReason string
	ForecastTokensKnown           bool
	ForecastTokensP50             uint64
	ForecastTokensP90             uint64
	ForecastTokensUnknownReason   string
	ForecastCostKnown             bool
	ForecastCostP50               domain.Money
	ForecastCostP90               domain.Money
	ForecastCostUnknownReason     string
	ActualDurationKnown           bool
	ActualDuration                time.Duration
	ActualDurationUnknownReason   string
	ActualTokens                  domain.TokenUsage
	ActualTokensUnknownReason     string
	ActualCostKnown               bool
	ActualCost                    domain.Money
	ActualCostUnknownReason       string
}

type Claim struct {
	ID               string
	Statement        string
	Scope            string
	Boundary         ClaimBoundary
	Guarantee        domain.AssuranceLevel
	GuaranteeReason  string
	EvidenceIDs      []domain.EvidenceID
	ValidationRunIDs []string
	GraphNodeIDs     []domain.NodeID
	Assumptions      []string
	Limitations      []string
}

type Report struct {
	ID                         string
	TaskID                     domain.TaskID
	RequirementRevision        uint64
	AcceptedPlanRevision       uint64
	PlanApprovalID             domain.ApprovalID
	BaseRevision               string
	DiffIdentity               string
	RiskClassificationRevision uint64
	Risk                       domain.RiskLevel
	RiskExplanation            string
	GraphRevisionID            domain.GraphRevisionID
	Metrics                    ForecastActual
	ChangedFiles               []ChangedFile
	Validations                []ValidationCheck
	Approvals                  []ApprovalUse
	Versions                   []VersionBinding
	Assumptions                []string
	Limitations                []string
	Claims                     []Claim
	IdempotencyKey             string
	CreatedAt                  time.Time
}

func (report Report) Validate() error {
	switch {
	case !validDigest(report.ID):
		return invalid("id", "must be a lowercase SHA-256 identity")
	case report.TaskID.IsZero():
		return invalid("task_id", "must not be empty")
	case report.RequirementRevision == 0:
		return invalid("requirement_revision", "must be greater than zero")
	case report.AcceptedPlanRevision == 0:
		return invalid("accepted_plan_revision", "must be greater than zero")
	case report.PlanApprovalID.IsZero():
		return invalid("plan_approval_id", "must not be empty")
	case !validGitRevision(report.BaseRevision):
		return invalid("base_revision", "must be a lowercase Git object identity")
	case !validDigest(report.DiffIdentity):
		return invalid("diff_identity", "must be a lowercase SHA-256 identity")
	case report.RiskClassificationRevision == 0 || !report.Risk.IsValid():
		return invalid("risk", "must bind a valid classification revision")
	case report.GraphRevisionID.IsZero():
		return invalid("graph_revision_id", "must not be empty")
	case !validUTC(report.CreatedAt):
		return invalid("created_at", "must be a non-zero UTC timestamp")
	}
	if err := boundedText("risk_explanation", report.RiskExplanation); err != nil {
		return err
	}
	if err := boundedText("idempotency_key", report.IdempotencyKey); err != nil || len(report.IdempotencyKey) > 255 {
		return invalid("idempotency_key", "must be non-empty and at most 255 bytes")
	}
	if err := validateMetrics(report.Metrics); err != nil {
		return err
	}
	if err := validateChangedFiles(report.ChangedFiles); err != nil {
		return err
	}
	if err := validateValidations(report.Validations, report.DiffIdentity); err != nil {
		return err
	}
	if err := validateApprovals(report.Approvals, report.PlanApprovalID); err != nil {
		return err
	}
	if err := validateVersions(report.Versions); err != nil {
		return err
	}
	if err := validateNarratives("assumptions", report.Assumptions); err != nil {
		return err
	}
	if err := validateNarratives("limitations", report.Limitations); err != nil {
		return err
	}
	return validateClaims(report.Claims, report.Validations)
}

func (report Report) Clone() Report {
	result := report
	result.ChangedFiles = slices.Clone(report.ChangedFiles)
	result.Validations = slices.Clone(report.Validations)
	result.Approvals = slices.Clone(report.Approvals)
	result.Versions = slices.Clone(report.Versions)
	result.Assumptions = slices.Clone(report.Assumptions)
	result.Limitations = slices.Clone(report.Limitations)
	result.Claims = make([]Claim, len(report.Claims))
	for i, claim := range report.Claims {
		result.Claims[i] = claim
		result.Claims[i].EvidenceIDs = slices.Clone(claim.EvidenceIDs)
		result.Claims[i].ValidationRunIDs = slices.Clone(claim.ValidationRunIDs)
		result.Claims[i].GraphNodeIDs = slices.Clone(claim.GraphNodeIDs)
		result.Claims[i].Assumptions = slices.Clone(claim.Assumptions)
		result.Claims[i].Limitations = slices.Clone(claim.Limitations)
	}
	result.Metrics.ActualTokens.ProviderSpecific = cloneTokenCategories(report.Metrics.ActualTokens.ProviderSpecific)
	return result
}

func validateChangedFiles(files []ChangedFile) error {
	if len(files) > MaximumChangedFiles {
		return invalid("changed_files", "exceeds the bounded list limit")
	}
	seen := map[string]struct{}{}
	for _, file := range files {
		if !validRelativePath(file.Path) || !file.Status.valid() {
			return invalid("changed_files", "contains an invalid path or status")
		}
		if file.Status == FileRenamed {
			if !validRelativePath(file.PriorPath) || file.PriorPath == file.Path {
				return invalid("changed_files.prior_path", "must identify a distinct valid prior path for a rename")
			}
		} else if file.PriorPath != "" {
			return invalid("changed_files.prior_path", "is only allowed for renamed files")
		}
		if _, duplicate := seen[file.Path]; duplicate {
			return invalid("changed_files", "contains a duplicate path")
		}
		if file.Insertions > math.MaxInt64 || file.Deletions > math.MaxInt64 {
			return invalid("changed_files", "contains a line count that cannot be stored exactly")
		}
		seen[file.Path] = struct{}{}
	}
	return nil
}

func validateValidations(checks []ValidationCheck, diffIdentity string) error {
	if len(checks) == 0 || len(checks) > MaximumValidations {
		return invalid("validations", "must contain a bounded complete check set")
	}
	seen := map[string]struct{}{}
	seenRuns := map[string]struct{}{}
	for _, check := range checks {
		if err := boundedText("validation.check_id", check.CheckID); err != nil || !check.Status.valid() {
			return invalid("validations", "contains an invalid check identity or status")
		}
		if check.ValidationRunID != "" {
			if _, err := domain.ParseValidationID(check.ValidationRunID); err != nil {
				return invalid("validation.run_id", "must be a canonical validation execution identity")
			}
			if _, duplicate := seenRuns[check.ValidationRunID]; duplicate {
				return invalid("validations", "contains a duplicate validation run identity")
			}
			seenRuns[check.ValidationRunID] = struct{}{}
			if !validDigest(check.CommandDigest) {
				return invalid("validation.command_digest", "is required when a validation run is cited")
			}
		}
		if err := boundedText("validation.summary", check.Summary); err != nil {
			return err
		}
		if check.Status != ValidationPassed && strings.TrimSpace(check.StatusReason) == "" {
			return invalid("validation.status_reason", "is required for every non-passed check")
		}
		if check.StatusReason != "" {
			if err := boundedText("validation.status_reason", check.StatusReason); err != nil {
				return err
			}
		}
		if check.CommandDigest != "" && !validDigest(check.CommandDigest) {
			return invalid("validation.command_digest", "must be empty or a lowercase SHA-256 identity")
		}
		if check.DiffIdentity != diffIdentity {
			return invalid("validation.diff_identity", "must match the final report diff")
		}
		if _, duplicate := seen[check.CheckID]; duplicate {
			return invalid("validations", "contains a duplicate check identity")
		}
		seen[check.CheckID] = struct{}{}
	}
	return nil
}

func validateApprovals(approvals []ApprovalUse, planApproval domain.ApprovalID) error {
	if len(approvals) == 0 || len(approvals) > MaximumApprovals {
		return invalid("approvals", "must contain a bounded authority history")
	}
	seen, planGranted := map[domain.ApprovalID]struct{}{}, false
	for _, approval := range approvals {
		if approval.ApprovalID.IsZero() || !approval.State.IsValid() {
			return invalid("approvals", "contains an invalid identity or state")
		}
		if err := boundedText("approval.scope", approval.Scope); err != nil {
			return err
		}
		if len(approval.Scope) > MaximumApprovalScopeBytes {
			return invalid("approval.scope", "exceeds the durable approval scope limit")
		}
		if approval.AuthorityUsed != "" {
			if approval.State != domain.ApprovalRequestStateGranted {
				return invalid("approval.authority_used", "may only be recorded for a granted approval")
			}
			if err := boundedText("approval.authority_used", approval.AuthorityUsed); err != nil {
				return err
			}
		}
		if _, duplicate := seen[approval.ApprovalID]; duplicate {
			return invalid("approvals", "contains a duplicate approval identity")
		}
		seen[approval.ApprovalID] = struct{}{}
		planGranted = planGranted || approval.ApprovalID == planApproval && approval.State == domain.ApprovalRequestStateGranted
	}
	if !planGranted {
		return invalid("plan_approval_id", "must identify a granted approval in the report")
	}
	return nil
}

func validateVersions(versions []VersionBinding) error {
	if len(versions) > MaximumVersions {
		return invalid("versions", "exceeds the bounded list limit")
	}
	seenKinds := map[VersionKind]bool{}
	seen := map[string]struct{}{}
	for _, version := range versions {
		if !version.Kind.valid() {
			return invalid("versions.kind", "is unsupported")
		}
		if err := boundedText("version.name", version.Name); err != nil {
			return err
		}
		if version.Known {
			if err := boundedText("version.version", version.Version); err != nil || version.UnknownReason != "" {
				return invalid("versions", "known versions require a value and no unknown reason")
			}
		} else {
			if version.Version != "" || strings.TrimSpace(version.UnknownReason) == "" {
				return invalid("versions", "unknown versions require a reason and no value")
			}
			if err := boundedText("version.unknown_reason", version.UnknownReason); err != nil {
				return err
			}
		}
		key := string(version.Kind) + "\x00" + version.Name
		if _, duplicate := seen[key]; duplicate {
			return invalid("versions", "contains a duplicate kind and name")
		}
		seen[key], seenKinds[version.Kind] = struct{}{}, true
	}
	for _, required := range []VersionKind{VersionModel, VersionProvider, VersionTool, VersionPolicy} {
		if !seenKinds[required] {
			return invalid("versions", "must explicitly include model, provider, tool, and policy versions")
		}
	}
	return nil
}

func validateClaims(claims []Claim, validations []ValidationCheck) error {
	if len(claims) == 0 || len(claims) > MaximumClaims {
		return invalid("claims", "must contain a bounded claim-level guarantee set")
	}
	seen := map[string]struct{}{}
	validationRuns := make(map[string]ValidationStatus, len(validations))
	for _, validation := range validations {
		if validation.ValidationRunID != "" {
			validationRuns[validation.ValidationRunID] = validation.Status
		}
	}
	for _, claim := range claims {
		if err := boundedText("claim.id", claim.ID); err != nil || len(claim.ID) > 255 {
			return invalid("claim.id", "must be non-empty and at most 255 bytes")
		}
		if err := boundedText("claim.statement", claim.Statement); err != nil {
			return err
		}
		if err := boundedText("claim.scope", claim.Scope); err != nil {
			return err
		}
		if !claim.Guarantee.IsValid() || (claim.Boundary != BoundaryInternal && claim.Boundary != BoundaryExternal) {
			return invalid("claim.guarantee", "has an invalid boundary or guarantee")
		}
		if claim.Boundary == BoundaryExternal && claim.Guarantee != domain.AssuranceLevelContractChecked &&
			claim.Guarantee != domain.AssuranceLevelRuntimeOnly && claim.Guarantee != domain.AssuranceLevelInvalidated {
			return invalid("claim.guarantee", "external-system behavior may only be contract-checked, runtime-only, or invalidated")
		}
		if claim.Boundary == BoundaryExternal && len(claim.Limitations) == 0 {
			return invalid("claim.limitations", "must state the external-system boundary")
		}
		if err := boundedText("claim.guarantee_reason", claim.GuaranteeReason); err != nil {
			return err
		}
		if len(claim.EvidenceIDs) > MaximumLinksPerClaim || len(claim.ValidationRunIDs) > MaximumLinksPerClaim || len(claim.GraphNodeIDs) > MaximumLinksPerClaim {
			return invalid("claim.links", "exceeds the per-claim link limit")
		}
		if len(claim.EvidenceIDs)+len(claim.ValidationRunIDs)+len(claim.GraphNodeIDs) == 0 {
			return invalid("claim.links", "must identify claim-level provenance")
		}
		if (claim.Guarantee == domain.AssuranceLevelFullyEvaluated || claim.Guarantee == domain.AssuranceLevelModelVerified) &&
			(len(claim.EvidenceIDs) == 0 || len(claim.ValidationRunIDs) == 0) {
			return invalid("claim.guarantee", "strong guarantees require both evidence and validation provenance")
		}
		if claim.Guarantee == domain.AssuranceLevelContractChecked && len(claim.ValidationRunIDs) == 0 {
			return invalid("claim.guarantee", "contract-checked guarantees require validation provenance")
		}
		if claim.Guarantee == domain.AssuranceLevelInvalidated && len(claim.Limitations) == 0 {
			return invalid("claim.limitations", "invalidated claims require an explicit limitation")
		}
		if duplicateTyped(claim.EvidenceIDs) || duplicateTyped(claim.GraphNodeIDs) || duplicateStrings(claim.ValidationRunIDs) {
			return invalid("claim.links", "contains a duplicate identity")
		}
		for _, id := range claim.EvidenceIDs {
			if id.IsZero() {
				return invalid("claim.evidence_ids", "contains an empty identity")
			}
		}
		for _, id := range claim.GraphNodeIDs {
			if id.IsZero() {
				return invalid("claim.graph_node_ids", "contains an empty identity")
			}
		}
		for _, id := range claim.ValidationRunIDs {
			if _, err := domain.ParseValidationID(id); err != nil {
				return invalid("claim.validation_run_id", "must be a canonical validation execution identity")
			}
			status, ok := validationRuns[id]
			if !ok {
				return invalid("claim.validation_run_ids", "must refer to a validation in this report")
			}
			if (claim.Guarantee == domain.AssuranceLevelFullyEvaluated || claim.Guarantee == domain.AssuranceLevelModelVerified || claim.Guarantee == domain.AssuranceLevelContractChecked) && status != ValidationPassed {
				return invalid("claim.guarantee", "evaluated guarantees may only cite passed validation runs")
			}
		}
		if err := validateNarratives("claim.assumptions", claim.Assumptions); err != nil {
			return err
		}
		if err := validateNarratives("claim.limitations", claim.Limitations); err != nil {
			return err
		}
		if _, duplicate := seen[claim.ID]; duplicate {
			return invalid("claims", "contains a duplicate claim identity")
		}
		seen[claim.ID] = struct{}{}
	}
	return nil
}

func validateMetrics(metrics ForecastActual) error {
	if metrics.ForecastDurationKnown {
		if metrics.ForecastP50 < 0 || metrics.ForecastP90 < metrics.ForecastP50 {
			return invalid("metrics.forecast_duration", "must be an ordered non-negative range")
		}
	} else if metrics.ForecastP50 != 0 || metrics.ForecastP90 != 0 {
		return invalid("metrics.forecast_duration", "unknown duration must not carry values")
	}
	if err := validateKnownReason("metrics.forecast_duration", metrics.ForecastDurationKnown, metrics.ForecastDurationUnknownReason); err != nil {
		return err
	}
	if metrics.ForecastTokensKnown {
		if metrics.ForecastTokensP90 < metrics.ForecastTokensP50 || metrics.ForecastTokensP90 > math.MaxInt64 {
			return invalid("metrics.forecast_tokens", "must be an ordered range")
		}
	} else if metrics.ForecastTokensP50 != 0 || metrics.ForecastTokensP90 != 0 {
		return invalid("metrics.forecast_tokens", "unknown tokens must not carry values")
	}
	if err := validateKnownReason("metrics.forecast_tokens", metrics.ForecastTokensKnown, metrics.ForecastTokensUnknownReason); err != nil {
		return err
	}
	if metrics.ForecastCostKnown {
		if err := metrics.ForecastCostP50.Validate(); err != nil {
			return err
		}
		if err := metrics.ForecastCostP90.Validate(); err != nil || metrics.ForecastCostP50.Currency != metrics.ForecastCostP90.Currency || metrics.ForecastCostP50.MinorUnits < 0 || metrics.ForecastCostP90.MinorUnits < metrics.ForecastCostP50.MinorUnits {
			return invalid("metrics.forecast_cost", "must be an ordered same-currency range")
		}
	} else if metrics.ForecastCostP50 != (domain.Money{}) || metrics.ForecastCostP90 != (domain.Money{}) {
		return invalid("metrics.forecast_cost", "unknown cost must not carry values")
	}
	if err := validateKnownReason("metrics.forecast_cost", metrics.ForecastCostKnown, metrics.ForecastCostUnknownReason); err != nil {
		return err
	}
	if metrics.ActualDurationKnown {
		if metrics.ActualDuration < 0 {
			return invalid("metrics.actual_duration", "must be non-negative")
		}
	} else if metrics.ActualDuration != 0 {
		return invalid("metrics.actual_duration", "unknown duration must not carry a value")
	}
	if err := validateKnownReason("metrics.actual_duration", metrics.ActualDurationKnown, metrics.ActualDurationUnknownReason); err != nil {
		return err
	}
	if err := metrics.ActualTokens.Validate(); err != nil {
		return err
	}
	if metrics.ActualTokens.Known {
		counts := []domain.TokenCount{metrics.ActualTokens.Input, metrics.ActualTokens.CachedInput, metrics.ActualTokens.CacheWrite, metrics.ActualTokens.Output, metrics.ActualTokens.Reasoning}
		for _, count := range counts {
			if uint64(count) > math.MaxInt64 {
				return invalid("metrics.actual_tokens", "contains a count that cannot be stored exactly")
			}
		}
		for category, count := range metrics.ActualTokens.ProviderSpecific {
			if len(category) > 64 || category != strings.TrimSpace(category) || !utf8.ValidString(category) || uint64(count) > math.MaxInt64 {
				return invalid("metrics.actual_tokens.provider_specific", "contains an invalid category or unstorable count")
			}
			for _, character := range category {
				if unicode.IsControl(character) {
					return invalid("metrics.actual_tokens.provider_specific", "contains a control character")
				}
			}
		}
	}
	if err := validateKnownReason("metrics.actual_tokens", metrics.ActualTokens.Known, metrics.ActualTokensUnknownReason); err != nil {
		return err
	}
	if metrics.ActualCostKnown {
		if err := metrics.ActualCost.Validate(); err != nil {
			return err
		}
		if metrics.ActualCost.MinorUnits < 0 {
			return invalid("metrics.actual_cost", "must be non-negative")
		}
	} else if metrics.ActualCost != (domain.Money{}) {
		return invalid("metrics.actual_cost", "unknown cost must not carry a value")
	}
	return validateKnownReason("metrics.actual_cost", metrics.ActualCostKnown, metrics.ActualCostUnknownReason)
}

func validateKnownReason(field string, known bool, reason string) error {
	if known {
		if reason != "" {
			return invalid(field+"_unknown_reason", "must be empty when the value is known")
		}
		return nil
	}
	return boundedText(field+"_unknown_reason", reason)
}

func validateNarratives(field string, values []string) error {
	if len(values) > MaximumNarratives || duplicateStrings(values) {
		return invalid(field, "exceeds the limit or contains duplicates")
	}
	for _, value := range values {
		if err := boundedText(field, value); err != nil {
			return err
		}
	}
	return nil
}

func boundedText(field, value string) error {
	if value == "" || value != strings.TrimSpace(value) || !utf8.ValidString(value) || len(value) > MaximumTextBytes {
		return invalid(field, "must be non-empty, trimmed, valid UTF-8, and bounded")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return invalid(field, "must not contain control characters")
		}
	}
	return nil
}

func validRelativePath(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 4096 && utf8.ValidString(value) &&
		!path.IsAbs(value) && !strings.Contains(value, "\\") && !strings.Contains(value, ":") &&
		path.Clean(value) == value && value != "." && value != ".." && !strings.HasPrefix(value, "../")
}

func validDigest(value string) bool      { return validLowerHex(value, 64) }
func validGitRevision(value string) bool { return validLowerHex(value, 40) || validLowerHex(value, 64) }
func validLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validUTC(value time.Time) bool { return !value.IsZero() && value.Location() == time.UTC }

func duplicateStrings(values []string) bool {
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func duplicateTyped[T comparable](values []T) bool {
	seen := map[T]struct{}{}
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func cloneTokenCategories(values map[string]domain.TokenCount) map[string]domain.TokenCount {
	if values == nil {
		return nil
	}
	result := make(map[string]domain.TokenCount, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func invalid(field, reason string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidReport, field, reason)
}
