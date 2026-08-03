package coordinator

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// runGoTestsForGraphFixture is one scripted turn that runs the project's own
// test command. engine_refinement_test.go already has an equivalent runTests
// helper, but it lives behind the `integration` build tag, so an ordinary
// `go test ./internal/coordinator/` run never links it. This graph-focused
// test does not need anything else that tag gates, so it defines its own
// rather than pulling the tag in.
func runGoTestsForGraphFixture(input agentloop.ModelInput) agentloop.ModelTurn {
	arguments, _ := json.Marshal(map[string]string{
		"executable": "go", "arg1": "test", "arg2": "./...",
	})
	callID, _ := domain.NewEventID()
	return agentloop.ModelTurn{ToolCalls: []agentloop.ModelToolCall{{
		Call: providers.ToolCall{
			ID: callID.String(), Name: string(executor.ToolTest),
			Arguments: arguments,
		},
		PlanStepID: stepAccepting(input, executor.ToolTest),
	}}}
}

// TestTheEngineDrawsSixDistinctGraphNodeClassesForARealRun is REPO-044's
// discriminating end-to-end proof.
//
// Before this change, agentGraphRecorder — the only projector caller wired
// into a live task run — emitted exactly three of the projector's seven
// NodeClass values (Requirement, AtomOperation, Effect): declarePlan never
// projected a plan region, and nothing ever called a validation or artifact
// emitter, even though internal/graph/projector.go implemented all seven. A
// real run's diagram showed edits and commands and never the plan,
// validation, or artifact structure the run actually has.
//
// This drives the real booted coordinator over real SQLite exactly the way
// TestTheEngineCarriesARequirementThroughToRealWorkOnDisk does — intake,
// forecast, approval, preflight binding, start, a real Git worktree, the real
// agent loop, the real tool executor — with a model scripted to write a
// source file, write its test file, and then run the real test suite, so
// every one of the newly wired emit sites fires for real: declarePlan draws
// the plan region once the plan it names is durably recorded,
// narratingExecutor's ToolTest branch draws a validation obligation once the
// test run's verdict is known, and persistProducedFile's new call draws an
// artifact once RecordProducedArtifact has actually committed the file.
//
// It discriminates six ways at once: commenting out declarePlan's
// graph.ProjectionPlanDeclared record removes the PlanRegion class (and
// starves every operation of a real plan-step predecessor); commenting out
// agent_narration.go's recordValidation call for executor.ToolTest removes
// the Obligation class; commenting out its recordArtifact call after
// RecordProducedArtifact removes the ArtifactResult class. Requirement,
// AtomOperation, and Effect were already covered before this ticket and stay
// covered here so a regression in any of the three does not read as
// unrelated to a passing test.
func TestTheEngineDrawsSixDistinctGraphNodeClassesForARealRun(t *testing.T) {
	const requirement = "Create greet.go and greet_test.go so that Greet returns hello."
	const program = "package main\n\n" +
		"// Greet returns the greeting this program exists to produce.\n" +
		"func Greet() string {\n\treturn \"hello\"\n}\n"
	const test = "package main\n\nimport \"testing\"\n\n" +
		"func TestGreet(t *testing.T) {\n" +
		"\tif Greet() != \"hello\" {\n\t\tt.Fatalf(\"Greet() = %q\", Greet())\n\t}\n}\n"

	engine := startEngineFixture(t, &scriptedEngineModel{
		turns: []func(agentloop.ModelInput) agentloop.ModelTurn{
			writeFile("greet.go", program),
			writeFile("greet_test.go", test),
			runGoTestsForGraphFixture,
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
		AffectedPackages:         []string{"workspace"},
		IdempotencyKey:           "graph-shapes-requirement",
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
		"graph-shapes-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "graph-shapes-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}

	// The run drives itself to completion detached from this goroutine, same
	// as every other engine test, so what is actually in the database is
	// polled rather than assumed. A validation obligation and an artifact node
	// are the last two classes to appear — they trail the test tool call and
	// the produced-file write — so waiting for six distinct classes is a
	// tighter, more honest condition than waiting for any fixed row count.
	task, err := engine.repositories.GetTask(ctx, created.TaskID)
	if err != nil {
		t.Fatalf("reading the task failed: %v", err)
	}
	repository, err := engine.repositories.GetRepository(ctx, task.RepositoryID)
	if err != nil {
		t.Fatalf("reading the repository failed: %v", err)
	}
	queryService, err := NewGraphQueryService(engine.repositories)
	if err != nil {
		t.Fatal(err)
	}
	scope := transport.GraphQueryScope{ProjectID: repository.ProjectID, TaskID: created.TaskID}

	classesOf := func() map[graph.NodeClass]int {
		view, err := queryService.GetGraphSlice(ctx, transport.GraphSliceQuery{
			Scope: scope, Mode: graph.ModeProgram, MaxNodes: 64, MaxEdges: 64,
		})
		if err != nil {
			return nil
		}
		classes := map[graph.NodeClass]int{}
		for _, node := range view.Nodes {
			classes[node.Node.Class()]++
		}
		return classes
	}

	var observed map[graph.NodeClass]int
	engine.waitFor(t, "the run to draw six distinct node classes", func() bool {
		observed = classesOf()
		return len(observed) >= 6
	})

	for _, want := range []graph.NodeClass{
		graph.NodeClassRequirement, graph.NodeClassPlanRegion,
		graph.NodeClassAtomOperation, graph.NodeClassEffect,
		graph.NodeClassObligation, graph.NodeClassArtifactResult,
	} {
		if observed[want] == 0 {
			t.Errorf("no %s node projected by a real run; observed classes = %#v\nThe run said:\n%s",
				want, observed, engine.narration())
		}
	}
}

// TestAgentGraphRecorderProjectsSixDistinctNodeClassesAgainstARealDurablePlan
// is REPO-044's second discriminating proof, isolated from the live agent
// loop so it does not depend on the tool executor, the worktree, or the
// verification-stage machinery those exercise. It gets there the way
// TestPIPE019a_MultiLineAcceptanceRequirementReachesABoundPlan does — real
// intake, a real recorded requirement, a real context manifest, and a real
// RecordPlanRevision — so plan.Revision here is not a fake number the way the
// no-durable-revision test's is: it is a plan the store genuinely has, and
// graph_plan_bindings' foreign key onto agent_plan_revisions(task_id,
// revision) actually accepts it.
//
// With that real plan in hand, it drives agentGraphRecorder's methods
// directly and asserts all six wired classes appear. It discriminates the
// same three ways TestTheEngineDrawsSixDistinctGraphNodeClassesForARealRun
// does — commenting out declarePlan's, recordValidation's, or
// recordArtifact's graph.ProjectionEvent record call each removes exactly one
// of PlanRegion, Obligation, or ArtifactResult and nothing else — and was
// added because the full end-to-end proof depends on the live agent loop and
// tool executor, which several other lanes are actively changing; this proof
// does not.
func TestAgentGraphRecorderProjectsSixDistinctNodeClassesAgainstARealDurablePlan(t *testing.T) {
	const requirement = "Create cmd/answer/main.go so the program prints the answer."

	engine := startEngineFixture(t, &scriptedEngineModel{})
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
		AffectedPackages:         []string{"cmd/answer"},
		IdempotencyKey:           "graph-shapes-durable-plan-requirement",
	})
	if err != nil {
		t.Fatalf("intake refused the requirement: %v", err)
	}

	analysis, err := storage.AnalyzeTaskRequirement(requirement)
	if err != nil {
		t.Fatalf("analysing the requirement failed: %v", err)
	}
	requirementRevision, err := engine.repositories.RecordTaskRequirement(ctx,
		storage.RecordTaskRequirement{
			TaskID: created.TaskID, MessageID: requestID,
			Analysis:       analysis,
			IdempotencyKey: "graph-shapes-durable-plan-requirement-record",
		})
	if err != nil {
		t.Fatalf("recording the requirement failed: %v", err)
	}

	task, err := engine.repositories.GetTask(ctx, created.TaskID)
	if err != nil {
		t.Fatalf("reading the task failed: %v", err)
	}
	repository, err := engine.repositories.GetRepository(ctx, task.RepositoryID)
	if err != nil {
		t.Fatalf("reading the repository failed: %v", err)
	}

	const repositoryRevision = "2222222222222222222222222222222222222222"
	stepNodeID, err := domain.NewNodeID()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := storage.BuildAgentPlan(analysis, storage.AgentPlanDraft{
		Goal:               "Print the answer.",
		Scope:              []string{"cmd/answer"},
		ExpectedFiles:      []string{"cmd/answer/main.go"},
		ValidationCommands: canonicalTestCommand(),
		Steps: []storage.AgentPlanStepDraft{{
			Kind:           storage.StepKindEdit,
			Title:          "Write cmd/answer/main.go",
			DetailRedacted: "Write the program that prints the answer.",
			ExpectedFiles:  []string{"cmd/answer/main.go"},
			GraphNodeIDs:   []domain.NodeID{stepNodeID},
		}},
		CompletionCriteria: []string{"The program prints the answer."},
	})
	if err != nil {
		t.Fatalf("building the plan failed: %v", err)
	}
	manifestID := strings.Repeat("7", 64)
	if _, err := engine.repositories.RecordContextManifest(ctx,
		storage.RecordContextManifest{
			ID: manifestID, RepositoryID: task.RepositoryID,
			RepositoryRevision: repositoryRevision,
			MapRevision:        digestOf(repositoryRevision),
			RequirementSHA256:  requirementRevision.OriginalBodySHA256,
			SelectionPolicy:    1, MaxFiles: 64, MaxBytes: 1 << 20, MaxEstimatedTokens: 200000,
		}); err != nil {
		t.Fatalf("recording the plan context failed: %v", err)
	}
	budget, err := engine.repositories.GetBudgetSnapshot(ctx, created.TaskID)
	if err != nil {
		t.Fatalf("reading the task budget failed: %v", err)
	}
	recordedPlan, err := engine.repositories.RecordPlanRevision(ctx,
		storage.RecordPlanRevision{
			TaskID: created.TaskID, RequirementRevision: requirementRevision.Revision,
			RepositoryRevision:     repositoryRevision,
			ContextManifestID:      manifestID,
			PolicyRevision:         created.PolicyRevision,
			ForecastRevision:       created.ForecastRevision,
			BudgetID:               budget.BudgetID,
			BudgetLimitRevision:    budget.LimitRevision,
			BudgetSnapshotRevision: budget.Revision,
			Plan:                   plan,
			IdempotencyKey:         "graph-shapes-durable-plan-record",
		})
	if err != nil {
		t.Fatalf("recording the plan failed: %v", err)
	}
	if recordedPlan.Revision == 0 || len(recordedPlan.Plan.Steps) == 0 {
		t.Fatalf("recorded plan is incomplete: %#v", recordedPlan)
	}
	durableStepID := recordedPlan.Plan.Steps[0].ID

	service, err := NewGraphProjectionService(engine.repositories, nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := newAgentGraphRecorder(ctx, service, engine.repositories,
		repository.ProjectID, created.TaskID, requirement)
	if !recorder.available || recorder.Failure() != nil {
		t.Fatalf("recorder unavailable after construction: %v", recorder.Failure())
	}

	recorder.declarePlan(ctx,
		durablePlan{Revision: recordedPlan.Revision, Steps: map[string]string{"edit-1": durableStepID}},
		[]graphPlanStep{{ID: durableStepID, Summary: "Write cmd/answer/main.go"}},
	)
	if recorder.Failure() != nil {
		t.Fatalf("declarePlan against a real durable plan failed: %v", recorder.Failure())
	}
	if recorder.planRegion == recorder.requirement {
		t.Fatal("declarePlan left the plan region aliased to the requirement even though a real plan revision was recorded")
	}

	editNode := recorder.recordFileEdit(ctx, durableStepID, "cmd/answer/main.go", recordedPlan.Revision, true)
	if editNode.IsZero() {
		t.Fatalf("recordFileEdit did not draw a node: %v", recorder.Failure())
	}
	recorder.recordArtifact(ctx, graph.ArtifactChangedFile, editNode, durableStepID, "cmd/answer/main.go", recordedPlan.Revision, true)
	if recorder.Failure() != nil {
		t.Fatalf("recordArtifact failed: %v", recorder.Failure())
	}
	recorder.recordValidation(ctx, durableStepID, "go test ./...", recordedPlan.Revision, true)
	if recorder.Failure() != nil {
		t.Fatalf("recordValidation failed: %v", recorder.Failure())
	}
	recorder.recordCommand(ctx, durableStepID, "go test ./...", recordedPlan.Revision, true)
	if recorder.Failure() != nil {
		t.Fatalf("recordCommand failed: %v", recorder.Failure())
	}

	revision, ok := recorder.projection.Revision()
	if !ok {
		t.Fatal("no committed graph revision after recording")
	}
	classes := map[graph.NodeClass]int{}
	for _, node := range revision.Nodes() {
		classes[node.Class()]++
	}
	for _, want := range []graph.NodeClass{
		graph.NodeClassRequirement, graph.NodeClassPlanRegion,
		graph.NodeClassAtomOperation, graph.NodeClassEffect,
		graph.NodeClassObligation, graph.NodeClassArtifactResult,
	} {
		if classes[want] == 0 {
			t.Fatalf("no %s node projected; observed classes = %#v", want, classes)
		}
	}
}

// TestAgentGraphRecorderDeclarePlanWithoutDurableRevisionProjectsNothing
// proves declarePlan does not draw a plan region when recordDurablePlan
// never committed one (plan.Revision == 0): the honest behavior this ticket
// requires is an absent node, not one that names a plan that does not exist.
func TestAgentGraphRecorderDeclarePlanWithoutDurableRevisionProjectsNothing(t *testing.T) {
	ctx := t.Context()
	repositories := openGraphProjectionRepositories(t)
	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	sessionID, _ := domain.NewSessionID()
	taskID, _ := domain.NewTaskID()
	if _, err := repositories.CreateProject(ctx, storage.CreateProject{ID: projectID, Name: "Agent graph recorder no plan"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(ctx, storage.CreateRepository{ID: repositoryID, ProjectID: projectID, CanonicalPath: filepath.Join(t.TempDir(), "repository"), GitIdentity: "agent-graph-recorder-no-plan"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Agent graph recorder no plan"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateSession(ctx, storage.CreateSession{ID: sessionID, ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateTask(ctx, storage.CreateTask{ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetCorrectness, ReasoningEffort: domain.ReasoningEffortMaximum,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelContractChecked,
		IdempotencyKey: "agent-graph-recorder-no-plan-task"}); err != nil {
		t.Fatal(err)
	}

	service, err := NewGraphProjectionService(repositories, nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := newAgentGraphRecorder(ctx, service, repositories, projectID, taskID, "Build the thing")
	if !recorder.available {
		t.Fatalf("recorder unavailable after construction: %v", recorder.Failure())
	}

	recorder.declarePlan(ctx, durablePlan{}, []graphPlanStep{{ID: "edit-1", Summary: "Write cmd/generated/main.go"}})
	if recorder.Failure() != nil {
		t.Fatalf("declarePlan with no durable revision should be a no-op, not a failure: %v", recorder.Failure())
	}
	if recorder.planRegion != recorder.requirement {
		t.Fatalf("declarePlan with plan.Revision == 0 projected a plan region anyway: %s", recorder.planRegion)
	}
	revision, ok := recorder.projection.Revision()
	if !ok {
		t.Fatal("no committed graph revision after recording the requirement")
	}
	for _, node := range revision.Nodes() {
		if node.Class() == graph.NodeClassPlanRegion {
			t.Fatalf("plan region %s was projected with no durable plan behind it", node.ID())
		}
	}
}
