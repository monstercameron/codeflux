package coordinator

import (
	"context"
	"sort"
	"strings"

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
	// The seed is what makes the same lesson from two runs one fact rather than
	// two. It is the gate and the reason, not the detail, because the detail
	// carries file names and line numbers that differ between runs of the same
	// mistake.
	execution.storeLesson(ctx, scope,
		"run-lesson:"+gate+":"+strings.ToLower(strings.TrimSpace(because)),
		statement)
}

// recordToolLesson writes down a mistake a tool refused, as distinct from one a
// gate refused.
//
// Gates see a finished attempt; tools see the mistake as it is made, and some
// of the most repeated ones never reach a gate at all. Ladder rung 3 had two
// writes refused for wrapping Go source in markdown, a call left with the wrong
// number of arguments after a signature changed, and a use of strings with no
// import — four attempts spent on four things a sentence apiece would have
// prevented, none of which the gate vocabulary has a word for.
//
// Only recognised shapes are recorded. A lesson invented from arbitrary
// compiler output carries the file and line it happened at, so it never merges
// with the same mistake made elsewhere, and a lessons list that grows by one
// per failure is the context bloat that makes a run skip the section.
func (execution *AgentExecution) recordToolLesson(
	ctx context.Context,
	scope agentScope,
	output string,
) {
	if execution == nil || execution.repositories == nil {
		return
	}
	key, statement, recognised := toolLessonFor(output)
	if !recognised {
		return
	}
	execution.storeLesson(ctx, scope, "tool-lesson:"+key, statement)
}

// toolLessonFor names the mistake behind a tool's output, if it is one this
// recognises.
func toolLessonFor(output string) (key, statement string, ok bool) {
	lower := strings.ToLower(output)
	for _, known := range toolFailureLessons {
		if strings.Contains(lower, known.marker) {
			return known.key, known.statement, true
		}
	}
	return "", "", false
}

// toolFailureLessons is the imperative behind each mistake a tool refuses.
//
// Ordered most specific first: a markdown-wrapped file also fails to compile,
// and telling a run to check its imports when what it actually did was paste a
// fenced block would send it looking in the wrong place.
var toolFailureLessons = []struct {
	marker    string
	key       string
	statement string
}{
	{"does not parse as go", "unparsable-write",
		"Write a Go file's source exactly, with no prose, markdown fence, " +
			"diff marker or heading around it. The file is the file, not a " +
			"message about the file."},
	{"arguments in call to", "signature-drift",
		"When a function's signature changes, change every call to it in the " +
			"same edit. A caller left behind does not compile."},
	{"undefined:", "missing-import",
		"Add the import a new call needs in the same edit that introduces the " +
			"call, and remove one when its last use goes."},
	{"declared and not used", "unused-declaration",
		"Remove a variable in the same edit that removes its last use. Go " +
			"refuses to compile one that is left behind."},
}

// storeLesson records one statement as a project observation.
//
// It is recorded as an observation rather than a rule, with evidence strength
// fixed at none by the domain constructor, because one run failing one way is
// not yet a law about this project -- it is a thing that happened, which is
// exactly what the observation tier is for.
//
// The evidence source is the automated validation run, not the agent. A refusal
// is something the platform observed; the agent's own account of it would be
// self-report, which UpsertExtractedMemoryFact refuses outright and should.
func (execution *AgentExecution) storeLesson(
	ctx context.Context,
	scope agentScope,
	seed string,
	statement string,
) {
	content, err := domain.NewObservationHypothesisContent(
		scope.repositoryID, statement)
	if err != nil {
		return
	}
	_, _ = execution.repositories.UpsertExtractedMemoryFact(ctx,
		storage.UpsertExtractedMemoryFact{
			ProjectID: scope.projectID,
			Content: domain.MemoryArtifactContent{
				Kind:                  domain.MemoryArtifactKindObservationHypothesis,
				ObservationHypothesis: &content,
			},
			DeterministicSeed: seed,
			Evidence: &storage.UpsertExtractedMemoryFactEvidence{
				Source: domain.EvidenceSourceKindAutomatedValidationRun,
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

// rankLessonsForRequirement puts the lessons most likely to matter to this
// request first.
//
// The match is deliberately fuzzy and deliberately cheap: shared meaningful
// words between the lesson and the requirement. A run asked to parse JSON
// should be shown what went wrong the last time something parsed JSON here
// before it is shown a lesson about doc comments, and neither this nor any
// available index can do better than word overlap without a model call, which
// is not worth making to sort a list of eight.
//
// Recency breaks ties rather than driving the order. Newest-first alone put
// whatever failed most recently in front of a run it may have nothing to do
// with, and a list a run learns to skip is worse than no list, because it
// costs context on every attempt and teaches the run to ignore the section.
//
// Lessons that share no words are kept, at the back. Several are general
// enough to apply to anything -- build before you claim to be finished, never
// discard an error -- and dropping them for scoring zero would lose exactly
// the ones that are always true.
func rankLessonsForRequirement(
	lessons []storage.ProjectLesson, requirement string,
) []storage.ProjectLesson {
	wanted := meaningfulWords(requirement)
	if len(wanted) == 0 || len(lessons) < 2 {
		return lessons
	}
	type scored struct {
		lesson storage.ProjectLesson
		score  int
		order  int
	}
	ranked := make([]scored, 0, len(lessons))
	for index, lesson := range lessons {
		overlap := 0
		for word := range meaningfulWords(lesson.Statement) {
			if wanted[word] {
				overlap++
			}
		}
		ranked = append(ranked, scored{lesson: lesson, score: overlap, order: index})
	}
	sort.SliceStable(ranked, func(first, second int) bool {
		if ranked[first].score != ranked[second].score {
			return ranked[first].score > ranked[second].score
		}
		return ranked[first].order < ranked[second].order
	})
	out := make([]storage.ProjectLesson, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, item.lesson)
	}
	return out
}

// meaningfulWords reduces text to the words worth matching on.
//
// Short words and the handful that appear in every requirement carry no
// signal, and counting them would score every lesson equally, which is the
// same as not ranking at all.
func meaningfulWords(text string) map[string]bool {
	skip := map[string]bool{
		"the": true, "and": true, "that": true, "this": true, "with": true,
		"from": true, "into": true, "for": true, "not": true, "its": true,
		"which": true, "when": true, "what": true, "each": true, "every": true,
		"than": true, "then": true, "them": true, "they": true, "have": true,
		"has": true, "was": true, "are": true, "one": true, "run": true,
		"code": true, "test": true, "tests": true, "write": true,
	}
	words := map[string]bool{}
	for _, raw := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if len(raw) < 4 || skip[raw] {
			continue
		}
		words[raw] = true
	}
	return words
}
