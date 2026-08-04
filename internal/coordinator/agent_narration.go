package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/storage"
)

// narratingExecutor runs a tool and publishes what it did.
//
// The publication happens around the real call rather than instead of it: the
// interface shows a tool starting, then the same tool's actual result. Nothing
// here decides anything about the tool.
type narratingExecutor struct {
	inner     agentloop.ToolExecutor
	execution *AgentExecution
	scope     agentScope
	ctx       context.Context
	// journal knows which plan step each tool request was made for. The
	// executor's own request deliberately carries no planning vocabulary, so
	// the journal is the only place that sees both.
	journal *agentToolJournal
	// ranValidation records whether the run ever reached its test step. A run
	// that never ran the tests and a run whose tests passed are different
	// facts, and reporting them the same way is how an unchecked program comes
	// to read as a verified one.
	ranValidation bool
	// validationFailed is the verdict of the most recent validation run, not a
	// memory of every failure before it.
	validationFailed bool
	// filesChangedSinceValidation records whether anything was written after
	// the last test run, which makes that run's verdict stale.
	//
	// Without this the verification gate read the most recent validation
	// regardless of what happened next, so a run that tested, then edited, and
	// then stopped reported "the project's own test command ran and passed" —
	// about code that no longer existed. Observed on ladder rung 2: the gate
	// said the tests passed while the worktree failed to build with "undefined:
	// run", and two later stages that re-ran the suite themselves contradicted
	// it in the same ledger.
	filesChangedSinceValidation bool
	// lastTestFingerprint identifies the produced work as it stood the last
	// time the suite was run, and lastTestOutcome what that run said.
	//
	// Running the same command against byte-identical files asks a question
	// already answered, and the answer costs whatever the suite costs plus a
	// turn of the model's attention. Ladder rung 3 ran the same failing test
	// three times in a row without an edit between them.
	lastTestFingerprint string
	// writesSinceLastTest counts the writes that landed since the suite
	// last ran, so a suite answered from that run can say whether nothing
	// was written or whether what was written cancelled itself out.
	writesSinceLastTest int
	// unchangedTestsInARow counts suite calls answered from the last run with
	// nothing written between them. Reset by any write and by a real run,
	// because either makes the next call worth answering.
	unchangedTestsInARow int
	lastTestOutput       string
	lastTestFailed       bool
	// worktree is where the produced work lives, needed to fingerprint it.
	worktree string
	// wholeFileWrites counts how many times each path has been rewritten
	// wholesale this attempt.
	//
	// The first is how a file comes into existence. The second is a rewrite of
	// something that already exists, which is what apply-patch is for, and the
	// eleventh is a run that has stopped editing and started guessing. Ladder
	// rung 6 wrote whole files eleven and thirteen times in single attempts.
	wholeFileWrites map[string]int
	// permitted is what this attempt is allowed to change. Set by the attempt
	// loop from the gate that sent the work back, and cleared to editAnything
	// for an ordinary round.
	permitted editScope
	// lastFailure is the output of the most recent tool that did not succeed.
	// It is what the next attempt is shown: an agent told only that its tests
	// failed will guess at why, and the guess is usually a second failure.
	lastFailure string
}

