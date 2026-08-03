package coordinator

import (
	"context"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/retrieval/recallkey"
	"codeflux.dev/codeflux/internal/storage"
)

// registrationTestRedactionPipeline builds a real, minimal redaction
// pipeline, the same shape internal/storage's own atom-documentation tests
// use (mustTestRedactionPipeline there): admitProducedDeclaration refuses to
// run at all without one attached to AgentExecution.redactor, matching
// AGENTS.md "Redact secrets before persistence ... prompts".
func registrationTestRedactionPipeline(t *testing.T) *redact.Pipeline {
	t.Helper()
	pipeline, err := redact.NewPipeline(nil, redact.Limits{
		MaximumInputBytes:  32 * 1024,
		MaximumOutputBytes: 16 * 1024,
	})
	if err != nil {
		t.Fatalf("construct redaction pipeline: %v", err)
	}
	t.Cleanup(pipeline.Close)
	return pipeline
}

// atomRegistrationFixtureSource is a synthetic, complete schema-v1 comment
// on a pure atom with no calls into other produced code, so
// registerVerifiedAtoms (not registerVerifiedMolecules) is what admits it.
const atomRegistrationFixtureSource = `package reserve

// ReserveFundsForCheckout reserves an amount against an account without
// capturing it.
//
//codeflux:atom
// Codeflux atom documentation (schema v1):
//   Purpose:
//     Hold a requested amount against an account balance so it cannot be
//     spent by a second concurrent operation before capture.
//   Use when:
//     A caller needs a short-lived hold before payment capture completes.
//   Do not use when:
//     The caller wants a permanent balance decrement; capture the hold
//     instead once the amount is actually charged.
//   Semantics:
//     Reserves the requested amount atomically and returns a reservation
//     identity; an uncaptured reservation expires after its configured
//     lifetime.
//   Inputs:
//     - Amount is the requested hold amount in the account's minor unit; it
//       must be a positive integer.
//   Outputs:
//     - The reservation identity, required by the matching capture or
//       release call.
//   Preconditions:
//     - The account must exist and hold sufficient available balance.
//   Postconditions:
//     - On success, the reserved amount is subtracted from the available
//       balance until released, captured, or expired.
//   Effects:
//     - None: pure atom for this fixture's purposes.
//   Failure semantics:
//     - Insufficient balance is a safe, retryable outcome.
//   Determinism:
//     Deterministic given the same account state; the generated
//     reservation identity is not deterministic across retries.
//   Idempotency and retry:
//     Logical identity is the account and amount pair; a retry with the
//     same pair returns the existing hold for its configured key lifetime.
//   Reconciliation and compensation:
//     An expired, uncaptured hold is released automatically; no manual
//     compensation step exists for this atom.
//   Security and privacy:
//     The account identity is a capability-scoped reference and is never
//     logged alongside balance details.
//   Dependencies and bindings:
//     None: this fixture depends on nothing external.
//   Complexity and limits:
//     Bounded to one hold per call; no loop, no recursion.
//   Examples:
//     - Reserving 500 minor units against an account with sufficient
//       balance is the representative use.
//     - Reserving zero is a non-example; the caller must request at least
//       one minor unit.
//   Verification:
//     Covered by a real-storage integration test asserting exactly one
//     hold row per account under concurrent reservation attempts.
//   Retrieval concepts:
//     Funds hold, balance reservation, checkout lock.
func ReserveFundsForCheckout(amount int) (string, error) {
	return "reservation", nil
}
`

