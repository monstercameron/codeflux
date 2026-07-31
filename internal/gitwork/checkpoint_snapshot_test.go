package gitwork

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

func TestCheckpointSnapshotPreservesHeadIndexBranchAndDirtyTree(t *testing.T) {
	service, _, _, taskID, binding := createWorktreeFixture(t, 8000)
	writeFile(
		t,
		filepath.Join(binding.WorktreePath, "main.go"),
		"package main\n\nconst Staged = true\n",
	)
	runGit(t, binding.WorktreePath, "add", "main.go")
	writeFile(
		t,
		filepath.Join(binding.WorktreePath, "main.go"),
		"package main\n\nconst Working = true\n",
	)
	binary := []byte{0, 1, 2, 3, 0xff}
	if err := os.WriteFile(
		filepath.Join(binding.WorktreePath, "binary.dat"),
		binary,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	headBefore := runGit(t, binding.WorktreePath, "rev-parse", "HEAD")
	branchBefore := runGit(
		t,
		binding.WorktreePath,
		"symbolic-ref",
		"--short",
		"HEAD",
	)
	indexBefore := runGit(
		t,
		binding.WorktreePath,
		"diff",
		"--cached",
		"--binary",
	)
	statusBefore := gitBytes(
		t,
		binding.WorktreePath,
		"status",
		"--porcelain=v1",
		"-z",
	)
	mainBefore, err := os.ReadFile(
		filepath.Join(binding.WorktreePath, "main.go"),
	)
	if err != nil {
		t.Fatal(err)
	}

	firstID := fixtureCheckpointID(t, 8001)
	first, err := service.CaptureCheckpointWorktreeState(
		t.Context(),
		taskID,
		firstID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.PreservedRef !=
		checkpointReferencePrefix+firstID.String() ||
		runGit(t, binding.WorktreePath, "rev-parse", "HEAD") !=
			headBefore ||
		runGit(
			t,
			binding.WorktreePath,
			"symbolic-ref",
			"--short",
			"HEAD",
		) != branchBefore ||
		runGit(
			t,
			binding.WorktreePath,
			"diff",
			"--cached",
			"--binary",
		) != indexBefore ||
		!bytes.Equal(
			gitBytes(
				t,
				binding.WorktreePath,
				"status",
				"--porcelain=v1",
				"-z",
			),
			statusBefore,
		) {
		t.Fatalf("checkpoint capture changed Git state: %#v", first)
	}
	mainAfter, err := os.ReadFile(
		filepath.Join(binding.WorktreePath, "main.go"),
	)
	if err != nil ||
		!bytes.Equal(mainAfter, mainBefore) {
		t.Fatalf("working file changed = %q, %v", mainAfter, err)
	}
	if got := runGit(
		t,
		binding.WorktreePath,
		"show",
		first.PreservedRevision+":main.go",
	); got != strings.TrimSpace(string(mainBefore)) {
		t.Fatalf("preserved main.go = %q", got)
	}
	preservedBinary := gitBytes(
		t,
		binding.WorktreePath,
		"show",
		first.PreservedRevision+":binary.dat",
	)
	if !bytes.Equal(preservedBinary, binary) {
		t.Fatalf("preserved binary = %v", preservedBinary)
	}

	secondID := fixtureCheckpointID(t, 8002)
	second, err := service.CaptureCheckpointWorktreeState(
		t.Context(),
		taskID,
		secondID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if second.PreservedRevision != first.PreservedRevision {
		t.Fatalf(
			"identical state produced revisions %q and %q",
			first.PreservedRevision,
			second.PreservedRevision,
		)
	}
	if err := service.RemoveCheckpointWorktreePreservation(
		t.Context(),
		taskID,
		secondID,
		second.PreservedRef,
		second.PreservedRevision,
	); err != nil {
		t.Fatal(err)
	}
	if checkpointRefExists(
		t,
		binding.WorktreePath,
		second.PreservedRef,
	) {
		t.Fatal("deduplicated checkpoint ref was retained")
	}
}

func TestCheckpointDatabaseFailureCleansPreservedRef(t *testing.T) {
	service, _, _, taskID, binding := createWorktreeFixture(t, 8010)
	writeFile(
		t,
		filepath.Join(binding.WorktreePath, "main.go"),
		"package main\n\nconst Pending = true\n",
	)
	repository := newMemoryCheckpointStore()
	repository.createErr = errors.New("injected database failure")
	service.SetCheckpointRepository(repository)
	checkpointID := fixtureCheckpointID(t, 8011)
	if _, err := service.CreateCheckpoint(
		t.Context(),
		CreateCheckpointInput{
			ID: checkpointID, TaskID: taskID,
			IdempotencyKey: "checkpoint-database-failure",
		},
	); err == nil {
		t.Fatal("database failure unexpectedly succeeded")
	}
	reference := checkpointReferencePrefix + checkpointID.String()
	if checkpointRefExists(t, binding.WorktreePath, reference) {
		t.Fatal("database failure retained checkpoint ref")
	}
	if runGit(t, binding.WorktreePath, "rev-parse", "HEAD") !=
		binding.HeadRevision {
		t.Fatal("database failure moved HEAD")
	}
	if !strings.Contains(
		runGit(t, binding.WorktreePath, "status", "--porcelain=v1"),
		"main.go",
	) {
		t.Fatal("database failure cleaned the dirty worktree")
	}
}

func TestCheckpointCaptureKeepsCommittedRefAndCleansStateAliasRef(
	t *testing.T,
) {
	service, _, _, taskID, binding := createWorktreeFixture(t, 8015)
	writeFile(
		t,
		filepath.Join(binding.WorktreePath, "main.go"),
		"package main\n\nconst Boundary = true\n",
	)
	runID, _ := domain.NewRunID()
	budgetID, _ := domain.NewBudgetID()
	currency, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	cost := checkpoint.ExactCost{
		Currency: currency, Denominator: 1,
	}
	runtime := &checkpointRuntimeStub{state: checkpoint.RuntimeState{
		PlanRevision: 1, PolicyRevision: 1,
		PlanSteps: []checkpoint.PlanStepSnapshot{{
			ID: "step-001", State: checkpoint.PlanStepPending,
		}},
		Budget: checkpoint.BudgetPosition{
			BudgetID:     budgetID,
			ReservedCost: cost, ChargedCost: cost, ActualKnownCost: cost,
		},
		Policy: checkpoint.PolicyBinding{
			Revision: 1, Version: "fixed-policy-v1",
			ContentSHA256: strings.Repeat("a", 64),
		},
		Provider: checkpoint.ProviderBinding{
			SettingsRevision:       1,
			RunConfigurationSHA256: strings.Repeat("c", 64),
			Adapter:                "fixture-adapter", AdapterVersion: "v1",
			Provider: "fixture-provider", ProviderVersion: "v1",
			Model: "fixture-model", ModelRevision: "revision-1",
		},
		Tools: checkpoint.ToolBinding{
			SchemaVersion: 1,
			CatalogSHA256: strings.Repeat("b", 64),
		},
	}}
	store := newCheckpointAtomicStore()
	store.loseFirstResponse = true
	captureService, err := checkpoint.NewService(
		service,
		runtime,
		store,
		checkpointSecretGuardStub{},
	)
	if err != nil {
		t.Fatal(err)
	}
	firstID := fixtureCheckpointID(t, 8016)
	first, err := captureService.Capture(
		t.Context(),
		checkpoint.CaptureCommand{
			CheckpointID: firstID, TaskID: taskID, RunID: runID,
			ExpectedPlanRevision: 1,
			Trigger:              checkpoint.TriggerUserPaused,
			IdempotencyKey:       "checkpoint-lost-response",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Replayed || first.Created ||
		!checkpointRefExists(
			t,
			binding.WorktreePath,
			checkpointReferencePrefix+firstID.String(),
		) {
		t.Fatalf("lost-response recovery = %#v", first)
	}
	secondID := fixtureCheckpointID(t, 8017)
	second, err := captureService.Capture(
		t.Context(),
		checkpoint.CaptureCommand{
			CheckpointID: secondID, TaskID: taskID, RunID: runID,
			ExpectedPlanRevision: 1,
			Trigger:              checkpoint.TriggerUserPaused,
			IdempotencyKey:       "checkpoint-state-alias",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || second.Created ||
		second.Checkpoint.ID != firstID ||
		checkpointRefExists(
			t,
			binding.WorktreePath,
			checkpointReferencePrefix+secondID.String(),
		) {
		t.Fatalf("state alias recovery = %#v", second)
	}
}

func TestCheckpointPatchExportSurvivesMissingWorktree(t *testing.T) {
	service, _, repositoryPath, taskID, binding :=
		createWorktreeFixture(t, 8020)
	writeFile(
		t,
		filepath.Join(binding.WorktreePath, "main.go"),
		"package main\n\nconst Preserved = true\n",
	)
	binary := []byte{0, 1, 2, 3, 0xff}
	if err := os.WriteFile(
		filepath.Join(binding.WorktreePath, "binary.dat"),
		binary,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	checkpointID := fixtureCheckpointID(t, 8021)
	worktreeState, err := service.CaptureCheckpointWorktreeState(
		t.Context(),
		taskID,
		checkpointID,
	)
	if err != nil {
		t.Fatal(err)
	}
	persisted := checkpointSnapshotFixture(
		t,
		checkpointID,
		taskID,
		worktreeState,
	)
	snapshots := &checkpointSnapshotStore{
		persisted: persisted,
		repository: storage.Repository{
			ID: worktreeState.RepositoryID, CanonicalPath: repositoryPath,
		},
	}
	service.SetCheckpointRepository(snapshots)
	if _, err := (ExecRunner{}).Run(
		t.Context(),
		repositoryPath,
		"git",
		"worktree",
		"remove",
		"--force",
		binding.WorktreePath,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(binding.WorktreePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("task worktree still exists: %v", err)
	}
	path, available, err := service.PreserveCheckpointPatch(
		t.Context(),
		checkpointID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !available || path == "" {
		t.Fatalf("patch export = %q, %t", path, available)
	}
	patch, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(patch, []byte("const Preserved = true")) ||
		!bytes.Contains(patch, []byte("GIT binary patch")) {
		t.Fatalf("preserved patch lacks changes:\n%s", patch)
	}
}

func TestObserveRecoveryWorktreeReportsDivergenceWithoutMutation(t *testing.T) {
	service, _, _, taskID, binding := createWorktreeFixture(t, 8030)
	writeFile(
		t,
		filepath.Join(binding.WorktreePath, "main.go"),
		"package main\n\nconst ExternalCommit = true\n",
	)
	runGit(t, binding.WorktreePath, "add", "main.go")
	runGit(
		t,
		binding.WorktreePath,
		"-c",
		"user.name=Recovery Fixture",
		"-c",
		"user.email=recovery@codeflux.invalid",
		"commit",
		"-m",
		"external commit",
	)
	externalHead := strings.TrimSpace(
		runGit(t, binding.WorktreePath, "rev-parse", "HEAD"),
	)
	writeFile(
		t,
		filepath.Join(binding.WorktreePath, "main.go"),
		"package main\n\nconst ExternalCommit = false\n",
	)
	mergeHeadPath := strings.TrimSpace(
		runGit(
			t,
			binding.WorktreePath,
			"rev-parse",
			"--git-path",
			"MERGE_HEAD",
		),
	)
	if !filepath.IsAbs(mergeHeadPath) {
		mergeHeadPath = filepath.Join(binding.WorktreePath, mergeHeadPath)
	}
	writeFile(t, mergeHeadPath, binding.HeadRevision+"\n")
	statusBefore := runGit(
		t,
		binding.WorktreePath,
		"status",
		"--porcelain=v1",
	)
	indexBefore := runGit(t, binding.WorktreePath, "diff", "--cached")

	facts, err := service.ObserveRecoveryWorktree(
		t.Context(),
		binding,
		checkpoint.Snapshot{
			TaskID: taskID, RepositoryID: binding.RepositoryID,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !facts.Exists || !facts.Owned ||
		facts.RepositoryID != binding.RepositoryID ||
		facts.BindingRevision != binding.Revision ||
		facts.HeadRevision != externalHead ||
		facts.HeadRevision == binding.HeadRevision ||
		len(facts.DirtyFiles) != 1 ||
		facts.DirtyFiles[0].Path != "main.go" ||
		facts.DiffSHA256 == "" ||
		!slices.Equal(facts.UnresolvedGitOperations, []string{"merge"}) {
		t.Fatalf("divergent recovery facts = %#v", facts)
	}
	if got := strings.TrimSpace(
		runGit(t, binding.WorktreePath, "rev-parse", "HEAD"),
	); got != externalHead {
		t.Fatalf("recovery observation moved HEAD to %q", got)
	}
	if got := runGit(
		t,
		binding.WorktreePath,
		"status",
		"--porcelain=v1",
	); got != statusBefore {
		t.Fatalf("recovery observation changed status: %q / %q", got, statusBefore)
	}
	if got := runGit(t, binding.WorktreePath, "diff", "--cached"); got != indexBefore {
		t.Fatalf("recovery observation changed index: %q / %q", got, indexBefore)
	}
	if _, err := os.Stat(mergeHeadPath); err != nil {
		t.Fatalf("recovery observation removed merge state: %v", err)
	}
}

func TestObserveRecoveryWorktreeReportsMissingPath(t *testing.T) {
	service, _, repositoryPath, taskID, binding :=
		createWorktreeFixture(t, 8040)
	if _, err := (ExecRunner{}).Run(
		t.Context(),
		repositoryPath,
		"git",
		"worktree",
		"remove",
		"--force",
		binding.WorktreePath,
	); err != nil {
		t.Fatal(err)
	}
	facts, err := service.ObserveRecoveryWorktree(
		t.Context(),
		binding,
		checkpoint.Snapshot{
			TaskID: taskID, RepositoryID: binding.RepositoryID,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Exists || facts.Owned ||
		facts.RepositoryID != binding.RepositoryID ||
		facts.BindingRevision != binding.Revision {
		t.Fatalf("missing recovery worktree facts = %#v", facts)
	}
}

func checkpointSnapshotFixture(
	t *testing.T,
	checkpointID domain.CheckpointID,
	taskID domain.TaskID,
	worktree checkpoint.WorktreeState,
) checkpoint.PersistedCheckpoint {
	t.Helper()
	runID, _ := domain.NewRunID()
	budgetID, _ := domain.NewBudgetID()
	currency, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	cost := checkpoint.ExactCost{
		Currency: currency, Denominator: 1,
	}
	state, err := checkpoint.Canonicalize(checkpoint.Snapshot{
		SchemaVersion:           checkpoint.SchemaVersion,
		TaskID:                  taskID,
		RunID:                   runID,
		RepositoryID:            worktree.RepositoryID,
		WorktreeBindingRevision: worktree.WorktreeBindingRevision,
		PlanRevision:            1,
		BaseRevision:            worktree.BaseRevision,
		WorktreeHead:            worktree.HeadRevision,
		PreservedRevision:       worktree.PreservedRevision,
		DirtyFiles:              worktree.DirtyFiles,
		DiffSHA256:              worktree.DiffSHA256,
		PendingPlanSteps: []checkpoint.PlanStepSnapshot{{
			ID: "step-001", State: checkpoint.PlanStepPending,
		}},
		Budget: checkpoint.BudgetPosition{
			BudgetID:     budgetID,
			ReservedCost: cost, ChargedCost: cost, ActualKnownCost: cost,
		},
		Policy: checkpoint.PolicyBinding{
			Revision: 1, Version: "fixed-policy-v1",
			ContentSHA256: strings.Repeat("a", 64),
		},
		Provider: checkpoint.ProviderBinding{
			SettingsRevision:       1,
			RunConfigurationSHA256: strings.Repeat("c", 64),
			Adapter:                "fixture-adapter", AdapterVersion: "v1",
			Provider: "fixture-provider", ProviderVersion: "v1",
			Model: "fixture-model", ModelRevision: "revision-1",
		},
		Tools: checkpoint.ToolBinding{
			SchemaVersion: 1,
			CatalogSHA256: strings.Repeat("b", 64),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return checkpoint.PersistedCheckpoint{
		ID: checkpointID, TaskID: taskID, RunID: runID,
		SchemaVersion: checkpoint.SchemaVersion,
		StateJSON:     state.JSON, StateSHA256: state.StateSHA256,
		CaptureRequestSHA256:    strings.Repeat("c", 64),
		CheckpointEventSequence: 1,
		PreservedRevision:       worktree.PreservedRevision,
		PreservedRef:            worktree.PreservedRef,
		IdempotencyKey:          "checkpoint-patch-export",
	}
}

type checkpointSnapshotStore struct {
	persisted  checkpoint.PersistedCheckpoint
	repository storage.Repository
}

type checkpointRuntimeStub struct {
	state checkpoint.RuntimeState
}

func (stub *checkpointRuntimeStub) ReadCheckpointRuntimeState(
	context.Context,
	domain.TaskID,
	domain.RunID,
) (checkpoint.RuntimeState, error) {
	return stub.state, nil
}

type checkpointSecretGuardStub struct{}

func (checkpointSecretGuardStub) EnsureCheckpointSecretFree(string) error {
	return nil
}

type checkpointAtomicStore struct {
	byKey             map[string]checkpoint.PersistedCheckpoint
	byState           map[string]checkpoint.PersistedCheckpoint
	loseFirstResponse bool
}

func newCheckpointAtomicStore() *checkpointAtomicStore {
	return &checkpointAtomicStore{
		byKey:   make(map[string]checkpoint.PersistedCheckpoint),
		byState: make(map[string]checkpoint.PersistedCheckpoint),
	}
}

func (store *checkpointAtomicStore) FindCheckpointByIdempotency(
	_ context.Context,
	taskID domain.TaskID,
	key string,
) (checkpoint.PersistedCheckpoint, bool, error) {
	value, found := store.byKey[taskID.String()+"\x00"+key]
	return value, found, nil
}

func (store *checkpointAtomicStore) CommitCheckpointAndEvent(
	_ context.Context,
	input checkpoint.AtomicCommit,
) (checkpoint.PersistedCheckpoint, bool, error) {
	key := input.TaskID.String() + "\x00" + input.IdempotencyKey
	stateKey := input.TaskID.String() + "\x00" +
		input.RunID.String() + "\x00" + input.StateSHA256
	if existing, found := store.byState[stateKey]; found {
		existing.CaptureRequestSHA256 = input.CaptureRequestSHA256
		existing.IdempotencyKey = input.IdempotencyKey
		store.byKey[key] = existing
		return existing, false, nil
	}
	value := checkpoint.PersistedCheckpoint{
		ID: input.CheckpointID, TaskID: input.TaskID, RunID: input.RunID,
		SchemaVersion: input.SchemaVersion,
		StateJSON:     input.StateJSON, StateSHA256: input.StateSHA256,
		CaptureRequestSHA256:    input.CaptureRequestSHA256,
		CheckpointEventSequence: input.ExpectedEventSequence + 1,
		PreservedRevision:       input.PreservedRevision,
		PreservedRef:            input.PreservedRef,
		IdempotencyKey:          input.IdempotencyKey,
	}
	store.byKey[key] = value
	store.byState[stateKey] = value
	if store.loseFirstResponse {
		store.loseFirstResponse = false
		return checkpoint.PersistedCheckpoint{}, false,
			errors.New("injected lost database response")
	}
	return value, true, nil
}

func (store *checkpointSnapshotStore) CreateCheckpoint(
	context.Context,
	storage.CreateCheckpoint,
) (storage.Checkpoint, error) {
	return storage.Checkpoint{}, errors.New("not used")
}

func (store *checkpointSnapshotStore) GetCheckpoint(
	context.Context,
	domain.CheckpointID,
) (storage.Checkpoint, error) {
	return storage.Checkpoint{}, errors.New("not used")
}

func (store *checkpointSnapshotStore) LoadCheckpoint(
	context.Context,
	domain.CheckpointID,
) (checkpoint.PersistedCheckpoint, error) {
	return store.persisted, nil
}

func (store *checkpointSnapshotStore) GetRepository(
	context.Context,
	domain.RepositoryID,
) (storage.Repository, error) {
	return store.repository, nil
}

func gitBytes(
	t *testing.T,
	directory string,
	arguments ...string,
) []byte {
	t.Helper()
	result, err := (ExecRunner{}).Run(
		t.Context(),
		directory,
		"git",
		arguments...,
	)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, result.Stderr)
	}
	return result.Stdout
}

func checkpointRefExists(
	t *testing.T,
	directory string,
	reference string,
) bool {
	t.Helper()
	_, err := (ExecRunner{}).Run(
		t.Context(),
		directory,
		"git",
		"show-ref",
		"--verify",
		reference,
	)
	return err == nil
}
