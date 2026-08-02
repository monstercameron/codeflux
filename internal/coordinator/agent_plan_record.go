package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/storage"
)

// durablePlan is the recorded plan a run's work is attributed to.
//
// Every graph node an operation produces binds to a plan revision, so a run
// that recorded no plan could not draw a single thing it did: the diagram
// refused each operation with a constraint failure and the panel stayed empty.
// This records the plan the run is actually about to carry out.
type durablePlan struct {
	Revision uint64
	Steps    map[string]string
}

// recordDurablePlan writes the requirement and the plan the run will follow.
//
// It uses the product's own deterministic requirement analysis and plan
// builder rather than inventing a shape: those decide the task type, the
// validation profile, and which completion tool each kind of step allows, and
// a plan assembled by hand here would drift from the one everything else
// validates against.
func (execution *AgentExecution) recordDurablePlan(
	ctx context.Context,
	scope agentScope,
	steps []agentloop.PlanStep,
	attempt uint64,
) (durablePlan, error) {
	analysis, err := storage.AnalyzeTaskRequirement(scope.requirement)
	if err != nil {
		return durablePlan{}, fmt.Errorf("analyse the requirement: %w", err)
	}
	// The analysis is deliberately not adjusted here. The store re-derives it
	// from the stored message and requires an exact match, because the analysis
	// is defined as a function of what the person actually wrote. Anything that
	// needs the risk to line up has to line it up at intake, where the decision
	// is made and shown, not here where the work is already approved.
	// The requirement is recorded first, and the revision it was given is used
	// from then on. This assumed the answer was always one, and a run whose
	// requirement was refused went on to record a plan against a revision that
	// did not exist — reporting a missing requirement rather than the refusal
	// that caused it, which is two failures away from the real one.
	requirementRevision := uint64(1)
	if scope.requestMessage != nil {
		recorded, err := execution.repositories.RecordTaskRequirement(
			ctx, storage.RecordTaskRequirement{
				TaskID: scope.taskID, MessageID: *scope.requestMessage,
				Analysis:       analysis,
				IdempotencyKey: fmt.Sprintf("agent-requirement:%s", scope.taskID),
			},
		)
		switch {
		case err == nil:
			requirementRevision = recorded.Revision
		case errors.Is(err, storage.ErrConflict):
			// The same run recorded this requirement already. The existing one
			// is the one to plan against.
			existing, readErr := execution.repositories.GetTaskRequirement(
				ctx, scope.taskID, requirementRevision)
			if readErr != nil {
				return durablePlan{}, fmt.Errorf(
					"read the recorded requirement: %w", readErr)
			}
			requirementRevision = existing.Revision
		default:
			return durablePlan{}, fmt.Errorf("record the requirement: %w", err)
		}
	}

	// The scope and expected files are the paths the run will touch. A plan with
	// no scope is refused, and correctly: a plan that does not say what it may
	// change cannot be checked against what it did.
	files := agentPlanFiles(steps)
	draft := storage.AgentPlanDraft{
		Goal:               shortGraphLabel(scope.requirement, "the request"),
		Scope:              files,
		ExpectedFiles:      files,
		ValidationCommands: canonicalTestCommand(),
		CompletionCriteria: []string{"The declared files exist and the tests pass."},
	}
	for _, step := range steps {
		draft.Steps = append(draft.Steps, storage.AgentPlanStepDraft{
			Kind:           durablePlanStepKind(step),
			Title:          shortGraphLabel(step.SummaryRedacted, step.ID),
			DetailRedacted: step.SummaryRedacted,
			ExpectedFiles:  step.ExpectedFiles,
		})
	}
	plan, err := storage.BuildAgentPlan(analysis, draft)
	if err != nil {
		return durablePlan{}, fmt.Errorf("build the plan: %w", err)
	}

	forecast, err := execution.repositories.GetCurrentTaskForecast(ctx, scope.taskID)
	if err != nil {
		return durablePlan{}, fmt.Errorf("read the task forecast: %w", err)
	}
	budget, err := execution.repositories.GetBudgetSnapshot(ctx, scope.taskID)
	if err != nil {
		return durablePlan{}, fmt.Errorf("read the task budget: %w", err)
	}
	// The context the plan was made against is recorded before the plan, because
	// a plan points at it: a plan naming a manifest that does not exist is a
	// plan whose inputs cannot be checked, and the store refuses it.
	manifestID, err := execution.recordPlanContext(ctx, scope, plan, attempt)
	if err != nil {
		return durablePlan{}, err
	}
	recorded, err := execution.repositories.RecordPlanRevision(ctx, storage.RecordPlanRevision{
		TaskID: scope.taskID, RequirementRevision: requirementRevision,
		RepositoryRevision:     scope.revision,
		ContextManifestID:      manifestID,
		PolicyRevision:         forecast.PolicyRevision,
		ForecastRevision:       forecast.ForecastRevision,
		BudgetID:               budget.BudgetID,
		BudgetLimitRevision:    budget.LimitRevision,
		BudgetSnapshotRevision: budget.Revision,
		Plan:                   plan,
		IdempotencyKey:         fmt.Sprintf("agent-plan:%s:%d", scope.taskID, attempt),
	})
	if err != nil {
		return durablePlan{}, fmt.Errorf("record the plan: %w", err)
	}

	// The loop's step identities and the durable plan's are different
	// vocabularies, so they are paired by position. A run attributing its work
	// to a step the plan does not have would be worse than no attribution.
	result := durablePlan{Revision: recorded.Revision, Steps: map[string]string{}}
	for index, step := range steps {
		if index < len(plan.Steps) {
			result.Steps[step.ID] = plan.Steps[index].ID
		}
	}
	return result, nil
}

