package checkpoint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/redact"
)

func TestCaptureBindsCompleteCanonicalRecoveryState(t *testing.T) {
	fixture := newCaptureFixture(t)
	fixture.worktree.state.DirtyFiles = []DirtyFileHash{
		{Path: "z.go", Exists: true, SHA256: strings.Repeat("2", 64)},
		{Path: "a.go", Exists: true, SHA256: strings.Repeat("1", 64)},
		{Path: "deleted.go"},
	}
	fixture.runtime.state.PlanSteps = []PlanStepSnapshot{
		{ID: "validate", State: PlanStepPending},
		{ID: "edit", State: PlanStepImplemented},
	}
	command := fixture.commandFor(t, TriggerMaterialEditApplied)
	fixture.runtime.state.AmbiguousExternalActions = []AmbiguousExternalAction{
		{
			ActionID: "external-2", Kind: "external-write",
			IntentSHA256: strings.Repeat("d", 64),
		},
		{
			ActionID: "external-1", Kind: "provider-request",
			IntentSHA256:  strings.Repeat("c", 64),
			ToolRequestID: "tool-request-1",
		},
	}

	result, err := fixture.service.Capture(t.Context(), command)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.Replayed ||
		result.Checkpoint.CheckpointEventSequence !=
			fixture.runtime.state.LastDurableEventSequence+1 ||
		len(fixture.store.commits) != 1 {
		t.Fatalf("capture result = %#v", result)
	}
	state := result.State.Snapshot
	if state.SchemaVersion != SchemaVersion ||
		state.TaskID != command.TaskID ||
		state.RunID != command.RunID ||
		state.RepositoryID != fixture.worktree.state.RepositoryID ||
		state.PlanRevision != command.ExpectedPlanRevision ||
		state.BaseRevision != fixture.worktree.state.BaseRevision ||
		state.WorktreeHead != fixture.worktree.state.HeadRevision ||
		state.DiffSHA256 != fixture.worktree.state.DiffSHA256 ||
		state.Policy != fixture.runtime.state.Policy ||
		state.Tools != fixture.runtime.state.Tools ||
		!state.ExternalOutcomeAmbiguous ||
		len(state.AmbiguousExternalActions) != 2 {
		t.Fatalf("checkpoint state = %#v", state)
	}
	if state.DirtyFiles[0].Path != "a.go" ||
		state.DirtyFiles[1].Path != "deleted.go" ||
		state.DirtyFiles[2].Path != "z.go" ||
		len(state.CompletedPlanSteps) != 1 ||
		state.CompletedPlanSteps[0].ID != "edit" ||
		len(state.PendingPlanSteps) != 1 ||
		state.PendingPlanSteps[0].ID != "validate" ||
		state.AmbiguousExternalActions[0].ActionID != "external-1" {
		t.Fatalf("canonical ordering/state = %#v", state)
	}
	commit := fixture.store.commits[0]
	if commit.ExpectedEventSequence !=
		fixture.runtime.state.LastDurableEventSequence ||
		commit.ExpectedBudgetRevision !=
			fixture.runtime.state.Budget.SnapshotRevision ||
		commit.ExpectedWorktreeRevision !=
			fixture.worktree.state.WorktreeBindingRevision ||
		commit.StateSHA256 != result.State.StateSHA256 ||
		!strings.Contains(commit.EventPayloadJSON, result.State.StateSHA256) {
		t.Fatalf("atomic commit = %#v", commit)
	}
	for _, forbidden := range []string{
		"credential", "secret-value", "process_handle",
		"environment", "stream_handle",
	} {
		if strings.Contains(strings.ToLower(result.State.JSON), forbidden) {
			t.Fatalf(
				"checkpoint serialized forbidden field/value %q: %s",
				forbidden,
				result.State.JSON,
			)
		}
	}
}

