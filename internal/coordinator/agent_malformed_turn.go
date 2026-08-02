package coordinator

import "strings"

// malformedTurnInstruction tells the next attempt what the loop refused.
//
// The loop's refusals are all the same shape: the model asked for something
// the approved plan does not permit, so nothing was done and the turn was
// thrown away. That is recoverable — the model can ask for something the plan
// does permit — but only if it is told what the rule was. Handed the raw error
// alone, a run rewrites the same turn and is refused the same way.
//
// The rules named here are the ones the loop actually enforces, stated as
// things to do rather than as things that went wrong, because an instruction
// that only describes a failure leaves the reader to infer the remedy.
func malformedTurnInstruction(refusal error) string {
	var instruction strings.Builder
	instruction.WriteString(
		"The last turn was refused before anything was written, so nothing " +
			"from it survives. What the loop said:\n\n")
	instruction.WriteString(refusal.Error())
	instruction.WriteString("\n\nThe plan is a contract and every tool call is " +
		"checked against it:\n\n")
	instruction.WriteString(
		"1. Each plan step names exactly one file. Write that file, and " +
			"nothing else, under that step.\n")
	instruction.WriteString(
		"2. One step is completed by one call. Do not write the same file " +
			"twice in a turn — the second write is attributed to the next open " +
			"step, whose file it is not, and the whole turn is refused.\n")
	instruction.WriteString(
		"3. If a file needs more work, finish it in one write. There is no " +
			"partial edit that a later call in the same turn completes.\n")
	instruction.WriteString(
		"4. Only write files the plan names. A file the plan does not mention " +
			"has no step to bind to.\n\n")
	instruction.WriteString(
		"Make the next turn satisfy those rules. If the work genuinely needs " +
			"a file the plan does not name, write what the plan does name and " +
			"say what is missing rather than reaching outside it.")
	return instruction.String()
}
