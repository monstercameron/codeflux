package firstrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestM23_016_024_JourneyIsCompleteAndWellOrdered covers M23-016..024.
func TestM23_016_024_JourneyIsCompleteAndWellOrdered(t *testing.T) {
	if err := ValidateJourney(); err != nil {
		t.Fatalf("the first-run journey is invalid: %v", err)
	}

	steps := Steps()
	if len(steps) != 9 {
		t.Fatalf("the journey has %d steps, want 9", len(steps))
	}

	// Every M23-016..024 TODO must be claimed exactly once.
	claimed := map[string]bool{}
	for _, step := range steps {
		claimed[step.Todo] = true
	}
	for number := 16; number <= 24; number++ {
		todo := "M23-0" + itoa(number)
		if !claimed[todo] {
			t.Fatalf("no first-run step claims %s", todo)
		}
	}

	// The ordering constraints are the ones that change what a user
	// experiences, so they are asserted directly rather than left implicit.
	if indexOf(StepLocalArchitecture) >= indexOf(StepConfigureProvider) {
		t.Fatal("a credential is requested before the user is told where data lives")
	}
	if indexOf(StepTestProvider) >= indexOf(StepSelectRepository) {
		t.Fatal("the provider is tested after a repository is chosen")
	}
	if indexOf(StepReviewPermissions) <= indexOf(StepSelectRepository) {
		t.Fatal("permissions are shown before a repository is chosen, so they describe nothing")
	}
	if indexOf(StepExplainWorktree) >= indexOf(StepFirstTask) {
		t.Fatal("a first task is offered before the worktree model is explained")
	}
}

// TestM23_016_017_ArchitectureAndStorageClaimsAreSpecific covers M23-016 and
// M23-017, whose whole value is that they say something checkable rather than
// something reassuring.
func TestM23_016_017_ArchitectureAndStorageClaimsAreSpecific(t *testing.T) {
	architecture, ok := StepFor(StepLocalArchitecture)
	if !ok {
		t.Fatal("no local-architecture step")
	}
	for _, claim := range []string{"loopback", "SQLite", "outbound", "telemetry"} {
		if !containsFact(architecture, claim) {
			t.Fatalf("the architecture step does not address %q: %v", claim, architecture.Facts)
		}
	}

	storage, ok := StepFor(StepGitVersusSQLite)
	if !ok {
		t.Fatal("no git-versus-sqlite step")
	}
	// The distinction the step exists to draw must be stated in both
	// directions, or a reader learns only half of it.
	if !containsFact(storage, "Git") {
		t.Fatalf("the storage step does not say what stays in Git: %v", storage.Facts)
	}
	if !containsFact(storage, "database") {
		t.Fatalf("the storage step does not say what lives in the database: %v", storage.Facts)
	}
	if !containsFact(storage, "does not change your repository") {
		t.Fatalf("the storage step does not say deletion is safe for the repository: %v",
			storage.Facts)
	}
}

// TestM23_018_019_ProviderStepsProtectTheCredential covers M23-018 and M23-019.
func TestM23_018_019_ProviderStepsProtectTheCredential(t *testing.T) {
	configure, _ := StepFor(StepConfigureProvider)
	if configure.Kind != KindDecide || !configure.Blocking {
		t.Fatalf("configuring a provider is not a blocking decision: %+v", configure)
	}
	for _, claim := range []string{
		"credential store", "never written to the database", "never printed",
	} {
		if !containsFact(configure, claim) {
			t.Fatalf("the provider step does not promise %q: %v", claim, configure.Facts)
		}
	}

	test, _ := StepFor(StepTestProvider)
	if test.Kind != KindVerify || !test.Blocking {
		t.Fatalf("testing a provider is not a blocking verification: %+v", test)
	}
	if !containsFact(test, "before a repository is selected") {
		t.Fatalf("the test step does not state its ordering guarantee: %v", test.Facts)
	}
	if !containsFact(test, "stops the journey here") {
		t.Fatalf("the test step does not say a failure stops here: %v", test.Facts)
	}
}

