package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/providers"
)

var (
	ErrMalformedModelTurn = errors.New("agent model turn is malformed")
	// ErrPlanContract is a plan this loop will never accept, whoever sends it.
	//
	// A step whose completion tool does not match its kind, a kind this build
	// does not know, a step with no expected files where its kind demands them:
	// none of that is a model producing bad work, and none of it is repaired by
	// asking a better model. It is the coordinator's own metadata disagreeing
	// with the loop's, and it fails identically every time.
	//
	// It is separated so a caller can tell the two apart. Retrying this cost a
	// run three attempts and an escalation to the most expensive rung before it
	// gave up, and every one of those attempts failed on the same sentence.
	ErrPlanContract      = errors.New("agent plan does not satisfy the loop's contract")
	ErrUnknownTool       = errors.New("agent model requested an unknown tool")
	ErrAccountingUnknown = errors.New("agent usage or cost accounting is unknown")
)

const (
	absoluteMaximumRounds            = 256
	absoluteMaximumToolCalls         = 1024
	absoluteMaximumToolCallsPerRound = 64
	absoluteMaximumTokens            = domain.TokenCount(10_000_000)
	absoluteMaximumTokensPerRound    = domain.TokenCount(1_000_000)
	absoluteMaximumIdenticalFailures = 32
	absoluteMaximumContextItems      = 1024
	absoluteMaximumFactualEvents     = 4096
	absoluteMaximumContextBytes      = 16 << 20
)

// ExecutionLoop implements the smallest fixed-policy observe-think-act-result
// loop. It treats model output as untrusted proposals and crosses effects only
// through authority, durable journal, and tool ports.
type ExecutionLoop struct {
	dependencies LoopDependencies
}

func NewExecutionLoop(
	dependencies LoopDependencies,
) (*ExecutionLoop, error) {
	switch {
	case dependencies.Model == nil:
		return nil, errors.New("agent fixed model is required")
	case dependencies.Authority == nil:
		return nil, errors.New("agent authority router is required")
	case dependencies.Tools == nil:
		return nil, errors.New("agent tool executor is required")
	case dependencies.Journal == nil:
		return nil, errors.New("agent tool journal is required")
	case dependencies.PlanSteps == nil:
		return nil, errors.New("agent plan-step store is required")
	case dependencies.Checkpoints == nil:
		return nil, errors.New("agent checkpoint store is required")
	case dependencies.PlanApprovalCheckpoints == nil:
		return nil, errors.New(
			"agent plan-approved checkpoint store is required",
		)
	case dependencies.Control == nil:
		return nil, errors.New("agent control reader is required")
	case dependencies.Interrupts == nil:
		return nil, errors.New("agent control interrupt bridge is required")
	}
	if dependencies.Now == nil {
		dependencies.Now = time.Now
	}
	return &ExecutionLoop{dependencies: dependencies}, nil
}

