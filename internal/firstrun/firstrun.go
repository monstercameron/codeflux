// Package firstrun models the first-run journey: what a new user is told,
// what they must decide, and in what order (M23-015..025).
//
// It is a pure model with no I/O so the CLI, the browser shell, and the tests
// all describe the same journey. A journey that existed only inside a
// renderer could not be timed, tested, or kept in step with the CLI.
package firstrun

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// StepID names one step of the first-run journey.
type StepID string

const (
	StepLocalArchitecture StepID = "local-architecture"
	StepGitVersusSQLite   StepID = "git-versus-sqlite"
	StepConfigureProvider StepID = "configure-provider"
	StepTestProvider      StepID = "test-provider"
	StepSelectRepository  StepID = "select-repository"
	StepReviewPermissions StepID = "review-permissions"
	StepExplainWorktree   StepID = "explain-worktree"
	StepFirstTask         StepID = "first-task"
	StepDataAndBackups    StepID = "data-and-backups"
)

// Order is the journey in the order it is presented (M23-016..024).
//
// The order is load-bearing rather than cosmetic. A user is told where their
// data lives BEFORE being asked for a provider credential, because deciding
// whether to hand over a key requires knowing where it will be kept. The
// provider is tested BEFORE a repository is chosen, because a broken provider
// makes everything after it fail for a reason that looks unrelated.
func Order() []StepID {
	return []StepID{
		StepLocalArchitecture,
		StepGitVersusSQLite,
		StepConfigureProvider,
		StepTestProvider,
		StepSelectRepository,
		StepReviewPermissions,
		StepExplainWorktree,
		StepFirstTask,
		StepDataAndBackups,
	}
}

// StepKind separates what the journey tells a user from what it asks them.
type StepKind string

const (
	// KindExplain presents information and needs no decision.
	KindExplain StepKind = "explain"
	// KindDecide needs an answer before the journey can continue.
	KindDecide StepKind = "decide"
	// KindVerify checks something and reports a real result.
	KindVerify StepKind = "verify"
)

// Step is one fully described first-run step.
type Step struct {
	ID    StepID
	Kind  StepKind
	Todo  string
	Title string
	// Body is what the user reads. It is stored here rather than in a
	// renderer so the CLI and the browser say the same thing.
	Body string
	// Facts are the specific claims this step makes. Each is separately
	// checkable, which is what stops the body drifting into reassurance.
	Facts []string
	// Blocking records whether the journey may proceed without this step.
	// Only the two decisions and the verification block.
	Blocking bool
}