// TestM23_020_022_RepositoryStepsStateTheRealBoundaries covers M23-020..022.
func TestM23_020_022_RepositoryStepsStateTheRealBoundaries(t *testing.T) {
	repository, _ := StepFor(StepSelectRepository)
	if !containsFact(repository, "clean") {
		t.Fatalf("the repository step does not require a clean tree: %v", repository.Facts)
	}

	permissions, _ := StepFor(StepReviewPermissions)
	// Both halves must be present: what happens silently and what always asks.
	if !containsFact(permissions, "without asking") {
		t.Fatalf("the permissions step does not say what happens silently: %v", permissions.Facts)
	}
	if !containsFact(permissions, "asked about") {
		t.Fatalf("the permissions step does not say what is asked about: %v", permissions.Facts)
	}
	// The substitution rule matters most and is easiest to forget.
	if !containsFact(permissions, "not retried through a different tool") {
		t.Fatalf("the permissions step does not state the denial rule: %v", permissions.Facts)
	}

	worktree, _ := StepFor(StepExplainWorktree)
	if !containsFact(worktree, "never edited") {
		t.Fatalf("the worktree step does not promise the checkout is untouched: %v",
			worktree.Facts)
	}
	if !containsFact(worktree, "only when you accept") {
		t.Fatalf("the worktree step does not say acceptance gates the change: %v", worktree.Facts)
	}
}

// TestM23_023_024_ClosingStepsOfferAnExitAndAnUndo covers M23-023 and M23-024.
func TestM23_023_024_ClosingStepsOfferAnExitAndAnUndo(t *testing.T) {
	first, _ := StepFor(StepFirstTask)
	if first.Blocking {
		t.Fatal("the sample task blocks completion; a user must be able to start empty")
	}
	if !containsFact(first, "optional") {
		t.Fatalf("the sample task is not stated to be optional: %v", first.Facts)
	}
	if !containsFact(first, "changes nothing until you accept") {
		t.Fatalf("the sample task does not state its safety property: %v", first.Facts)
	}

	data, _ := StepFor(StepDataAndBackups)
	for _, claim := range []string{"copy", "backup", "deleting", "never changes your repository"} {
		if !containsFact(data, claim) {
			t.Fatalf("the data step does not address %q: %v", claim, data.Facts)
		}
	}
}

// TestM23_015_DetectionDistinguishesEveryCondition covers M23-015.
func TestM23_015_DetectionDistinguishesEveryCondition(t *testing.T) {
	cases := []struct {
		name        string
		environment stubEnvironment
		condition   Condition
		firstRun    bool
		resumeAt    StepID
	}{
		{
			"no database", stubEnvironment{},
			ConditionNoDatabase, true, Order()[0],
		},
		{
			"no provider",
			stubEnvironment{databaseExists: true},
			ConditionNoProvider, true, StepConfigureProvider,
		},
		{
			"no repository",
			stubEnvironment{databaseExists: true, providers: []string{"anthropic"}},
			ConditionNoRepository, true, StepSelectRepository,
		},
		{
			"ready",
			stubEnvironment{
				databaseExists: true,
				providers:      []string{"anthropic"},
				repositories:   []string{"repo_1"},
			},
			ConditionReady, false, "",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			detection, err := Detect(t.Context(), testCase.environment)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if detection.Condition != testCase.condition {
				t.Fatalf("condition = %q, want %q", detection.Condition, testCase.condition)
			}
			if detection.IsFirstRun != testCase.firstRun {
				t.Fatalf("first run = %v, want %v", detection.IsFirstRun, testCase.firstRun)
			}
			if detection.ResumeAt != testCase.resumeAt {
				t.Fatalf("resume at %q, want %q", detection.ResumeAt, testCase.resumeAt)
			}
			if strings.TrimSpace(detection.Reason) == "" {
				t.Fatal("detection gives no reason")
			}
		})
	}
}

