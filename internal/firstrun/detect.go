package firstrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// Condition is why a run is or is not a first run (M23-015).
//
// The distinction matters because the responses differ: a missing database
// means show the journey, while an unreadable one means say so rather than
// silently starting over on top of the user's existing work.
type Condition string

const (
	// ConditionNoDatabase means nothing has been created yet.
	ConditionNoDatabase Condition = "no-database"
	// ConditionNoProvider means the database exists but no provider is
	// configured, so no work is possible yet.
	ConditionNoProvider Condition = "no-provider"
	// ConditionNoRepository means a provider exists but no repository has been
	// selected.
	ConditionNoRepository Condition = "no-repository"
	// ConditionReady means the system is configured and first run is over.
	ConditionReady Condition = "ready"
	// ConditionUnreadable means the state cannot be determined. It is NOT
	// treated as a first run: starting the journey on top of a database that
	// exists but cannot be read risks overwriting work.
	ConditionUnreadable Condition = "unreadable"
)

// Detection is the result of asking whether this is a first run.
type Detection struct {
	Condition Condition
	// IsFirstRun is true only when the journey should be shown.
	IsFirstRun bool
	// Reason is a short, user-readable explanation. It never contains a path,
	// because a path routinely carries the user's name.
	Reason string
	// ResumeAt names where an interrupted journey should continue.
	ResumeAt StepID
}

// Environment is what detection inspects. It is an interface so detection can
// be tested without a database, a credential store, or a repository.
type Environment interface {
	// DatabaseExists reports whether the durable store is present. The error
	// is for "cannot tell", which is different from "not there".
	DatabaseExists(context.Context) (bool, error)
	// ConfiguredProviders returns the provider names with usable credentials.
	ConfiguredProviders(context.Context) ([]string, error)
	// SelectedRepositories returns the repositories already registered.
	SelectedRepositories(context.Context) ([]string, error)
}

// Detect decides whether to show the first-run journey (M23-015).
func Detect(ctx context.Context, environment Environment) (Detection, error) {
	if environment == nil {
		return Detection{}, errors.New("first-run detection requires an environment")
	}

	exists, err := environment.DatabaseExists(ctx)
	if err != nil {
		// Unreadable is reported, never guessed past. Treating it as a first
		// run would start the journey on top of data that may be intact.
		return Detection{
			Condition: ConditionUnreadable, IsFirstRun: false,
			Reason: "the CodeFlux database exists but cannot be read; " +
				"resolve that before continuing rather than starting over",
		}, nil
	}
	if !exists {
		return Detection{
			Condition: ConditionNoDatabase, IsFirstRun: true,
			Reason:   "no CodeFlux database exists yet",
			ResumeAt: Order()[0],
		}, nil
	}

	providers, err := environment.ConfiguredProviders(ctx)
	if err != nil {
		return Detection{
			Condition: ConditionUnreadable, IsFirstRun: false,
			Reason: "the configured providers cannot be read",
		}, nil
	}
	if len(providers) == 0 {
		return Detection{
			Condition: ConditionNoProvider, IsFirstRun: true,
			Reason:   "no model provider is configured, so no work is possible yet",
			ResumeAt: StepConfigureProvider,
		}, nil
	}

	repositories, err := environment.SelectedRepositories(ctx)
	if err != nil {
		return Detection{
			Condition: ConditionUnreadable, IsFirstRun: false,
			Reason: "the selected repositories cannot be read",
		}, nil
	}
	if len(repositories) == 0 {
		return Detection{
			Condition: ConditionNoRepository, IsFirstRun: true,
			Reason:   "no repository has been selected yet",
			ResumeAt: StepSelectRepository,
		}, nil
	}

	return Detection{
		Condition: ConditionReady, IsFirstRun: false,
		Reason: "CodeFlux is configured",
	}, nil
}

// FileEnvironment is the production Environment, backed by the filesystem and
// the supplied lookups.
type FileEnvironment struct {
	DatabasePath string
	Providers    func(context.Context) ([]string, error)
	Repositories func(context.Context) ([]string, error)
}

