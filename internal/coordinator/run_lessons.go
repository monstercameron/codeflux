package coordinator

import (
	"context"
	"fmt"
	"strings"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// maximumLessonsInContext bounds how many past lessons a run is shown.
//
// A run handed forty accumulated hints reads none of them, and the ones it
// skips are not the ones it judged least important — they are the ones at the
// bottom of a long list. The same reasoning already bounds the synthesised
// case ladder.
const maximumLessonsInContext = 8

// recordRunLesson writes down what went wrong so a later run is told before it
// repeats it.
//
// This is the write half of the loop §31 describes, at the moment the run
// actually learns something: a gate refused the work and said why. It is
// recorded as an observation rather than a rule, with evidence strength fixed
// at none by the domain constructor, because one run failing one way is not
// yet a law about this project — it is a thing that happened, which is exactly
// what the observation tier is for.
//
// The evidence source is the automated validation run, not the agent. A gate
// failing is something the platform observed; the agent's own account of it
// would be self-report, which UpsertExtractedMemoryFact refuses outright and
// should.
//
// Failures here are swallowed. A run that could not write down its lesson has
// still done its work, and losing the lesson must not lose the run.
func (execution *AgentExecution) recordRunLesson(
	ctx context.Context,
	scope agentScope,
	gate string,
	because string,
	detail string,
) {
	if execution == nil || execution.repositories == nil {
		return
	}
	statement := lessonStatement(gate, because, detail)
	if statement == "" {
		return
	}
	content, err := domain.NewObservationHypothesisContent(
		scope.repositoryID, statement)
	if err != nil {
		return
	}
	source := domain.EvidenceSourceKindAutomatedValidationRun
	_, _ = execution.repositories.UpsertExtractedMemoryFact(ctx,
		storage.UpsertExtractedMemoryFact{
			ProjectID: scope.projectID,
			Content: domain.MemoryArtifactContent{
				Kind:                  domain.MemoryArtifactKindObservationHypothesis,
				ObservationHypothesis: &content,
			},
			// The seed is what makes the same lesson from two runs one fact
			// rather than two. It is the gate and the reason, not the detail,
			// because the detail carries file names and line numbers that
			// differ between runs of the same mistake.
			DeterministicSeed: "run-lesson:" + gate + ":" +
				strings.ToLower(strings.TrimSpace(because)),
			Evidence: &storage.UpsertExtractedMemoryFactEvidence{
				Source: source,
				TaskID: scope.taskID,
			},
		})
}

// lessonStatement renders one failure as a single thing to do differently.
//
// Sharp and atomic, not a narration. "An earlier run was sent back at the
// assembly gate because it did not compile" tells the next run nothing it can
// act on; "Compile the package before reporting the work finished" does. One
// lesson is one instruction, so two lessons cannot half-overlap into a
// paragraph nobody reads.
//
// Known failure shapes get a written rule. Anything else falls back to a
// compact form of what the gate said, because inventing an imperative for a
// failure this does not recognise would be guessing at a fix — and a confident
// wrong rule is worse in a prompt than a plain observation.
func lessonStatement(gate, because, detail string) string {
	gate = strings.TrimSpace(gate)
	because = strings.TrimSpace(because)
	if gate == "" || because == "" {
		return ""
	}
	if rule, known := sharpLessons[gate]; known {
		return rule
	}
	return "Avoid whatever caused this: the " + gate + " gate sent work back " +
		"because " + strings.TrimSuffix(because, ".") + "."
}

// sharpLessons is the imperative each gate's refusal actually means.
//
// Keyed by gate rather than by the failure text, because the text carries file
// names and line numbers that differ between runs of the same mistake, and a
// lesson that differs per run is a lesson that never merges and fills the
// context with restatements of one idea.
var sharpLessons = map[string]string{
	"assembly": "Build the package before reporting the work finished. A run " +
		"that does not compile has produced nothing.",
	"acceptance": "Reproduce the acceptance examples exactly, byte for byte, " +
		"including spacing and the final line. Fix the program, never the " +
		"example.",
	"integration-tests": "Run the tests again after the last edit. A suite " +
		"result from before a write describes code that no longer exists.",
	"completeness": "Give every function a doc comment starting with its own " +
		"name, and a test that calls it by name.",
	"atom-case-synthesis": "Test the empty input, the single-element input " +
		"and the repeated-element input, not only the ordinary one.",
	"adversarial-review": "Never discard an error. A function that ignores " +
		"what a call returns hides the failure it was meant to report.",
}

// lessonContextItems are the project's past lessons, ready to put in front of
// a run before it starts.
//
// Returned as ordinary context items rather than appended to the instruction,
// so a lesson is visibly a thing the project learned rather than part of what
// this request is asking for. A run that cannot tell those apart will treat an
// old lesson as a new requirement.
func (execution *AgentExecution) lessonContextItems(
	ctx context.Context,
	scope agentScope,
) []agentloop.RepositoryContextItem {
	if execution == nil || execution.repositories == nil {
		return nil
	}
	lessons, err := execution.repositories.ListProjectLessons(
		ctx, scope.projectID, maximumLessonsInContext)
	if err != nil || len(lessons) == 0 {
		return nil
	}
	var body strings.Builder
	body.WriteString(
		"Earlier runs in this project were sent back for these reasons. They " +
			"are not part of this request; they are mistakes already made " +
			"here, and repeating one costs an attempt.\n\n")
	for index, lesson := range lessons {
		fmt.Fprintf(&body, "%d. %s\n", index+1, lesson.Statement)
	}
	return []agentloop.RepositoryContextItem{
		agentContextItem("lessons-from-earlier-runs", body.String()),
	}
}
