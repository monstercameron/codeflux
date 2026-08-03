package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/retrieval/recallkey"
	"codeflux.dev/codeflux/internal/storage"
)

// registrationRecord is what became of one produced declaration's
// registration attempt (PIPE-048, PIPE-049): either it was admitted and
// persisted, or it was not, and why.
type registrationRecord struct {
	Name         string
	Registered   bool
	RevisionID   string
	AtomID       string
	ContractHash string
	// SkipReason is set whenever Registered is false. It is never empty in
	// that case: a registration attempt that did nothing always says why,
	// the same discipline recallDecision's Justification already follows.
	SkipReason string
}

// registerVerifiedAtoms is StageAtomRegistration's performer (PIPE-048).
//
// It writes one atom_documentation_revisions row for every produced
// function that (a) calls no other produced function -- the flow's own
// definition of an atom, carried on producedFunction.Calls -- and (b)
// carries a schema-v1 //codeflux:atom documentation comment on its
// declaration, the only authoring mode internal/atomdoc's
// ClassifyAtomDocumentationAuthoring recognises for a Go-implemented atom.
// A produced function missing that comment is not silently registered with
// invented content: it is recorded as not registered, with the reason, so
// the stage's own evidence states plainly that documentation -- not this
// stage -- is what is missing.
//
// It never runs before the atom it registers has already been produced and
// examined by this same run (recallKnownAtoms calls it after
// bindRecallDecisions), so every row it writes carries scope.revision as
// SourceRepositoryRevision: "the exact revision the atom was verified at"
// PIPE-048 asks for.
func (execution *AgentExecution) registerVerifiedAtoms(
	ctx context.Context,
	scope agentScope,
	worktree string,
	atoms map[string]producedFunction,
	registry map[atomdoc.ContractHash]storage.AtomDocumentationRevision,
) []registrationRecord {
	names := sortedFunctionNames(atoms)
	records := make([]registrationRecord, 0, len(names))
	for _, name := range names {
		records = append(records, admitProducedDeclaration(
			ctx, execution, scope, worktree, atoms[name], nil, registry))
	}
	return records
}

// registerVerifiedMolecules is StageMoleculeRegistration's performer
// (PIPE-049): atom-registration's own admission pipeline, run over every
// produced function that composes at least one other produced function
// (producedFunction.Calls is non-empty), "on the same terms" as an atom.
//
// The one addition PIPE-049 asks for is recorded here rather than in
// admitProducedDeclaration: dependencyBindingsForMolecule turns the
// composed function names into atomdoc.DependencyBinding entries, so the
// persisted revision names the atoms it composes as its parts, not merely
// that it composes something.
func (execution *AgentExecution) registerVerifiedMolecules(
	ctx context.Context,
	scope agentScope,
	worktree string,
	molecules map[string]producedFunction,
	registry map[atomdoc.ContractHash]storage.AtomDocumentationRevision,
) []registrationRecord {
	names := sortedFunctionNames(molecules)
	records := make([]registrationRecord, 0, len(names))
	for _, name := range names {
		function := molecules[name]
		records = append(records, admitProducedDeclaration(
			ctx, execution, scope, worktree, function,
			dependencyBindingsForMolecule(function, scope), registry))
	}
	return records
}

// dependencyBindingsForMolecule renders a molecule's composed atoms as
// dependency bindings, deduplicated and sorted for a deterministic
// registered row. atomdoc.DependencyBinding requires a non-empty Version, so
// each binding is stamped with the repository revision this run built
// against -- the same value SourceRepositoryRevision carries -- rather than
// a meaningless placeholder.
func dependencyBindingsForMolecule(
	function producedFunction, scope agentScope,
) []atomdoc.DependencyBinding {
	version := scope.revision
	if version == "" {
		version = "unknown-revision"
	}
	calls := append([]string(nil), function.Calls...)
	sort.Strings(calls)
	bindings := make([]atomdoc.DependencyBinding, 0, len(calls))
	seen := map[string]bool{}
	for _, call := range calls {
		if call == "" || seen[call] {
			continue
		}
		seen[call] = true
		bindings = append(bindings, atomdoc.DependencyBinding{Name: call, Version: version})
	}
	return bindings
}