func (narrator *narratingExecutor) ExecuteTool(
	ctx context.Context,
	request executor.AuthorizedToolRequest,
) (executor.ToolResult, error) {
	name := string(request.Request.Name)
	detail := toolDetail(request.Request)
	// One execution identity for one tool call. Started and completed were
	// given different identities, so the completion referred to an execution
	// nothing had ever seen begin, and the projection that pairs them could not
	// be built — which is what left the interface unable to connect at all.
	executionID := request.Request.ID
	if strings.TrimSpace(executionID) == "" {
		executionID = name + ":" + detail
	}
	// The state must be one the command-execution vocabulary declares. It was
	// published as "started", which is not one of them, and an invalid state
	// made the whole session snapshot unusable: the client could not build a
	// projection, so it never connected, and the interface reported
	// "Disconnected" while the coordinator was running an agent perfectly well.
	narrator.execution.publishTool(narrator.ctx, narrator.scope,
		events.KindToolStarted, executionID, name,
		string(domain.CommandExecutionStateRunning), detail)

	// Whether the produced work has moved since the last test run, read before
	// the tool runs so the answer is about the tree the tool is about to see.
	unchanged := false
	if executor.ToolName(name) == executor.ToolTest && narrator.worktree != "" {
		if fingerprint := producedTreeDigest(narrator.worktree); fingerprint != "" {
			unchanged = fingerprint == narrator.lastTestFingerprint
		}
	}
	// The tree as it stands before a write, so a write that changes nothing can
	// be told from one that does.
	//
	// Both write tools, because a patch that changes nothing is the same event
	// as a rewrite that changes nothing and reads to the run as the same
	// progress. This checked apply-edit alone, so a run that had moved to
	// patching — which is every run after its first attempt — was no longer
	// told when its writes did nothing.
	beforeWrite := ""
	if isWriteTool(executor.ToolName(name)) && narrator.worktree != "" {
		beforeWrite = producedTreeDigest(narrator.worktree)
	}

	// A suite run against bytes the suite has already seen is answered from the
	// last run rather than performed again.
	//
	// The result cannot differ — the files are identical — so executing it buys
	// nothing and costs whatever the suite costs. The note already told the run
	// so, but only after paying for it.
	//
	// This protection existed before as a withheld tool: the suite was not
	// offered when nothing had changed. Making a completed check callable again
	// removed it, which was necessary — a run that tests, patches and must test
	// again had no way to — and reintroduced the waste it had been guarding.
	// Ladder rung 4 on 2026-08-03 ran the suite twenty-five times in one run
	// and twelve of those were against unchanged files.
	//
	// The round is still spent, because the call has already been made by the
	// time it arrives here. What is saved is the execution, and what is gained
	// is that the answer is now instant rather than late.
	if unchanged && narrator.lastTestOutput != "" {
		narrator.unchangedTestsInARow++
		// Answering the same question a third time is not answering it.
		//
		// The note already says the files have not moved and says not to run the
		// suite again until one does. A run that asks anyway has not read it,
		// and handing it the same output once more — labelled succeeded, or
		// failed with the same failure — is the pipeline agreeing that the call
		// was worth making. Rung 16 on 2026-08-04 spent 40 of its 73 rounds
		// this way across 14 attempts, writing six times in all, and exhausted
		// its whole attempt budget without ever reaching the compile error its
		// last real suite run had reported.
		//
		// Refused rather than withheld. Withholding the tool for the attempt was
		// how this was once handled and it is worse: an attempt is sent back
		// precisely to change files, and the moment its first patch lands the
		// tool that would say whether the patch held is already gone. A refusal
		// costs one round, says exactly what would make the call worth making,
		// and the very next write makes it callable again.
		if narrator.unchangedTestsInARow > maximumUnchangedSuiteAnswers {
			why := "The produced files have not changed since the suite last " +
				"ran, and this is the " +
				ordinal(narrator.unchangedTestsInARow) +
				" time in a row you have asked for it. The answer cannot " +
				"differ until a file does, so it is not being given again. " +
				"Write the change first — the last result is above, in the " +
				"round that ran it — and then run the suite."
			tracef("tool", "%-12s %-10s %s", name, "refused",
				"asked for an unchanged suite "+
					counted(narrator.unchangedTestsInARow, "time")+" in a row")
			narrator.execution.publishTool(narrator.ctx, narrator.scope,
				events.KindToolCompleted, executionID, name,
				string(domain.CommandExecutionStateFailed),
				detail+" — refused: the files have not changed")
			narrator.lastFailure = why
			return narrator.refuse(request, why), nil
		}
		state := domain.CommandExecutionStateSucceeded
		if narrator.lastTestFailed {
			state = domain.CommandExecutionStateFailed
		}
		tracef("tool", "%-12s %-10s %s", name, "unchanged",
			"answered from the last run; the suite was not executed again")
		narrator.execution.publishTool(narrator.ctx, narrator.scope,
			events.KindToolCompleted, executionID, name, string(state),
			detail+" — answered from the last run")
		result := executor.ToolResult{
			RequestID:     request.Request.ID,
			SchemaVersion: executor.ToolSchemaVersion,
			State:         "succeeded",
			StdoutRedacted: strings.TrimSpace(narrator.lastTestOutput) +
				unchangedTestNote(narrator.writesSinceLastTest),
		}
		if narrator.lastTestFailed {
			result.State = "failed"
			result.ExitCode = 1
			narrator.lastFailure = strings.TrimSpace(result.StdoutRedacted)
		}
		if sources := narrator.producedSourcesNow(); sources != "" {
			result.StdoutRedacted += sources
		}
		return result, nil
	}

	// A wholesale rewrite of a file that already exists is refused after the
	// first, and pointed at the tool for the job.
	//
	// Not on the first: that is how a file is created, and a run that has just
	// written main.go has done the right thing. What is refused is the habit of
	// re-emitting two thousand bytes to move ten, which costs the tokens, risks
	// every line it touches, and is the churn this whole patch tool exists to
	// stop.
	if executor.ToolName(name) == executor.ToolApplyEdit &&
		narrator.worktree != "" {
		path := toolArgument(request.Request, "path")
		if narrator.wholeFileWrites == nil {
			narrator.wholeFileWrites = map[string]int{}
		}
		narrator.wholeFileWrites[path]++
		if narrator.wholeFileWrites[path] > 1 {
			why := "You have already written " + path + " whole this attempt. " +
				"Rewriting it again re-sends every line to change a few, and " +
				"every line it re-sends is a chance to drop something that " +
				"was working. Use apply-patch: \"*** Update File: " + path +
				"\", then \"@@\", the lines to remove prefixed \"-\", the " +
				"lines to add prefixed \"+\", and enough unprefixed context " +
				"around them to say where the change goes. Read the file " +
				"first if you need the exact text."
			tracef("tool", "%-12s %-10s %s", name, "rewrite",
				"already written whole this attempt: "+path)
			narrator.execution.publishTool(narrator.ctx, narrator.scope,
				events.KindToolCompleted, executionID, name,
				string(domain.CommandExecutionStateFailed),
				detail+" — refused: already written whole this attempt")
			narrator.lastFailure = why
			return narrator.refuse(request, why), nil
		}
	}

	// A write outside this round's scope is refused before it lands.
	//
	// The instruction already asks for it — "add the missing test first",
	// "these functions have no doc comment" — and an instruction is a request.
	// Rung 6 was asked for a comment on main and rewrote the evaluator's error
	// semantics, breaking a program that had been correct. Refusing at the
	// write tells the run immediately, with the working program still intact,
	// instead of three gates later with it gone.
	if (executor.ToolName(name) == executor.ToolApplyEdit ||
		executor.ToolName(name) == executor.ToolApplyPatch) &&
		narrator.permitted != editAnything && narrator.worktree != "" {
		path := toolArgument(request.Request, "path")
		content := []byte(toolArgument(request.Request, "content"))
		if executor.ToolName(name) == executor.ToolApplyPatch {
			content = []byte(toolArgument(request.Request, "patch"))
			if why := narrator.patchStaysSmall(request.Request, path); why != "" {
				tracef("tool", "%-12s %-10s %s", name, "too-broad",
					traceOneLine(why, 110))
				narrator.execution.publishTool(narrator.ctx, narrator.scope,
					events.KindToolCompleted, executionID, name,
					string(domain.CommandExecutionStateFailed),
					detail+" — refused: broader than this round allows")
				narrator.lastFailure = why
				return narrator.refuse(request, why), nil
			}
		}
		if allowed, why := narrator.permitted.permits(
			narrator.worktree, path, content,
		); !allowed {
			tracef("tool", "%-12s %-10s %s", name, "out-of-scope",
				traceOneLine(why, 110))
			narrator.execution.publishTool(narrator.ctx, narrator.scope,
				events.KindToolCompleted, executionID, name,
				string(domain.CommandExecutionStateFailed),
				detail+" — refused: outside this round's scope")
			narrator.lastFailure = why
			return narrator.refuse(request, why), nil
		}
	}

	result, err := narrator.inner.ExecuteTool(ctx, request)
	if err != nil {
		narrator.execution.publishTool(narrator.ctx, narrator.scope,
			events.KindToolCompleted, executionID, name,
			string(domain.CommandExecutionStateFailed),
			detail+" — refused: "+err.Error())
		narrator.execution.recordToolLesson(
			narrator.ctx, narrator.scope, err.Error())
		return result, err
	}
	summary := detail
	if outcome := strings.TrimSpace(
		succeedingLineOf(result.StdoutRedacted),
	); outcome != "" {
		summary += " — " + outcome
	}
	if result.State != "succeeded" {
		// The reason a tool failed is not reliably its first line. "go test
		// ./..." prints a benign "[no test files]" for the root package before
		// it prints the package that actually broke, so every failure in this
		// timeline read "failed: [no test files]" — a sentence that contradicts
		// itself and names nothing. What a reader needs is the first line that
		// says something went wrong.
		if failure := failingLineOf(
			result.StderrRedacted + "\n" + result.StdoutRedacted,
		); failure != "" {
			summary = detail + " — " + failure
		}
	}
	// The executor already speaks this vocabulary: succeeded, failed,
	// cancelled, outcome-unknown. Anything else is refused rather than passed
	// through, so a new executor state cannot silently break the projection.
	completed := domain.CommandExecutionState(result.State)
	if !completed.IsValid() {
		completed = domain.CommandExecutionStateOutcomeUnknown
	}
	// A second identical run of the same suite against byte-identical files
	// answers a question already answered, and the model cannot see that for
	// itself. Ladder rung 3 ran the same failing test three times in a row with
	// no edit between them.
	if unchanged {
		tracef("tool", "%-12s %-10s %s", name, "unchanged",
			"same command, same files as the last run")
		result.StdoutRedacted = strings.TrimSpace(result.StdoutRedacted) +
			unchangedTestNote(narrator.writesSinceLastTest)
	}
	// Every patch is answered with the file as it now stands.
	//
	// The refusal already says to read the file and copy the lines from it,
	// which the run cannot do: the file is put in front of it once, when the
	// attempt starts, and every patch that lands after that makes the copy it
	// is holding a description of a file that no longer exists. So it retypes
	// the lines from memory, misses again, and spends another round.
	//
	// Ladder rung 3 on 2026-08-03: thirty of forty patch calls failed, and at
	// roughly five seconds of model latency each they were most of the run.
	// The whole file is a thousand-odd bytes against a sixty-thousand-byte
	// result ceiling, so answering with it is cheap in exactly the place that
	// is currently expensive.
	// A suite run answers with the code it ran against.
	//
	// The produced files reach the model on a patch result and nowhere else, so
	// a round that ran the tests instead put the model back on the snapshot it
	// was given when the attempt began. Ladder rung 4 on 2026-08-03 measured
	// the cost exactly: a patch written straight after a test call failed 15
	// times out of 21, while a patch written after another patch failed twice
	// out of seven. The run interleaves the two constantly, so most of its
	// patches were written against a file that had already moved.
	if executor.ToolName(name) == executor.ToolTest &&
		completed == domain.CommandExecutionStateSucceeded {
		result.StdoutRedacted = strings.TrimSpace(result.StdoutRedacted) +
			narrator.sourcesIfTheyMoved()
	}
	if executor.ToolName(name) == executor.ToolApplyPatch {
		if current := narrator.currentContentOf(
			toolArgument(request.Request, "path")); current != "" {
			if completed == domain.CommandExecutionStateSucceeded {
				// A patch that landed changed the file, so the copy the run is
				// holding is now wrong in exactly the way that makes the next
				// hunk miss. Answering a success with the new text is what
				// keeps a second patch in the same attempt writable at all:
				// the file is offered once, when the attempt begins, and
				// nothing else refreshes it.
				result.StdoutRedacted = strings.TrimSpace(
					result.StdoutRedacted) + current
			} else {
				result.StderrRedacted = strings.TrimSpace(
					result.StderrRedacted) + current
			}
		}
	}
	if completed != domain.CommandExecutionStateSucceeded {
		narrator.lastFailure = strings.TrimSpace(
			result.StdoutRedacted + "\n" + result.StderrRedacted)
		// Written down here rather than at the gate, because the most repeated
		// mistakes never reach one: a markdown-fenced file is refused by the
		// write, a caller left behind by a signature change is refused by the
		// compiler, and the run fixes both and arrives at the gate clean. The
		// lesson is real, and nothing downstream would ever have seen it.
		narrator.execution.recordToolLesson(
			narrator.ctx, narrator.scope, narrator.lastFailure)
	}
	narrator.execution.publishTool(narrator.ctx, narrator.scope,
		events.KindToolCompleted, executionID, name, string(completed), summary)
	if executor.ToolName(name) == executor.ToolTest {
		// Whether the tests passed is read from this run of them, not from
		// whether anything has ever failed. lastFailure is sticky by design —
		// the next attempt needs to be told what broke — so judging the run by
		// it declared a run that failed, refined, and then passed to be
		// unverified, which is the exact case refinement exists to produce.
		narrator.ranValidation = true
		narrator.validationFailed =
			completed != domain.CommandExecutionStateSucceeded
		narrator.filesChangedSinceValidation = false
		narrator.writesSinceLastTest = 0
		narrator.unchangedTestsInARow = 0
		narrator.lastTestFingerprint = producedTreeDigest(narrator.worktree)
		narrator.lastTestOutput = strings.TrimSpace(
			result.StdoutRedacted + "\n" + result.StderrRedacted)
		narrator.lastTestFailed =
			completed != domain.CommandExecutionStateSucceeded
	}
	// A write of the bytes that were already there is not a change.
	//
	// It costs a suite run, because it marks the last validation stale; it
	// costs the model a turn; and it reads to the run as progress. Ladder rung
	// 5 wrote an identical main.go twice in one attempt. Saying so is the
	// remedy — the model believes it edited something, and the only way it
	// finds out otherwise is an identical failure two steps later.
	if isWriteTool(executor.ToolName(name)) &&
		completed == domain.CommandExecutionStateSucceeded &&
		beforeWrite != "" && beforeWrite == producedTreeDigest(narrator.worktree) {
		tracef("tool", "%-12s %-10s %s", name, "no-op",
			"the file already held exactly these bytes")
		result.StdoutRedacted = strings.TrimSpace(result.StdoutRedacted) +
			noOpWriteNote()
		narrator.execution.publishTool(narrator.ctx, narrator.scope,
			events.KindToolCompleted, executionID, name, string(completed),
			summary+" — changed nothing")
		return result, nil
	}
	if isWriteTool(executor.ToolName(name)) &&
		completed == domain.CommandExecutionStateSucceeded {
		// A successful write supersedes whatever the last test run judged.
		// A failed one changed nothing, so it leaves the verdict standing.
		narrator.filesChangedSinceValidation = true
		// Counted so that a suite answered from the last run can tell the two
		// reasons apart: nothing was written at all, or things were written and
		// they cancelled each other out.
		narrator.writesSinceLastTest++
		narrator.unchangedTestsInARow = 0
	}
	operationNode := narrator.recordInGraph(request, name, detail,
		completed == domain.CommandExecutionStateSucceeded)
	narrator.persistProducedFile(request,
		completed == domain.CommandExecutionStateSucceeded, operationNode)
	// Each finished tool is also said out loud in the conversation. The tool
	// events carry the same facts, but nothing renders them yet, and a person
	// watching a run needs to see it move rather than infer that it did.
	tracef("tool", "%-12s %-10s %s", name, completed, traceOneLine(summary, 110))
	if completed != domain.CommandExecutionStateSucceeded &&
		narrator.lastFailure != "" {
		traceBlock("output", "what it reported:", traceOneLine(narrator.lastFailure, 600))
	}
	narrator.execution.say(narrator.ctx, narrator.scope,
		events.KindMessageFinal, toolNarration(name, string(completed), summary))
	return result, nil
}

