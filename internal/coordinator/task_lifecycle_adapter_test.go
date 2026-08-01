package coordinator

import (
	"context"
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
	adapter, err := NewTaskLifecycleAdapter(preflight, repositories)
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
	adapter, err := NewTaskLifecycleAdapter(preflight, repositories)
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
	adapter, err := NewTaskLifecycleAdapter(preflight, repositories)
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
	if _, err := NewTaskLifecycleAdapter(nil, nil); err == nil {
		t.Fatal("an adapter with no preflight service must be rejected")
	}
	var store taskLifecycleStore
	if _, err := NewTaskLifecycleAdapter(&TaskPreflightService{}, store); err == nil {
		t.Fatal("an adapter with no store must be rejected")
	}
}

// compile-time proof the adapter satisfies the transport surface.
var _ transport.TaskLifecycleApplication = (*TaskLifecycleAdapter)(nil)

// compile-time proof the concrete repositories satisfy the narrow store.
var _ taskLifecycleStore = (*storage.Repositories)(nil)