// TestPIPE048_RegisterVerifiedAtomsWritesARealRegistryRow covers PIPE-048.
//
// It drives recallKnownAtoms end to end over a produced atom carrying a
// complete schema-v1 //codeflux:atom comment, and proves the registry row
// this stage is supposed to write actually lands in storage: documentation
// (readable back through GetAtomDocumentationRevision), the exact contract
// hash recallkey.ComputeContractHash computes for this contract, and the
// exact repository revision (scope.revision) this atom was verified at.
//
// Proven to discriminate: before this change, nothing in recallKnownAtoms
// ever called CreateAtomDocumentationRevision, so
// ListAtomDocumentationRevisionsByProject after a call to recallKnownAtoms
// always returned zero rows for any project -- which is exactly the defect
// TODOS.md's PIPE-048 entry names: "No run writes the registry today, so
// recall has nothing to search."
func TestPIPE048_RegisterVerifiedAtomsWritesARealRegistryRow(t *testing.T) {
	execution, scope := recallFixture(t)
	execution.redactor = registrationTestRedactionPipeline(t)
	scope.revision = "abc123def456"

	worktree := recallWorktree(t, atomRegistrationFixtureSource)

	outcome, _ := execution.recallKnownAtoms(context.Background(), scope, worktree)
	if !outcome.Held {
		t.Fatalf("recall did not hold: %s", outcome.Detail)
	}

	registered, ok := outcome.Evidence["atoms_registered"].(int)
	if !ok || registered != 1 {
		t.Fatalf("expected exactly one atom registered, got %v (evidence: %v)",
			outcome.Evidence["atoms_registered"], outcome.Evidence)
	}

	revisions, err := execution.repositories.ListAtomDocumentationRevisionsByProject(
		context.Background(), scope.projectID, 10)
	if err != nil {
		t.Fatalf("list atom documentation revisions: %v", err)
	}
	if len(revisions) != 1 {
		t.Fatalf("expected exactly one registered revision, got %d", len(revisions))
	}
	revision := revisions[0]

	wantContract := normalizeWantedContract(producedFunction{
		Name: "ReserveFundsForCheckout", Parameters: []string{"int"},
		Results: []string{"string", "error"}, ReturnsError: true,
	})
	wantHash := recallkey.ComputeContractHash(wantContract)
	if revision.ContractHash != wantHash {
		t.Fatalf("registered contract hash = %s, want %s",
			revision.ContractHash.String(), wantHash.String())
	}
	if revision.SourceRepositoryRevision != scope.revision {
		t.Fatalf("registered revision carries repository revision %q, want %q",
			revision.SourceRepositoryRevision, scope.revision)
	}
	if revision.ValidationStatus != storage.AtomDocumentationValidationStatusAdmitted {
		t.Fatalf("registered revision status = %q, want admitted",
			revision.ValidationStatus)
	}

	full, err := execution.repositories.GetAtomDocumentationRevision(
		context.Background(), revision.RevisionID)
	if err != nil {
		t.Fatalf("read back full revision: %v", err)
	}
	if full.Document.Purpose.Text == "" {
		t.Fatal("registered revision carries no Purpose text")
	}
}

// TestPIPE048_UndocumentedProducedAtomIsNotRegistered proves registration
// never invents documentation for a function that carries none: a produced
// atom with an ordinary (non-schema-v1) comment is reported not registered,
// with a reason, rather than silently admitted.
func TestPIPE048_UndocumentedProducedAtomIsNotRegistered(t *testing.T) {
	execution, scope := recallFixture(t)
	execution.redactor = registrationTestRedactionPipeline(t)

	worktree := recallWorktree(t, `package reserve

// ReserveFundsForCheckout has an ordinary comment, not a schema-v1 one.
func ReserveFundsForCheckout(amount int) (string, error) {
	return "reservation", nil
}
`)

	outcome, _ := execution.recallKnownAtoms(context.Background(), scope, worktree)
	if !outcome.Held {
		t.Fatalf("recall did not hold: %s", outcome.Detail)
	}
	registered, ok := outcome.Evidence["atoms_registered"].(int)
	if !ok || registered != 0 {
		t.Fatalf("expected zero atoms registered, got %v", outcome.Evidence["atoms_registered"])
	}

	revisions, err := execution.repositories.ListAtomDocumentationRevisionsByProject(
		context.Background(), scope.projectID, 10)
	if err != nil {
		t.Fatalf("list atom documentation revisions: %v", err)
	}
	if len(revisions) != 0 {
		t.Fatalf("expected no registered revisions, got %d", len(revisions))
	}
}