// toolNarration says what one tool did, in the interface's voice.
func toolNarration(tool, state, summary string) string {
	verb := map[string]string{
		"apply-edit": "Wrote", "test": "Ran tests", "read-file": "Read",
		"list-directory": "Listed",
	}[tool]
	if verb == "" {
		verb = "Ran " + tool
	}
	if state != "succeeded" {
		return verb + " — " + state + ": " + summary
	}
	return verb + " " + summary
}

// toolDetail names what a call is acting on, for a person reading a timeline.
func toolDetail(request executor.ToolRequest) string {
	values := map[string]string{}
	for _, argument := range request.Arguments {
		values[argument.Name] = argument.Value
	}
	if path := strings.TrimSpace(values["path"]); path != "" {
		return path
	}
	command := strings.TrimSpace(strings.Join([]string{
		values["executable"], values["arg1"], values["arg2"],
	}, " "))
	if command != "" {
		return command
	}
	return string(request.Name)
}

// firstLineOf bounds output to something a timeline row can hold.
func firstLineOf(text string) string {
	trimmed := strings.TrimSpace(text)
	if index := strings.IndexByte(trimmed, '\n'); index >= 0 {
		trimmed = trimmed[:index]
	}
	const bound = 200
	if len(trimmed) > bound {
		trimmed = trimmed[:bound] + "…"
	}
	return trimmed
}

