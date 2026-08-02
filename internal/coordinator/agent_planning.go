package coordinator

import (
	"context"
	"strings"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/executor"
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
	requirement string,
) ([]agentloop.PlanStep, string) {
	parsed := agentPlanSteps(requirement)
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
	})
	if err != nil {
		return parsed, "parsed from the request, because the planning model " +
			"did not answer: " + err.Error()
	}
	behaviours := parseBehaviours(turn.MessageRedacted)
	if len(behaviours) == 0 {
		return parsed, "parsed from the request, because the planning model " +
			"named no distinct behaviours"
	}
	return stepsForBehaviours(behaviours, parsed),
		"planned on " + execution.settings.PlanningRung + ": " +
			counted(len(behaviours), "behaviour") + " named"
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
		step := agentloop.PlanStep{
			ID: "edit-" + itoaPlan(index+1), Kind: agentloop.StepKindEdit,
			State:           agentloop.StepPending,
			SummaryRedacted: "Write " + file + " — " + shared,
			MaterialEdit:    true, ValidationRequired: true,
			ExpectedFiles:   []string{file},
			CompletionTools: []executor.ToolName{executor.ToolApplyEdit},
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