func (loop *ExecutionLoop) Run(
	ctx context.Context,
	input LoopInput,
) (LoopOutcome, error) {
	if loop == nil {
		return LoopOutcome{}, errors.New("agent execution loop is unavailable")
	}
	if err := validateLoopInput(input); err != nil {
		return LoopOutcome{}, err
	}
	modelIdentity := loop.dependencies.Model.Identity()
	if err := validateFixedModelIdentity(modelIdentity); err != nil {
		return LoopOutcome{}, err
	}
	declarations, approvedTools, err := buildToolDeclarations(input.ApprovedTools)
	if err != nil {
		return LoopOutcome{}, err
	}
	cost, err := newCostAccumulator(input.Limits.MaximumCost)
	if err != nil {
		return LoopOutcome{}, err
	}
	plan := clonePlan(input.Plan)
	contextItems := append(
		[]RepositoryContextItem(nil),
		input.RepositoryContext...,
	)
	factualEvents := append([]FactualEvent(nil), input.FactualEvents...)
	started := loop.dependencies.Now().UTC()
	deadline := started.Add(input.Limits.MaximumWallClock)
	// writesSinceContextRead counts material writes made since the repository
	// context was last read, so it is re-read exactly when it has gone stale.
	writesSinceContextRead := 0
	var (
		rounds          uint32
		toolCalls       uint32
		tokens          domain.TokenCount
		previousResults []ToolFeedback
	)
	// writesSinceLastTest is whether a material edit has landed that no test run
	// has seen since, and lastTestFailed what the most recent run of the suite
	// said. Together they decide whether an attempt may end. emptyTurnRetries
	// and unverifiedCompletionRefusals bound the two places the loop answers a
	// turn itself, so neither can spend the round budget.
	writesSinceLastTest := false
	lastTestFailed := false
	emptyTurnRetries := 0
	// batchingRetries counts turns refused for asking more calls than the round
	// takes. Bounded, because a run that keeps over-batching after being told
	// the bound is not going to stop being told it.
	batchingRetries := 0
	outOfScopeRetries := 0
	unverifiedCompletionRefusals := 0
	failureCounts := make(map[string]uint32)
	callIDs := make(map[string]struct{})
	modelRequestIDs := make(map[domain.ModelRequestID]struct{})

	control, err := loop.readControl(ctx, input)
	if err != nil {
		return LoopOutcome{}, err
	}
	if outcome, stop := controlStopOutcome(
		control, rounds, toolCalls, tokens, cost.amount(), plan,
	); stop {
		return outcome, nil
	}
	if err := loop.dependencies.PlanApprovalCheckpoints.
		CreatePlanApprovedCheckpoint(
			ctx,
			PlanApprovedCheckpointRequest{
				TaskID: input.TaskID, RunID: input.RunID,
				PlanRevision: input.Plan.Revision,
				ApprovalID:   input.PlanApprovalID,
			},
		); err != nil {
		return LoopOutcome{}, fmt.Errorf(
			"capture approved plan before execution: %w",
			err,
		)
	}

	for rounds < input.Limits.MaximumRounds {
		if !loop.dependencies.Now().UTC().Before(deadline) {
			return finishLoopOutcome(
				OutcomeLimitReached,
				"agent wall-clock limit reached",
				rounds, toolCalls, tokens, cost.amount(), plan, true,
			), nil
		}
		control, err = loop.readControl(ctx, input)
		if err != nil {
			return LoopOutcome{}, err
		}
		if outcome, stop := controlStopOutcome(
			control, rounds, toolCalls, tokens, cost.amount(), plan,
		); stop {
			return outcome, nil
		}

		rounds++
		deadlineContext, cancelDeadline := context.WithDeadline(ctx, deadline)
		turnContext, cancelTurn, err :=
			loop.dependencies.Interrupts.BindActionContext(
				deadlineContext,
				input.TaskID,
				input.RunID,
				ActionDescriptor{
					Kind:        ActionModel,
					RequestID:   fmt.Sprintf("model-round-%d", rounds),
					LongRunning: true,
				},
			)
		if err != nil {
			cancelDeadline()
			if errors.Is(err, ErrActionBlocked) {
				latest, controlErr := loop.readControl(
					context.WithoutCancel(ctx),
					input,
				)
				if controlErr != nil {
					return LoopOutcome{}, controlErr
				}
				if outcome, stop := controlStopOutcome(
					latest,
					rounds,
					toolCalls,
					tokens,
					cost.amount(),
					plan,
				); stop {
					return outcome, nil
				}
			}
			return LoopOutcome{}, err
		}
		// The files are re-read once a write has landed on them.
		//
		// Read once at the start of the attempt and reused, the context
		// presents files as they were before this attempt touched anything —
		// confidently, in a field called content, at the top of the document.
		// A patch is matched against a file's exact text, so a hunk written
		// from that copy cannot apply, and the current copy exists only inside
		// the previous round's tool result. Rung 16 on 2026-08-04 failed 90
		// patches in one run with "does not match anything", every one of them
		// against context several edits old.
		//
		// Only after a write, because re-reading before one would cost a walk
		// of the worktree per round to produce the bytes already in hand.
		if writesSinceContextRead > 0 && loop.dependencies.RefreshContext != nil {
			contextItems = mergeContextByPath(
				contextItems,
				loop.dependencies.RefreshContext(context.WithoutCancel(ctx)),
			)
			writesSinceContextRead = 0
		}
		turn, turnErr := loop.dependencies.Model.ObserveThink(
			turnContext,
			ModelInput{
				TaskID: input.TaskID, RunID: input.RunID,
				Model: modelIdentity, Round: rounds,
				RepositoryContext: append(
					[]RepositoryContextItem(nil),
					contextItems...,
				),
				Plan:          clonePlan(plan),
				FactualEvents: append([]FactualEvent(nil), factualEvents...),
				ApprovedTools: cloneToolDeclarations(declarations),
				PreviousResults: append(
					[]ToolFeedback(nil),
					previousResults...,
				),
			},
		)
		cancelTurn()
		cancelDeadline()
		if turnErr != nil {
			latest, controlErr := loop.readControl(
				context.WithoutCancel(ctx),
				input,
			)
			if controlErr != nil {
				return LoopOutcome{}, errors.Join(turnErr, controlErr)
			}
			if outcome, stop := controlStopOutcome(
				latest, rounds, toolCalls, tokens, cost.amount(), plan,
			); stop {
				return outcome, nil
			}
			if errors.Is(turnErr, context.Canceled) ||
				errors.Is(ctx.Err(), context.Canceled) {
				return finishLoopOutcome(
					OutcomeCancelled,
					"agent model request was cancelled",
					rounds, toolCalls, tokens, cost.amount(), plan, true,
				), nil
			}
			if errors.Is(turnErr, context.DeadlineExceeded) {
				return finishLoopOutcome(
					OutcomeLimitReached,
					"agent model request reached the wall-clock limit",
					rounds, toolCalls, tokens, cost.amount(), plan, true,
				), nil
			}
			return LoopOutcome{}, fmt.Errorf("agent fixed model turn: %w", turnErr)
		}
		if turn.Model != modelIdentity {
			return LoopOutcome{}, fmt.Errorf(
				"%w: fixed model identity changed",
				ErrMalformedModelTurn,
			)
		}
		if turn.ModelRequestID.IsZero() {
			return LoopOutcome{}, fmt.Errorf(
				"%w: model request identity is missing",
				ErrMalformedModelTurn,
			)
		}
		if _, exists := modelRequestIDs[turn.ModelRequestID]; exists {
			return LoopOutcome{}, fmt.Errorf(
				"%w: model request identity was reused across rounds",
				ErrMalformedModelTurn,
			)
		}
		modelRequestIDs[turn.ModelRequestID] = struct{}{}
		roundTokens, err := usageTokenCount(turn.Usage)
		if err != nil {
			return LoopOutcome{}, errors.Join(ErrAccountingUnknown, err)
		}
		if roundTokens > input.Limits.MaximumTokensPerRound {
			return finishLoopOutcome(
				OutcomeLimitReached,
				"agent per-round token limit reached",
				rounds, toolCalls, tokens, cost.amount(), plan, true,
			), nil
		}
		tokens, err = addTokens(tokens, roundTokens)
		if err != nil {
			return LoopOutcome{}, err
		}
		if tokens > input.Limits.MaximumTokens {
			return finishLoopOutcome(
				OutcomeLimitReached,
				"agent cumulative token limit reached",
				rounds, toolCalls, tokens, cost.amount(), plan, true,
			), nil
		}
		if err := cost.add(turn.Cost); err != nil {
			return LoopOutcome{}, errors.Join(ErrAccountingUnknown, err)
		}
		if cost.exceeds(input.Limits.MaximumCost) {
			return finishLoopOutcome(
				OutcomeLimitReached,
				"agent cost limit reached",
				rounds, toolCalls, tokens, cost.amount(), plan, true,
			), nil
		}
		if err := validateModelTurn(
			turn,
			input.Limits.MaximumToolCallsPerRound,
		); err != nil {
			// A turn that said nothing is answered, not escalated.
			//
			// Every malformed turn ends the attempt: the coordinator treats it
			// as the model's mistake rather than the machinery's, sends the
			// work back, and starts again. That is right for a turn that
			// proposed something impossible, because the next attempt needs to
			// be told. It is far too expensive for a turn that proposed
			// nothing at all, which is a slip the very next round fixes — an
			// attempt costs thirty to sixty seconds against a round's four.
			//
			// Ladder rung 4 on 2026-08-03 lost attempt 2 to exactly this, with
			// the whole diagnosis being "turn contains neither tool calls nor
			// completion".
			//
			// A turn with more calls than the round takes costs a round too.
			//
			// It was counted as a model disagreeing with the contract rather
			// than losing its place, and cost the whole attempt. That is the
			// wrong price: the run knows what it wants to do and got the
			// batching wrong, which one sentence corrects. Ladder rung 18 on
			// 2026-08-04 opened by writing all three of its files in one turn,
			// lost the attempt, did it again, and the run was over in
			// seventy-seven seconds having produced nothing.
			if errors.Is(err, errTooManyCallsInATurn) &&
				batchingRetries < maximumBatchingRetries {
				batchingRetries++
				previousResults = []ToolFeedback{{
					Tool:  "none",
					State: "failed", IsError: true,
					SummaryRedacted: "too many tool calls in one turn",
					StdoutRedacted: "That turn asked for more tool calls than " +
						"this round takes, so none of them ran. " +
						err.Error() + ". Send the first one now; the next " +
						"round is for the one after it. Nothing you have " +
						"written has been lost.",
				}}
				continue
			}
			// Only the empty turn and the over-batched one, and only a bounded
			// number of times. A turn carrying both tool calls and completion
			// is a model disagreeing with the contract rather than losing its
			// place, and it still costs the attempt.
			if errors.Is(err, errEmptyModelTurn) &&
				emptyTurnRetries < maximumEmptyTurnRetries {
				emptyTurnRetries++
				previousResults = []ToolFeedback{{
					Tool:  "none",
					State: "failed", IsError: true,
					SummaryRedacted: "that turn did nothing",
					StdoutRedacted: "Your last turn produced no tool call this " +
						"round could accept and said nothing, so nothing " +
						"happened. A call naming a step that is already " +
						"finished is discarded, which looks exactly like this. " +
						openStepAdvice(plan) + " Call one of the tools listed " +
						"in tools_you_may_call against one of those steps, or " +
						"reply in plain text that the work is finished.",
				}}
				continue
			}
			return LoopOutcome{}, err
		}
		// A durable pause/cancel may arrive with the final stream frame. Re-read
		// it before accepting either a tool proposal or completion signal.
		control, err = loop.readControl(context.WithoutCancel(ctx), input)
		if err != nil {
			return LoopOutcome{}, err
		}
		if outcome, stop := controlStopOutcome(
			control, rounds, toolCalls, tokens, cost.amount(), plan,
		); stop {
			return outcome, nil
		}
		if turn.Completion != CompletionNone {
			// An attempt may not end on a write nobody ran the tests over.
			//
			// The coordinator re-runs the suite on whatever the attempt leaves
			// behind, and when that fails the whole refinement is discarded and
			// the worktree put back — deliberately, because a broken candidate
			// makes a worse baseline than the verified revision it replaced.
			// The expensive part is not the discard, it is that the run had the
			// test tool the entire time and simply stopped one call short: the
			// failure it would have seen was its own, in the file it had just
			// written, and fixing it in the same attempt costs one round.
			//
			// Ladder rung 3 on 2026-08-03 lost attempts 5 and 7 that way,
			// roughly 200 seconds, each time reported as "files were written
			// after the run's own last test, so the suite was run again here
			// and did not pass". It had reached 30 of 36 synthesised cases and
			// gave the work back twice.
			//
			// Refused once per run, not every time. If the run tests, still
			// claims completion, and the suite genuinely disagrees, that is the
			// coordinator's gate to judge — this guard exists to stop an
			// unverified ending, not to argue with a verified one.
			if (writesSinceLastTest || lastTestFailed) &&
				unverifiedCompletionRefusals < maximumUnverifiedCompletionRefusals &&
				toolCanStillBeCalled(plan, executor.ToolTest) {
				unverifiedCompletionRefusals++
				why := "You have written files since the last time the tests " +
					"ran, so nothing has checked what you are about to hand back."
				if lastTestFailed {
					why = "The last run of the tests did not pass, so what you " +
						"are about to hand back is known to be broken."
				}
				previousResults = []ToolFeedback{{
					Tool:  string(executor.ToolTest),
					State: "failed", IsError: true,
					SummaryRedacted: "the work is not finished yet",
					StdoutRedacted: why + " Run the tests now. If they pass, say " +
						"the work is finished; if they do not, fix what they " +
						"report first — a failure found here costs one round, " +
						"and the same failure found after this attempt ends " +
						"costs the whole attempt, because the work is put back " +
						"to the last revision that passed.",
				}}
				continue
			}
			return completionOutcome(
				turn.Completion,
				control,
				rounds,
				toolCalls,
				tokens,
				cost.amount(),
				plan,
			), nil
		}

		currentResults := make([]ToolFeedback, 0, len(turn.ToolCalls))
		for _, proposed := range turn.ToolCalls {
			if toolCalls >= input.Limits.MaximumToolCalls {
				return finishLoopOutcome(
					OutcomeLimitReached,
					"agent tool-call limit reached",
					rounds, toolCalls, tokens, cost.amount(), plan, true,
				), nil
			}
			control, err = loop.readControl(ctx, input)
			if err != nil {
				return LoopOutcome{}, err
			}
			if outcome, stop := controlStopOutcome(
				control, rounds, toolCalls, tokens, cost.amount(), plan,
			); stop {
				return outcome, nil
			}
			if _, exists := callIDs[proposed.Call.ID]; exists {
				return LoopOutcome{}, fmt.Errorf(
					"%w: tool-call ID was reused",
					ErrMalformedModelTurn,
				)
			}
			tool, exists := approvedTools[proposed.Call.Name]
			if !exists {
				return LoopOutcome{}, fmt.Errorf(
					"%w: %q",
					ErrUnknownTool,
					proposed.Call.Name,
				)
			}
			stepIndex, err := executableStepIndex(plan, proposed.PlanStepID)
			if err != nil {
				return LoopOutcome{}, err
			}
			if err := validateToolStepCompatibility(
				plan.Steps[stepIndex],
				tool,
			); err != nil {
				return LoopOutcome{}, err
			}
			decoded, err := decodeModelToolCall(
				input.TaskID,
				input.RunID,
				input.WorktreePath,
				modelIdentity,
				tool,
				proposed.Call,
				proposed.PlanStepID,
			)
			if err != nil {
				return LoopOutcome{}, errors.Join(ErrMalformedModelTurn, err)
			}
			if err := validateToolStepPathScope(
				plan.Steps[stepIndex],
				decoded.request,
			); err != nil {
				// The model labelled the call with the wrong step. If exactly
				// one other executable step names this very path, the label is
				// the only thing wrong and the plan is not being violated: the
				// file is one the plan asked for, written by a call that
				// misfiled itself.
				//
				// Refusing the turn instead discarded everything in it. On
				// ladder rung 5 that cost two of six attempts — a third of the
				// run's budget — for a mislabelled step on a file the plan
				// itself named. The contract is unchanged: a path no step
				// names, or one that several name, is still refused.
				corrected, ok := stepOwningPath(plan, decoded.request)
				if !ok {
					// Writing outside the plan costs a round, not the attempt.
					//
					// The contract is unchanged and the write does not happen:
					// what changes is the price of proposing it. A run reaching
					// for a file no step names has made a correctable mistake —
					// on a generated workspace it is nearly always the stub
					// main.go it was shown in its opening context, mistaken for
					// the cmd/generated/main.go the plan actually owns — and
					// ending the attempt spends sixty seconds to say what one
					// round can say better, while it still has the file open.
					//
					// Ladder rung 9 on 2026-08-03 lost three of nine attempts
					// this way, every one of them to that same stub.
					//
					// Bounded, so a run that keeps reaching outside the plan
					// still ends the attempt rather than spending the round
					// budget being told no.
					if outOfScopeRetries < maximumOutOfScopeRetries {
						outOfScopeRetries++
						// Appended to this round's results rather than
						// assigned over the previous ones: this is inside the
						// loop over a turn's tool calls, so the round's own
						// tail is what carries results forward, and anything
						// written straight to previousResults is overwritten
						// by it before the next round ever sees it.
						currentResults = append(currentResults, ToolFeedback{
							CallID: decoded.request.ID,
							Tool:   string(decoded.request.Name),
							State:  "failed", IsError: true,
							SummaryRedacted: "that file is not in this plan",
							StdoutRedacted: err.Error() + ". " +
								writableFilesAdvice(plan) + " Nothing was " +
								"written. Send the same change against one of " +
								"those files.",
						})
						continue
					}
					return LoopOutcome{}, err
				}
				stepIndex = corrected
				if err := validateToolStepCompatibility(
					plan.Steps[stepIndex], tool,
				); err != nil {
					return LoopOutcome{}, err
				}
			}
			// Every record this call produces names the step the loop actually
			// bound it to, not the label the model sent.
			//
			// The correction above moved stepIndex and left proposed.PlanStepID
			// alone, so the tool request, its result, the checkpoints and the
			// step transition were written from two different answers to the
			// same question. The durable consistency trigger on
			// agent_plan_step_transitions requires them to agree — it matches a
			// transition against a tool request on task, run, plan revision,
			// step and model request — and transitionStep reads the corrected
			// step while everything else wrote the mislabelled one.
			//
			// So a mislabelled call succeeded, its work landed, and then the
			// transition recording it was refused with "plan step transition
			// attribution is inconsistent". The loop returns that error, which
			// discards the whole attempt: ladder rung 1 lost attempt 2 to it on
			// 2026-08-03, after four patches, with the message "the last attempt
			// was refused before any of its work was recorded".
			//
			// The correction is what saved rung 5 and is not in question. What
			// was wrong is that it was applied in one place and not the other
			// four.
			boundStepID := plan.Steps[stepIndex].ID
			authorization, err := loop.dependencies.Authority.RouteTool(
				ctx,
				decoded.request,
			)
			if err != nil {
				return LoopOutcome{}, err
			}
			if err := validateToolAuthorization(
				decoded.request,
				authorization,
				input.PolicyRevision,
				input.PolicySHA256,
			); err != nil {
				return LoopOutcome{}, err
			}
			switch authorization.Classification.Outcome {
			case executor.OutcomeApprovalRequired:
				return finishLoopOutcome(
					OutcomeAwaitingApproval,
					"tool action requires attributable approval",
					rounds, toolCalls, tokens, cost.amount(), plan, true,
				), nil
			case executor.OutcomeDenied:
				return finishLoopOutcome(
					OutcomePermissionDenied,
					"tool action was denied by permission policy",
					rounds, toolCalls, tokens, cost.amount(), plan, true,
				), nil
			}
			startRecord := ToolStartRecord{
				TaskID: input.TaskID, RunID: input.RunID,
				PlanRevision:          plan.Revision,
				PlanStepID:            boundStepID,
				Round:                 rounds,
				RequestID:             decoded.request.ID,
				ModelRequestID:        turn.ModelRequestID,
				ToolName:              decoded.request.Name,
				ToolSchemaVersion:     decoded.request.SchemaVersion,
				ArgumentsRedactedJSON: decoded.redactedArguments,
				ArgumentsSHA256:       decoded.argumentsSHA256,
				Authorization:         authorization,
			}
			if err := loop.dependencies.Journal.PersistToolStart(
				ctx,
				startRecord,
			); err != nil {
				return LoopOutcome{}, err
			}
			requiredAuthority := executor.RequiredAuthority(decoded.request)
			if requiredAuthority != executor.AuthorityAutomaticRead &&
				requiredAuthority != executor.AuthorityTaskWrite {
				if err := loop.dependencies.Checkpoints.CreateCheckpoint(
					context.WithoutCancel(ctx),
					CheckpointRequest{
						TaskID: input.TaskID, RunID: input.RunID,
						PlanRevision:   plan.Revision,
						PlanStepID:     boundStepID,
						ToolRequestID:  decoded.request.ID,
						ModelRequestID: turn.ModelRequestID,
						Round:          rounds, Trigger: CheckpointBeforeRisky,
						PermissionID: authorization.DecisionID,
						ActionSHA256: executor.ActionSHA256(decoded.request),
						Reason:       "before risky approved action",
					},
				); err != nil {
					return LoopOutcome{}, err
				}
			}
			if plan.Steps[stepIndex].State == StepPending {
				err = loop.transitionStep(
					ctx,
					input,
					&plan,
					stepIndex,
					StepInProgress,
					"approved tool action began",
					decoded.request.ID,
					turn.ModelRequestID,
				)
				if err != nil {
					return LoopOutcome{}, err
				}
			}
			callIDs[decoded.request.ID] = struct{}{}
			toolCalls++
			toolDeadlineContext, cancelToolDeadline :=
				context.WithDeadline(ctx, deadline)
			toolContext, cancelTool, err :=
				loop.dependencies.Interrupts.BindActionContext(
					toolDeadlineContext,
					input.TaskID,
					input.RunID,
					ActionDescriptor{
						Kind:      ActionTool,
						RequestID: decoded.request.ID,
						SafeRead: executor.RequiredAuthority(
							decoded.request,
						) == executor.AuthorityAutomaticRead,
					},
				)
			if err != nil {
				cancelToolDeadline()
				if errors.Is(err, ErrActionBlocked) {
					latest, controlErr := loop.readControl(
						context.WithoutCancel(ctx),
						input,
					)
					if controlErr != nil {
						return LoopOutcome{}, controlErr
					}
					if outcome, stop := controlStopOutcome(
						latest,
						rounds,
						toolCalls,
						tokens,
						cost.amount(),
						plan,
					); stop {
						return outcome, nil
					}
				}
				return LoopOutcome{}, err
			}
			result, executeErr := loop.dependencies.Tools.ExecuteTool(
				toolContext,
				executor.AuthorizedToolRequest{
					Request:        decoded.request,
					Classification: authorization.Classification,
					WorktreePath:   input.WorktreePath,
				},
			)
			actionContextErr := toolContext.Err()
			cancelTool()
			cancelToolDeadline()
			result = classifyToolExecutionResult(
				result,
				executeErr,
				actionContextErr,
				decoded.request.ID,
			)
			result, err = boundedToolResult(
				result,
				decoded.request.ID,
				input.Limits.MaximumResultBytes,
			)
			if err != nil {
				return LoopOutcome{}, err
			}
			resultDigest, err := hashToolResult(result)
			if err != nil {
				return LoopOutcome{}, err
			}
			if err := loop.dependencies.Journal.PersistToolResult(
				context.WithoutCancel(ctx),
				ToolResultRecord{
					TaskID: input.TaskID, RunID: input.RunID,
					PlanRevision: plan.Revision,
					PlanStepID:   boundStepID,
					Round:        rounds,
					RequestID:    decoded.request.ID,
					Result:       result,
					ResultSHA256: resultDigest,
				},
			); err != nil {
				return LoopOutcome{}, err
			}
			succeeded := result.State == "succeeded"
			if executor.ToolName(decoded.request.Name) == executor.ToolTest {
				writesSinceLastTest = !succeeded && writesSinceLastTest
				lastTestFailed = !succeeded
			}
			if tool.MaterialEdit && succeeded {
				writesSinceLastTest = true
				// The context the next round reads has just gone stale.
				writesSinceContextRead++
				if err := loop.dependencies.Checkpoints.CreateCheckpoint(
					context.WithoutCancel(ctx),
					CheckpointRequest{
						TaskID: input.TaskID, RunID: input.RunID,
						PlanRevision:   plan.Revision,
						PlanStepID:     boundStepID,
						ToolRequestID:  decoded.request.ID,
						ModelRequestID: turn.ModelRequestID,
						Round:          rounds,
						Trigger:        CheckpointMaterialEdit,
						Reason:         "material edit batch completed",
					},
				); err != nil {
					return LoopOutcome{}, err
				}
			}
			// A step already implemented is not completed a second time.
			//
			// A write bound to a closed step is the model rewriting a file the
			// plan named, which stepOwningPath deliberately allows rather than
			// discarding the attempt over. Completing the step again asks for
			// an implemented-to-implemented transition, which the state machine
			// rightly refuses — and refusing it ended the whole run, trading
			// one wasted attempt for a dead one.
			//
			// The write itself has already happened and is not undone. What is
			// skipped is only the bookkeeping that says the step is done, which
			// it already was.
			if succeeded && plan.Steps[stepIndex].State != StepImplemented {
				if err := loop.transitionStep(
					context.WithoutCancel(ctx),
					input,
					&plan,
					stepIndex,
					StepImplemented,
					"approved tool action completed the plan step",
					decoded.request.ID,
					turn.ModelRequestID,
				); err != nil {
					return LoopOutcome{}, err
				}
			}
			feedback := toolFeedback(result, decoded.request.Name)
			currentResults = append(currentResults, feedback)
			if result.State == "cancelled" {
				control, err = loop.readControl(
					context.WithoutCancel(ctx),
					input,
				)
				if err != nil {
					return LoopOutcome{}, err
				}
				if outcome, stop := controlStopOutcome(
					control, rounds, toolCalls, tokens, cost.amount(), plan,
				); stop {
					return outcome, nil
				}
				return finishLoopOutcome(
					OutcomeCancelled,
					"active tool action was cancelled",
					rounds, toolCalls, tokens, cost.amount(), plan, true,
				), nil
			}
			if !succeeded {
				fingerprint := failedActionFingerprint(
					decoded.request,
					result,
				)
				failureCounts[fingerprint]++
				if failureCounts[fingerprint] >=
					input.Limits.MaximumIdenticalFailures {
					// The step is not marked failed for this.
					//
					// A run of identical tool failures ends the attempt, which
					// is right: repeating a call that has failed three times
					// will not work the fourth. But the plan is rebuilt for
					// every attempt while the durable step record is not, so a
					// step written off here stays written off for the rest of
					// the run — and completion evidence must attribute to an
					// implemented step, so the run can go on to finish the work,
					// satisfy every gate, and still be unable to record that it
					// did.
					//
					// Ladder rung 4 on 2026-08-03 ended exactly there: "the
					// program is correct and the pipeline did not converge",
					// with the real reason four lines further down — "validate
					// attributed plan step: plan step \"step-001\" is failed,
					// not implemented". Three failed patches in one early
					// attempt had closed the door.
					//
					// The attempt still ends, and the outcome below still says
					// why. What is not done is writing that verdict into a
					// record the next five attempts inherit.
					return finishLoopOutcome(
						OutcomeAwaitingDirection,
						"identical failed action repeated; user direction is required",
						rounds, toolCalls, tokens, cost.amount(), plan, true,
					), nil
				}
			}
			control, err = loop.readControl(ctx, input)
			if err != nil {
				return LoopOutcome{}, err
			}
			if outcome, stop := controlStopOutcome(
				control, rounds, toolCalls, tokens, cost.amount(), plan,
			); stop {
				return outcome, nil
			}
		}
		previousResults = currentResults
	}
	return finishLoopOutcome(
		OutcomeLimitReached,
		"agent round limit reached",
		rounds, toolCalls, tokens, cost.amount(), plan, true,
	), nil
}