// say records one assistant message and announces it.
//
// The message is appended to the thread as well as published, because the
// conversation is a list of messages and a session event is not one. Emitting
// only the event left a run that had written files, run tests, and reported
// what it did showing "0 messages" — the work had happened and the record of it
// existed nowhere a person could read.
func (execution *AgentExecution) say(
	ctx context.Context,
	scope agentScope,
	kind events.Kind,
	body string,
) {
	messageID, err := domain.NewMessageID()
	if err != nil {
		return
	}
	if _, appendErr := execution.repositories.AppendMessage(ctx, storage.AppendMessage{
		ID: messageID, ThreadID: scope.threadID,
		Role: storage.MessageRoleAssistant, BodyRedacted: body,
		IdempotencyKey: "agent:" + messageID.String(),
	}); appendErr != nil {
		return
	}
	execution.publish(ctx, scope, events.NewSessionEvent{
		Kind: kind, PayloadVersion: 1,
		Payload: events.Payload{MessageFinal: &events.MessageFinal{
			MessageID: messageID, Role: "assistant", RedactedBody: body,
		}},
	})
}

// publishPlan states the plan before any work starts.
func (execution *AgentExecution) publishPlan(
	ctx context.Context,
	scope agentScope,
	steps []agentloop.PlanStep,
) {
	summaries := make([]string, 0, len(steps))
	for _, step := range steps {
		summaries = append(summaries, step.SummaryRedacted)
	}
	revision := scope.revisions.next(&scope.revisions.plan)
	execution.publish(ctx, scope, events.NewSessionEvent{
		Kind: events.KindPlanCreated, PayloadVersion: 1, Revision: revision,
		Payload: events.Payload{Plan: &events.Plan{
			Revision: revision, RedactedSummary: strings.Join(summaries, " · "),
		}},
	})
}

// publishTool states one tool lifecycle transition.
func (execution *AgentExecution) publishTool(
	ctx context.Context,
	scope agentScope,
	kind events.Kind,
	executionID, tool, state, summary string,
) {
	execution.publish(ctx, scope, events.NewSessionEvent{
		Kind: kind, PayloadVersion: 1,
		Revision: scope.revisions.next(&scope.revisions.tool),
		Payload: events.Payload{Tool: &events.Tool{
			ExecutionID: executionID, CommandName: tool,
			State: state, RedactedSummary: summary,
		}},
	})
}

// publishValidation states whether the work is believed done.
func (execution *AgentExecution) publishValidation(
	ctx context.Context,
	scope agentScope,
	outcome agentloop.LoopOutcome,
) {
	validationID, err := domain.NewValidationID()
	if err != nil {
		return
	}
	state := domain.ValidationStatePassed
	if outcome.Kind != agentloop.OutcomeImplementationComplete &&
		outcome.Kind != agentloop.OutcomeValidationComplete {
		state = domain.ValidationStatePending
	}
	execution.publish(ctx, scope, events.NewSessionEvent{
		Kind: events.KindValidationUpdated, PayloadVersion: 1,
		Revision: scope.revisions.next(&scope.revisions.validation),
		Payload: events.Payload{Validation: &events.Validation{
			ValidationID: validationID, State: state, Required: true,
			RedactedSummary: string(outcome.Kind) + ": " + outcome.Reason,
		}},
	})
}

// publish records and broadcasts one event.
//
// A failure here is swallowed on purpose: narration must never be able to stop
// the work it is describing. The work's own outcome is reported separately.
func (execution *AgentExecution) publish(
	ctx context.Context,
	scope agentScope,
	input events.NewSessionEvent,
) {
	input.SessionID = scope.sessionID
	input.ThreadID = scope.threadID
	taskID := scope.taskID
	input.TaskID = &taskID
	_, _ = execution.repositories.PersistAndPublishSessionEvent(
		ctx, input, execution.events)
}

// agentPlanSteps turns a requirement into the steps a run executes.
//
// The files come from the requirement when it names them and from a
// conventional default when it does not. A step is completed by one tool call,
// so each file gets its own: a single step covering two files leaves the second
// write with nowhere to bind.
func agentPlanSteps(worktree, requirement string) []agentloop.PlanStep {
	return agentPlanStepsForFiles(worktree, requirement, filesNamedIn(requirement))
}

// agentPlanStepsForFiles is the same builder over a layout somebody else chose.
//
// Split out so a planner that named the files can produce steps through exactly
// the code path the parser's files go through — the step kinds, the summaries,
// the verification step and its bounds are one implementation, not two that
// have to be kept agreeing.
func agentPlanStepsForFiles(
	worktree, requirement string,
	files []string,
) []agentloop.PlanStep {
	steps := make([]agentloop.PlanStep, 0, len(files)+1)
	for index, file := range files {
		// How the file is going to be written, from whether it is already
		// there. The plan is rebuilt every attempt precisely so this can change
		// between them: attempt one creates main.go, attempt three adjusts the
		// main.go attempt one wrote.
		kind := writeStepKindFor(worktree, file)
		steps = append(steps, agentloop.PlanStep{
			ID: fmt.Sprintf("edit-%d", index+1), Kind: kind,
			State: agentloop.StepPending,
			// A bounded summary of the requirement travels with every step, not
			// the requirement itself (PIPE-019b). "Write cmd/greet/main.go" alone
			// tells the model the path and nothing about the program — that
			// produced a hello-world stub that satisfied the plan exactly as
			// written — so the requirement's own point still needs to travel.
			// requirementSummary is what makes that safe: a plan step's detail is
			// a label read by a person and validated as one (storage.AgentPlan.
			// Validate refuses leading/trailing whitespace and caps it at 4096
			// bytes), not a copy of an arbitrarily long, model-authored acceptance
			// block, which is mandatory on every requirement since PIPE-019 and
			// reliably ends in a trailing newline.
			SummaryRedacted: "Write " + file + " — " + requirementSummary(requirement),
			MaterialEdit:    true, ValidationRequired: true,
			ExpectedFiles:   []string{file},
			CompletionTools: writeToolsFor(kind),
		})
	}
	return append(steps, agentloop.PlanStep{
		ID: "verify", Kind: agentloop.StepKindTest,
		State:              agentloop.StepPending,
		SummaryRedacted:    "Run the tests: executable go, arg1 test, arg2 ./...",
		ValidationRequired: true,
		CompletionTools:    []executor.ToolName{executor.ToolTest},
	})
}