func TestCheckpointCanonicalStateNormalizesListsAndRejectsNewerSchema(
	t *testing.T,
) {
	fixture := newCaptureFixture(t)
	fixture.worktree.state.DirtyFiles = nil
	command := fixture.commandFor(t, TriggerUserPaused)
	result, err := fixture.service.Capture(t.Context(), command)
	if err != nil {
		t.Fatal(err)
	}
	emptyLists := result.State.Snapshot
	emptyLists.DirtyFiles = []DirtyFileHash{}
	emptyLists.CompletedPlanSteps = []PlanStepSnapshot{}
	emptyLists.AmbiguousExternalActions = []AmbiguousExternalAction{}
	normalized, err := Canonicalize(emptyLists)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.JSON != result.State.JSON ||
		normalized.StateSHA256 != result.State.StateSHA256 {
		t.Fatal("nil and empty checkpoint lists produced different identities")
	}
	invalidProvider := result.State.Snapshot
	invalidProvider.Provider.SettingsRevision = 0
	if _, err := Canonicalize(invalidProvider); err == nil {
		t.Fatal("zero settings revision was accepted in provider binding")
	}
	newer := strings.Replace(
		result.State.JSON,
		`"schema_version":1`,
		`"schema_version":2`,
		1,
	)
	digest := sha256.Sum256([]byte(newer))
	if _, err := DecodeCanonicalState(
		newer,
		hex.EncodeToString(digest[:]),
	); err == nil {
		t.Fatal("newer checkpoint schema unexpectedly loaded")
	}
}

func TestCaptureDeduplicatesIdenticalStateAndIdempotentRetry(t *testing.T) {
	t.Run("identical state under another request", func(t *testing.T) {
		fixture := newCaptureFixture(t)
		first, err := fixture.service.Capture(
			t.Context(),
			fixture.commandFor(t, TriggerUserPaused),
		)
		if err != nil {
			t.Fatal(err)
		}
		secondCommand := fixture.commandFor(t, TriggerUserPaused)
		second, err := fixture.service.Capture(t.Context(), secondCommand)
		if err != nil {
			t.Fatal(err)
		}
		if !first.Created || second.Created || !second.Replayed ||
			first.Checkpoint.ID != second.Checkpoint.ID ||
			fixture.store.eventCount != 1 ||
			fixture.worktree.removals != 1 {
			t.Fatalf("first=%#v second=%#v", first, second)
		}
	})

	t.Run("commit response lost", func(t *testing.T) {
		fixture := newCaptureFixture(t)
		fixture.store.failAfterCommitOnce = true
		command := fixture.commandFor(t, TriggerUserPaused)
		recovered, err := fixture.service.Capture(t.Context(), command)
		if err != nil {
			t.Fatal(err)
		}
		if fixture.store.eventCount != 1 {
			t.Fatalf("events after lost response = %d", fixture.store.eventCount)
		}
		retry, err := fixture.service.Capture(t.Context(), command)
		if err != nil {
			t.Fatal(err)
		}
		if !recovered.Replayed || recovered.Created ||
			!retry.Replayed || retry.Created ||
			fixture.store.eventCount != 1 ||
			fixture.worktree.calls != 1 ||
			fixture.worktree.removals != 0 ||
			fixture.runtime.calls != 1 {
			t.Fatalf(
				"retry=%#v events=%d worktree-calls=%d runtime-calls=%d",
				retry,
				fixture.store.eventCount,
				fixture.worktree.calls,
				fixture.runtime.calls,
			)
		}
	})
}