// TestM23_015_UnreadableStateIsNeverTreatedAsAFirstRun is the safety half of
// M23-015: starting the journey on top of an existing database would risk
// overwriting somebody's work.
func TestM23_015_UnreadableStateIsNeverTreatedAsAFirstRun(t *testing.T) {
	for name, environment := range map[string]stubEnvironment{
		"database unreadable": {databaseErr: errors.New("permission denied")},
		"providers unreadable": {
			databaseExists: true, providerErr: errors.New("keychain locked"),
		},
		"repositories unreadable": {
			databaseExists: true, providers: []string{"anthropic"},
			repositoryErr: errors.New("query failed"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			detection, err := Detect(t.Context(), environment)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if detection.IsFirstRun {
				t.Fatal("an unreadable state was treated as a first run")
			}
			if detection.Condition != ConditionUnreadable {
				t.Fatalf("condition = %q, want %q", detection.Condition, ConditionUnreadable)
			}
			if strings.TrimSpace(detection.Reason) == "" {
				t.Fatal("an unreadable state gives no reason")
			}
		})
	}

	if _, err := Detect(context.Background(), nil); err == nil {
		t.Fatal("detection with no environment succeeded")
	}
}

// TestM23_015_FileEnvironmentReadsRealState exercises the production
// Environment against the filesystem.
func TestM23_015_FileEnvironmentReadsRealState(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "codeflux.sqlite3")
	environment := FileEnvironment{
		DatabasePath: path,
		Providers:    func(context.Context) ([]string, error) { return nil, nil },
		Repositories: func(context.Context) ([]string, error) { return nil, nil },
	}

	exists, err := environment.DatabaseExists(t.Context())
	if err != nil || exists {
		t.Fatalf("an absent database reported exists=%v err=%v", exists, err)
	}

	// A zero-length file is not a database: reporting it as one would put a
	// brand-new user into the unreadable state on their very first run.
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	exists, err = environment.DatabaseExists(t.Context())
	if err != nil || exists {
		t.Fatalf("an empty file reported exists=%v err=%v", exists, err)
	}

	if err := os.WriteFile(path, []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatalf("write database: %v", err)
	}
	exists, err = environment.DatabaseExists(t.Context())
	if err != nil || !exists {
		t.Fatalf("a real database reported exists=%v err=%v", exists, err)
	}

	// A directory in the database's place is an error, not an absence.
	directory := FileEnvironment{DatabasePath: root}
	if _, err := directory.DatabaseExists(t.Context()); err == nil {
		t.Fatal("a directory was accepted as a database")
	}
	if _, err := (FileEnvironment{}).DatabaseExists(t.Context()); err == nil {
		t.Fatal("an empty database path was accepted")
	}
	if _, err := (FileEnvironment{DatabasePath: path}).ConfiguredProviders(t.Context()); err == nil {
		t.Fatal("a missing provider lookup was accepted")
	}
	if _, err := (FileEnvironment{DatabasePath: path}).SelectedRepositories(t.Context()); err == nil {
		t.Fatal("a missing repository lookup was accepted")
	}
}