// requirementSummary bounds a requirement to a short label for a plan step's
// detail (PIPE-019b).
//
// Embedding the requirement verbatim was wrong independent of whitespace: a
// plan step's detail is a label, and a requirement carrying a forty-line
// acceptance block would produce an unreadable step even if it happened to
// trim cleanly. Trimming the symptom — the trailing whitespace an appended
// <<<ACCEPTANCE ... >>> block leaves behind — would satisfy
// storage.AgentPlan.Validate and still leave the underlying defect in place.
//
// So this drops the acceptance block first, since it is a machine-checked
// example for the acceptance-oracle and end-to-end stages rather than part of
// what the step is *for*, and then takes the first line of what remains,
// capped the same way toolDetail's own output is bounded for a timeline row
// elsewhere in this file: a fixed character bound with an ellipsis when it
// was cut. A requirement's first line is normally its own summary — the
// acceptance block and any elaboration follow it — so this rarely drops
// anything a person reading the step actually needs; when a requirement is
// nothing but an acceptance block, a fixed fallback label is used instead of
// an empty one, which the validator refuses on its own.
func requirementSummary(requirement string) string {
	body := requirement
	if index := strings.Index(body, acceptanceOpen); index >= 0 {
		body = body[:index]
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "as specified in the task requirement"
	}
	if index := strings.IndexByte(body, '\n'); index >= 0 {
		body = strings.TrimSpace(body[:index])
	}
	const bound = 200
	if len(body) > bound {
		body = strings.TrimSpace(body[:bound]) + "…"
	}
	return body
}

// filesNamedIn reads the paths a requirement mentions.
//
// A path in the requirement is the person saying where the work goes, which is
// better information than anything this could infer. When they name none, two
// conventional files are offered so a run still has somewhere to write; the
// model chooses what to put in them.
func filesNamedIn(requirement string) []string {
	// The product already has one reader of requirements, and the plan the
	// store validates is built from it. This used to run a second, narrower
	// rule of its own that skipped any name without a directory in it: asked
	// for "greet.go", the analysis expected the file and the steps did not
	// mention it, so the plan named a file no step covered and was refused.
	// One extractor, so the steps and the plan cannot disagree.
	if analysis, err := storage.AnalyzeTaskRequirement(requirement); err == nil &&
		len(analysis.ExplicitFiles) > 0 {
		return withTestsFor(analysis.ExplicitFiles)
	}
	// When the request names no file, two conventional ones are offered so a
	// run still has somewhere to write; the model chooses what to put in them.
	return []string{"cmd/generated/main.go", "cmd/generated/main_test.go"}
}

// agentApprovedTools is what a run may do.
// agentApprovedTools is what a run may do this round.
//
// offerTest is whether running the suite is worth offering at all. A round that
// has changed nothing since the last test run would learn nothing from another,
// and the model cannot see that: it re-ran the same command against the same
// bytes repeatedly, and each run looked to it like a step forward. Withholding
// the tool is more honest than letting it be called and then explaining
// afterwards that the answer was already known.
func agentApprovedTools(offerTest bool) []agentloop.ApprovedTool {
	catalog := map[executor.ToolName]executor.ToolDescriptor{}
	for _, descriptor := range executor.ToolCatalog() {
		catalog[descriptor.Name] = descriptor
	}
	tools := []agentloop.ApprovedTool{
		{
			Descriptor: catalog[executor.ToolApplyEdit],
			Arguments: []agentloop.ToolArgumentDefinition{
				{Name: "path", Required: true},
				{Name: "content", Required: true, MaxBytes: producedSourceMaxBytes},
			},
			DefaultTimeout:    30 * time.Second,
			MaterialEdit:      true,
			CreatesCheckpoint: true,
		},
		{
			// The default way to change an existing file.
			//
			// A whole-file write to move ten lines replaces two or three
			// thousand bytes, and every byte it replaces is a chance to drop a
			// function, drift a signature, or reword an acceptance-sensitive
			// literal by accident. Ladder rungs 5 and 6 spent most of their
			// attempts undoing exactly that.
			Descriptor: catalog[executor.ToolApplyPatch],
			Arguments: []agentloop.ToolArgumentDefinition{
				{Name: "path", Required: true},
				{Name: "patch", Required: true, MaxBytes: producedSourceMaxBytes},
			},
			DefaultTimeout:    30 * time.Second,
			MaterialEdit:      true,
			CreatesCheckpoint: true,
		},
	}
	if offerTest {
		tools = append(tools, agentloop.ApprovedTool{
			Descriptor: catalog[executor.ToolTest],
			Arguments: []agentloop.ToolArgumentDefinition{
				{Name: "executable", Required: true},
				{Name: "arg1", Required: true},
				{Name: "arg2", Required: true},
			},
			DefaultTimeout: 4 * time.Minute,
		})
	}
	return tools
}

// agentContextItem binds content to its digest.
func agentContextItem(path, content string) agentloop.RepositoryContextItem {
	digest := sha256.Sum256([]byte(content))
	return agentloop.RepositoryContextItem{
		Path: path, ContentRedacted: content,
		ContentSHA256: hex.EncodeToString(digest[:]),
	}
}

// agentNoDurableJournal accepts the loop's durable ports.
//
// The session journal is what this product shows and what people read, and it
// is written by the narration above. These ports write a second, parallel
// record that nothing renders yet; they are accepted rather than wired so a run
// is not blocked on a surface that does not exist.
type agentNoDurableJournal struct{}

func (agentNoDurableJournal) PersistToolStart(
	context.Context, agentloop.ToolStartRecord,
) error {
	return nil
}

func (agentNoDurableJournal) PersistToolResult(
	context.Context, agentloop.ToolResultRecord,
) error {
	return nil
}

func (agentNoDurableJournal) PersistPlanStepTransition(
	context.Context, agentloop.PlanStepTransition,
) error {
	return nil
}

func (agentNoDurableJournal) CreateCheckpoint(
	context.Context, agentloop.CheckpointRequest,
) error {
	return nil
}

func (agentNoDurableJournal) CreatePlanApprovedCheckpoint(
	context.Context, agentloop.PlanApprovedCheckpointRequest,
) error {
	return nil
}

// agentActiveControl keeps a run active.
//
// Pause and cancel are real controls with a real durable path, and binding them
// here is the next step. Until then a run is honestly unpausable rather than
// appearing to accept a pause it would ignore.
type agentActiveControl struct{}

func (agentActiveControl) ReadControl(
	context.Context, domain.TaskID, domain.RunID,
) (agentloop.ControlState, error) {
	return agentloop.ControlState{
		Disposition: agentloop.ControlActive, BudgetAvailable: true,
		PolicyCurrent: true,
	}, nil
}

func (agentActiveControl) BindActionContext(
	ctx context.Context, _ domain.TaskID, _ domain.RunID, _ agentloop.ActionDescriptor,
) (context.Context, context.CancelFunc, error) {
	inner, cancel := context.WithCancel(ctx)
	return inner, cancel, nil
}