func TestCaptureEveryRequiredTriggerAcrossAtomicFailureBoundaries(t *testing.T) {
	triggers := []Trigger{
		TriggerPlanApproved,
		TriggerMaterialEditApplied,
		TriggerBeforeRiskyAction,
		TriggerValidationSucceeded,
		TriggerUserPaused,
		TriggerGracefulShutdown,
	}
	for _, trigger := range triggers {
		t.Run(string(trigger)+"/before-commit", func(t *testing.T) {
			fixture := newCaptureFixture(t)
			fixture.store.failBeforeCommit = true
			_, err := fixture.service.Capture(
				t.Context(),
				fixture.commandFor(t, trigger),
			)
			if err == nil ||
				len(fixture.store.byKey) != 0 ||
				fixture.store.eventCount != 0 {
				t.Fatalf(
					"error=%v records=%d events=%d",
					err,
					len(fixture.store.byKey),
					fixture.store.eventCount,
				)
			}
		})
		t.Run(string(trigger)+"/after-commit", func(t *testing.T) {
			fixture := newCaptureFixture(t)
			fixture.store.failAfterCommitOnce = true
			command := fixture.commandFor(t, trigger)
			recovered, err := fixture.service.Capture(t.Context(), command)
			if err != nil {
				t.Fatal(err)
			}
			retry, err := fixture.service.Capture(t.Context(), command)
			if err != nil {
				t.Fatal(err)
			}
			if !recovered.Replayed || !retry.Replayed ||
				fixture.store.eventCount != 1 ||
				len(fixture.store.byKey) != 1 {
				t.Fatalf(
					"retry=%#v records=%d events=%d",
					retry,
					len(fixture.store.byKey),
					fixture.store.eventCount,
				)
			}
		})
	}
}

func TestCaptureGracefulShutdownIsBounded(t *testing.T) {
	fixture := newCaptureFixture(t)
	fixture.worktree.block = true
	command := fixture.commandFor(t, TriggerUserPaused)
	started := time.Now()
	_, err := fixture.service.CaptureGracefulShutdown(
		t.Context(),
		command,
		10*time.Millisecond,
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("bounded graceful checkpoint took %s", elapsed)
	}
}

func TestCaptureRejectsInvalidBoundaryBeforeReadingState(t *testing.T) {
	fixture := newCaptureFixture(t)
	command := fixture.commandFor(t, TriggerBeforeRiskyAction)
	command.Attribution.ActionSHA256 = ""
	if _, err := fixture.service.Capture(t.Context(), command); err == nil {
		t.Fatal("invalid risky boundary unexpectedly captured")
	}
	if fixture.worktree.calls != 0 || fixture.runtime.calls != 0 ||
		len(fixture.store.commits) != 0 {
		t.Fatalf(
			"worktree=%d runtime=%d commits=%d",
			fixture.worktree.calls,
			fixture.runtime.calls,
			len(fixture.store.commits),
		)
	}
}

