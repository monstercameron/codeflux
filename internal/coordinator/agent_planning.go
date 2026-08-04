package coordinator

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
)

// planFromRequirement decides how a request is broken into steps.
//
// Until this existed, planning was a regular expression: filenames were pulled
// out of the requirement, one edit step was emitted per file, and a verify step
// was added on the end. That plan is exactly as good as the requirement's
// phrasing. A request that named its files got a reasonable plan; one that
// described three behaviours in one file got a single step that said "write
// this file" and a run that had to hold all three in mind at once with no
// feedback until every one of them was right — which is precisely the shape
// that stalls, escalates, and ends up being decomposed four rungs later at
// several times the cost.
//
// So it is a model request now, and it is the one place a run spends the most
// up front on purpose. Planning is a handful of tokens that every later attempt
// is spent against: a decomposition that misses a behaviour is paid for on
// every rung, on every attempt, and no amount of effort further down recovers
// it. The rung it runs on does not climb, because there is nothing to climb
// from — it is at the top from the first request.
//
// The parsed plan is the fallback rather than the error path. A provider
// hiccup during planning should cost a worse decomposition, not the run.
func (execution *AgentExecution) planFromRequirement(
	ctx context.Context,
	worktree string,
	requirement string,
) ([]agentloop.PlanStep, string) {
	parsed := agentPlanSteps(worktree, requirement)
	if execution.escalate == nil || execution.settings.PlanningRung == "" {
		return parsed, "parsed from the request, because no planning model is " +
			"configured"
	}
	planner, err := execution.escalate(execution.settings.PlanningRung)
	if err != nil {
		return parsed, "parsed from the request, because the planning model " +
			"could not be built: " + err.Error()
	}
	turn, err := planner.ObserveThink(ctx, agentloop.ModelInput{
		RepositoryContext: []agentloop.RepositoryContextItem{
			agentContextItem("planning-request", planningInstruction(requirement)),
		},
		StructuredOutput: behaviourSchema(),
	})
	if err != nil {
		return parsed, "parsed from the request, because the planning model " +
			"did not answer: " + err.Error()
	}
	// The schema first, the line parser as the fallback for a model that could
	// not honour it.
	behaviours, named, structured := decodedBehaviours(turn.MessageRedacted)
	if !structured {
		behaviours = parseBehaviours(turn.MessageRedacted)
	}
	if len(behaviours) == 0 {
		return parsed, "parsed from the request, because the planning model " +
			"named no distinct behaviours"
	}
	// The layout is the planner's only when the request did not give one.
	//
	// A path in the request is the person saying where the work goes, and it
	// outranks anything inferred; filesNamedIn already prefers it, so a request
	// that named files reaches here with those files and must keep them. What
	// is being replaced is the case where filesNamedIn had nothing to go on and
	// answered with a constant.
	layout := ""
	if requirementNamesNoFile(requirement) {
		if files := layoutFromPlanner(named); len(files) > 0 {
			parsed = agentPlanStepsForFiles(worktree, requirement, files)
			layout = ", laid out in " + counted(len(files), "file")
		}
	}
	return stepsForBehaviours(worktree, behaviours, parsed),
		"planned on " + execution.settings.PlanningRung + ": " +
			counted(len(behaviours), "behaviour") + " named" + layout
}

// requirementNamesNoFile reports that the request left the layout open.
//
// Asked of the same extractor filesNamedIn consults, so the two cannot disagree
// about whether a request named a path. Two readers of one requirement drifting
// is how a plan came to name a file no step covered, and it is not worth
// repeating for this.
func requirementNamesNoFile(requirement string) bool {
	analysis, err := storage.AnalyzeTaskRequirement(requirement)
	return err != nil || len(analysis.ExplicitFiles) == 0
}

// planningInstruction asks for a decomposition and nothing else.
//
// It asks for behaviours rather than files because files are what the old
// parser already knew and are not where the difficulty is. What a run needs
// before it starts is the list of things that can independently be got wrong,
// since that is the list its tests have to cover and the unit at which it can
// get feedback.
func planningInstruction(requirement string) string {
	var instruction strings.Builder
	instruction.WriteString(
		"Break this request into the distinct behaviours it asks for. A " +
			"behaviour is distinct when it could be got right while another " +
			"is got wrong, and so needs its own test.\n\n")
	instruction.WriteString(
		"Answer with one behaviour per line and nothing else. Each line is a " +
			"short imperative phrase naming what the program must do — not a " +
			"file, not a step, not a heading. Do not number them. Do not " +
			"explain. If the request asks for one behaviour, answer with one " +
			"line.\n\n")
	// The layout, because the alternative was a constant.
	//
	// Where the work goes was not anybody's decision: when the request named no
	// path, the plan was cmd/generated/main.go and its test whatever had been
	// asked for. A request whose point is that a command imports a package it
	// exports from was planned into one directory, and the run could not have
	// produced otherwise — the plan's files are the scope every write is checked
	// against, so a second package was unreachable rather than merely unplanned.
	instruction.WriteString(
		"Then say which files the work goes in, as module-relative paths " +
			"ending .go. Group it the way the request describes: code that one " +
			"part imports from another has to be in a package that part can " +
			"import, and a package cannot import a command. Name only source " +
			"files — a test file is added beside each one for you. If the " +
			"request already says where the work goes, answer with no files at " +
			"all.\n\n")
	instruction.WriteString("The request:\n\n")
	instruction.WriteString(strings.TrimSpace(requirement))
	return instruction.String()
}