func validateLoopInput(input LoopInput) error {
	switch {
	case input.TaskID.IsZero() || input.RunID.IsZero() ||
		input.PlanApprovalID.IsZero():
		return errors.New(
			"agent task, run, and plan approval IDs are required",
		)
	case !filepath.IsAbs(input.WorktreePath):
		return errors.New("agent worktree path must be absolute")
	case input.Plan.Revision == 0:
		return errors.New("agent plan revision is required")
	case input.PolicyRevision == 0 || !validSHA256(input.PolicySHA256):
		return errors.New("agent execution policy binding is invalid")
	case strings.TrimSpace(input.Plan.RepositoryRevision) !=
		input.Plan.RepositoryRevision ||
		input.Plan.RepositoryRevision == "" ||
		len(input.Plan.RepositoryRevision) > 512:
		return errors.New("agent plan repository revision is invalid")
	case len(input.Plan.Steps) == 0 || len(input.Plan.Steps) > 256:
		return errors.New("agent plan steps are outside supported bounds")
	}
	if err := validateLoopLimits(input.Limits); err != nil {
		return err
	}
	steps := make(map[string]struct{}, len(input.Plan.Steps))
	for _, step := range input.Plan.Steps {
		if strings.TrimSpace(step.ID) != step.ID ||
			step.ID == "" || len(step.ID) > 255 ||
			strings.TrimSpace(step.SummaryRedacted) == "" ||
			len(step.SummaryRedacted) > 4096 ||
			!validStepState(step.State) {
			return errors.New("agent plan step is invalid")
		}
		if err := validatePlanStepContract(step); err != nil {
			return err
		}
		if _, exists := steps[step.ID]; exists {
			return errors.New("agent plan step ID is duplicated")
		}
		steps[step.ID] = struct{}{}
	}
	if len(input.RepositoryContext) > input.Limits.MaximumContextItems {
		return errors.New("agent repository context exceeds the item bound")
	}
	contextBytes := 0
	for _, item := range input.RepositoryContext {
		contextBytes += len(item.Path) + len(item.ContentRedacted)
		if strings.TrimSpace(item.Path) != item.Path ||
			item.Path == "" || len(item.Path) > 4096 ||
			!validSHA256(item.ContentSHA256) {
			return errors.New("agent repository context item is invalid")
		}
	}
	if contextBytes > input.Limits.MaximumContextBytes {
		return errors.New("agent repository context exceeds the byte bound")
	}
	if len(input.FactualEvents) > input.Limits.MaximumFactualEvents {
		return errors.New("agent factual events exceed the item bound")
	}
	var sequence uint64
	for index, event := range input.FactualEvents {
		if event.Sequence == 0 ||
			index != 0 && event.Sequence <= sequence ||
			strings.TrimSpace(event.Type) != event.Type ||
			event.Type == "" || len(event.Type) > 255 ||
			len(event.SummaryRedacted) > 4096 {
			return errors.New("agent factual event is invalid")
		}
		sequence = event.Sequence
	}
	return nil
}