// DatabaseExists reports whether the database file is present.
func (environment FileEnvironment) DatabaseExists(context.Context) (bool, error) {
	if environment.DatabasePath == "" {
		return false, errors.New("no database path was supplied")
	}
	info, err := os.Stat(environment.DatabasePath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect database: %w", err)
	}
	if info.IsDir() {
		return false, errors.New("the database path is a directory")
	}
	// A zero-length file is not a database. Treating it as one would send a
	// user into an "unreadable" state on their very first run, when in fact
	// nothing has been created.
	if info.Size() == 0 {
		return false, nil
	}
	return true, nil
}

// ConfiguredProviders returns configured provider names.
func (environment FileEnvironment) ConfiguredProviders(ctx context.Context) ([]string, error) {
	if environment.Providers == nil {
		return nil, errors.New("no provider lookup was supplied")
	}
	return environment.Providers(ctx)
}

// SelectedRepositories returns registered repositories.
func (environment FileEnvironment) SelectedRepositories(ctx context.Context) ([]string, error) {
	if environment.Repositories == nil {
		return nil, errors.New("no repository lookup was supplied")
	}
	return environment.Repositories(ctx)
}

// JourneyTiming is one measured first-run walkthrough (M23-025).
type JourneyTiming struct {
	StepDurations map[StepID]time.Duration
	Total         time.Duration
	// ReadingBudget is the time attributed to reading rather than deciding.
	// It is tracked separately because a journey that is slow because someone
	// read carefully is fine, and one that is slow because a step is confusing
	// is not.
	ReadingBudget  time.Duration
	DecisionBudget time.Duration
}

// MeasureJourney walks the whole journey with a supplied per-step cost and
// reports where the time went (M23-025).
//
// The cost function is injected so the measurement is deterministic: timing a
// real person is not reproducible, and a first-run budget that changed between
// runs could never be enforced.
func MeasureJourney(cost func(Step) time.Duration) (JourneyTiming, error) {
	if cost == nil {
		return JourneyTiming{}, errors.New("measuring a journey requires a per-step cost")
	}
	timing := JourneyTiming{StepDurations: map[StepID]time.Duration{}}
	state := NewState(time.Unix(0, 0).UTC())
	elapsed := time.Duration(0)

	for _, id := range Order() {
		step, ok := StepFor(id)
		if !ok {
			return JourneyTiming{}, fmt.Errorf("step %q is ordered but not declared", id)
		}
		duration := cost(step)
		if duration < 0 {
			return JourneyTiming{}, fmt.Errorf("step %q has a negative cost", id)
		}
		timing.StepDurations[id] = duration
		elapsed += duration
		if step.Kind == KindExplain {
			timing.ReadingBudget += duration
		} else {
			timing.DecisionBudget += duration
		}
		if err := state.Complete(id, time.Unix(0, 0).UTC().Add(elapsed)); err != nil {
			return JourneyTiming{}, err
		}
	}
	if !state.Finished() {
		return JourneyTiming{}, errors.New("the journey did not reach a finished state")
	}
	timing.Total = elapsed
	return timing, nil
}

// EstimatedReadingTime is a deterministic reading cost for a step.
//
// It is derived from the text a step actually shows, at a deliberately slow
// 180 words per minute, so a step that grows into a wall of text makes the
// measured journey longer and trips the budget rather than passing silently.
func EstimatedReadingTime(step Step) time.Duration {
	words := len(splitWords(step.Title)) + len(splitWords(step.Body))
	for _, fact := range step.Facts {
		words += len(splitWords(fact))
	}
	const wordsPerMinute = 180
	seconds := float64(words) * 60 / wordsPerMinute
	return time.Duration(seconds * float64(time.Second))
}

func splitWords(text string) []string {
	var words []string
	current := ""
	for _, character := range text {
		if character == ' ' || character == '\n' || character == '\t' {
			if current != "" {
				words = append(words, current)
				current = ""
			}
			continue
		}
		current += string(character)
	}
	if current != "" {
		words = append(words, current)
	}
	return words
}