// moleculeRegistrationFixtureSource declares two produced functions: addOne,
// an atom with no comment at all, and ComposeAddOneTwice, a molecule (it
// calls addOne) carrying a complete schema-v1 comment.
const moleculeRegistrationFixtureSource = `package compose

func addOne(x int) int {
	return x + 1
}

// ComposeAddOneTwice applies addOne twice to its input.
//
//codeflux:atom
// Codeflux atom documentation (schema v1):
//   Purpose:
//     Advance a counter by exactly two by composing the single-step
//     increment twice.
//   Use when:
//     A caller needs a two-step advance built from the project's own
//     single-step primitive rather than a hand-written literal +2.
//   Do not use when:
//     A caller needs a configurable step count; write a loop instead.
//   Semantics:
//     Applies addOne to x, then applies addOne to that result, and returns
//     the final value.
//   Inputs:
//     - X is the starting integer value.
//   Outputs:
//     - The input advanced by two.
//   Preconditions:
//     - None: any integer is accepted.
//   Postconditions:
//     - The result equals x plus two.
//   Effects:
//     - None: pure composition of a pure part.
//   Failure semantics:
//     - None: this composition cannot fail.
//   Determinism:
//     Fully deterministic: identical input always produces identical output.
//   Idempotency and retry:
//     Pure and side-effect free, so retrying is always safe.
//   Reconciliation and compensation:
//     None: nothing to reconcile for a pure function.
//   Security and privacy:
//     None: no sensitive data is involved.
//   Dependencies and bindings:
//     Depends on this project's own addOne.
//   Complexity and limits:
//     Constant time and space.
//   Examples:
//     - Calling this with three as the input returns five.
//     - Calling this with negative one returns one, a boundary case worth naming.
//   Verification:
//     Covered by a table test over representative and boundary inputs.
//   Retrieval concepts:
//     Two-step increment, double addOne, counter advance.
func ComposeAddOneTwice(x int) int {
	return addOne(addOne(x))
}
`

// TestPIPE049_RegisterVerifiedMoleculesNamesItsComposedAtoms covers
// PIPE-049: a produced function that calls another produced function is
// registered "on the same terms" as an atom, additionally naming what it
// composes as its parts through DependencyBindings.
//
// addOne itself carries no schema-v1 comment, so it is not registered
// (proving registration does not register a molecule's parts on its
// behalf); only ComposeAddOneTwice is, and its registered dependency
// bindings must name addOne.
func TestPIPE049_RegisterVerifiedMoleculesNamesItsComposedAtoms(t *testing.T) {
	execution, scope := recallFixture(t)
	execution.redactor = registrationTestRedactionPipeline(t)
	scope.revision = "molecule-fixture-revision"

	worktree := recallWorktree(t, moleculeRegistrationFixtureSource)

	outcome, _ := execution.recallKnownAtoms(context.Background(), scope, worktree)
	if !outcome.Held {
		t.Fatalf("recall did not hold: %s", outcome.Detail)
	}

	atomsRegistered, _ := outcome.Evidence["atoms_registered"].(int)
	moleculesRegistered, _ := outcome.Evidence["molecules_registered"].(int)
	if atomsRegistered != 0 {
		t.Fatalf("expected addOne (undocumented) not to register as an atom, got %d", atomsRegistered)
	}
	if moleculesRegistered != 1 {
		t.Fatalf("expected exactly one molecule registered, got %d", moleculesRegistered)
	}

	revisions, err := execution.repositories.ListAtomDocumentationRevisionsByProject(
		context.Background(), scope.projectID, 10)
	if err != nil {
		t.Fatalf("list atom documentation revisions: %v", err)
	}
	if len(revisions) != 1 {
		t.Fatalf("expected exactly one registered revision, got %d", len(revisions))
	}
	if len(revisions[0].DependencyBindings) != 1 ||
		revisions[0].DependencyBindings[0].Name != "addOne" {
		t.Fatalf("expected a dependency binding naming addOne, got %+v",
			revisions[0].DependencyBindings)
	}
	if revisions[0].DependencyBindings[0].Version != scope.revision {
		t.Fatalf("dependency binding version = %q, want %q",
			revisions[0].DependencyBindings[0].Version, scope.revision)
	}
}

