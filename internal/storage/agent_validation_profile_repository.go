package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

type SelectedValidationCommandEvidence struct {
	Ordinal              uint64   `json:"ordinal"`
	CommandID            string   `json:"command_id"`
	CommandFingerprint   string   `json:"command_fingerprint"`
	PlanCommand          string   `json:"plan_command"`
	Required             bool     `json:"required"`
	AcceptanceTest       bool     `json:"acceptance_test"`
	RelevantChangedFiles []string `json:"relevant_changed_files"`
	PlanStepIDs          []string `json:"plan_step_ids"`
}

type SelectedValidationProfileEvidence struct {
	TaskID         domain.TaskID
	RunID          domain.RunID
	PlanRevision   uint64
	ProfileName    string
	ProfileVersion string
	ProfileDigest  string
	Commands       []SelectedValidationCommandEvidence
	IdempotencyKey string
	CreatedAt      time.Time
}

type BindRunValidationProfile struct {
	TaskID         domain.TaskID
	RunID          domain.RunID
	PlanRevision   uint64
	ProfileName    string
	ProfileVersion string
	ProfileDigest  string
	Commands       []SelectedValidationCommandEvidence
	IdempotencyKey string
}

type DurableValidationExecution struct {
	TaskID                   domain.TaskID
	RunID                    domain.RunID
	PlanRevision             uint64
	ProfileDigest            string
	Round                    uint64
	Ordinal                  uint64
	ValidationID             domain.ValidationID
	CommandID                string
	CommandFingerprint       string
	State                    domain.ValidationState
	CommandExecutionID       *string
	PlanStepIDs              []string
	ValidationPassed         bool
	FailureSummaryRedacted   string
	FailureChangedFiles      []string
	FailurePlanStepIDs       []string
	FailurePresent           bool
	OutputTruncated          bool
	PresentationRedactedJSON string
	PresentationSHA256       string
	OperationDigest          string
	IdempotencyKey           string
	CreatedAt                time.Time
}

type validationOperationPresentation struct {
	ValidationID       domain.ValidationID         `json:"validation_id"`
	CommandID          string                      `json:"command_id"`
	CommandFingerprint string                      `json:"command_fingerprint"`
	Required           bool                        `json:"required"`
	AcceptanceTest     bool                        `json:"acceptance_test"`
	PlanStepIDs        []string                    `json:"plan_step_ids"`
	State              domain.ValidationState      `json:"state"`
	CommandExecutionID string                      `json:"command_execution_id"`
	Failure            *validationOperationFailure `json:"failure"`
}

type validationOperationFailure struct {
	SummaryRedacted string   `json:"summary_redacted"`
	ChangedFiles    []string `json:"changed_files"`
	PlanStepIDs     []string `json:"plan_step_ids"`
	OutputTruncated *bool    `json:"output_truncated"`
}

type durableValidationFailure struct {
	Present         bool
	ChangedFiles    []string
	PlanStepIDs     []string
	OutputTruncated bool
}