// TestM23_015_StateEnforcesTheBlockingOrder proves a user cannot reach a
// repository choice without a working provider.
func TestM23_015_StateEnforcesTheBlockingOrder(t *testing.T) {
	state := NewState(time.Unix(0, 0).UTC())
	if state.Finished() {
		t.Fatal("a fresh journey reports itself finished")
	}
	next, ok := state.NextStep()
	if !ok || next.ID != Order()[0] {
		t.Fatalf("the first step is %q", next.ID)
	}

	// Skipping ahead past a blocking step is refused.
	if err := state.Complete(StepSelectRepository, time.Unix(1, 0).UTC()); err == nil {
		t.Fatal("a repository was selected before a provider was configured")
	}
	if err := state.Complete(StepID("invented"), time.Unix(1, 0).UTC()); err == nil {
		t.Fatal("an unknown step was completed")
	}

	// Walking the order in sequence works, and finishing is decided by the
	// blocking steps alone.
	at := time.Unix(0, 0).UTC()
	for _, id := range Order() {
		at = at.Add(time.Second)
		if err := state.Complete(id, at); err != nil {
			t.Fatalf("complete %q: %v", id, err)
		}
	}
	if !state.Finished() {
		t.Fatal("a fully walked journey is not finished")
	}
	if _, ok := state.NextStep(); ok {
		t.Fatal("a finished journey still has a next step")
	}
	elapsed, known := state.Elapsed()
	if !known || elapsed <= 0 {
		t.Fatalf("elapsed = %v known=%v", elapsed, known)
	}

	// An explanation must not gate completion: a user who skipped the reading
	// has still configured a working system.
	skipped := NewState(time.Unix(0, 0).UTC())
	for _, id := range BlockingSteps() {
		if err := skipped.Complete(id, time.Unix(1, 0).UTC()); err != nil {
			t.Fatalf("complete blocking step %q: %v", id, err)
		}
	}
	if !skipped.Finished() {
		t.Fatal("completing every blocking step did not finish the journey")
	}
}

// TestM23_025_FreshUserJourneyFitsTheBudget covers M23-025.
func TestM23_025_FreshUserJourneyFitsTheBudget(t *testing.T) {
	timing, err := MeasureJourney(EstimatedReadingTime)
	if err != nil {
		t.Fatalf("measure journey: %v", err)
	}
	if timing.Total <= 0 {
		t.Fatal("the journey measured no time")
	}
	if len(timing.StepDurations) != len(Order()) {
		t.Fatalf("timed %d steps of %d", len(timing.StepDurations), len(Order()))
	}

	// Reading the whole journey must be a small fraction of the budget. The
	// rest is for the two decisions and the check, which is where a fresh
	// user's time actually goes.
	if timing.ReadingBudget > TargetDuration/2 {
		t.Fatalf("reading alone takes %v of a %v budget; the journey is too wordy",
			timing.ReadingBudget, TargetDuration)
	}
	if timing.Total > TargetDuration {
		t.Fatalf("the journey takes %v, over the %v budget", timing.Total, TargetDuration)
	}

	// With realistic decision time added, the journey must still fit. These
	// are the costs a fresh user genuinely pays: finding and pasting a key,
	// waiting for the check, and picking a repository.
	realistic, err := MeasureJourney(func(step Step) time.Duration {
		cost := EstimatedReadingTime(step)
		switch step.ID {
		case StepConfigureProvider:
			cost += 90 * time.Second
		case StepTestProvider:
			cost += 10 * time.Second
		case StepSelectRepository:
			cost += 45 * time.Second
		}
		return cost
	})
	if err != nil {
		t.Fatalf("measure realistic journey: %v", err)
	}
	if realistic.Total > TargetDuration {
		t.Fatalf("a realistic first run takes %v, over the %v budget",
			realistic.Total, TargetDuration)
	}
	if realistic.DecisionBudget <= realistic.ReadingBudget {
		t.Fatalf("reading (%v) costs more than deciding (%v); the journey is explaining too much",
			realistic.ReadingBudget, realistic.DecisionBudget)
	}

	// A completed state must be able to report whether it met the budget.
	// Two minutes per step is eighteen minutes across nine steps, comfortably
	// past the budget, so the verdict must be negative.
	slow := NewState(time.Unix(0, 0).UTC())
	at := time.Unix(0, 0).UTC()
	for _, id := range Order() {
		at = at.Add(2 * time.Minute)
		if err := slow.Complete(id, at); err != nil {
			t.Fatalf("complete %q: %v", id, err)
		}
	}
	within, err := slow.WithinTarget()
	if err != nil {
		t.Fatalf("within target: %v", err)
	}
	if within {
		t.Fatal("an eighteen-minute journey was reported within a ten-minute budget")
	}

	// And a brisk journey must be reported as within it, or the verdict is a
	// constant rather than a measurement.
	quick := NewState(time.Unix(0, 0).UTC())
	at = time.Unix(0, 0).UTC()
	for _, id := range Order() {
		at = at.Add(20 * time.Second)
		if err := quick.Complete(id, at); err != nil {
			t.Fatalf("complete %q: %v", id, err)
		}
	}
	if ok, err := quick.WithinTarget(); err != nil || !ok {
		t.Fatalf("a three-minute journey was reported outside the budget (ok=%v err=%v)", ok, err)
	}
	if _, err := NewState(time.Unix(0, 0).UTC()).WithinTarget(); err == nil {
		t.Fatal("an unfinished journey reported a budget verdict")
	}
}

