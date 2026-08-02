package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TestPIPE002_ASecondWriteForAStageIsRefusedRatherThanDropped covers PIPE-002.
//
// Stage storage is first-write-wins. A second write for the same stage in the
// same attempt used to reach storage and be discarded there, so a run could
// compute a better verdict, send it, and leave the earlier one standing with
// nothing recording that it had happened.
func TestPIPE002_ASecondWriteForAStageIsRefusedRatherThanDropped(t *testing.T) {
	ledger := &pipelineLedger{
		recorded: map[pipeline.Number]bool{},
	}
	// A nil repository means no storage write is attempted, which is what this
	// test wants: the refusal is in the ledger, not in SQLite.
	ledger.record(context.Background(), pipeline.StageAdversarial,
		pipeline.StateBlocked, "first", nil)
	if len(ledger.duplicateWrites()) != 0 {
		t.Fatalf("a first write was reported as a duplicate: %+v", ledger.duplicateWrites())
	}

	// With no repository the first write never marks the stage recorded, so
	// drive the marker directly to model a run that did record it.
	ledger.recorded[pipeline.StageAdversarial] = true
	ledger.record(context.Background(), pipeline.StageAdversarial,
		pipeline.StateSatisfied, "second", nil)

	duplicates := ledger.duplicateWrites()
	if len(duplicates) != 1 {
		t.Fatalf("the second write was not caught: %+v", duplicates)
	}
	if duplicates[0].Stage != pipeline.StageAdversarial ||
		duplicates[0].State != pipeline.StateSatisfied {
		t.Errorf("the caught duplicate does not describe the refused write: %+v", duplicates[0])
	}
}

// TestPIPE001_AFailingRunKeepsItsRealAdversarialAndEvidenceVerdicts covers
// PIPE-001.
//
// The unverified-run sweep blocked five stages, three of which the run then
// computed anyway: the adversarial probe, the acceptance check, and the
// evidence bundle. First-write-wins meant the blocked row survived and the
// three real answers were thrown away, so a reader of a failing run's ledger
// saw "blocked" where the run had a verdict — and lost the evidence bundle,
// which is what is most worth reading when the news is bad.
func TestPIPE001_AFailingRunKeepsItsRealAdversarialAndEvidenceVerdicts(t *testing.T) {
	// A requirement whose program compiles but whose behaviour is wrong, so
	// verification fails while everything downstream still has something real
	// to say.
	const requirement = "Create cmd/wrong/main.go so the program prints the answer."
	const program = "package main\n\nimport \"fmt\"\n\n" +
		"func main() {\n\tfmt.Println(\"unverified\")\n}\n"

	engine := startEngineFixture(t, &scriptedEngineModel{
		turns: []func(agentloop.ModelInput) agentloop.ModelTurn{
			writeFile("cmd/wrong/main.go", program),
		},
	})
	ctx := context.Background()

	requestID := engine.request(t, requirement)
	created, err := engine.lifecycle.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 engine.threadID,
		RequestMessageID:         &requestID,
		Requirement:              requirement,
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       strings.Repeat("1", 40),
		BaselineModelRevision:    "scripted-provider-fixture",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"cmd/wrong"},
		IdempotencyKey:           "pipe001-requirement",
	})
	if err != nil {
		t.Fatalf("intake refused the requirement: %v", err)
	}
	readyRevision := driveTaskToReady(t, engine.repositories, created.TaskID, created.Revision)
	preflight, err := engine.application.TaskPreflightService().BindExecution(
		ctx, created.TaskID, readyRevision,
		ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"pipe001-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "pipe001-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}

	// Wait for the run to reach the end of its flow rather than for the first
	// row. The three stages under test are recorded late, and reading after
	// the first write would report them missing rather than blocked.
	engine.waitFor(t, "the run to reach its evidence bundle", func() bool {
		_, recorded := pipelineStageStates(t, engine, created.TaskID)[pipeline.StageEvidenceBundle]
		return recorded
	})

	// The evidence bundle must carry a real verdict rather than a blocked row.
	// This is the stage the old sweep most reliably destroyed.
	states := pipelineStageStates(t, engine, created.TaskID)
	for _, stage := range []pipeline.Number{
		pipeline.StageEvidenceBundle,
		pipeline.StageAdversarial,
		pipeline.StageEndToEndTests,
	} {
		state, recorded := states[stage]
		if !recorded {
			t.Errorf("stage %d was never recorded at all", stage)
			continue
		}
		if state == pipeline.StateBlocked {
			t.Errorf("stage %d is blocked; the run computes it and the pre-block "+
				"sweep discarded the real verdict", stage)
		}
	}
}

// pipelineStageStates reads the recorded state of every stage for a task,
// through the repository's own ordered read rather than a test-only query.
func pipelineStageStates(
	t *testing.T,
	engine engineFixture,
	taskID domain.TaskID,
) map[pipeline.Number]pipeline.State {
	t.Helper()
	records, err := engine.repositories.ListPipelineStages(
		context.Background(), taskID, 1)
	if err != nil {
		t.Fatalf("reading the pipeline ledger: %v", err)
	}
	states := make(map[pipeline.Number]pipeline.State, len(records))
	for _, record := range records {
		states[record.Stage] = record.State
	}
	return states
}

