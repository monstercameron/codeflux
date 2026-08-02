package pipeline

import (
	"fmt"
	"sort"
)

// Settings are every choice the flow leaves to the person running it.
//
// They were constants scattered through the code: how many attempts a run
// gets, what mutation score is enough, how long a suite may take, whether to
// cross-compile. Each was a reasonable default and none of them was visible,
// so the only way to learn a run's shape was to read the source and the only
// way to change it was to edit and rebuild.
//
// Gathering them here is what makes them a surface rather than a set of
// opinions: one type to pass around, one place to validate, and one
// description a settings page can render without knowing what any of them
// mean.
type Settings struct {
	// Ambiguity is what a run does with a request it can read two ways.
	Ambiguity string
	// Platforms is which systems a produced program must build for. Most work
	// happens on one machine, so the default is that machine.
	Platforms string
	// MaximumAttempts bounds how many times a run may rewrite its own work
	// before it stops and says so.
	MaximumAttempts int
	// MutationThresholdPercent is the share of deliberate defects the tests
	// must catch before they count as able to detect anything.
	MutationThresholdPercent int
	// RepetitionRuns is how many times the suite is repeated to tell a correct
	// program from an intermittently correct one.
	RepetitionRuns int
	// SuiteBudgetSeconds is how long the produced tests may take before that
	// is a different kind of problem.
	SuiteBudgetSeconds int
	// FuzzSeconds is how long each fuzz target runs. Zero disables fuzzing,
	// which is honest on a program with no parsing boundary.
	FuzzSeconds int
	// HostileTimeoutSeconds is how long a produced program may take on input
	// nobody intended before it counts as hung.
	HostileTimeoutSeconds int
	// TangleThreshold is the number of decision points at which a function is
	// asked to be split up.
	TangleThreshold int
	// RequireTests demands a direct test for every function a run writes.
	RequireTests bool
	// RequireDocComments demands a doc comment on every declaration.
	RequireDocComments bool
	// AdversarialReview runs the review that attacks work already passing.
	AdversarialReview bool
	// CodeStyle is the shape a project wants its code in. Neutral asks only
	// for things true of any working program; anything else is a preference,
	// and a preference the engine holds without being asked is a bias.
	CodeStyle string
	// ModelLadder is the rungs a run climbs when it stops making progress,
	// cheapest first, each written model:effort. The first is what every run
	// starts on.
	//
	// Effort is a rung of its own because it is the cheaper axis: raising it
	// bills more tokens at the current model's rate, while changing model
	// raises the rate on every token.
	ModelLadder []string
	// StallBeforeEscalation is how many attempts in a row may fail the same
	// gate with the same failure before the run moves up the ladder.
	//
	// The trigger is repetition, not the attempt count. A run on attempt eight
	// that has failed a different gate each time is converging and should be
	// left alone; a run that has failed the same gate three times with the same
	// message is not going to succeed on the twelfth, and the attempts between
	// are spent proving it.
	StallBeforeEscalation int
	// DecomposeWhenExhausted is what happens at the top of the ladder: the
	// request is broken into smaller units and the run starts again, on the
	// reading that a request the best model cannot satisfy in one piece is too
	// big rather than too hard.
	DecomposeWhenExhausted bool
	// StageModels overrides the rung for named stages, regardless of where the
	// ladder has got to. It is for the stage a project knows is harder than
	// the rest, so it does not have to fail its way up to it first.
	StageModels map[string]string
	// PlanningRung is what decides how the work is broken up, and it does not
	// climb: it is at the top from the first request.
	//
	// Planning is a handful of tokens that every later attempt is spent
	// against. A decomposition that misses a behaviour is paid for by every
	// rung of the ladder, on every attempt, and no amount of effort further
	// down recovers it. It is the one place where spending the most up front
	// is cheaper than spending the least.
	PlanningRung string
	// ApprovalRungs are the rungs a run may not reach on its own.
	//
	// A ladder that climbs without asking is right up to the point where the
	// next step is a decision about money rather than about capability. Naming
	// a rung here stops a run when it would otherwise escalate to it, and says
	// what it had tried and what the step would cost.
	ApprovalRungs []string
	// ForbiddenCapabilities are the things a produced program may not reach
	// for, named in plain words: "network", "processes", "filesystem". Empty
	// means no restriction, which is the honest default — a program asked to
	// serve HTTP is not defective for opening a socket.
	ForbiddenCapabilities []string
}