// TestM23_025_MeasurementRejectsUnusableCosts proves the harness is not
// silently accepting nonsense.
func TestM23_025_MeasurementRejectsUnusableCosts(t *testing.T) {
	if _, err := MeasureJourney(nil); err == nil {
		t.Fatal("measuring with no cost function succeeded")
	}
	if _, err := MeasureJourney(func(Step) time.Duration { return -time.Second }); err == nil {
		t.Fatal("a negative step cost was accepted")
	}
	// A zero-cost journey is legitimate and must still complete.
	timing, err := MeasureJourney(func(Step) time.Duration { return 0 })
	if err != nil {
		t.Fatalf("a zero-cost journey failed: %v", err)
	}
	if timing.Total != 0 {
		t.Fatalf("a zero-cost journey measured %v", timing.Total)
	}
}

// TestM23_016_024_JourneyValidationIsLoadBearing proves the validator rejects
// the ways a journey can become unusable.
func TestM23_016_024_JourneyValidationIsLoadBearing(t *testing.T) {
	valid, _ := StepFor(StepLocalArchitecture)
	corruptions := map[string]func(Step) Step{
		"unknown id": func(step Step) Step {
			step.ID = StepID("invented")
			return step
		},
		"unknown kind": func(step Step) Step {
			step.Kind = StepKind("invented")
			return step
		},
		"foreign todo": func(step Step) Step {
			step.Todo = "M22-001"
			return step
		},
		"no title": func(step Step) Step {
			step.Title = ""
			return step
		},
		"no body": func(step Step) Step {
			step.Body = ""
			return step
		},
		"no facts": func(step Step) Step {
			step.Facts = nil
			return step
		},
		"empty fact": func(step Step) Step {
			step.Facts = []string{""}
			return step
		},
		"explanation that blocks": func(step Step) Step {
			step.Blocking = true
			return step
		},
	}
	for name, corrupt := range corruptions {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(valid).Validate(); err == nil {
				t.Fatalf("an unusable step validated: %s", name)
			}
		})
	}

	decision, _ := StepFor(StepConfigureProvider)
	decision.Blocking = false
	if err := decision.Validate(); err == nil {
		t.Fatal("a non-blocking decision validated")
	}
	if _, ok := StepFor(StepID("invented")); ok {
		t.Fatal("an unknown step resolved")
	}
}

// stubEnvironment answers detection from fixed values.
type stubEnvironment struct {
	databaseExists bool
	databaseErr    error
	providers      []string
	providerErr    error
	repositories   []string
	repositoryErr  error
}

func (environment stubEnvironment) DatabaseExists(context.Context) (bool, error) {
	return environment.databaseExists, environment.databaseErr
}

func (environment stubEnvironment) ConfiguredProviders(context.Context) ([]string, error) {
	return environment.providers, environment.providerErr
}

func (environment stubEnvironment) SelectedRepositories(context.Context) ([]string, error) {
	return environment.repositories, environment.repositoryErr
}

func containsFact(step Step, needle string) bool {
	for _, fact := range step.Facts {
		if strings.Contains(fact, needle) {
			return true
		}
	}
	return strings.Contains(step.Body, needle)
}

func itoa(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}
