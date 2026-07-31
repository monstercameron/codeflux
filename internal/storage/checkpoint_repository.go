package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/policy"
)

// ReadCheckpointRuntimeState projects only authoritative durable bindings.
// Policy, tools, budget, plan progress, events, and ambiguous outcomes are
// derived here rather than accepted from a capture caller.
func (repositories *Repositories) ReadCheckpointRuntimeState(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
) (checkpoint.RuntimeState, error) {
	if taskID.IsZero() || runID.IsZero() {
		return checkpoint.RuntimeState{},
			errors.New("checkpoint task and run are required")
	}
	binding, err := repositories.GetRunPlanBinding(ctx, runID)
	if err != nil {
		return checkpoint.RuntimeState{}, err
	}
	if binding.TaskID != taskID {
		return checkpoint.RuntimeState{},
			typedError(
				ErrConstraint,
				"read checkpoint runtime",
				errors.New("run plan belongs to another task"),
			)
	}
	storedSteps, err := repositories.ListPlanStepStates(ctx, runID)
	if err != nil {
		return checkpoint.RuntimeState{}, err
	}
	steps := make([]checkpoint.PlanStepSnapshot, 0, len(storedSteps))
	for _, value := range storedSteps {
		state, err := checkpointStepState(value.State)
		if err != nil {
			return checkpoint.RuntimeState{}, err
		}
		steps = append(steps, checkpoint.PlanStepSnapshot{
			ID: value.PlanStepID, State: state,
		})
	}
	budget, err := repositories.GetBudgetSnapshot(ctx, taskID)
	if err != nil {
		return checkpoint.RuntimeState{}, err
	}
	if budget.BudgetID != binding.BudgetID {
		return checkpoint.RuntimeState{},
			typedError(
				ErrCorrupt,
				"read checkpoint runtime",
				errors.New("run plan and budget differ"),
			)
	}
	var policyBinding checkpoint.PolicyBinding
	var policyJSON string
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT revision, policy_version, canonical_json, content_sha256
		 FROM execution_policy_revisions
		 WHERE task_id = ? AND revision = ?`,
		taskID,
		binding.PolicyRevision,
	).Scan(
		&policyBinding.Revision,
		&policyBinding.Version,
		&policyJSON,
		&policyBinding.ContentSHA256,
	); err != nil {
		return checkpoint.RuntimeState{},
			classify("read checkpoint policy binding", err)
	}
	var policySnapshot policy.Snapshot
	if err := json.Unmarshal([]byte(policyJSON), &policySnapshot); err != nil ||
		policySnapshot.Validate() != nil {
		return checkpoint.RuntimeState{}, typedError(
			ErrCorrupt,
			"read checkpoint provider binding",
			errors.New("execution policy snapshot is invalid"),
		)
	}
	runConfiguration, err := repositories.GetRunConfiguration(ctx, runID)
	if err != nil {
		return checkpoint.RuntimeState{}, err
	}
	providerBinding := checkpoint.ProviderBinding{
		SettingsRevision:       runConfiguration.SettingsRevision,
		RunConfigurationSHA256: runConfiguration.ContentSHA256,
		Adapter:                policySnapshot.Model.Provider.Adapter,
		AdapterVersion:         policySnapshot.Model.Provider.AdapterVersion,
		Provider:               policySnapshot.Model.Provider.Provider,
		ProviderVersion:        policySnapshot.Model.Provider.ProviderVersion,
		Model:                  policySnapshot.Model.Model,
		ModelRevision:          policySnapshot.Model.Revision,
	}
	var toolVersion int
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT schema_version FROM run_tool_schemas WHERE run_id = ?`,
		runID,
	).Scan(&toolVersion); err != nil {
		return checkpoint.RuntimeState{},
			classify("read checkpoint tool binding", err)
	}
	replay, err := repositories.ReplayTask(ctx, taskID)
	if err != nil {
		return checkpoint.RuntimeState{}, err
	}
	ambiguous, err := repositories.listCheckpointAmbiguousActions(
		ctx,
		taskID,
		runID,
	)
	if err != nil {
		return checkpoint.RuntimeState{}, err
	}
	return checkpoint.RuntimeState{
		PlanRevision:   binding.PlanRevision,
		PolicyRevision: binding.PolicyRevision,
		PlanSteps:      steps,
		Budget:         checkpointBudgetPosition(budget),
		Policy:         policyBinding,
		Provider:       providerBinding,
		Tools: checkpoint.ToolBinding{
			SchemaVersion: toolVersion,
			CatalogSHA256: checkpointToolCatalogSHA256(),
		},
		AmbiguousExternalActions: ambiguous,
		LastDurableEventSequence: replay.EventCount,
	}, nil
}