// Ambiguity postures.
const (
	AmbiguityAsk    = "ask"
	AmbiguityAssume = "assume"
)

// Platform postures.
const (
	PlatformsHost     = "host"
	PlatformsPortable = "portable"
)

// Capabilities a produced program can reach for.
//
// They are named here rather than in the check that detects them so a settings
// page can offer the same words the engine enforces. A page that invented its
// own names would offer restrictions nothing acts on.
const (
	// CapabilityNetwork is opening connections or making requests.
	CapabilityNetwork = "network"
	// CapabilityProcesses is starting other programs.
	CapabilityProcesses = "processes"
	// CapabilityFilesystem is reading or writing files and the environment.
	CapabilityFilesystem = "filesystem"
	// CapabilitySyscalls is calling the operating system directly.
	CapabilitySyscalls = "syscalls"
	// CapabilityUnsafe is stepping outside the type system or loading code at
	// runtime.
	CapabilityUnsafe = "unsafe"
)

// Code style postures.
const (
	// StyleNeutral asks only for what is true of any working program: complete
	// code, no placeholders, failures reported rather than discarded. It is
	// the default because everything beyond it is a house preference, and a
	// preference the engine applies without being asked is a bias dressed as a
	// standard.
	StyleNeutral = "neutral"
	// StyleFunctional asks for small total functions, values over mutation,
	// and effects at the edges.
	StyleFunctional = "functional"
)

// DefaultSettings is the shape a run takes when nobody has said otherwise.
//
// The defaults lean toward being told the truth quickly: ask rather than
// assume, check the machine in front of you rather than three you are not
// using, and keep every check that costs little and catches much.
func DefaultSettings() Settings {
	return Settings{
		Ambiguity:                AmbiguityAsk,
		Platforms:                PlatformsHost,
		MaximumAttempts:          6,
		MutationThresholdPercent: 50,
		RepetitionRuns:           3,
		SuiteBudgetSeconds:       60,
		FuzzSeconds:              20,
		HostileTimeoutSeconds:    10,
		TangleThreshold:          8,
		RequireTests:             true,
		RequireDocComments:       true,
		AdversarialReview:        true,
		CodeStyle:                StyleNeutral,
		ModelLadder:              DefaultLadder(),
		StallBeforeEscalation:    3,
		DecomposeWhenExhausted:   true,
		PlanningRung:             Rung{Model: ModelSol, Effort: EffortHigh}.String(),
		ApprovalRungs:            DefaultApprovalRungs(),
	}
}

// ToggleKind is how a settings page should render one choice.
type ToggleKind string

const (
	// ToggleChoice is one of a fixed set of named options.
	ToggleChoice ToggleKind = "choice"
	// ToggleNumber is a bounded whole number.
	ToggleNumber ToggleKind = "number"
	// ToggleSwitch is on or off.
	ToggleSwitch ToggleKind = "switch"
	// ToggleSet is any number of a fixed set of named options, none included.
	ToggleSet ToggleKind = "set"
	// ToggleSequence is an ordered list of named options where the order is
	// what the setting means, so a page has to let it be rearranged rather
	// than only ticked.
	ToggleSequence ToggleKind = "sequence"
	// ToggleMapping is a set of named keys each given one of the choices.
	ToggleMapping ToggleKind = "mapping"
)

// Toggle describes one setting well enough to render and validate it.
//
// The description travels with the setting rather than living in the interface
// because the two drift apart otherwise: a bound changed here and not there
// produces a control that offers a value the engine will refuse.
type Toggle struct {
	Key   string
	Label string
	// Help says what the setting costs and what it buys, in the terms of
	// someone deciding whether to change it — not a restatement of the label.
	Help    string
	Kind    ToggleKind
	Choices []string
	Minimum int
	Maximum int
	// Group lets a page put related controls together without inventing its
	// own taxonomy and getting a different one from the next page.
	Group string
}

