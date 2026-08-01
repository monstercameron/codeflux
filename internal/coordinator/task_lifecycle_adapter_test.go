package coordinator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TestLifecycleAdapterCreatesAForecastedTaskFromARequirement proves the
// transport-facing lifecycle surface works end to end over real storage: a
// submitted requirement becomes a real task with an immutable policy,
// forecast, and budget to present for approval.
//
// This is the link that did not exist before: CreateTask was declared in the
// proto and returned Unimplemented, so nothing could turn a message into
// work.
func TestLifecycleAdapterCreatesAForecastedTaskFromARequirement(t *testing.T) {
	ctx := context.Background()
	repositories, retrievalService := mustOpenRetrievalRepositories(t)
	preflight, err := NewTaskPreflightService(repositories, retrievalService)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewTaskLifecycleAdapter(preflight, repositories, &recordingRunLauncher{})
	if err != nil {
		t.Fatal(err)
	}
	thread := mustIntakeFixtureThread(t, repositories)

	view, err := adapter.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 thread.ID,
		Requirement:              "Add a readiness probe to the server.",
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       strings.Repeat("c", 40),
		BaselineModelRevision:    "fixture-model-2026-08-01",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"internal/server"},
		IdempotencyKey:           "lifecycle-create",
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.TaskID.IsZero() {
		t.Fatal("CreateTaskFromRequirement must return a real task identity")
	}
	if view.State != domain.TaskStateDraft {
		t.Fatalf("state = %s, want draft: a created task is not approved yet", view.State)
	}
	if view.PolicyRevision == 0 || view.ForecastRevision == 0 {
		t.Fatalf("view = %#v, want immutable policy and forecast revisions to present", view)
	}
	if view.BudgetID.IsZero() {
		t.Fatal("the policy-materialized budget must be surfaced for approval")
	}

	// Project memory was consulted before planning, and the empty-project
	// fallback was durably recorded rather than silently skipped.
	report, err := retrievalService.ListTaskMemoryInfluence(ctx, view.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.FellBackQueries) == 0 {
		t.Fatal("intake must consult project memory before planning")
	}
}

// TestLifecycleAdapterRefusesStartWithoutAnApprovedPreflight proves a task
// cannot begin work from an unreviewed or nonexistent binding. Starting is
// gated on the exact preflight the user approved, not on a client's say-so.
func TestLifecycleAdapterRefusesStartWithoutAnApprovedPreflight(t *testing.T) {
	ctx := context.Background()
	repositories, retrievalService := mustOpenRetrievalRepositories(t)
	preflight, err := NewTaskPreflightService(repositories, retrievalService)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewTaskLifecycleAdapter(preflight, repositories, &recordingRunLauncher{})
	if err != nil {
		t.Fatal(err)
	}
	thread := mustIntakeFixtureThread(t, repositories)

	view, err := adapter.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 thread.ID,
		Requirement:              "Tighten the retry backoff.",
		TaskClass:                string(fingerprint.TaskClassBugFix),
		RepositoryRevision:       strings.Repeat("d", 40),
		BaselineModelRevision:    "fixture-model-2026-08-01",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		IdempotencyKey:           "lifecycle-start-refusal",
	})
	if err != nil {
		t.Fatal(err)
	}

	// No preflight was ever bound, because the task was never approved into
	// Ready. Starting must refuse rather than invent a binding.
	if _, err := adapter.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            view.TaskID,
		ExpectedRevision:  view.Revision,
		PreflightRevision: 1,
		IdempotencyKey:    "lifecycle-start-refusal",
	}); err == nil {
		t.Fatal("starting a task with no approved preflight binding must be refused")
	}
}

// TestLifecycleAdapterRejectsUndeclaredTaskShape proves the transport surface
// inherits intake's refusal to guess. A caller that omits the declared shape
// gets an error, not a fabricated fingerprint.
func TestLifecycleAdapterRejectsUndeclaredTaskShape(t *testing.T) {
	ctx := context.Background()
	repositories, retrievalService := mustOpenRetrievalRepositories(t)
	preflight, err := NewTaskPreflightService(repositories, retrievalService)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewTaskLifecycleAdapter(preflight, repositories, &recordingRunLauncher{})
	if err != nil {
		t.Fatal(err)
	}
	thread := mustIntakeFixtureThread(t, repositories)

	if _, err := adapter.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:       thread.ID,
		Requirement:    "Do the thing.",
		IdempotencyKey: "lifecycle-undeclared",
	}); err == nil {
		t.Fatal("a requirement with no declared task class or base revision must be rejected")
	}
}