// TestPIPE048_RegistrationReusesTheSameAtomIdentityAcrossRuns proves a
// second run's registration of an unchanged contract extends the SAME atom
// identity rather than minting a new one -- resolveRegistrationIdentity's
// whole reason to read the registry before minting.
func TestPIPE048_RegistrationReusesTheSameAtomIdentityAcrossRuns(t *testing.T) {
	execution, scope := recallFixture(t)
	execution.redactor = registrationTestRedactionPipeline(t)
	scope.revision = "identity-fixture-revision"

	worktree := recallWorktree(t, atomRegistrationFixtureSource)
	first, _ := execution.recallKnownAtoms(context.Background(), scope, worktree)
	if !first.Held {
		t.Fatalf("first recall did not hold: %s", first.Detail)
	}

	firstRevisions, err := execution.repositories.ListAtomDocumentationRevisionsByProject(
		context.Background(), scope.projectID, 10)
	if err != nil || len(firstRevisions) != 1 {
		t.Fatalf("expected exactly one revision after the first run, got %d (err %v)",
			len(firstRevisions), err)
	}
	firstAtomID := firstRevisions[0].AtomID

	// A second call, registering the identical produced contract again --
	// atom_documentation_revisions carries no task scoping at all, so this
	// only needs to prove that registering the same contract twice extends
	// the same atom identity rather than forking a new one.
	second, _ := execution.recallKnownAtoms(context.Background(), scope, worktree)
	if !second.Held {
		t.Fatalf("second recall did not hold: %s", second.Detail)
	}

	secondRevisions, err := execution.repositories.ListAtomDocumentationRevisionsByProject(
		context.Background(), scope.projectID, 10)
	if err != nil {
		t.Fatalf("list after second run: %v", err)
	}
	foundSameAtom := false
	for _, revision := range secondRevisions {
		if revision.AtomID == firstAtomID {
			foundSameAtom = true
		}
	}
	if !foundSameAtom {
		t.Fatalf("second registration did not extend the first run's atom identity %s: %+v",
			firstAtomID, secondRevisions)
	}
}