func (repositories *Repositories) BindRunValidationProfile(
	ctx context.Context,
	input BindRunValidationProfile,
) (SelectedValidationProfileEvidence, error) {
	commandsJSON, err := validateSelectedValidationProfile(input)
	if err != nil {
		return SelectedValidationProfileEvidence{}, err
	}
	var value SelectedValidationProfileEvidence
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		plan, err := scanPlanRevision(
			transaction.sql.QueryRowContext(
				ctx,
				planRevisionSelect+
					` WHERE plan.task_id = ? AND plan.revision = ?`,
				input.TaskID, input.PlanRevision,
			),
		)
		if err != nil {
			return err
		}
		selectedRank, selectedValid := validationProfileRank(input.ProfileName)
		floorRank, floorValid := validationProfileRank(
			string(plan.ValidationProfile),
		)
		if !selectedValid || !floorValid || selectedRank < floorRank {
			return typedError(
				ErrConstraint, "bind run validation profile",
				errors.New("selected profile is weaker than the plan floor"),
			)
		}
		requiredCommands := 0
		for _, command := range input.Commands {
			if command.Required {
				requiredCommands++
			}
		}
		if len(input.Commands) < int(selectedRank) ||
			requiredCommands < int(selectedRank) {
			return typedError(
				ErrConstraint, "bind run validation profile",
				errors.New(
					"selected profile label exceeds its command strength",
				),
			)
		}
		if len(input.Commands) != len(plan.Plan.ValidationCommands) {
			return typedError(
				ErrConstraint, "bind run validation profile",
				errors.New("selected commands differ from plan commands"),
			)
		}
		for index, command := range input.Commands {
			if command.PlanCommand != plan.Plan.ValidationCommands[index] {
				return typedError(
					ErrConstraint, "bind run validation profile",
					errors.New("selected command order differs from plan"),
				)
			}
			for _, changedFile := range command.RelevantChangedFiles {
				if !coveredByPlanPath(
					plan.Plan.ExpectedFiles, changedFile,
				) {
					return typedError(
						ErrConstraint, "bind run validation profile",
						errors.New("selected changed file is outside plan scope"),
					)
				}
			}
		}
		existing, found, err := findSelectedValidationProfile(
			ctx, transaction.sql, input.RunID,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.TaskID != input.TaskID ||
				existing.RunID != input.RunID ||
				existing.PlanRevision != input.PlanRevision ||
				existing.ProfileName != input.ProfileName ||
				existing.ProfileVersion != input.ProfileVersion ||
				existing.ProfileDigest != input.ProfileDigest ||
				string(mustJSON(existing.Commands)) != string(commandsJSON) ||
				existing.IdempotencyKey != input.IdempotencyKey {
				return typedError(
					ErrConflict, "bind run validation profile",
					errors.New("run already has another selected validation profile"),
				)
			}
			value = existing
			return nil
		}
		for _, command := range input.Commands {
			for _, stepID := range command.PlanStepIDs {
				var count uint64
				if err := transaction.sql.QueryRowContext(
					ctx,
					`SELECT COUNT(*) FROM agent_plan_steps
					 WHERE task_id = ? AND plan_revision = ? AND step_id = ?`,
					input.TaskID, input.PlanRevision, stepID,
				).Scan(&count); err != nil {
					return classify("verify selected validation plan step", err)
				}
				if count != 1 {
					return typedError(
						ErrConstraint, "bind run validation profile",
						errors.New("selected validation command references another plan"),
					)
				}
			}
		}
		_, micros := repositories.timestamp()
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO run_validation_profiles (
				task_id, run_id, plan_revision, profile_name, profile_version,
				profile_digest, commands_json, idempotency_key,
				created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.TaskID, input.RunID, input.PlanRevision,
			input.ProfileName, input.ProfileVersion, input.ProfileDigest,
			string(commandsJSON), input.IdempotencyKey, micros,
		); err != nil {
			return repositoryWriteError("bind run validation profile", err)
		}
		value, _, err = findSelectedValidationProfile(
			ctx, transaction.sql, input.RunID,
		)
		return err
	})
	return value, err
}

func validationProfileRank(name string) (uint8, bool) {
	switch ValidationProfileName(name) {
	case ValidationProfileRoutine:
		return 1, true
	case ValidationProfileElevated:
		return 2, true
	case ValidationProfileProtected:
		return 3, true
	default:
		return 0, false
	}
}

func (repositories *Repositories) GetRunValidationProfile(
	ctx context.Context,
	runID domain.RunID,
) (SelectedValidationProfileEvidence, error) {
	if runID.IsZero() {
		return SelectedValidationProfileEvidence{}, errors.New("run is required")
	}
	value, found, err := findSelectedValidationProfile(
		ctx, repositories.database.sql, runID,
	)
	if err != nil {
		return SelectedValidationProfileEvidence{}, err
	}
	if !found {
		return SelectedValidationProfileEvidence{},
			typedError(ErrNotFound, "get run validation profile", sql.ErrNoRows)
	}
	return value, nil
}