func validateLoopLimits(limits LoopLimits) error {
	switch {
	case limits.MaximumRounds == 0 ||
		limits.MaximumRounds > absoluteMaximumRounds:
		return errors.New("agent round limit is required")
	case limits.MaximumToolCalls == 0 ||
		limits.MaximumToolCalls > absoluteMaximumToolCalls:
		return errors.New("agent tool-call limit is required")
	case limits.MaximumToolCallsPerRound == 0 ||
		limits.MaximumToolCallsPerRound > limits.MaximumToolCalls ||
		limits.MaximumToolCallsPerRound > absoluteMaximumToolCallsPerRound:
		return errors.New("agent per-round tool-call limit is invalid")
	case limits.MaximumTokens == 0 ||
		limits.MaximumTokens > absoluteMaximumTokens ||
		limits.MaximumTokensPerRound == 0 ||
		limits.MaximumTokensPerRound > limits.MaximumTokens ||
		limits.MaximumTokensPerRound > absoluteMaximumTokensPerRound:
		return errors.New("agent token limits are invalid")
	case limits.MaximumWallClock < time.Second ||
		limits.MaximumWallClock > 24*time.Hour:
		return errors.New("agent wall-clock limit is invalid")
	case limits.MaximumIdenticalFailures < 2 ||
		limits.MaximumIdenticalFailures > absoluteMaximumIdenticalFailures:
		return errors.New("agent identical-failure limit must be at least two")
	case limits.MaximumContextItems < 0 ||
		limits.MaximumContextItems > absoluteMaximumContextItems ||
		limits.MaximumFactualEvents < 0 ||
		limits.MaximumFactualEvents > absoluteMaximumFactualEvents ||
		limits.MaximumContextBytes < 0 ||
		limits.MaximumContextBytes > absoluteMaximumContextBytes:
		return errors.New("agent context bounds are invalid")
	case limits.MaximumResultBytes < 256 ||
		limits.MaximumResultBytes > 1<<20:
		return errors.New("agent result bound is invalid")
	}
	if _, err := newCostAccumulator(limits.MaximumCost); err != nil {
		return err
	}
	if !costWithinAbsoluteMaximum(limits.MaximumCost) {
		return errors.New("agent cost limit exceeds the absolute ceiling")
	}
	return nil
}