// TestPIPE048_ChangedContractMintsADifferentAtomIdentity is the other half
// of the identity claim TestPIPE048_RegistrationReusesTheSameAtomIdentity
// AcrossRuns only proves one direction of: an unchanged contract must reuse
// an identity, and a genuinely changed one must NOT collide with it.
//
// Proven to discriminate: a defective resolveRegistrationIdentity that
// always returned the single most-recently-registered atom (ignoring the
// contract hash key entirely) would pass the first test above -- there is
// only ever one prior revision there -- and fail this one, because the
// second worktree's function shares nothing with the first's contract.
func TestPIPE048_ChangedContractMintsADifferentAtomIdentity(t *testing.T) {
	execution, scope := recallFixture(t)
	execution.redactor = registrationTestRedactionPipeline(t)
	scope.revision = "identity-fixture-revision"

	first, _ := execution.recallKnownAtoms(context.Background(),
		scope, recallWorktree(t, atomRegistrationFixtureSource))
	if !first.Held {
		t.Fatalf("first recall did not hold: %s", first.Detail)
	}
	firstRevisions, err := execution.repositories.ListAtomDocumentationRevisionsByProject(
		context.Background(), scope.projectID, 10)
	if err != nil || len(firstRevisions) != 1 {
		t.Fatalf("expected exactly one revision after the first run, got %d (err %v)",
			len(firstRevisions), err)
	}
	firstAtomID := firstRevisions[0].AtomID
	firstContractHash := firstRevisions[0].ContractHash

	// A second, unrelated task in the same project, registering a function
	// with a genuinely different contract (a different parameter type and no
	// declared return-error), through the same execution and project scope
	// so it lands in the same registry.
	secondTaskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	secondScope := scope
	secondScope.taskID = secondTaskID

	secondWorktree := recallWorktree(t, `package reserve

// ReleaseFundsHoldByIdentity releases a previously created hold by its
// reservation identity, an entirely different contract from
// ReserveFundsForCheckout.
//
//codeflux:atom
// Codeflux atom documentation (schema v1):
//   Purpose:
//     Release a previously created funds hold so its amount becomes
//     available again.
//   Use when:
//     A caller holds a reservation identity it no longer intends to
//     capture.
//   Do not use when:
//     The caller wants to capture the hold rather than release it.
//   Semantics:
//     Marks the named hold released, freeing its amount immediately.
//   Inputs:
//     - HoldID is the reservation identity returned by the original hold.
//   Outputs:
//     - None: this fixture reports success by returning without an error.
//   Preconditions:
//     - The hold must currently exist and not already be released.
//   Postconditions:
//     - The named hold no longer reserves any amount.
//   Effects:
//     - None: pure atom for this fixture's purposes.
//   Failure semantics:
//     - None: this fixture cannot fail.
//   Determinism:
//     Fully deterministic: identical input always produces identical output.
//   Idempotency and retry:
//     Releasing an already-released hold is a safe no-op retry.
//   Reconciliation and compensation:
//     None: nothing to reconcile for this fixture.
//   Security and privacy:
//     None: no sensitive data is involved.
//   Dependencies and bindings:
//     None: this fixture depends on nothing external.
//   Complexity and limits:
//     Constant time and space.
//   Examples:
//     - Releasing a hold that exists succeeds silently.
//     - Releasing an unknown hold identity is a non-example this fixture
//       does not model.
//   Verification:
//     Covered by this test alone.
//   Retrieval concepts:
//     Hold release, reservation cancellation.
func ReleaseFundsHoldByIdentity(holdID string) {
}
`)
	second, _ := execution.recallKnownAtoms(context.Background(), secondScope, secondWorktree)
	if !second.Held {
		t.Fatalf("second recall did not hold: %s", second.Detail)
	}

	secondRevisions, err := execution.repositories.ListAtomDocumentationRevisionsByProject(
		context.Background(), scope.projectID, 10)
	if err != nil {
		t.Fatalf("list after second run: %v", err)
	}
	if len(secondRevisions) != 2 {
		t.Fatalf("expected two distinct revisions after registering two different "+
			"contracts, got %d: %+v", len(secondRevisions), secondRevisions)
	}
	for _, revision := range secondRevisions {
		if revision.ContractHash == firstContractHash && revision.AtomID != firstAtomID {
			t.Fatalf("a revision sharing the first contract hash carries a "+
				"different atom identity: %+v", revision)
		}
		if revision.ContractHash != firstContractHash && revision.AtomID == firstAtomID {
			t.Fatalf("a revision with a genuinely different contract hash "+
				"was registered under the first run's atom identity %s: %+v",
				firstAtomID, revision)
		}
	}
}