// recordInGraph draws one finished tool call into the task diagram and
// returns the operation node it was drawn as, so a caller that later confirms
// a durable side effect of the same tool call — a stored artifact — can point
// at the operation that produced it rather than re-deriving it.
//
// The file edits and the command are the flow of the work, and the diagram is
// derived from what actually ran rather than from a description of it. A
// completed run of the test tool is additionally drawn as a validation
// obligation: the effect node already says a command ran, and this says
// separately whether the thing that command exists to check was satisfied,
// which is a different fact the graph's evidence mode depends on.
func (narrator *narratingExecutor) recordInGraph(
	request executor.AuthorizedToolRequest,
	tool, detail string,
	succeeded bool,
) domain.NodeID {
	recorder := narrator.scope.graph
	if recorder == nil {
		return domain.NodeID{}
	}
	// The step comes from the journal, which recorded it when the loop asked
	// for this tool. The request's own idempotency key was used here, and it
	// names an execution rather than a plan step, so every drawn operation
	// claimed a step no plan had and the store refused the whole diagram.
	attribution, known := narrator.journal.attributionOf(request.Request.ID)
	if !known {
		return domain.NodeID{}
	}
	switch executor.ToolName(tool) {
	// A patch is a file edit and is drawn as one. Falling to the default drew
	// it as a command, so the diagram showed a run issuing commands at a file
	// nothing had ever edited, and persistProducedFile had no operation node
	// to hang the stored artifact from.
	case executor.ToolApplyEdit, executor.ToolApplyPatch:
		return recorder.recordFileEdit(narrator.ctx, attribution.PlanStepID, detail,
			attribution.PlanRevision, succeeded)
	case executor.ToolTest:
		recorder.recordValidation(narrator.ctx, attribution.PlanStepID, detail,
			attribution.PlanRevision, succeeded)
		recorder.recordCommand(narrator.ctx, attribution.PlanStepID, detail,
			attribution.PlanRevision, succeeded)
	default:
		recorder.recordCommand(narrator.ctx, attribution.PlanStepID, detail,
			attribution.PlanRevision, succeeded)
	}
	return domain.NodeID{}
}

// persistProducedFile stores what a successful edit actually wrote.
//
// The worktree is temporary. Without this the database could say a file was
// written and could not say what was in it, so the one thing a person most
// wants after a run — the code — survived only as long as the directory did.
// The content is read back from disk rather than taken from the tool arguments
// so that what is stored is what the file actually holds.
func (narrator *narratingExecutor) persistProducedFile(
	request executor.AuthorizedToolRequest,
	succeeded bool,
	operationNode domain.NodeID,
) {
	// A patch changes a file exactly as much as a write does.
	//
	// Only apply-edit was stored, which was right when a write was the only
	// way to change a file and became wrong the moment the patch tool was
	// offered. Every refinement after the first write left the record holding
	// the first draft: ladder rung 1 on 2026-08-03 produced a correct 544-byte
	// program and stored the 472-byte version it had started from, and the run
	// was failed for it — "the program is on disk but not in the record".
	//
	// Both tools declare the file they change as a required path argument, so
	// the file is read back from disk here either way and what is stored is
	// what the file actually holds, whichever tool put it there.
	tool := executor.ToolName(request.Request.Name)
	if !succeeded ||
		(tool != executor.ToolApplyEdit && tool != executor.ToolApplyPatch) {
		return
	}
	path := toolArgument(request.Request, "path")
	if path == "" || narrator.scope.worktree == "" {
		return
	}
	content, err := os.ReadFile(
		filepath.Join(narrator.scope.worktree, filepath.FromSlash(path)))
	if err != nil {
		return
	}
	// Produced source goes through the same redaction as everything else this
	// product persists. Usually it passes through untouched; when it does not,
	// the stored artifact is no longer a faithful copy of the file, and that
	// has to be said rather than left for someone to discover by diffing. The
	// kind records which it is, so a reader never has to guess whether the
	// bytes in the record are the bytes that were written.
	sanitized := narrator.execution.redactForStorage(content)
	kind := "generated-source"
	if len(sanitized) != len(content) || string(sanitized) != string(content) {
		kind = "generated-source-redacted"
		narrator.execution.say(narrator.ctx, narrator.scope,
			events.KindMessageFinal, fmt.Sprintf(
				"Stored %s with redactions: the file on disk is %d bytes and "+
					"the stored copy is %d, so the record is not byte-identical "+
					"to what was written.", path, len(content), len(sanitized)))
	}
	taskID := narrator.scope.taskID
	repositoryID := narrator.scope.repositoryID
	if _, err := narrator.execution.repositories.RecordProducedArtifact(
		narrator.ctx, storage.RecordArtifact{
			ProjectID: narrator.scope.projectID, RepositoryID: &repositoryID,
			TaskID: &taskID, Type: kind, Path: path,
			// The pipeline that redacts everything else a run says is applied
			// here too: a produced file is model output written into a
			// repository, and it is not exempt from that just because it
			// compiles.
			Content:      sanitized,
			MediaType:    storage.ArtifactMediaType(path),
			StorageClass: storage.ArtifactPermanentSemantic,
		},
	); err != nil {
		narrator.execution.say(narrator.ctx, narrator.scope,
			events.KindMessageFinal,
			"The file was written but could not be stored for later reading: "+
				err.Error())
		return
	}
	// The artifact is only drawn now, after RecordProducedArtifact has
	// committed: this is the point the stored file genuinely exists, and the
	// operation node it points back at is the same edit narrator.recordInGraph
	// already drew for this exact tool call.
	if recorder := narrator.scope.graph; recorder != nil && !operationNode.IsZero() {
		if attribution, known := narrator.journal.attributionOf(request.Request.ID); known {
			recorder.recordArtifact(narrator.ctx, graph.ArtifactChangedFile,
				operationNode, attribution.PlanStepID, path,
				attribution.PlanRevision, true)
		}
	}
}

// toolArgument reads one named argument from a tool request.
func toolArgument(request executor.ToolRequest, name string) string {
	for _, argument := range request.Arguments {
		if argument.Name == name {
			return argument.Value
		}
	}
	return ""
}

// redactForStorage runs produced content through the shared redaction pipeline.
//
// A file a model wrote into a repository is model output, and it goes through
// the same filter as everything else the product persists. If the pipeline
// cannot process it the content is not stored at all: storing it unfiltered
// would put whatever was in the worktree into the database, and that is the one
// outcome this boundary exists to prevent.
func (execution *AgentExecution) redactForStorage(content []byte) []byte {
	if execution.redactor == nil {
		return nil
	}
	result, err := execution.redactor.Redact(
		redact.BoundaryLogPersistence, string(content))
	if err != nil {
		return nil
	}
	return []byte(result.Text)
}

// withTestsFor adds a test file beside every Go source file the plan names.
//
// The run validates itself by running the repository's tests. When it writes a
// program and no tests, that command finds nothing to run and reports success,
// so the whole safety net passes vacuously: a program that printed the wrong
// answer reached implementation-complete with a green suite behind it, because
// the suite was empty.
//
// Demanding the test alongside the source is what makes the existing gate mean
// something. It also forces the work to have a callable shape — a package with
// nothing but an inlined main has nothing a test can reach — which is the same
// thing the house style asks for and the reason both arrive together.
func withTestsFor(files []string) []string {
	seen := make(map[string]bool, len(files)*2)
	for _, file := range files {
		seen[file] = true
	}
	result := append([]string(nil), files...)
	for _, file := range files {
		companion, wanted := testCompanionOf(file)
		if !wanted || seen[companion] {
			continue
		}
		seen[companion] = true
		result = append(result, companion)
	}
	return result
}

