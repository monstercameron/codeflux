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
	ErrUnknownTool        = errors.New("agent model requested an unknown tool")
	ErrAccountingUnknown  = errors.New("agent usage or cost accounting is unknown")
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
	var (
		rounds          uint32
		toolCalls       uint32
		tokens          domain.TokenCount
		previousResults []ToolFeedback
	)
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
					return LoopOutcome{}, err
				}
				stepIndex = corrected
				if err := validateToolStepCompatibility(
					plan.Steps[stepIndex], tool,
				); err != nil {
					return LoopOutcome{}, err
				}
			}
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
				PlanStepID:            proposed.PlanStepID,
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
						PlanStepID:     proposed.PlanStepID,
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
					PlanStepID:   proposed.PlanStepID,
					Round:        rounds,
					RequestID:    decoded.request.ID,
					Result:       result,
					ResultSHA256: resultDigest,
				},
			); err != nil {
				return LoopOutcome{}, err
			}
			succeeded := result.State == "succeeded"
			if tool.MaterialEdit && succeeded {
				if err := loop.dependencies.Checkpoints.CreateCheckpoint(
					context.WithoutCancel(ctx),
					CheckpointRequest{
						TaskID: input.TaskID, RunID: input.RunID,
						PlanRevision:   plan.Revision,
						PlanStepID:     proposed.PlanStepID,
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
			if succeeded {
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
					if plan.Steps[stepIndex].State != StepFailed {
						if err := loop.transitionStep(
							context.WithoutCancel(ctx),
							input,
							&plan,
							stepIndex,
							StepFailed,
							"identical failed action reached the stop threshold",
							decoded.request.ID,
							turn.ModelRequestID,
						); err != nil {
							return LoopOutcome{}, err
						}
					}
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
		return fmt.Errorf(
			"%w: turn contains neither tool calls nor completion",
			ErrMalformedModelTurn,
		)
	}
	if len(turn.ToolCalls) != 0 && turn.Completion != CompletionNone {
		return fmt.Errorf(
			"%w: tool calls and completion are mutually exclusive",
			ErrMalformedModelTurn,
		)
	}
	if uint32(len(turn.ToolCalls)) > perRound {
		return fmt.Errorf(
			"%w: turn exceeds the per-round tool-call bound",
			ErrMalformedModelTurn,
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
		if step.State == StepImplemented ||
			step.State == StepValidated ||
			step.State == StepFailed ||
			step.State == StepSkipped {
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
	completionTool     executor.ToolName
	materialEdit       bool
	validationRequired bool
}

func validatePlanStepContract(step PlanStep) error {
	contract, ok := planStepContract(step.Kind)
	if !ok {
		return errors.New("agent plan step kind is invalid")
	}
	if len(step.CompletionTools) != 1 ||
		step.CompletionTools[0] != contract.completionTool {
		return errors.New(
			"agent plan step completion tools differ from its kind",
		)
	}
	if step.MaterialEdit != contract.materialEdit ||
		step.ValidationRequired != contract.validationRequired {
		return errors.New("agent plan step semantics differ from its kind")
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
	case StepKindEdit, StepKindReadFile, StepKindListDirectory,
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
	if index, ok := stepOwningPathInState(plan, path, false); ok {
		return index, true
	}
	return stepOwningPathInState(plan, path, true)
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
