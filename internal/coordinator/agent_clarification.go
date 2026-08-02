package coordinator

import (
	"context"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/storage"
)

// clarification is what a run decided to do about a request it could read more
// than one way.
type clarification struct {
	// Blocked is true when the run must stop and wait for a person.
	Blocked bool
	// Question is what to ask. It is empty unless Blocked.
	Question string
	// Assumptions are the readings the run adopted and is proceeding on.
	Assumptions []string
}

// resolveAmbiguity decides whether a run may proceed on the request it was
// given.
//
// The analysis already separates a material ambiguity — one where the two
// readings produce different work — from a bounded one where a narrow default
// is defensible. What was missing was anything that acted on the distinction:
// a material ambiguity reached the store, which refused to record the plan,
// and the run died reporting a database constraint. The person never saw the
// question that would have unblocked it in one sentence.
func resolveAmbiguity(
	analysis storage.RequirementAnalysis,
	policy domain.AmbiguityPolicy,
) clarification {
	result := clarification{Assumptions: analysis.Assumptions}
	if !analysis.RequiresClarification() {
		return result
	}
	if policy == domain.AmbiguityAssume {
		// Assume is a posture about bounded readings, not a licence to invent
		// an answer to a question with no defensible default. A material
		// ambiguity is exactly that question, so it is still put to the person
		// — and the reason it could not be defaulted is said plainly.
		result.Blocked = true
		result.Question = analysis.ClarificationQuestion() +
			" (No default was taken: the readings lead to different work.)"
		return result
	}
	result.Blocked = true
	result.Question = analysis.ClarificationQuestion()
	return result
}

// askForClarity puts the run's question to the person and stops.
//
// The question goes into the conversation the person is already reading, as a
// message rather than an event, because a question nobody is shown is the same
// as no question at all.
func (execution *AgentExecution) askForClarity(
	ctx context.Context,
	scope agentScope,
	question string,
) {
	execution.say(ctx, scope, events.KindMessageFinal, strings.TrimSpace(
		"I need one answer before I start, because the request reads two ways "+
			"and the readings lead to different work.\n\n"+question+
			"\n\nNothing has been changed. Reply and I will plan from your answer.",
	))
}

// noteAssumptions records the readings a run adopted before doing the work.
//
// They are said before the work rather than reported after it, so a person who
// disagrees can stop the run while stopping it is still cheap.
func (execution *AgentExecution) noteAssumptions(
	ctx context.Context,
	scope agentScope,
	assumptions []string,
) {
	if len(assumptions) == 0 {
		return
	}
	var stated strings.Builder
	stated.WriteString("The request left some room, so I am taking it this way:")
	for _, assumption := range assumptions {
		fmt.Fprintf(&stated, "\n- %s", assumption)
	}
	stated.WriteString("\n\nSay so now if that is not what you meant.")
	execution.say(ctx, scope, events.KindMessageFinal, stated.String())
}