// testCompanionOf names the test file for one source file, if it should have
// one.
//
// Only Go sources get a companion, and a test file is not given a test of its
// own. Everything else — a module file, a document — is left alone, because a
// test beside it would be a file nobody could write.
func testCompanionOf(file string) (string, bool) {
	if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
		return "", false
	}
	return strings.TrimSuffix(file, ".go") + "_test.go", true
}

// failingLineOf finds the line that explains why something failed.
//
// It prefers a line that names a file and position, then one the toolchain
// marks as a failure, and only then falls back to the first line of all. The
// order is deliberate: a compiler's file:line:message is the most actionable
// thing in the output, and it is almost never the first thing printed.
func failingLineOf(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var marked, first string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if first == "" {
			first = trimmed
		}
		// A position within a file: the most useful line there is.
		if strings.Contains(trimmed, ".go:") {
			return boundLine(trimmed)
		}
		if marked == "" && (strings.HasPrefix(trimmed, "--- FAIL") ||
			strings.HasPrefix(trimmed, "FAIL") ||
			strings.HasPrefix(trimmed, "# ") ||
			strings.HasPrefix(trimmed, "panic:")) {
			marked = trimmed
		}
	}
	if marked != "" {
		return boundLine(marked)
	}
	return boundLine(first)
}

// boundLine keeps one line short enough for a timeline row.
func boundLine(line string) string {
	const bound = 200
	if len(line) > bound {
		return line[:bound] + "…"
	}
	return line
}

// succeedingLineOf is the line worth quoting from a command that worked.
//
// It is the mirror of failingLineOf and exists for the same reason. Go prints
// a package's status before it prints the package that matters, so a passing
// `go test ./...` in a module whose root holds no tests summarised as
// "? codeflux.test/workspace [no test files]" — which reads as "nothing ran"
// for a run whose tests had just passed. That sentence was misread as a
// failure twice on 2026-08-03, once by a person reading a timeline and once
// while diagnosing a different defect entirely.
//
// An "ok" line is preferred, then any line that is not a package-has-no-tests
// notice, and the first line is the fallback so a command with nothing else to
// say still says something.
func succeedingLineOf(text string) string {
	var firstOther string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "ok ") || strings.HasPrefix(trimmed, "ok\t") {
			return trimmed
		}
		if strings.HasPrefix(trimmed, "?") {
			continue
		}
		if firstOther == "" {
			firstOther = trimmed
		}
	}
	if firstOther != "" {
		return firstOther
	}
	return strings.TrimSpace(firstLineOf(text))
}

// noOpWriteNote tells a run that the file already held what it just wrote.
//
// The model believes it edited something. Without this the only way it finds
// out otherwise is an identical failure two steps later, which reads as the
// edit not having worked rather than as the edit not having differed.
func noOpWriteNote() string {
	return "\n\n[codeflux] This write changed nothing: the file already held " +
		"exactly these bytes. Nothing has moved, so re-running the tests will " +
		"produce the same result. Read the file and decide what actually needs " +
		"to differ."
}

// unchangedTestNote tells a run that its last edit changed nothing.
//
// It is a fact the model cannot observe: it wrote a file, or believes it did,
// and the suite it just ran saw exactly the bytes the previous run saw. Without
// this the only signal is an identical failure, which reads as bad luck rather
// than as a write that did not land.
//
// Said in the output rather than by refusing to run the tool. An earlier
// version short-circuited the call entirely, which skipped the executor that
// records the tool in the run's journal; the loop then refused the turn and
// ended the run, and ladder rung 3 lost all thirty-seven of its remaining
// stages to a saving of one test run.
func unchangedTestNote(writes int) string {
	note := "\n\n[codeflux] Not one byte of the produced files changed since " +
		"the last time these tests ran, so this result is that result. "
	if writes == 0 {
		return note + "Nothing was written since that run. If you meant to " +
			"change something, the write did not land — check the path and " +
			"the content, and do not run the tests again until a file differs."
	}
	// The writes landed. Sending the run to check its paths and its content
	// would send it looking for a failure that did not happen: rung 9 on
	// 2026-08-03 spent attempt 2 adding a guard, removing it again, adding it
	// back and removing it again, and was told twice that its write had not
	// landed while all four patches had applied cleanly.
	return note + plural(writes, "write") + " landed since that run and the " +
		"files are byte for byte what they were, so they undid each other. " +
		"The failure below is still the current failure. Decide what " +
		"behaviour the failing test is asking for and change that, rather " +
		"than moving the same lines back and forth."
}

// plural renders a small count with its noun, for a sentence a run reads once.
func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return itoaPlan(count) + " " + noun + "s"
}

// isWriteTool is the one place that says which tools change produced files.
//
// It had been spelled out as "== apply-edit" in four places, written when
// apply-edit was the only write tool there was. apply-patch was added and none
// of them were revisited, so the no-op detection, the staleness flag and the
// write counter all stopped seeing the writes that every attempt after the
// first actually makes.
func isWriteTool(name executor.ToolName) bool {
	return name == executor.ToolApplyEdit || name == executor.ToolApplyPatch
}

// maximumUnchangedSuiteAnswers is how many times one unchanged suite result is
// handed back before the call is refused instead.
//
// Two. The first is the run checking, which is reasonable — it may not have
// known. The second is the note being read and acted on anyway, which happens.
// The third is a loop, and a loop here is expensive: every round in it costs a
// model turn and buys nothing, because the answer is a copy of an answer the
// run already has.
const maximumUnchangedSuiteAnswers = 2

// ordinal renders a small count as a place, for a sentence a run reads once.
func ordinal(count int) string {
	switch count {
	case 1:
		return "first"
	case 2:
		return "second"
	case 3:
		return "third"
	case 4:
		return "fourth"
	default:
		return itoaPlan(count) + "th"
	}
}

// maximumPatchEchoBytes bounds the file a failed patch is answered with.
//
// Generated programs are small and the result ceiling is sixty thousand bytes,
// so this never binds in practice. It exists so that a large file cannot turn
// one mistyped hunk into a result that crowds out everything else the round
// needs to read.
const maximumPatchEchoBytes = 16000

