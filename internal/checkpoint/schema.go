// Package checkpoint defines versioned, secret-free recovery snapshots and
// coordinates their capture at durable execution boundaries.
package checkpoint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

// SchemaVersion is the first immutable checkpoint-state encoding.
const SchemaVersion uint64 = 1

const (
	maximumDirtyFiles       = 2048
	maximumPlanSteps        = 256
	maximumAmbiguousActions = 128
)

// PlanStepState is the checkpoint projection of one immutable plan step.
type PlanStepState string

const (
	PlanStepPending     PlanStepState = "pending"
	PlanStepInProgress  PlanStepState = "in-progress"
	PlanStepImplemented PlanStepState = "implemented"
	PlanStepValidated   PlanStepState = "validated"
	PlanStepFailed      PlanStepState = "failed"
	PlanStepSkipped     PlanStepState = "skipped"
)

// DirtyFileHash records only recovery-safe path, existence, and content
// identity. File content is never part of a checkpoint snapshot.
type DirtyFileHash struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	SHA256 string `json:"sha256,omitempty"`
}

// PlanStepSnapshot records the exact current state of one plan step.
type PlanStepSnapshot struct {
	ID    string        `json:"id"`
	State PlanStepState `json:"state"`
}

// ExactCost is an exact rational count of currency minor units.
type ExactCost struct {
	Currency    domain.CurrencyCode `json:"currency"`
	Numerator   int64               `json:"numerator"`
	Denominator int64               `json:"denominator"`
}

// BudgetPosition is the immutable ledger projection observed at capture.
type BudgetPosition struct {
	BudgetID               domain.BudgetID   `json:"budget_id"`
	SnapshotRevision       uint64            `json:"snapshot_revision"`
	LimitRevision          uint64            `json:"limit_revision"`
	ReservedCost           ExactCost         `json:"reserved_cost"`
	ChargedCost            ExactCost         `json:"charged_cost"`
	ActualKnownCost        ExactCost         `json:"actual_known_cost"`
	CostAccountingUnknown  bool              `json:"cost_accounting_unknown"`
	ReservedTokens         domain.TokenCount `json:"reserved_tokens"`
	ChargedTokens          domain.TokenCount `json:"charged_tokens"`
	ActualTokens           domain.TokenCount `json:"actual_tokens"`
	TokenAccountingUnknown bool              `json:"token_accounting_unknown"`
	ProviderCallSlots      uint64            `json:"provider_call_slots"`
	ReconciliationPending  bool              `json:"reconciliation_pending"`
}

// PolicyBinding identifies the exact effective non-secret execution policy.
type PolicyBinding struct {
	Revision      uint64 `json:"revision"`
	Version       string `json:"version"`
	ContentSHA256 string `json:"content_sha256"`
}

// ToolBinding identifies the exact approved tool schema and catalog.
type ToolBinding struct {
	SchemaVersion int    `json:"schema_version"`
	CatalogSHA256 string `json:"catalog_sha256"`
}

// ProviderBinding identifies the exact selected provider/model implementation
// and immutable non-secret effective run configuration.
type ProviderBinding struct {
	SettingsRevision       uint64 `json:"settings_revision"`
	RunConfigurationSHA256 string `json:"run_configuration_sha256"`
	Adapter                string `json:"adapter"`
	AdapterVersion         string `json:"adapter_version"`
	Provider               string `json:"provider"`
	ProviderVersion        string `json:"provider_version"`
	Model                  string `json:"model"`
	ModelRevision          string `json:"model_revision"`
}

// AmbiguousExternalAction records a persisted intent whose external outcome
// cannot yet be classified as completed or safely retryable.
type AmbiguousExternalAction struct {
	ActionID      string `json:"action_id"`
	Kind          string `json:"kind"`
	IntentSHA256  string `json:"intent_sha256"`
	ToolRequestID string `json:"tool_request_id,omitempty"`
}

