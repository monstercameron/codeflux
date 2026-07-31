package storage

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
)

func TestAgentExecutionRepairAndCompletionAttributionLifecycle(t *testing.T) {
	fixture := createBoundAgentRunFixture(t, 5200)
	modelRequest := createAgentModelRequestFixture(t, fixture)
	stepID := fixture.plan.Plan.Steps[0].ID

	started, err := fixture.repositories.RecordPlanStepTransition(
		t.Context(),
		RecordPlanStepTransition{
			ID: "step-started", TaskID: fixture.task.ID,
			RunID: fixture.runID, PlanRevision: fixture.plan.Revision,
			PlanStepID: stepID, From: PlanStepPending, To: PlanStepInProgress,
			ReasonRedacted: "begin approved plan step",
			ModelRequestID: modelRequest.ID,
			IdempotencyKey: "step-started",
		},
	)
	if err != nil || started.To != PlanStepInProgress {
		t.Fatalf("started step = %#v, %v", started, err)
	}
	if started.Sequence != 1 {
		t.Fatalf("first transition sequence = %d", started.Sequence)
	}
	if _, err := fixture.repositories.RecordPlanStepTransition(
		t.Context(),
		RecordPlanStepTransition{
			ID: "step-started", TaskID: fixture.task.ID,
			RunID: fixture.runID, PlanRevision: fixture.plan.Revision,
			PlanStepID: stepID, From: PlanStepPending, To: PlanStepInProgress,
			ReasonRedacted: "changed retry reason",
			ModelRequestID: modelRequest.ID,
			IdempotencyKey: "step-started",
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed transition retry error = %v", err)
	}
	request, err := fixture.repositories.RecordAgentToolRequest(
		t.Context(),
		RecordAgentToolRequest{
			ID: "agent-tool-request", TaskID: fixture.task.ID,
			RunID: fixture.runID, PlanRevision: fixture.plan.Revision,
			PlanStepID: stepID, ModelRequestID: modelRequest.ID,
			ToolName: "workspace.apply-edit", ToolSchemaVersion: 1,
			ArgumentsRedactedJSON: `{"path":"internal/widget.go"}`,
			ArgumentsSHA256:       strings.Repeat("a", 64),
			IdempotencyKey:        "agent-tool-request",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.RecordAgentToolRequest(
		t.Context(),
		RecordAgentToolRequest{
			ID: "agent-tool-request", TaskID: fixture.task.ID,
			RunID: fixture.runID, PlanRevision: fixture.plan.Revision,
			PlanStepID: stepID, ModelRequestID: modelRequest.ID,
			ToolName: "workspace.read", ToolSchemaVersion: 1,
			ArgumentsRedactedJSON: `{"path":"internal/widget.go"}`,
			ArgumentsSHA256:       strings.Repeat("a", 64),
			IdempotencyKey:        "agent-tool-request",
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed tool request retry error = %v", err)
	}
	result, err := fixture.repositories.RecordAgentToolResult(
		t.Context(),
		RecordAgentToolResult{
			ID: "agent-tool-result", ToolRequestID: request.ID,
			State:              AgentToolResultSucceeded,
			ResultRedactedJSON: `{"changed":true}`,
			ResultSHA256:       hashJSON(`{"changed":true}`),
			IdempotencyKey:     "agent-tool-result",
		},
	)
	if err != nil || result.State != AgentToolResultSucceeded {
		t.Fatalf("tool result = %#v, %v", result, err)
	}
	if _, err := fixture.repositories.RecordAgentToolResult(
		t.Context(),
		RecordAgentToolResult{
			ID: "agent-tool-result", ToolRequestID: request.ID,
			State:              AgentToolResultSucceeded,
			ResultRedactedJSON: `{"changed":false}`,
			ResultSHA256:       hashJSON(`{"changed":false}`),
			IdempotencyKey:     "agent-tool-result",
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed tool result retry error = %v", err)
	}
	if _, err := fixture.repositories.RecordAgentToolResult(
		t.Context(),
		RecordAgentToolResult{
			ID: "other-tool-result", ToolRequestID: request.ID,
			State:              AgentToolResultSucceeded,
			ResultRedactedJSON: `{"changed":false}`,
			ResultSHA256:       hashJSON(`{"changed":true}`),
			IdempotencyKey:     "other-tool-result",
		},
	); err == nil {
		t.Fatal("inconsistent tool result digest unexpectedly accepted")
	}
	requestID := request.ID
	implemented, err := fixture.repositories.RecordPlanStepTransition(
		t.Context(),
		RecordPlanStepTransition{
			ID: "step-implemented", TaskID: fixture.task.ID,
			RunID: fixture.runID, PlanRevision: fixture.plan.Revision,
			PlanStepID: stepID, From: PlanStepInProgress,
			To:             PlanStepImplemented,
			ReasonRedacted: "bounded edit completed",
			ModelRequestID: modelRequest.ID, ToolRequestID: &requestID,
			IdempotencyKey: "step-implemented",
		},
	)
	if err != nil || implemented.To != PlanStepImplemented {
		t.Fatalf("implemented step = %#v, %v", implemented, err)
	}
	if implemented.Sequence != 2 {
		t.Fatalf("second transition sequence = %d", implemented.Sequence)
	}
	statuses, err := fixture.repositories.ListPlanStepStates(
		t.Context(),
		fixture.runID,
	)
	if err != nil || len(statuses) != 1 ||
		statuses[0].State != PlanStepImplemented ||
		statuses[0].Sequence != 2 ||
		statuses[0].TaskID != fixture.task.ID ||
		statuses[0].RunID != fixture.runID ||
		statuses[0].PlanRevision != fixture.plan.Revision {
		t.Fatalf("step statuses = %#v, %v", statuses, err)
	}

	checkpoint, err := fixture.repositories.CreateCheckpoint(
		t.Context(),
		CreateCheckpoint{
			ID: testCheckpointID(t, 5260), TaskID: fixture.task.ID,
			RunID: &fixture.runID, State: domain.CheckpointStateReady,
			RepositoryRevision: strings.Repeat("8", 40),
			WorktreeDiffHash:   strings.Repeat("c", 64),
			EventSequence:      1, IdempotencyKey: "pre-repair-checkpoint",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	failedValidation, err := fixture.repositories.CreateValidation(
		t.Context(),
		CreateValidation{
			ID: testValidationID(t, 5261), TaskID: fixture.task.ID,
			RunID: &fixture.runID, State: domain.ValidationStateFailed,
			Severity:        domain.ValidationSeverityBlocking,
			ProfileName:     string(fixture.plan.ValidationProfile),
			SummaryRedacted: stringPointer("targeted test failed"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.RecordPlanValidationAttribution(
		t.Context(),
		RecordPlanValidationAttribution{
			TaskID: fixture.task.ID, RunID: fixture.runID,
			PlanRevision: fixture.plan.Revision, PlanStepID: stepID,
			ValidationID:   failedValidation.ID,
			IdempotencyKey: "failed-validation-attribution",
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.RecordPlanValidationAttribution(
		t.Context(),
		RecordPlanValidationAttribution{
			TaskID: fixture.task.ID, RunID: fixture.runID,
			PlanRevision: fixture.plan.Revision + 1, PlanStepID: stepID,
			ValidationID:   failedValidation.ID,
			IdempotencyKey: "failed-validation-attribution",
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed validation attribution retry error = %v", err)
	}
	repair, err := fixture.repositories.BeginRepairAttempt(
		t.Context(),
		BeginRepairAttempt{
			TaskID: fixture.task.ID, RunID: fixture.runID,
			PlanRevision: fixture.plan.Revision, Ordinal: 1,
			FailedValidationID:    failedValidation.ID,
			PreRepairCheckpointID: checkpoint.ID,
			ReasonRedacted:        "repair the observed targeted failure",
			IdempotencyKey:        "repair-attempt",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.BeginRepairAttempt(
		t.Context(),
		BeginRepairAttempt{
			TaskID: fixture.task.ID, RunID: fixture.runID,
			PlanRevision: fixture.plan.Revision, Ordinal: 1,
			FailedValidationID:    failedValidation.ID,
			PreRepairCheckpointID: checkpoint.ID,
			ReasonRedacted:        "changed retry reason",
			IdempotencyKey:        "repair-attempt",
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed repair retry error = %v", err)
	}
	passedValidation, err := fixture.repositories.CreateValidation(
		t.Context(),
		CreateValidation{
			ID: testValidationID(t, 5262), TaskID: fixture.task.ID,
			RunID: &fixture.runID, State: domain.ValidationStatePassed,
			Severity:        domain.ValidationSeverityBlocking,
			ProfileName:     string(fixture.plan.ValidationProfile),
			SummaryRedacted: stringPointer("targeted test passed after repair"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := fixture.repositories.RecordRepairAttemptOutcome(
		t.Context(),
		RecordRepairAttemptOutcome{
			TaskID: fixture.task.ID, RunID: fixture.runID,
			PlanRevision:              fixture.plan.Revision,
			RepairAttemptRevision:     repair.Revision,
			Outcome:                   RepairOutcomeValidationPassed,
			PostRepairValidationID:    &passedValidation.ID,
			UnresolvedSummaryRedacted: "no unresolved targeted failures",
			IdempotencyKey:            "repair-outcome",
		},
	)
	if err != nil || outcome.Outcome != RepairOutcomeValidationPassed {
		t.Fatalf("repair outcome = %#v, %v", outcome, err)
	}
	if _, err := fixture.repositories.RecordRepairAttemptOutcome(
		t.Context(),
		RecordRepairAttemptOutcome{
			TaskID: fixture.task.ID, RunID: fixture.runID,
			PlanRevision:              fixture.plan.Revision,
			RepairAttemptRevision:     repair.Revision,
			Outcome:                   RepairOutcomeValidationPassed,
			PostRepairValidationID:    &passedValidation.ID,
			UnresolvedSummaryRedacted: "changed retry summary",
			IdempotencyKey:            "repair-outcome",
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed repair outcome retry error = %v", err)
	}
	repairRevision := repair.Revision
	if _, err := fixture.repositories.RecordPlanValidationAttribution(
		t.Context(),
		RecordPlanValidationAttribution{
			TaskID: fixture.task.ID, RunID: fixture.runID,
			PlanRevision: fixture.plan.Revision, PlanStepID: stepID,
			ValidationID:          passedValidation.ID,
			RepairAttemptRevision: &repairRevision,
			IdempotencyKey:        "passed-validation-attribution",
		},
	); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.repositories.database.sql.ExecContext(
		t.Context(),
		`UPDATE runs SET state = 'validating', revision = revision + 1
		 WHERE id = ? AND state = 'starting'`,
		fixture.runID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.database.sql.ExecContext(
		t.Context(),
		`UPDATE tasks SET state = 'validating', revision = revision + 1
		 WHERE id = ? AND state = 'running'`,
		fixture.task.ID,
	); err != nil {
		t.Fatal(err)
	}
	currentTask, err := fixture.repositories.GetTask(t.Context(), fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var runRevision uint64
	if err := fixture.repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT revision FROM runs WHERE id = ?`,
		fixture.runID,
	).Scan(&runRevision); err != nil {
		t.Fatal(err)
	}
	candidateInput := RecordCompletionCandidate{
		TaskID: fixture.task.ID, RunID: fixture.runID,
		PlanRevision:           fixture.plan.Revision,
		ExpectedTaskRevision:   currentTask.Revision,
		ExpectedRunRevision:    runRevision,
		EventID:                testEventID(t, 5270),
		EventIdempotencyKey:    "completion-event",
		RepositoryStatusJSON:   `{"clean":false}`,
		DiffSummaryJSON:        `{"files":["internal/widget.go"]}`,
		DiffSHA256:             strings.Repeat("d", 64),
		ValidationSummaryJSON:  `{"passed":true}`,
		BudgetSummaryJSON:      `{"known":true}`,
		AssumptionsJSON:        `["smallest in-scope change"]`,
		LimitationsJSON:        `[]`,
		ImplementationComplete: true, ValidationComplete: true,
		IdempotencyKey: "completion-candidate",
	}
	candidate, err := fixture.repositories.RecordCompletionCandidate(
		t.Context(),
		candidateInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	changedCandidate := candidateInput
	changedCandidate.EventIdempotencyKey = "changed-completion-event"
	if _, err := fixture.repositories.RecordCompletionCandidate(
		t.Context(),
		changedCandidate,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed completion retry error = %v", err)
	}
	currentTask, err = fixture.repositories.GetTask(t.Context(), fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if currentTask.State != domain.TaskStateAwaitingReview {
		t.Fatalf("task after completion candidate = %#v", currentTask)
	}
	decisionInput := RecordTaskReviewDecision{
		TaskID: fixture.task.ID, RunID: fixture.runID,
		PlanRevision:         fixture.plan.Revision,
		CompletionRevision:   candidate.Revision,
		ExpectedTaskRevision: currentTask.Revision,
		ExpectedRunRevision:  runRevision,
		Decision:             TaskReviewAccept,
		ActorReference:       "user:fixture",
		AuthorityReference:   "review:fixture",
		ReasonRedacted:       "validated change accepted",
		EventID:              testEventID(t, 5271),
		EventIdempotencyKey:  "review-event",
		IdempotencyKey:       "review-decision",
	}
	decision, err := fixture.repositories.RecordTaskReviewDecision(
		t.Context(),
		decisionInput,
	)
	if err != nil || decision.Decision != TaskReviewAccept {
		t.Fatalf("review decision = %#v, %v", decision, err)
	}
	changedDecision := decisionInput
	changedDecision.ActorReference = "user:other"
	if _, err := fixture.repositories.RecordTaskReviewDecision(
		t.Context(),
		changedDecision,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed review retry error = %v", err)
	}
	currentTask, err = fixture.repositories.GetTask(t.Context(), fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var runState domain.RunState
	if err := fixture.repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT state FROM runs WHERE id = ?`,
		fixture.runID,
	).Scan(&runState); err != nil {
		t.Fatal(err)
	}
	if currentTask.State != domain.TaskStateCompleted ||
		runState != domain.RunStateCompleted {
		t.Fatalf("final task/run = %#v / %s", currentTask, runState)
	}
	if _, err := fixture.repositories.database.sql.ExecContext(
		t.Context(),
		`DELETE FROM task_review_decisions
		 WHERE task_id = ? AND revision = ?`,
		fixture.task.ID,
		decision.Revision,
	); err == nil {
		t.Fatal("immutable review decision delete unexpectedly succeeded")
	}
	if _, err := fixture.repositories.database.sql.ExecContext(
		t.Context(),
		`DELETE FROM agent_plan_step_transitions WHERE id = 'step-started'`,
	); err == nil {
		t.Fatal("immutable plan step transition delete unexpectedly succeeded")
	}
}

func TestTaskReviewDecisionsApplyAtomicLifecycle(t *testing.T) {
	tests := []struct {
		name      string
		decision  TaskReviewDecisionKind
		taskState domain.TaskState
		runState  domain.RunState
	}{
		{
			name: "accept", decision: TaskReviewAccept,
			taskState: domain.TaskStateCompleted,
			runState:  domain.RunStateCompleted,
		},
		{
			name: "request-repair", decision: TaskReviewRequestRepair,
			taskState: domain.TaskStateRunning,
			runState:  domain.RunStateRunning,
		},
		{
			name: "rollback", decision: TaskReviewRollback,
			taskState: domain.TaskStateRolledBack,
			runState:  domain.RunStateCompleted,
		},
		{
			name: "abandon", decision: TaskReviewAbandon,
			taskState: domain.TaskStateCancelled,
			runState:  domain.RunStateCancelled,
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := createBoundAgentRunFixture(t, 5400+index*100)
			if _, err := fixture.repositories.database.sql.ExecContext(
				t.Context(),
				`UPDATE runs SET state = 'validating', revision = revision + 1
				 WHERE id = ? AND state = 'starting'`,
				fixture.runID,
			); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.repositories.database.sql.ExecContext(
				t.Context(),
				`UPDATE tasks SET state = 'validating', revision = revision + 1
				 WHERE id = ? AND state = 'running'`,
				fixture.task.ID,
			); err != nil {
				t.Fatal(err)
			}
			task, err := fixture.repositories.GetTask(
				t.Context(),
				fixture.task.ID,
			)
			if err != nil {
				t.Fatal(err)
			}
			var runRevision uint64
			if err := fixture.repositories.database.sql.QueryRowContext(
				t.Context(),
				`SELECT revision FROM runs WHERE id = ?`,
				fixture.runID,
			).Scan(&runRevision); err != nil {
				t.Fatal(err)
			}
			candidate, err := fixture.repositories.RecordCompletionCandidate(
				t.Context(),
				RecordCompletionCandidate{
					TaskID: fixture.task.ID, RunID: fixture.runID,
					PlanRevision:          fixture.plan.Revision,
					ExpectedTaskRevision:  task.Revision,
					ExpectedRunRevision:   runRevision,
					EventID:               testEventID(t, 9000+index*2),
					EventIdempotencyKey:   "review-case-completion-event-" + test.name,
					RepositoryStatusJSON:  `{"captured":true}`,
					DiffSummaryJSON:       `{"captured":true}`,
					DiffSHA256:            strings.Repeat("1", 64),
					ValidationSummaryJSON: `{"complete":true}`,
					BudgetSummaryJSON:     `{"captured":true}`,
					AssumptionsJSON:       `[]`, LimitationsJSON: `[]`,
					ImplementationComplete: true, ValidationComplete: true,
					IdempotencyKey: "review-case-completion-" + test.name,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			task, err = fixture.repositories.GetTask(
				t.Context(),
				fixture.task.ID,
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.repositories.RecordTaskReviewDecision(
				t.Context(),
				RecordTaskReviewDecision{
					TaskID: fixture.task.ID, RunID: fixture.runID,
					PlanRevision:         fixture.plan.Revision,
					CompletionRevision:   candidate.Revision,
					ExpectedTaskRevision: task.Revision,
					ExpectedRunRevision:  runRevision,
					Decision:             test.decision,
					ActorReference:       "user:fixture",
					AuthorityReference:   "review:" + test.name,
					ReasonRedacted:       "explicit review decision",
					EventID:              testEventID(t, 9001+index*2),
					EventIdempotencyKey:  "review-case-event-" + test.name,
					IdempotencyKey:       "review-case-" + test.name,
				},
			); err != nil {
				t.Fatal(err)
			}
			task, err = fixture.repositories.GetTask(
				t.Context(),
				fixture.task.ID,
			)
			if err != nil {
				t.Fatal(err)
			}
			var runState domain.RunState
			if err := fixture.repositories.database.sql.QueryRowContext(
				t.Context(),
				`SELECT state FROM runs WHERE id = ?`,
				fixture.runID,
			).Scan(&runState); err != nil {
				t.Fatal(err)
			}
			if task.State != test.taskState || runState != test.runState {
				t.Fatalf(
					"review lifecycle = %s/%s, want %s/%s",
					task.State, runState, test.taskState, test.runState,
				)
			}
		})
	}
}

func TestAgentToolRequestRejectsCrossRunModelAttribution(t *testing.T) {
	first := createBoundAgentRunFixture(t, 5900)
	request := createAgentModelRequestFixture(t, first)
	secondRunID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	_, micros := first.repositories.timestamp()
	if _, err := first.repositories.database.sql.ExecContext(
		t.Context(),
		`INSERT INTO runs (
			id, task_id, state, attempt, task_revision, idempotency_key,
			created_at_unix_micros, updated_at_unix_micros, revision
		) VALUES (?, ?, 'starting', 2, ?, 'cross-run', ?, ?, 0)`,
		secondRunID, first.task.ID, first.task.Revision, micros, micros,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := first.repositories.database.sql.ExecContext(
		t.Context(),
		`INSERT INTO run_execution_bindings (
			run_id, task_id, preflight_revision, policy_revision,
			forecast_revision, budget_id, budget_limit_revision,
			budget_snapshot_revision, created_at_unix_micros
		)
		SELECT ?, task_id, preflight_revision, policy_revision,
		       forecast_revision, budget_id, budget_limit_revision,
		       budget_snapshot_revision, ?
		FROM run_execution_bindings WHERE run_id = ?`,
		secondRunID, micros, first.runID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := first.repositories.BindRunPlan(
		t.Context(),
		BindRunPlan{
			RunID: secondRunID, TaskID: first.task.ID,
			PlanRevision:   first.plan.Revision,
			IdempotencyKey: "cross-run-plan",
		},
	); err != nil {
		t.Fatal(err)
	}
	_, err = first.repositories.RecordAgentToolRequest(
		t.Context(),
		RecordAgentToolRequest{
			ID: "cross-run-tool-request", TaskID: first.task.ID,
			RunID: secondRunID, PlanRevision: first.plan.Revision,
			PlanStepID:     first.plan.Plan.Steps[0].ID,
			ModelRequestID: request.ID,
			ToolName:       "workspace.read", ToolSchemaVersion: 1,
			ArgumentsRedactedJSON: `{}`,
			ArgumentsSHA256:       strings.Repeat("2", 64),
			IdempotencyKey:        "cross-run-tool-request",
		},
	)
	if err == nil {
		t.Fatal("cross-run model attribution unexpectedly succeeded")
	}
}

func TestPlanValidationAttributionsAreAtomicAndValidateEveryLinkedStep(
	t *testing.T,
) {
	fixture := createAgentPlanFixture(t, 6200)
	nodeOne, err := domain.NewNodeID()
	if err != nil {
		t.Fatal(err)
	}
	nodeTwo, err := domain.NewNodeID()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildAgentPlan(
		fixture.requirement.Analysis,
		AgentPlanDraft{
			Goal:          "Implement and validate two bounded steps.",
			Scope:         []string{"internal widget package"},
			ExpectedFiles: []string{"internal/widget.go"},
			Steps: []AgentPlanStepDraft{
				{
					Kind:           StepKindEdit,
					Title:          "Implement widget",
					DetailRedacted: "Apply the bounded widget implementation.",
					ExpectedFiles:  []string{"internal/widget.go"},
					GraphNodeIDs:   []domain.NodeID{nodeOne},
				},
				{
					Kind:           StepKindTest,
					Title:          "Validate widget",
					DetailRedacted: "Run the required widget validation.",
					GraphNodeIDs:   []domain.NodeID{nodeTwo},
				},
			},
			Risks:              []string{"bounded behavior change"},
			CompletionCriteria: []string{"both linked steps validate"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	previous := fixture.plan.Revision
	redirectMessage, err := fixture.repositories.AppendMessage(
		t.Context(),
		AppendMessage{
			ID: testMessageID(t, 6260), ThreadID: fixture.task.ThreadID,
			Role: MessageRoleUser, BodyRedacted: "Use the explicit two-step plan.",
			IdempotencyKey: "two-step-plan-redirect-message",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	redirected := fixture.planInput
	redirected.Plan = plan
	redirected.SupersedesRevision = &previous
	redirected.RedirectMessageID = &redirectMessage.ID
	redirected.IdempotencyKey = "two-step-agent-plan"
	fixture.plan, err = fixture.repositories.RecordPlanRevision(
		t.Context(),
		redirected,
	)
	if err != nil {
		t.Fatal(err)
	}
	bound := bindAgentPlanFixture(t, fixture, 6200)
	modelRequest := createAgentModelRequestFixture(t, bound)
	for index, step := range bound.plan.Plan.Steps {
		startKey := "multi-step-start-" + step.ID
		if _, err := bound.repositories.RecordPlanStepTransition(
			t.Context(),
			RecordPlanStepTransition{
				ID: startKey, TaskID: bound.task.ID, RunID: bound.runID,
				PlanRevision: bound.plan.Revision, PlanStepID: step.ID,
				From: PlanStepPending, To: PlanStepInProgress,
				ReasonRedacted: "begin linked plan step",
				ModelRequestID: modelRequest.ID, IdempotencyKey: startKey,
			},
		); err != nil {
			t.Fatalf("start step %d: %v", index, err)
		}
		implementedKey := "multi-step-implemented-" + step.ID
		if _, err := bound.repositories.RecordPlanStepTransition(
			t.Context(),
			RecordPlanStepTransition{
				ID: implementedKey, TaskID: bound.task.ID, RunID: bound.runID,
				PlanRevision: bound.plan.Revision, PlanStepID: step.ID,
				From: PlanStepInProgress, To: PlanStepImplemented,
				ReasonRedacted: "complete linked plan step",
				ModelRequestID: modelRequest.ID,
				IdempotencyKey: implementedKey,
			},
		); err != nil {
			t.Fatalf("implement step %d: %v", index, err)
		}
	}
	passed, err := bound.repositories.CreateValidation(
		t.Context(),
		CreateValidation{
			ID: testValidationID(t, 6270), TaskID: bound.task.ID,
			RunID: &bound.runID, State: domain.ValidationStatePassed,
			Severity:        domain.ValidationSeverityBlocking,
			ProfileName:     string(bound.plan.ValidationProfile),
			SummaryRedacted: stringPointer("all linked validation passed"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	stepIDs := []string{
		bound.plan.Plan.Steps[0].ID,
		bound.plan.Plan.Steps[1].ID,
	}
	commandFingerprint := strings.Repeat("d", 64)
	equivalentRequest := executor.ToolRequest{
		SchemaVersion: executor.ToolSchemaVersion,
		ID:            "post-run-validation-request",
		TaskID:        bound.task.ID,
		RunID:         bound.runID,
		Name:          executor.ToolTest,
		Arguments: []executor.ToolArgument{
			{Name: "executable", Value: "go"},
			{Name: "argument", Value: "test"},
			{Name: "argument", Value: "./internal/widget"},
		},
		WorkingDirectory: "C:/task-worktree",
		Timeout:          time.Minute,
		ClaimedAuthority: executor.AuthorityAutomaticRead,
		ExpectedSideEffects: []executor.SideEffect{
			executor.EffectSubprocess,
			executor.EffectRepositoryRead,
		},
		IdempotencyKey: "post-run-validation-request",
		Requester:      "storage-binding-test",
	}
	equivalentPlanCommand, err :=
		executor.RenderValidationPlanCommand(equivalentRequest)
	if err != nil ||
		equivalentPlanCommand != bound.plan.Plan.ValidationCommands[0] {
		t.Fatalf(
			"post-run command = %q, plan = %q, error = %v",
			equivalentPlanCommand,
			bound.plan.Plan.ValidationCommands[0],
			err,
		)
	}
	commands := []SelectedValidationCommandEvidence{{
		Ordinal: 1, CommandID: "targeted-widget-tests",
		CommandFingerprint: commandFingerprint,
		PlanCommand:        bound.plan.Plan.ValidationCommands[0],
		Required:           true, AcceptanceTest: true,
		RelevantChangedFiles: []string{"internal/widget.go"},
		PlanStepIDs:          stepIDs,
	}}
	profileName := string(bound.plan.ValidationProfile)
	profileVersion := "v1"
	profileDigest := selectedValidationProfileDigest(
		profileName, profileVersion, commands,
	)
	selected, err := bound.repositories.BindRunValidationProfile(
		t.Context(),
		BindRunValidationProfile{
			TaskID: bound.task.ID, RunID: bound.runID,
			PlanRevision: bound.plan.Revision,
			ProfileName:  profileName, ProfileVersion: profileVersion,
			ProfileDigest: profileDigest, Commands: commands,
			IdempotencyKey: "selected-validation-profile",
		},
	)
	if err != nil || selected.ProfileDigest != profileDigest ||
		!reflect.DeepEqual(selected.Commands, commands) {
		t.Fatalf("selected validation profile = %#v, %v", selected, err)
	}
	if _, err := bound.repositories.BindRunValidationProfile(
		t.Context(),
		BindRunValidationProfile{
			TaskID: bound.task.ID, RunID: bound.runID,
			PlanRevision: bound.plan.Revision,
			ProfileName:  profileName, ProfileVersion: "changed-version",
			ProfileDigest: profileDigest, Commands: commands,
			IdempotencyKey: "selected-validation-profile",
		},
	); err == nil {
		t.Fatal("changed selected validation profile unexpectedly accepted")
	}
	weaker := commands
	if _, err := bound.repositories.BindRunValidationProfile(
		t.Context(),
		BindRunValidationProfile{
			TaskID: bound.task.ID, RunID: bound.runID,
			PlanRevision: bound.plan.Revision,
			ProfileName:  "weaker-v1", ProfileVersion: profileVersion,
			ProfileDigest: selectedValidationProfileDigest(
				"weaker-v1", profileVersion, weaker,
			),
			Commands: weaker, IdempotencyKey: "weaker-profile",
		},
	); !errors.Is(err, ErrConstraint) {
		t.Fatalf("weaker selected profile error = %v", err)
	}
	mismatchedCommand := append(
		[]SelectedValidationCommandEvidence(nil), commands...,
	)
	substitutedRequest := equivalentRequest
	substitutedRequest.Arguments = append(
		[]executor.ToolArgument(nil), equivalentRequest.Arguments...,
	)
	substitutedRequest.Arguments[2].Value = "./..."
	mismatchedCommand[0].PlanCommand, err =
		executor.RenderValidationPlanCommand(substitutedRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.repositories.BindRunValidationProfile(
		t.Context(),
		BindRunValidationProfile{
			TaskID: bound.task.ID, RunID: bound.runID,
			PlanRevision: bound.plan.Revision,
			ProfileName:  profileName, ProfileVersion: profileVersion,
			ProfileDigest: profileDigest, Commands: mismatchedCommand,
			IdempotencyKey: "mismatched-plan-command",
		},
	); !errors.Is(err, ErrConstraint) {
		t.Fatalf("mismatched plan command error = %v", err)
	}
	presentation := string(mustJSON(validationOperationPresentation{
		ValidationID: passed.ID, CommandID: "targeted-widget-tests",
		CommandFingerprint: commandFingerprint,
		Required:           true, AcceptanceTest: true, PlanStepIDs: stepIDs,
		State: domain.ValidationStatePassed,
	}))
	bulkInput := RecordPlanValidationAttributions{
		TaskID: bound.task.ID, RunID: bound.runID,
		PlanRevision: bound.plan.Revision, PlanStepIDs: stepIDs,
		ValidationID: passed.ID, ValidationPassed: true,
		ProfileDigest: profileDigest, Round: 0, CommandOrdinal: 1,
		CommandID:                "targeted-widget-tests",
		CommandFingerprint:       commandFingerprint,
		PresentationRedactedJSON: presentation,
		PresentationSHA256:       hashJSON(presentation),
		IdempotencyKey:           "multi-step-validation",
	}
	links, err := bound.repositories.RecordPlanValidationAttributions(
		t.Context(),
		bulkInput,
	)
	if err != nil || len(links) != 2 {
		t.Fatalf("bulk validation links = %#v, %v", links, err)
	}
	statuses, err := bound.repositories.ListPlanStepStates(
		t.Context(),
		bound.runID,
	)
	if err != nil || len(statuses) != 2 {
		t.Fatalf("multi-step statuses = %#v, %v", statuses, err)
	}
	for _, status := range statuses {
		if status.PlanRevision != bound.plan.Revision ||
			status.State != PlanStepValidated ||
			status.Sequence != 3 {
			t.Fatalf("validated status = %#v", status)
		}
	}
	listed, err := bound.repositories.ListPlanValidationAttributions(
		t.Context(),
		bound.runID,
	)
	if err != nil || len(listed) != 2 {
		t.Fatalf("listed validation links = %#v, %v", listed, err)
	}
	if retried, err := bound.repositories.RecordPlanValidationAttributions(
		t.Context(),
		bulkInput,
	); err != nil || len(retried) != 2 {
		t.Fatalf("idempotent bulk validation = %#v, %v", retried, err)
	}
	changed := bulkInput
	changed.PlanRevision++
	if _, err := bound.repositories.RecordPlanValidationAttributions(
		t.Context(),
		changed,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed bulk validation retry error = %v", err)
	}
	changed = bulkInput
	changed.ValidationPassed = false
	if _, err := bound.repositories.RecordPlanValidationAttributions(
		t.Context(),
		changed,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed bulk validation effect retry error = %v", err)
	}
	changed = bulkInput
	changed.PlanStepIDs = []string{stepIDs[1], stepIDs[0]}
	if _, err := bound.repositories.RecordPlanValidationAttributions(
		t.Context(),
		changed,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed bulk validation order retry error = %v", err)
	}
	executions, err := bound.repositories.ListDurableValidationExecutions(
		t.Context(), bound.runID,
	)
	if err != nil || len(executions) != 1 ||
		executions[0].ProfileDigest != profileDigest ||
		executions[0].Ordinal != 1 ||
		executions[0].State != domain.ValidationStatePassed ||
		executions[0].FailurePresent ||
		executions[0].OutputTruncated ||
		executions[0].PresentationSHA256 != hashJSON(presentation) ||
		!reflect.DeepEqual(executions[0].PlanStepIDs, stepIDs) {
		t.Fatalf("durable validation executions = %#v, %v", executions, err)
	}
	if _, err := bound.repositories.database.sql.ExecContext(
		t.Context(),
		`UPDATE run_validation_profiles SET profile_version = 'v2'
		 WHERE run_id = ?`,
		bound.runID,
	); err == nil {
		t.Fatal("selected validation profile update unexpectedly succeeded")
	}
	if _, err := bound.repositories.database.sql.ExecContext(
		t.Context(),
		`UPDATE plan_validation_operations SET validation_passed = 0
		 WHERE run_id = ?`,
		bound.runID,
	); err == nil {
		t.Fatal("validation operation update unexpectedly succeeded")
	}

	failed, err := bound.repositories.CreateValidation(
		t.Context(),
		CreateValidation{
			ID: testValidationID(t, 6271), TaskID: bound.task.ID,
			RunID: &bound.runID, State: domain.ValidationStateFailed,
			Severity:        domain.ValidationSeverityBlocking,
			ProfileName:     string(bound.plan.ValidationProfile),
			SummaryRedacted: stringPointer("linked validation failed"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.repositories.RecordPlanValidationAttributions(
		t.Context(),
		RecordPlanValidationAttributions{
			TaskID: bound.task.ID, RunID: bound.runID,
			PlanRevision: bound.plan.Revision,
			PlanStepIDs:  []string{stepIDs[0], "missing-step"},
			ValidationID: failed.ID, IdempotencyKey: "atomic-failed-links",
			ProfileDigest: profileDigest, Round: 1, CommandOrdinal: 1,
			CommandID:                "targeted-widget-tests",
			CommandFingerprint:       commandFingerprint,
			PresentationRedactedJSON: `{"state":"failed"}`,
			PresentationSHA256:       hashJSON(`{"state":"failed"}`),
		},
	); err == nil {
		t.Fatal("partially invalid validation links unexpectedly committed")
	}
	falseValue := false
	misleadingFailure := string(mustJSON(validationOperationPresentation{
		ValidationID: failed.ID, CommandID: "targeted-widget-tests",
		CommandFingerprint: commandFingerprint,
		Required:           true, AcceptanceTest: true, PlanStepIDs: stepIDs,
		State: domain.ValidationStateFailed,
		Failure: &validationOperationFailure{
			SummaryRedacted: "linked validation failed",
			ChangedFiles:    []string{"internal/unrelated.go"},
			PlanStepIDs:     []string{stepIDs[0]},
			OutputTruncated: &falseValue,
		},
	}))
	if _, err := bound.repositories.RecordPlanValidationAttributions(
		t.Context(),
		RecordPlanValidationAttributions{
			TaskID: bound.task.ID, RunID: bound.runID,
			PlanRevision: bound.plan.Revision, PlanStepIDs: stepIDs,
			ValidationID: failed.ID, ValidationPassed: false,
			ProfileDigest: profileDigest, Round: 1, CommandOrdinal: 1,
			CommandID:                "targeted-widget-tests",
			CommandFingerprint:       commandFingerprint,
			PresentationRedactedJSON: misleadingFailure,
			PresentationSHA256:       hashJSON(misleadingFailure),
			IdempotencyKey:           "misleading-failure-linkage",
		},
	); !errors.Is(err, ErrConstraint) {
		t.Fatalf("misleading failure linkage error = %v", err)
	}
	listed, err = bound.repositories.ListPlanValidationAttributions(
		t.Context(),
		bound.runID,
	)
	if err != nil || len(listed) != 2 {
		t.Fatalf("failed bulk link rollback = %#v, %v", listed, err)
	}
	trueValue := true
	failurePresentation := string(mustJSON(validationOperationPresentation{
		ValidationID: failed.ID, CommandID: "targeted-widget-tests",
		CommandFingerprint: commandFingerprint,
		Required:           true, AcceptanceTest: true, PlanStepIDs: stepIDs,
		State: domain.ValidationStateFailed,
		Failure: &validationOperationFailure{
			SummaryRedacted: "linked validation failed",
			ChangedFiles:    []string{"internal/widget.go"},
			PlanStepIDs:     stepIDs,
			OutputTruncated: &trueValue,
		},
	}))
	if _, err := bound.repositories.RecordPlanValidationAttributions(
		t.Context(),
		RecordPlanValidationAttributions{
			TaskID: bound.task.ID, RunID: bound.runID,
			PlanRevision: bound.plan.Revision, PlanStepIDs: stepIDs,
			ValidationID: failed.ID, ValidationPassed: false,
			ProfileDigest: profileDigest, Round: 1, CommandOrdinal: 1,
			CommandID:                "targeted-widget-tests",
			CommandFingerprint:       commandFingerprint,
			PresentationRedactedJSON: failurePresentation,
			PresentationSHA256:       hashJSON(failurePresentation),
			IdempotencyKey:           "exact-failure-linkage",
		},
	); err != nil {
		t.Fatalf("exact failure linkage = %v", err)
	}
	executions, err = bound.repositories.ListDurableValidationExecutions(
		t.Context(), bound.runID,
	)
	if err != nil || len(executions) != 2 ||
		!executions[1].FailurePresent ||
		!executions[1].OutputTruncated ||
		!reflect.DeepEqual(
			executions[1].FailureChangedFiles,
			[]string{"internal/widget.go"},
		) ||
		!reflect.DeepEqual(executions[1].FailurePlanStepIDs, stepIDs) {
		t.Fatalf("durable failed validation = %#v, %v", executions, err)
	}
}

func TestValidationProfileBindingEnforcesOrderedMinimumFloor(t *testing.T) {
	routine := createBoundAgentRunFixture(t, 6350)
	routineCommands := validationCommandsForBoundFixture(routine)
	protectedDigest := selectedValidationProfileDigest(
		string(ValidationProfileProtected), "v1", routineCommands,
	)
	if _, err := routine.repositories.BindRunValidationProfile(
		t.Context(),
		BindRunValidationProfile{
			TaskID: routine.task.ID, RunID: routine.runID,
			PlanRevision:   routine.plan.Revision,
			ProfileName:    string(ValidationProfileProtected),
			ProfileVersion: "v1", ProfileDigest: protectedDigest,
			Commands: routineCommands, IdempotencyKey: "stronger-profile",
		},
	); !errors.Is(err, ErrConstraint) {
		t.Fatalf("label-only routine to protected profile error = %v", err)
	}
	if _, err := routine.repositories.BindRunValidationProfile(
		t.Context(),
		BindRunValidationProfile{
			TaskID: routine.task.ID, RunID: routine.runID,
			PlanRevision:   routine.plan.Revision,
			ProfileName:    string(ValidationProfileElevated),
			ProfileVersion: "v1",
			ProfileDigest: selectedValidationProfileDigest(
				string(ValidationProfileElevated), "v1", routineCommands,
			),
			Commands: routineCommands, IdempotencyKey: "elevated-label-only",
		},
	); !errors.Is(err, ErrConstraint) {
		t.Fatalf("label-only routine to elevated profile error = %v", err)
	}
	if _, err := routine.repositories.BindRunValidationProfile(
		t.Context(),
		BindRunValidationProfile{
			TaskID: routine.task.ID, RunID: routine.runID,
			PlanRevision: routine.plan.Revision,
			ProfileName:  "unknown-v1", ProfileVersion: "v1",
			ProfileDigest: selectedValidationProfileDigest(
				"unknown-v1", "v1", routineCommands,
			),
			Commands: routineCommands, IdempotencyKey: "unknown-profile",
		},
	); !errors.Is(err, ErrConstraint) {
		t.Fatalf("unknown profile error = %v", err)
	}
	if _, err := routine.repositories.BindRunValidationProfile(
		t.Context(),
		BindRunValidationProfile{
			TaskID: routine.task.ID, RunID: routine.runID,
			PlanRevision:   routine.plan.Revision,
			ProfileName:    string(ValidationProfileRoutine),
			ProfileVersion: "v1",
			ProfileDigest: selectedValidationProfileDigest(
				string(ValidationProfileRoutine), "v1", routineCommands,
			),
			Commands: routineCommands, IdempotencyKey: "routine-profile",
		},
	); err != nil {
		t.Fatalf("routine one-command profile = %v", err)
	}

	protectedPlan := createAgentPlanFixtureWithBody(
		t,
		6450,
		strings.Join([]string{
			"Implement credential authentication in internal/widget.go.",
			"go test ./internal/widget",
			"go vet ./internal/widget",
			"make test",
			"Acceptance: authentication tests pass.",
		}, "\n"),
	)
	protected := approveAndBindAgentPlanFixture(
		t, protectedPlan, 6450,
	)
	protectedCommands := validationCommandsForBoundFixture(protected)
	duplicatePlanCommands := append(
		[]SelectedValidationCommandEvidence(nil), protectedCommands...,
	)
	duplicatePlanCommands[1].PlanCommand =
		duplicatePlanCommands[0].PlanCommand
	if _, err := protected.repositories.BindRunValidationProfile(
		t.Context(),
		BindRunValidationProfile{
			TaskID: protected.task.ID, RunID: protected.runID,
			PlanRevision:   protected.plan.Revision,
			ProfileName:    string(ValidationProfileProtected),
			ProfileVersion: "v1",
			ProfileDigest: selectedValidationProfileDigest(
				string(ValidationProfileProtected), "v1",
				duplicatePlanCommands,
			),
			Commands:       duplicatePlanCommands,
			IdempotencyKey: "duplicate-plan-command",
		},
	); err == nil {
		t.Fatal("duplicate selected plan command unexpectedly succeeded")
	}
	duplicateFingerprints := append(
		[]SelectedValidationCommandEvidence(nil), protectedCommands...,
	)
	duplicateFingerprints[1].CommandFingerprint =
		duplicateFingerprints[0].CommandFingerprint
	if _, err := protected.repositories.BindRunValidationProfile(
		t.Context(),
		BindRunValidationProfile{
			TaskID: protected.task.ID, RunID: protected.runID,
			PlanRevision:   protected.plan.Revision,
			ProfileName:    string(ValidationProfileProtected),
			ProfileVersion: "v1",
			ProfileDigest: selectedValidationProfileDigest(
				string(ValidationProfileProtected), "v1",
				duplicateFingerprints,
			),
			Commands:       duplicateFingerprints,
			IdempotencyKey: "duplicate-command-fingerprint",
		},
	); err == nil {
		t.Fatal("duplicate selected command fingerprint unexpectedly succeeded")
	}
	if _, err := protected.repositories.BindRunValidationProfile(
		t.Context(),
		BindRunValidationProfile{
			TaskID: protected.task.ID, RunID: protected.runID,
			PlanRevision:   protected.plan.Revision,
			ProfileName:    string(ValidationProfileProtected),
			ProfileVersion: "v1",
			ProfileDigest: selectedValidationProfileDigest(
				string(ValidationProfileProtected), "v1", protectedCommands,
			),
			Commands: protectedCommands, IdempotencyKey: "protected-profile",
		},
	); err != nil {
		t.Fatalf("protected three-command profile = %v", err)
	}
	if _, err := protected.repositories.BindRunValidationProfile(
		t.Context(),
		BindRunValidationProfile{
			TaskID: protected.task.ID, RunID: protected.runID,
			PlanRevision:   protected.plan.Revision,
			ProfileName:    string(ValidationProfileRoutine),
			ProfileVersion: "v1",
			ProfileDigest: selectedValidationProfileDigest(
				string(ValidationProfileRoutine), "v1", protectedCommands,
			),
			Commands: protectedCommands, IdempotencyKey: "weaker-profile",
		},
	); !errors.Is(err, ErrConstraint) {
		t.Fatalf("protected to routine profile error = %v", err)
	}
}

func validationCommandsForBoundFixture(
	fixture boundAgentRunFixture,
) []SelectedValidationCommandEvidence {
	commands := make(
		[]SelectedValidationCommandEvidence,
		len(fixture.plan.Plan.ValidationCommands),
	)
	for index, planCommand := range fixture.plan.Plan.ValidationCommands {
		commands[index] = SelectedValidationCommandEvidence{
			Ordinal:              uint64(index + 1),
			CommandID:            fmt.Sprintf("plan-validation-%03d", index+1),
			CommandFingerprint:   fmt.Sprintf("%064x", index+1),
			PlanCommand:          planCommand,
			Required:             true,
			AcceptanceTest:       index == 0,
			RelevantChangedFiles: fixture.plan.Plan.ExpectedFiles,
			PlanStepIDs:          []string{fixture.plan.Plan.Steps[0].ID},
		}
	}
	return commands
}

type boundAgentRunFixture struct {
	agentPlanFixture
	runID domain.RunID
}

func approveAndBindAgentPlanFixture(
	t *testing.T,
	fixture agentPlanFixture,
	base int,
) boundAgentRunFixture {
	t.Helper()
	approvalID, err := domain.NewApprovalID()
	if err != nil {
		t.Fatal(err)
	}
	approval, err := fixture.repositories.CreateApproval(
		t.Context(),
		CreateApproval{
			ID: approvalID, TaskID: fixture.task.ID,
			Scope:          PlanApprovalScope(fixture.plan),
			RequestReason:  "approve protected validation floor test",
			IdempotencyKey: "approve-protected-profile-plan",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.ResolveApproval(
		t.Context(),
		ResolveApproval{
			ID: approval.ID, ExpectedRevision: approval.Revision,
			To:               domain.ApprovalRequestStateGranted,
			ResolutionReason: "approve protected profile test",
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.ApprovePlanRevision(
		t.Context(),
		ApprovePlanRevision{
			TaskID: fixture.task.ID, PlanRevision: fixture.plan.Revision,
			ApprovalID:     approval.ID,
			IdempotencyKey: "bind-protected-profile-approval",
		},
	); err != nil {
		t.Fatal(err)
	}
	return bindAgentPlanFixture(t, fixture, base)
}

func createBoundAgentRunFixture(
	t *testing.T,
	base int,
) boundAgentRunFixture {
	t.Helper()
	fixture := createAgentPlanFixture(t, base)
	return bindAgentPlanFixture(t, fixture, base)
}

func bindAgentPlanFixture(
	t *testing.T,
	fixture agentPlanFixture,
	base int,
) boundAgentRunFixture {
	t.Helper()
	task := transitionTaskFixtureToReady(
		t, fixture.repositories, fixture.task, base+30,
	)
	preflight, err := fixture.repositories.PrepareTaskExecution(
		t.Context(),
		PrepareTaskExecution{
			TaskID: task.ID, ExpectedTaskRevision: task.Revision,
			PolicyRevision:   fixture.policyRevision,
			ForecastRevision: fixture.forecastRevision,
			BudgetID:         fixture.budgetID, BudgetLimitRevision: 0,
			IdempotencyKey: "bound-agent-preflight",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.StartPreparedTaskRun(
		t.Context(),
		StartPreparedTaskRun{
			RunID: runID, EventID: testEventID(t, base+40),
			TaskID: task.ID, PreflightRevision: preflight.Revision,
			ExpectedTaskRevision: task.Revision, Attempt: 1,
			IdempotencyKey:      "bound-agent-run",
			EventIdempotencyKey: "bound-agent-run-event",
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.BindRunPlan(
		t.Context(),
		BindRunPlan{
			RunID: runID, TaskID: task.ID,
			PlanRevision:   fixture.plan.Revision,
			IdempotencyKey: "bound-agent-plan",
		},
	); err != nil {
		t.Fatal(err)
	}
	fixture.task = task
	return boundAgentRunFixture{agentPlanFixture: fixture, runID: runID}
}

func createAgentModelRequestFixture(
	t *testing.T,
	fixture boundAgentRunFixture,
) ProviderLogicalRequest {
	t.Helper()
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	_, micros := fixture.repositories.timestamp()
	if _, err := fixture.repositories.database.sql.ExecContext(
		t.Context(),
		`INSERT INTO providers (
			id, display_name, provider_type, enabled,
			created_at_unix_micros, updated_at_unix_micros, revision
		) VALUES (?, 'Agent fixture', 'fixture', 1, ?, ?, 0)`,
		providerID, micros, micros,
	); err != nil {
		t.Fatal(err)
	}
	configuration, err := fixture.repositories.CreateProviderConfigurationRevision(
		t.Context(),
		CreateProviderConfigurationRevision{
			ID: "agent-config", ProviderID: providerID,
			ExpectedLatestRevision: 0,
			AdapterName:            "fixture-adapter", AdapterVersion: "v1",
			ProviderVersion:  "v1",
			EndpointRedacted: "https://example.invalid",
			CapabilitiesJSON: `{"tools":true}`,
			ContentSHA256:    strings.Repeat("e", 64),
			IdempotencyKey:   "agent-config",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	pricing, err := fixture.repositories.CreateProviderPricingRevision(
		t.Context(),
		CreateProviderPricingRevision{
			ID: "agent-pricing", ProviderID: providerID,
			ModelIdentifier: "fixture-model", ModelVersion: "v1",
			PricingKnown: false,
			EffectiveAt:  time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	pricingID := pricing.ID
	request, err := fixture.repositories.PlanProviderLogicalRequest(
		t.Context(),
		PlanProviderLogicalRequest{
			ID: requestID, TaskID: fixture.task.ID, RunID: &fixture.runID,
			ProviderID:                      providerID,
			ProviderConfigurationRevisionID: configuration.ID,
			AdapterName:                     configuration.AdapterName,
			AdapterVersion:                  configuration.AdapterVersion,
			ProviderVersion:                 configuration.ProviderVersion,
			ModelIdentifier:                 pricing.ModelIdentifier,
			ModelVersion:                    pricing.ModelVersion,
			PricingRevisionID:               &pricingID,
			RequestSHA256:                   strings.Repeat("f", 64),
			IdempotencyKey:                  "agent-model-request",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