func validateFixedModelIdentity(model providers.ModelIdentity) error {
	fields := []string{
		model.Provider.Adapter,
		model.Provider.AdapterVersion,
		model.Provider.Provider,
		model.Provider.ProviderVersion,
		model.Model,
		model.Revision,
	}
	for _, field := range fields {
		if strings.TrimSpace(field) != field ||
			field == "" || len(field) > 255 {
			return errors.New("agent fixed model identity is invalid")
		}
	}
	return nil
}

func validateModelTurn(turn ModelTurn, perRound uint32) error {
	if len(turn.ToolCalls) == 0 && turn.Completion == CompletionNone {
		return fmt.Errorf("%w: %w", ErrMalformedModelTurn, errEmptyModelTurn)
	}
	if len(turn.ToolCalls) != 0 && turn.Completion != CompletionNone {
		return fmt.Errorf(
			"%w: tool calls and completion are mutually exclusive",
			ErrMalformedModelTurn,
		)
	}
	if uint32(len(turn.ToolCalls)) > perRound {
		return fmt.Errorf(
			"%w: %w: %d calls in one turn, and this round takes %d",
			ErrMalformedModelTurn, errTooManyCallsInATurn,
			len(turn.ToolCalls), perRound,
		)
	}
	switch turn.Completion {
	case CompletionNone,
		CompletionImplementationComplete,
		CompletionValidationComplete,
		CompletionNeedsDirection:
	default:
		return fmt.Errorf(
			"%w: completion signal is unknown",
			ErrMalformedModelTurn,
		)
	}
	return nil
}

func validateToolAuthorization(
	request executor.ToolRequest,
	authorization ToolAuthorization,
	policyRevision uint64,
	policySHA256 string,
) error {
	classification := authorization.Classification
	switch classification.Outcome {
	case executor.OutcomeAutomatic,
		executor.OutcomeTaskScoped,
		executor.OutcomeApprovalRequired,
		executor.OutcomeDenied:
	default:
		return errors.New("tool authority router returned an invalid outcome")
	}
	if classification.ScopeHash != executor.ActionSHA256(request) {
		return errors.New("tool authority decision is bound to another action")
	}
	if strings.TrimSpace(authorization.DecisionID) != authorization.DecisionID ||
		authorization.DecisionID == "" ||
		len(authorization.DecisionID) > 255 ||
		authorization.PolicyRevision != policyRevision ||
		authorization.PolicySHA256 != policySHA256 {
		return errors.New(
			"tool authority decision is not bound to the current execution policy",
		)
	}
	if classification.Required == "" ||
		classification.Capability != classification.Required {
		return errors.New("tool authority decision lacks a derived capability")
	}
	if classification.Outcome == executor.OutcomeTaskScoped &&
		strings.TrimSpace(classification.MatchedGrantID) == "" {
		return errors.New("task-scoped tool authority lacks attribution")
	}
	return nil
}

func (loop *ExecutionLoop) readControl(
	ctx context.Context,
	input LoopInput,
) (ControlState, error) {
	state, err := loop.dependencies.Control.ReadControl(
		ctx,
		input.TaskID,
		input.RunID,
	)
	if err != nil {
		return ControlState{}, err
	}
	switch state.Disposition {
	case ControlActive, ControlPauseRequested, ControlPaused, ControlCancelled,
		ControlStopped:
	default:
		return ControlState{}, errors.New("agent control state is invalid")
	}
	return state, nil
}

func controlStopOutcome(
	control ControlState,
	rounds uint32,
	toolCalls uint32,
	tokens domain.TokenCount,
	cost providers.ExactAmount,
	plan PlanProjection,
) (LoopOutcome, bool) {
	switch control.Disposition {
	case ControlPauseRequested, ControlPaused:
		return finishLoopOutcome(
			OutcomePaused, "task execution is paused",
			rounds, toolCalls, tokens, cost, plan, true,
		), true
	case ControlCancelled:
		return finishLoopOutcome(
			OutcomeCancelled, "task execution is cancelled",
			rounds, toolCalls, tokens, cost, plan, true,
		), true
	case ControlStopped:
		return finishLoopOutcome(
			OutcomeStopped, "task execution is stopped",
			rounds, toolCalls, tokens, cost, plan, true,
		), true
	}
	if !control.PolicyCurrent {
		return finishLoopOutcome(
			OutcomePolicyBlocked, "execution policy is no longer current",
			rounds, toolCalls, tokens, cost, plan, true,
		), true
	}
	if !control.BudgetAvailable {
		return finishLoopOutcome(
			OutcomeBudgetExhausted, "task hard budget blocks the next action",
			rounds, toolCalls, tokens, cost, plan, true,
		), true
	}
	return LoopOutcome{}, false
}

