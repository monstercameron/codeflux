package coordinator

import (
	"context"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/forecast"
	"codeflux.dev/codeflux/internal/storage"
)

// mustIntakeFixtureThread creates a real project, repository, and thread for
// an intake test and returns the thread.
func mustIntakeFixtureThread(t *testing.T, repositories *storage.Repositories) storage.Thread {
	t.Helper()
	ctx := context.Background()
	projectID, repositoryID := mustCreateForecastFixtureProjectAndRepository(t, repositories)
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	thread, err := repositories.CreateThread(ctx, storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "intake fixture thread",
	})
	if err != nil {
		t.Fatal(err)
	}
	return thread
}

// TestIntakeTaskTurnsARequirementIntoAForecastedTask is the end-to-end proof
// that a submitted requirement now becomes a startable task. Before this
// path existed, TaskForecastInput was built only in tests and nothing could
// begin work at all.
func TestIntakeTaskTurnsARequirementIntoAForecastedTask(t *testing.T) {
	ctx := context.Background()
	repositories, retrievalService := mustOpenRetrievalRepositories(t)
	service, err := NewTaskPreflightService(repositories, retrievalService)
	if err != nil {
		t.Fatal(err)
	}
	thread := mustIntakeFixtureThread(t, repositories)

	result, err := service.IntakeTask(ctx, TaskIntakeRequest{
		ThreadID:                 thread.ID,
		Requirement:              "Add a health endpoint that returns 200 with a JSON body.",
		TaskClass:                fingerprint.TaskClassFeature,
		RepositoryRevision:       strings.Repeat("a", 40),
		BaselineModelRevision:    "fixture-model-2026-08-01",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"internal/server"},
		RepositorySize:           forecast.RepositorySize{Files: 120, Bytes: 512_000},
		IdempotencyKey:           "intake-happy-path",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Task.ID.IsZero() {
		t.Fatal("intake must create a real task identity")
	}
	if result.Task.ThreadID != thread.ID || result.Task.RepositoryID != thread.RepositoryID {
		t.Fatalf("task = %#v, want it bound to the thread's own repository", result.Task)
	}
	if result.Budget.BudgetID.IsZero() {
		t.Fatal("intake must surface the policy-materialized hard budget")
	}
	if result.Forecasted.Policy.Revision == 0 {
		t.Fatal("intake must produce an immutable execution policy revision to present")
	}
	if result.Forecasted.Forecast.Revision == 0 {
		t.Fatal("intake must produce an immutable effort forecast to present")
	}

	// The pre-work retrieval gate ran before planning: with an empty project
	// it must have fallen back, and that fallback must be durably recorded
	// against this task rather than silently skipped.
	report, err := retrievalService.ListTaskMemoryInfluence(ctx, result.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.FellBackQueries) == 0 {
		t.Fatal("intake must consult project memory before planning, recording a fallback when nothing is eligible")
	}
	if len(report.Influenced) != 0 {
		t.Fatalf("an empty project cannot have influenced the task, got %#v", report.Influenced)
	}
}

// TestIntakeTaskRefusesUnobservableInputs proves intake never fabricates a
// fact it cannot observe. Each rejected field would otherwise put a guess
// inside the fingerprint that gates retrieval and routing.
func TestIntakeTaskRefusesUnobservableInputs(t *testing.T) {
	ctx := context.Background()
	repositories, retrievalService := mustOpenRetrievalRepositories(t)
	service, err := NewTaskPreflightService(repositories, retrievalService)
	if err != nil {
		t.Fatal(err)
	}
	thread := mustIntakeFixtureThread(t, repositories)

	valid := TaskIntakeRequest{
		ThreadID:                 thread.ID,
		Requirement:              "Fix the failing retry test.",
		TaskClass:                fingerprint.TaskClassBugFix,
		RepositoryRevision:       strings.Repeat("b", 40),
		BaselineModelRevision:    "fixture-model-2026-08-01",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		IdempotencyKey:           "intake-validation",
	}

	cases := []struct {
		name   string
		mutate func(TaskIntakeRequest) TaskIntakeRequest
	}{
		{
			name:   "empty requirement",
			mutate: func(r TaskIntakeRequest) TaskIntakeRequest { r.Requirement = "   "; return r },
		},
		{
			name: "requirement past the bound",
			mutate: func(r TaskIntakeRequest) TaskIntakeRequest {
				r.Requirement = strings.Repeat("x", MaximumRequirementBytes+1)
				return r
			},
		},
		{
			name:   "undeclared task class",
			mutate: func(r TaskIntakeRequest) TaskIntakeRequest { r.TaskClass = ""; return r },
		},
		{
			name:   "unknown repository revision",
			mutate: func(r TaskIntakeRequest) TaskIntakeRequest { r.RepositoryRevision = ""; return r },
		},
		{
			name: "unknown baseline model revision",
			mutate: func(r TaskIntakeRequest) TaskIntakeRequest {
				r.BaselineModelRevision = ""
				return r
			},
		},
		{
			name:   "no thread",
			mutate: func(r TaskIntakeRequest) TaskIntakeRequest { r.ThreadID = domain.ThreadID{}; return r },
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := service.IntakeTask(ctx, testCase.mutate(valid)); err == nil {
				t.Fatalf("%s must be rejected rather than guessed at", testCase.name)
			}
		})
	}
}

// TestIntakeTaskDefaultsAreConservative proves the fields intake does default
// can only narrow what a task may do. A default that widened authority or
// lowered an assurance floor would be a silent policy decision.
func TestIntakeTaskDefaultsAreConservative(t *testing.T) {
	defaulted := applyIntakeDefaults(TaskIntakeRequest{})
	if defaulted.RiskLevel != domain.RiskLevelRoutine {
		t.Fatalf("default risk = %s, want the routine floor", defaulted.RiskLevel)
	}
	if defaulted.RequiredAssurance != domain.AssuranceLevelRuntimeOnly {
		t.Fatalf("default assurance = %s, want runtime-only", defaulted.RequiredAssurance)
	}
	if defaulted.PolicyPreset != domain.PolicyPresetBalanced {
		t.Fatalf("default policy preset = %s, want balanced", defaulted.PolicyPreset)
	}
	if len(defaulted.RequestedAuthority) != 0 {
		t.Fatalf("defaults must never grant authority, got %#v", defaulted.RequestedAuthority)
	}
	if len(defaulted.AffectedPaths) != 0 || len(defaulted.AffectedPackages) != 0 || len(defaulted.AffectedSymbols) != 0 {
		t.Fatal("defaults must never invent scope hints")
	}
}