// TestPIPE052_ReuseIsNotAdmittedWhenThisRunsVerificationFailed covers
// PIPE-052: a contract-hash match must not be admitted as reuse when this
// run's own StageAtomVerification is on record, for the same attempt, as
// having failed.
//
// Proven to discriminate: without admitReuseDecisions' downgrade,
// AuthorizeAndHoldFunds -- whose contract hash matches the stored
// ReserveFunds exactly, the same fixture PIPE-051's own recall test uses --
// is reported reused regardless of whether this run's own tests passed.
// This test records a failing StageAtomVerification row for the same task
// and attempt recall reads and proves the decision is downgraded to write,
// carrying a justification that names PIPE-052, and that the function no
// longer appears in already_in_project.
func TestPIPE052_ReuseIsNotAdmittedWhenThisRunsVerificationFailed(t *testing.T) {
	// recallFixture creates a project but no task row, which
	// RecordPipelineStageResult's foreign key requires; this test needs a
	// real task to seed a pipeline-stage row against, so it uses
	// mustOpenRunEpisodeFixture (episode_lifecycle_test.go) instead, which
	// already builds that full chain for the same reason.
	execution, scope, _, _ := mustOpenRunEpisodeFixture(t)

	storeKnownArtifact(t, execution, scope.projectID, `package reserve

func ReserveFunds(amount int) (string, error) {
	return "reservation", nil
}
`)

	worktree := recallWorktree(t, `package reserve

// AuthorizeAndHoldFunds is the same contract as ReserveFunds, renamed.
func AuthorizeAndHoldFunds(amount int) (string, error) {
	return "reservation", nil
}
`)

	// Attempt 1's own atom-verification is recorded failed, before recall
	// runs -- exactly the order Run() (agent_execution.go) uses: examineStructure
	// (which performs StageAtomVerification) runs before recallKnownAtoms.
	if _, err := execution.repositories.RecordPipelineStageResult(context.Background(),
		storage.RecordPipelineStage{
			TaskID: scope.taskID, Attempt: 1, Stage: pipeline.StageAtomVerification,
			State: pipeline.StateFailed, DetailRedacted: "a test failed this attempt",
		}); err != nil {
		t.Fatalf("seed a failing atom-verification row: %v", err)
	}

	outcome, _ := execution.recallKnownAtoms(context.Background(), scope, worktree)
	if !outcome.Held {
		t.Fatalf("recall did not hold: %s", outcome.Detail)
	}

	reused, _ := outcome.Evidence["already_in_project"].([]string)
	for _, name := range reused {
		if name == "AuthorizeAndHoldFunds" {
			t.Fatalf("a contract-hash match was admitted as reuse despite this "+
				"run's own verification failing: %v", outcome.Evidence)
		}
	}
	decisions, ok := outcome.Evidence["decisions"].(map[string]recallDecision)
	if !ok {
		t.Fatalf("evidence carries no decision map: %v", outcome.Evidence)
	}
	decision := decisions["AuthorizeAndHoldFunds"]
	if decision.Decision != "write" {
		t.Fatalf("expected the unadmitted match to read as write, got %+v", decision)
	}
	if !containsSubstring(decision.Justification, "PIPE-052") {
		t.Fatalf("write justification does not explain the PIPE-052 downgrade: %q",
			decision.Justification)
	}

	if checked, _ := outcome.Evidence["reverification_checked"].(bool); !checked {
		t.Fatal("expected reverification_checked to be true once a ledger row was seeded")
	}
	if held, _ := outcome.Evidence["reverification_held"].(bool); held {
		t.Fatal("expected reverification_held to be false for a seeded failing row")
	}
}

// TestPIPE052_ReuseStillAdmittedWhenNoVerificationEvidenceExists proves the
// fail-open half of PIPE-052's own boundary: recall's existing, already
// relied-on binding behaviour (PIPE-050/PIPE-051) must not regress for a
// caller -- like every fixture in agent_stage_recall_test.go -- that never
// wrote a StageAtomVerification row at all.
func TestPIPE052_ReuseStillAdmittedWhenNoVerificationEvidenceExists(t *testing.T) {
	execution, scope := recallFixture(t)

	storeKnownArtifact(t, execution, scope.projectID, `package reserve

func ReserveFunds(amount int) (string, error) {
	return "reservation", nil
}
`)
	worktree := recallWorktree(t, `package reserve

// AuthorizeAndHoldFunds is the same contract as ReserveFunds, renamed.
func AuthorizeAndHoldFunds(amount int) (string, error) {
	return "reservation", nil
}
`)

	outcome, _ := execution.recallKnownAtoms(context.Background(), scope, worktree)
	if !outcome.Held {
		t.Fatalf("recall did not hold: %s", outcome.Detail)
	}
	reused, ok := outcome.Evidence["already_in_project"].([]string)
	if !ok || len(reused) != 1 || reused[0] != "AuthorizeAndHoldFunds" {
		t.Fatalf("expected the renamed function to still be recalled as reused "+
			"when no verification evidence exists, got %v", reused)
	}
	if checked, _ := outcome.Evidence["reverification_checked"].(bool); checked {
		t.Fatal("expected reverification_checked to be false when nothing was ever recorded")
	}
}