// Describe is every setting, in the order a page should show them.
//
// It is a function rather than a variable so a caller cannot quietly mutate
// the description that everything else validates against.
func Describe() []Toggle {
	return []Toggle{
		{
			Key: "ambiguity", Label: "When a request reads two ways",
			Help: "Ask stops before doing anything and puts the question to " +
				"you. Assume states the narrowest defensible reading and gets " +
				"on with it. Asking costs a round trip; assuming costs being " +
				"wrong without knowing it.",
			Kind: ToggleChoice, Group: "Intake",
			Choices: []string{AmbiguityAsk, AmbiguityAssume},
		},
		{
			Key: "platforms", Label: "Platforms a program must build for",
			Help: "Host checks the machine you are on. Portable also " +
				"cross-compiles for Linux, macOS and Windows, which costs a " +
				"build per platform on every run and only matters if you ship " +
				"to them.",
			Kind: ToggleChoice, Group: "Build",
			Choices: []string{PlatformsHost, PlatformsPortable},
		},
		{
			Key: "maximum_attempts", Label: "Attempts before stopping",
			Help: "How many times a run may rewrite its own work when a check " +
				"fails. Each attempt is a full model round, so this is the " +
				"main thing a run costs. It is the allowance on each rung of " +
				"the model ladder, not the whole run: a run that escalates " +
				"starts the new model on a fresh one.",
			Kind: ToggleNumber, Group: "Refinement", Minimum: 1, Maximum: 12,
		},
		{
			Key:   "mutation_threshold_percent",
			Label: "Defects the tests must catch",
			Help: "Deliberate faults are injected and the tests are run. Below " +
				"this share caught, the tests are not treated as able to " +
				"detect anything. Raising it finds weaker suites and costs a " +
				"test run per fault.",
			Kind: ToggleNumber, Group: "Verification", Minimum: 0, Maximum: 100,
		},
		{
			Key: "repetition_runs", Label: "Times to repeat the suite",
			Help: "Repeating tells a correct program from an intermittently " +
				"correct one. One run cannot. Set to 1 to skip the check.",
			Kind: ToggleNumber, Group: "Verification", Minimum: 1, Maximum: 20,
		},
		{
			Key: "suite_budget_seconds", Label: "Seconds the tests may take",
			Help: "Past this, a slow suite is treated as its own problem " +
				"rather than a passing one.",
			Kind: ToggleNumber, Group: "Verification", Minimum: 1, Maximum: 3600,
		},
		{
			Key: "fuzz_seconds", Label: "Seconds to fuzz each target",
			Help: "Applies only where the run wrote a fuzz target. Zero turns " +
				"fuzzing off, which is the truth on a program with nothing to " +
				"parse.",
			Kind: ToggleNumber, Group: "Verification", Minimum: 0, Maximum: 600,
		},
		{
			Key:   "hostile_timeout_seconds",
			Label: "Seconds a program may take on hostile input",
			Help: "A produced program is run against input nobody intended. " +
				"Past this it counts as hung rather than slow.",
			Kind: ToggleNumber, Group: "Verification", Minimum: 1, Maximum: 120,
		},
		{
			Key: "tangle_threshold", Label: "Decision points before splitting",
			Help: "A function with more branches than this is asked to be " +
				"broken up. Lower is stricter and produces more rewriting.",
			Kind: ToggleNumber, Group: "Quality", Minimum: 2, Maximum: 40,
		},
		{
			Key: "require_tests", Label: "Every function needs its own test",
			Help: "A test that calls the function directly, not one that " +
				"reaches it through something else. Off, a run may ship " +
				"functions nothing has examined.",
			Kind: ToggleSwitch, Group: "Quality",
		},
		{
			Key: "require_doc_comments", Label: "Every function needs a comment",
			Help: "A doc comment on the declaration saying what it is for and " +
				"how it works. Off, the code ships undocumented.",
			Kind: ToggleSwitch, Group: "Quality",
		},
		{
			Key: "code_style", Label: "House style for produced code",
			Help: "Neutral asks only for what is true of any working program. " +
				"Functional additionally asks for small total functions, values " +
				"rather than mutation, and effects pushed to the edges. Neutral " +
				"is the default because a style the engine applies without " +
				"being asked is a bias, not a standard.",
			Kind: ToggleChoice, Group: "Quality",
			Choices: []string{StyleNeutral, StyleFunctional},
		},
		{
			Key: "adversarial_review", Label: "Review work that already passes",
			Help: "Attacks work once every gate holds, looking for surviving " +
				"defects, swallowed failures and untried inputs. It costs an " +
				"attempt and is the only check that asks whether the tests are " +
				"worth anything.",
			Kind: ToggleSwitch, Group: "Quality",
		},
		{
			Key: "model_ladder", Label: "Models to climb, cheapest first",
			Help: "Every run starts on the first and moves up only when it " +
				"stops making progress, so the expensive model is paid for on " +
				"the requests that need it and nothing else. Each rung gets its " +
				"own full allowance of attempts, so a longer ladder raises what " +
				"a stuck run can cost; one model means runs never escalate.",
			Kind: ToggleSequence, Group: "Escalation", Choices: KnownRungs(),
		},
		{
			Key: "planning_rung", Label: "Model that decides how work is split",
			Help: "Does not climb: it is at the top from the first request. " +
				"Planning is a handful of tokens every later attempt is spent " +
				"against, and a decomposition that misses a behaviour is paid " +
				"for on every rung, on every attempt. Lowering this is the " +
				"cheapest saving on paper and the most expensive in practice.",
			Kind: ToggleChoice, Group: "Escalation", Choices: KnownRungs(),
		},
		{
			Key: "approval_rungs", Label: "Rungs that need a person to say yes",
			Help: "A run stops rather than climbing to one of these, and says " +
				"what it tried and what the step would cost. There is no " +
				"resume: the run ends and is started again once the rung is " +
				"allowed, so this is a stop, not a pause.",
			Kind: ToggleSet, Group: "Escalation", Choices: KnownRungs(),
		},
		{
			Key:   "stall_before_escalation",
			Label: "Repeated identical failures before moving up",
			Help: "Counted on the same gate failing with the same message, not " +
				"on attempts. A run failing something different each time is " +
				"converging and is left alone. Lower reaches the better model " +
				"sooner and costs more; higher spends attempts proving the " +
				"cheap model cannot do it.",
			Kind: ToggleNumber, Group: "Escalation", Minimum: 1, Maximum: 10,
		},
		{
			Key: "decompose_when_exhausted", Label: "Split the request at the top",
			Help: "When the best model has also stalled, break the request into " +
				"smaller units and start again rather than keep asking. Off, " +
				"the run stops and says where it got stuck.",
			Kind: ToggleSwitch, Group: "Escalation",
		},
		{
			Key: "stage_models", Label: "Model for particular stages",
			Help: "Pins a stage to a model whatever rung the ladder is on, for " +
				"the one part of the work a project already knows is harder " +
				"than the rest. Only stages that make a model request can take " +
				"one; the others are analysis and would ignore it.",
			Kind: ToggleMapping, Group: "Escalation", Choices: KnownRungs(),
		},
		{
			Key: "forbidden_capabilities", Label: "Capabilities a program may not take",
			Help: "Nothing is forbidden by default: the engine reports what a " +
				"program can reach for and leaves the judgement to you, because " +
				"a fixed prohibition fails a program for doing what it was " +
				"asked. Name one here and taking it becomes a failure.",
			Kind: ToggleSet, Group: "Build",
			Choices: []string{
				CapabilityNetwork, CapabilityProcesses, CapabilityFilesystem,
				CapabilitySyscalls, CapabilityUnsafe,
			},
		},
	}
}

