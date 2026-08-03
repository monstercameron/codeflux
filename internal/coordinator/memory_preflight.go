package coordinator

import (
	"context"
	"fmt"
	"strings"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/storage"
)

// memoryPreflight is what the project already knows, gathered before the run
// plans anything.
//
// The order was the defect, not the machinery. Retrieval and registration both
// lived in recallKnownAtoms, which runs after the attempt loop has finished —
// so a run was told what it could have reused at the moment it could no longer
// use it, and the stage's report that the project held no earlier work was
// filed after the work was already rebuilt. Registration was implemented and
// worked; nothing ever read it in time to matter.
//
// Retrieve before planning, register after acceptance, record the influence
// between them. This is the first of those three.
type memoryPreflight struct {
	// candidates is everything the project holds that could conceivably apply,
	// before any gate has looked at it.
	candidates int
	// presented is what survived the gates and is put in front of the run.
	presented []presentedMemory
	// rejected is what did not, each with the reason it did not. A count of
	// zero presented items means one of two very different things — the project
	// holds nothing, or the project holds things this run must not reuse — and
	// a reader cannot tell them apart without the reasons.
	rejected []rejectedMemory
}

// presentedMemory is one reusable item, in the shape a run can act on.
type presentedMemory struct {
	name        string
	why         string
	applicable  string
	avoid       string
	use         string
	assurance   string
	fromLessons bool
}

// rejectedMemory is one candidate that was not presented, and why.
type rejectedMemory struct {
	name   string
	reason string
}

// runMemoryPreflight gathers, gates, and shapes what this run should be shown.
//
// Two sources, deliberately kept distinct. Registered atoms are code that has
// been verified and admitted, and reusing one skips building it. Lessons are
// what went wrong here before, and heeding one skips a mistake. Squeezing the
// second into the first was never possible — an execution recipe is not a
// function — and it is why the most valuable thing these runs produce had
// nowhere to live until the lesson tier was built.
func (execution *AgentExecution) runMemoryPreflight(
	ctx context.Context,
	scope agentScope,
) memoryPreflight {
	var flight memoryPreflight
	if execution == nil || execution.repositories == nil {
		return flight
	}

	documented, err := execution.repositories.
		ListAtomDocumentationDiscoveryTextByProject(ctx, scope.projectID)
	if err == nil {
		flight.candidates += len(documented)
		for _, atom := range documented {
			name := atom.Fields["Purpose"]
			if strings.TrimSpace(name) == "" {
				name = atom.AtomID.String()
			}
			if reason, refused := refuseAtomForTask(atom, scope.requirement); refused {
				flight.rejected = append(flight.rejected,
					rejectedMemory{name: traceOneLine(name, 60), reason: reason})
				continue
			}
			flight.presented = append(flight.presented, presentedMemory{
				name:       traceOneLine(name, 60),
				why:        "this task and this atom describe the same behaviour",
				applicable: atom.Fields["Use when"],
				avoid:      atom.Fields["Do not use when"],
				use:        atom.Fields["Semantics"],
				assurance:  "verified and admitted at contract " + shortHash(atom.ContractHash),
			})
		}
	}

	lessons, err := execution.repositories.ListProjectLessons(
		ctx, scope.projectID, maximumLessonsInContext*5)
	if err == nil {
		flight.candidates += len(lessons)
		ranked := rankLessonsForRequirement(lessons, scope.requirement)
		for index, lesson := range ranked {
			if index >= maximumLessonsInContext {
				flight.rejected = append(flight.rejected, rejectedMemory{
					name:   traceOneLine(lesson.Statement, 60),
					reason: "ranked below the eight this run is shown",
				})
				continue
			}
			flight.presented = append(flight.presented, presentedMemory{
				name:        traceOneLine(lesson.Statement, 60),
				use:         lesson.Statement,
				assurance:   "observed here, not yet validated",
				fromLessons: true,
			})
		}
	}

	tracef("memory", "preflight candidates=%d presented=%d rejected=%d",
		flight.candidates, len(flight.presented), len(flight.rejected))
	for _, refused := range flight.rejected {
		tracef("memory", "  rejected %s — %s", refused.name, refused.reason)
	}
	return flight
}