// Steps returns every declared step (M23-016..024).
func Steps() []Step {
	return []Step{
		{
			ID: StepLocalArchitecture, Kind: KindExplain, Todo: "M23-016",
			Title: "Everything runs on this machine",
			Body: "CodeFlux runs entirely on your computer. The coordinator, your task history, " +
				"and every piece of evidence it produces stay here. The only thing that leaves " +
				"this machine is the request you send to the model provider you configure next.",
			Facts: []string{
				"the coordinator listens on loopback only and is not reachable from your network",
				"your database is a single SQLite file on this machine",
				"the only outbound traffic is to the model provider you configure",
				"no telemetry is sent anywhere",
			},
		},
		{
			ID: StepGitVersusSQLite, Kind: KindExplain, Todo: "M23-017",
			Title: "What lives in Git, and what lives in the database",
			Body: "Your code and its history stay in Git, exactly as they are now — CodeFlux " +
				"does not take ownership of them. What CodeFlux keeps in its own database is " +
				"everything about the work: tasks, plans, approvals, costs, and evidence. " +
				"Delete the database and you lose that record; your repository is untouched.",
			Facts: []string{
				"source code and commits remain in Git and are never copied into the database",
				"tasks, plans, approvals, costs, and evidence live in the CodeFlux database",
				"deleting the CodeFlux database does not change your repository",
				"CodeFlux writes to your repository only through a task worktree",
			},
		},
		{
			ID: StepConfigureProvider, Kind: KindDecide, Todo: "M23-018", Blocking: true,
			Title: "Choose a model provider",
			Body: "CodeFlux needs one model provider to do any work. Your credential goes into " +
				"this machine's credential store, not into the database and not into a file " +
				"in your repository. You can change or remove it at any time.",
			Facts: []string{
				"exactly one provider is required to continue",
				"the credential is stored in the operating system credential store",
				"the credential is never written to the database or to your repository",
				"the credential is never printed, logged, or included in a diagnostic export",
			},
		},
		{
			ID: StepTestProvider, Kind: KindVerify, Todo: "M23-019", Blocking: true,
			Title: "Check the provider before continuing",
			Body: "Before anything else, CodeFlux checks that the credential you just gave it " +
				"actually works. Finding out now is much better than finding out halfway " +
				"through your first task, when the failure will look like something else.",
			Facts: []string{
				"the check is performed before a repository is selected",
				"a failed check stops the journey here rather than later",
				"the check reports what failed: the credential, the endpoint, or the network",
			},
		},
		{
			ID: StepSelectRepository, Kind: KindDecide, Todo: "M23-020", Blocking: true,
			Title: "Choose a repository",
			Body: "Pick the repository CodeFlux may read and change. It must be a Git " +
				"repository with a clean working tree: uncommitted changes would make it " +
				"impossible to tell your edits from the agent's.",
			Facts: []string{
				"the repository must be a Git repository",
				"the working tree must be clean before work begins",
				"CodeFlux operates on exactly the repository you choose and no other",
			},
		},
		{
			ID: StepReviewPermissions, Kind: KindExplain, Todo: "M23-021",
			Title: "What CodeFlux may do here, and what it must ask about",
			Body: "CodeFlux read your repository and can now tell you exactly what it will do " +
				"without asking, and what it will always stop and ask about. Nothing in the " +
				"second list happens without you saying yes, each time.",
			Facts: []string{
				"reading files and searching the repository happens without asking",
				"writing inside the task worktree happens without asking",
				"running any command, installing any dependency, and any network access is asked about",
				"anything touching files outside the task worktree is asked about",
				"a denied action is not retried through a different tool",
			},
		},
		{
			ID: StepExplainWorktree, Kind: KindExplain, Todo: "M23-022",
			Title: "Each task gets its own worktree",
			Body: "CodeFlux never edits your checkout directly. Each task gets a separate Git " +
				"worktree, so you can keep working while a task runs, and so nothing the agent " +
				"does can surprise you in the files you have open.",
			Facts: []string{
				"your working checkout is never edited by a task",
				"each task gets its own Git worktree",
				"you can keep working in your checkout while a task runs",
				"changes reach your branch only when you accept them",
			},
		},
		{
			ID: StepFirstTask, Kind: KindExplain, Todo: "M23-023",
			Title: "Start with something small, or start empty",
			Body: "You can begin with a small prepared task that exercises the whole flow " +
				"end to end, or open an empty thread and describe your own. The prepared task " +
				"changes nothing until you accept its result, like any other task.",
			Facts: []string{
				"a prepared sample task is offered, and is optional",
				"the sample task changes nothing until you accept it",
				"an empty thread is always available instead",
			},
		},
		{
			ID: StepDataAndBackups, Kind: KindExplain, Todo: "M23-024",
			Title: "Where your data is, and how to remove it",
			Body: "Everything CodeFlux keeps is in one directory. Back it up by copying one " +
				"file; remove CodeFlux completely by deleting that directory. Your repository " +
				"is not part of it and is not affected either way.",
			Facts: []string{
				"the database is a single file you can copy",
				"`codeflux backup` writes a consistent snapshot while CodeFlux is running",
				"deleting the data directory removes everything CodeFlux stored",
				"deleting CodeFlux data never changes your repository",
			},
		},
	}
}

// Validate rejects a step that could not be presented or checked.
func (step Step) Validate() error {
	switch {
	case !slices.Contains(Order(), step.ID):
		return fmt.Errorf("step %q is not in the declared order", step.ID)
	case step.Kind != KindExplain && step.Kind != KindDecide && step.Kind != KindVerify:
		return fmt.Errorf("step %q has unknown kind %q", step.ID, step.Kind)
	case !strings.HasPrefix(step.Todo, "M23-"):
		return fmt.Errorf("step %q cites %q, want an M23 TODO", step.ID, step.Todo)
	case strings.TrimSpace(step.Title) == "":
		return fmt.Errorf("step %q has no title", step.ID)
	case strings.TrimSpace(step.Body) == "":
		return fmt.Errorf("step %q has no body", step.ID)
	case len(step.Facts) == 0:
		return fmt.Errorf("step %q states no checkable fact", step.ID)
	}
	for index, fact := range step.Facts {
		if strings.TrimSpace(fact) == "" {
			return fmt.Errorf("step %q fact %d is empty", step.ID, index)
		}
	}
	// Only a decision or a verification may block; an explanation that blocked
	// would be a wall of text between a user and the thing they came to do.
	if step.Blocking && step.Kind == KindExplain {
		return fmt.Errorf("step %q explains but blocks", step.ID)
	}
	if !step.Blocking && step.Kind != KindExplain {
		return fmt.Errorf("step %q asks or verifies but does not block", step.ID)
	}
	return nil
}