// durablePlanStepKind maps a loop step onto the plan vocabulary.
//
// The kinds are closed and each one fixes which tool may complete it, so the
// mapping is explicit rather than a string conversion that would silently
// produce an invalid plan.
func durablePlanStepKind(step agentloop.PlanStep) storage.AgentPlanStepKind {
	switch step.Kind {
	case agentloop.StepKindEdit:
		return storage.StepKindEdit
	case agentloop.StepKindTest:
		return storage.StepKindTest
	case agentloop.StepKindBuild:
		return storage.StepKindBuild
	default:
		return storage.StepKindReadFile
	}
}

// agentPlanFiles is every path the plan's steps declare.
func agentPlanFiles(steps []agentloop.PlanStep) []string {
	seen := map[string]bool{}
	var files []string
	for _, step := range steps {
		for _, file := range step.ExpectedFiles {
			if seen[file] {
				continue
			}
			seen[file] = true
			files = append(files, file)
		}
	}
	return files
}

// canonicalTestCommand renders the project's test command in the exact form a
// plan may carry.
//
// A plan's validation commands are stored canonically rather than as typed
// text, so that "the plan said to run this" and "this is what ran" compare as
// values. A human string is refused, and correctly.
func canonicalTestCommand() []string {
	rendered, err := executor.RenderValidationPlanCommand(executor.ToolRequest{
		SchemaVersion: executor.ToolSchemaVersion,
		Name:          executor.ToolTest,
		Arguments: []executor.ToolArgument{
			{Name: "executable", Value: "go"},
			{Name: "arg1", Value: "test"},
			{Name: "arg2", Value: "./..."},
		},
		Timeout:          4 * time.Minute,
		ClaimedAuthority: executor.AuthorityAutomaticRead,
		ExpectedSideEffects: []executor.SideEffect{
			executor.EffectRepositoryRead, executor.EffectSubprocess,
		},
	})
	if err != nil {
		return nil
	}
	return []string{rendered}
}