// parseBehaviours reads the answer back.
//
// It is deliberately forgiving about decoration and strict about length. A
// model that numbered its lines or added a heading has still answered; one
// that returned a paragraph has not, and treating a paragraph as one behaviour
// would produce a plan claiming the work is a single unit when nothing
// established that.
func parseBehaviours(answer string) []string {
	var behaviours []string
	for _, line := range strings.Split(answer, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "-*• \t")
		line = trimLeadingNumber(line)
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasSuffix(line, ":"):
			// A heading, not a behaviour.
			continue
		case len(line) > 200:
			// Prose. A behaviour that takes two hundred characters to name is
			// more than one behaviour, and splitting it here would be guessing.
			continue
		}
		behaviours = append(behaviours, line)
		// A bound, because a plan with forty steps is not a plan. A request
		// that genuinely has forty behaviours needs to be several requests,
		// and the decomposition rung is where that gets said.
		if len(behaviours) >= 12 {
			break
		}
	}
	return behaviours
}

// trimLeadingNumber removes "1." or "1)" from the front of a line.
func trimLeadingNumber(line string) string {
	digits := 0
	for digits < len(line) && line[digits] >= '0' && line[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits >= len(line) {
		return line
	}
	if line[digits] == '.' || line[digits] == ')' {
		return line[digits+1:]
	}
	return line
}

// stepsForBehaviours turns named behaviours into the steps a run executes.
//
// The files still come from the parser: which files exist is a fact about the
// request and the repository, and asking a model to invent paths would produce
// a plan whose steps cannot bind to anything on disk. What the model
// contributes is what each step is *for*, which is the part the parser could
// never know.
func stepsForBehaviours(
	worktree string,
	behaviours []string,
	parsed []agentloop.PlanStep,
) []agentloop.PlanStep {
	var files []string
	var verify agentloop.PlanStep
	for _, step := range parsed {
		if step.Kind == agentloop.StepKindTest {
			verify = step
			continue
		}
		files = append(files, step.ExpectedFiles...)
	}
	if len(files) == 0 {
		return parsed
	}
	steps := make([]agentloop.PlanStep, 0, len(files)+1)
	// One step per file still, because a step is completed by one tool call
	// and a step covering two files leaves the second write with nowhere to
	// bind. What changes is that every step now carries the whole
	// decomposition, so a run writing one file knows what the others owe.
	shared := "The behaviours this program must have, each of which needs its " +
		"own test:\n" + numbered(behaviours)
	for index, file := range files {
		kind := writeStepKindFor(worktree, file)
		step := agentloop.PlanStep{
			ID: "edit-" + itoaPlan(index+1), Kind: kind,
			State:           agentloop.StepPending,
			SummaryRedacted: "Write " + file + " — " + shared,
			MaterialEdit:    true, ValidationRequired: true,
			ExpectedFiles:   []string{file},
			CompletionTools: writeToolsFor(kind),
		}
		steps = append(steps, step)
	}
	if verify.ID != "" {
		return append(steps, verify)
	}
	return steps
}

// numbered renders the behaviours as a list a model can act on.
func numbered(behaviours []string) string {
	var list strings.Builder
	for index, behaviour := range behaviours {
		list.WriteString(itoaPlan(index + 1))
		list.WriteString(". ")
		list.WriteString(behaviour)
		list.WriteString("\n")
	}
	return strings.TrimRight(list.String(), "\n")
}

// itoaPlan renders a small positive number.
func itoaPlan(value int) string {
	if value < 10 {
		return string(rune('0' + value))
	}
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}

// behaviourSchema is the shape a planning answer must have.
//
// The prose instruction asked for "one behaviour per line and nothing else"
// and got numbered lines, bullet characters and headings often enough that
// parseBehaviours has a branch for each of them. Those branches are guesses
// about what a model meant, and the one that costs most is silent: a paragraph
// that survives the length check counts as a single behaviour, producing a plan
// that claims the work is one unit when nothing established that.
//
// A schema is not a stricter instruction. It is the difference between asking
// and constraining, and it is the reason the branches can eventually go.
//
// additionalProperties is false and every property is required, because the
// Responses API refuses a strict schema that leaves either open — and because
// a schema that permits extra fields permits the model to answer a different
// question in them.
func behaviourSchema() *providers.StructuredOutputRequirement {
	return &providers.StructuredOutputRequirement{
		Name: "decomposition",
		Description: "the distinct behaviours a request asks for, each one " +
			"something that could be got right while another is got wrong, and " +
			"the files they are written in",
		Strict: true,
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "behaviours": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "string",
        "description": "a short imperative phrase naming what the program must do, not a file and not a step"
      }
    },
    "files": {
      "type": "array",
      "items": {
        "type": "string",
        "description": "a module-relative path ending .go, such as internal/stats/stats.go or cmd/report/main.go. Empty when the request already says where the work goes."
      }
    }
  },
  "required": ["behaviours", "files"],
  "additionalProperties": false
}`),
	}
}

// decodedBehaviours reads a schema-constrained planning answer.
//
// Reported separately from the line parser rather than replacing it outright.
// Not every model configured here advertises structured output, and a run whose
// planner cannot honour a schema should get a worse decomposition rather than
// no plan — which is the same reasoning that makes the whole planning call a
// fallback rather than an error path.
func decodedBehaviours(answer string) ([]string, []string, bool) {
	var decoded struct {
		Behaviours []string `json:"behaviours"`
		Files      []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(answer)), &decoded); err != nil {
		return nil, nil, false
	}
	var behaviours []string
	for _, behaviour := range decoded.Behaviours {
		if trimmed := strings.TrimSpace(behaviour); trimmed != "" {
			behaviours = append(behaviours, trimmed)
		}
	}
	return behaviours, decoded.Files, len(behaviours) > 0
}

// maximumPlannedFiles bounds a layout.
//
// Six source files and their tests is already a larger program than any single
// request here describes, and a plan with thirty steps is not a plan: every one
// of them has to be written, tested and documented inside one attempt budget.
const maximumPlannedFiles = 12

// layoutFromPlanner turns the files a planner named into a plan's file list, or
// returns nothing when what it named cannot be used.
//
// The layout is asked for only when the request names no path, and it is asked
// for because the fallback was a constant: cmd/generated/main.go and its test,
// whatever was requested. A request whose whole point is that a command imports
// a package it exports from — rung 16 — was planned into one directory, and
// then every gate judged the plan and passed, because the gates check the plan
// and the plan was already wrong. Nothing downstream could have caught it: the
// loop refuses writes outside the plan's files, so a second package was not
// merely unplanned, it was unreachable.
//
// Validated hard and discarded whole. A layout is not a suggestion the run can
// partly follow — the plan's file list is the scope every write is checked
// against — so a list with one unusable path in it is not repaired here, where
// repairing means inventing the path the planner did not give. It falls back to
// the pair, which is a worse layout and a working run, and that is the same
// trade planFromRequirement already makes when the planner does not answer.
func layoutFromPlanner(named []string) []string {
	files := make([]string, 0, len(named)*2)
	seen := map[string]bool{}
	command := false
	for _, raw := range named {
		path, ok := canonicalPlannedPath(raw)
		if !ok {
			return nil
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, path)
		// A test file names no package of its own and cannot hold the program,
		// so it settles nothing about whether there is something to run.
		if !strings.HasSuffix(path, "_test.go") && isCommandPath(path) {
			command = true
		}
	}
	// Nothing to run is not a layout. Every request here asks for a program
	// that is built and executed, and the ladder runs the binary; a plan of
	// libraries would satisfy its own steps and produce nothing to invoke.
	if !command {
		return nil
	}
	// A test beside every source file, added rather than demanded: the gates
	// ask each function for a direct test, and a run asked for one with nowhere
	// to write it is refused for the plan's omission rather than its own.
	for _, path := range slices.Clone(files) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		test := strings.TrimSuffix(path, ".go") + "_test.go"
		if seen[test] {
			continue
		}
		seen[test] = true
		files = append(files, test)
	}
	if len(files) > maximumPlannedFiles {
		return nil
	}
	return files
}

// canonicalPlannedPath accepts a module-relative Go source path and refuses
// anything else.
//
// canonicalRelativePath is the loop's own rule, shared rather than restated so
// a path this accepts cannot be one the plan validator later refuses. What is
// added here is that it must be Go source: the plan's files are what the write
// tools are scoped to, and a run cannot be asked to write a behaviour into a
// README.
func canonicalPlannedPath(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !strings.HasSuffix(trimmed, ".go") {
		return "", false
	}
	return agentloop.CanonicalPlanPath(trimmed)
}

// isCommandPath reports whether a path is somewhere a main package lives.
//
// By convention rather than by parsing, because at planning time the file does
// not exist and there is no package clause to read. Both shapes the ladder's
// requirements produce are covered: a cmd/ directory, and a main.go at the
// module root.
func isCommandPath(path string) bool {
	return strings.HasPrefix(path, "cmd/") || path == "main.go"
}