// Snapshot is the complete M15 recovery state. Its closed scalar/list schema
// has no credential bytes, environment, process, stream, or live-handle field.
type Snapshot struct {
	SchemaVersion            uint64                    `json:"schema_version"`
	TaskID                   domain.TaskID             `json:"task_id"`
	RunID                    domain.RunID              `json:"run_id"`
	RepositoryID             domain.RepositoryID       `json:"repository_id"`
	WorktreeBindingRevision  uint64                    `json:"worktree_binding_revision"`
	PlanRevision             uint64                    `json:"plan_revision"`
	BaseRevision             string                    `json:"base_revision"`
	WorktreeHead             string                    `json:"worktree_head"`
	PreservedRevision        string                    `json:"preserved_revision"`
	DirtyFiles               []DirtyFileHash           `json:"dirty_files"`
	DiffSHA256               string                    `json:"diff_sha256"`
	CompletedPlanSteps       []PlanStepSnapshot        `json:"completed_plan_steps"`
	PendingPlanSteps         []PlanStepSnapshot        `json:"pending_plan_steps"`
	Budget                   BudgetPosition            `json:"budget"`
	Policy                   PolicyBinding             `json:"policy"`
	Provider                 ProviderBinding           `json:"provider"`
	Tools                    ToolBinding               `json:"tools"`
	LastDurableEventSequence uint64                    `json:"last_durable_event_sequence"`
	ExternalOutcomeAmbiguous bool                      `json:"external_outcome_ambiguous"`
	AmbiguousExternalActions []AmbiguousExternalAction `json:"ambiguous_external_actions"`
}

// CanonicalState contains the normalized snapshot encoding and its identity.
type CanonicalState struct {
	Snapshot    Snapshot
	JSON        string
	StateSHA256 string
}

// Canonicalize validates, sorts, and encodes a snapshot deterministically.
func Canonicalize(value Snapshot) (CanonicalState, error) {
	normalized := cloneSnapshot(value)
	slices.SortFunc(normalized.DirtyFiles, func(left, right DirtyFileHash) int {
		return strings.Compare(left.Path, right.Path)
	})
	slices.SortFunc(
		normalized.CompletedPlanSteps,
		func(left, right PlanStepSnapshot) int {
			return strings.Compare(left.ID, right.ID)
		},
	)
	slices.SortFunc(
		normalized.PendingPlanSteps,
		func(left, right PlanStepSnapshot) int {
			return strings.Compare(left.ID, right.ID)
		},
	)
	slices.SortFunc(
		normalized.AmbiguousExternalActions,
		func(left, right AmbiguousExternalAction) int {
			return strings.Compare(left.ActionID, right.ActionID)
		},
	)
	if err := validateSnapshot(normalized); err != nil {
		return CanonicalState{}, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return CanonicalState{}, fmt.Errorf("encode checkpoint state: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return CanonicalState{
		Snapshot:    normalized,
		JSON:        string(encoded),
		StateSHA256: hex.EncodeToString(digest[:]),
	}, nil
}

// DecodeCanonicalState loads only checkpoint schemas supported by this
// application version and verifies both canonical bytes and their identity.
func DecodeCanonicalState(
	encoded string,
	expectedSHA256 string,
) (CanonicalState, error) {
	if !validSHA256(expectedSHA256) || !json.Valid([]byte(encoded)) {
		return CanonicalState{}, errors.New(
			"checkpoint canonical state encoding is invalid",
		)
	}
	var envelope struct {
		SchemaVersion uint64 `json:"schema_version"`
	}
	if err := json.Unmarshal([]byte(encoded), &envelope); err != nil {
		return CanonicalState{}, errors.New(
			"checkpoint canonical state encoding is invalid",
		)
	}
	if envelope.SchemaVersion != SchemaVersion {
		return CanonicalState{}, errors.New(
			"checkpoint schema version is not compatible with this application",
		)
	}
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return CanonicalState{}, errors.New(
			"checkpoint canonical state encoding is invalid",
		)
	}
	state, err := Canonicalize(snapshot)
	if err != nil {
		return CanonicalState{}, err
	}
	if state.JSON != encoded || state.StateSHA256 != expectedSHA256 {
		return CanonicalState{}, errors.New(
			"checkpoint canonical state or hash is inconsistent",
		)
	}
	return state, nil
}

