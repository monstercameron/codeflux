package storage

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestAnalyzeTaskRequirementAndBuildAgentPlanAreDeterministic(t *testing.T) {
	body := strings.Join([]string{
		"Implement Widget in internal/widget.go if needed.",
		"`Widget` must return the configured value.",
		"go test ./internal/widget",
		"Acceptance: the targeted package tests pass.",
	}, "\n")
	first, err := AnalyzeTaskRequirement(body)
	if err != nil {
		t.Fatal(err)
	}
	second, err := AnalyzeTaskRequirement(body)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("deterministic analyses differ: %#v / %#v", first, second)
	}
	if first.TaskType != TaskTypeFeature ||
		first.RiskLevel != domain.RiskLevelRoutine ||
		first.ValidationProfile != ValidationProfileRoutine ||
		!containsString(first.ExplicitFiles, "internal/widget.go") ||
		!containsString(first.ExplicitSymbols, "Widget") ||
		!containsString(first.ExplicitCommands, "go test ./internal/widget") ||
		len(first.Assumptions) != 1 ||
		first.RequiresClarification() {
		t.Fatalf("requirement analysis = %#v", first)
	}
	nodeID, err := domain.NewNodeID()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildAgentPlan(first, AgentPlanDraft{
		Goal:          "Implement the bounded widget behavior.",
		Scope:         []string{"internal widget package"},
		ExpectedFiles: []string{"internal/widget.go"},
		Steps: []AgentPlanStepDraft{{
			Kind:           StepKindEdit,
			Title:          "Implement widget",
			DetailRedacted: "Update the widget behavior within the named package.",
			ExpectedFiles:  []string{"internal/widget.go"},
			GraphNodeIDs:   []domain.NodeID{nodeID},
		}},
		Risks:              []string{"targeted behavior change"},
		CompletionCriteria: []string{"targeted tests pass"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].ID != "step-001" ||
		plan.Steps[0].Kind != StepKindEdit ||
		!reflect.DeepEqual(plan.Steps[0].CompletionTools, []string{"apply-edit"}) ||
		len(plan.Assumptions) != 1 ||
		!strings.Contains(plan.UserSummary(), "step-001") ||
		!strings.Contains(
			plan.UserSummary(), "[kind=edit; completion-tools=apply-edit;",
		) ||
		!strings.Contains(plan.UserSummary(), "Assumptions:") ||
		!strings.Contains(plan.UserSummary(), "Expected files: internal/widget.go") ||
		!strings.Contains(
			plan.UserSummary(),
			"Requested validation commands: go test ./internal/widget",
		) ||
		!strings.Contains(plan.UserSummary(), `"tool":"test"`) ||
		!strings.Contains(plan.UserSummary(), "Completion criteria:") ||
		!strings.Contains(plan.UserSummary(), "Update the widget behavior") {
		t.Fatalf("agent plan = %#v, summary = %q", plan, plan.UserSummary())
	}
	for name, mutate := range map[string]func(*AgentPlan){
		"unknown-kind": func(value *AgentPlan) {
			value.Steps[0].Kind = AgentPlanStepKind("shell")
			value.Steps[0].CompletionTools = nil
		},
		"kind-tool-downgrade": func(value *AgentPlan) {
			value.Steps[0].Kind = StepKindReadFile
			value.Steps[0].CompletionTools = []string{"read-file"}
		},
		"missing-edit-files": func(value *AgentPlan) {
			value.Steps[0].ExpectedFiles = nil
		},
		"uncovered-top-level-file": func(value *AgentPlan) {
			value.ExpectedFiles = append(value.ExpectedFiles, "internal/other.go")
		},
		"raw-step-validation-label": func(value *AgentPlan) {
			value.Steps[0].ValidationCommands =
				[]string{"go test ./internal/widget"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			malformed := plan
			malformed.Steps = append([]AgentPlanStep(nil), plan.Steps...)
			mutate(&malformed)
			if err := malformed.Validate(); err == nil {
				t.Fatalf("%s unexpectedly validated", name)
			}
		})
	}

	investigation, err := AnalyzeTaskRequirement(strings.Join([]string{
		"Investigate why internal/widget.go is failing.",
		"go test ./internal/widget",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	readOnly, err := BuildAgentPlan(investigation, AgentPlanDraft{
		Goal:          "Inspect the named widget failure.",
		Scope:         []string{"internal widget package"},
		ExpectedFiles: nil,
		Steps: []AgentPlanStepDraft{{
			Kind:           StepKindReadFile,
			Title:          "Read widget source",
			DetailRedacted: "Inspect the current widget implementation.",
			ExpectedFiles:  []string{"internal/widget.go"},
		}},
		CompletionCriteria: []string{"cause is identified"},
	})
	if err != nil {
		t.Fatalf("read-only investigation = %v", err)
	}
	if !reflect.DeepEqual(
		readOnly.Steps[0].CompletionTools, []string{"read-file"},
	) || !reflect.DeepEqual(
		readOnly.ExpectedFiles, []string{"internal/widget.go"},
	) {
		t.Fatalf("derived investigation tools = %#v", readOnly.Steps[0])
	}
	mismatchedInvestigation := readOnly
	mismatchedInvestigation.Steps = append(
		[]AgentPlanStep(nil), readOnly.Steps...,
	)
	mismatchedInvestigation.Steps[0].ExpectedFiles =
		[]string{"internal/other.go"}
	if err := mismatchedInvestigation.Validate(); err == nil {
		t.Fatal("investigation with unrelated step scope unexpectedly validated")
	}
	descendantInvestigation := readOnly
	descendantInvestigation.ExpectedFiles = []string{"internal/widget"}
	descendantInvestigation.Steps = append(
		[]AgentPlanStep(nil), readOnly.Steps...,
	)
	descendantInvestigation.Steps[0].ExpectedFiles =
		[]string{"internal/widget/source.go"}
	if err := descendantInvestigation.Validate(); err != nil {
		t.Fatalf("descendant investigation scope = %v", err)
	}
	for _, body := range []string{
		"Investigate the current behavior, then implement the fix.",
		"Investigate and change the widget.",
		"Analyze and modify the widget.",
		"Investigate and update the widget.",
		"Analyze and remove the widget.",
		"Investigate and delete the widget.",
		"Diagnose and replace the widget.",
		"Investigate and migrate the widget.",
	} {
		mixed, err := AnalyzeTaskRequirement(body)
		if err != nil {
			t.Fatal(err)
		}
		if mixed.TaskType == TaskTypeInvestigation {
			t.Fatalf("mixed change intent was downgraded for %q", body)
		}
	}
	for _, unsafeBody := range []string{
		"Implement the widget.\ngofmt -w internal/widget.go\nAcceptance: done.",
		"Implement the widget.\ngo test ./...; rm -rf output\nAcceptance: done.",
		"Implement the widget.\ngo test API_KEY=value\nAcceptance: done.",
		"Implement the widget.\ngo test sk-proj-sensitive-placeholder\nAcceptance: done.",
	} {
		unsafe, err := AnalyzeTaskRequirement(unsafeBody)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := BuildAgentPlan(unsafe, AgentPlanDraft{
			Goal:  "Implement the widget safely.",
			Scope: []string{"widget"},
			Steps: []AgentPlanStepDraft{{
				Kind: StepKindEdit, Title: "Edit widget",
				DetailRedacted: "Apply the bounded widget change.",
				ExpectedFiles:  []string{"internal/widget.go"},
			}},
			ExpectedFiles:      []string{"internal/widget.go"},
			CompletionCriteria: []string{"validation succeeds"},
		}); err == nil {
			t.Fatalf("unsafe validation command unexpectedly planned: %q", unsafeBody)
		}
	}
	for name, tools := range map[string][]string{
		"empty":     nil,
		"duplicate": {"apply-edit", "apply-edit"},
		"unknown":   {"unregistered-tool"},
	} {
		t.Run("completion-tools-"+name, func(t *testing.T) {
			malformed := plan
			malformed.Steps = append([]AgentPlanStep(nil), plan.Steps...)
			malformed.Steps[0].CompletionTools = tools
			if err := malformed.Validate(); err == nil {
				t.Fatalf("completion tools %#v unexpectedly validated", tools)
			}
		})
	}

	blocking, err := AnalyzeTaskRequirement(
		"Implement either the local adapter or whichever remote adapter is preferred.",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !blocking.RequiresClarification() ||
		blocking.ClarificationQuestion() == "" {
		t.Fatalf("blocking ambiguity = %#v", blocking)
	}
}

func TestRecordTaskRequirementRejectsCallerForgedRiskDowngrade(t *testing.T) {
	fixture := createAgentPlanFixtureWithBody(
		t,
		6050,
		strings.Join([]string{
			"Delete the production migration after verifying the protected change.",
			"go test ./internal/widget",
			"go vet ./internal/widget",
			"make test",
		}, "\n"),
	)
	if fixture.requirement.Analysis.RiskLevel != domain.RiskLevelProtected {
		t.Fatalf(
			"fixture risk = %s",
			fixture.requirement.Analysis.RiskLevel,
		)
	}
	forged := fixture.requirement.Analysis
	forged.RiskLevel = domain.RiskLevelRoutine
	forged.ValidationProfile = ValidationProfileRoutine
	if _, err := fixture.repositories.RecordTaskRequirement(
		t.Context(),
		RecordTaskRequirement{
			TaskID: fixture.task.ID, MessageID: fixture.message.ID,
			Analysis: forged, IdempotencyKey: "agent-plan-requirement",
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("forged requirement analysis error = %v", err)
	}
}

func TestRecordPlanRevisionRejectsDurableRequirementDowngrade(t *testing.T) {
	for index, testCase := range []struct {
		name   string
		mutate func(*AgentPlan)
	}{
		{
			name: "investigation-task-type",
			mutate: func(plan *AgentPlan) {
				plan.TaskType = TaskTypeInvestigation
			},
		},
		{
			name: "substituted-explicit-file",
			mutate: func(plan *AgentPlan) {
				plan.ExpectedFiles = []string{"internal/substitute.go"}
				plan.Steps[0].ExpectedFiles = []string{"internal/substitute.go"}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := createAgentPlanFixture(t, 6060+index*100)
			message, err := fixture.repositories.AppendMessage(
				t.Context(),
				AppendMessage{
					ID:             testMessageID(t, 6090+index*100),
					ThreadID:       fixture.task.ThreadID,
					Role:           MessageRoleUser,
					BodyRedacted:   "Keep the durable requirement unchanged.",
					IdempotencyKey: "semantic-downgrade-message",
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			plan := fixture.plan.Plan
			plan.ExpectedFiles = append([]string(nil), plan.ExpectedFiles...)
			plan.Steps = append([]AgentPlanStep(nil), plan.Steps...)
			plan.Steps[0].ExpectedFiles = append(
				[]string(nil), plan.Steps[0].ExpectedFiles...,
			)
			testCase.mutate(&plan)
			if err := plan.Validate(); err != nil {
				t.Fatalf("forged plan must be internally valid: %v", err)
			}
			previous := fixture.plan.Revision
			input := fixture.planInput
			input.Plan = plan
			input.SupersedesRevision = &previous
			input.RedirectMessageID = &message.ID
			input.IdempotencyKey = "semantic-downgrade-plan"
			if _, err := fixture.repositories.RecordPlanRevision(
				t.Context(), input,
			); !errors.Is(err, ErrConstraint) {
				t.Fatalf("semantic downgrade error = %v", err)
			}
		})
	}
}

func TestRequirementAndPlanRevisionsPersistOriginalMessageAndSupersession(
	t *testing.T,
) {
	fixture := createAgentPlanFixture(t, 5000)
	if fixture.requirement.MessageID != fixture.message.ID ||
		fixture.requirement.OriginalBodySHA256 != hashJSON(fixture.message.BodyRedacted) ||
		fixture.plan.Revision != 1 ||
		fixture.plan.ContentSHA256 == "" ||
		fixture.plan.PresentationJSON == "" ||
		!strings.Contains(fixture.plan.PresentationJSON, `"forecast_budget"`) ||
		!reflect.DeepEqual(
			fixture.plan.Plan.Steps[0].CompletionTools,
			[]string{"apply-edit"},
		) ||
		fixture.plan.ApprovalRequired {
		t.Fatalf("requirement = %#v, plan = %#v", fixture.requirement, fixture.plan)
	}
	var stepKind, completionToolsJSON string
	if err := fixture.repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT step_kind, completion_tools_json
		 FROM agent_plan_steps
		 WHERE task_id = ? AND plan_revision = ? AND step_id = ?`,
		fixture.task.ID,
		fixture.plan.Revision,
		fixture.plan.Plan.Steps[0].ID,
	).Scan(&stepKind, &completionToolsJSON); err != nil {
		t.Fatal(err)
	}
	if stepKind != string(StepKindEdit) ||
		completionToolsJSON != `["apply-edit"]` {
		t.Fatalf(
			"persisted step contract = kind %q, tools %s",
			stepKind, completionToolsJSON,
		)
	}
	retried, err := fixture.repositories.RecordPlanRevision(
		t.Context(),
		fixture.planInput,
	)
	if err != nil || retried.ContentSHA256 != fixture.plan.ContentSHA256 {
		t.Fatalf("idempotent plan = %#v, %v", retried, err)
	}
	if _, err := fixture.repositories.database.sql.ExecContext(
		t.Context(),
		`UPDATE agent_plan_revisions SET user_summary = 'rewritten'
		 WHERE task_id = ? AND revision = 1`,
		fixture.task.ID,
	); err == nil {
		t.Fatal("immutable plan update unexpectedly succeeded")
	}
	if _, err := fixture.repositories.database.sql.ExecContext(
		t.Context(),
		`DELETE FROM task_requirement_revisions
		 WHERE task_id = ? AND revision = 1`,
		fixture.task.ID,
	); err == nil {
		t.Fatal("immutable requirement delete unexpectedly succeeded")
	}

	redirect, err := fixture.repositories.AppendMessage(
		t.Context(),
		AppendMessage{
			ID: testMessageID(t, 5050), ThreadID: fixture.task.ThreadID,
			Role:           MessageRoleUser,
			BodyRedacted:   "Keep the same scope but validate formatting too.",
			IdempotencyKey: "plan-redirect-message",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	revision := fixture.plan.Revision
	redirectedPlan := fixture.plan.Plan
	extraCommands, err := canonicalRequirementValidationCommands(
		[]string{"go vet ./internal/widget"},
	)
	if err != nil {
		t.Fatal(err)
	}
	redirectedPlan.ValidationCommands = normalizedStrings(append(
		redirectedPlan.ValidationCommands,
		extraCommands...,
	))
	redirectInput := fixture.planInput
	redirectInput.Plan = redirectedPlan
	redirectInput.SupersedesRevision = &revision
	redirectInput.RedirectMessageID = &redirect.ID
	redirectInput.IdempotencyKey = "redirected-agent-plan"
	redirected, err := fixture.repositories.RecordPlanRevision(
		t.Context(),
		redirectInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	if redirected.Revision != 2 ||
		redirected.SupersedesRevision == nil ||
		*redirected.SupersedesRevision != 1 ||
		redirected.RedirectMessageID == nil ||
		*redirected.RedirectMessageID != redirect.ID {
		t.Fatalf("redirected plan = %#v", redirected)
	}

	task := transitionTaskFixtureToReady(
		t,
		fixture.repositories,
		fixture.task,
		5060,
	)
	preflight, err := fixture.repositories.PrepareTaskExecution(
		t.Context(),
		PrepareTaskExecution{
			TaskID: task.ID, ExpectedTaskRevision: task.Revision,
			PolicyRevision:   fixture.policyRevision,
			ForecastRevision: fixture.forecastRevision,
			BudgetID:         fixture.budgetID, BudgetLimitRevision: 0,
			IdempotencyKey: "agent-plan-preflight",
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
			RunID: runID, EventID: testEventID(t, 5070), TaskID: task.ID,
			PreflightRevision:    preflight.Revision,
			ExpectedTaskRevision: task.Revision, Attempt: 1,
			IdempotencyKey:      "agent-plan-run",
			EventIdempotencyKey: "agent-plan-run-event",
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.BindRunPlan(
		t.Context(),
		BindRunPlan{
			RunID: runID, TaskID: task.ID, PlanRevision: 1,
			IdempotencyKey: "bind-superseded-plan",
		},
	); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("superseded plan bind error = %v", err)
	}
	bound, err := fixture.repositories.BindRunPlan(
		t.Context(),
		BindRunPlan{
			RunID: runID, TaskID: task.ID, PlanRevision: redirected.Revision,
			IdempotencyKey: "bind-current-plan",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if bound.PlanRevision != redirected.Revision ||
		bound.PlanSHA256 != redirected.ContentSHA256 ||
		bound.PolicyRevision != redirected.PolicyRevision {
		t.Fatalf("run plan binding = %#v", bound)
	}
}

func TestProtectedPlanRequiresExactGrantedApproval(t *testing.T) {
	body := strings.Join([]string{
		"Implement credential authentication in internal/widget.go.",
		"go test ./internal/widget",
		"go vet ./internal/widget",
		"make test",
		"Acceptance: authentication tests pass.",
	}, "\n")
	fixture := createAgentPlanFixtureWithBody(t, 5100, body)
	if !fixture.plan.ApprovalRequired ||
		fixture.plan.RiskLevel != domain.RiskLevelProtected ||
		fixture.plan.ValidationProfile != ValidationProfileProtected {
		t.Fatalf("protected plan = %#v", fixture.plan)
	}
	task := transitionTaskFixtureToReady(
		t, fixture.repositories, fixture.task, 5150,
	)
	preflight, err := fixture.repositories.PrepareTaskExecution(
		t.Context(),
		PrepareTaskExecution{
			TaskID: task.ID, ExpectedTaskRevision: task.Revision,
			PolicyRevision:   fixture.policyRevision,
			ForecastRevision: fixture.forecastRevision,
			BudgetID:         fixture.budgetID, BudgetLimitRevision: 0,
			IdempotencyKey: "protected-plan-preflight",
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
			RunID: runID, EventID: testEventID(t, 5160),
			TaskID: task.ID, PreflightRevision: preflight.Revision,
			ExpectedTaskRevision: task.Revision, Attempt: 1,
			IdempotencyKey:      "protected-plan-run",
			EventIdempotencyKey: "protected-plan-run-event",
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.BindRunPlan(
		t.Context(),
		BindRunPlan{
			RunID: runID, TaskID: task.ID, PlanRevision: fixture.plan.Revision,
			IdempotencyKey: "protected-plan-unapproved",
		},
	); !errors.Is(err, ErrConstraint) {
		t.Fatalf("unapproved protected plan bind error = %v", err)
	}
	wrongApprovalID, err := domain.NewApprovalID()
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := fixture.repositories.CreateApproval(
		t.Context(),
		CreateApproval{
			ID: wrongApprovalID, TaskID: task.ID,
			Scope: "unrelated-scope", RequestReason: "wrong scope test",
			IdempotencyKey: "wrong-plan-approval",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.ResolveApproval(
		t.Context(),
		ResolveApproval{
			ID: wrong.ID, ExpectedRevision: wrong.Revision,
			To:               domain.ApprovalRequestStateGranted,
			ResolutionReason: "granted for test",
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.ApprovePlanRevision(
		t.Context(),
		ApprovePlanRevision{
			TaskID: task.ID, PlanRevision: fixture.plan.Revision,
			ApprovalID: wrong.ID, IdempotencyKey: "wrong-plan-binding",
		},
	); !errors.Is(err, ErrConstraint) {
		t.Fatalf("wrong-scope approval error = %v", err)
	}
	approvalID, err := domain.NewApprovalID()
	if err != nil {
		t.Fatal(err)
	}
	approval, err := fixture.repositories.CreateApproval(
		t.Context(),
		CreateApproval{
			ID: approvalID, TaskID: task.ID,
			Scope:          PlanApprovalScope(fixture.plan),
			RequestReason:  "protected plan requires exact approval",
			IdempotencyKey: "exact-plan-approval",
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
			ResolutionReason: "explicitly approved",
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.ApprovePlanRevision(
		t.Context(),
		ApprovePlanRevision{
			TaskID: task.ID, PlanRevision: fixture.plan.Revision,
			ApprovalID: approval.ID, IdempotencyKey: "exact-plan-binding",
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.BindRunPlan(
		t.Context(),
		BindRunPlan{
			RunID: runID, TaskID: task.ID, PlanRevision: fixture.plan.Revision,
			IdempotencyKey: "protected-plan-approved",
		},
	); err != nil {
		t.Fatal(err)
	}
}

type agentPlanFixture struct {
	repositories     *Repositories
	task             Task
	message          Message
	requirement      TaskRequirementRevision
	plan             PlanRevision
	planInput        RecordPlanRevision
	policyRevision   uint64
	forecastRevision uint64
	budgetID         domain.BudgetID
}

func createAgentPlanFixture(t *testing.T, base int) agentPlanFixture {
	t.Helper()
	body := strings.Join([]string{
		"Implement Widget in internal/widget.go if needed.",
		"`Widget` must return the configured value.",
		"go test ./internal/widget",
		"Acceptance: the targeted package tests pass.",
	}, "\n")
	return createAgentPlanFixtureWithBody(t, base, body)
}

func createAgentPlanFixtureWithBody(
	t *testing.T,
	base int,
	body string,
) agentPlanFixture {
	t.Helper()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, base)
	repositoryID := testRepositoryID(t, base+1)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	threadID := testThreadID(t, base+2)
	if _, err := repositories.CreateThread(t.Context(), CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID,
		Title: "Agent plan fixture",
	}); err != nil {
		t.Fatal(err)
	}
	analysis, err := AnalyzeTaskRequirement(body)
	if err != nil {
		t.Fatal(err)
	}
	message, err := repositories.AppendMessage(t.Context(), AppendMessage{
		ID: testMessageID(t, base+3), ThreadID: threadID,
		Role: MessageRoleUser, BodyRedacted: body,
		IdempotencyKey: "agent-plan-message",
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repositories.CreateTask(t.Context(), CreateTask{
		ID: testTaskID(t, base+4), ThreadID: threadID,
		RepositoryID: repositoryID, RequestMessageID: &message.ID,
		PolicyPreset:      domain.PolicyPresetBalanced,
		ReasoningEffort:   domain.ReasoningEffortStandard,
		RiskLevel:         analysis.RiskLevel,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey:    "agent-plan-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	requirement, err := repositories.RecordTaskRequirement(
		t.Context(),
		RecordTaskRequirement{
			TaskID: task.ID, MessageID: message.ID, Analysis: analysis,
			IdempotencyKey: "agent-plan-requirement",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	contextInput := validContextManifestInput(repositoryID)
	contextInput.ID = strings.Repeat("7", 64)
	contextInput.RepositoryRevision = strings.Repeat("8", 40)
	contextInput.RequirementSHA256 = requirement.OriginalBodySHA256
	if _, err := repositories.RecordContextManifest(
		t.Context(),
		contextInput,
	); err != nil {
		t.Fatal(err)
	}
	policy, forecast, budgetID := recordPreflightInputs(
		t, repositories, task, base+10, nil,
	)
	nodeID, err := domain.NewNodeID()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildAgentPlan(analysis, AgentPlanDraft{
		Goal:          "Implement the bounded widget behavior.",
		Scope:         []string{"internal widget package"},
		ExpectedFiles: []string{"internal/widget.go"},
		Steps: []AgentPlanStepDraft{{
			Kind:           StepKindEdit,
			Title:          "Implement widget",
			DetailRedacted: "Update the widget behavior in the named package.",
			ExpectedFiles:  []string{"internal/widget.go"},
			GraphNodeIDs:   []domain.NodeID{nodeID},
		}},
		Risks:              []string{"targeted behavior change"},
		CompletionCriteria: []string{"targeted tests pass"},
	})
	if err != nil {
		t.Fatal(err)
	}
	planInput := RecordPlanRevision{
		TaskID: task.ID, RequirementRevision: requirement.Revision,
		RepositoryRevision:  contextInput.RepositoryRevision,
		ContextManifestID:   contextInput.ID,
		PolicyRevision:      policy.Revision,
		ForecastRevision:    forecast.Revision,
		BudgetID:            budgetID,
		BudgetLimitRevision: 0, BudgetSnapshotRevision: 0,
		Plan: plan, IdempotencyKey: "initial-agent-plan",
	}
	recorded, err := repositories.RecordPlanRevision(t.Context(), planInput)
	if err != nil {
		t.Fatal(err)
	}
	return agentPlanFixture{
		repositories: repositories, task: task, message: message,
		requirement: requirement, plan: recorded, planInput: planInput,
		policyRevision:   policy.Revision,
		forecastRevision: forecast.Revision,
		budgetID:         budgetID,
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