// admitProducedDeclaration runs one produced function through
// internal/atomdoc's real admission pipeline and, on success, persists the
// result through storage.CreateAtomDocumentationRevision. It is the shared
// core registerVerifiedAtoms and registerVerifiedMolecules both call.
//
// Registration never invents an atom identity across runs by minting a
// fresh one every time: registry, read once by loadRegisteredContractIndex
// before this run registers anything, is consulted first. When the same
// exact contract hash is already registered, this declaration is admitted
// as a new revision of that SAME atom identity and version, so a rename or
// a re-verification of unchanged behaviour does not fork the registry's
// identity for something recall would otherwise recognise as the same
// contract. Only a genuinely new contract mints a fresh domain.AtomID.
// Resolving a stable identity by declared *name* (surviving a change of
// contract, the inverse case) is the separate atom-naming milestone
// (AGENTS.md "Atom Naming Style", M21-153..168) and is deliberately not
// reimplemented here.
//
// Every branch that does not register something sets SkipReason and returns
// rather than panicking or erroring the caller: a registration stage that
// cannot register one declaration must not stop the run, the same posture
// recallKnownAtoms's own storage reads already take.
func admitProducedDeclaration(
	ctx context.Context,
	execution *AgentExecution,
	scope agentScope,
	worktree string,
	function producedFunction,
	dependencies []atomdoc.DependencyBinding,
	registry map[atomdoc.ContractHash]storage.AtomDocumentationRevision,
) registrationRecord {
	record := registrationRecord{Name: function.Name}

	if execution == nil || execution.repositories == nil {
		record.SkipReason = "no repository is attached to this run, so nothing could be persisted"
		return record
	}
	if execution.redactor == nil {
		record.SkipReason = "no redaction pipeline is attached to this run, so admission was refused rather than persisting unscrubbed content"
		return record
	}
	if function.File == "" {
		record.SkipReason = "the produced declaration carries no source file"
		return record
	}

	fileSet := token.NewFileSet()
	tree, err := parser.ParseFile(
		fileSet, filepath.Join(worktree, function.File), nil, parser.ParseComments)
	if err != nil {
		record.SkipReason = "the produced source could not be parsed: " + err.Error()
		return record
	}
	candidates, err := atomdoc.LocateAtomDeclarationCandidates(fileSet, tree)
	if err != nil {
		record.SkipReason = "the file's atom directive could not be located: " + err.Error()
		return record
	}
	var candidate *atomdoc.SourceCandidate
	for index := range candidates {
		if candidates[index].Identifier == function.Name {
			candidate = &candidates[index]
			break
		}
	}
	if candidate == nil {
		record.SkipReason = "no //codeflux:atom schema-v1 documentation comment is attached to this declaration"
		return record
	}

	contract := normalizeWantedContract(function)
	contractHash := recallkey.ComputeContractHash(contract)

	atomID, atomVersionNumber := resolveRegistrationIdentity(registry, contractHash)
	if atomID.IsZero() {
		minted, mintErr := domain.NewAtomID()
		if mintErr != nil {
			record.SkipReason = "an atom identity could not be minted: " + mintErr.Error()
			return record
		}
		atomID = minted
		atomVersionNumber = 1
	}
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, atomVersionNumber)
	if err != nil {
		record.SkipReason = "an atom-version identity could not be constructed: " + err.Error()
		return record
	}

	admission, err := atomdoc.AdmitSourceAtomDocumentation(ctx, atomdoc.SourceAdmissionInput{
		Candidate:                *candidate,
		AtomID:                   atomID,
		AtomVersion:              atomVersion,
		SourceRepositoryRevision: scope.revision,
		ContractHash:             contractHash,
		DependencyBindings:       dependencies,
		RedactionPipeline:        execution.redactor,
	})
	if err != nil {
		record.SkipReason = "admission could not run: " + err.Error()
		return record
	}
	if admission.Status != atomdoc.AdmissionStatusAdmitted || admission.Revision == nil {
		record.SkipReason = fmt.Sprintf(
			"documentation was rejected (%s): %s",
			admission.RejectionReason, admission.RejectionDetail)
		return record
	}

	persisted, err := execution.repositories.CreateAtomDocumentationRevision(ctx,
		storage.CreateAtomDocumentationRevision{
			ProjectID: scope.projectID,
			Revision:  *admission.Revision,
		})
	if err != nil {
		record.SkipReason = "the registry row could not be persisted: " + err.Error()
		return record
	}

	record.Registered = true
	record.RevisionID = persisted.RevisionID.String()
	record.AtomID = persisted.AtomID.String()
	record.ContractHash = contractHash.String()
	return record
}