func validateSnapshot(value Snapshot) error {
	switch {
	case value.SchemaVersion != SchemaVersion:
		return errors.New("checkpoint schema version is unsupported")
	case value.TaskID.IsZero() || value.RunID.IsZero() ||
		value.RepositoryID.IsZero():
		return errors.New("checkpoint task, run, and repository are required")
	case value.PlanRevision == 0:
		return errors.New("checkpoint plan revision is required")
	case !validGitObjectID(value.BaseRevision) ||
		!validGitObjectID(value.WorktreeHead) ||
		!validGitObjectID(value.PreservedRevision):
		return errors.New("checkpoint Git revisions are invalid")
	case !validSHA256(value.DiffSHA256):
		return errors.New("checkpoint diff identity is invalid")
	case len(value.DirtyFiles) > maximumDirtyFiles:
		return errors.New("checkpoint dirty-file count exceeds the bound")
	case len(value.CompletedPlanSteps)+len(value.PendingPlanSteps) == 0 ||
		len(value.CompletedPlanSteps)+len(value.PendingPlanSteps) >
			maximumPlanSteps:
		return errors.New("checkpoint plan-step count is outside the bound")
	case value.ExternalOutcomeAmbiguous !=
		(len(value.AmbiguousExternalActions) != 0):
		return errors.New("checkpoint ambiguous-outcome flag differs from actions")
	case len(value.AmbiguousExternalActions) > maximumAmbiguousActions:
		return errors.New("checkpoint ambiguous-action count exceeds the bound")
	}
	if err := validateDirtyFiles(value.DirtyFiles); err != nil {
		return err
	}
	if err := validatePlanSteps(
		value.CompletedPlanSteps,
		value.PendingPlanSteps,
	); err != nil {
		return err
	}
	if err := validateBudgetPosition(value.Budget); err != nil {
		return err
	}
	if value.Policy.Revision == 0 ||
		!boundedTrimmed(value.Policy.Version, 255) ||
		!validSHA256(value.Policy.ContentSHA256) {
		return errors.New("checkpoint policy binding is invalid")
	}
	if value.Tools.SchemaVersion < 1 ||
		!validSHA256(value.Tools.CatalogSHA256) {
		return errors.New("checkpoint tool binding is invalid")
	}
	if value.Provider.SettingsRevision == 0 ||
		!validSHA256(value.Provider.RunConfigurationSHA256) {
		return errors.New("checkpoint run-configuration binding is invalid")
	}
	providerFields := []string{
		value.Provider.Adapter,
		value.Provider.AdapterVersion,
		value.Provider.Provider,
		value.Provider.ProviderVersion,
		value.Provider.Model,
		value.Provider.ModelRevision,
	}
	for _, field := range providerFields {
		if !boundedTrimmed(field, 255) {
			return errors.New("checkpoint provider/model binding is invalid")
		}
	}
	return validateAmbiguousActions(value.AmbiguousExternalActions)
}

func validateDirtyFiles(values []DirtyFileHash) error {
	previous := ""
	for _, value := range values {
		if value.Path == "" || value.Path != path.Clean(value.Path) ||
			len(value.Path) > 4096 ||
			strings.Contains(value.Path, "\\") ||
			path.IsAbs(value.Path) ||
			value.Path == ".." || strings.HasPrefix(value.Path, "../") {
			return errors.New("checkpoint dirty-file path is unsafe")
		}
		if previous != "" && value.Path <= previous {
			return errors.New("checkpoint dirty-file paths are not unique")
		}
		previous = value.Path
		if value.Exists && !validSHA256(value.SHA256) {
			return errors.New("checkpoint dirty-file identity is invalid")
		}
		if !value.Exists && value.SHA256 != "" {
			return errors.New("deleted checkpoint file cannot carry a content hash")
		}
	}
	return nil
}