// TestPIPE003_ASecondRunRecordsItsOwnAttempt covers PIPE-003.
//
// The attempt number was pinned to one in newPipelineLedger, in the start
// command, and in assembleEvidence. A task started a second time therefore
// wrote every stage under attempt one, where the first run's rows already sat,
// and ON CONFLICT DO NOTHING dropped the new run's whole record. The interface
// then showed the first run's ledger under the newer run's identity.
func TestPIPE003_ASecondRunRecordsItsOwnAttempt(t *testing.T) {
	repositories := pipelineAttemptRepositories(t)
	taskID, runID := pipelineAttemptScope(t, repositories)

	_ = runID

	first := newPipelineLedger(context.Background(), repositories, taskID, runID)
	if first.currentAttempt() != 1 {
		t.Fatalf("a task with no history started at attempt %d, want 1",
			first.currentAttempt())
	}
	// The stage row is written through the repository rather than the ledger,
	// because the ledger binds a run identity and this task has no run row.
	// What is under test is the attempt the next ledger computes from history.
	recordAttemptStage(t, repositories, taskID, 1, "first attempt")

	second := newPipelineLedger(context.Background(), repositories, taskID, runID)
	if second.currentAttempt() != 2 {
		t.Fatalf("a second run recorded attempt %d, want 2; its ledger would "+
			"collide with the first run's rows", second.currentAttempt())
	}
	recordAttemptStage(t, repositories, taskID, second.currentAttempt(), "second attempt")

	third := newPipelineLedger(context.Background(), repositories, taskID, runID)
	if third.currentAttempt() != 3 {
		t.Errorf("a third run recorded attempt %d, want 3", third.currentAttempt())
	}

	// Both attempts survive, and each carries its own detail.
	for attempt, wanted := range map[uint64]string{1: "first attempt", 2: "second attempt"} {
		records, err := repositories.ListPipelineStages(
			context.Background(), taskID, attempt)
		if err != nil {
			t.Fatalf("reading attempt %d: %v", attempt, err)
		}
		if len(records) == 0 {
			t.Fatalf("attempt %d recorded nothing", attempt)
		}
		if records[0].DetailRedacted != wanted {
			t.Errorf("attempt %d says %q, want %q",
				attempt, records[0].DetailRedacted, wanted)
		}
	}
}

// recordAttemptStage writes one stage row for a task attempt.
func recordAttemptStage(
	t *testing.T,
	repositories *storage.Repositories,
	taskID domain.TaskID,
	attempt uint64,
	detail string,
) {
	t.Helper()
	if _, err := repositories.RecordPipelineStageResult(
		t.Context(),
		storage.RecordPipelineStage{
			TaskID: taskID, Attempt: attempt,
			Stage: pipeline.StageInstructions, State: pipeline.StateSatisfied,
			DetailRedacted: detail,
		},
	); err != nil {
		t.Fatalf("recording attempt %d: %v", attempt, err)
	}
}

// pipelineAttemptRepositories opens a migrated database for ledger tests.
func pipelineAttemptRepositories(t *testing.T) *storage.Repositories {
	t.Helper()
	return pipelineAttemptRepositoriesWithClock(t, time.Now)
}

// pipelineAttemptRepositoriesWithClock is the same, with the clock supplied so
// a test can measure a span without sleeping.
func pipelineAttemptRepositoriesWithClock(
	t *testing.T,
	clock func() time.Time,
) *storage.Repositories {
	t.Helper()
	root := t.TempDir()
	database, err := storage.Open(t.Context(), storage.OpenOptions{
		Path: filepath.Join(root, "codeflux.sqlite3"),
	})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(t.Context()) })
	if _, err := database.Migrate(t.Context(), storage.MigrationOptions{
		ApplicationVersion: "pipe-003-test",
		BackupDirectory:    filepath.Join(root, "backups"),
		AvailableBytes:     func(string) (uint64, error) { return ^uint64(0), nil },
	}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repositories, err := storage.NewRepositories(database, clock)
	if err != nil {
		t.Fatalf("build repositories: %v", err)
	}
	return repositories
}