// Validate refuses a configuration the engine would not honour.
//
// It is checked here rather than at the point of use so a bad value is
// reported when it is set, by something that can say which setting is wrong,
// instead of surfacing later as a run behaving oddly.
func (settings Settings) Validate() error {
	byKey := map[string]Toggle{}
	for _, toggle := range Describe() {
		byKey[toggle.Key] = toggle
	}
	for _, check := range []struct {
		key   string
		value string
	}{
		{"ambiguity", settings.Ambiguity},
		{"platforms", settings.Platforms},
		{"code_style", settings.CodeStyle},
		{"planning_rung", settings.PlanningRung},
	} {
		toggle := byKey[check.key]
		if !contains(toggle.Choices, check.value) {
			return fmt.Errorf("%s is %q, which is not one of %v",
				toggle.Label, check.value, toggle.Choices)
		}
	}
	for _, check := range []struct {
		key   string
		value int
	}{
		{"maximum_attempts", settings.MaximumAttempts},
		{"mutation_threshold_percent", settings.MutationThresholdPercent},
		{"repetition_runs", settings.RepetitionRuns},
		{"suite_budget_seconds", settings.SuiteBudgetSeconds},
		{"fuzz_seconds", settings.FuzzSeconds},
		{"hostile_timeout_seconds", settings.HostileTimeoutSeconds},
		{"tangle_threshold", settings.TangleThreshold},
		{"stall_before_escalation", settings.StallBeforeEscalation},
	} {
		toggle := byKey[check.key]
		if check.value < toggle.Minimum || check.value > toggle.Maximum {
			return fmt.Errorf("%s is %d, outside %d to %d",
				toggle.Label, check.value, toggle.Minimum, toggle.Maximum)
		}
	}
	// A restriction the engine does not recognise would be silently ignored,
	// which is the worst outcome available: the person believes a limit is in
	// force and nothing is checking it.
	forbidden := byKey["forbidden_capabilities"]
	for _, named := range settings.ForbiddenCapabilities {
		if !contains(forbidden.Choices, named) {
			return fmt.Errorf("%s names %q, which is not one of %v, so nothing "+
				"would enforce it", forbidden.Label, named, forbidden.Choices)
		}
	}
	return validateEscalation(settings)
}

