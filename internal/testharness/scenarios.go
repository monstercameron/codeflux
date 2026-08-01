package testharness

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/testfixtures"
)

// ScenarioName identifies one named interactive fake scenario (M22-113).
//
// The set is closed and mirrors the twelve situations docs/plan.md requires be
// reproducible without a paid provider. Naming them here rather than letting
// each test invent its own means two tests exercising "recovery" exercise the
// same recovery.
type ScenarioName string

const (
	ScenarioSuccess          ScenarioName = "success"
	ScenarioPlanRevision     ScenarioName = "plan-revision"
	ScenarioApproval         ScenarioName = "approval"
	ScenarioDenial           ScenarioName = "denial"
	ScenarioRepair           ScenarioName = "repair"
	ScenarioBudgetCap        ScenarioName = "budget-cap"
	ScenarioProviderFailure  ScenarioName = "provider-failure"
	ScenarioReconnect        ScenarioName = "reconnect"
	ScenarioWorkerCrash      ScenarioName = "worker-crash"
	ScenarioCoordinatorCrash ScenarioName = "coordinator-crash"
	ScenarioConcurrentEdit   ScenarioName = "concurrent-edit"
	ScenarioRecovery         ScenarioName = "recovery"
)

// AllScenarioNames returns the twelve declared scenarios.
func AllScenarioNames() []ScenarioName {
	return []ScenarioName{
		ScenarioSuccess, ScenarioPlanRevision, ScenarioApproval, ScenarioDenial,
		ScenarioRepair, ScenarioBudgetCap, ScenarioProviderFailure, ScenarioReconnect,
		ScenarioWorkerCrash, ScenarioCoordinatorCrash, ScenarioConcurrentEdit,
		ScenarioRecovery,
	}
}

// Scenario is one fully described interactive fake (M22-113).
type Scenario struct {
	Name ScenarioName
	// Summary states, in one line, what a reader should conclude if it passes.
	Summary string
	// RepositoryState is the git condition the scenario starts from.
	RepositoryState testfixtures.RepositoryState
	// ProviderScript drives the model side.
	ProviderScript []testfixtures.ProviderStep
	// Fault, when set, is injected at the named point.
	Fault testfixtures.FaultPoint
	// ExpectedOutcome is the safe answer the user must be left with.
	ExpectedOutcome testfixtures.SafeOutcome
	// EventKinds is the durable event sequence the scenario produces, in order.
	EventKinds []string
	// RequiresApproval records whether the flow must stop for the user.
	RequiresApproval bool
}

// Validate rejects a scenario that could not be run or that claims something
// the harness cannot check.
func (scenario Scenario) Validate() error {
	if !slices.Contains(AllScenarioNames(), scenario.Name) {
		return fmt.Errorf("unknown scenario name %q", scenario.Name)
	}
	if strings.TrimSpace(scenario.Summary) == "" {
		return fmt.Errorf("scenario %q states no conclusion", scenario.Name)
	}
	if !scenario.RepositoryState.Valid() {
		return fmt.Errorf("scenario %q has invalid repository state %q",
			scenario.Name, scenario.RepositoryState)
	}
	if len(scenario.ProviderScript) == 0 {
		return fmt.Errorf("scenario %q has no provider script", scenario.Name)
	}
	for index, step := range scenario.ProviderScript {
		if err := step.Validate(); err != nil {
			return fmt.Errorf("scenario %q step %d: %w", scenario.Name, index, err)
		}
	}
	if !scenario.ExpectedOutcome.Valid() {
		return fmt.Errorf("scenario %q expects outcome %q, which is not one of the four safe answers",
			scenario.Name, scenario.ExpectedOutcome)
	}
	if len(scenario.EventKinds) == 0 {
		return fmt.Errorf("scenario %q produces no events, so nothing about it is observable",
			scenario.Name)
	}
	if scenario.Fault != "" && !slices.Contains(testfixtures.AllFaultPoints(), scenario.Fault) {
		return fmt.Errorf("scenario %q injects unknown fault %q", scenario.Name, scenario.Fault)
	}
	return nil
}