func (repositories *Repositories) ListDurableValidationExecutions(
	ctx context.Context,
	runID domain.RunID,
) ([]DurableValidationExecution, error) {
	if runID.IsZero() {
		return nil, errors.New("run is required")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT operation.task_id, operation.run_id, operation.plan_revision,
		        operation.profile_digest, operation.round, operation.ordinal,
		        operation.validation_id, operation.command_id,
		        operation.command_fingerprint, validation.state,
		        operation.command_execution_id, operation.plan_step_ids_json,
		        operation.validation_passed, validation.summary_redacted,
		        operation.presentation_redacted_json,
		        operation.presentation_sha256,
		        operation.failure_present,
		        operation.failure_changed_files_json,
		        operation.failure_plan_step_ids_json,
		        operation.output_truncated,
		        operation.operation_digest, operation.idempotency_key,
		        operation.created_at_unix_micros
		 FROM plan_validation_operations AS operation
		 JOIN validations AS validation ON validation.id = operation.validation_id
		 WHERE operation.run_id = ?
		 ORDER BY operation.round, operation.ordinal`,
		runID,
	)
	if err != nil {
		return nil, classify("list durable validation executions", err)
	}
	defer rows.Close()
	var values []DurableValidationExecution
	for rows.Next() {
		var value DurableValidationExecution
		var command sql.NullString
		var stepJSON string
		var passed int
		var summary sql.NullString
		var failurePresent, outputTruncated int
		var failureFilesJSON, failureStepsJSON string
		var micros int64
		if err := rows.Scan(
			&value.TaskID, &value.RunID, &value.PlanRevision,
			&value.ProfileDigest, &value.Round, &value.Ordinal,
			&value.ValidationID, &value.CommandID, &value.CommandFingerprint,
			&value.State, &command, &stepJSON, &passed, &summary,
			&value.PresentationRedactedJSON, &value.PresentationSHA256,
			&failurePresent, &failureFilesJSON, &failureStepsJSON,
			&outputTruncated,
			&value.OperationDigest, &value.IdempotencyKey, &micros,
		); err != nil {
			return nil, classify("scan durable validation execution", err)
		}
		if err := json.Unmarshal([]byte(stepJSON), &value.PlanStepIDs); err != nil {
			return nil, typedError(
				ErrCorrupt, "decode durable validation plan steps", err,
			)
		}
		if command.Valid {
			value.CommandExecutionID = &command.String
		}
		if summary.Valid {
			value.FailureSummaryRedacted = summary.String
		}
		if err := json.Unmarshal(
			[]byte(failureFilesJSON), &value.FailureChangedFiles,
		); err != nil {
			return nil, typedError(
				ErrCorrupt, "decode durable validation failure files", err,
			)
		}
		if err := json.Unmarshal(
			[]byte(failureStepsJSON), &value.FailurePlanStepIDs,
		); err != nil {
			return nil, typedError(
				ErrCorrupt, "decode durable validation failure steps", err,
			)
		}
		value.FailurePresent = failurePresent != 0
		value.OutputTruncated = outputTruncated != 0
		value.ValidationPassed = passed != 0
		value.CreatedAt = repositoryTime(micros)
		values = append(values, value)
	}
	return values, rows.Err()
}

func validateSelectedValidationProfile(
	input BindRunValidationProfile,
) ([]byte, error) {
	if input.TaskID.IsZero() || input.RunID.IsZero() ||
		input.PlanRevision == 0 ||
		!boundedProfileText(input.ProfileName, 255) ||
		!boundedProfileText(input.ProfileVersion, 255) ||
		!lowerHex(input.ProfileDigest) || len(input.ProfileDigest) != 64 ||
		!boundedProfileText(input.IdempotencyKey, 255) ||
		len(input.Commands) == 0 || len(input.Commands) > 64 {
		return nil, errors.New("selected validation profile is invalid")
	}
	seen := make(map[string]struct{}, len(input.Commands))
	seenPlanCommands := make(map[string]struct{}, len(input.Commands))
	seenFingerprints := make(map[string]struct{}, len(input.Commands))
	required := 0
	for index, command := range input.Commands {
		if command.Ordinal != uint64(index+1) ||
			!boundedProfileText(command.CommandID, 255) ||
			!boundedProfileText(command.PlanCommand, 4096) ||
			len(command.CommandFingerprint) != 64 ||
			!lowerHex(command.CommandFingerprint) ||
			!normalizedPlanPaths(command.RelevantChangedFiles, 128) ||
			len(command.PlanStepIDs) == 0 ||
			len(command.PlanStepIDs) > 64 {
			return nil, errors.New("selected validation command is invalid")
		}
		if command.AcceptanceTest && !command.Required {
			return nil, errors.New(
				"selected validation acceptance command must be required",
			)
		}
		if command.Required {
			required++
		}
		if _, exists := seen[command.CommandID]; exists {
			return nil, errors.New("selected validation command is duplicated")
		}
		seen[command.CommandID] = struct{}{}
		if _, exists := seenPlanCommands[command.PlanCommand]; exists {
			return nil, errors.New(
				"selected validation plan command is duplicated",
			)
		}
		seenPlanCommands[command.PlanCommand] = struct{}{}
		if _, exists := seenFingerprints[command.CommandFingerprint]; exists {
			return nil, errors.New(
				"selected validation command fingerprint is duplicated",
			)
		}
		seenFingerprints[command.CommandFingerprint] = struct{}{}
		seenSteps := make(map[string]struct{}, len(command.PlanStepIDs))
		for _, stepID := range command.PlanStepIDs {
			if !boundedProfileText(stepID, 64) {
				return nil, errors.New("selected validation plan step is invalid")
			}
			if _, exists := seenSteps[stepID]; exists {
				return nil, errors.New(
					"selected validation plan step is duplicated",
				)
			}
			seenSteps[stepID] = struct{}{}
		}
	}
	if required == 0 {
		return nil, errors.New("selected validation profile requires a gate")
	}
	if selectedValidationProfileDigest(
		input.ProfileName, input.ProfileVersion, input.Commands,
	) != input.ProfileDigest {
		return nil, errors.New("selected validation profile digest is inconsistent")
	}
	return json.Marshal(input.Commands)
}

func selectedValidationProfileDigest(
	name, version string,
	commands []SelectedValidationCommandEvidence,
) string {
	hash := sha256.New()
	writeValidationHashField(hash, name)
	writeValidationHashField(hash, version)
	for _, command := range commands {
		writeValidationHashField(hash, command.CommandFingerprint)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func writeValidationHashField(
	hash interface{ Write([]byte) (int, error) },
	value string,
) {
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(value))
}

func boundedProfileText(value string, maximum int) bool {
	return value != "" && strings.TrimSpace(value) == value &&
		len(value) <= maximum
}

func findSelectedValidationProfile(
	ctx context.Context,
	queries queryRower,
	runID domain.RunID,
) (SelectedValidationProfileEvidence, bool, error) {
	var value SelectedValidationProfileEvidence
	var commandsJSON string
	var micros int64
	err := queries.QueryRowContext(
		ctx,
		`SELECT task_id, run_id, plan_revision, profile_name, profile_version,
		        profile_digest, commands_json, idempotency_key,
		        created_at_unix_micros
		 FROM run_validation_profiles WHERE run_id = ?`,
		runID,
	).Scan(
		&value.TaskID, &value.RunID, &value.PlanRevision,
		&value.ProfileName, &value.ProfileVersion, &value.ProfileDigest,
		&commandsJSON, &value.IdempotencyKey, &micros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SelectedValidationProfileEvidence{}, false, nil
	}
	if err != nil {
		return SelectedValidationProfileEvidence{}, false,
			classify("read run validation profile", err)
	}
	if err := json.Unmarshal([]byte(commandsJSON), &value.Commands); err != nil {
		return SelectedValidationProfileEvidence{}, false,
			typedError(ErrCorrupt, "decode run validation profile", err)
	}
	if selectedValidationProfileDigest(
		value.ProfileName, value.ProfileVersion, value.Commands,
	) != value.ProfileDigest {
		return SelectedValidationProfileEvidence{}, false,
			typedError(
				ErrCorrupt, "validate run validation profile",
				errors.New("stored validation profile digest is inconsistent"),
			)
	}
	value.CreatedAt = repositoryTime(micros)
	return value, true, nil
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func validationOperationDigest(
	input RecordPlanValidationAttributions,
	stepJSON []byte,
) string {
	commandExecutionID := ""
	if input.CommandExecutionID != nil {
		commandExecutionID = *input.CommandExecutionID
	}
	repairAttemptRevision := ""
	if input.RepairAttemptRevision != nil {
		repairAttemptRevision = fmt.Sprint(*input.RepairAttemptRevision)
	}
	hash := sha256.New()
	for _, value := range []string{
		input.TaskID.String(), input.RunID.String(),
		fmt.Sprint(input.PlanRevision), input.ProfileDigest,
		fmt.Sprint(input.Round), fmt.Sprint(input.CommandOrdinal),
		input.ValidationID.String(), input.CommandID,
		input.CommandFingerprint, string(stepJSON),
		fmt.Sprint(input.ValidationPassed), input.PresentationSHA256,
		commandExecutionID, repairAttemptRevision,
	} {
		writeValidationHashField(hash, value)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func validateValidationOperationPresentation(
	input RecordPlanValidationAttributions,
	command SelectedValidationCommandEvidence,
	state domain.ValidationState,
	summary sql.NullString,
) (durableValidationFailure, error) {
	var presentation validationOperationPresentation
	if err := json.Unmarshal(
		[]byte(input.PresentationRedactedJSON), &presentation,
	); err != nil {
		return durableValidationFailure{}, err
	}
	commandExecutionID := ""
	if input.CommandExecutionID != nil {
		commandExecutionID = *input.CommandExecutionID
	}
	if presentation.ValidationID != input.ValidationID ||
		presentation.CommandID != command.CommandID ||
		presentation.CommandFingerprint != command.CommandFingerprint ||
		presentation.Required != command.Required ||
		presentation.AcceptanceTest != command.AcceptanceTest ||
		!equalStrings(presentation.PlanStepIDs, command.PlanStepIDs) ||
		presentation.State != state ||
		presentation.CommandExecutionID != commandExecutionID {
		return durableValidationFailure{},
			errors.New("validation presentation identity differs")
	}
	switch state {
	case domain.ValidationStatePassed:
		if presentation.Failure != nil {
			return durableValidationFailure{},
				errors.New("passed validation presentation has a failure")
		}
		return durableValidationFailure{
			ChangedFiles: []string{}, PlanStepIDs: []string{},
		}, nil
	case domain.ValidationStateFailed, domain.ValidationStateCancelled:
		if presentation.Failure == nil ||
			presentation.Failure.OutputTruncated == nil ||
			!boundedProfileText(
				presentation.Failure.SummaryRedacted, 4096,
			) ||
			!normalizedPlanPaths(
				presentation.Failure.ChangedFiles, 128,
			) ||
			!equalStrings(
				presentation.Failure.PlanStepIDs, command.PlanStepIDs,
			) {
			return durableValidationFailure{},
				errors.New("failed validation presentation linkage is invalid")
		}
		if !summary.Valid ||
			presentation.Failure.SummaryRedacted != summary.String {
			return durableValidationFailure{},
				errors.New("failed validation presentation summary differs")
		}
		for _, changedFile := range presentation.Failure.ChangedFiles {
			if !coveredByPlanPath(
				command.RelevantChangedFiles, changedFile,
			) {
				return durableValidationFailure{},
					errors.New("failure changed file is outside selected scope")
			}
		}
		return durableValidationFailure{
			Present:         true,
			ChangedFiles:    presentation.Failure.ChangedFiles,
			PlanStepIDs:     presentation.Failure.PlanStepIDs,
			OutputTruncated: *presentation.Failure.OutputTruncated,
		}, nil
	default:
		return durableValidationFailure{},
			errors.New("validation presentation state is not executable")
	}
}

func coveredByPlanPath(scopes []string, target string) bool {
	for _, scope := range scopes {
		if planPathCovers(scope, target) {
			return true
		}
	}
	return false
}

func recordValidationOperationTransaction(
	ctx context.Context,
	repositories *Repositories,
	transaction *Transaction,
	input RecordPlanValidationAttributions,
) error {
	if !json.Valid([]byte(input.PresentationRedactedJSON)) ||
		len(input.PresentationRedactedJSON) > 1_048_576 ||
		len(input.PresentationSHA256) != 64 ||
		!lowerHex(input.PresentationSHA256) ||
		hashJSON(input.PresentationRedactedJSON) != input.PresentationSHA256 {
		return typedError(
			ErrConstraint, "record validation operation",
			errors.New("validation presentation or digest is invalid"),
		)
	}
	stepJSON, _ := json.Marshal(input.PlanStepIDs)
	digest := validationOperationDigest(input, stepJSON)
	var existingDigest string
	err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT operation_digest FROM plan_validation_operations
		 WHERE run_id = ? AND idempotency_key = ?`,
		input.RunID, input.IdempotencyKey,
	).Scan(&existingDigest)
	if err == nil {
		if existingDigest != digest {
			return typedError(
				ErrConflict, "record validation operation",
				errors.New("validation operation idempotency identity changed"),
			)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return classify("find validation operation", err)
	}
	profile, found, err := findSelectedValidationProfile(
		ctx, transaction.sql, input.RunID,
	)
	if err != nil {
		return err
	}
	if !found || profile.TaskID != input.TaskID ||
		profile.PlanRevision != input.PlanRevision ||
		profile.ProfileDigest != input.ProfileDigest ||
		input.CommandOrdinal == 0 ||
		input.CommandOrdinal > uint64(len(profile.Commands)) {
		return typedError(
			ErrConstraint, "record validation operation",
			errors.New("validation operation differs from selected profile"),
		)
	}
	command := profile.Commands[input.CommandOrdinal-1]
	if command.CommandID != input.CommandID ||
		command.CommandFingerprint != input.CommandFingerprint ||
		!equalStrings(command.PlanStepIDs, input.PlanStepIDs) {
		return typedError(
			ErrConstraint, "record validation operation",
			errors.New("validation command identity or plan-step order differs"),
		)
	}
	var state domain.ValidationState
	var validationProfile string
	var summary sql.NullString
	if err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT state, profile_name, summary_redacted FROM validations
		 WHERE id = ? AND task_id = ? AND run_id = ?`,
		input.ValidationID, input.TaskID, input.RunID,
	).Scan(&state, &validationProfile, &summary); err != nil {
		return classify("verify validation operation", err)
	}
	if validationProfile != profile.ProfileName ||
		(input.ValidationPassed != (state == domain.ValidationStatePassed)) {
		return typedError(
			ErrConstraint, "record validation operation",
			errors.New("validation record differs from selected profile or outcome"),
		)
	}
	failure, err := validateValidationOperationPresentation(
		input, command, state, summary,
	)
	if err != nil {
		return typedError(
			ErrConstraint, "record validation operation presentation", err,
		)
	}
	failureFilesJSON, _ := json.Marshal(failure.ChangedFiles)
	failureStepsJSON, _ := json.Marshal(failure.PlanStepIDs)
	_, micros := repositories.timestamp()
	if _, err := transaction.sql.ExecContext(
		ctx,
		`INSERT INTO plan_validation_operations (
			task_id, run_id, plan_revision, profile_digest, round, ordinal,
			validation_id, command_id, command_fingerprint,
			command_execution_id, plan_step_ids_json, validation_passed,
			presentation_redacted_json, presentation_sha256,
			failure_present, failure_changed_files_json,
			failure_plan_step_ids_json, output_truncated,
			operation_digest, idempotency_key, created_at_unix_micros
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		)`,
		input.TaskID, input.RunID, input.PlanRevision, input.ProfileDigest,
		input.Round, input.CommandOrdinal, input.ValidationID,
		input.CommandID, input.CommandFingerprint,
		nullableString(input.CommandExecutionID), string(stepJSON),
		boolInteger(input.ValidationPassed), input.PresentationRedactedJSON,
		input.PresentationSHA256, boolInteger(failure.Present),
		string(failureFilesJSON), string(failureStepsJSON),
		boolInteger(failure.OutputTruncated),
		digest, input.IdempotencyKey, micros,
	); err != nil {
		return repositoryWriteError("record validation operation", err)
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
