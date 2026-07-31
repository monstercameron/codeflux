package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/redact"
)

func TestDeterministicFakeAgentCompletesPlanEditTestReviewStateMachine(
	t *testing.T,
) {
	taskID := repairCoordinatorTaskID(t, 900)
	runID := repairCoordinatorRunID(t, 900)
	modelRequestOne := gateModelRequestID(t, 900)
	modelRequestTwo := gateModelRequestID(t, 901)
	policySHA256 := strings.Repeat("b", 64)
	modelIdentity := providers.ModelIdentity{
		Provider: providers.ProviderIdentity{
			Adapter: "deterministic-fake", AdapterVersion: "1",
			Provider: "deterministic-fake", ProviderVersion: "1",
		},
		Model: "fixed-model", Revision: "fixed-revision",
	}
	harness := &agentStateMachineGateHarness{
		taskID: taskID, runID: runID,
		modelIdentity:  modelIdentity,
		policyRevision: 4, policySHA256: policySHA256,
		validationRuns: []ValidationCommandRun{
			failedValidationRun(t, 900),
			passedValidationRun(t, 901),
		},
		repositorySummary: agentloop.RepositoryCompletionSummary{
			StatusRedacted: "M internal/widget.go",
			DiffRedacted:   "bounded deterministic diff",
			DiffSHA256:     strings.Repeat("a", 64),
			ChangedFiles:   []string{"internal/widget.go"},
		},
		budgetSummary: repairCoordinatorBudgetSummary(),
	}
	harness.modelTurns = []agentloop.ModelTurn{
		{
			ModelRequestID: modelRequestOne,
			Model:          modelIdentity,
			ToolCalls: []agentloop.ModelToolCall{{
				Call: providers.ToolCall{
					ID: "edit-widget", Name: string(executor.ToolApplyEdit),
					Arguments: json.RawMessage(
						`{"path":"internal/widget.go","content":"package widget"}`,
					),
				},
				PlanStepID: "step-implementation", CompletesStep: false,
			}},
			Usage: gateKnownUsage(),
			Cost:  gateKnownCost(t, 1, 100),
		},
		{
			ModelRequestID: modelRequestTwo,
			Model:          modelIdentity,
			Completion:     agentloop.CompletionImplementationComplete,
			Usage:          gateKnownUsage(),
			Cost:           gateKnownCost(t, 1, 100),
		},
	}
	executionLoop, err := agentloop.NewExecutionLoop(agentloop.LoopDependencies{
		Model: harness, Authority: harness, Tools: harness,
		Journal: harness, PlanSteps: harness, Checkpoints: harness,
		PlanApprovalCheckpoints: harness,
		Control:                 harness, Interrupts: harness, Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "worktree")
	approvalID, err := domain.NewApprovalID()
	if err != nil {
		t.Fatal(err)
	}
	loopInput := agentloop.LoopInput{
		TaskID: taskID, RunID: runID, WorktreePath: worktree,
		PlanApprovalID: approvalID,
		PolicyRevision: harness.policyRevision, PolicySHA256: policySHA256,
		Plan: agentloop.PlanProjection{
			Revision: 7, RepositoryRevision: "git-revision-7",
			Steps: []agentloop.PlanStep{
				{
					ID:              "step-implementation",
					Kind:            agentloop.StepKindEdit,
					SummaryRedacted: "Apply the approved implementation",
					State:           agentloop.StepPending, MaterialEdit: true,
					ValidationRequired: true,
					ExpectedFiles:      []string{"internal/widget.go"},
					CompletionTools: []executor.ToolName{
						executor.ToolApplyEdit,
					},
				},
			},
		},
		RepositoryContext: []agentloop.RepositoryContextItem{{
			Path:            "internal/widget.go",
			ContentSHA256:   strings.Repeat("c", 64),
			ContentRedacted: "package widget",
		}},
		FactualEvents: []agentloop.FactualEvent{{
			Sequence: 1, Type: "requirement.bound",
			SummaryRedacted: "requirement revision 2 bound to plan revision 7",
		}},
		ApprovedTools: []agentloop.ApprovedTool{gateApplyEditTool(t)},
		Limits: agentloop.LoopLimits{
			MaximumRounds: 4, MaximumToolCalls: 2,
			MaximumToolCallsPerRound: 1,
			MaximumTokens:            100,
			MaximumTokensPerRound:    20,
			MaximumWallClock:         time.Minute,
			MaximumCost:              gateKnownCost(t, 1, 1),
			MaximumIdenticalFailures: 2,
			MaximumContextItems:      2,
			MaximumFactualEvents:     2,
			MaximumContextBytes:      4096,
			MaximumResultBytes:       1024,
		},
	}
	loopOutcome, err := executionLoop.Run(t.Context(), loopInput)
	if err != nil {
		t.Fatal(err)
	}
	if loopOutcome.Kind != agentloop.OutcomeImplementationComplete ||
		!loopOutcome.ValidationRequired ||
		loopOutcome.Plan.Revision != loopInput.Plan.Revision ||
		loopOutcome.Plan.RepositoryRevision !=
			loopInput.Plan.RepositoryRevision {
		t.Fatalf("execution outcome = %#v", loopOutcome)
	}

	pipeline, err := redact.NewPipeline(nil, redact.Limits{
		MaximumInputBytes: 1 << 20, MaximumOutputBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pipeline.Close)
	completionService, err := NewRepairCompletionService(
		RepairCompletionDependencies{
			Validations: harness, Checkpoints: harness, Repairs: harness,
			CurrentValidation: &completionValidationGateStub{},
			Control:           harness, Store: harness, Repository: harness,
			Budget: harness, Redactor: pipeline,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	profile := repairCoordinatorProfile(taskID, runID)
	profile.Commands[0].RelevantChangedFiles = []string{"internal/widget.go"}
	profile.Commands[0].PlanStepIDs = []string{"step-implementation"}
	validation, err := completionService.ValidateAndRepair(
		t.Context(),
		ValidationRepairInput{
			TaskID: taskID, RunID: runID, PlanRevision: 7,
			Profile: profile, ChangedFiles: []string{"internal/widget.go"},
			MaximumRepairRounds: 1,
			IdempotencyPrefix:   "m14-gate",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !validation.ReadyForCompletion ||
		validation.RepairRounds != 1 ||
		len(validation.Reports) != 2 ||
		validation.Reports[0].ProfileDigest !=
			validation.Reports[1].ProfileDigest ||
		validation.Reports[0].Executions[0].CommandFingerprint !=
			validation.Reports[1].Executions[0].CommandFingerprint {
		t.Fatalf("validation outcome = %#v", validation)
	}
	completion, err := completionService.PrepareCompletion(
		t.Context(),
		CompletionInput{
			TaskID: taskID, RunID: runID, PlanRevision: 7,
			ExpectedTaskRevision: 12, ExpectedRunRevision: 8,
			EventID:             repairCoordinatorEventID(t, 900),
			EventIdempotencyKey: "m14-gate-completion-event",
			Assumptions:         []string{"deterministic fake provider selected"},
			Limitations:         []string{"external provider network was not exercised"},
			IdempotencyKey:      "m14-gate-completion",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if completion.State != domain.TaskStateAwaitingReview ||
		harness.reviewCalls != 0 {
		t.Fatalf(
			"completion = %#v, premature reviews = %d",
			completion,
			harness.reviewCalls,
		)
	}
	finalState, err := completionService.DecideReview(
		t.Context(),
		agentloop.ReviewDecisionRequest{
			TaskID: taskID, RunID: runID, PlanRevision: 7,
			CompletionRevision:   completion.Revision,
			ExpectedTaskRevision: 13, ExpectedRunRevision: 8,
			Decision: agentloop.ReviewDecisionAccept,
			Actor:    "user:gate", AuthorityReference: "review:m14-gate",
			ReasonRedacted: "accepted deterministic completion evidence",
			IdempotencyKey: "m14-gate-accept",
		},
		repairCoordinatorEventID(t, 901),
		"m14-gate-review-event",
	)
	if err != nil || finalState != domain.TaskStateCompleted {
		t.Fatalf("review = %s, %v", finalState, err)
	}

	wantOrder := []string{
		"checkpoint:plan-approved",
		"model:1",
		"authority:apply-edit",
		"tool:start",
		"step:in-progress",
		"tool:execute",
		"tool:result",
		"checkpoint:edit",
		"step:implemented",
		"model:2",
		"validation:go-test:0",
		"validation:attribution:go-test",
		"checkpoint:repair",
		"repair:begin",
		"repair:execute",
		"validation:go-test:1",
		"validation:attribution:go-test",
		"step:validated",
		"repair:outcome:validation-passed",
		"repository:inspect",
		"budget:inspect",
		"completion:awaiting-review",
		"review:accept",
	}
	if !slices.Equal(harness.events, wantOrder) {
		t.Fatalf("state-machine order = %#v, want %#v", harness.events, wantOrder)
	}
	if harness.modelCalls != 2 ||
		harness.authorityCalls != 1 ||
		harness.toolCalls != 1 ||
		len(harness.validationCommands) != 2 ||
		harness.validationCommands[0].ID != "go-test" ||
		harness.validationCommands[1].ID != "go-test" {
		t.Fatalf("unexpected fallback or skipped action: %#v", harness)
	}
	start := harness.toolStarts[0]
	request := harness.authorityRequests[0]
	if start.TaskID != taskID || start.RunID != runID ||
		start.PlanRevision != 7 ||
		start.PlanStepID != "step-implementation" ||
		start.ModelRequestID != modelRequestOne ||
		start.RequestID != "edit-widget" ||
		start.Authorization.DecisionID != "policy-decision-gate" ||
		start.Authorization.PolicyRevision != harness.policyRevision ||
		start.Authorization.PolicySHA256 != policySHA256 ||
		start.Authorization.Classification.Required !=
			request.ClaimedAuthority ||
		start.Authorization.Classification.Capability !=
			request.ClaimedAuthority ||
		start.Authorization.Classification.ScopeHash !=
			executor.ActionSHA256(request) ||
		start.Authorization.Classification.Outcome !=
			executor.OutcomeTaskScoped {
		t.Fatalf("tool attribution or authority widened: %#v", start)
	}
	for _, attribution := range harness.validationAttributions {
		if attribution.TaskID != taskID || attribution.RunID != runID ||
			attribution.PlanRevision != 7 ||
			!slices.Equal(
				attribution.PlanStepIDs,
				[]string{"step-implementation"},
			) {
			t.Fatalf("validation attribution = %#v", attribution)
		}
	}
	if harness.validationAttributions[0].RepairAttemptRevision != nil ||
		harness.validationAttributions[1].RepairAttemptRevision == nil ||
		*harness.validationAttributions[1].RepairAttemptRevision != 1 {
		t.Fatalf(
			"repair validation attribution = %#v",
			harness.validationAttributions,
		)
	}
	for _, input := range harness.modelInputs {
		if input.TaskID != taskID || input.RunID != runID ||
			input.Model != modelIdentity ||
			input.Plan.Revision != 7 ||
			input.Plan.RepositoryRevision != "git-revision-7" ||
			len(input.ApprovedTools) != 1 ||
			input.ApprovedTools[0].Name != string(executor.ToolApplyEdit) ||
			len(input.FactualEvents) != 1 ||
			input.FactualEvents[0].Type != "requirement.bound" {
			t.Fatalf("model binding or fixed-tool set changed = %#v", input)
		}
	}
	for _, transition := range harness.planTransitions {
		if transition.TaskID != taskID || transition.RunID != runID ||
			transition.PlanRevision != 7 ||
			transition.PlanStepID != "step-implementation" ||
			transition.ModelRequestID != modelRequestOne ||
			transition.ToolRequestID != "edit-widget" {
			t.Fatalf("plan transition attribution = %#v", transition)
		}
	}
	if len(harness.toolResults) != 1 ||
		harness.toolResults[0].TaskID != taskID ||
		harness.toolResults[0].RunID != runID ||
		harness.toolResults[0].PlanRevision != 7 ||
		harness.toolResults[0].PlanStepID != "step-implementation" ||
		harness.toolResults[0].RequestID != "edit-widget" ||
		len(harness.editCheckpoints) != 1 ||
		harness.editCheckpoints[0].TaskID != taskID ||
		harness.editCheckpoints[0].RunID != runID ||
		harness.editCheckpoints[0].PlanRevision != 7 ||
		harness.editCheckpoints[0].ModelRequestID != modelRequestOne ||
		harness.editCheckpoints[0].ToolRequestID != "edit-widget" {
		t.Fatalf(
			"tool result/checkpoint attribution = %#v / %#v",
			harness.toolResults,
			harness.editCheckpoints,
		)
	}
	if harness.beginRepairRecord.TaskID != taskID ||
		harness.beginRepairRecord.RunID != runID ||
		harness.beginRepairRecord.PlanRevision != 7 ||
		harness.beginRepairRecord.FailedValidationID !=
			harness.validationRuns[0].ValidationID ||
		len(harness.repairRequests) != 1 ||
		harness.repairRequests[0].TaskID != taskID ||
		harness.repairRequests[0].RunID != runID ||
		harness.repairRequests[0].PlanRevision != 7 ||
		harness.completionRecord.TaskID != taskID ||
		harness.completionRecord.RunID != runID ||
		harness.completionRecord.PlanRevision != 7 ||
		harness.reviewRecord.TaskID != taskID ||
		harness.reviewRecord.RunID != runID ||
		harness.reviewRecord.PlanRevision != 7 ||
		harness.reviewRecord.CompletionRevision != completion.Revision ||
		harness.validationTransitions != 1 ||
		harness.durablePlanStepState != agentloop.StepValidated {
		t.Fatalf("repair/completion/review attribution = %#v", harness)
	}
}

type agentStateMachineGateHarness struct {
	taskID                 domain.TaskID
	runID                  domain.RunID
	modelIdentity          providers.ModelIdentity
	modelTurns             []agentloop.ModelTurn
	modelInputs            []agentloop.ModelInput
	modelCalls             int
	policyRevision         uint64
	policySHA256           string
	authorityCalls         int
	authorityRequests      []executor.ToolRequest
	toolCalls              int
	toolStarts             []agentloop.ToolStartRecord
	toolResults            []agentloop.ToolResultRecord
	planTransitions        []agentloop.PlanStepTransition
	editCheckpoints        []agentloop.CheckpointRequest
	validationRuns         []ValidationCommandRun
	validationCommands     []agentloop.ValidationCommand
	validationAttributions []validationAttributionRecord
	selectedProfile        selectedValidationProfileRecord
	beginRepairRecord      beginRepairRecord
	repairRequests         []RepairRequest
	repairOutcomes         []repairOutcomeRecord
	completionRecord       completionCandidateRecord
	reviewRecord           reviewDecisionRecord
	durablePlanStepState   agentloop.StepState
	validationTransitions  int
	repositorySummary      agentloop.RepositoryCompletionSummary
	budgetSummary          agentloop.BudgetCompletionSummary
	reviewCalls            int
	events                 []string
}

func (harness *agentStateMachineGateHarness) Identity() providers.ModelIdentity {
	return harness.modelIdentity
}

func (harness *agentStateMachineGateHarness) ObserveThink(
	_ context.Context,
	input agentloop.ModelInput,
) (agentloop.ModelTurn, error) {
	if harness.modelCalls >= len(harness.modelTurns) {
		return agentloop.ModelTurn{}, errors.New("unexpected model fallback")
	}
	harness.modelInputs = append(harness.modelInputs, input)
	harness.modelCalls++
	harness.addEvent(fmt.Sprintf("model:%d", harness.modelCalls))
	return harness.modelTurns[harness.modelCalls-1], nil
}

func (harness *agentStateMachineGateHarness) RouteTool(
	_ context.Context,
	request executor.ToolRequest,
) (agentloop.ToolAuthorization, error) {
	harness.authorityCalls++
	harness.authorityRequests = append(harness.authorityRequests, request)
	harness.addEvent("authority:" + string(request.Name))
	return agentloop.ToolAuthorization{
		Classification: executor.AuthorityClassification{
			Outcome:        executor.OutcomeTaskScoped,
			Required:       request.ClaimedAuthority,
			Capability:     request.ClaimedAuthority,
			MatchedGrantID: "grant:m14-gate",
			ScopeHash:      executor.ActionSHA256(request),
			Description:    executor.UserReadableToolSummary(request),
		},
		DecisionID:     "policy-decision-gate",
		PolicyRevision: harness.policyRevision,
		PolicySHA256:   harness.policySHA256,
	}, nil
}

func (harness *agentStateMachineGateHarness) ExecuteTool(
	_ context.Context,
	request executor.AuthorizedToolRequest,
) (executor.ToolResult, error) {
	harness.toolCalls++
	harness.addEvent("tool:execute")
	return executor.ToolResult{
		RequestID:     request.Request.ID,
		SchemaVersion: executor.ToolSchemaVersion,
		State:         "succeeded", ExitCode: 0, Summary: "edit applied",
	}, nil
}

func (harness *agentStateMachineGateHarness) PersistToolStart(
	_ context.Context,
	record agentloop.ToolStartRecord,
) error {
	harness.toolStarts = append(harness.toolStarts, record)
	harness.addEvent("tool:start")
	return nil
}

func (harness *agentStateMachineGateHarness) PersistToolResult(
	_ context.Context,
	record agentloop.ToolResultRecord,
) error {
	harness.toolResults = append(harness.toolResults, record)
	harness.addEvent("tool:result")
	return nil
}

func (harness *agentStateMachineGateHarness) PersistPlanStepTransition(
	_ context.Context,
	record agentloop.PlanStepTransition,
) error {
	harness.planTransitions = append(harness.planTransitions, record)
	harness.durablePlanStepState = record.To
	harness.addEvent("step:" + string(record.To))
	return nil
}

func (harness *agentStateMachineGateHarness) CreateCheckpoint(
	_ context.Context,
	record agentloop.CheckpointRequest,
) error {
	harness.editCheckpoints = append(harness.editCheckpoints, record)
	harness.addEvent("checkpoint:edit")
	return nil
}

func (harness *agentStateMachineGateHarness) CreatePlanApprovedCheckpoint(
	_ context.Context,
	_ agentloop.PlanApprovedCheckpointRequest,
) error {
	harness.addEvent("checkpoint:plan-approved")
	return nil
}

func (harness *agentStateMachineGateHarness) ReadControl(
	context.Context,
	domain.TaskID,
	domain.RunID,
) (agentloop.ControlState, error) {
	return agentloop.ControlState{
		Disposition:     agentloop.ControlActive,
		BudgetAvailable: true, PolicyCurrent: true,
	}, nil
}

func (harness *agentStateMachineGateHarness) BindActionContext(
	parent context.Context,
	_ domain.TaskID,
	_ domain.RunID,
	_ agentloop.ActionDescriptor,
) (context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, nil
}

func (harness *agentStateMachineGateHarness) RunValidationCommand(
	_ context.Context,
	command agentloop.ValidationCommand,
	round uint32,
) (ValidationCommandRun, error) {
	index := len(harness.validationCommands)
	if index >= len(harness.validationRuns) {
		return ValidationCommandRun{}, errors.New("required validation skipped")
	}
	harness.validationCommands = append(harness.validationCommands, command)
	harness.addEvent(fmt.Sprintf("validation:%s:%d", command.ID, round))
	return harness.validationRuns[index], nil
}

func (harness *agentStateMachineGateHarness) BindValidationProfile(
	_ context.Context,
	record selectedValidationProfileRecord,
) error {
	harness.selectedProfile = record
	return nil
}

func (harness *agentStateMachineGateHarness) CreatePreRepairCheckpoint(
	context.Context,
	domain.TaskID,
	domain.RunID,
	uint64,
	uint32,
	string,
) (domain.CheckpointID, error) {
	harness.addEvent("checkpoint:repair")
	return repairCoordinatorCheckpointIDForGate(), nil
}

func (harness *agentStateMachineGateHarness) ExecuteRepair(
	_ context.Context,
	request RepairRequest,
) (RepairResult, error) {
	harness.repairRequests = append(harness.repairRequests, request)
	harness.addEvent("repair:execute")
	return RepairResult{ChangedFiles: []string{"internal/widget.go"}}, nil
}

func (harness *agentStateMachineGateHarness) RecordValidationAttribution(
	_ context.Context,
	record validationAttributionRecord,
) error {
	harness.validationAttributions = append(
		harness.validationAttributions,
		record,
	)
	harness.addEvent("validation:attribution:go-test")
	if record.ValidationPassed {
		if harness.durablePlanStepState != agentloop.StepImplemented {
			return errors.New("validation attempted before durable implementation")
		}
		harness.durablePlanStepState = agentloop.StepValidated
		harness.validationTransitions++
		harness.addEvent("step:validated")
	}
	return nil
}

func (harness *agentStateMachineGateHarness) ReadCompletionEvidence(
	_ context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
	planRevision uint64,
) (completionEvidenceRecord, error) {
	evidence := completionEvidenceRecord{
		PlanSteps: []completionPlanStepRecord{{
			PlanStepID: "step-implementation",
			State:      harness.durablePlanStepState,
		}},
	}
	for _, attribution := range harness.validationAttributions {
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
		}
	}
	return evidence, nil
}

func (harness *agentStateMachineGateHarness) ReadFinalValidationReport(
	context.Context,
	domain.TaskID,
	domain.RunID,
	uint64,
) (agentloop.ValidationReport, error) {
	if harness.selectedProfile.ProfileDigest == "" {
		return agentloop.ValidationReport{}, ErrRequiredValidationIncomplete
	}
	var finalRound uint32
	for _, attribution := range harness.validationAttributions {
		if attribution.Round > finalRound {
			finalRound = attribution.Round
		}
	}
	report := agentloop.ValidationReport{
		ProfileName:    harness.selectedProfile.Profile.Name,
		ProfileVersion: harness.selectedProfile.Profile.Version,
		ProfileDigest:  harness.selectedProfile.ProfileDigest,
		Round:          finalRound,
	}
	for _, attribution := range harness.validationAttributions {
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
	return report, report.Validate()
}

func (harness *agentStateMachineGateHarness) BeginRepair(
	_ context.Context,
	record beginRepairRecord,
) (uint64, error) {
	harness.beginRepairRecord = record
	harness.addEvent("repair:begin")
	return 1, nil
}

func (harness *agentStateMachineGateHarness) RecordRepairOutcome(
	_ context.Context,
	record repairOutcomeRecord,
) error {
	harness.repairOutcomes = append(harness.repairOutcomes, record)
	harness.addEvent("repair:outcome:" + string(record.Outcome))
	return nil
}

func (harness *agentStateMachineGateHarness) RecordCompletion(
	_ context.Context,
	record completionCandidateRecord,
) (uint64, domain.TaskState, error) {
	harness.completionRecord = record
	harness.addEvent("completion:awaiting-review")
	return 1, domain.TaskStateAwaitingReview, nil
}

func (harness *agentStateMachineGateHarness) RecordReviewDecision(
	_ context.Context,
	record reviewDecisionRecord,
) (domain.TaskState, error) {
	harness.reviewCalls++
	harness.reviewRecord = record
	harness.addEvent("review:" + string(record.Decision))
	return record.Decision.ResultingTaskState()
}

func (harness *agentStateMachineGateHarness) InspectFinalRepository(
	context.Context,
	domain.TaskID,
	domain.RunID,
) (agentloop.RepositoryCompletionSummary, error) {
	harness.addEvent("repository:inspect")
	return harness.repositorySummary, nil
}

func (harness *agentStateMachineGateHarness) InspectFinalBudget(
	context.Context,
	domain.TaskID,
	domain.RunID,
) (agentloop.BudgetCompletionSummary, error) {
	harness.addEvent("budget:inspect")
	return harness.budgetSummary, nil
}

func (harness *agentStateMachineGateHarness) addEvent(value string) {
	harness.events = append(harness.events, value)
}

func gateApplyEditTool(t *testing.T) agentloop.ApprovedTool {
	t.Helper()
	var descriptor executor.ToolDescriptor
	for _, candidate := range executor.ToolCatalog() {
		if candidate.Name == executor.ToolApplyEdit {
			descriptor = candidate
			break
		}
	}
	if descriptor.Name == "" {
		t.Fatal("apply-edit descriptor unavailable")
	}
	return agentloop.ApprovedTool{
		Descriptor: descriptor,
		Arguments: []agentloop.ToolArgumentDefinition{
			{Name: "path", Required: true, MaxBytes: 4096},
			{Name: "content", Required: true, Sensitive: true, MaxBytes: 32768},
		},
		DefaultTimeout: time.Minute,
		MaterialEdit:   true, CreatesCheckpoint: true,
	}
}

func gateKnownUsage() providers.Usage {
	return providers.Usage{
		Known: true, Source: providers.UsageSourceProvider,
		InputTokens: 1, OutputTokens: 1,
	}
}

func gateKnownCost(
	t *testing.T,
	numerator int64,
	denominator int64,
) providers.ExactAmount {
	t.Helper()
	value, err := providers.NewExactAmount("USD", numerator, denominator)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func gateModelRequestID(
	t *testing.T,
	number int,
) domain.ModelRequestID {
	t.Helper()
	value, err := domain.ParseModelRequestID(
		"mrq_01890f3c-4a00-7abc-8def-" +
			fmt.Sprintf("%012x", number),
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func repairCoordinatorCheckpointIDForGate() domain.CheckpointID {
	value, _ := domain.ParseCheckpointID(
		"ckp_01890f3c-4a00-7abc-8def-000000000900",
	)
	return value
}