// refuseAtomForTask decides whether a registered atom may be put in front of
// this run, and says why when it may not.
//
// The reasons are kept exact rather than collapsed into "not eligible". A
// reader looking at a run that reused nothing needs to tell "the project holds
// nothing" from "the project holds things this run was right to refuse", and
// one word covering both hides the second case entirely.
func refuseAtomForTask(
	atom storage.AtomDocumentationDiscoveryText, requirement string,
) (string, bool) {
	if strings.TrimSpace(atom.ContractHash) == "" {
		return "no contract hash, so nothing pins what it promises", true
	}
	if strings.TrimSpace(atom.Fields["Purpose"]) == "" {
		return "no purpose recorded, so its applicability cannot be stated", true
	}
	// Word overlap, for the same reason the lesson ranking uses it: docs/plan.md
	// §0 holds the vector branch closed until a measured recall failure justifies
	// opening it, and TestAUDIT026 fails the build if production code reaches a
	// vector writer first. This is the measurement that gate is waiting for.
	wanted := meaningfulWords(requirement)
	if len(wanted) == 0 {
		return "", false
	}
	overlap := 0
	for word := range meaningfulWords(
		atom.Fields["Purpose"] + " " + atom.Fields["Retrieval concepts"],
	) {
		if wanted[word] {
			overlap++
		}
	}
	if overlap == 0 {
		return "describes no part of what this task asks for", true
	}
	return "", false
}

// contextItems renders the preflight as something a run can decide about.
//
// Not a dump of the records. A run handed a stored row reads prose and forms an
// opinion; a run handed a named item, the reason it was retrieved, where it
// applies, where it does not, and an explicit instruction to use it unchanged,
// adapt it, or reject it with a reason, makes a decision — and a decision is
// the thing that can be recorded afterwards as influence.
func (flight memoryPreflight) contextItems() []agentloop.RepositoryContextItem {
	if len(flight.presented) == 0 {
		return nil
	}
	var reusable, lessons []presentedMemory
	for _, item := range flight.presented {
		if item.fromLessons {
			lessons = append(lessons, item)
			continue
		}
		reusable = append(reusable, item)
	}

	var items []agentloop.RepositoryContextItem
	if len(reusable) > 0 {
		var body strings.Builder
		body.WriteString(
			"This project has already built and verified these. Before writing " +
				"anything that does what one of them does, decide about it: use " +
				"it unchanged, adapt it, or reject it and say why in your " +
				"reasoning. Rebuilding one silently is the one option that is " +
				"not available.\n\n")
		for _, item := range reusable {
			fmt.Fprintf(&body, "Reusable item: %s\n", item.name)
			fmt.Fprintf(&body, "  Why retrieved: %s\n", item.why)
			writeIfPresent(&body, "  Applicability", item.applicable)
			writeIfPresent(&body, "  Do not use when", item.avoid)
			writeIfPresent(&body, "  Use", item.use)
			fmt.Fprintf(&body, "  Assurance: %s\n\n", item.assurance)
		}
		items = append(items,
			agentContextItem("reusable-work-in-this-project", body.String()))
	}
	if len(lessons) > 0 {
		var body strings.Builder
		body.WriteString(
			"Earlier runs in this project were sent back for these reasons. " +
				"They are not part of this request; they are mistakes already " +
				"made here, and repeating one costs an attempt.\n\n")
		for index, item := range lessons {
			fmt.Fprintf(&body, "%d. %s\n", index+1, item.use)
		}
		items = append(items,
			agentContextItem("lessons-from-earlier-runs", body.String()))
	}
	return items
}

// summary is the one line a run's record carries about what memory did for it.
func (flight memoryPreflight) summary() string {
	if flight.candidates == 0 {
		return "the project holds nothing earlier to reuse"
	}
	return fmt.Sprintf(
		"%d candidate(s) found, %d presented, %d refused",
		flight.candidates, len(flight.presented), len(flight.rejected))
}

// writeIfPresent skips a field the documentation left empty, rather than
// printing a heading with nothing under it.
func writeIfPresent(body *strings.Builder, label, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	fmt.Fprintf(body, "%s: %s\n", label, strings.TrimSpace(value))
}

// shortHash keeps a contract hash readable in a line a person reads.
func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}