// resolveRegistrationIdentity looks for an already-registered atom sharing
// contractHash exactly, so registering an unchanged contract again -- a
// rename, or a plain re-verification -- extends that atom's identity rather
// than forking a new one. A zero domain.AtomID return means none was found
// and the caller must mint a fresh one.
func resolveRegistrationIdentity(
	registry map[atomdoc.ContractHash]storage.AtomDocumentationRevision,
	hash atomdoc.ContractHash,
) (domain.AtomID, uint32) {
	if existing, found := registry[hash]; found {
		return existing.AtomID, existing.AtomVersion.Number()
	}
	return domain.AtomID{}, 0
}

// sortedFunctionNames returns a map's keys in a deterministic order, used
// throughout this file so two runs over identical input register in
// identical order and produce identical evidence ordering.
func sortedFunctionNames(functions map[string]producedFunction) []string {
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// loadRegisteredContractIndex reads the atom registry "as it now stands"
// (PIPE-054's phrase): every currently admitted revision in this project,
// indexed by contract hash, first-admitted-wins. It is read once per
// recallKnownAtoms call, before this run registers anything of its own, so
// both the reuse-regret classification (PIPE-054/PIPE-055) and the
// cross-run atom-identity resolution (resolveRegistrationIdentity) see the
// registry as prior work left it -- not a registry this run's own writes
// have already started changing underneath it.
//
// A read failure or an unattached repository returns an empty index rather
// than propagating an error: a registry read that cannot run must not stop
// recall, matching every other storage read in this file's posture.
func loadRegisteredContractIndex(
	ctx context.Context, execution *AgentExecution, scope agentScope,
) map[atomdoc.ContractHash]storage.AtomDocumentationRevision {
	index := map[atomdoc.ContractHash]storage.AtomDocumentationRevision{}
	if execution == nil || execution.repositories == nil || scope.projectID.IsZero() {
		return index
	}
	revisions, err := execution.repositories.ListAtomDocumentationRevisionsByProject(
		ctx, scope.projectID, 500)
	if err != nil {
		return index
	}
	for _, revision := range revisions {
		if _, exists := index[revision.ContractHash]; !exists {
			index[revision.ContractHash] = revision
		}
	}
	return index
}

// currentAttemptBestEffort returns the pipeline attempt number this run's
// own ledger is, in practice, already writing under.
//
// recallKnownAtoms runs late in AgentExecution.Run (agent_execution.go),
// after many earlier stages of the same attempt have already been recorded
// through the pipelineLedger type there. storage.NextPipelineAttempt reads
// MAX(attempt)+1 for the task, so by the time this function is called it
// reads one past the attempt actually in progress -- subtracting one
// recovers it without needing the pipelineLedger value itself, which
// recallKnownAtoms's call site (agent_execution.go) does not pass in.
//
// This is a best-effort read, not a substitute for holding the ledger's own
// attempt number: a task with no rows recorded yet (a fixture calling
// recallKnownAtoms directly, never through Run) reads attempt one, which is
// also the correct default for that case. The narrow risk this cannot rule
// out -- a second run for the same task starting concurrently between the
// read this depends on and the write it makes -- is the same bounded risk
// storage.NextPipelineAttempt's own doc comment already accepts.
func currentAttemptBestEffort(
	ctx context.Context, repositories *storage.Repositories, taskID domain.TaskID,
) uint64 {
	if repositories == nil || taskID.IsZero() {
		return 1
	}
	next, err := repositories.NextPipelineAttempt(ctx, taskID)
	if err != nil || next <= 1 {
		return 1
	}
	return next - 1
}

// recordRegistrationStageResult writes StageAtomRegistration's or
// StageMoleculeRegistration's own pipeline_stage_records row directly
// through storage.RecordPipelineStageResult, rather than through the
// pipelineLedger type recallKnownAtoms's caller (agent_execution.go, not
// owned by this change) constructs and holds.
//
// This is a deliberate, narrow exception to this package's usual "the
// ledger is the only writer" shape, made because the alternative is that a
// registration stage which genuinely runs and genuinely writes registry
// rows would leave no ledger row of its own at all, reading identically to
// a stage nothing performs -- which AGENTS.md's own distinction between
// skipped and not-implemented says two different things must never read
// the same way. RecordPipelineStageResult's own INSERT is idempotent
// (`ON CONFLICT (task_id, attempt, stage_number) DO NOTHING`), so calling
// it here even if agent_execution.go is later restructured to also call it
// through the ledger is safe: whichever write happens first wins, and the
// second is a no-op rather than a conflicting second answer.
func recordRegistrationStageResult(
	ctx context.Context,
	execution *AgentExecution,
	scope agentScope,
	stage pipeline.Number,
	kind string,
	records []registrationRecord,
) {
	if execution == nil || execution.repositories == nil || scope.taskID.IsZero() {
		return
	}
	attempt := currentAttemptBestEffort(ctx, execution.repositories, scope.taskID)

	registered := 0
	var names []string
	skipReasons := map[string]int{}
	for _, record := range records {
		if record.Registered {
			registered++
			names = append(names, record.Name)
			continue
		}
		if record.SkipReason != "" {
			skipReasons[record.SkipReason]++
		}
	}
	sort.Strings(names)

	state := pipeline.StateSkipped
	var detail string
	switch {
	case len(records) == 0:
		detail = fmt.Sprintf("this run produced no candidate %s", kind)
	case registered > 0:
		state = pipeline.StateSatisfied
		detail = fmt.Sprintf("%d of %d produced %s(s) registered with schema-v1 documentation",
			registered, len(records), kind)
	default:
		detail = fmt.Sprintf(
			"0 of %d produced %s(s) carried schema-v1 //codeflux:atom documentation eligible for registration",
			len(records), kind)
	}

	evidence := map[string]any{
		"candidates":   len(records),
		"registered":   names,
		"skip_reasons": skipReasons,
	}
	// Evidence is passed through storage.RecordPipelineStage as map[string]any
	// and marshalled there; json.Marshal is called here only to fail fast
	// (and skip the write) if this evidence ever stopped being JSON-safe,
	// rather than let a malformed row reach the ledger's own storage layer.
	if _, marshalErr := json.Marshal(evidence); marshalErr != nil {
		return
	}

	_, _ = execution.repositories.RecordPipelineStageResult(ctx, storage.RecordPipelineStage{
		TaskID:         scope.taskID,
		Attempt:        attempt,
		Stage:          stage,
		State:          state,
		DetailRedacted: detail,
		Evidence:       evidence,
		StartedAt:      time.Now(),
	})
}

// registerAndRecordProducedDeclarations is recallKnownAtoms's single call
// into this file's machinery (PIPE-048, PIPE-049): it splits this run's own
// produced functions into atoms and molecules using the flow's own
// definition -- producedFunction.Calls empty is an atom, non-empty composes
// -- registers each set, and writes each stage's own best-effort ledger row.
func (execution *AgentExecution) registerAndRecordProducedDeclarations(
	ctx context.Context,
	scope agentScope,
	worktree string,
	wanted map[string]producedFunction,
	registry map[atomdoc.ContractHash]storage.AtomDocumentationRevision,
) (atomRecords, moleculeRecords []registrationRecord) {
	atoms := map[string]producedFunction{}
	molecules := map[string]producedFunction{}
	for name, function := range wanted {
		if len(function.Calls) == 0 {
			atoms[name] = function
		} else {
			molecules[name] = function
		}
	}

	atomRecords = execution.registerVerifiedAtoms(ctx, scope, worktree, atoms, registry)
	recordRegistrationStageResult(ctx, execution, scope, pipeline.StageAtomRegistration, "atom", atomRecords)

	moleculeRecords = execution.registerVerifiedMolecules(ctx, scope, worktree, molecules, registry)
	recordRegistrationStageResult(ctx, execution, scope, pipeline.StageMoleculeRegistration, "molecule", moleculeRecords)

	return atomRecords, moleculeRecords
}