func completionOutcome(
	signal CompletionSignal,
	control ControlState,
	rounds uint32,
	toolCalls uint32,
	tokens domain.TokenCount,
	cost providers.ExactAmount,
	plan PlanProjection,
) LoopOutcome {
	if signal != CompletionNeedsDirection &&
		!implementationStepsComplete(plan) {
		return finishLoopOutcome(
			OutcomeAwaitingDirection,
			"fixed model declared completion before implementation steps completed",
			rounds, toolCalls, tokens, cost, plan, true,
		)
	}
	switch signal {
	case CompletionNeedsDirection:
		return finishLoopOutcome(
			OutcomeAwaitingDirection,
			"fixed model requested user direction",
			rounds, toolCalls, tokens, cost, plan, true,
		)
	case CompletionValidationComplete:
		if control.ValidationComplete && validationStepsComplete(plan) {
			return finishLoopOutcome(
				OutcomeValidationComplete,
				"implementation and required validation are complete",
				rounds, toolCalls, tokens, cost, plan, false,
			)
		}
		return finishLoopOutcome(
			OutcomeImplementationComplete,
			"implementation is complete but validation evidence is incomplete",
			rounds, toolCalls, tokens, cost, plan, true,
		)
	default:
		return finishLoopOutcome(
			OutcomeImplementationComplete,
			"implementation is complete; validation remains a separate gate",
			rounds, toolCalls, tokens, cost, plan,
			!control.ValidationComplete || !validationStepsComplete(plan),
		)
	}
}

func implementationStepsComplete(plan PlanProjection) bool {
	for _, step := range plan.Steps {
		if step.State != StepImplemented &&
			step.State != StepValidated &&
			step.State != StepSkipped {
			return false
		}
	}
	return true
}

func validationStepsComplete(plan PlanProjection) bool {
	for _, step := range plan.Steps {
		if step.ValidationRequired {
			if step.State != StepValidated {
				return false
			}
			continue
		}
		if step.State != StepImplemented &&
			step.State != StepValidated &&
			step.State != StepSkipped {
			return false
		}
	}
	return true
}

func executableStepIndex(plan PlanProjection, stepID string) (int, error) {
	if strings.TrimSpace(stepID) != stepID || stepID == "" {
		return 0, fmt.Errorf(
			"%w: tool call lacks a plan step",
			ErrMalformedModelTurn,
		)
	}
	for index, step := range plan.Steps {
		if step.ID != stepID {
			continue
		}
		// A check may be performed again after it has been performed.
		//
		// Running the suite is not a deliverable a run finishes once; it is how
		// it finds out whether everything else is finished, and the answer stops
		// being true the moment the next file is written. Closing the step on
		// the first call took the tool away for the rest of the attempt, because
		// a tool is offered only while some step that can accept it is open. A
		// run that tested early and then kept patching could not check itself
		// again however much it changed, so it ended on work nothing had seen
		// and the coordinator discarded the whole attempt.
		//
		// Ladder rung 3 on 2026-08-03: attempt 4 was offered [apply-patch test]
		// for two rounds, ran the suite, and was offered [apply-patch] for the
		// seven rounds after it. Attempts 5 and 6 went the same way.
		//
		// The step still closes on its first successful call, because
		// implementationStepsComplete reads every step's state to decide whether
		// completion may be declared, and a step held open forever makes
		// completion unreachable. What changes is only that a closed check can
		// be called again: transitionStep is already skipped for a step that is
		// implemented, so a repeat call records its work and asks for no
		// transition the state machine would refuse.
		if !StepMayAcceptAnotherCall(step.State) {
			return 0, fmt.Errorf(
				"%w: plan step %q is not executable",
				ErrMalformedModelTurn,
				stepID,
			)
		}
		return index, nil
	}
	return 0, fmt.Errorf(
		"%w: plan step %q is unknown",
		ErrMalformedModelTurn,
		stepID,
	)
}

