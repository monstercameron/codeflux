package coordinator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/storage"
)

func TestRepairCompletionSuccessfulValidationAwaitsExplicitReview(t *testing.T) {
	fixture := newRepairCompletionFixture(t)
	fixture.runner.results = []ValidationCommandRun{
		passedValidationRun(t, 1),
	}
	service := fixture.service(t)
	validation, err := service.ValidateAndRepair(
		t.Context(),
		fixture.validationInput(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !validation.ReadyForCompletion ||
		validation.RepairRounds != 0 ||
		len(fixture.store.attributions) != 1 ||
		len(fixture.checkpoints.calls) != 0 ||
		len(fixture.repairs.calls) != 0 {
		t.Fatalf("validation = %#v, fixture = %#v", validation, fixture)
	}
	completion, err := service.PrepareCompletion(
		t.Context(),
		CompletionInput{
			TaskID: fixture.taskID, RunID: fixture.runID,
			PlanRevision:         1,
			ExpectedTaskRevision: 7,
			ExpectedRunRevision:  11,
			EventID:              repairCoordinatorEventID(t, 2),
			EventIdempotencyKey:  "completion-event",
			Assumptions:          []string{"fixed validation profile selected"},
			Limitations:          []string{"live provider not exercised"},
			IdempotencyKey:       "completion-candidate",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if completion.State != domain.TaskStateAwaitingReview ||
		fixture.store.completion.State != domain.TaskStateAwaitingReview ||
		strings.Contains(
			fixture.store.completionInput.DiffSummaryJSON,
			"ABCDEFGHIJKLMNOP",
		) {
		t.Fatalf(
			"completion = %#v, stored = %#v",
			completion,
			fixture.store.completionInput,
		)
	}
	state, err := service.DecideReview(
		t.Context(),
		agentloop.ReviewDecisionRequest{
			TaskID: fixture.taskID, RunID: fixture.runID,
			PlanRevision: 1, CompletionRevision: completion.Revision,
			ExpectedTaskRevision: 8,
			ExpectedRunRevision:  12,
			Decision:             agentloop.ReviewDecisionAccept,
			Actor:                "user:fixture", AuthorityReference: "review:fixture",
			ReasonRedacted: "reviewed exact completion evidence",
			IdempotencyKey: "accept-completion",
		},
		repairCoordinatorEventID(t, 3),
		"accept-completion-event",
	)
	if err != nil || state != domain.TaskStateCompleted ||
		fixture.store.review.Decision != agentloop.ReviewDecisionAccept {
		t.Fatalf("review state = %s, %v; record = %#v", state, err, fixture.store.review)
	}
}

func TestRepairCompletionRejectsCallerClaimsWithoutDurableValidationEvidence(
	t *testing.T,
) {
	tests := []struct {
		name     string
		evidence completionEvidenceRecord
	}{
		{
			name: "implemented is not validated",
			evidence: completionEvidenceRecord{
				PlanSteps: []completionPlanStepRecord{{
					PlanStepID: "step-validation",
					State:      agentloop.StepImplemented,
				}},
				ValidationAttributions: []completionValidationAttributionRecord{{
					TaskID:       repairCoordinatorTaskID(t, 100),
					RunID:        repairCoordinatorRunID(t, 101),
					PlanRevision: 1, PlanStepID: "step-validation",
					ValidationID: repairCoordinatorValidationID(t, 2),
				}},
			},
		},
		{
			name: "validated state lacks current validation link",
			evidence: completionEvidenceRecord{
				PlanSteps: []completionPlanStepRecord{{
					PlanStepID: "step-validation",
					State:      agentloop.StepValidated,
				}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRepairCompletionFixture(t)
			fixture.runner.results = []ValidationCommandRun{
				passedValidationRun(t, 2),
			}
			service := fixture.service(t)
			_, err := service.ValidateAndRepair(
				t.Context(),
				fixture.validationInput(0),
			)
			if err != nil {
				t.Fatal(err)
			}
			fixture.store.completionEvidence = &test.evidence

			_, err = service.PrepareCompletion(
				t.Context(),
				CompletionInput{
					TaskID: fixture.taskID, RunID: fixture.runID,
					PlanRevision:         1,
					ExpectedTaskRevision: 7,
					ExpectedRunRevision:  11,
					EventID:              repairCoordinatorEventID(t, 20),
					EventIdempotencyKey:  "blocked-completion-event",
					IdempotencyKey:       "blocked-completion",
				},
			)
			if !errors.Is(err, ErrRequiredValidationIncomplete) ||
				fixture.store.completion.Revision != 0 {
				t.Fatalf(
					"completion error=%v record=%#v",
					err,
					fixture.store.completion,
				)
			}
		})
	}
}

func TestRepairCompletionPersistsEveryValidationPlanStep(t *testing.T) {
	fixture := newRepairCompletionFixture(t)
	fixture.profile.Commands[0].PlanStepIDs = []string{
		"step-implementation",
		"step-validation",
	}
	fixture.runner.results = []ValidationCommandRun{
		passedValidationRun(t, 3),
	}
	outcome, err := fixture.service(t).ValidateAndRepair(
		t.Context(),
		fixture.validationInput(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.ReadyForCompletion ||
		len(fixture.store.attributions) != 1 ||
		!fixture.store.attributions[0].ValidationPassed ||
		!slices.Equal(
			fixture.store.attributions[0].PlanStepIDs,
			fixture.profile.Commands[0].PlanStepIDs,
		) {
		t.Fatalf(
			"outcome=%#v attribution=%#v",
			outcome,
			fixture.store.attributions,
		)
	}
}

func TestRepairCompletionRejectsForgedStrongerInMemoryOutcome(
	t *testing.T,
) {
	fixture := newRepairCompletionFixture(t)
	fixture.runner.results = []ValidationCommandRun{
		passedValidationRun(t, 4),
	}
	service := fixture.service(t)
	outcome, err := service.ValidateAndRepair(
		t.Context(),
		fixture.validationInput(0),
	)
	if err != nil || !outcome.ReadyForCompletion {
		t.Fatalf("outcome=%#v error=%v", outcome, err)
	}
	durable := outcome.Reports[len(outcome.Reports)-1]
	durable.Executions[0].State = domain.ValidationStateFailed
	durable.Executions[0].Failure = &agentloop.ValidationFailure{
		SummaryRedacted: "durable validation failed",
		PlanStepIDs:     append([]string{}, durable.Executions[0].PlanStepIDs...),
	}
	fixture.store.finalValidationReport = &durable

	_, err = service.PrepareCompletion(
		t.Context(),
		CompletionInput{
			TaskID: fixture.taskID, RunID: fixture.runID,
			PlanRevision:         1,
			ExpectedTaskRevision: 7, ExpectedRunRevision: 11,
			EventID:             repairCoordinatorEventID(t, 21),
			EventIdempotencyKey: "forged-stronger-event",
			IdempotencyKey:      "forged-stronger",
		},
	)
	if !errors.Is(err, ErrRequiredValidationIncomplete) ||
		fixture.store.completion.Revision != 0 {
		t.Fatalf("completion error=%v record=%#v", err, fixture.store.completion)
	}
}

func TestStoredCompletionReconstructionRejectsWeakOrForgedFinalPass(
	t *testing.T,
) {
	taskID := repairCoordinatorTaskID(t, 85)
	runID := repairCoordinatorRunID(t, 85)
	validationID := repairCoordinatorValidationID(t, 85)
	passedPresentation := agentloop.ValidationExecution{
		ValidationID: validationID, CommandID: "go-test",
		CommandFingerprint: strings.Repeat("b", 64),
		Required:           true, AcceptanceTest: true,
		PlanStepIDs: []string{"step-one"},
		State:       domain.ValidationStatePassed,
	}
	presentationJSON, presentationSHA256, err :=
		validationExecutionPresentation(passedPresentation)
	if err != nil {
		t.Fatal(err)
	}
	baseProfile := storage.SelectedValidationProfileEvidence{
		TaskID: taskID, RunID: runID, PlanRevision: 2,
		ProfileName: "routine-v1", ProfileVersion: "v1",
		ProfileDigest: strings.Repeat("a", 64),
		Commands: []storage.SelectedValidationCommandEvidence{
			{
				Ordinal: 1, CommandID: "go-test",
				CommandFingerprint: strings.Repeat("b", 64),
				PlanCommand:        "go test ./...",
				Required:           true, AcceptanceTest: true,
				RelevantChangedFiles: []string{"internal/agent/a.go"},
				PlanStepIDs:          []string{"step-one"},
			},
			{
				Ordinal: 2, CommandID: "go-build",
				CommandFingerprint: strings.Repeat("c", 64),
				PlanCommand:        "go build ./...",
				Required:           true,
				PlanStepIDs:        []string{"step-two"},
			},
		},
	}
	baseExecution := storage.DurableValidationExecution{
		TaskID: taskID, RunID: runID, PlanRevision: 2,
		ProfileDigest: baseProfile.ProfileDigest,
		Round:         3, Ordinal: 1, ValidationID: validationID,
		CommandID:          "go-test",
		CommandFingerprint: strings.Repeat("b", 64),
		State:              domain.ValidationStatePassed,
		PlanStepIDs:        []string{"step-one"}, ValidationPassed: true,
		PresentationRedactedJSON: presentationJSON,
		PresentationSHA256:       presentationSHA256,
	}
	tests := []struct {
		name       string
		profile    storage.SelectedValidationProfileEvidence
		executions []storage.DurableValidationExecution
	}{
		{
			name:    "older weak subset cannot satisfy stronger selected profile",
			profile: baseProfile, executions: []storage.DurableValidationExecution{
				baseExecution,
			},
		},
		{
			name: "forged passed presentation cannot override durable failure",
			profile: func() storage.SelectedValidationProfileEvidence {
				value := baseProfile
				value.Commands = value.Commands[:1]
				return value
			}(),
			executions: []storage.DurableValidationExecution{
				func() storage.DurableValidationExecution {
					value := baseExecution
					value.State = domain.ValidationStateFailed
					value.ValidationPassed = false
					value.FailureSummaryRedacted = "durable failure"
					return value
				}(),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repositories := &recordingRepairCompletionRepositories{
				selectedEvidence:  test.profile,
				durableExecutions: test.executions,
			}
			store := &storageRepairCompletionStore{repositories: repositories}
			if _, err := store.ReadFinalValidationReport(
				t.Context(),
				taskID,
				runID,
				2,
			); err == nil {
				t.Fatal("forged or weak durable validation unexpectedly passed")
			}
		})
	}
}

func TestStoredCompletionReconstructionRejectsForgedFailureLinkage(
	t *testing.T,
) {
	taskID := repairCoordinatorTaskID(t, 86)
	runID := repairCoordinatorRunID(t, 86)
	validationID := repairCoordinatorValidationID(t, 86)
	command := storage.SelectedValidationCommandEvidence{
		Ordinal: 1, CommandID: "go-test",
		CommandFingerprint:   strings.Repeat("b", 64),
		PlanCommand:          "go test ./...",
		Required:             true,
		AcceptanceTest:       true,
		RelevantChangedFiles: []string{"internal/agent"},
		PlanStepIDs:          []string{"step-one"},
	}
	profile := storage.SelectedValidationProfileEvidence{
		TaskID: taskID, RunID: runID, PlanRevision: 2,
		ProfileName: "routine-v1", ProfileVersion: "v1",
		ProfileDigest: strings.Repeat("a", 64),
		Commands:      []storage.SelectedValidationCommandEvidence{command},
	}
	baseFailure := agentloop.ValidationFailure{
		SummaryRedacted: "validation failed",
		ChangedFiles:    []string{"internal/agent/sub/a.go"},
		PlanStepIDs:     []string{"step-one"},
		OutputTruncated: true,
	}
	t.Run("directory scope accepts canonical descendant", func(t *testing.T) {
		presentation := agentloop.ValidationExecution{
			ValidationID: validationID, CommandID: command.CommandID,
			CommandFingerprint: command.CommandFingerprint,
			Required:           true, AcceptanceTest: true,
			PlanStepIDs: append([]string{}, command.PlanStepIDs...),
			State:       domain.ValidationStateFailed,
			Failure:     &baseFailure,
		}
		presentationJSON, presentationSHA256, err :=
			validationExecutionPresentation(presentation)
		if err != nil {
			t.Fatal(err)
		}
		execution := storage.DurableValidationExecution{
			TaskID: taskID, RunID: runID, PlanRevision: 2,
			ProfileDigest: profile.ProfileDigest,
			Round:         1, Ordinal: 1, ValidationID: validationID,
			CommandID:              command.CommandID,
			CommandFingerprint:     command.CommandFingerprint,
			State:                  domain.ValidationStateFailed,
			PlanStepIDs:            append([]string{}, command.PlanStepIDs...),
			FailureSummaryRedacted: baseFailure.SummaryRedacted,
			FailureChangedFiles: append(
				[]string{},
				baseFailure.ChangedFiles...,
			),
			FailurePlanStepIDs: append(
				[]string{},
				baseFailure.PlanStepIDs...,
			),
			FailurePresent:           true,
			OutputTruncated:          baseFailure.OutputTruncated,
			PresentationRedactedJSON: presentationJSON,
			PresentationSHA256:       presentationSHA256,
		}
		store := &storageRepairCompletionStore{
			repositories: &recordingRepairCompletionRepositories{
				selectedEvidence: profile,
				durableExecutions: []storage.DurableValidationExecution{
					execution,
				},
			},
		}
		if _, err := store.ReadFinalValidationReport(
			t.Context(),
			taskID,
			runID,
			2,
		); err != nil {
			t.Fatalf("directory-scoped child failure rejected: %v", err)
		}
	})
	tests := []struct {
		name          string
		mutateFailure func(*agentloop.ValidationFailure)
		mutateDurable func(*storage.DurableValidationExecution)
	}{
		{
			name: "failure step IDs differ from selected command",
			mutateFailure: func(failure *agentloop.ValidationFailure) {
				failure.PlanStepIDs = []string{"step-two"}
			},
			mutateDurable: func(execution *storage.DurableValidationExecution) {
				execution.FailurePlanStepIDs = []string{"step-two"}
			},
		},
		{
			name: "failure changed file lies outside selected scope",
			mutateFailure: func(failure *agentloop.ValidationFailure) {
				failure.ChangedFiles = []string{"internal/agent-old/other.go"}
			},
			mutateDurable: func(execution *storage.DurableValidationExecution) {
				execution.FailureChangedFiles = []string{
					"internal/agent-old/other.go",
				}
			},
		},
		{
			name:          "presentation truncation differs from durable operation",
			mutateFailure: func(*agentloop.ValidationFailure) {},
			mutateDurable: func(execution *storage.DurableValidationExecution) {
				execution.OutputTruncated = false
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := baseFailure
			failure.ChangedFiles = append([]string{}, baseFailure.ChangedFiles...)
			failure.PlanStepIDs = append([]string{}, baseFailure.PlanStepIDs...)
			test.mutateFailure(&failure)
			presentation := agentloop.ValidationExecution{
				ValidationID: validationID, CommandID: command.CommandID,
				CommandFingerprint: command.CommandFingerprint,
				Required:           true, AcceptanceTest: true,
				PlanStepIDs: append([]string{}, command.PlanStepIDs...),
				State:       domain.ValidationStateFailed,
				Failure:     &failure,
			}
			presentationJSON, presentationSHA256, err :=
				validationExecutionPresentation(presentation)
			if err != nil {
				t.Fatal(err)
			}
			execution := storage.DurableValidationExecution{
				TaskID: taskID, RunID: runID, PlanRevision: 2,
				ProfileDigest: profile.ProfileDigest,
				Round:         1, Ordinal: 1, ValidationID: validationID,
				CommandID:              command.CommandID,
				CommandFingerprint:     command.CommandFingerprint,
				State:                  domain.ValidationStateFailed,
				PlanStepIDs:            append([]string{}, command.PlanStepIDs...),
				FailureSummaryRedacted: failure.SummaryRedacted,
				FailureChangedFiles: append(
					[]string{},
					failure.ChangedFiles...,
				),
				FailurePlanStepIDs: append(
					[]string{},
					failure.PlanStepIDs...,
				),
				FailurePresent:           true,
				OutputTruncated:          failure.OutputTruncated,
				PresentationRedactedJSON: presentationJSON,
				PresentationSHA256:       presentationSHA256,
			}
			test.mutateDurable(&execution)
			store := &storageRepairCompletionStore{
				repositories: &recordingRepairCompletionRepositories{
					selectedEvidence:  profile,
					durableExecutions: []storage.DurableValidationExecution{execution},
				},
			}
			if _, err := store.ReadFinalValidationReport(
				t.Context(),
				taskID,
				runID,
				2,
			); err == nil {
				t.Fatal("forged failure linkage unexpectedly passed")
			}
		})
	}
}

func TestStorageRepairCompletionAdapterMapsDurableRecords(t *testing.T) {
	repositories := &recordingRepairCompletionRepositories{}
	store := &storageRepairCompletionStore{repositories: repositories}
	taskID := repairCoordinatorTaskID(t, 80)
	runID := repairCoordinatorRunID(t, 80)
	validationID := repairCoordinatorValidationID(t, 80)
	checkpointID := repairCoordinatorCheckpointID(t, 80)
	eventID := repairCoordinatorEventID(t, 80)
	repairRevision := uint64(3)
	commandExecutionID := "command-execution-80"

	profile := repairCoordinatorProfile(taskID, runID)
	profileDigest, err := profile.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BindValidationProfile(
		t.Context(),
		selectedValidationProfileRecord{
			TaskID: taskID, RunID: runID, PlanRevision: 2,
			Profile: profile, ProfileDigest: profileDigest,
			IdempotencyKey: "profile-80",
		},
	); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordValidationAttribution(
		t.Context(),
		validationAttributionRecord{
			TaskID: taskID, RunID: runID, PlanRevision: 2,
			PlanStepIDs: []string{"step-80"}, ValidationID: validationID,
			CommandExecutionID:    commandExecutionID,
			RepairAttemptRevision: &repairRevision,
			ValidationPassed:      true,
			IdempotencyKey:        "validation-attribution-80",
		},
	); err != nil {
		t.Fatal(err)
	}
	revision, err := store.BeginRepair(
		t.Context(),
		beginRepairRecord{
			TaskID: taskID, RunID: runID, PlanRevision: 2, Ordinal: 1,
			FailedValidationID:    validationID,
			PreRepairCheckpointID: checkpointID,
			ReasonRedacted:        "bounded failure", IdempotencyKey: "repair-80",
		},
	)
	if err != nil || revision != repairRevision {
		t.Fatalf("repair revision = %d, %v", revision, err)
	}
	if err := store.RecordRepairOutcome(
		t.Context(),
		repairOutcomeRecord{
			TaskID: taskID, RunID: runID, PlanRevision: 2,
			RepairAttemptRevision:     repairRevision,
			Outcome:                   repairBudgetExhausted,
			PostRepairValidationID:    &validationID,
			UnresolvedSummaryRedacted: "budget exhausted after rerun",
			IdempotencyKey:            "repair-outcome-80",
		},
	); err != nil {
		t.Fatal(err)
	}
	completionRevision, state, err := store.RecordCompletion(
		t.Context(),
		completionCandidateRecord{
			TaskID: taskID, RunID: runID, PlanRevision: 2,
			ExpectedTaskRevision: 9, ExpectedRunRevision: 10,
			EventID: eventID, EventIdempotencyKey: "completion-event-80",
			RepositoryStatusJSON:   `{"clean":true}`,
			DiffSummaryJSON:        `{"summary":"bounded"}`,
			DiffSHA256:             strings.Repeat("a", 64),
			ValidationSummaryJSON:  `{"required_passed":true}`,
			BudgetSummaryJSON:      `{"actual":1}`,
			AssumptionsJSON:        `[]`,
			LimitationsJSON:        `[]`,
			ImplementationComplete: true,
			ValidationComplete:     true,
			IdempotencyKey:         "completion-80",
		},
	)
	if err != nil || completionRevision != 4 ||
		state != domain.TaskStateAwaitingReview {
		t.Fatalf(
			"completion = %d, %s, %v",
			completionRevision,
			state,
			err,
		)
	}
	state, err = store.RecordReviewDecision(
		t.Context(),
		reviewDecisionRecord{
			TaskID: taskID, RunID: runID, PlanRevision: 2,
			CompletionRevision: 4, ExpectedTaskRevision: 10,
			ExpectedRunRevision: 11, EventID: eventID,
			EventIdempotencyKey: "review-event-80",
			Decision:            agentloop.ReviewDecisionAbandon,
			Actor:               "user:80", AuthorityReference: "review:80",
			ReasonRedacted: "user abandoned", IdempotencyKey: "review-80",
		},
	)
	if err != nil || state != domain.TaskStateCancelled {
		t.Fatalf("review state = %s, %v", state, err)
	}

	if repositories.validation.CommandExecutionID == nil ||
		*repositories.validation.CommandExecutionID != commandExecutionID ||
		repositories.validation.RepairAttemptRevision == nil ||
		*repositories.validation.RepairAttemptRevision != repairRevision ||
		!repositories.validation.ValidationPassed ||
		!slices.Equal(
			repositories.validation.PlanStepIDs,
			[]string{"step-80"},
		) ||
		repositories.repair.Ordinal != 1 ||
		repositories.outcome.Outcome != storage.RepairOutcomeBudgetExhausted ||
		repositories.completion.ExpectedTaskRevision != 9 ||
		repositories.completion.ExpectedRunRevision != 10 ||
		repositories.review.Decision != storage.TaskReviewAbandon ||
		repositories.review.EventID != eventID ||
		len(repositories.profile.Commands) != 1 ||
		repositories.profile.Commands[0].PlanCommand !=
			profile.Commands[0].PlanCommand ||
		!slices.Equal(
			repositories.profile.Commands[0].RelevantChangedFiles,
			[]string{"internal/agent/repair_completion.go"},
		) {
		t.Fatalf("adapter mapping = %#v", repositories)
	}
}

func TestRepairCompletionPreservesCheckpointAndRerunsExactProfile(t *testing.T) {
	fixture := newRepairCompletionFixture(t)
	profile := fixture.profile
	second := cloneValidationCommand(profile.Commands[0])
	second.ID = "go-build"
	second.Request.ID = "validation-go-build"
	second.Request.Name = executor.ToolBuild
	second.Request.IdempotencyKey = "validation-go-build"
	planCommand, err := agentloop.RenderValidationPlanCommand(
		second.Request,
	)
	if err != nil {
		t.Fatal(err)
	}
	second.PlanCommand = planCommand
	profile.Commands = append(profile.Commands, second)
	fixture.runner.results = []ValidationCommandRun{
		failedValidationRun(t, 10),
		passedValidationRun(t, 11),
		passedValidationRun(t, 12),
		passedValidationRun(t, 13),
	}
	fixture.repairs.results = []RepairResult{
		{ChangedFiles: []string{"internal/agent/repaired.go"}},
	}
	service := fixture.service(t)
	input := fixture.validationInput(2)
	input.Profile = profile
	outcome, err := service.ValidateAndRepair(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.ReadyForCompletion ||
		outcome.RepairRounds != 1 ||
		len(outcome.Reports) != 2 ||
		len(fixture.checkpoints.calls) != 1 ||
		len(fixture.repairs.calls) != 1 ||
		len(fixture.store.repairs) != 1 ||
		len(fixture.store.repairOutcomes) != 1 {
		t.Fatalf("repair outcome = %#v, fixture = %#v", outcome, fixture)
	}
	gotCommands := fixture.runner.commandIDs
	wantCommands := []string{"go-test", "go-build", "go-test", "go-build"}
	if fmt.Sprint(gotCommands) != fmt.Sprint(wantCommands) {
		t.Fatalf("validation rerun commands = %#v, want %#v", gotCommands, wantCommands)
	}
	if outcome.Reports[0].ProfileDigest != outcome.Reports[1].ProfileDigest ||
		outcome.Reports[0].Executions[0].CommandFingerprint !=
			outcome.Reports[1].Executions[0].CommandFingerprint {
		t.Fatal("repair rerun changed the selected validation floor")
	}
	firstFailure := outcome.Reports[0].Executions[0].Failure
	if firstFailure == nil ||
		strings.Contains(firstFailure.SummaryRedacted, "ABCDEFGHIJKLMNOP") ||
		fixture.store.repairs[0].PreRepairCheckpointID.IsZero() ||
		fixture.store.repairs[0].ReasonRedacted == "" {
		t.Fatalf(
			"repair failure/checkpoint attribution = %#v / %#v",
			firstFailure,
			fixture.store.repairs[0],
		)
	}
}

func TestRepairCompletionStopsWhenBudgetExhaustsBetweenRounds(t *testing.T) {
	fixture := newRepairCompletionFixture(t)
	fixture.runner.results = []ValidationCommandRun{
		failedValidationRun(t, 20),
		failedValidationRun(t, 21),
	}
	fixture.repairs.results = []RepairResult{{}}
	fixture.control.states = []agentloop.ControlState{
		{
			Disposition:     agentloop.ControlActive,
			BudgetAvailable: true,
			PolicyCurrent:   true,
		},
		{
			Disposition:     agentloop.ControlActive,
			BudgetAvailable: false,
			PolicyCurrent:   true,
		},
	}
	service := fixture.service(t)
	outcome, err := service.ValidateAndRepair(
		t.Context(),
		fixture.validationInput(2),
	)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.ReadyForCompletion ||
		!outcome.BudgetExhausted ||
		outcome.RepairRounds != 1 ||
		outcome.UnresolvedReason != ErrRepairBudgetExhausted.Error() ||
		len(fixture.checkpoints.calls) != 1 ||
		len(fixture.repairs.calls) != 1 ||
		len(fixture.store.repairOutcomes) != 1 ||
		fixture.store.repairOutcomes[0].Outcome != repairBudgetExhausted {
		t.Fatalf("budget stop outcome = %#v, fixture = %#v", outcome, fixture)
	}
	if _, err := service.PrepareCompletion(
		t.Context(),
		CompletionInput{
			TaskID: fixture.taskID, RunID: fixture.runID,
			PlanRevision: 1,
		},
	); !errors.Is(err, ErrRequiredValidationIncomplete) {
		t.Fatalf("budget-exhausted completion error = %v", err)
	}
}

func TestRepairCompletionReportsUnresolvedFailureAtRepairLimit(t *testing.T) {
	fixture := newRepairCompletionFixture(t)
	fixture.runner.results = []ValidationCommandRun{
		failedValidationRun(t, 30),
		failedValidationRun(t, 31),
	}
	fixture.repairs.results = []RepairResult{{}}
	service := fixture.service(t)
	outcome, err := service.ValidateAndRepair(
		t.Context(),
		fixture.validationInput(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.ReadyForCompletion ||
		outcome.BudgetExhausted ||
		outcome.RepairRounds != 1 ||
		!strings.Contains(outcome.UnresolvedReason, "repair limit reached") ||
		len(fixture.store.repairOutcomes) != 1 ||
		fixture.store.repairOutcomes[0].Outcome != repairValidationFailed {
		t.Fatalf("repair-limit outcome = %#v", outcome)
	}
}

type repairCompletionFixture struct {
	taskID      domain.TaskID
	runID       domain.RunID
	profile     agentloop.ValidationProfile
	runner      *repairValidationRunnerStub
	checkpoints *repairCheckpointStub
	repairs     *repairExecutorStub
	control     *repairControlStub
	store       *repairCompletionStoreStub
	repository  *repairRepositoryStub
	budget      *repairBudgetStub
	redactor    *redact.Pipeline
}

func newRepairCompletionFixture(t *testing.T) *repairCompletionFixture {
	t.Helper()
	taskID := repairCoordinatorTaskID(t, 100)
	runID := repairCoordinatorRunID(t, 101)
	pipeline, err := redact.NewPipeline(nil, redact.Limits{
		MaximumInputBytes: 1 << 20, MaximumOutputBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pipeline.Close)
	fixture := &repairCompletionFixture{
		taskID: taskID, runID: runID,
		profile:     repairCoordinatorProfile(taskID, runID),
		runner:      &repairValidationRunnerStub{},
		checkpoints: &repairCheckpointStub{},
		repairs:     &repairExecutorStub{},
		control: &repairControlStub{
			states: []agentloop.ControlState{
				{
					Disposition:     agentloop.ControlActive,
					BudgetAvailable: true,
					PolicyCurrent:   true,
				},
			},
		},
		store: &repairCompletionStoreStub{},
		repository: &repairRepositoryStub{
			summary: agentloop.RepositoryCompletionSummary{
				StatusRedacted: "M internal/agent/repair_completion.go",
				DiffRedacted: `diff includes OPENAI_API_KEY="` +
					`sk-proj-ABCDEFGHIJKLMNOPQRSTUVWX"`,
				DiffSHA256:   strings.Repeat("a", 64),
				ChangedFiles: []string{"internal/agent/repair_completion.go"},
			},
		},
		budget: &repairBudgetStub{
			summary: repairCoordinatorBudgetSummary(),
		},
		redactor: pipeline,
	}
	fixture.checkpoints.ids = []domain.CheckpointID{
		repairCoordinatorCheckpointID(t, 102),
		repairCoordinatorCheckpointID(t, 103),
	}
	return fixture
}

func (fixture *repairCompletionFixture) service(
	t *testing.T,
) *RepairCompletionService {
	t.Helper()
	service, err := NewRepairCompletionService(RepairCompletionDependencies{
		Validations: fixture.runner, Checkpoints: fixture.checkpoints,
		Repairs: fixture.repairs, Control: fixture.control,
		Store: fixture.store, Repository: fixture.repository,
		Budget: fixture.budget, Redactor: fixture.redactor,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func (fixture *repairCompletionFixture) validationInput(
	maximum uint32,
) ValidationRepairInput {
	return ValidationRepairInput{
		TaskID: fixture.taskID, RunID: fixture.runID,
		PlanRevision: 1, Profile: fixture.profile,
		ChangedFiles:        []string{"internal/agent/repair_completion.go"},
		MaximumRepairRounds: maximum,
		IdempotencyPrefix:   "repair-completion-fixture",
	}
}

type repairValidationRunnerStub struct {
	results    []ValidationCommandRun
	errors     []error
	commandIDs []string
}

func (stub *repairValidationRunnerStub) RunValidationCommand(
	_ context.Context,
	command agentloop.ValidationCommand,
	_ uint32,
) (ValidationCommandRun, error) {
	stub.commandIDs = append(stub.commandIDs, command.ID)
	index := len(stub.commandIDs) - 1
	var err error
	if index < len(stub.errors) {
		err = stub.errors[index]
	}
	if index >= len(stub.results) {
		return ValidationCommandRun{}, errors.New("unexpected validation call")
	}
	return stub.results[index], err
}

type repairCheckpointCall struct {
	ordinal uint32
	reason  string
}

type repairCheckpointStub struct {
	ids   []domain.CheckpointID
	calls []repairCheckpointCall
}

func (stub *repairCheckpointStub) CreatePreRepairCheckpoint(
	_ context.Context,
	_ domain.TaskID,
	_ domain.RunID,
	_ uint64,
	ordinal uint32,
	reason string,
) (domain.CheckpointID, error) {
	stub.calls = append(stub.calls, repairCheckpointCall{
		ordinal: ordinal, reason: reason,
	})
	index := len(stub.calls) - 1
	if index >= len(stub.ids) {
		return domain.CheckpointID{}, errors.New("unexpected checkpoint call")
	}
	return stub.ids[index], nil
}

type repairExecutorStub struct {
	results []RepairResult
	errors  []error
	calls   []RepairRequest
}

func (stub *repairExecutorStub) ExecuteRepair(
	_ context.Context,
	request RepairRequest,
) (RepairResult, error) {
	stub.calls = append(stub.calls, request)
	index := len(stub.calls) - 1
	var err error
	if index < len(stub.errors) {
		err = stub.errors[index]
	}
	if index >= len(stub.results) {
		return RepairResult{}, errors.New("unexpected repair call")
	}
	return stub.results[index], err
}

type repairControlStub struct {
	states []agentloop.ControlState
	calls  int
}

func (stub *repairControlStub) ReadControl(
	context.Context,
	domain.TaskID,
	domain.RunID,
) (agentloop.ControlState, error) {
	index := stub.calls
	stub.calls++
	if index >= len(stub.states) {
		index = len(stub.states) - 1
	}
	return stub.states[index], nil
}

type repairCompletionStoreStub struct {
	selectedProfile       selectedValidationProfileRecord
	attributions          []validationAttributionRecord
	repairs               []beginRepairRecord
	repairOutcomes        []repairOutcomeRecord
	completionInput       completionCandidateRecord
	completion            CompletionResult
	review                reviewDecisionRecord
	completionEvidence    *completionEvidenceRecord
	finalValidationReport *agentloop.ValidationReport
}

func (stub *repairCompletionStoreStub) BindValidationProfile(
	_ context.Context,
	record selectedValidationProfileRecord,
) error {
	stub.selectedProfile = record
	return nil
}

func (stub *repairCompletionStoreStub) RecordValidationAttribution(
	_ context.Context,
	record validationAttributionRecord,
) error {
	stub.attributions = append(stub.attributions, record)
	return nil
}

func (stub *repairCompletionStoreStub) ReadFinalValidationReport(
	context.Context,
	domain.TaskID,
	domain.RunID,
	uint64,
) (agentloop.ValidationReport, error) {
	if stub.finalValidationReport != nil {
		return *stub.finalValidationReport, nil
	}
	if stub.selectedProfile.ProfileDigest == "" ||
		len(stub.attributions) == 0 {
		return agentloop.ValidationReport{}, ErrRequiredValidationIncomplete
	}
	var finalRound uint32
	for _, attribution := range stub.attributions {
		if attribution.Round > finalRound {
			finalRound = attribution.Round
		}
	}
	report := agentloop.ValidationReport{
		ProfileName:    stub.selectedProfile.Profile.Name,
		ProfileVersion: stub.selectedProfile.Profile.Version,
		ProfileDigest:  stub.selectedProfile.ProfileDigest,
		Round:          finalRound,
	}
	for _, attribution := range stub.attributions {
		if attribution.Round != finalRound {
			continue
		}
		report.Executions = append(
			report.Executions,
			agentloop.ValidationExecution{
				ValidationID:       attribution.ValidationID,
				CommandID:          attribution.CommandID,
				CommandFingerprint: attribution.CommandFingerprint,
				Required:           attribution.Required,
				AcceptanceTest:     attribution.AcceptanceTest,
				PlanStepIDs:        append([]string{}, attribution.PlanStepIDs...),
				State:              attribution.State,
				CommandExecutionID: attribution.CommandExecutionID,
				Failure:            attribution.Failure,
			},
		)
	}
	if err := report.Validate(); err != nil {
		return agentloop.ValidationReport{}, err
	}
	return report, nil
}

func (stub *repairCompletionStoreStub) ReadCompletionEvidence(
	_ context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
	planRevision uint64,
) (completionEvidenceRecord, error) {
	if stub.completionEvidence != nil {
		return *stub.completionEvidence, nil
	}
	evidence := completionEvidenceRecord{}
	seenSteps := make(map[string]struct{})
	for _, attribution := range stub.attributions {
		for _, stepID := range attribution.PlanStepIDs {
			evidence.ValidationAttributions = append(
				evidence.ValidationAttributions,
				completionValidationAttributionRecord{
					TaskID: taskID, RunID: runID,
					PlanRevision: planRevision,
					PlanStepID:   stepID,
					ValidationID: attribution.ValidationID,
				},
			)
			if !attribution.ValidationPassed {
				continue
			}
			if _, exists := seenSteps[stepID]; exists {
				continue
			}
			seenSteps[stepID] = struct{}{}
			evidence.PlanSteps = append(
				evidence.PlanSteps,
				completionPlanStepRecord{
					PlanStepID: stepID,
					State:      agentloop.StepValidated,
				},
			)
		}
	}
	return evidence, nil
}

func (stub *repairCompletionStoreStub) BeginRepair(
	_ context.Context,
	record beginRepairRecord,
) (uint64, error) {
	stub.repairs = append(stub.repairs, record)
	return uint64(len(stub.repairs)), nil
}

func (stub *repairCompletionStoreStub) RecordRepairOutcome(
	_ context.Context,
	record repairOutcomeRecord,
) error {
	stub.repairOutcomes = append(stub.repairOutcomes, record)
	return nil
}

func (stub *repairCompletionStoreStub) RecordCompletion(
	_ context.Context,
	record completionCandidateRecord,
) (uint64, domain.TaskState, error) {
	stub.completionInput = record
	stub.completion = CompletionResult{
		Revision: 1, State: domain.TaskStateAwaitingReview,
	}
	return stub.completion.Revision, stub.completion.State, nil
}

func (stub *repairCompletionStoreStub) RecordReviewDecision(
	_ context.Context,
	record reviewDecisionRecord,
) (domain.TaskState, error) {
	stub.review = record
	return record.Decision.ResultingTaskState()
}

type repairRepositoryStub struct {
	summary agentloop.RepositoryCompletionSummary
}

func (stub *repairRepositoryStub) InspectFinalRepository(
	context.Context,
	domain.TaskID,
	domain.RunID,
) (agentloop.RepositoryCompletionSummary, error) {
	return stub.summary, nil
}

type repairBudgetStub struct {
	summary agentloop.BudgetCompletionSummary
}

func (stub *repairBudgetStub) InspectFinalBudget(
	context.Context,
	domain.TaskID,
	domain.RunID,
) (agentloop.BudgetCompletionSummary, error) {
	return stub.summary, nil
}

func passedValidationRun(
	t *testing.T,
	number int,
) ValidationCommandRun {
	t.Helper()
	return ValidationCommandRun{
		ValidationID:       repairCoordinatorValidationID(t, number),
		CommandExecutionID: fmt.Sprintf("command-%d", number),
		Result: executor.ToolResult{
			RequestID:     fmt.Sprintf("validation-%d", number),
			SchemaVersion: executor.ToolSchemaVersion,
			State:         "succeeded", ExitCode: 0,
			Summary: "validation passed",
		},
	}
}

func failedValidationRun(
	t *testing.T,
	number int,
) ValidationCommandRun {
	t.Helper()
	return ValidationCommandRun{
		ValidationID:       repairCoordinatorValidationID(t, number),
		CommandExecutionID: fmt.Sprintf("command-%d", number),
		Result: executor.ToolResult{
			RequestID:     fmt.Sprintf("validation-%d", number),
			SchemaVersion: executor.ToolSchemaVersion,
			State:         "failed", ExitCode: 1,
			Summary: "validation failed",
			StderrRedacted: `internal/agent/repair_completion.go:42 ` +
				`OPENAI_API_KEY="sk-proj-ABCDEFGHIJKLMNOPQRSTUVWX"`,
		},
	}
}

func repairCoordinatorProfile(
	taskID domain.TaskID,
	runID domain.RunID,
) agentloop.ValidationProfile {
	command := agentloop.ValidationCommand{
		ID: "go-test", Required: true, AcceptanceTest: true,
		Request: executor.ToolRequest{
			SchemaVersion: executor.ToolSchemaVersion,
			ID:            "validation-go-test", TaskID: taskID, RunID: runID,
			Name: executor.ToolTest,
			Arguments: []executor.ToolArgument{
				{Name: "executable", Value: "go"},
				{Name: "argument", Value: "test"},
				{Name: "argument", Value: "./..."},
			},
			WorkingDirectory: `C:\fixture\worktree`,
			Timeout:          time.Minute,
			ClaimedAuthority: executor.AuthorityAutomaticRead,
			ExpectedSideEffects: []executor.SideEffect{
				executor.EffectSubprocess,
				executor.EffectRepositoryRead,
			},
			IdempotencyKey: "validation-go-test",
			Requester:      "fixed-agent",
		},
		RelevantChangedFiles: []string{
			"internal/agent/repair_completion.go",
		},
		PlanStepIDs: []string{"step-validation"},
	}
	planCommand, err := agentloop.RenderValidationPlanCommand(command.Request)
	if err != nil {
		panic(err)
	}
	command.PlanCommand = planCommand
	return agentloop.ValidationProfile{
		Name: "routine-v1", Version: "v1",
		Commands: []agentloop.ValidationCommand{
			command,
		},
	}
}

func repairCoordinatorBudgetSummary() agentloop.BudgetCompletionSummary {
	currency := domain.CurrencyCode("USD")
	return agentloop.BudgetCompletionSummary{
		Forecast: agentloop.ExactCostSummary{
			Known: true, Numerator: 2, Denominator: 1, Currency: currency,
		},
		Reserved: agentloop.ExactCostSummary{
			Known: true, Numerator: 0, Denominator: 1, Currency: currency,
		},
		Actual: agentloop.ExactCostSummary{
			Known: true, Numerator: 1, Denominator: 1, Currency: currency,
		},
		Remaining: agentloop.ExactCostSummary{
			Known: true, Numerator: 4, Denominator: 1, Currency: currency,
		},
	}
}

func repairCoordinatorTaskID(t *testing.T, number int) domain.TaskID {
	t.Helper()
	id, err := domain.ParseTaskID(
		"tsk_01890f3c-4a00-7abc-8def-" +
			fmt.Sprintf("%012x", number),
	)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func repairCoordinatorRunID(t *testing.T, number int) domain.RunID {
	t.Helper()
	id, err := domain.ParseRunID(
		"run_01890f3c-4a00-7abc-8def-" +
			fmt.Sprintf("%012x", number),
	)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func repairCoordinatorValidationID(
	t *testing.T,
	number int,
) domain.ValidationID {
	t.Helper()
	id, err := domain.ParseValidationID(
		"val_01890f3c-4a00-7abc-8def-" +
			fmt.Sprintf("%012x", number),
	)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func repairCoordinatorCheckpointID(
	t *testing.T,
	number int,
) domain.CheckpointID {
	t.Helper()
	id, err := domain.ParseCheckpointID(
		"ckp_01890f3c-4a00-7abc-8def-" +
			fmt.Sprintf("%012x", number),
	)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func repairCoordinatorEventID(t *testing.T, number int) domain.EventID {
	t.Helper()
	id, err := domain.ParseEventID(
		"evt_01890f3c-4a00-7abc-8def-" +
			fmt.Sprintf("%012x", number),
	)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type recordingRepairCompletionRepositories struct {
	profile           storage.BindRunValidationProfile
	validation        storage.RecordPlanValidationAttributions
	repair            storage.BeginRepairAttempt
	outcome           storage.RecordRepairAttemptOutcome
	completion        storage.RecordCompletionCandidate
	review            storage.RecordTaskReviewDecision
	selectedEvidence  storage.SelectedValidationProfileEvidence
	durableExecutions []storage.DurableValidationExecution
}

func (repositories *recordingRepairCompletionRepositories) BindRunValidationProfile(
	_ context.Context,
	input storage.BindRunValidationProfile,
) (storage.SelectedValidationProfileEvidence, error) {
	repositories.profile = input
	return storage.SelectedValidationProfileEvidence{}, nil
}

func (repositories *recordingRepairCompletionRepositories) GetRunValidationProfile(
	_ context.Context,
	_ domain.RunID,
) (storage.SelectedValidationProfileEvidence, error) {
	return repositories.selectedEvidence, nil
}

func (repositories *recordingRepairCompletionRepositories) ListDurableValidationExecutions(
	_ context.Context,
	_ domain.RunID,
) ([]storage.DurableValidationExecution, error) {
	return append(
		[]storage.DurableValidationExecution{},
		repositories.durableExecutions...,
	), nil
}

func (repositories *recordingRepairCompletionRepositories) RecordPlanValidationAttributions(
	_ context.Context,
	input storage.RecordPlanValidationAttributions,
) ([]storage.PlanValidationAttribution, error) {
	repositories.validation = input
	return nil, nil
}

func (*recordingRepairCompletionRepositories) GetPlanRevision(
	context.Context,
	domain.TaskID,
	uint64,
) (storage.PlanRevision, error) {
	return storage.PlanRevision{}, nil
}

func (*recordingRepairCompletionRepositories) ListPlanStepStates(
	context.Context,
	domain.RunID,
) ([]storage.PlanStepStatus, error) {
	return nil, nil
}

func (*recordingRepairCompletionRepositories) ListPlanValidationAttributions(
	context.Context,
	domain.RunID,
) ([]storage.PlanValidationAttribution, error) {
	return nil, nil
}

func (repositories *recordingRepairCompletionRepositories) BeginRepairAttempt(
	_ context.Context,
	input storage.BeginRepairAttempt,
) (storage.RepairAttempt, error) {
	repositories.repair = input
	return storage.RepairAttempt{Revision: 3}, nil
}

func (repositories *recordingRepairCompletionRepositories) RecordRepairAttemptOutcome(
	_ context.Context,
	input storage.RecordRepairAttemptOutcome,
) (storage.RepairAttemptOutcome, error) {
	repositories.outcome = input
	return storage.RepairAttemptOutcome{}, nil
}

func (repositories *recordingRepairCompletionRepositories) RecordCompletionCandidate(
	_ context.Context,
	input storage.RecordCompletionCandidate,
) (storage.CompletionCandidate, error) {
	repositories.completion = input
	return storage.CompletionCandidate{Revision: 4}, nil
}

func (repositories *recordingRepairCompletionRepositories) RecordTaskReviewDecision(
	_ context.Context,
	input storage.RecordTaskReviewDecision,
) (storage.TaskReviewDecision, error) {
	repositories.review = input
	return storage.TaskReviewDecision{}, nil
}
