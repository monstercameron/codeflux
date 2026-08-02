package coordinator

import "strings"

// decompositionInstruction is what a run is told when the ladder is spent.
//
// It is the third rung of the escalation and it is a different kind of move
// from the first two. Those change who is doing the work; this changes what
// the work is. A request the best available model has failed three times in
// the same place is usually not one unit of work that is hard — it is several
// units of work presented as one, where the run has to hold all of them in
// mind at once and gets no feedback until every one of them is right.
//
// Nothing here mentions the failure. The failure is appended by the caller,
// after this, because it has already been shown twice and shown again produces
// the same attempt a third time. What is new is the instruction to stop trying
// to land it in one piece.
func decompositionInstruction(requirement string) string {
	var instruction strings.Builder
	instruction.WriteString(
		"This request has now failed the same check the same way at the top " +
			"of the model ladder. Do not try to satisfy it in one piece again.\n\n")
	instruction.WriteString(
		"Break the work into the smallest units that can each be finished and " +
			"checked on their own, and build them one at a time:\n\n")
	instruction.WriteString(
		"1. Name every distinct behaviour the request asks for. A behaviour is " +
			"distinct when it could be got right while another is got wrong.\n")
	instruction.WriteString(
		"2. For each one, write the function that does only that, and a test " +
			"that calls it directly with the inputs it must handle — including " +
			"the empty, the degenerate and the invalid.\n")
	instruction.WriteString(
		"3. Get each one passing before writing the next. Do not write the " +
			"composition until every part it composes passes on its own.\n")
	instruction.WriteString(
		"4. Only then wire them together, and only then run the whole thing " +
			"against what was asked.\n\n")
	instruction.WriteString(
		"If two behaviours keep failing together, they are one behaviour and " +
			"belong in one function. If one function needs more than a handful " +
			"of tests to pin down, it is more than one behaviour and should be " +
			"split further.\n\n")
	// The request is restated because the run is being asked to re-read it,
	// not to remember it. A decomposition done from memory of a request is a
	// decomposition of the misreading that has been failing.
	instruction.WriteString("Re-read what was asked before you start:\n\n")
	instruction.WriteString(strings.TrimSpace(requirement))
	return instruction.String()
}