func validateToolStepCompatibility(
	step PlanStep,
	tool ApprovedTool,
) error {
	allowed := false
	for _, name := range step.CompletionTools {
		if name == tool.Descriptor.Name {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf(
			"%w: tool %q cannot complete plan step %q",
			ErrMalformedModelTurn,
			tool.Descriptor.Name,
			step.ID,
		)
	}
	if step.MaterialEdit != tool.MaterialEdit {
		return fmt.Errorf(
			"%w: tool and plan-step materiality differ",
			ErrMalformedModelTurn,
		)
	}
	if step.MaterialEdit && !tool.CreatesCheckpoint {
		return fmt.Errorf(
			"%w: material edit tool lacks checkpoint authority",
			ErrMalformedModelTurn,
		)
	}
	return nil
}

const (
	maximumExpectedFiles    = 512
	maximumExpectedPathSize = 4096
)

func validateToolStepPathScope(
	step PlanStep,
	request executor.ToolRequest,
) error {
	if !stepKindRequiresExpectedFiles(step.Kind) {
		return nil
	}
	path, found := requestArgument(request, "path")
	if !found {
		return fmt.Errorf(
			"%w: path-scoped tool %q lacks a path",
			ErrMalformedModelTurn,
			request.Name,
		)
	}
	normalized, ok := canonicalRelativePath(path)
	if !ok || normalized != path {
		return fmt.Errorf(
			"%w: tool path is not canonical repository-relative scope",
			ErrMalformedModelTurn,
		)
	}
	for _, expected := range step.ExpectedFiles {
		if path == expected ||
			strings.HasPrefix(path, expected+"/") {
			return nil
		}
	}
	return fmt.Errorf(
		"%w: tool path is outside plan step %q scope",
		ErrMalformedModelTurn,
		step.ID,
	)
}

func requestArgument(
	request executor.ToolRequest,
	name string,
) (string, bool) {
	var value string
	found := false
	for _, argument := range request.Arguments {
		if argument.Name != name {
			continue
		}
		if found {
			return "", false
		}
		value = argument.Value
		found = true
	}
	return value, found
}

type planStepKindContract struct {
	completionTool executor.ToolName
	// alsoCompletedBy is the one other tool this kind may declare, or empty
	// when the kind admits exactly one. It exists for the write kinds, where
	// which tool is right depends on whether the file is there, and that
	// changes during the attempt rather than between attempts.
	alsoCompletedBy    executor.ToolName
	materialEdit       bool
	validationRequired bool
}

// permits reports whether a declared completion-tool set is the one this kind
// allows.
//
// The primary tool is required and must come first, because it is the tool the
// step is named for and the one a run reaches for by default. The alternate is
// optional: a plan may declare it or not, and both are the same kind of step.
func (contract planStepKindContract) permits(declared []executor.ToolName) bool {
	if len(declared) == 0 || declared[0] != contract.completionTool {
		return false
	}
	if len(declared) == 1 {
		return true
	}
	return len(declared) == 2 &&
		contract.alsoCompletedBy != "" &&
		declared[1] == contract.alsoCompletedBy
}

func validatePlanStepContract(step PlanStep) error {
	contract, ok := planStepContract(step.Kind)
	if !ok {
		return fmt.Errorf("%w: step kind is invalid", ErrPlanContract)
	}
	if !contract.permits(step.CompletionTools) {
		return fmt.Errorf(
			"%w: a %s step is completed by %s and this one declares %v",
			ErrPlanContract, step.Kind, contract.completionTool,
			step.CompletionTools)
	}
	if step.MaterialEdit != contract.materialEdit ||
		step.ValidationRequired != contract.validationRequired {
		return fmt.Errorf("%w: step semantics differ from its kind",
			ErrPlanContract)
	}
	if !validExpectedFiles(step.ExpectedFiles) {
		return errors.New("agent plan step expected-file scope is invalid")
	}
	if stepKindRequiresExpectedFiles(step.Kind) &&
		len(step.ExpectedFiles) == 0 {
		return errors.New("path-scoped agent plan step lacks expected files")
	}
	return nil
}

func stepKindRequiresExpectedFiles(kind StepKind) bool {
	switch kind {
	case StepKindEdit, StepKindPatch, StepKindReadFile, StepKindListDirectory,
		StepKindSearchText, StepKindSearchSymbol:
		return true
	default:
		return false
	}
}

func validExpectedFiles(paths []string) bool {
	if len(paths) > maximumExpectedFiles {
		return false
	}
	previous := ""
	for index, path := range paths {
		normalized, ok := canonicalRelativePath(path)
		if !ok || normalized != path ||
			(index > 0 && path <= previous) {
			return false
		}
		previous = path
	}
	return true
}

func canonicalRelativePath(path string) (string, bool) {
	if !boundedTrimmedPath(path, maximumExpectedPathSize) ||
		strings.Contains(path, "\\") ||
		strings.Contains(path, ":") ||
		strings.IndexFunc(path, func(character rune) bool {
			return character < 0x20 || character == 0x7f
		}) >= 0 ||
		filepath.IsAbs(path) ||
		filepath.VolumeName(path) != "" {
		return "", false
	}
	normalized := filepath.ToSlash(filepath.Clean(path))
	if normalized == "." || normalized == ".." ||
		strings.HasPrefix(normalized, "../") {
		return "", false
	}
	return normalized, true
}

func boundedTrimmedPath(path string, maximum int) bool {
	return path != "" &&
		strings.TrimSpace(path) == path &&
		len(path) <= maximum &&
		utf8.ValidString(path)
}

func planStepContract(kind StepKind) (planStepKindContract, bool) {
	switch kind {
	case StepKindEdit:
		return planStepKindContract{
			completionTool: executor.ToolApplyEdit,
			// A step is planned as an edit because the file it names does not
			// exist yet, and it stops being true the moment the step's first
			// call lands. The rest of the attempt is revising a file that is
			// now there, which is what apply-patch is for, so an edit step
			// carries both and the write tools themselves decide which is
			// right: apply-edit refuses a second wholesale rewrite, and
			// apply-patch cannot create a file it has no context for.
			alsoCompletedBy: executor.ToolApplyPatch,
			materialEdit:    true, validationRequired: true,
		}, true
	case StepKindPatch:
		return planStepKindContract{
			completionTool: executor.ToolApplyPatch,
			materialEdit:   true, validationRequired: true,
		}, true
	case StepKindReadFile:
		return planStepKindContract{completionTool: executor.ToolReadFile}, true
	case StepKindListDirectory:
		return planStepKindContract{
			completionTool: executor.ToolListDirectory,
		}, true
	case StepKindSearchText:
		return planStepKindContract{completionTool: executor.ToolSearchText}, true
	case StepKindSearchSymbol:
		return planStepKindContract{
			completionTool: executor.ToolSearchSymbol,
		}, true
	case StepKindInspectDiff:
		return planStepKindContract{completionTool: executor.ToolInspectDiff}, true
	case StepKindGitStatus:
		return planStepKindContract{completionTool: executor.ToolGitStatus}, true
	case StepKindGitHistory:
		return planStepKindContract{completionTool: executor.ToolGitHistory}, true
	case StepKindTest:
		return planStepKindContract{
			completionTool: executor.ToolTest, validationRequired: true,
		}, true
	case StepKindBuild:
		return planStepKindContract{
			completionTool: executor.ToolBuild, validationRequired: true,
		}, true
	case StepKindStaticAnalysis:
		return planStepKindContract{
			completionTool:     executor.ToolStaticAnalysis,
			validationRequired: true,
		}, true
	default:
		return planStepKindContract{}, false
	}
}

func classifyToolExecutionResult(
	result executor.ToolResult,
	executeErr error,
	actionContextErr error,
	requestID string,
) executor.ToolResult {
	if executeErr == nil {
		return result
	}
	trustworthyEnvelope := result.RequestID == requestID &&
		result.SchemaVersion == executor.ToolSchemaVersion &&
		validToolResultState(result.State)
	if !trustworthyEnvelope {
		result = executor.ToolResult{
			RequestID: requestID, SchemaVersion: executor.ToolSchemaVersion,
		}
	}
	switch {
	case errors.Is(executeErr, context.DeadlineExceeded) ||
		errors.Is(actionContextErr, context.DeadlineExceeded):
		result.State = "cancelled"
		result.TimedOut = true
		result.Cancelled = false
		result.ExitCode = -1
		result.Summary = "tool execution reached its mediated deadline"
	case errors.Is(executeErr, context.Canceled) ||
		errors.Is(actionContextErr, context.Canceled):
		result.State = "cancelled"
		result.Cancelled = true
		result.TimedOut = false
		result.ExitCode = -1
		result.Summary = "tool execution was cancelled at the mediated boundary"
	default:
		if trustworthyEnvelope && result.State == "outcome-unknown" &&
			!result.Cancelled && !result.TimedOut {
			return result
		}
		result.State = "outcome-unknown"
		result.Cancelled = false
		result.TimedOut = false
		result.ExitCode = -1
		result.Summary = "tool execution outcome is unknown at the mediated boundary"
	}
	return result
}

func validToolResultState(state string) bool {
	switch state {
	case "succeeded", "failed", "cancelled", "outcome-unknown":
		return true
	default:
		return false
	}
}

func (loop *ExecutionLoop) transitionStep(
	ctx context.Context,
	input LoopInput,
	plan *PlanProjection,
	index int,
	to StepState,
	reason string,
	toolRequestID string,
	modelRequestID domain.ModelRequestID,
) error {
	from := plan.Steps[index].State
	if !stepTransitionAllowed(from, to) {
		return errors.New("agent plan-step transition is invalid")
	}
	transition := PlanStepTransition{
		TaskID: input.TaskID, RunID: input.RunID,
		PlanRevision: plan.Revision,
		PlanStepID:   plan.Steps[index].ID,
		From:         from, To: to, Reason: reason,
		ToolRequestID:  toolRequestID,
		ModelRequestID: modelRequestID,
	}
	if err := loop.dependencies.PlanSteps.PersistPlanStepTransition(
		ctx,
		transition,
	); err != nil {
		return err
	}
	plan.Steps[index].State = to
	return nil
}

func stepTransitionAllowed(from, to StepState) bool {
	switch from {
	case StepPending:
		return to == StepInProgress ||
			to == StepFailed ||
			to == StepSkipped
	case StepInProgress:
		return to == StepImplemented ||
			to == StepValidated ||
			to == StepFailed ||
			to == StepSkipped
	case StepImplemented:
		return to == StepValidated ||
			to == StepFailed
	default:
		return false
	}
}

func validStepState(state StepState) bool {
	switch state {
	case StepPending, StepInProgress, StepImplemented, StepValidated,
		StepFailed, StepSkipped:
		return true
	default:
		return false
	}
}

func boundedToolResult(
	result executor.ToolResult,
	requestID string,
	maximumBytes int,
) (executor.ToolResult, error) {
	if result.RequestID != requestID ||
		result.SchemaVersion != executor.ToolSchemaVersion {
		return executor.ToolResult{}, errors.New(
			"tool result identity or schema does not match the request",
		)
	}
	if !validToolResultState(result.State) {
		return executor.ToolResult{}, errors.New("tool result state is invalid")
	}
	remaining := maximumBytes
	result.Summary, result.StdoutTruncated = boundText(
		result.Summary,
		&remaining,
		result.StdoutTruncated,
	)
	result.StdoutRedacted, result.StdoutTruncated = boundText(
		result.StdoutRedacted,
		&remaining,
		result.StdoutTruncated,
	)
	result.StderrRedacted, result.StderrTruncated = boundText(
		result.StderrRedacted,
		&remaining,
		result.StderrTruncated,
	)
	return result, nil
}

func boundText(value string, remaining *int, alreadyTruncated bool) (string, bool) {
	value = strings.ToValidUTF8(value, "\uFFFD")
	if *remaining <= 0 {
		return "", alreadyTruncated || value != ""
	}
	if len(value) <= *remaining {
		*remaining -= len(value)
		return value, alreadyTruncated
	}
	end := *remaining
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	bounded := value[:end]
	*remaining = 0
	return bounded, true
}

func hashToolResult(result executor.ToolResult) (string, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func toolFeedback(
	result executor.ToolResult,
	name executor.ToolName,
) ToolFeedback {
	return ToolFeedback{
		CallID: result.RequestID, Tool: string(name),
		State: result.State, ExitCode: result.ExitCode,
		SummaryRedacted: result.Summary,
		StdoutRedacted:  result.StdoutRedacted,
		StderrRedacted:  result.StderrRedacted,
		Truncated:       result.StdoutTruncated || result.StderrTruncated,
		IsError:         result.State != "succeeded",
	}
}

func failedActionFingerprint(
	request executor.ToolRequest,
	result executor.ToolResult,
) string {
	value := fmt.Sprintf(
		"%s\x00%s\x00%d\x00%t\x00%t",
		executor.ActionSHA256(request),
		result.State,
		result.ExitCode,
		result.TimedOut,
		result.Cancelled,
	)
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func finishLoopOutcome(
	kind OutcomeKind,
	reason string,
	rounds uint32,
	toolCalls uint32,
	tokens domain.TokenCount,
	cost providers.ExactAmount,
	plan PlanProjection,
	validationRequired bool,
) LoopOutcome {
	return LoopOutcome{
		Kind: kind, Reason: reason,
		Rounds: rounds, ToolCalls: toolCalls, Tokens: tokens, Cost: cost,
		Plan: clonePlan(plan), ValidationRequired: validationRequired,
	}
}

func clonePlan(plan PlanProjection) PlanProjection {
	plan.Steps = append([]PlanStep(nil), plan.Steps...)
	for index := range plan.Steps {
		plan.Steps[index].ExpectedFiles = append(
			[]string(nil),
			plan.Steps[index].ExpectedFiles...,
		)
		plan.Steps[index].CompletionTools = append(
			[]executor.ToolName(nil),
			plan.Steps[index].CompletionTools...,
		)
	}
	return plan
}

func cloneToolDeclarations(
	declarations []providers.ToolDeclaration,
) []providers.ToolDeclaration {
	cloned := make([]providers.ToolDeclaration, len(declarations))
	for index, declaration := range declarations {
		cloned[index] = declaration
		cloned[index].InputSchema = append(
			json.RawMessage(nil),
			declaration.InputSchema...,
		)
	}
	return cloned
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

// stepOwningPath finds the one executable plan step that names a tool call's
// path.
//
// It reports false unless exactly one step matches. Zero means the plan never
// asked for this file and the call is reaching outside the contract. Several
// means the plan is ambiguous about who owns it, and guessing there would
// attribute work to a step by luck — which is the failure the step scope
// exists to prevent, not one worth trading for convenience.
func stepOwningPath(plan PlanProjection, request executor.ToolRequest) (int, bool) {
	path, found := requestArgument(request, "path")
	if !found {
		return 0, false
	}
	// A patch states its target twice, and the two can disagree.
	//
	// The tool takes a path argument, and the diff itself opens with
	// "*** Update File: <path>" — which is the line the descriptor tells the
	// model to write, and the one the tool actually applies against. When they
	// differ, the argument is the label and the payload is the work, so a
	// mislabelled argument would otherwise discard an attempt whose patch was
	// aimed at a file the plan names all along.
	//
	// The payload is tried only after the argument, and only when the argument
	// owns nothing: an argument that names a plan file is taken at its word.
	// What stays refused is a patch whose target is named by no step under
	// either reading, which is the contract this check exists to keep.
	declared := patchDeclaredTarget(request)
	// Two passes, and the order is the point. An open step that owns the path
	// is always preferred; a step already closed is considered only when no
	// open one owns it.
	//
	// The closed pass exists because a model that writes one file twice in a
	// turn closed the owning step with its first call, and the second was then
	// owned by nothing and refused — discarding the whole attempt, including
	// the first write. That cost an attempt on rung 1 and on rung 5.
	//
	// The contract that matters is "only write files the plan named", and a
	// second write to a named file does not breach it: it is the model
	// correcting itself, and the file that survives is the one it meant. What
	// stays refused is a path no step names, and a path several name — the
	// first reaches outside the plan, and the second would attribute work by
	// luck.
	for _, candidate := range []string{path, declared} {
		if candidate == "" {
			continue
		}
		if index, ok := stepOwningPathInState(plan, candidate, false); ok {
			return index, true
		}
		if index, ok := stepOwningPathInState(plan, candidate, true); ok {
			return index, true
		}
	}
	return 0, false
}

// patchDeclaredTarget reads the file a patch payload says it changes.
//
// Empty when the payload declares nothing, so a caller falls back to whatever
// the argument said rather than to a guess.
func patchDeclaredTarget(request executor.ToolRequest) string {
	payload, found := requestArgument(request, "patch")
	if !found {
		return ""
	}
	for _, line := range strings.Split(payload, "\n") {
		for _, marker := range []string{"*** Update File:", "*** Add File:"} {
			if rest, cut := strings.CutPrefix(strings.TrimSpace(line), marker); cut {
				return strings.TrimSpace(rest)
			}
		}
	}
	return ""
}

// stepOwningPathInState finds the one step owning a path, either among the
// steps still open or among those already closed.
func stepOwningPathInState(
	plan PlanProjection, path string, closed bool,
) (int, bool) {
	matched, count := 0, 0
	for index, step := range plan.Steps {
		if !stepKindRequiresExpectedFiles(step.Kind) {
			continue
		}
		isClosed := step.State == StepImplemented ||
			step.State == StepValidated ||
			step.State == StepFailed || step.State == StepSkipped
		if isClosed != closed {
			continue
		}
		for _, expected := range step.ExpectedFiles {
			if path == expected || strings.HasPrefix(path, expected+"/") {
				matched, count = index, count+1
				break
			}
		}
	}
	if count != 1 {
		return 0, false
	}
	return matched, true
}

// ValidatePlanStep is the loop's own judgement about one plan step, exported so
// whatever builds a plan can check its work before a run pays for it.
//
// The plan builder and this validator hold the same fact — which tool completes
// which kind of step — and they drifted: the builder listed two tools for the
// edit kind while this requires exactly the one the kind names. Every plan was
// refused before the first prompt, three attempts produced nothing, and the
// failure surfaced as a model-quality problem it was not.
//
// Nothing here is new logic. It is the same check the loop applies, reachable
// from the side that produces the thing being checked, so the two can be
// asserted to agree instead of assumed to.
func ValidatePlanStep(step PlanStep) error {
	return validatePlanStepContract(step)
}

// CanonicalPlanPath is the loop's own rule about what a plan step may name,
// reachable from the side that builds the plan.
//
// Nothing here is new logic. A path this accepts is one validatePlanStepContract
// accepts, which is the point: the coordinator decides a layout and the loop
// then decides whether that layout is admissible, and the two disagreeing means
// a plan refused before the first prompt is sent. That has happened once
// already, over completion tools rather than paths, and cost three attempts and
// a diagnosis that blamed the model.
func CanonicalPlanPath(path string) (string, bool) {
	return canonicalRelativePath(path)
}

// mergeContextByPath replaces the items a refresh re-read and keeps the rest.
//
// A refresher answers for the files it can re-read, which is the files the run
// may change. Everything else in the context — the selection's excerpts, what
// the project already knows — was gathered from sources this cannot consult and
// has not gone stale, so replacing the whole list with the refresher's answer
// would throw away most of what the round is supposed to know.
func mergeContextByPath(
	existing, fresh []RepositoryContextItem,
) []RepositoryContextItem {
	if len(fresh) == 0 {
		return existing
	}
	replaced := make(map[string]RepositoryContextItem, len(fresh))
	for _, item := range fresh {
		replaced[item.Path] = item
	}
	merged := make([]RepositoryContextItem, 0, len(existing)+len(fresh))
	for _, item := range existing {
		if updated, ok := replaced[item.Path]; ok {
			merged = append(merged, updated)
			delete(replaced, item.Path)
			continue
		}
		merged = append(merged, item)
	}
	// Anything the refresh knows about that the context did not: a file this
	// attempt created after the context was read.
	for _, item := range fresh {
		if _, unused := replaced[item.Path]; unused {
			merged = append(merged, item)
			delete(replaced, item.Path)
		}
	}
	return merged
}