// TestLifecycleAdapterRequiresItsDependencies keeps a half-constructed
// adapter from reporting success.
func TestLifecycleAdapterRequiresItsDependencies(t *testing.T) {
	if _, err := NewTaskLifecycleAdapter(nil, nil, nil); err == nil {
		t.Fatal("an adapter with no preflight service must be rejected")
	}
	var store taskLifecycleStore
	if _, err := NewTaskLifecycleAdapter(
		&TaskPreflightService{}, store, &recordingRunLauncher{},
	); err == nil {
		t.Fatal("an adapter with no store must be rejected")
	}
	// An adapter with no launcher would record that a run started and start
	// nothing, which is the exact defect this argument was added to prevent.
	repositories, _ := mustOpenRetrievalRepositories(t)
	if _, err := NewTaskLifecycleAdapter(
		&TaskPreflightService{}, repositories, nil,
	); err == nil {
		t.Fatal("an adapter with no run launcher must be rejected")
	}
}

// compile-time proof the adapter satisfies the transport surface.
var _ transport.TaskLifecycleApplication = (*TaskLifecycleAdapter)(nil)

// compile-time proof the concrete repositories satisfy the narrow store.
var _ taskLifecycleStore = (*storage.Repositories)(nil)

// TestStartingATaskAsksForAWorker is the regression guard for the defect this
// path was built to fix.
//
// StartPreparedTask used to record a run and return. The task read as running,
// no subprocess existed, and no provider request was ever made. Asserting the
// recorded launch — not just the returned state — is what makes that
// impossible to reintroduce, because the state was correct even when nothing
// ran.
func TestStartingATaskAsksForAWorker(t *testing.T) {
	ctx := context.Background()
	repositories, retrievalService := mustOpenRetrievalRepositories(t)
	preflightService, err := NewTaskPreflightService(repositories, retrievalService)
	if err != nil {
		t.Fatal(err)
	}
	launcher := &recordingRunLauncher{}
	adapter, err := NewTaskLifecycleAdapter(preflightService, repositories, launcher)
	if err != nil {
		t.Fatal(err)
	}
	thread := mustIntakeFixtureThread(t, repositories)

	created, err := adapter.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 thread.ID,
		Requirement:              "Add a readiness probe.",
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       strings.Repeat("a", 40),
		BaselineModelRevision:    "fixture-model-2026-08-01",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"internal/server"},
		IdempotencyKey:           "launch-assert-create",
	})
	if err != nil {
		t.Fatal(err)
	}
	readyRevision := driveTaskToReady(t, repositories, created.TaskID, created.Revision)
	preflight, err := preflightService.BindExecution(
		ctx, created.TaskID, readyRevision,
		ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"launch-assert-bind",
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := adapter.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "launch-assert-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}

	recorded := launcher.recorded()
	if len(recorded) != 1 {
		t.Fatalf("start asked for %d worker(s), want exactly 1", len(recorded))
	}
	launch := recorded[0]
	if launch.TaskID != created.TaskID {
		t.Errorf("launched task = %s, want %s", launch.TaskID, created.TaskID)
	}
	if launch.RunID.IsZero() {
		t.Error("the launch carries no run identity")
	}
	if launch.PolicyRevision != created.PolicyRevision {
		t.Errorf("launched policy revision = %d, want the approved %d",
			launch.PolicyRevision, created.PolicyRevision)
	}
	// The provider comes from the reviewed policy, not from a current default:
	// a run approved against one provider must not be queued against another.
	if launch.ProviderKey == "" {
		t.Error("the launch names no provider, so the queue cannot schedule it")
	}
}

// TestAFailedLaunchIsReportedNotSwallowed keeps a start that could not run
// from reading as a success.
func TestAFailedLaunchIsReportedNotSwallowed(t *testing.T) {
	ctx := context.Background()
	repositories, retrievalService := mustOpenRetrievalRepositories(t)
	preflightService, err := NewTaskPreflightService(repositories, retrievalService)
	if err != nil {
		t.Fatal(err)
	}
	launcher := &recordingRunLauncher{err: errors.New("no worker executable")}
	adapter, err := NewTaskLifecycleAdapter(preflightService, repositories, launcher)
	if err != nil {
		t.Fatal(err)
	}
	thread := mustIntakeFixtureThread(t, repositories)

	created, err := adapter.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 thread.ID,
		Requirement:              "Add a readiness probe.",
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       strings.Repeat("b", 40),
		BaselineModelRevision:    "fixture-model-2026-08-01",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"internal/server"},
		IdempotencyKey:           "launch-failure-create",
	})
	if err != nil {
		t.Fatal(err)
	}
	readyRevision := driveTaskToReady(t, repositories, created.TaskID, created.Revision)
	preflight, err := preflightService.BindExecution(
		ctx, created.TaskID, readyRevision,
		ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"launch-failure-bind",
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = adapter.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "launch-failure-start",
	})
	if err == nil {
		t.Fatal("a start whose worker never launched was reported as success")
	}
	if !strings.Contains(err.Error(), "worker did not start") {
		t.Errorf("the error does not say what failed: %v", err)
	}
}
