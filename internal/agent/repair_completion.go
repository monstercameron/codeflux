package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/redact"
)

const (
	maximumValidationCommands  = 64
	maximumFailureSummaryBytes = 4 << 10
	maximumLinkedFiles         = 128
	maximumLinkedPlanSteps     = 64
)

var (
	// ErrInvalidValidationProfile classifies a malformed or weakened selected
	// validation profile.
	ErrInvalidValidationProfile = errors.New("invalid selected validation profile")
	// ErrInvalidReviewDecision classifies an unattributed or unsupported user
	// review decision.
	ErrInvalidReviewDecision = errors.New("invalid review decision")
)

// ValidationCommand is one immutable command selected before execution.
type ValidationCommand struct {
	ID                   string
	Required             bool
	AcceptanceTest       bool
	PlanCommand          string
	Request              executor.ToolRequest
	RelevantChangedFiles []string
	PlanStepIDs          []string
}

// RenderValidationPlanCommand derives the only accepted plan/display command
// from the exact executable tool and ordered, non-sensitive arguments.
func RenderValidationPlanCommand(
	request executor.ToolRequest,
) (string, error) {
	if err := executor.ValidateToolRequest(request); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidValidationProfile, err)
	}
	value, err := executor.RenderValidationPlanCommand(request)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidValidationProfile, err)
	}
	if !boundedTrimmed(value, 4096) {
		return "", fmt.Errorf(
			"%w: derived validation plan command exceeds its bound",
			ErrInvalidValidationProfile,
		)
	}
	return value, nil
}

// Validate rejects a caller-authored display command, sensitive executable
// arguments, unsafe linkage, and malformed executable requests.
func (command ValidationCommand) Validate() error {
	if !boundedTrimmed(command.ID, 255) ||
		!boundedTrimmed(command.PlanCommand, 4096) ||
		!canonicalUniquePaths(
			command.RelevantChangedFiles,
			maximumLinkedFiles,
		) ||
		!boundedUniqueStrings(command.PlanStepIDs, maximumLinkedPlanSteps, 255) ||
		len(command.PlanStepIDs) == 0 {
		return ErrInvalidValidationProfile
	}
	if err := executor.ValidateToolRequest(command.Request); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidValidationProfile, err)
	}
	derived, err := RenderValidationPlanCommand(command.Request)
	if err != nil {
		return err
	}
	if command.PlanCommand != derived {
		return fmt.Errorf(
			"%w: validation plan command differs from executable request",
			ErrInvalidValidationProfile,
		)
	}
	return nil
}

