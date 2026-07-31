package coordinator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/review"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/testselection"
	"codeflux.dev/codeflux/internal/validation"
	"codeflux.dev/codeflux/internal/validationbaseline"
)

func TestValidationReviewWorkflowComposesPlanningExecutionAndStaleRerun(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	runID, _ := domain.NewRunID()
	diffA, diffB, diffC := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	repository := &validationWorkflowRepositoryStub{}
	bindings := &validationWorkflowBindingsStub{values: []validation.WorktreeDiffBinding{
		{WorktreeRevision: strings.Repeat("1", 40), DiffIdentity: diffA},
		{WorktreeRevision: strings.Repeat("1", 40), DiffIdentity: diffB},
		{WorktreeRevision: strings.Repeat("1", 40), DiffIdentity: diffB},
		{WorktreeRevision: strings.Repeat("1", 40), DiffIdentity: diffB},
		{WorktreeRevision: strings.Repeat("1", 40), DiffIdentity: diffB},
		{WorktreeRevision: strings.Repeat("1", 40), DiffIdentity: diffB},
	}}
	events := &validationWorkflowEventsStub{}
	workflow, err := NewValidationReviewWorkflow(repository, bindings, validationWorkflowRunnerStub{}, events)
	if err != nil {
		t.Fatal(err)
	}
	command := validationbaseline.CommandBinding{DefinitionID: "go-test-package", ExecutableIdentity: "go-toolchain-v1"}
	baseline, err := validationbaseline.NewEvidence(validationbaseline.Binding{
		Command: command, Revision: validationbaseline.RevisionBinding{WorktreeRevision: "base", DiffIdentity: diffA},
	}, []validationbaseline.Attempt{{Ordinal: 1, Status: validationbaseline.AttemptUnavailable, UnavailableReason: "pre-change execution exceeded the approved budget"}})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := validationbaseline.NewEvidence(validationbaseline.Binding{
		Command: command, Revision: validationbaseline.RevisionBinding{WorktreeRevision: "candidate", DiffIdentity: diffB},
	}, []validationbaseline.Attempt{{Ordinal: 1, Status: validationbaseline.AttemptPassed}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := workflow.Plan(t.Context(), ValidationPlanningInput{
		TaskID: taskID, RunID: runID,
		RiskSignals: []review.RiskSignal{review.RiskSignalNarrowScopedChange},
		TestSelection: testselection.Request{
			ChangedFiles: []testselection.ChangedFile{{Path: "internal/widget.go", Package: "codeflux.dev/project/internal"}},
			PackageChecks: []testselection.PackageCheck{{Package: "codeflux.dev/project/internal", Command: testselection.Command{
				Executable: "go", Arguments: []string{"test", "./internal"}, Cost: testselection.CostFocused,
			}}},
		},
		BaselineEvidence: []BaselineEvidencePair{{Baseline: baseline, Candidate: candidate}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Risk.SelectedRisk() != domain.RiskLevelRoutine || len(plan.Tests.Checks) != 1 ||
		len(plan.Profile.Checks) != 1 || len(plan.BaselineRecords) != 1 ||
		plan.BaselineRecords[0].Comparison != validationbaseline.ComparisonBaselineUnavailable ||
		len(plan.BaselineRecords[0].Limitations) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	report, err := workflow.Execute(t.Context(), ValidationExecutionInput{
		Plan: plan, WorktreePath: t.TempDir(), IdempotencyPrefix: "validation-workflow-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Completions) != 2 || report.DiffIdentity != diffB || len(events.invalidations) == 0 {
		t.Fatalf("execution report = %#v, invalidations = %#v", report, events.invalidations)
	}
	if err := workflow.EnsureCompletionReady(t.Context(), taskID, runID); err != nil {
		t.Fatalf("current completion gate = %v", err)
	}
	bindings.values = []validation.WorktreeDiffBinding{{WorktreeRevision: strings.Repeat("1", 40), DiffIdentity: diffC}}
	bindings.index = 0
	if err := workflow.EnsureCompletionReady(t.Context(), taskID, runID); !errors.Is(err, ErrRequiredValidationIncomplete) {
		t.Fatalf("changed-diff completion gate = %v", err)
	}
}

type validationWorkflowRepositoryStub struct {
	classification *storage.TaskRiskClassification
	intents        []validation.RunIntent
	results        map[domain.ValidationID]validation.RunResult
	invalidated    map[string]struct{}
}

func (stub *validationWorkflowRepositoryStub) GetLatestTaskRiskClassification(context.Context, domain.TaskID) (storage.TaskRiskClassification, error) {
	if stub.classification == nil {
		return storage.TaskRiskClassification{}, storage.ErrNotFound
	}
	return *stub.classification, nil
}

func (stub *validationWorkflowRepositoryStub) RecordInitialTaskRiskClassification(_ context.Context, taskID domain.TaskID, signals []review.RiskSignal, override domain.RiskLevel) (storage.TaskRiskClassification, error) {
	classification, err := review.ClassifyChangeRisk(signals, override)
	value := storage.TaskRiskClassification{TaskID: taskID, Revision: 1, Classification: classification}
	stub.classification = &value
	return value, err
}

func (stub *validationWorkflowRepositoryStub) EscalateTaskRiskClassification(_ context.Context, taskID domain.TaskID, signals []review.RiskSignal, override domain.RiskLevel) (storage.TaskRiskClassification, error) {
	classification, err := review.EscalateChangeRisk(stub.classification.Classification, signals, override)
	value := storage.TaskRiskClassification{TaskID: taskID, Revision: stub.classification.Revision + 1, Classification: classification}
	stub.classification = &value
	return value, err
}

func (stub *validationWorkflowRepositoryStub) CreateValidationRunIntent(_ context.Context, intent validation.RunIntent) (validation.RunIntent, error) {
	stub.intents = append(stub.intents, intent)
	if stub.results == nil {
		stub.results = make(map[domain.ValidationID]validation.RunResult)
	}
	return intent, nil
}

func (stub *validationWorkflowRepositoryStub) CommitValidationRunResult(_ context.Context, result validation.RunResult) (validation.RunResult, error) {
	stub.results[result.ValidationRunID] = result
	return result, nil
}

func (stub *validationWorkflowRepositoryStub) InvalidateValidationRuns(_ context.Context, runID domain.RunID, current string) ([]validation.Invalidation, error) {
	if stub.invalidated == nil {
		stub.invalidated = make(map[string]struct{})
	}
	var result []validation.Invalidation
	for _, intent := range stub.intents {
		if intent.RunID != runID || intent.DiffIdentity == current {
			continue
		}
		key := intent.ID.String() + current
		if _, exists := stub.invalidated[key]; exists {
			continue
		}
		stub.invalidated[key] = struct{}{}
		result = append(result, validation.Invalidation{
			ValidationRunID: intent.ID, PreviousDiffIdentity: intent.DiffIdentity,
			CurrentDiffIdentity: current, Reason: "underlying-diff-changed",
		})
	}
	return result, nil
}

func (stub *validationWorkflowRepositoryStub) ListRequiredValidationReruns(_ context.Context, runID domain.RunID, current string) ([]validation.RequiredRerun, error) {
	passed := make(map[string]bool)
	for _, intent := range stub.intents {
		result, exists := stub.results[intent.ID]
		if intent.RunID == runID && intent.Required && intent.DiffIdentity == current && exists && result.State == domain.ValidationStatePassed {
			passed[intent.CheckID] = true
		}
	}
	seen := make(map[string]bool)
	var reruns []validation.RequiredRerun
	for _, intent := range stub.intents {
		if intent.RunID != runID || !intent.Required || intent.DiffIdentity == current || passed[intent.CheckID] || seen[intent.CheckID] {
			continue
		}
		seen[intent.CheckID] = true
		reruns = append(reruns, validation.RequiredRerun{
			CheckID: intent.CheckID, PreviousRunID: intent.ID,
			PreviousDiffIdentity: intent.DiffIdentity, CurrentDiffIdentity: current,
		})
	}
	return reruns, nil
}

type validationWorkflowBindingsStub struct {
	values []validation.WorktreeDiffBinding
	index  int
}

func (stub *validationWorkflowBindingsStub) CurrentValidationBinding(context.Context, domain.TaskID) (validation.WorktreeDiffBinding, error) {
	if len(stub.values) == 0 {
		return validation.WorktreeDiffBinding{}, errors.New("validation binding unavailable")
	}
	if stub.index >= len(stub.values) {
		return stub.values[len(stub.values)-1], nil
	}
	value := stub.values[stub.index]
	stub.index++
	return value, nil
}

type validationWorkflowRunnerStub struct{}

func (validationWorkflowRunnerStub) ResolveExecutable([]string) (validation.ExecutableIdentity, error) {
	return validation.ExecutableIdentity{Path: "C:/Go/bin/go.exe", SHA256: strings.Repeat("d", 64)}, nil
}

func (validationWorkflowRunnerStub) Execute(context.Context, validation.RunExecution) (validation.ExecutionResult, error) {
	return validation.ExecutionResult{ExitCode: 0, Duration: time.Second, Summary: "go test passed"}, nil
}

type validationWorkflowEventsStub struct{ invalidations []validation.Invalidation }

func (*validationWorkflowEventsStub) ValidationStarted(context.Context, validation.RunIntent) error {
	return nil
}
func (*validationWorkflowEventsStub) ValidationResultCommitted(context.Context, validation.RunIntent, validation.RunResult) error {
	return nil
}
func (stub *validationWorkflowEventsStub) ValidationInvalidated(_ context.Context, value validation.Invalidation) error {
	stub.invalidations = append(stub.invalidations, value)
	return nil
}