// NamedScenarios returns every declared scenario (M22-113).
func NamedScenarios() []Scenario {
	textStep := func(text string) testfixtures.ProviderStep {
		return testfixtures.ProviderStep{
			Kind: testfixtures.StepText, Text: text,
			Usage: testfixtures.FixtureUsage{UncachedInputTokens: 900, OutputTokens: 120},
		}
	}
	toolStep := func(tool, arguments string) testfixtures.ProviderStep {
		return testfixtures.ProviderStep{
			Kind: testfixtures.StepToolCall, ToolName: tool, ToolArgumentsJSON: arguments,
			Usage: testfixtures.FixtureUsage{UncachedInputTokens: 400, OutputTokens: 60},
		}
	}

	return []Scenario{
		{
			Name:            ScenarioSuccess,
			Summary:         "a routine change is planned, executed, validated, and accepted with no intervention",
			RepositoryState: testfixtures.StateClean,
			ProviderScript: []testfixtures.ProviderStep{
				textStep("I will add the missing bound and a test for it."),
				toolStep("apply-edit", `{"path":"internal/server/server.go"}`),
				toolStep("test", `{"package":"./internal/server"}`),
			},
			ExpectedOutcome: testfixtures.OutcomeResumable,
			EventKinds: []string{
				"task-created", "plan-created", "tool-started", "tool-completed",
				"validation-started", "validation-completed", "task-completed",
			},
		},
		{
			Name:            ScenarioPlanRevision,
			Summary:         "the user rejects the first plan and the agent produces a revised one that resets approval",
			RepositoryState: testfixtures.StateClean,
			ProviderScript: []testfixtures.ProviderStep{
				textStep("First plan: change the public signature."),
				textStep("Revised plan: keep the signature and add an overload."),
			},
			ExpectedOutcome:  testfixtures.OutcomeResumable,
			RequiresApproval: true,
			EventKinds: []string{
				"task-created", "plan-created", "plan-changed", "task-completed",
			},
		},
		{
			Name:            ScenarioApproval,
			Summary:         "a command needing authority stops for the user and proceeds once granted",
			RepositoryState: testfixtures.StateClean,
			ProviderScript: []testfixtures.ProviderStep{
				textStep("The dependency must be installed before the build can run."),
				toolStep("run-command", `{"argv":["go","get","example.com/dep"]}`),
			},
			ExpectedOutcome:  testfixtures.OutcomeResumable,
			RequiresApproval: true,
			EventKinds: []string{
				"task-created", "plan-created", "approval-requested",
				"approval-resolved", "tool-started", "tool-completed",
			},
		},
		{
			Name:            ScenarioDenial,
			Summary:         "a denied command is not retried through another tool and the task ends cleanly",
			RepositoryState: testfixtures.StateClean,
			ProviderScript: []testfixtures.ProviderStep{
				textStep("I will fetch the schema from the vendor endpoint."),
				toolStep("run-command", `{"argv":["curl","https://vendor.invalid/schema"]}`),
			},
			ExpectedOutcome:  testfixtures.OutcomeTerminatedCleanly,
			RequiresApproval: true,
			EventKinds: []string{
				"task-created", "plan-created", "approval-requested",
				"approval-resolved", "task-completed",
			},
		},
		{
			Name:            ScenarioRepair,
			Summary:         "a failing validation drives one bounded repair attempt that resets stale evidence",
			RepositoryState: testfixtures.StateClean,
			ProviderScript: []testfixtures.ProviderStep{
				textStep("Applying the change."),
				toolStep("test", `{"package":"./internal/server"}`),
				textStep("The test failed; repairing the off-by-one."),
			},
			ExpectedOutcome: testfixtures.OutcomeResumable,
			EventKinds: []string{
				"task-created", "plan-created", "validation-started",
				"validation-completed", "plan-changed", "task-completed",
			},
		},
		{
			Name:            ScenarioBudgetCap,
			Summary:         "reaching the hard budget stops new paid work and leaves the task resumable",
			RepositoryState: testfixtures.StateClean,
			ProviderScript: []testfixtures.ProviderStep{
				testfixtures.ProviderStep{
					Kind: testfixtures.StepText, Text: "This will take several turns.",
					Usage: testfixtures.FixtureUsage{UncachedInputTokens: 900_000, OutputTokens: 400_000},
				},
			},
			ExpectedOutcome: testfixtures.OutcomeResumable,
			EventKinds:      []string{"task-created", "plan-created", "budget-exhausted"},
		},
		{
			Name:            ScenarioProviderFailure,
			Summary:         "a rate limit is retried and an authentication failure is not",
			RepositoryState: testfixtures.StateClean,
			ProviderScript: []testfixtures.ProviderStep{
				{Kind: testfixtures.StepRateLimit, RetryAfter: 15 * time.Second},
				textStep("Retried successfully after the limit cleared."),
				{Kind: testfixtures.StepAuthFailure},
			},
			Fault:           testfixtures.FaultProviderRateLimited,
			ExpectedOutcome: testfixtures.OutcomeRetryable,
			EventKinds:      []string{"task-created", "plan-created", "task-failed"},
		},
		{
			Name:            ScenarioReconnect,
			Summary:         "the browser loses and regains transport without duplicating a delivered event",
			RepositoryState: testfixtures.StateClean,
			ProviderScript: []testfixtures.ProviderStep{
				textStep("Work continues while the UI is disconnected."),
			},
			Fault:           testfixtures.FaultCoordinatorAfterCommit,
			ExpectedOutcome: testfixtures.OutcomeResumable,
			EventKinds: []string{
				"task-created", "plan-created", "tool-started", "tool-completed",
			},
		},
		{
			Name:            ScenarioWorkerCrash,
			Summary:         "a worker dying mid-edit leaves the worktree reconcilable rather than half-applied",
			RepositoryState: testfixtures.StateClean,
			ProviderScript: []testfixtures.ProviderStep{
				toolStep("apply-edit", `{"path":"internal/server/server.go"}`),
			},
			Fault:           testfixtures.FaultWorkerDuringFileEdit,
			ExpectedOutcome: testfixtures.OutcomeRequiresReconciliation,
			EventKinds:      []string{"task-created", "plan-created", "tool-started"},
		},
		{
			Name:            ScenarioCoordinatorCrash,
			Summary:         "the coordinator dying before an event commit loses nothing and duplicates nothing",
			RepositoryState: testfixtures.StateClean,
			ProviderScript: []testfixtures.ProviderStep{
				textStep("Committing progress."),
			},
			Fault:           testfixtures.FaultCoordinatorBeforeCommit,
			ExpectedOutcome: testfixtures.OutcomeResumable,
			EventKinds:      []string{"task-created", "plan-created"},
		},
		{
			Name:            ScenarioConcurrentEdit,
			Summary:         "a user edit landing during agent work is detected rather than silently overwritten",
			RepositoryState: testfixtures.StateDirty,
			ProviderScript: []testfixtures.ProviderStep{
				// The agent reads before it writes, which is what makes the
				// concurrent edit detectable: the content it edits against is
				// the content it saw, and the user changed it in between.
				toolStep("read-file", `{"path":"internal/server/server.go"}`),
				toolStep("apply-edit", `{"path":"internal/server/server.go"}`),
			},
			ExpectedOutcome: testfixtures.OutcomeRequiresReconciliation,
			EventKinds:      []string{"task-created", "plan-created", "tool-started"},
		},
		{
			Name:            ScenarioRecovery,
			Summary:         "an ambiguous external effect forces reconciliation before anything else proceeds",
			RepositoryState: testfixtures.StateClean,
			ProviderScript: []testfixtures.ProviderStep{
				toolStep("run-command", `{"argv":["./publish.sh"]}`),
			},
			Fault:           testfixtures.FaultWorkerDuringCommand,
			ExpectedOutcome: testfixtures.OutcomeRequiresReconciliation,
			EventKinds:      []string{"task-created", "plan-created", "tool-started"},
		},
	}
}