// Digest binds the exact command action, gate strength, and scope linkage.
func (command ValidationCommand) Digest() (string, error) {
	if err := command.Validate(); err != nil {
		return "", err
	}
	hash := sha256.New()
	writeHashField(hash, command.ID)
	writeHashField(hash, fmt.Sprintf(
		"%t:%t",
		command.Required,
		command.AcceptanceTest,
	))
	writeHashField(hash, command.PlanCommand)
	writeHashField(hash, executor.ActionSHA256(command.Request))
	for _, path := range command.RelevantChangedFiles {
		writeHashField(hash, path)
	}
	for _, stepID := range command.PlanStepIDs {
		writeHashField(hash, stepID)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ValidationProfile is the fixed ordered validation floor for one plan.
type ValidationProfile struct {
	Name     string
	Version  string
	Commands []ValidationCommand
}

// Validate rejects missing attribution, duplicate commands, unsupported
// subprocess tools, and profiles without a required validation floor.
func (profile ValidationProfile) Validate(
	taskID domain.TaskID,
	runID domain.RunID,
) error {
	if taskID.IsZero() || runID.IsZero() {
		return fmt.Errorf("%w: task and run are required", ErrInvalidValidationProfile)
	}
	if !boundedTrimmed(profile.Name, 255) ||
		!boundedTrimmed(profile.Version, 255) ||
		len(profile.Commands) == 0 ||
		len(profile.Commands) > maximumValidationCommands {
		return fmt.Errorf("%w: profile identity or command count is invalid",
			ErrInvalidValidationProfile)
	}
	seen := make(map[string]struct{}, len(profile.Commands))
	required := 0
	for _, command := range profile.Commands {
		if err := command.Validate(); err != nil {
			return fmt.Errorf("%w: validation command identity is invalid",
				ErrInvalidValidationProfile)
		}
		if _, duplicate := seen[command.ID]; duplicate {
			return fmt.Errorf("%w: duplicate validation command %q",
				ErrInvalidValidationProfile, command.ID)
		}
		seen[command.ID] = struct{}{}
		if command.Required {
			required++
		}
		if command.AcceptanceTest && !command.Required {
			return fmt.Errorf("%w: acceptance test %q must be required",
				ErrInvalidValidationProfile, command.ID)
		}
		if command.Request.TaskID != taskID || command.Request.RunID != runID {
			return fmt.Errorf("%w: command %q belongs to another task or run",
				ErrInvalidValidationProfile, command.ID)
		}
		switch command.Request.Name {
		case executor.ToolRunCommand, executor.ToolTest, executor.ToolBuild,
			executor.ToolStaticAnalysis:
		default:
			return fmt.Errorf("%w: command %q is not a validation tool",
				ErrInvalidValidationProfile, command.ID)
		}
	}
	if required == 0 {
		return fmt.Errorf("%w: at least one required validation is needed",
			ErrInvalidValidationProfile)
	}
	return nil
}

// Digest binds the exact ordered profile and every executable action.
func (profile ValidationProfile) Digest() (string, error) {
	if !boundedTrimmed(profile.Name, 255) ||
		!boundedTrimmed(profile.Version, 255) ||
		len(profile.Commands) == 0 ||
		len(profile.Commands) > maximumValidationCommands {
		return "", ErrInvalidValidationProfile
	}
	hash := sha256.New()
	writeHashField(hash, profile.Name)
	writeHashField(hash, profile.Version)
	for _, command := range profile.Commands {
		digest, err := command.Digest()
		if err != nil {
			return "", err
		}
		writeHashField(hash, digest)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ValidationExecution is the bounded attributable outcome of one selected
// command execution.
type ValidationExecution struct {
	ValidationID       domain.ValidationID    `json:"validation_id"`
	CommandID          string                 `json:"command_id"`
	CommandFingerprint string                 `json:"command_fingerprint"`
	Required           bool                   `json:"required"`
	AcceptanceTest     bool                   `json:"acceptance_test"`
	PlanStepIDs        []string               `json:"plan_step_ids"`
	State              domain.ValidationState `json:"state"`
	CommandExecutionID string                 `json:"command_execution_id,omitempty"`
	Failure            *ValidationFailure     `json:"failure,omitempty"`
}

// ValidationFailure is safe for persistence and links the failure to the
// changed files and plan steps that were in scope.
type ValidationFailure struct {
	SummaryRedacted string   `json:"summary_redacted"`
	ChangedFiles    []string `json:"changed_files"`
	PlanStepIDs     []string `json:"plan_step_ids"`
	OutputTruncated bool     `json:"output_truncated"`
}

// ValidationReport preserves one full validation pass without treating an
// advisory failure as a required-gate success.
type ValidationReport struct {
	ProfileName    string                `json:"profile_name"`
	ProfileVersion string                `json:"profile_version"`
	ProfileDigest  string                `json:"profile_digest"`
	Round          uint32                `json:"round"`
	Executions     []ValidationExecution `json:"executions"`
}

// Validate rejects forged, incomplete, or unbounded validation summaries.
func (report ValidationReport) Validate() error {
	if !boundedTrimmed(report.ProfileName, 255) ||
		!boundedTrimmed(report.ProfileVersion, 255) ||
		!lowerHexSHA256(report.ProfileDigest) ||
		len(report.Executions) == 0 ||
		len(report.Executions) > maximumValidationCommands {
		return errors.New("validation report identity or execution count is invalid")
	}
	seen := make(map[string]struct{}, len(report.Executions))
	required := 0
	for _, execution := range report.Executions {
		if execution.ValidationID.IsZero() ||
			!boundedTrimmed(execution.CommandID, 255) ||
			!lowerHexSHA256(execution.CommandFingerprint) {
			return errors.New("validation execution attribution is invalid")
		}
		if _, duplicate := seen[execution.CommandID]; duplicate {
			return errors.New("validation command was recorded more than once")
		}
		seen[execution.CommandID] = struct{}{}
		if execution.Required {
			required++
		}
		if execution.AcceptanceTest && !execution.Required {
			return errors.New("acceptance validation is not required")
		}
		if !boundedUniqueStrings(
			execution.PlanStepIDs,
			maximumLinkedPlanSteps,
			255,
		) || len(execution.PlanStepIDs) == 0 {
			return errors.New("validation execution plan-step linkage is invalid")
		}
		switch execution.State {
		case domain.ValidationStatePassed:
			if execution.Failure != nil {
				return errors.New("passed validation retains a failure")
			}
		case domain.ValidationStateFailed, domain.ValidationStateCancelled:
			if execution.Failure == nil ||
				!boundedTrimmed(
					execution.Failure.SummaryRedacted,
					maximumFailureSummaryBytes,
				) ||
				!canonicalUniquePaths(
					execution.Failure.ChangedFiles,
					maximumLinkedFiles,
				) ||
				!boundedUniqueStrings(
					execution.Failure.PlanStepIDs,
					maximumLinkedPlanSteps,
					255,
				) ||
				!slices.Equal(
					execution.Failure.PlanStepIDs,
					execution.PlanStepIDs,
				) {
				return errors.New("failed validation summary is invalid")
			}
		default:
			return errors.New("validation execution is not terminal")
		}
	}
	if required == 0 {
		return errors.New("validation report has no required command")
	}
	return nil
}

// RequiredPassed reports whether every required command ran and passed.
func (report ValidationReport) RequiredPassed() bool {
	required := 0
	for _, execution := range report.Executions {
		if !execution.Required {
			continue
		}
		required++
		if execution.State != domain.ValidationStatePassed {
			return false
		}
	}
	return required != 0
}

// AllPassed reports whether every selected command passed.
func (report ValidationReport) AllPassed() bool {
	if len(report.Executions) == 0 {
		return false
	}
	for _, execution := range report.Executions {
		if execution.State != domain.ValidationStatePassed {
			return false
		}
	}
	return true
}

// FailedCommandIDs returns stable command IDs in profile order.
func (report ValidationReport) FailedCommandIDs() []string {
	var result []string
	for _, execution := range report.Executions {
		if execution.State != domain.ValidationStatePassed {
			result = append(result, execution.CommandID)
		}
	}
	return result
}

// ParseValidationFailure redacts and bounds untrusted command output, then
// links only known changed files and predeclared plan steps.
func ParseValidationFailure(
	command ValidationCommand,
	result executor.ToolResult,
	executionErr error,
	changedFiles []string,
	pipeline *redact.Pipeline,
) (ValidationFailure, error) {
	if pipeline == nil {
		return ValidationFailure{}, errors.New("validation redactor is required")
	}
	raw := strings.Join(nonEmptyStrings(
		result.Summary,
		result.StderrRedacted,
		result.StdoutRedacted,
		errorString(executionErr),
	), "\n")
	redacted, err := pipeline.Redact(redact.BoundaryLogPersistence, raw)
	if err != nil {
		return ValidationFailure{}, err
	}
	summary, locallyTruncated := boundUTF8(
		strings.TrimSpace(redacted.Text),
		maximumFailureSummaryBytes,
	)
	if summary == "" {
		summary = "validation failed without a retained diagnostic"
	}
	normalizedChanged := normalizedUniquePaths(changedFiles, maximumLinkedFiles)
	relevant := normalizedUniquePaths(
		command.RelevantChangedFiles,
		maximumLinkedFiles,
	)
	var linked []string
	for _, path := range normalizedChanged {
		if pathWithinValidationScopes(path, relevant) {
			linked = append(linked, path)
		}
	}
	return ValidationFailure{
		SummaryRedacted: summary,
		ChangedFiles:    linked,
		PlanStepIDs:     append([]string(nil), command.PlanStepIDs...),
		OutputTruncated: result.StdoutTruncated ||
			result.StderrTruncated ||
			redacted.Report.InputTruncated ||
			redacted.Report.OutputTruncated ||
			locallyTruncated,
	}, nil
}

func pathWithinValidationScopes(path string, scopes []string) bool {
	for _, scope := range scopes {
		if path == scope || strings.HasPrefix(path, scope+"/") {
			return true
		}
	}
	return false
}

// RepositoryCompletionSummary is the required final Git status and diff
// evidence. Its textual fields must already be redacted.
type RepositoryCompletionSummary struct {
	StatusRedacted string   `json:"status_redacted"`
	DiffRedacted   string   `json:"diff_redacted"`
	DiffSHA256     string   `json:"diff_sha256"`
	ChangedFiles   []string `json:"changed_files"`
}

// ExactCostSummary never serializes an unknown amount as zero.
type ExactCostSummary struct {
	Known       bool                `json:"known"`
	Numerator   int64               `json:"numerator,omitempty"`
	Denominator int64               `json:"denominator,omitempty"`
	Currency    domain.CurrencyCode `json:"currency,omitempty"`
}

// BudgetCompletionSummary keeps forecast, reservation, actual, and remaining
// accounting separately inspectable.
type BudgetCompletionSummary struct {
	Forecast  ExactCostSummary `json:"forecast"`
	Reserved  ExactCostSummary `json:"reserved"`
	Actual    ExactCostSummary `json:"actual"`
	Remaining ExactCostSummary `json:"remaining"`
}

// CompletionSummary is the evidence-backed completion candidate shown before
// any user acceptance decision.
type CompletionSummary struct {
	TaskID                 domain.TaskID               `json:"task_id"`
	RunID                  domain.RunID                `json:"run_id"`
	PlanRevision           uint64                      `json:"plan_revision"`
	Repository             RepositoryCompletionSummary `json:"repository"`
	Validation             ValidationReport            `json:"validation"`
	Budget                 BudgetCompletionSummary     `json:"budget"`
	Assumptions            []string                    `json:"assumptions"`
	Limitations            []string                    `json:"limitations"`
	ImplementationComplete bool                        `json:"implementation_complete"`
	ValidationComplete     bool                        `json:"validation_complete"`
	State                  domain.TaskState            `json:"state"`
}

// Validate enforces honest completion and the awaiting-review boundary.
func (summary CompletionSummary) Validate() error {
	if summary.TaskID.IsZero() || summary.RunID.IsZero() ||
		summary.PlanRevision == 0 {
		return errors.New("completion task, run, and plan revision are required")
	}
	if summary.State != domain.TaskStateAwaitingReview {
		return errors.New("completion candidate must await review")
	}
	if !summary.ImplementationComplete {
		return errors.New("implementation is not complete")
	}
	if err := summary.Validation.Validate(); err != nil {
		return err
	}
	if summary.ValidationComplete != summary.Validation.RequiredPassed() {
		return errors.New("validation completion disagrees with required evidence")
	}
	if !summary.ValidationComplete {
		return errors.New("required validation is incomplete")
	}
	if !lowerHexSHA256(summary.Repository.DiffSHA256) ||
		!boundedText(summary.Repository.StatusRedacted, 64<<10) ||
		!boundedText(summary.Repository.DiffRedacted, 1<<20) ||
		!boundedUniqueStrings(summary.Repository.ChangedFiles, 512, 4096) ||
		!boundedUniqueStrings(summary.Assumptions, 128, 2048) ||
		!boundedUniqueStrings(summary.Limitations, 128, 2048) {
		return errors.New("completion evidence is malformed or unbounded")
	}
	for _, amount := range []ExactCostSummary{
		summary.Budget.Forecast,
		summary.Budget.Reserved,
		summary.Budget.Actual,
		summary.Budget.Remaining,
	} {
		if err := validateExactCostSummary(amount); err != nil {
			return err
		}
	}
	return nil
}

// ReviewDecision is one explicit user action at the review boundary.
type ReviewDecision string

const (
	ReviewDecisionAccept        ReviewDecision = "accept"
	ReviewDecisionRequestRepair ReviewDecision = "request-repair"
	ReviewDecisionRollback      ReviewDecision = "rollback"
	ReviewDecisionAbandon       ReviewDecision = "abandon"
)

// ReviewDecisionRequest binds a user decision to the exact completion
// candidate and its authority evidence.
type ReviewDecisionRequest struct {
	TaskID               domain.TaskID
	RunID                domain.RunID
	PlanRevision         uint64
	CompletionRevision   uint64
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	Decision             ReviewDecision
	Actor                string
	AuthorityReference   string
	ReasonRedacted       string
	IdempotencyKey       string
}

// Validate rejects unattributed decisions and prevents non-review states from
// being treated as review authority.
func (request ReviewDecisionRequest) Validate() error {
	if request.TaskID.IsZero() || request.RunID.IsZero() ||
		request.PlanRevision == 0 || request.CompletionRevision == 0 {
		return fmt.Errorf("%w: task, run, plan, and completion are required",
			ErrInvalidReviewDecision)
	}
	switch request.Decision {
	case ReviewDecisionAccept,
		ReviewDecisionRequestRepair,
		ReviewDecisionRollback,
		ReviewDecisionAbandon:
	default:
		return fmt.Errorf("%w: unsupported decision", ErrInvalidReviewDecision)
	}
	for label, value := range map[string]string{
		"actor":               request.Actor,
		"authority reference": request.AuthorityReference,
		"reason":              request.ReasonRedacted,
		"idempotency key":     request.IdempotencyKey,
	} {
		maximum := 2048
		if label != "reason" {
			maximum = 255
		}
		if !boundedTrimmed(value, maximum) {
			return fmt.Errorf("%w: %s is invalid",
				ErrInvalidReviewDecision, label)
		}
	}
	return nil
}

// ResultingTaskState maps the recorded user decision without auto-acceptance.
func (decision ReviewDecision) ResultingTaskState() (domain.TaskState, error) {
	switch decision {
	case ReviewDecisionAccept:
		return domain.TaskStateCompleted, nil
	case ReviewDecisionRequestRepair:
		return domain.TaskStateRunning, nil
	case ReviewDecisionRollback:
		return domain.TaskStateRolledBack, nil
	case ReviewDecisionAbandon:
		return domain.TaskStateCancelled, nil
	default:
		return "", ErrInvalidReviewDecision
	}
}

func validateExactCostSummary(value ExactCostSummary) error {
	if !value.Known {
		if value.Numerator != 0 || value.Denominator != 0 ||
			value.Currency != "" {
			return errors.New("unknown cost contains a numeric value")
		}
		return nil
	}
	if value.Numerator < 0 || value.Denominator <= 0 ||
		value.Currency == "" {
		return errors.New("known cost is invalid")
	}
	return nil
}

func boundedTrimmed(value string, maximum int) bool {
	return value != "" &&
		strings.TrimSpace(value) == value &&
		len(value) <= maximum &&
		utf8.ValidString(value)
}

func lowerHexSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func boundedText(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value)
}

func boundedUniqueStrings(values []string, count, maximum int) bool {
	if len(values) > count {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !boundedTrimmed(value, maximum) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func canonicalUniquePaths(values []string, count int) bool {
	if len(values) > count {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	previous := ""
	for index, path := range values {
		normalized, ok := canonicalRelativePath(path)
		if !ok || normalized != path ||
			(index > 0 && path <= previous) {
			return false
		}
		if _, duplicate := seen[path]; duplicate {
			return false
		}
		seen[path] = struct{}{}
		previous = path
	}
	return true
}

func normalizedUniquePaths(values []string, maximum int) []string {
	result := make([]string, 0, min(len(values), maximum))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if len(result) == maximum {
			break
		}
		normalized := filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
		if normalized == "." || normalized == "" || filepath.IsAbs(normalized) ||
			normalized == ".." || strings.HasPrefix(normalized, "../") {
			continue
		}
		if _, duplicate := seen[normalized]; duplicate {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	slices.Sort(result)
	return result
}

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func boundUTF8(value string, maximum int) (string, bool) {
	if len(value) <= maximum {
		return value, false
	}
	end := maximum - len("[TRUNCATED]")
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end] + "[TRUNCATED]", true
}

func writeHashField(hash interface{ Write([]byte) (int, error) }, value string) {
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(value))
}
