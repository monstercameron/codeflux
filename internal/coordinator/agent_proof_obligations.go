package coordinator

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
)

// obligationStore is the durable half of §9's unit of assurance (PIPE-016).
//
// It is an interface so a run with nothing attached still works: the stages
// record that no obligation could be made durable, rather than claiming one.
type obligationStore interface {
	RaiseProofObligation(
		context.Context, storage.RaiseProofObligation,
	) (storage.ProofObligation, error)
	DischargeProofObligation(
		context.Context, domain.TaskID, uint64, storage.ObligationKind,
		string, pipeline.Number, string,
	) error
	ListProofObligations(
		context.Context, domain.TaskID, uint64,
	) ([]storage.ProofObligation, error)
}

// obligationScope binds the run an obligation belongs to.
type obligationScope struct {
	Store   obligationStore
	TaskID  domain.TaskID
	Attempt uint64
}

// available reports whether obligations can be made durable at all.
func (scope obligationScope) available() bool {
	return scope.Store != nil && !scope.TaskID.IsZero() && scope.Attempt != 0
}

// raiseObligations records one claim per subject and reports what was raised.
//
// A stage that states obligations and cannot store them records skipped: the
// gate is that an obligation exists to be discharged later, and prose in a
// detail string is not one.
func raiseObligations(
	ctx context.Context,
	scope obligationScope,
	kind storage.ObligationKind,
	statements map[string]string,
) (raised []string, err error) {
	if !scope.available() {
		return nil, fmt.Errorf("no durable obligation store is attached")
	}
	subjects := make([]string, 0, len(statements))
	for subject := range statements {
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	for _, subject := range subjects {
		if _, raiseErr := scope.Store.RaiseProofObligation(
			ctx, storage.RaiseProofObligation{
				TaskID: scope.TaskID, Attempt: scope.Attempt,
				Kind: kind, Subject: subject,
				StatementRedacted: statements[subject],
			},
		); raiseErr != nil {
			return raised, raiseErr
		}
		raised = append(raised, subject)
	}
	return raised, nil
}

// dischargeObligations settles the claims a verifying stage established.
//
// Every subject it could not settle is returned rather than swallowed: an
// obligation left open is the finding, and a verifying stage that reported
// satisfied while leaving one open is what §9 exists to prevent.
func dischargeObligations(
	ctx context.Context,
	scope obligationScope,
	kind storage.ObligationKind,
	stage pipeline.Number,
	settled map[string]string,
) (discharged []string, unsettled []string, err error) {
	if !scope.available() {
		return nil, nil, fmt.Errorf("no durable obligation store is attached")
	}
	existing, err := scope.Store.ListProofObligations(
		ctx, scope.TaskID, scope.Attempt)
	if err != nil {
		return nil, nil, err
	}
	for _, obligation := range existing {
		if obligation.Kind != kind {
			continue
		}
		detail, established := settled[obligation.Subject]
		if !established {
			unsettled = append(unsettled, obligation.Subject)
			continue
		}
		if dischargeErr := scope.Store.DischargeProofObligation(
			ctx, scope.TaskID, scope.Attempt, kind,
			obligation.Subject, stage, detail,
		); dischargeErr != nil {
			return discharged, unsettled, dischargeErr
		}
		discharged = append(discharged, obligation.Subject)
	}
	sort.Strings(discharged)
	sort.Strings(unsettled)
	return discharged, unsettled, nil
}

// composeCompositionObligations raises one durable obligation per composition
// and reports what a later stage will have to discharge (PIPE-016).
//
// It replaces a stage that stated obligations in a detail string and reported
// satisfied for having stated them. An obligation nobody can discharge by name
// is a sentence, not §9's unit of assurance.
func composeCompositionObligations(
	ctx context.Context,
	worktree string,
	scope obligationScope,
) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	_, molecules := atomsAndMolecules(functions)
	statements := map[string]string{}
	for _, molecule := range molecules {
		statements[molecule.Name] = fmt.Sprintf(
			"joining %d part(s) in %s produces the whole answer, not merely a "+
				"type-compatible one", len(molecule.Calls), molecule.Name)
	}
	evidence := map[string]any{
		"compositions": len(statements),
		"durable":      scope.available(),
	}
	if len(statements) == 0 {
		return skippedWith(
			"the run produced no composition, so there is nothing to guarantee",
			evidence)
	}
	if !scope.available() {
		return skippedWith(
			"no durable obligation store is attached, so these compositions "+
				"cannot be stated as obligations anything is able to discharge",
			evidence)
	}
	raised, err := raiseObligations(
		ctx, scope, storage.ObligationComposition, statements)
	evidence["raised"] = raised
	if err != nil {
		return broke("the composition obligations could not be recorded: "+
			err.Error(), evidence)
	}
	return held(fmt.Sprintf(
		"%d composition obligation(s) recorded for molecule-verification to "+
			"discharge by name", len(raised)), evidence)
}

