package storage

import (
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
)

func TestCheckpointRepositoryCommitsEventAndDeduplicatesAliasesAtomically(
	t *testing.T,
) {
	fixture := createBoundAgentRunFixture(t, 7000)
	binding, err := fixture.repositories.CreateWorktreeBinding(
		t.Context(),
		CreateWorktreeBinding{
			WorkspaceID:  testWorkspaceID(t, 7001),
			TaskID:       fixture.task.ID,
			RepositoryID: fixture.task.RepositoryID,
			BaseRevision: fixture.plan.RepositoryRevision,
			HeadRevision: fixture.plan.RepositoryRevision,
			BranchName:   "codeflux/task/checkpoint-storage",
			WorktreePath: t.TempDir(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.RecordRunToolSchema(
		t.Context(),
		fixture.runID,
		executor.ToolSchemaVersion,
	); err != nil {
		t.Fatal(err)
	}
	runConfiguration := recordCheckpointRunConfiguration(t, fixture)
	runtimeState, err := fixture.repositories.ReadCheckpointRuntimeState(
		t.Context(),
		fixture.task.ID,
		fixture.runID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if runtimeState.Policy.Revision != fixture.plan.PolicyRevision ||
		runtimeState.Provider.SettingsRevision !=
			runConfiguration.SettingsRevision ||
		runtimeState.Provider.RunConfigurationSHA256 !=
			runConfiguration.ContentSHA256 ||
		runtimeState.Provider.Adapter == "" ||
		runtimeState.Provider.ModelRevision == "" ||
		runtimeState.Tools.SchemaVersion != executor.ToolSchemaVersion ||
		len(runtimeState.Tools.CatalogSHA256) != 64 {
		t.Fatalf("authoritative checkpoint runtime = %#v", runtimeState)
	}
	checkpointID, err := domain.NewCheckpointID()
	if err != nil {
		t.Fatal(err)
	}
	canonical := canonicalCheckpointFixture(
		t,
		fixture,
		binding,
		runtimeState,
		strings.Repeat("9", 40),
	)
	input := atomicCheckpointFixture(
		checkpointID,
		fixture,
		binding,
		runtimeState,
		canonical,
		"checkpoint-storage-first",
	)
	created, wasCreated, err :=
		fixture.repositories.CommitCheckpointAndEvent(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !wasCreated ||
		created.ID != checkpointID ||
		created.CheckpointEventSequence !=
			runtimeState.LastDurableEventSequence+1 {
		t.Fatalf("created checkpoint = %#v, %t", created, wasCreated)
	}
	var checkpointCount, eventCount, aliasCount int
	if err := fixture.repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT
		    (SELECT count(*) FROM checkpoints WHERE schema_version = 1),
		    (SELECT count(*) FROM task_events
		      WHERE task_id = ? AND event_type = 'checkpoint.created'),
		    (SELECT count(*) FROM checkpoint_request_aliases
		      WHERE task_id = ?)`,
		fixture.task.ID,
		fixture.task.ID,
	).Scan(&checkpointCount, &eventCount, &aliasCount); err != nil {
		t.Fatal(err)
	}
	if checkpointCount != 1 || eventCount != 1 || aliasCount != 1 {
		t.Fatalf(
			"checkpoint/event/alias counts = %d/%d/%d",
			checkpointCount,
			eventCount,
			aliasCount,
		)
	}

	secondID, _ := domain.NewCheckpointID()
	second := input
	second.CheckpointID = secondID
	second.PreservedRef =
		"refs/codeflux/checkpoints/" + secondID.String()
	second.IdempotencyKey = "checkpoint-storage-alias"
	second.CaptureRequestSHA256 = strings.Repeat("8", 64)
	deduplicated, wasCreated, err :=
		fixture.repositories.CommitCheckpointAndEvent(t.Context(), second)
	if err != nil {
		t.Fatal(err)
	}
	if wasCreated || deduplicated.ID != checkpointID ||
		deduplicated.IdempotencyKey != second.IdempotencyKey ||
		deduplicated.CaptureRequestSHA256 !=
			second.CaptureRequestSHA256 {
		t.Fatalf("deduplicated checkpoint = %#v, %t", deduplicated, wasCreated)
	}
	if err := fixture.repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT
		    (SELECT count(*) FROM checkpoints WHERE schema_version = 1),
		    (SELECT count(*) FROM task_events
		      WHERE task_id = ? AND event_type = 'checkpoint.created'),
		    (SELECT count(*) FROM checkpoint_request_aliases
		      WHERE task_id = ?)`,
		fixture.task.ID,
		fixture.task.ID,
	).Scan(&checkpointCount, &eventCount, &aliasCount); err != nil {
		t.Fatal(err)
	}
	if checkpointCount != 1 || eventCount != 1 || aliasCount != 2 {
		t.Fatalf(
			"deduplicated counts = %d/%d/%d",
			checkpointCount,
			eventCount,
			aliasCount,
		)
	}
	latest, err := fixture.repositories.LatestCheckpoint(
		t.Context(),
		fixture.task.ID,
		fixture.runID,
	)
	if err != nil || latest.ID != checkpointID ||
		latest.StateJSON != canonical.JSON ||
		latest.StateSHA256 != canonical.StateSHA256 ||
		latest.PreservedRef != created.PreservedRef {
		t.Fatalf("latest checkpoint = %#v, %v", latest, err)
	}
}

func TestCheckpointRepositoryStaleBoundaryRollsBackEventAndCheckpoint(
	t *testing.T,
) {
	fixture := createBoundAgentRunFixture(t, 7100)
	binding, err := fixture.repositories.CreateWorktreeBinding(
		t.Context(),
		CreateWorktreeBinding{
			WorkspaceID:  testWorkspaceID(t, 7101),
			TaskID:       fixture.task.ID,
			RepositoryID: fixture.task.RepositoryID,
			BaseRevision: fixture.plan.RepositoryRevision,
			HeadRevision: fixture.plan.RepositoryRevision,
			BranchName:   "codeflux/task/checkpoint-stale",
			WorktreePath: t.TempDir(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.RecordRunToolSchema(
		t.Context(),
		fixture.runID,
		executor.ToolSchemaVersion,
	); err != nil {
		t.Fatal(err)
	}
	recordCheckpointRunConfiguration(t, fixture)
	runtimeState, err := fixture.repositories.ReadCheckpointRuntimeState(
		t.Context(),
		fixture.task.ID,
		fixture.runID,
	)
	if err != nil {
		t.Fatal(err)
	}
	canonical := canonicalCheckpointFixture(
		t,
		fixture,
		binding,
		runtimeState,
		strings.Repeat("7", 40),
	)
	id, _ := domain.NewCheckpointID()
	input := atomicCheckpointFixture(
		id,
		fixture,
		binding,
		runtimeState,
		canonical,
		"checkpoint-storage-stale",
	)
	substitutedRuntime := runtimeState
	substitutedRuntime.Provider.Model = "substituted-model"
	substituted := canonicalCheckpointFixture(
		t,
		fixture,
		binding,
		substitutedRuntime,
		strings.Repeat("8", 40),
	)
	substitutedID, _ := domain.NewCheckpointID()
	substitutedInput := atomicCheckpointFixture(
		substitutedID,
		fixture,
		binding,
		runtimeState,
		substituted,
		"checkpoint-storage-provider-substitution",
	)
	if _, _, err := fixture.repositories.CommitCheckpointAndEvent(
		t.Context(),
		substitutedInput,
	); !errors.Is(err, ErrConstraint) {
		t.Fatalf("substituted provider binding error = %v", err)
	}
	if _, err := fixture.repositories.AppendTaskEvent(
		t.Context(),
		AppendTaskEvent{
			ID:             testEventID(t, 7110),
			TaskID:         fixture.task.ID,
			RunID:          &fixture.runID,
			EventType:      "fixture.intervening",
			PayloadJSON:    `{}`,
			IdempotencyKey: "fixture-intervening-event",
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.repositories.CommitCheckpointAndEvent(
		t.Context(),
		input,
	); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale checkpoint boundary error = %v", err)
	}
	var checkpoints, events int
	if err := fixture.repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT
		    (SELECT count(*) FROM checkpoints WHERE schema_version = 1),
		    (SELECT count(*) FROM task_events
		      WHERE task_id = ? AND event_type = 'checkpoint.created')`,
		fixture.task.ID,
	).Scan(&checkpoints, &events); err != nil {
		t.Fatal(err)
	}
	if checkpoints != 0 || events != 0 {
		t.Fatalf(
			"stale boundary retained checkpoints/events = %d/%d",
			checkpoints,
			events,
		)
	}
}

func TestRecoveryAssessmentAllowsActiveRunBeforeFirstCheckpoint(t *testing.T) {
	fixture := createBoundAgentRunFixture(t, 7200)
	if _, err := fixture.repositories.database.sql.ExecContext(
		t.Context(),
		`INSERT INTO checkpoint_recovery_assessments (
			id, task_id, run_id, checkpoint_id, classification,
			findings_redacted_json, divergences_redacted_json,
			patch_available, patch_locator, patch_path,
			observation_sha256, idempotency_key, created_at_unix_micros
		 ) VALUES (?, ?, ?, NULL, 'unrecoverable', '[]', '[]',
		           0, NULL, NULL, ?, ?, ?)`,
		"recovery-assessment-before-checkpoint",
		fixture.task.ID,
		fixture.runID,
		strings.Repeat("a", 64),
		"recovery-assessment-before-checkpoint",
		1,
	); err != nil {
		t.Fatal(err)
	}
	var checkpointID *string
	if err := fixture.repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT checkpoint_id
		 FROM checkpoint_recovery_assessments
		 WHERE id = 'recovery-assessment-before-checkpoint'`,
	).Scan(&checkpointID); err != nil {
		t.Fatal(err)
	}
	if checkpointID != nil {
		t.Fatalf("pre-checkpoint recovery assessment = %v", *checkpointID)
	}
}

func TestCheckpointRuntimeDerivesUnresolvedIntentsAndRejectsSubstitution(
	t *testing.T,
) {
	fixture := createBoundAgentRunFixture(t, 7300)
	binding, err := fixture.repositories.CreateWorktreeBinding(
		t.Context(),
		CreateWorktreeBinding{
			WorkspaceID:  testWorkspaceID(t, 7301),
			TaskID:       fixture.task.ID,
			RepositoryID: fixture.task.RepositoryID,
			BaseRevision: fixture.plan.RepositoryRevision,
			HeadRevision: fixture.plan.RepositoryRevision,
			BranchName:   "codeflux/task/checkpoint-ambiguous",
			WorktreePath: t.TempDir(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.RecordRunToolSchema(
		t.Context(),
		fixture.runID,
		executor.ToolSchemaVersion,
	); err != nil {
		t.Fatal(err)
	}
	recordCheckpointRunConfiguration(t, fixture)
	modelRequest := createAgentModelRequestFixture(t, fixture)
	if _, err := fixture.repositories.TransitionProviderLogicalRequest(
		t.Context(),
		TransitionProviderLogicalRequest{
			ID: modelRequest.ID, ExpectedRevision: modelRequest.Revision,
			From:             ProviderLogicalRequestPlanned,
			To:               ProviderLogicalRequestInFlight,
			AccountingStatus: ProviderAccountingUnknown,
		},
	); err != nil {
		t.Fatal(err)
	}
	toolRequest, err := fixture.repositories.RecordAgentToolRequest(
		t.Context(),
		RecordAgentToolRequest{
			ID: "checkpoint-ambiguous-tool", TaskID: fixture.task.ID,
			RunID: fixture.runID, PlanRevision: fixture.plan.Revision,
			PlanStepID:     fixture.plan.Plan.Steps[0].ID,
			ModelRequestID: modelRequest.ID,
			ToolName:       "workspace.apply-edit", ToolSchemaVersion: 1,
			ArgumentsRedactedJSON: `{"path":"internal/widget.go"}`,
			ArgumentsSHA256:       strings.Repeat("d", 64),
			IdempotencyKey:        "checkpoint-ambiguous-tool",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	command, err := fixture.repositories.CreateCommandExecution(
		t.Context(),
		CreateCommandExecution{
			ID: "checkpoint-running-command", TaskID: fixture.task.ID,
			RunID:       fixture.runID,
			State:       domain.CommandExecutionStateAuthorized,
			CommandName: "go", ArgumentsRedactedJSON: `["test","./..."]`,
			WorkingDirectoryScope: "task-worktree",
			IdempotencyKey:        "checkpoint-running-command",
			ToolSchemaVersion:     executor.ToolSchemaVersion,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.TransitionCommandExecution(
		t.Context(),
		TransitionCommandExecution{
			ID: command.ID, ExpectedRevision: command.Revision,
			To: domain.CommandExecutionStateRunning,
		},
	); err != nil {
		t.Fatal(err)
	}
	runtimeState, err := fixture.repositories.ReadCheckpointRuntimeState(
		t.Context(),
		fixture.task.ID,
		fixture.runID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtimeState.AmbiguousExternalActions) != 3 {
		t.Fatalf("derived ambiguous actions = %#v", runtimeState)
	}
	actions := make(map[string]checkpoint.AmbiguousExternalAction, 3)
	for _, action := range runtimeState.AmbiguousExternalActions {
		actions[action.ActionID] = action
	}
	if actions[modelRequest.ID.String()].Kind != "provider-request" ||
		actions[toolRequest.ID].Kind != "tool-request" ||
		actions[toolRequest.ID].IntentSHA256 != strings.Repeat("d", 64) ||
		actions[command.ID].Kind != "command-execution" ||
		len(actions[command.ID].IntentSHA256) != 64 {
		t.Fatalf("derived ambiguous action identities = %#v", actions)
	}
	forgedRuntime := runtimeState
	forgedRuntime.AmbiguousExternalActions = nil
	forged := canonicalCheckpointFixture(
		t,
		fixture,
		binding,
		forgedRuntime,
		strings.Repeat("4", 40),
	)
	id, _ := domain.NewCheckpointID()
	input := atomicCheckpointFixture(
		id,
		fixture,
		binding,
		runtimeState,
		forged,
		"checkpoint-forged-ambiguity",
	)
	if _, _, err := fixture.repositories.CommitCheckpointAndEvent(
		t.Context(),
		input,
	); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("forged ambiguous checkpoint error = %v", err)
	}
}

func recordCheckpointRunConfiguration(
	t *testing.T,
	fixture boundAgentRunFixture,
) RunConfiguration {
	t.Helper()
	settings, err := fixture.repositories.CreateSettingsRevision(
		t.Context(),
		CreateSettingsRevision{
			Scope:             SettingsScopeUser,
			ConfigurationJSON: `{"provider_profile":"fixture"}`,
			IdempotencyKey:    "checkpoint-settings",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.database.sql.ExecContext(
		t.Context(),
		`UPDATE task_settings_bindings
		 SET settings_revision = ?
		 WHERE task_id = ?`,
		settings.Revision,
		fixture.task.ID,
	); err != nil {
		t.Fatal(err)
	}
	value, err := fixture.repositories.RecordRunConfiguration(
		t.Context(),
		RecordRunConfiguration{
			RunID:            fixture.runID,
			SettingsRevision: settings.Revision,
			EffectiveJSON:    `{"provider_profile":"fixture"}`,
			SourcesJSON:      `{"provider_profile":"test"}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func canonicalCheckpointFixture(
	t *testing.T,
	fixture boundAgentRunFixture,
	binding WorktreeBinding,
	runtimeState checkpoint.RuntimeState,
	preservedRevision string,
) checkpoint.CanonicalState {
	t.Helper()
	var completed, pending []checkpoint.PlanStepSnapshot
	for _, step := range runtimeState.PlanSteps {
		switch step.State {
		case checkpoint.PlanStepImplemented,
			checkpoint.PlanStepValidated,
			checkpoint.PlanStepSkipped:
			completed = append(completed, step)
		default:
			pending = append(pending, step)
		}
	}
	value, err := checkpoint.Canonicalize(checkpoint.Snapshot{
		SchemaVersion:            checkpoint.SchemaVersion,
		TaskID:                   fixture.task.ID,
		RunID:                    fixture.runID,
		RepositoryID:             fixture.task.RepositoryID,
		WorktreeBindingRevision:  binding.Revision,
		PlanRevision:             runtimeState.PlanRevision,
		BaseRevision:             binding.BaseRevision,
		WorktreeHead:             binding.HeadRevision,
		PreservedRevision:        preservedRevision,
		DirtyFiles:               nil,
		DiffSHA256:               strings.Repeat("6", 64),
		CompletedPlanSteps:       completed,
		PendingPlanSteps:         pending,
		Budget:                   runtimeState.Budget,
		Policy:                   runtimeState.Policy,
		Provider:                 runtimeState.Provider,
		Tools:                    runtimeState.Tools,
		LastDurableEventSequence: runtimeState.LastDurableEventSequence,
		ExternalOutcomeAmbiguous: len(runtimeState.AmbiguousExternalActions) != 0,
		AmbiguousExternalActions: runtimeState.AmbiguousExternalActions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func atomicCheckpointFixture(
	id domain.CheckpointID,
	fixture boundAgentRunFixture,
	binding WorktreeBinding,
	runtimeState checkpoint.RuntimeState,
	state checkpoint.CanonicalState,
	key string,
) checkpoint.AtomicCommit {
	return checkpoint.AtomicCommit{
		CheckpointID:             id,
		TaskID:                   fixture.task.ID,
		RunID:                    fixture.runID,
		Trigger:                  checkpoint.TriggerUserPaused,
		SchemaVersion:            checkpoint.SchemaVersion,
		StateJSON:                state.JSON,
		StateSHA256:              state.StateSHA256,
		CaptureRequestSHA256:     strings.Repeat("5", 64),
		PreservedRevision:        state.Snapshot.PreservedRevision,
		PreservedRef:             "refs/codeflux/checkpoints/" + id.String(),
		ExpectedEventSequence:    runtimeState.LastDurableEventSequence,
		ExpectedBudgetRevision:   runtimeState.Budget.SnapshotRevision,
		ExpectedWorktreeRevision: binding.Revision,
		EventPayloadJSON:         `{"schema_version":1}`,
		IdempotencyKey:           key,
	}
}