// TestPIPE052_ReuseIsAdmittedWhenThisRunsVerificationExplicitlyHeld is the
// positive counterpart to TestPIPE052_ReuseIsNotAdmittedWhenThisRuns
// VerificationFailed: it is not enough that reuse survives when no evidence
// exists (the fail-open default); it must also be positively admitted when
// this run's own StageAtomVerification is on record, for the same attempt,
// as pipeline.StateSatisfied -- the actual affirmative case PIPE-052 exists
// to let through, not merely fail to break.
//
// Proven to discriminate against a defective admitReuseDecisions that
// downgraded every reuse decision whenever verificationChecked is true
// (rather than only when verificationHeld is false): that defect would
// still pass TestPIPE052_ReuseStillAdmittedWhenNoVerificationEvidenceExists
// (which never sets verificationChecked at all) but would fail this test,
// because here verificationChecked is true and verificationHeld is also
// true, and the reuse decision must survive both.
func TestPIPE052_ReuseIsAdmittedWhenThisRunsVerificationExplicitlyHeld(t *testing.T) {
	execution, scope, _, _ := mustOpenRunEpisodeFixture(t)

	storeKnownArtifact(t, execution, scope.projectID, `package reserve

func ReserveFunds(amount int) (string, error) {
	return "reservation", nil
}
`)
	worktree := recallWorktree(t, `package reserve

// AuthorizeAndHoldFunds is the same contract as ReserveFunds, renamed.
func AuthorizeAndHoldFunds(amount int) (string, error) {
	return "reservation", nil
}
`)

	if _, err := execution.repositories.RecordPipelineStageResult(context.Background(),
		storage.RecordPipelineStage{
			TaskID: scope.taskID, Attempt: 1, Stage: pipeline.StageAtomVerification,
			State: pipeline.StateSatisfied, DetailRedacted: "every atom test passed this attempt",
		}); err != nil {
		t.Fatalf("seed a satisfied atom-verification row: %v", err)
	}

	outcome, _ := execution.recallKnownAtoms(context.Background(), scope, worktree)
	if !outcome.Held {
		t.Fatalf("recall did not hold: %s", outcome.Detail)
	}

	reused, ok := outcome.Evidence["already_in_project"].([]string)
	if !ok || len(reused) != 1 || reused[0] != "AuthorizeAndHoldFunds" {
		t.Fatalf("expected the renamed function to be recalled as reused when "+
			"this run's own verification explicitly held, got %v (evidence: %v)",
			reused, outcome.Evidence)
	}
	decisions, ok := outcome.Evidence["decisions"].(map[string]recallDecision)
	if !ok || decisions["AuthorizeAndHoldFunds"].Decision != "reuse" {
		t.Fatalf("expected a reuse decision for AuthorizeAndHoldFunds, got %+v",
			decisions["AuthorizeAndHoldFunds"])
	}
	if checked, _ := outcome.Evidence["reverification_checked"].(bool); !checked {
		t.Fatal("expected reverification_checked to be true once a satisfied row was seeded")
	}
	if held, _ := outcome.Evidence["reverification_held"].(bool); !held {
		t.Fatal("expected reverification_held to be true for a seeded satisfied row")
	}
}

