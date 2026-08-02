package coordinator

import (
	"strings"

	"codeflux.dev/codeflux/internal/pipeline"
)

// approvalRequest is what a person is shown when a run wants a rung it may not
// take on its own.
//
// The gate is only worth having if the person can decide. "Approve maximum
// effort?" is a question nobody has grounds to answer, so it gets answered
// yes every time, and a rubber stamp is worse than no gate at all: it looks
// like oversight while being a click-through. Everything needed to say no is
// already known by the time the run stops — what it has tried, what keeps
// failing, what it has cost so far, and what the next step changes — so all of
// it is put in front of the person rather than summarised away.
func approvalRequest(
	rung string,
	gate string,
	failure string,
	progress *convergence,
) string {
	var request strings.Builder
	request.WriteString("This run has stopped and is asking before going further.\n\n")

	request.WriteString("What it wants: ")
	if parsed, err := pipeline.ParseRung(rung); err == nil {
		request.WriteString(parsed.Describe())
	} else {
		request.WriteString(rung)
	}
	request.WriteString(", which this machine is configured not to reach " +
		"without being asked.\n\n")

	request.WriteString("Where it got to: ")
	request.WriteString(progress.summary())
	request.WriteString(".\n\n")

	request.WriteString("What keeps failing — the ")
	request.WriteString(gate)
	request.WriteString(" check, unchanged across ")
	request.WriteString(counted(progress.settings.StallBeforeEscalation, "attempt"))
	request.WriteString(":\n")
	request.WriteString(firstLines(failure, 12))
	request.WriteString("\n\n")

	request.WriteString("What the step changes: ")
	request.WriteString(costChange(progress.currentModel(), rung))
	request.WriteString("\n\n")

	// Said plainly, because a person told a run is "awaiting approval" would
	// reasonably expect it to carry on by itself once they said yes. It will
	// not: there is no resume, and a run that ended is started again.
	request.WriteString("There is no resume. This run has ended. To let it " +
		"go further, allow that rung in the settings and start the task again; " +
		"it begins from the request, not from where this one stopped.")
	return request.String()
}

// costChange says what moving between two rungs does to what a run is billed.
//
// The two axes are not comparable and saying so is the point: more effort
// bills more tokens at the rate already in force, while a different model
// changes the rate on every token. A person deciding whether to approve is
// deciding between those two, not between two numbers.
func costChange(from string, to string) string {
	here, hereErr := pipeline.ParseRung(from)
	there, thereErr := pipeline.ParseRung(to)
	if hereErr != nil || thereErr != nil {
		return "unknown, because one of the rungs could not be read"
	}
	switch {
	case here.Model == there.Model:
		return "the same model thinking harder, so more tokens at the rate " +
			"already in force — a larger bill, not a different rate"
	case there.Model == pipeline.ModelSol:
		return "a different model, so a higher rate on every token, not only " +
			"on the extra thinking"
	default:
		return "a different model at a different rate"
	}
}

// firstLines is the head of a failure, so a long diff does not bury the point.
func firstLines(text string, limit int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) <= limit {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:limit], "\n") + "\n… and " +
		counted(len(lines)-limit, "more line")
}