// pipelineAttemptScope creates the parent rows a stage record needs.
func pipelineAttemptScope(
	t *testing.T,
	repositories *storage.Repositories,
) (domain.TaskID, domain.RunID) {
	t.Helper()
	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	taskID, _ := domain.NewTaskID()
	runID, _ := domain.NewRunID()

	if _, err := repositories.CreateProject(t.Context(), storage.CreateProject{
		ID: projectID, Name: "Pipe 003",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(t.Context(), storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID,
		CanonicalPath: t.TempDir(), GitIdentity: "pipe-003-repository",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(t.Context(), storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Pipe 003",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateTask(t.Context(), storage.CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset:      domain.PolicyPresetBalanced,
		ReasoningEffort:   domain.ReasoningEffortStandard,
		RiskLevel:         domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		SettingsRevision:  0, IdempotencyKey: "pipe-003-task",
	}); err != nil {
		t.Fatal(err)
	}
	return taskID, runID
}

// TestPIPE004_NoStageWithoutACheckIsEverWrittenAsSatisfied enforces the
// binding table against the coordinator's real write sites.
//
// internal/pipeline declares which stages may report satisfied. That
// declaration is only worth something if the code obeys it, so the write sites
// are read back and checked against the table.
func TestPIPE004_NoStageWithoutACheckIsEverWrittenAsSatisfied(t *testing.T) {
	root := repositoryRootForCoordinatorTest(t)
	sources, err := filepath.Glob(filepath.Join(root, "internal", "coordinator", "*.go"))
	if err != nil {
		t.Fatal(err)
	}

	// The two ledger calls that can produce a satisfied row.
	satisfying := regexp.MustCompile(
		`ledger\.(?:satisfied|require)\(ctx, pipeline\.(Stage[A-Za-z]+)`)

	forbidden := map[string]struct{}{}
	for _, check := range pipeline.Checks {
		if check.MaySatisfy {
			continue
		}
		forbidden[stageConstantName(check.Stage)] = struct{}{}
	}
	if len(forbidden) == 0 {
		t.Fatal("no stage is forbidden from being satisfied; the check is vacuous")
	}

	checked := 0
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		for _, match := range satisfying.FindAllStringSubmatch(string(content), -1) {
			checked++
			if _, banned := forbidden[match[1]]; banned {
				relative, _ := filepath.Rel(root, path)
				t.Errorf("%s records %s as satisfied, but internal/pipeline "+
					"declares it has no check and may record only skipped or "+
					"not-implemented", filepath.ToSlash(relative), match[1])
			}
		}
	}
	if checked == 0 {
		t.Fatal("no satisfying write site was found; the scan has broken")
	}
}

// stageConstantName renders a stage number as the Go constant naming it, so a
// table entry can be compared with a source match.
func stageConstantName(stage pipeline.Number) string {
	for _, declared := range pipeline.Flow {
		if declared.Number != stage {
			continue
		}
		var builder strings.Builder
		builder.WriteString("Stage")
		for _, word := range strings.Split(declared.Name, "-") {
			if word == "" {
				continue
			}
			builder.WriteString(strings.ToUpper(word[:1]) + word[1:])
		}
		return builder.String()
	}
	return ""
}

// TestPIPE006_AStageRecordsARealSpanRatherThanOneInstant covers PIPE-006.
//
// Both timestamp columns received the write time, so the schema modelled a
// duration and no duration was recordable: every stage of every run took zero.
func TestPIPE006_AStageRecordsARealSpanRatherThanOneInstant(t *testing.T) {
	// One clock, shared by the ledger and by storage. Using two would compare
	// a fabricated start against a real finish and prove nothing.
	instant := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		instant = instant.Add(250 * time.Millisecond)
		return instant
	}
	repositories := pipelineAttemptRepositoriesWithClock(t, clock)
	taskID, _ := pipelineAttemptScope(t, repositories)

	// The ledger holds no run identity, so its rows are attributed to the task.
	ledger := newPipelineLedger(
		context.Background(), repositories, taskID, domain.RunID{})
	ledger.now = clock

	ledger.satisfied(context.Background(), pipeline.StageInstructions, "first", nil)
	ledger.satisfied(context.Background(), pipeline.StageClarification, "second", nil)

	records, err := repositories.ListPipelineStages(
		context.Background(), taskID, ledger.currentAttempt())
	if err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	if len(records) < 2 {
		t.Fatalf("recorded %d stages, want at least 2", len(records))
	}
	for _, record := range records {
		if record.FinishedAt.Before(record.StartedAt) {
			t.Errorf("stage %d finished before it started", record.Stage)
		}
	}

	// The second stage must start no earlier than the first finished, which is
	// what makes the spans read as a sequence rather than as overlapping
	// instants.
	if records[1].StartedAt.Before(records[0].FinishedAt) {
		t.Errorf("stage %d starts before stage %d finished",
			records[1].Stage, records[0].Stage)
	}
}

// TestPIPE006_AStageWithNoTrackedStartRecordsNoSpan keeps the honest default.
//
// A caller that does not track starts must not have a duration invented for
// it: equal timestamps say "not measured", and a fabricated span would be a
// measurement nobody took.
func TestPIPE006_AStageWithNoTrackedStartRecordsNoSpan(t *testing.T) {
	repositories := pipelineAttemptRepositories(t)
	taskID, _ := pipelineAttemptScope(t, repositories)

	record, err := repositories.RecordPipelineStageResult(
		t.Context(),
		storage.RecordPipelineStage{
			TaskID: taskID, Attempt: 1,
			Stage: pipeline.StageInstructions, State: pipeline.StateSatisfied,
			DetailRedacted: "no tracked start",
		},
	)
	if err != nil {
		t.Fatalf("recording: %v", err)
	}
	if record.Duration() != 0 {
		t.Errorf("an untracked stage reported a span of %s", record.Duration())
	}
}