// composeControlObligations raises one durable obligation per branching
// function.
//
// The unit is the function that declares the paths, matching PIPE-015: an
// obligation per individual path needs the path itself to have an identity,
// which nothing yet derives.
func composeControlObligations(
	ctx context.Context,
	worktree string,
	scope obligationScope,
) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	statements := map[string]string{}
	paths := 0
	for _, function := range functions {
		if isTestScaffolding(function) || function.Branches == 0 {
			continue
		}
		paths += function.Branches
		statements[function.Name] = fmt.Sprintf(
			"every one of the %d path(s) through %s terminates and reports its "+
				"outcome rather than exiting silently",
			function.Branches, function.Name)
	}
	evidence := map[string]any{
		"branching_functions": len(statements),
		"decision_points":     paths,
		"durable":             scope.available(),
		"unit": "one branching function, not one individual path: a path has " +
			"no identity of its own until something derives one",
	}
	if len(statements) == 0 {
		return skippedWith(
			"the program takes no decision, so every run follows one path",
			evidence)
	}
	if !scope.available() {
		return skippedWith(
			"no durable obligation store is attached, so these paths cannot be "+
				"stated as obligations anything is able to discharge", evidence)
	}
	raised, err := raiseObligations(
		ctx, scope, storage.ObligationControlPath, statements)
	evidence["raised"] = raised
	if err != nil {
		return broke("the control obligations could not be recorded: "+
			err.Error(), evidence)
	}
	return held(fmt.Sprintf(
		"%d control obligation(s) recorded for control-flow to discharge by name",
		len(raised)), evidence)
}

// dischargeStage settles obligations of one kind and turns what is left open
// into the stage's verdict.
func dischargeStage(
	ctx context.Context,
	scope obligationScope,
	kind storage.ObligationKind,
	stage pipeline.Number,
	settled map[string]string,
	evidence map[string]any,
	holdDetail func(discharged int) string,
) stageOutcome {
	if evidence == nil {
		evidence = map[string]any{}
	}
	evidence["durable"] = scope.available()
	if !scope.available() {
		return skippedWith(
			"no durable obligation store is attached, so nothing can be "+
				"discharged by name", evidence)
	}
	discharged, unsettled, err := dischargeObligations(
		ctx, scope, kind, stage, settled)
	evidence["discharged"] = discharged
	evidence["left_open"] = unsettled
	if err != nil {
		return broke("the obligations could not be settled: "+err.Error(), evidence)
	}
	if len(discharged) == 0 && len(unsettled) == 0 {
		return skippedWith(
			"no obligation of this kind was raised, so there is nothing to "+
				"discharge", evidence)
	}
	if len(unsettled) > 0 {
		return broke(fmt.Sprintf(
			"%d obligation(s) were left open: %s",
			len(unsettled), strings.Join(unsettled, ", ")), evidence)
	}
	return held(holdDetail(len(discharged)), evidence)
}

// dischargeControlFlow settles the control obligations this run established.
//
// A branching function is settled when its own check holds for it: the
// existing control-flow analysis decides, and its verdict is what discharges
// the obligation rather than a separate assertion.
func dischargeControlFlow(
	ctx context.Context,
	worktree string,
	scope obligationScope,
) stageOutcome {
	outcome := checkControlFlow(worktree)
	// A control-flow analysis that could not run settles nothing. Passing its
	// own failure through keeps the reason rather than replacing it with an
	// obligation count.
	if !outcome.Held && !outcome.Skipped {
		return outcome
	}

	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	settled := map[string]string{}
	for _, function := range functions {
		if isTestScaffolding(function) || function.Branches == 0 {
			continue
		}
		settled[function.Name] = fmt.Sprintf(
			"control-flow analysis found every path through %s terminating",
			function.Name)
	}
	return dischargeStage(ctx, scope, storage.ObligationControlPath,
		pipeline.StageControlFlow, settled,
		map[string]any{"control_flow_detail": outcome.Detail},
		func(discharged int) string {
			return fmt.Sprintf(
				"%d control obligation(s) discharged by control-flow analysis",
				discharged)
		})
}

// dischargeMoleculeVerification settles the composition obligations.
func dischargeMoleculeVerification(
	ctx context.Context,
	worktree string,
	scope obligationScope,
) stageOutcome {
	outcome := checkMoleculeVerification(ctx, worktree)
	if !outcome.Held && !outcome.Skipped {
		return outcome
	}

	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	_, molecules := atomsAndMolecules(functions)
	settled := map[string]string{}
	for _, molecule := range molecules {
		settled[molecule.Name] = fmt.Sprintf(
			"molecule-verification exercised %s as a composition", molecule.Name)
	}
	return dischargeStage(ctx, scope, storage.ObligationComposition,
		pipeline.StageMoleculeVerification, settled,
		map[string]any{"verification_detail": outcome.Detail},
		func(discharged int) string {
			return fmt.Sprintf(
				"%d composition obligation(s) discharged by molecule-verification",
				discharged)
		})
}
