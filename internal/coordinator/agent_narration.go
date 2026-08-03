package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

	result, err := narrator.inner.ExecuteTool(ctx, request)
	if err != nil {
		narrator.execution.publishTool(narrator.ctx, narrator.scope,
			events.KindToolCompleted, executionID, name,
			string(domain.CommandExecutionStateFailed),
			detail+" — refused: "+err.Error())
		return result, err
	}
	summary := detail
	if outcome := strings.TrimSpace(firstLineOf(result.StdoutRedacted)); outcome != "" {
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
	if completed != domain.CommandExecutionStateSucceeded {
		narrator.lastFailure = strings.TrimSpace(
			result.StdoutRedacted + "\n" + result.StderrRedacted)
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
	}
	if executor.ToolName(name) == executor.ToolApplyEdit &&
		completed == domain.CommandExecutionStateSucceeded {
		// A successful write supersedes whatever the last test run judged.
		// A failed one changed nothing, so it leaves the verdict standing.
		narrator.filesChangedSinceValidation = true
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
func agentPlanSteps(requirement string) []agentloop.PlanStep {
	files := filesNamedIn(requirement)
	steps := make([]agentloop.PlanStep, 0, len(files)+1)
	for index, file := range files {
		steps = append(steps, agentloop.PlanStep{
			ID: fmt.Sprintf("edit-%d", index+1), Kind: agentloop.StepKindEdit,
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
			CompletionTools: []executor.ToolName{executor.ToolApplyEdit},
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
func agentApprovedTools() []agentloop.ApprovedTool {
	catalog := map[executor.ToolName]executor.ToolDescriptor{}
	for _, descriptor := range executor.ToolCatalog() {
		catalog[descriptor.Name] = descriptor
	}
	return []agentloop.ApprovedTool{
		{
			Descriptor: catalog[executor.ToolApplyEdit],
			Arguments: []agentloop.ToolArgumentDefinition{
				{Name: "path", Required: true},
				{Name: "content", Required: true},
			},
			DefaultTimeout:    30 * time.Second,
			MaterialEdit:      true,
			CreatesCheckpoint: true,
		},
		{
			Descriptor: catalog[executor.ToolTest],
			Arguments: []agentloop.ToolArgumentDefinition{
				{Name: "executable", Required: true},
				{Name: "arg1", Required: true},
				{Name: "arg2", Required: true},
			},
			DefaultTimeout: 4 * time.Minute,
		},
	}
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
	case executor.ToolApplyEdit:
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
	if !succeeded || executor.ToolName(request.Request.Name) != executor.ToolApplyEdit {
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