func checkpointToolCatalogSHA256() string {
	catalog := executor.ToolCatalog()
	slices.SortFunc(catalog, func(left, right executor.ToolDescriptor) int {
		return strings.Compare(string(left.Name), string(right.Name))
	})
	encoded, _ := json.Marshal(catalog)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

type checkpointQueryer interface {
	queryRower
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (repositories *Repositories) listCheckpointAmbiguousActions(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
) ([]checkpoint.AmbiguousExternalAction, error) {
	return listCheckpointAmbiguousActions(
		ctx,
		repositories.database.sql,
		taskID,
		runID,
	)
}

func listCheckpointAmbiguousActions(
	ctx context.Context,
	queries checkpointQueryer,
	taskID domain.TaskID,
	runID domain.RunID,
) ([]checkpoint.AmbiguousExternalAction, error) {
	return listRecoveryAmbiguousActions(ctx, queries, taskID, runID)
}

// FindCheckpointByIdempotency returns the checkpoint plus the exact request
// alias used by this conceptual capture attempt.
func (repositories *Repositories) FindCheckpointByIdempotency(
	ctx context.Context,
	taskID domain.TaskID,
	key string,
) (checkpoint.PersistedCheckpoint, bool, error) {
	if taskID.IsZero() || strings.TrimSpace(key) != key ||
		key == "" || len(key) > 255 {
		return checkpoint.PersistedCheckpoint{}, false,
			errors.New("checkpoint request identity is invalid")
	}
	value, err := scanVersionedCheckpoint(
		repositories.database.sql.QueryRowContext(
			ctx,
			versionedCheckpointSelect+
				` JOIN checkpoint_request_aliases AS alias
				    ON alias.checkpoint_id = checkpoint.id
				   AND alias.task_id = checkpoint.task_id
				  WHERE alias.task_id = ? AND alias.idempotency_key = ?`,
			taskID,
			key,
		),
	)
	if errors.Is(err, ErrNotFound) {
		return checkpoint.PersistedCheckpoint{}, false, nil
	}
	return value, err == nil, err
}

// CommitCheckpointAndEvent commits a new canonical checkpoint and its ordered
// event together, or creates only an immutable request alias when identical
// state already exists for the run.
func (repositories *Repositories) CommitCheckpointAndEvent(
	ctx context.Context,
	input checkpoint.AtomicCommit,
) (checkpoint.PersistedCheckpoint, bool, error) {
	state, err := validateAtomicCheckpointCommit(input)
	if err != nil {
		return checkpoint.PersistedCheckpoint{}, false, err
	}
	now, micros := repositories.timestamp()
	var value checkpoint.PersistedCheckpoint
	created := false
	err = repositories.database.RunInTransaction(
		ctx,
		func(transaction *Transaction) error {
			existing, found, err := findCheckpointAlias(
				ctx,
				transaction.sql,
				input.TaskID,
				input.IdempotencyKey,
			)
			if err != nil {
				return err
			}
			if found {
				if existing.CaptureRequestSHA256 !=
					input.CaptureRequestSHA256 {
					return typedError(
						ErrConflict,
						"commit checkpoint",
						errors.New(
							"checkpoint idempotency key belongs to another request",
						),
					)
				}
				value = existing
				return nil
			}
			if state.Snapshot.Tools.CatalogSHA256 !=
				checkpointToolCatalogSHA256() {
				return typedError(
					ErrConstraint,
					"commit checkpoint",
					errors.New(
						"checkpoint tool catalog differs from the registered catalog",
					),
				)
			}
			currentAmbiguous, err := listCheckpointAmbiguousActions(
				ctx,
				transaction.sql,
				input.TaskID,
				input.RunID,
			)
			if err != nil {
				return err
			}
			if !slices.Equal(
				currentAmbiguous,
				state.Snapshot.AmbiguousExternalActions,
			) {
				return typedError(
					ErrStaleRevision,
					"commit checkpoint",
					errors.New(
						"ambiguous external outcomes changed before checkpoint commit",
					),
				)
			}
			existing, found, err = findCheckpointByState(
				ctx,
				transaction.sql,
				input.TaskID,
				input.RunID,
				input.StateSHA256,
			)
			if err != nil {
				return err
			}
			if found {
				if _, err := transaction.sql.ExecContext(
					ctx,
					`INSERT INTO checkpoint_request_aliases (
						task_id, idempotency_key, checkpoint_id,
						capture_request_sha256, created_at_unix_micros
					 ) VALUES (?, ?, ?, ?, ?)`,
					input.TaskID,
					input.IdempotencyKey,
					existing.ID,
					input.CaptureRequestSHA256,
					micros,
				); err != nil {
					return repositoryWriteError(
						"alias deduplicated checkpoint",
						err,
					)
				}
				existing.CaptureRequestSHA256 =
					input.CaptureRequestSHA256
				existing.IdempotencyKey = input.IdempotencyKey
				value = existing
				return nil
			}
			var latestSequence uint64
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT COALESCE(MAX(sequence), 0)
				 FROM task_events WHERE task_id = ?`,
				input.TaskID,
			).Scan(&latestSequence); err != nil {
				return classify(
					"verify checkpoint event boundary",
					err,
				)
			}
			if latestSequence != input.ExpectedEventSequence {
				return typedError(
					ErrStaleRevision,
					"commit checkpoint",
					errors.New(
						"task event sequence changed before checkpoint commit",
					),
				)
			}
			eventID, err := domain.NewEventID()
			if err != nil {
				return err
			}
			event, err := appendTaskEventTransaction(
				ctx,
				transaction,
				AppendTaskEvent{
					ID:          eventID,
					TaskID:      input.TaskID,
					RunID:       &input.RunID,
					EventType:   "checkpoint.created",
					PayloadJSON: input.EventPayloadJSON,
					IdempotencyKey: "checkpoint-event/" +
						input.IdempotencyKey,
				},
				now,
				micros,
			)
			if err != nil {
				return err
			}
			attributionJSON, err := json.Marshal(input.Attribution)
			if err != nil {
				return err
			}
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO checkpoints (
					id, task_id, run_id, state, repository_revision,
					worktree_diff_hash, event_sequence, idempotency_key,
					created_at_unix_micros, updated_at_unix_micros, revision,
					schema_version, repository_id,
					worktree_binding_revision, plan_revision, base_revision,
					worktree_head, preserved_revision, preserved_ref,
					trigger_kind, attribution_json, canonical_state_json,
					state_sha256
				 ) VALUES (
					?, ?, ?, 'ready', ?, ?, ?, ?, ?, ?, 0,
					?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
				 )`,
				input.CheckpointID,
				input.TaskID,
				input.RunID,
				input.PreservedRevision,
				state.Snapshot.DiffSHA256,
				event.Sequence,
				input.IdempotencyKey,
				micros,
				micros,
				input.SchemaVersion,
				state.Snapshot.RepositoryID,
				state.Snapshot.WorktreeBindingRevision,
				state.Snapshot.PlanRevision,
				state.Snapshot.BaseRevision,
				state.Snapshot.WorktreeHead,
				input.PreservedRevision,
				input.PreservedRef,
				input.Trigger,
				string(attributionJSON),
				input.StateJSON,
				input.StateSHA256,
			); err != nil {
				return repositoryWriteError("insert versioned checkpoint", err)
			}
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO checkpoint_request_aliases (
					task_id, idempotency_key, checkpoint_id,
					capture_request_sha256, created_at_unix_micros
				 ) VALUES (?, ?, ?, ?, ?)`,
				input.TaskID,
				input.IdempotencyKey,
				input.CheckpointID,
				input.CaptureRequestSHA256,
				micros,
			); err != nil {
				return repositoryWriteError(
					"insert checkpoint request alias",
					err,
				)
			}
			value, found, err = findCheckpointAlias(
				ctx,
				transaction.sql,
				input.TaskID,
				input.IdempotencyKey,
			)
			created = err == nil && found
			return err
		},
	)
	return value, created, err
}

// LoadCheckpoint reads one version-compatible canonical checkpoint by ID.
func (repositories *Repositories) LoadCheckpoint(
	ctx context.Context,
	id domain.CheckpointID,
) (checkpoint.PersistedCheckpoint, error) {
	if id.IsZero() {
		return checkpoint.PersistedCheckpoint{},
			errors.New("checkpoint ID is required")
	}
	return scanVersionedCheckpoint(
		repositories.database.sql.QueryRowContext(
			ctx,
			versionedCheckpointSelect+
				` JOIN checkpoint_request_aliases AS alias
				    ON alias.checkpoint_id = checkpoint.id
				   AND alias.task_id = checkpoint.task_id
				  WHERE checkpoint.id = ?
				  ORDER BY alias.created_at_unix_micros, alias.idempotency_key
				  LIMIT 1`,
			id,
		),
	)
}

// LatestCheckpoint reads the newest compatible checkpoint for one exact run.
func (repositories *Repositories) LatestCheckpoint(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
) (checkpoint.PersistedCheckpoint, error) {
	if taskID.IsZero() || runID.IsZero() {
		return checkpoint.PersistedCheckpoint{},
			errors.New("checkpoint task and run are required")
	}
	return scanVersionedCheckpoint(
		repositories.database.sql.QueryRowContext(
			ctx,
			versionedCheckpointSelect+
				` JOIN checkpoint_request_aliases AS alias
				    ON alias.checkpoint_id = checkpoint.id
				   AND alias.task_id = checkpoint.task_id
				  WHERE checkpoint.task_id = ? AND checkpoint.run_id = ?
				  ORDER BY checkpoint.event_sequence DESC,
				           alias.created_at_unix_micros,
				           alias.idempotency_key
				  LIMIT 1`,
			taskID,
			runID,
		),
	)
}

const versionedCheckpointSelect = `SELECT
	checkpoint.id, checkpoint.task_id, checkpoint.run_id,
	checkpoint.schema_version, checkpoint.canonical_state_json,
	checkpoint.state_sha256, alias.capture_request_sha256,
	checkpoint.event_sequence, checkpoint.preserved_revision,
	checkpoint.preserved_ref, alias.idempotency_key
 FROM checkpoints AS checkpoint`

func scanVersionedCheckpoint(
	row rowScanner,
) (checkpoint.PersistedCheckpoint, error) {
	var value checkpoint.PersistedCheckpoint
	if err := row.Scan(
		&value.ID,
		&value.TaskID,
		&value.RunID,
		&value.SchemaVersion,
		&value.StateJSON,
		&value.StateSHA256,
		&value.CaptureRequestSHA256,
		&value.CheckpointEventSequence,
		&value.PreservedRevision,
		&value.PreservedRef,
		&value.IdempotencyKey,
	); errors.Is(err, sql.ErrNoRows) {
		return checkpoint.PersistedCheckpoint{},
			typedError(ErrNotFound, "read versioned checkpoint", err)
	} else if err != nil {
		return checkpoint.PersistedCheckpoint{},
			classify("read versioned checkpoint", err)
	}
	state, err := checkpoint.DecodeCanonicalState(
		value.StateJSON,
		value.StateSHA256,
	)
	if err != nil ||
		state.Snapshot.SchemaVersion != value.SchemaVersion ||
		state.Snapshot.TaskID != value.TaskID ||
		state.Snapshot.RunID != value.RunID ||
		state.Snapshot.PreservedRevision != value.PreservedRevision ||
		value.PreservedRef !=
			"refs/codeflux/checkpoints/"+value.ID.String() {
		return checkpoint.PersistedCheckpoint{},
			typedError(
				ErrCorrupt,
				"read versioned checkpoint",
				errors.New(
					"stored checkpoint is incompatible or inconsistent",
				),
			)
	}
	return value, nil
}

func findCheckpointAlias(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	key string,
) (checkpoint.PersistedCheckpoint, bool, error) {
	value, err := scanVersionedCheckpoint(
		queries.QueryRowContext(
			ctx,
			versionedCheckpointSelect+
				` JOIN checkpoint_request_aliases AS alias
				    ON alias.checkpoint_id = checkpoint.id
				   AND alias.task_id = checkpoint.task_id
				  WHERE alias.task_id = ? AND alias.idempotency_key = ?`,
			taskID,
			key,
		),
	)
	if errors.Is(err, ErrNotFound) {
		return checkpoint.PersistedCheckpoint{}, false, nil
	}
	return value, err == nil, err
}

func findCheckpointByState(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	runID domain.RunID,
	stateSHA string,
) (checkpoint.PersistedCheckpoint, bool, error) {
	value, err := scanVersionedCheckpoint(
		queries.QueryRowContext(
			ctx,
			versionedCheckpointSelect+
				` JOIN checkpoint_request_aliases AS alias
				    ON alias.checkpoint_id = checkpoint.id
				   AND alias.task_id = checkpoint.task_id
				  WHERE checkpoint.task_id = ? AND checkpoint.run_id = ?
				    AND checkpoint.state_sha256 = ?
				  ORDER BY alias.created_at_unix_micros, alias.idempotency_key
				  LIMIT 1`,
			taskID,
			runID,
			stateSHA,
		),
	)
	if errors.Is(err, ErrNotFound) {
		return checkpoint.PersistedCheckpoint{}, false, nil
	}
	return value, err == nil, err
}

func validateAtomicCheckpointCommit(
	input checkpoint.AtomicCommit,
) (checkpoint.CanonicalState, error) {
	switch {
	case input.CheckpointID.IsZero() ||
		input.TaskID.IsZero() ||
		input.RunID.IsZero():
		return checkpoint.CanonicalState{},
			errors.New("checkpoint atomic identities are required")
	case input.SchemaVersion != checkpoint.SchemaVersion:
		return checkpoint.CanonicalState{},
			errors.New("checkpoint atomic schema is unsupported")
	case len(input.CaptureRequestSHA256) != 64 ||
		len(input.PreservedRevision) != 40 &&
			len(input.PreservedRevision) != 64:
		return checkpoint.CanonicalState{},
			errors.New("checkpoint atomic hashes are invalid")
	case input.PreservedRef !=
		"refs/codeflux/checkpoints/"+input.CheckpointID.String():
		return checkpoint.CanonicalState{},
			errors.New("checkpoint atomic preservation ref is invalid")
	case strings.TrimSpace(input.IdempotencyKey) != input.IdempotencyKey ||
		input.IdempotencyKey == "" ||
		len(input.IdempotencyKey) > 255 ||
		!json.Valid([]byte(input.EventPayloadJSON)):
		return checkpoint.CanonicalState{},
			errors.New("checkpoint atomic request is invalid")
	}
	state, err := checkpoint.DecodeCanonicalState(
		input.StateJSON,
		input.StateSHA256,
	)
	if err != nil {
		return checkpoint.CanonicalState{}, err
	}
	if state.Snapshot.TaskID != input.TaskID ||
		state.Snapshot.RunID != input.RunID ||
		state.Snapshot.PreservedRevision != input.PreservedRevision ||
		state.Snapshot.Budget.SnapshotRevision !=
			input.ExpectedBudgetRevision ||
		state.Snapshot.WorktreeBindingRevision !=
			input.ExpectedWorktreeRevision ||
		state.Snapshot.LastDurableEventSequence !=
			input.ExpectedEventSequence {
		return checkpoint.CanonicalState{},
			errors.New("checkpoint atomic state differs from preconditions")
	}
	return state, nil
}

func checkpointBudgetPosition(value BudgetSnapshot) checkpoint.BudgetPosition {
	return checkpoint.BudgetPosition{
		BudgetID:               value.BudgetID,
		SnapshotRevision:       value.Revision,
		LimitRevision:          value.LimitRevision,
		ReservedCost:           checkpointExactCost(value.ReservedCost),
		ChargedCost:            checkpointExactCost(value.ChargedCost),
		ActualKnownCost:        checkpointExactCost(value.ActualKnownCost),
		CostAccountingUnknown:  value.CostAccountingUnknown,
		ReservedTokens:         value.ReservedTokens,
		ChargedTokens:          value.ChargedTokens,
		ActualTokens:           value.ActualTokens,
		TokenAccountingUnknown: value.TokenAccountingUnknown,
		ProviderCallSlots:      value.ProviderCallSlots,
		ReconciliationPending:  value.ReconciliationPending,
	}
}

func checkpointExactCost(value ExactMinorCost) checkpoint.ExactCost {
	return checkpoint.ExactCost{
		Currency:    value.Currency,
		Numerator:   value.Numerator,
		Denominator: value.Denominator,
	}
}

func checkpointStepState(
	value PlanStepState,
) (checkpoint.PlanStepState, error) {
	switch value {
	case PlanStepPending:
		return checkpoint.PlanStepPending, nil
	case PlanStepInProgress:
		return checkpoint.PlanStepInProgress, nil
	case PlanStepImplemented:
		return checkpoint.PlanStepImplemented, nil
	case PlanStepValidated:
		return checkpoint.PlanStepValidated, nil
	case PlanStepFailed:
		return checkpoint.PlanStepFailed, nil
	case PlanStepSkipped:
		return checkpoint.PlanStepSkipped, nil
	default:
		return "", typedError(
			ErrCorrupt,
			"read checkpoint runtime",
			fmt.Errorf("stored plan step state %q is invalid", value),
		)
	}
}

var (
	_ checkpoint.RuntimeStateSource = (*Repositories)(nil)
	_ checkpoint.AtomicStore        = (*Repositories)(nil)
)