func validatePlanSteps(
	completed []PlanStepSnapshot,
	pending []PlanStepSnapshot,
) error {
	seen := make(map[string]struct{}, len(completed)+len(pending))
	for _, value := range completed {
		if !validStepID(value.ID) ||
			value.State != PlanStepImplemented &&
				value.State != PlanStepValidated &&
				value.State != PlanStepSkipped {
			return errors.New("completed checkpoint plan step is invalid")
		}
		if _, exists := seen[value.ID]; exists {
			return errors.New("checkpoint plan-step ID is duplicated")
		}
		seen[value.ID] = struct{}{}
	}
	for _, value := range pending {
		if !validStepID(value.ID) ||
			value.State != PlanStepPending &&
				value.State != PlanStepInProgress &&
				value.State != PlanStepFailed {
			return errors.New("pending checkpoint plan step is invalid")
		}
		if _, exists := seen[value.ID]; exists {
			return errors.New("checkpoint plan-step ID is duplicated")
		}
		seen[value.ID] = struct{}{}
	}
	return nil
}

func validateBudgetPosition(value BudgetPosition) error {
	if value.BudgetID.IsZero() {
		return errors.New("checkpoint budget position is incomplete")
	}
	costs := []ExactCost{
		value.ReservedCost,
		value.ChargedCost,
		value.ActualKnownCost,
	}
	for _, cost := range costs {
		if err := validateExactCost(cost); err != nil {
			return err
		}
		if cost.Currency != costs[0].Currency {
			return errors.New("checkpoint budget currencies differ")
		}
	}
	return nil
}

func validateExactCost(value ExactCost) error {
	if _, err := domain.ParseCurrencyCode(string(value.Currency)); err != nil ||
		value.Numerator < 0 || value.Denominator < 1 {
		return errors.New("checkpoint exact cost is invalid")
	}
	return nil
}

func validateAmbiguousActions(values []AmbiguousExternalAction) error {
	previous := ""
	for _, value := range values {
		if !boundedIdentifier(value.ActionID, 255) ||
			!boundedIdentifier(value.Kind, 128) ||
			!validSHA256(value.IntentSHA256) ||
			value.ToolRequestID != "" &&
				!boundedIdentifier(value.ToolRequestID, 255) {
			return errors.New("checkpoint ambiguous external action is invalid")
		}
		if previous != "" && value.ActionID <= previous {
			return errors.New(
				"checkpoint ambiguous external-action IDs are not unique",
			)
		}
		previous = value.ActionID
	}
	return nil
}

func cloneSnapshot(value Snapshot) Snapshot {
	value.DirtyFiles = append([]DirtyFileHash(nil), value.DirtyFiles...)
	value.CompletedPlanSteps = append(
		[]PlanStepSnapshot(nil),
		value.CompletedPlanSteps...,
	)
	value.PendingPlanSteps = append(
		[]PlanStepSnapshot(nil),
		value.PendingPlanSteps...,
	)
	value.AmbiguousExternalActions = append(
		[]AmbiguousExternalAction(nil),
		value.AmbiguousExternalActions...,
	)
	if value.DirtyFiles == nil {
		value.DirtyFiles = []DirtyFileHash{}
	}
	if value.CompletedPlanSteps == nil {
		value.CompletedPlanSteps = []PlanStepSnapshot{}
	}
	if value.PendingPlanSteps == nil {
		value.PendingPlanSteps = []PlanStepSnapshot{}
	}
	if value.AmbiguousExternalActions == nil {
		value.AmbiguousExternalActions = []AmbiguousExternalAction{}
	}
	return value
}

func validStepID(value string) bool {
	return boundedTrimmed(value, 255)
}

func boundedIdentifier(value string, maximum int) bool {
	if !boundedTrimmed(value, maximum) {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			strings.ContainsRune("-._:/", character) {
			continue
		}
		return false
	}
	return true
}

func boundedTrimmed(value string, maximum int) bool {
	return value != "" &&
		strings.TrimSpace(value) == value &&
		len(value) <= maximum
}

func validGitObjectID(value string) bool {
	return len(value) == 40 && lowerHex(value) ||
		len(value) == 64 && lowerHex(value)
}

func validSHA256(value string) bool {
	return len(value) == sha256.Size*2 && lowerHex(value)
}

func lowerHex(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}