// contextManifestIdentity digests the exact context this plan was made against.
//
// It is a hash rather than a label because it identifies content: two plans
// made against the same task, revision, and shape are the same context, and two
// made against different ones must not collide. The store requires a 64
// character lowercase digest for that reason.
func contextManifestIdentity(
	scope agentScope,
	plan storage.AgentPlan,
	attempt uint64,
) string {
	digest := sha256.Sum256(fmt.Appendf(nil, "%s|%s|%d|%s",
		scope.taskID, scope.revision, attempt, strings.Join(plan.ExpectedFiles, ",")))
	return hex.EncodeToString(digest[:])
}

// recordPlanContext records the repository context this plan was made against.
//
// The items are the same ones the model was shown, with their real digests, so
// what the plan was decided from can be checked later against what those files
// actually held.
func (execution *AgentExecution) recordPlanContext(
	ctx context.Context,
	scope agentScope,
	plan storage.AgentPlan,
	attempt uint64,
) (string, error) {
	items := worktreeContextItems(scope.worktree)
	manifest := storage.RecordContextManifest{
		ID:                 contextManifestIdentity(scope, plan, attempt),
		RepositoryID:       scope.repositoryID,
		RepositoryRevision: scope.revision,
		// The map revision identifies the symbol map this context was selected
		// against. No map is built yet, so it is the digest of the repository
		// revision: stable, distinct per revision, and honestly derived rather
		// than a constant that would claim two different trees were the same.
		MapRevision:        digestOf(scope.revision),
		RequirementSHA256:  digestOf(scope.requirement),
		SelectionPolicy:    1,
		MaxFiles:           64,
		MaxBytes:           1 << 20,
		MaxEstimatedTokens: 200000,
	}
	for _, item := range items {
		manifest.Items = append(manifest.Items, storage.ContextManifestItem{
			Path: item.Path, Kind: contextItemKind(item.Path),
			// The whole file was shown, so the excerpt is the whole file. Lines
			// are one-based, and a zero start would claim an excerpt nobody read.
			StartLine:       1,
			EndLine:         lineCountOf(item.ContentRedacted),
			ContentRedacted: item.ContentRedacted,
			ContentSHA256:   item.ContentSHA256,
			// A token count nobody measured is estimated at four bytes per token
			// and recorded per item, so the manifest's total is the sum of what
			// it actually holds rather than a number computed beside it.
			EstimatedTokens: len(item.ContentRedacted)/4 + 1,
			Reasons:         []string{"seeded-repository-context"},
			// Repository content is untrusted input: it is whatever happens to be
			// on disk, and a file can say anything. The store accepts one label
			// for exactly that reason.
			Trust: "untrusted-repository-data",
		})
		manifest.UsedBytes += len(item.ContentRedacted)
		manifest.UsedEstimatedTokens += len(item.ContentRedacted)/4 + 1
	}
	manifest.UsedFiles = len(manifest.Items)
	if _, err := execution.repositories.RecordContextManifest(ctx, manifest); err != nil {
		return "", fmt.Errorf("record plan revision context: %w", err)
	}
	return manifest.ID, nil
}

// digestOf hashes one string.
func digestOf(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

// contextItemKind names what one seeded context item is.
//
// The kinds are a closed set, and each one means something to a reader deciding
// whether the context was well chosen. Calling everything a source file would
// erase that distinction.
func contextItemKind(path string) string {
	switch {
	case strings.HasSuffix(path, "_test.go"):
		return "test"
	case strings.HasSuffix(path, "go.mod"), strings.HasSuffix(path, "go.sum"):
		return "module"
	case strings.HasSuffix(path, ".md"):
		return "instruction"
	case strings.HasSuffix(path, ".go"):
		return "source"
	default:
		return "configuration"
	}
}

// lineCountOf counts the lines one excerpt spans, never fewer than one.
func lineCountOf(content string) int {
	if content == "" {
		return 1
	}
	count := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		count++
	}
	if count < 1 {
		return 1
	}
	return count
}