// ScenarioByName returns one declared scenario.
func ScenarioByName(name ScenarioName) (Scenario, error) {
	for _, scenario := range NamedScenarios() {
		if scenario.Name == name {
			return scenario, nil
		}
	}
	return Scenario{}, fmt.Errorf("no scenario named %q", name)
}

// ValidateNamedScenarios checks the declared set is complete and well formed.
func ValidateNamedScenarios() error {
	scenarios := NamedScenarios()
	seen := map[ScenarioName]bool{}
	for _, scenario := range scenarios {
		if err := scenario.Validate(); err != nil {
			return err
		}
		if seen[scenario.Name] {
			return fmt.Errorf("scenario %q is declared twice", scenario.Name)
		}
		seen[scenario.Name] = true
	}
	for _, name := range AllScenarioNames() {
		if !seen[name] {
			return fmt.Errorf("scenario %q is named but not declared", name)
		}
	}
	if len(scenarios) != len(AllScenarioNames()) {
		return errors.New("the declared scenario set does not match the named set")
	}
	return nil
}

// HarnessOptionsFor turns a scenario into the coordinator options that run it.
func HarnessOptionsFor(scenario Scenario, root string) (CoordinatorOptions, error) {
	if err := scenario.Validate(); err != nil {
		return CoordinatorOptions{}, err
	}
	return CoordinatorOptions{
		Root:            root,
		RepositoryState: scenario.RepositoryState,
		ProviderScript:  scenario.ProviderScript,
		IDPrefix:        "scn",
	}, nil
}