// FirstRung is what a run starts on.
func (settings Settings) FirstRung() string {
	if len(settings.ModelLadder) == 0 {
		return Rung{Model: ModelLuna, Effort: EffortLow}.String()
	}
	return settings.ModelLadder[0]
}

// NextRung is the step above one, and whether there is one.
func (settings Settings) NextRung(current string) (string, bool) {
	for index, named := range settings.ModelLadder {
		if named == current && index+1 < len(settings.ModelLadder) {
			return settings.ModelLadder[index+1], true
		}
	}
	return "", false
}

// NeedsApproval reports whether reaching a rung requires being asked first.
func (settings Settings) NeedsApproval(rung string) bool {
	return contains(settings.ApprovalRungs, rung)
}

// RungForStage is the rung a stage's next attempt runs on.
//
// An override wins over the ladder, but only upward. A project that pinned a
// gate to a capable rung did so knowing it is the hard part, and escalation
// quietly moving it elsewhere would make the pin meaningless — while a pin
// below the current rung would undo an escalation the run earned by stalling.
func (settings Settings) RungForStage(stage string, current string) string {
	pinned, ok := settings.StageModels[stage]
	if !ok {
		return current
	}
	wanted, wantedErr := ParseRung(pinned)
	here, hereErr := ParseRung(current)
	if wantedErr != nil || hereErr != nil {
		return current
	}
	if rungCost(wanted) > rungCost(here) {
		return pinned
	}
	return current
}

// Groups lists the setting groups in the order a page should show them.
func Groups() []string {
	seen := map[string]bool{}
	var order []string
	for _, toggle := range Describe() {
		if seen[toggle.Group] {
			continue
		}
		seen[toggle.Group] = true
		order = append(order, toggle.Group)
	}
	return order
}

// SortedKeys is every setting key, for a caller storing them by name.
func SortedKeys() []string {
	keys := make([]string, 0, len(Describe()))
	for _, toggle := range Describe() {
		keys = append(keys, toggle.Key)
	}
	sort.Strings(keys)
	return keys
}

// contains reports whether a choice is one of those offered.
func contains(choices []string, value string) bool {
	for _, choice := range choices {
		if choice == value {
			return true
		}
	}
	return false
}