// ValidateJourney checks the declared journey is complete and well ordered.
func ValidateJourney() error {
	steps := Steps()
	if len(steps) != len(Order()) {
		return fmt.Errorf("the journey declares %d steps for %d ordered ids",
			len(steps), len(Order()))
	}
	seen := map[StepID]bool{}
	todos := map[string]StepID{}
	for index, step := range steps {
		if err := step.Validate(); err != nil {
			return err
		}
		if seen[step.ID] {
			return fmt.Errorf("step %q is declared twice", step.ID)
		}
		seen[step.ID] = true
		if other, clash := todos[step.Todo]; clash {
			return fmt.Errorf("steps %q and %q both claim %s", other, step.ID, step.Todo)
		}
		todos[step.Todo] = step.ID
		if step.ID != Order()[index] {
			return fmt.Errorf("step %d is %q, but the order declares %q",
				index, step.ID, Order()[index])
		}
	}
	// The provider must be tested before a repository is chosen, or a broken
	// credential surfaces as a repository problem.
	if indexOf(StepTestProvider) > indexOf(StepSelectRepository) {
		return errors.New("the provider is tested after the repository is chosen")
	}
	// Data location must be explained before a credential is requested.
	if indexOf(StepLocalArchitecture) > indexOf(StepConfigureProvider) {
		return errors.New("a credential is requested before the user is told where data lives")
	}
	return nil
}

func indexOf(id StepID) int {
	return slices.Index(Order(), id)
}

// StepFor returns one declared step.
func StepFor(id StepID) (Step, bool) {
	for _, step := range Steps() {
		if step.ID == id {
			return step, true
		}
	}
	return Step{}, false
}

// BlockingSteps returns the steps that must be completed to finish first run.
func BlockingSteps() []StepID {
	var blocking []StepID
	for _, step := range Steps() {
		if step.Blocking {
			blocking = append(blocking, step.ID)
		}
	}
	return blocking
}

// State tracks progress through the journey (M23-015).
type State struct {
	// Completed is the set of finished steps.
	Completed map[StepID]bool
	// StartedAt and CompletedAt bound the journey for timing (M23-025).
	StartedAt   time.Time
	CompletedAt time.Time
}

// NewState begins a journey at the supplied time.
func NewState(startedAt time.Time) State {
	return State{Completed: map[StepID]bool{}, StartedAt: startedAt}
}

// Complete marks one step done.
func (state *State) Complete(id StepID, at time.Time) error {
	if _, ok := StepFor(id); !ok {
		return fmt.Errorf("unknown first-run step %q", id)
	}
	if state.Completed == nil {
		state.Completed = map[StepID]bool{}
	}
	// A blocking step cannot be completed before the blocking steps ahead of
	// it, or a user could reach a repository choice with no working provider.
	for _, earlier := range Order() {
		if earlier == id {
			break
		}
		earlierStep, _ := StepFor(earlier)
		if earlierStep.Blocking && !state.Completed[earlier] {
			return fmt.Errorf("step %q cannot be completed before %q", id, earlier)
		}
	}
	state.Completed[id] = true
	if state.Finished() {
		// The stamp advances with every completed step, not only the one that
		// made the system usable. Freezing it at the last blocking step would
		// erase the time a user spent on the closing screens from the measured
		// journey, which is exactly the time a too-wordy ending would consume.
		state.CompletedAt = at
	}
	return nil
}

// Finished reports whether every blocking step is done.
//
// Explanatory steps deliberately do not gate completion: a user who read one
// screen and skipped ahead has still configured a working system, and refusing
// to let them start would be paternalism rather than safety.
func (state State) Finished() bool {
	for _, id := range BlockingSteps() {
		if !state.Completed[id] {
			return false
		}
	}
	return true
}

// NextStep returns the next step to present, and false when the journey is
// complete.
func (state State) NextStep() (Step, bool) {
	for _, id := range Order() {
		if !state.Completed[id] {
			step, _ := StepFor(id)
			return step, true
		}
	}
	return Step{}, false
}

// Elapsed reports how long the journey took, and whether it is known.
func (state State) Elapsed() (time.Duration, bool) {
	if state.StartedAt.IsZero() || state.CompletedAt.IsZero() {
		return 0, false
	}
	return state.CompletedAt.Sub(state.StartedAt), true
}

// TargetDuration is the M23-025 budget for a fresh user's first run.
//
// docs/plan.md's promise is that a person can go from nothing to a running
// first task without reading documentation. Ten minutes is the point past
// which that stops being true: someone who has spent longer has already gone
// looking for help.
const TargetDuration = 10 * time.Minute

// WithinTarget reports whether a completed journey met the budget.
func (state State) WithinTarget() (bool, error) {
	elapsed, known := state.Elapsed()
	if !known {
		return false, errors.New("the journey has no measured duration")
	}
	return elapsed <= TargetDuration, nil
}