// TestPIPE054_055_ClassifiesRegistryGapVersusRecallMiss covers PIPE-054's
// reuse-regret comparison and PIPE-055's classification.
//
// A function whose contract exists nowhere (neither the artifact-based
// search nor the structured registry) is a registry gap. A function whose
// contract exists in the structured registry but is missed by the
// artifact-based search (because it was registered, not merely stored as an
// artifact) is a recall miss -- two different defects, proven distinct here
// by checking both are reported, are non-overlapping, and name the right
// function.
func TestPIPE054_055_ClassifiesRegistryGapVersusRecallMiss(t *testing.T) {
	execution, scope := recallFixture(t)
	execution.redactor = registrationTestRedactionPipeline(t)

	// First: register NeverBuiltAgain's exact contract into the structured
	// registry via one run, under a different, unrelated task so recall's
	// own task-exclusion logic does not filter it out.
	seedScope := scope
	seedScope.revision = "seed-fixture-revision"
	seedWorktree := recallWorktree(t, `package reserve

// NeverBuiltAgain is documented, registered once, and then never produced
// again by any later run in this test.
//
//codeflux:atom
// Codeflux atom documentation (schema v1):
//   Purpose:
//     Exist once, get registered, and never be produced again -- the
//     fixture this test needs to prove a recall-miss.
//   Use when:
//     Never; this is a synthetic fixture.
//   Do not use when:
//     Always not-use; this is a synthetic fixture.
//   Semantics:
//     Returns a fixed reservation identity.
//   Inputs:
//     - Amount is an integer, unused beyond its type.
//   Outputs:
//     - A fixed reservation identity string.
//   Preconditions:
//     - None: any integer is accepted.
//   Postconditions:
//     - None beyond returning the fixed value.
//   Effects:
//     - None: pure atom for this fixture.
//   Failure semantics:
//     - None: this fixture cannot fail.
//   Determinism:
//     Fully deterministic: identical input always produces identical output.
//   Idempotency and retry:
//     Pure, so retrying is always safe.
//   Reconciliation and compensation:
//     None: nothing to reconcile for this fixture.
//   Security and privacy:
//     None: no sensitive data is involved.
//   Dependencies and bindings:
//     None: this fixture depends on nothing external.
//   Complexity and limits:
//     Constant time and space.
//   Examples:
//     - NeverBuiltAgain(1) returns "reservation".
//     - NeverBuiltAgain(0) is a non-example only in spirit; no input is
//       actually rejected.
//   Verification:
//     Covered by this test alone.
//   Retrieval concepts:
//     Synthetic fixture, recall-miss probe.
func NeverBuiltAgain(amount int) (string, error) {
	return "reservation", nil
}
`)
	seeded, _ := execution.recallKnownAtoms(context.Background(), seedScope, seedWorktree)
	if !seeded.Held {
		t.Fatalf("seeding recall did not hold: %s", seeded.Detail)
	}
	seededRegistered, _ := seeded.Evidence["atoms_registered"].(int)
	if seededRegistered != 1 {
		t.Fatalf("expected the seeding run to register exactly one atom, got %d", seededRegistered)
	}

	// Second: a fresh task in the same project, needing two functions
	// neither the artifact search nor (for one of them) the registry has
	// ever seen, plus a rebuild of NeverBuiltAgain's exact contract under a
	// brand-new name -- which the registry (not the artifact search) does
	// recognise.
	secondScope := scope
	newTaskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	secondScope.taskID = newTaskID

	worktree := recallWorktree(t, `package reserve

// GenuinelyNovelWork shares no contract with anything the registry or the
// project's stored artifacts have ever seen.
func GenuinelyNovelWork(amount int, currency string) (bool, error) {
	return true, nil
}

// RebuildsTheRegisteredContract shares NeverBuiltAgain's exact contract but
// was never itself registered -- only found by matching the registry.
func RebuildsTheRegisteredContract(amount int) (string, error) {
	return "reservation", nil
}
`)

	outcome, _ := execution.recallKnownAtoms(context.Background(), secondScope, worktree)
	if !outcome.Held {
		t.Fatalf("recall did not hold: %s", outcome.Detail)
	}
	regrets, ok := outcome.Evidence["reuse_regrets"].(map[string]string)
	if !ok {
		t.Fatalf("evidence carries no reuse_regrets map: %v", outcome.Evidence)
	}
	if regrets["GenuinelyNovelWork"] != "registry-gap" {
		t.Fatalf("GenuinelyNovelWork classified %q, want registry-gap",
			regrets["GenuinelyNovelWork"])
	}
	if regrets["RebuildsTheRegisteredContract"] != "recall-miss" {
		t.Fatalf("RebuildsTheRegisteredContract classified %q, want recall-miss",
			regrets["RebuildsTheRegisteredContract"])
	}
	if count, _ := outcome.Evidence["reuse_regret_count"].(int); count != len(regrets) {
		t.Fatalf("reuse_regret_count = %v, want %d", outcome.Evidence["reuse_regret_count"], len(regrets))
	}
}

// containsSubstring is declared in pipe042_skip_audit_test.go and reused
// here rather than redeclared.