func TestCaptureRejectsCredentialMaterialBeforeAtomicPersistence(t *testing.T) {
	fixture := newCaptureFixture(t)
	const credential = "sk-proj-ABCDEFGHIJKLMNOPQRSTUVWX"
	secret, err := credentials.NewSecret([]byte(credential))
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := redact.NewPipeline(
		[]credentials.Secret{secret},
		redact.Limits{
			MaximumInputBytes:  1 << 20,
			MaximumOutputBytes: 1 << 20,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Close()
	guard, err := NewRedactionSecretGuard(pipeline)
	if err != nil {
		t.Fatal(err)
	}
	fixture.service, err = NewService(
		fixture.worktree,
		fixture.runtime,
		fixture.store,
		guard,
	)
	if err != nil {
		t.Fatal(err)
	}
	command := fixture.commandFor(t, TriggerUserPaused)
	fixture.runtime.state.Policy.Version = credential
	if _, err := fixture.service.Capture(t.Context(), command); err == nil {
		t.Fatal("credential-bearing checkpoint unexpectedly persisted")
	}
	if len(fixture.store.commits) != 0 {
		t.Fatalf("atomic commits = %d, want zero", len(fixture.store.commits))
	}

	legacy := newCaptureFixture(t)
	legacyCommand := legacy.commandFor(t, TriggerUserPaused)
	legacy.runtime.state.Policy.Version = credential
	if _, err := legacy.service.Capture(
		t.Context(),
		legacyCommand,
	); err != nil {
		t.Fatal(err)
	}
	legacy.service, err = NewService(
		legacy.worktree,
		legacy.runtime,
		legacy.store,
		guard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.service.Capture(
		t.Context(),
		legacyCommand,
	); err == nil {
		t.Fatal("credential-bearing persisted replay unexpectedly returned")
	}
}

type captureFixture struct {
	service  *Service
	worktree *worktreeStateStub
	runtime  *runtimeStateStub
	store    *atomicStoreStub
	taskID   domain.TaskID
	runID    domain.RunID
}

func newCaptureFixture(t *testing.T) *captureFixture {
	t.Helper()
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	budgetID, err := domain.NewBudgetID()
	if err != nil {
		t.Fatal(err)
	}
	worktree := &worktreeStateStub{state: WorktreeState{
		RepositoryID: repositoryID, WorktreeBindingRevision: 4,
		BaseRevision:      strings.Repeat("a", 40),
		HeadRevision:      strings.Repeat("b", 40),
		PreservedRevision: strings.Repeat("9", 40),
		DiffSHA256:        strings.Repeat("c", 64),
		DirtyFiles: []DirtyFileHash{{
			Path: "main.go", Exists: true, SHA256: strings.Repeat("d", 64),
		}},
	}}
	currency, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	cost := ExactCost{Currency: currency, Numerator: 0, Denominator: 1}
	runtime := &runtimeStateStub{state: RuntimeState{
		PlanRevision: 3, PolicyRevision: 5,
		PlanSteps: []PlanStepSnapshot{{
			ID: "step-001", State: PlanStepPending,
		}},
		Budget: BudgetPosition{
			BudgetID: budgetID, SnapshotRevision: 7, LimitRevision: 2,
			ReservedCost: cost, ChargedCost: cost, ActualKnownCost: cost,
		},
		Policy: PolicyBinding{
			Revision: 5, Version: "fixed-policy-v1",
			ContentSHA256: strings.Repeat("e", 64),
		},
		Provider: ProviderBinding{
			SettingsRevision:       3,
			RunConfigurationSHA256: strings.Repeat("1", 64),
			Adapter:                "fixture-adapter",
			AdapterVersion:         "v1",
			Provider:               "fixture-provider",
			ProviderVersion:        "v1",
			Model:                  "fixture-model",
			ModelRevision:          "revision-1",
		},
		Tools: ToolBinding{
			SchemaVersion: 1, CatalogSHA256: strings.Repeat("f", 64),
		},
		LastDurableEventSequence: 12,
	}}
	store := newAtomicStoreStub()
	service, err := NewService(worktree, runtime, store, noSecretGuard{})
	if err != nil {
		t.Fatal(err)
	}
	return &captureFixture{
		service: service, worktree: worktree, runtime: runtime, store: store,
		taskID: taskID, runID: runID,
	}
}

func (fixture *captureFixture) commandFor(
	t *testing.T,
	trigger Trigger,
) CaptureCommand {
	t.Helper()
	checkpointID, err := domain.NewCheckpointID()
	if err != nil {
		t.Fatal(err)
	}
	command := CaptureCommand{
		CheckpointID:         checkpointID,
		TaskID:               fixture.taskID,
		RunID:                fixture.runID,
		ExpectedPlanRevision: fixture.runtime.state.PlanRevision,
		Trigger:              trigger,
		IdempotencyKey:       checkpointID.String(),
	}
	switch trigger {
	case TriggerPlanApproved:
		approvalID, err := domain.NewApprovalID()
		if err != nil {
			t.Fatal(err)
		}
		command.Attribution.ApprovalID = &approvalID
	case TriggerMaterialEditApplied:
		command.Attribution.ToolRequestID = "tool-request-1"
	case TriggerBeforeRiskyAction:
		command.Attribution.PermissionDecisionID = "permission-decision-1"
		command.Attribution.ActionSHA256 = strings.Repeat("1", 64)
	case TriggerValidationSucceeded:
		validationID, err := domain.NewValidationID()
		if err != nil {
			t.Fatal(err)
		}
		command.Attribution.ValidationID = &validationID
	}
	return command
}

type worktreeStateStub struct {
	state    WorktreeState
	err      error
	block    bool
	calls    int
	removals int
}

func (stub *worktreeStateStub) CaptureCheckpointWorktreeState(
	ctx context.Context,
	_ domain.TaskID,
	checkpointID domain.CheckpointID,
) (WorktreeState, error) {
	stub.calls++
	if stub.block {
		<-ctx.Done()
		return WorktreeState{}, ctx.Err()
	}
	value := stub.state
	value.PreservedRef = "refs/codeflux/checkpoints/" + checkpointID.String()
	return value, stub.err
}

func (stub *worktreeStateStub) RemoveCheckpointWorktreePreservation(
	context.Context,
	domain.TaskID,
	domain.CheckpointID,
	string,
	string,
) error {
	stub.removals++
	return nil
}

type runtimeStateStub struct {
	state RuntimeState
	err   error
	calls int
}

func (stub *runtimeStateStub) ReadCheckpointRuntimeState(
	context.Context,
	domain.TaskID,
	domain.RunID,
) (RuntimeState, error) {
	stub.calls++
	return stub.state, stub.err
}

type atomicStoreStub struct {
	byKey               map[string]PersistedCheckpoint
	byState             map[string]PersistedCheckpoint
	commits             []AtomicCommit
	eventCount          int
	failBeforeCommit    bool
	failAfterCommitOnce bool
}

type noSecretGuard struct{}

func (noSecretGuard) EnsureCheckpointSecretFree(string) error {
	return nil
}

func newAtomicStoreStub() *atomicStoreStub {
	return &atomicStoreStub{
		byKey:   make(map[string]PersistedCheckpoint),
		byState: make(map[string]PersistedCheckpoint),
	}
}

func (store *atomicStoreStub) FindCheckpointByIdempotency(
	_ context.Context,
	taskID domain.TaskID,
	key string,
) (PersistedCheckpoint, bool, error) {
	value, found := store.byKey[taskID.String()+"\x00"+key]
	return value, found, nil
}

func (store *atomicStoreStub) CommitCheckpointAndEvent(
	_ context.Context,
	input AtomicCommit,
) (PersistedCheckpoint, bool, error) {
	store.commits = append(store.commits, input)
	if store.failBeforeCommit {
		return PersistedCheckpoint{}, false, errors.New("injected pre-commit failure")
	}
	key := input.TaskID.String() + "\x00" + input.IdempotencyKey
	if existing, found := store.byKey[key]; found {
		return existing, false, nil
	}
	stateKey := input.TaskID.String() + "\x00" +
		input.RunID.String() + "\x00" + input.StateSHA256
	if existing, found := store.byState[stateKey]; found {
		existing.CaptureRequestSHA256 = input.CaptureRequestSHA256
		existing.IdempotencyKey = input.IdempotencyKey
		store.byKey[key] = existing
		return existing, false, nil
	}
	store.eventCount++
	value := PersistedCheckpoint{
		ID:                      input.CheckpointID,
		TaskID:                  input.TaskID,
		RunID:                   input.RunID,
		SchemaVersion:           input.SchemaVersion,
		StateJSON:               input.StateJSON,
		StateSHA256:             input.StateSHA256,
		CaptureRequestSHA256:    input.CaptureRequestSHA256,
		CheckpointEventSequence: input.ExpectedEventSequence + 1,
		PreservedRevision:       input.PreservedRevision,
		PreservedRef:            input.PreservedRef,
		IdempotencyKey:          input.IdempotencyKey,
	}
	store.byKey[key] = value
	store.byState[stateKey] = value
	if store.failAfterCommitOnce {
		store.failAfterCommitOnce = false
		return PersistedCheckpoint{}, false, errors.New(
			"injected lost commit response",
		)
	}
	return value, true, nil
}