// currentContentOf reads a produced file as it stands right now, for answering
// a failed patch with the text its hunks have to match.
//
// Returns empty for anything it cannot read or resolve inside the worktree,
// and for a file larger than the echo bound: a patch failure that also cannot
// show the file is still a patch failure, and saying less is better than
// saying something that is not the file.
func (narrator *narratingExecutor) currentContentOf(path string) string {
	if path == "" || narrator.worktree == "" {
		return ""
	}
	// The path came from a model tool argument, so it is resolved and confined
	// here rather than trusted. The executor has already refused an escaping
	// write, but this reads on the failure path of a call that may have been
	// refused for any reason, so it does its own check instead of inheriting
	// one.
	root, err := filepath.Abs(narrator.worktree)
	if err != nil {
		return ""
	}
	resolved, err := filepath.Abs(
		filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return ""
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	content, err := os.ReadFile(resolved)
	if err != nil || len(content) > maximumPatchEchoBytes {
		return ""
	}
	answer := "\n\nThis is " + path + " as it stands now, after everything this " +
		"attempt has already changed. Your hunks must match this text, not " +
		"the copy you were shown when the attempt began. Copy the lines you " +
		"are changing from here.\n\n" + string(content)
	return answer + narrator.siblingsOf(path, len(answer))
}

// siblingsOf offers the other produced files beside the one just patched.
//
// A tool result reaches the model carrying the previous round's results and
// nothing else, so echoing only the file this call touched leaves every other
// file at whatever the attempt-start snapshot said. Ladder rung 4 on
// 2026-08-03: one round patched main_test.go, the next patched main.go from
// context predating a change made to main.go two rounds earlier, and the hunk
// matched nothing. Twenty-four of thirty-three patches failed in that run.
//
// Only the produced sources beside it, and only inside the same total bound, so
// this cannot grow into shipping a repository through a tool result.
func (narrator *narratingExecutor) siblingsOf(path string, used int) string {
	directory := pathpkg.Dir(filepath.ToSlash(path))
	entries, err := os.ReadDir(
		filepath.Join(narrator.worktree, filepath.FromSlash(directory)))
	if err != nil {
		return ""
	}
	var offered strings.Builder
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		sibling := pathpkg.Join(directory, name)
		if sibling == filepath.ToSlash(path) {
			continue
		}
		content, readErr := os.ReadFile(
			filepath.Join(narrator.worktree, filepath.FromSlash(sibling)))
		if readErr != nil {
			continue
		}
		if used+offered.Len()+len(content) > maximumPatchEchoBytes {
			continue
		}
		offered.WriteString("\n\nThis is " + sibling +
			" as it stands now.\n\n" + string(content))
	}
	return offered.String()
}

// refuse builds the result for a tool this narrator turned down itself.
//
// It has to carry the request's own identity and the tool schema version. The
// loop validates both before it will accept any result — a result that names a
// different request, or no request, is how a confused executor would look — and
// a refusal built without them was rejected as an invalid result rather than
// read as a refusal. That ended the whole attempt, reported only as "tool
// result identity or schema does not match the request", for what should have
// cost one round and taught the run something specific.
//
// Ladder rung 4 on 2026-08-03 lost two attempts to it in a single run, one of
// them the escalation to the most expensive rung.
func (narrator *narratingExecutor) refuse(
	request executor.AuthorizedToolRequest, why string,
) executor.ToolResult {
	return executor.ToolResult{
		RequestID:      request.Request.ID,
		SchemaVersion:  executor.ToolSchemaVersion,
		State:          "failed",
		ExitCode:       -1,
		StdoutRedacted: why,
	}
}

// producedSourcesNow offers every produced Go file as it currently stands.
//
// Used to answer a suite run, which otherwise tells the model what happened
// without telling it what it happened to. Bounded by the same echo limit as a
// patch answer, and silent when it cannot read the tree: an incomplete picture
// of a file is worse than none, because a hunk copied from half a file matches
// nothing.
func (narrator *narratingExecutor) producedSourcesNow() string {
	if narrator.worktree == "" {
		return ""
	}
	var paths []string
	walkErr := filepath.WalkDir(narrator.worktree,
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(entry.Name(), ".go") {
				paths = append(paths, path)
			}
			return nil
		})
	if walkErr != nil {
		return ""
	}
	sort.Strings(paths)
	var offered strings.Builder
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		relative, relErr := filepath.Rel(narrator.worktree, path)
		if relErr != nil {
			continue
		}
		if offered.Len()+len(content) > maximumPatchEchoBytes {
			continue
		}
		offered.WriteString(producedFileHeading(filepath.ToSlash(relative)) +
			string(content))
	}
	if offered.Len() == 0 {
		return ""
	}
	return "\n\nThe suite ran against these files. Patch hunks must match " +
		"this text, not the copy you were shown when the attempt began." +
		offered.String()
}

// producedFileHeading introduces one file's current text inside a tool result.
func producedFileHeading(path string) string {
	return "\n\nThis is " + path + " as it stands now.\n\n"
}

// producedSourceMaxBytes is how large a file write or a patch may be.
//
// Both inherited the loop's four-kilobyte default for an unbounded argument,
// which is generous for a path and far too small for the thing being written.
// A produced test file passes four kilobytes as soon as it has a table in it,
// and a patch that adds a handful of cases to one passes it too.
//
// The cost of getting this wrong is not a refused call, which the run could
// answer. The bound is checked while the turn is decoded, so the whole turn is
// malformed and the attempt is spent: ladder rung 5 on 2026-08-03 lost attempt
// 5 to "model tool argument \"patch\" exceeds its bound", then ran out of
// attempts one line short of passing path-coverage.
//
// Thirty-two kilobytes, against the loop's own sixty-four-kilobyte ceiling for
// a whole tool call. Large enough for any file these runs produce and still
// half the ceiling, so a single argument cannot crowd out the rest of the call.
const producedSourceMaxBytes = 32 << 10

// sourcesIfTheyMoved attaches the produced sources, or says they have not
// changed since the last time they were attached.
//
// The echo is there so a hunk is written against the file as it now stands.
// Once the model has been shown that text, sending it again every round adds
// prompt without adding information — and prompt is what a round costs. The
// note keeps the guarantee explicit, so a run is never left guessing whether
// silence means unchanged or means nobody looked.
func (narrator *narratingExecutor) sourcesIfTheyMoved() string {
	// Sent every time, even when nothing has moved.
	//
	// Skipping the repeat looked free and was not. A round is given the results
	// of the round before it and nothing earlier, so text echoed three rounds
	// ago is not in the prompt at all — and a note saying the files are
	// unchanged since they were last shown points at something the model can no
	// longer see. Suppressing the echo suppressed the fix it was optimising.
	//
	// Measured on ladder rung 6, 2026-08-03: echoing every time landed 13
	// patches of 28, echoing only on change landed 9 of 25, while median round
	// latency fell from 6.5s to 5.0s. The cheaper round was not worth the
	// missed patch, because a patch that misses costs a whole round of its own.
	return narrator.producedSourcesNow()
}

// filesInSteps is the layout a plan is written across, in the order its steps
// name it.
//
// A run decides where its work goes once. Later attempts rebuild their steps so
// the step kinds can follow the filesystem — attempt one creates a file,
// attempt three patches the file attempt one wrote — and rebuilding them from
// the requirement again re-derives the layout too, through the fallback parser,
// which answers cmd/generated/main.go for any request naming no path. That is
// how a run whose planner chose cmd/stats and stats came to be told on its
// second attempt to write somewhere else entirely.
func filesInSteps(steps []agentloop.PlanStep) []string {
	var files []string
	seen := map[string]bool{}
	for _, step := range steps {
		for _, file := range step.ExpectedFiles {
			if file == "" || seen[file] {
				continue
			}
			seen[file] = true
			files = append(files, file)
		}
	}
	return files
}
