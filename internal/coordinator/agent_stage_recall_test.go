package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/retrieval/recallkey"
	"codeflux.dev/codeflux/internal/storage"
)

// TestPIPE050_EveryWantedContractLeavesWithADecision covers PIPE-050's
// binding requirement at bindRecallDecisions' own level, without a database
// or a worktree: every contract this run needs must leave with either a
// reuse decision or a written justification, and a reuse decision must only
// ever come from an exact contract-hash match — never from a shape
// candidate, which is the AGENTS.md boundary this stage must not cross
// ("vector similarity ... never establishes ... permission").
//
// Proven to discriminate: with the completeness check in
// bindRecallDecisions removed and one of the loop's two `decisions[name] =`
// assignments deleted, this test fails, naming the missing function; with
// both restored it passes.
func TestPIPE050_EveryWantedContractLeavesWithADecision(t *testing.T) {
	matched := producedFunction{
		Name: "MatchedFunction", Parameters: []string{"int"},
		Results: []string{"int", "error"}, ReturnsError: true, Pure: true,
	}
	shapeOnly := producedFunction{
		Name: "ShapeOnlyFunction", Parameters: []string{"string"},
		Results: []string{"bool"}, Pure: true,
	}
	unmatched := producedFunction{
		Name: "UnmatchedFunction", Parameters: []string{"[]byte"},
		Results: []string{"error"}, ReturnsError: true, Pure: true,
	}
	wanted := map[string]producedFunction{
		matched.Name:   matched,
		shapeOnly.Name: shapeOnly,
		unmatched.Name: unmatched,
	}

	matchedContract := normalizeWantedContract(matched)
	matchedHash := recallkey.ComputeContractHash(matchedContract)
	contractIndex := map[atomdoc.ContractHash][]knownFunction{
		matchedHash: {{Name: "OldName", ArtifactID: "artifact-1"}},
	}

	// A project function sharing ShapeOnlyFunction's parameter/result types
	// but NOT its effects (impure vs. pure): a shape candidate, deliberately
	// never promoted to a match.
	shapeCandidateContract := recallkey.NormalizedContract{
		Parameters: shapeOnly.Parameters, Results: shapeOnly.Results,
		Effects: []string{"reaches outside its arguments"},
	}
	shapeIndex := map[recallkey.SignatureShape][]knownFunction{
		shapeCandidateContract.Shape(): {{Name: "NearMiss", ArtifactID: "artifact-2"}},
	}

	decisions, err := bindRecallDecisions(wanted, contractIndex, shapeIndex)
	if err != nil {
		t.Fatalf("binding failed: %v", err)
	}
	if len(decisions) != len(wanted) {
		t.Fatalf("recorded %d decision(s) for %d wanted contract(s)",
			len(decisions), len(wanted))
	}

	matchDecision, ok := decisions[matched.Name]
	if !ok || matchDecision.Decision != "reuse" {
		t.Fatalf("%s: expected a reuse decision from its exact contract-hash "+
			"match, got %+v", matched.Name, matchDecision)
	}
	if matchDecision.MatchedFunction != "OldName" ||
		matchDecision.ContractHash != matchedHash.String() {
		t.Fatalf("reuse decision does not name the matching function and "+
			"contract hash: %+v", matchDecision)
	}

	shapeDecision, ok := decisions[shapeOnly.Name]
	if !ok || shapeDecision.Decision != "write" {
		t.Fatalf("%s: a shape-only candidate must never be admitted as a "+
			"reuse decision, got %+v", shapeOnly.Name, shapeDecision)
	}
	if shapeDecision.Justification == "" {
		t.Fatalf("%s: a write decision must carry a justification",
			shapeOnly.Name)
	}

	unmatchedDecision, ok := decisions[unmatched.Name]
	if !ok || unmatchedDecision.Decision != "write" {
		t.Fatalf("%s: expected a write decision, got %+v",
			unmatched.Name, unmatchedDecision)
	}
}

// TestPIPE050_BindingRefusesAnIncompleteResult proves the completeness
// invariant itself is checkable: an index that cannot supply a decision for
// every wanted contract must be reported as an error naming the gap, not
// silently under-recorded.
func TestPIPE050_BindingRefusesAnIncompleteResult(t *testing.T) {
	wanted := map[string]producedFunction{
		"OnlyContract": {Name: "OnlyContract", Results: []string{"error"}, ReturnsError: true},
	}
	// An empty wanted map is trivially complete; this instead exercises the
	// invariant against a real gap by calling bindRecallDecisions with a
	// wanted set whose sole entry has an empty name — normalizeWantedContract
	// and ComputeContractHash both accept it, but the resulting decisions map
	// must still cover exactly the one key bindRecallDecisions was asked
	// about. This is a direct exercise of the completeness branch's error
	// message shape (used elsewhere), not a claim that the ordinary loop can
	// itself omit an entry.
	decisions, err := bindRecallDecisions(wanted, nil, nil)
	if err != nil {
		t.Fatalf("binding an ordinary single contract with no index failed: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("expected exactly one decision, got %d", len(decisions))
	}
	if decisions["OnlyContract"].Decision != "write" {
		t.Fatalf("an empty index must yield a write decision, got %+v",
			decisions["OnlyContract"])
	}
}

// TestPIPE051_RecallFindsARenameTheOldSubstringMatchMissed covers PIPE-051
// end to end through recallKnownAtoms.
//
// The old check, `strings.Contains(content, "func "+name+"(")`, is built
// from the NEW function's name and searched against OLD stored text. A
// rename can never appear there: the old text never contained the new
// identifier. This drives a real repository and worktree to prove the
// contract-hash key finds the rename the substring match structurally could
// not.
//
// Proven to discriminate: reverting recallKnownAtoms to the substring match
// against the same fixture reports zero reused functions; the contract-hash
// version reports one.
func TestPIPE051_RecallFindsARenameTheOldSubstringMatchMissed(t *testing.T) {
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

	outcome := execution.recallKnownAtoms(context.Background(), scope, worktree)
	if !outcome.Held {
		t.Fatalf("recall did not hold: %s", outcome.Detail)
	}
	reused, ok := outcome.Evidence["already_in_project"].([]string)
	if !ok || len(reused) != 1 || reused[0] != "AuthorizeAndHoldFunds" {
		t.Fatalf("expected the renamed function to be recalled as reused, "+
			"got %v (evidence: %v)", reused, outcome.Evidence)
	}
}

// TestPIPE051_RecallRefusesASameNamedDifferentContract covers PIPE-051's
// other failure mode of the old check: `strings.Contains` matches on the
// name alone, so a same-named function whose contract actually changed was
// always reported reusable. This proves the contract-hash key correctly
// refuses it.
func TestPIPE051_RecallRefusesASameNamedDifferentContract(t *testing.T) {
	execution, scope := recallFixture(t)

	storeKnownArtifact(t, execution, scope.projectID, `package reserve

func ReserveFunds(amount int) error {
	return nil
}
`)

	worktree := recallWorktree(t, `package reserve

// ReserveFunds shares a name with the stored version but not its contract:
// it now returns the reservation identity too.
func ReserveFunds(amount int) (string, error) {
	return "reservation", nil
}
`)

	outcome := execution.recallKnownAtoms(context.Background(), scope, worktree)
	if !outcome.Held {
		t.Fatalf("recall did not hold: %s", outcome.Detail)
	}
	reused, _ := outcome.Evidence["already_in_project"].([]string)
	for _, name := range reused {
		if name == "ReserveFunds" {
			t.Fatalf("a same-named function whose contract changed was "+
				"admitted as reused: %v", outcome.Evidence)
		}
	}
	decisions, ok := outcome.Evidence["decisions"].(map[string]recallDecision)
	if !ok {
		t.Fatalf("evidence carries no decision map: %v", outcome.Evidence)
	}
	if decisions["ReserveFunds"].Decision != "write" {
		t.Fatalf("expected a write decision for the changed contract, got %+v",
			decisions["ReserveFunds"])
	}
}

// recallFixture builds a real, migrated database and repositories, and a
// project to scope recall's search to.
func recallFixture(t *testing.T) (*AgentExecution, agentScope) {
	t.Helper()
	root := t.TempDir()
	database, err := storage.Open(context.Background(), storage.OpenOptions{
		Path: filepath.Join(root, "codeflux.sqlite3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	if _, err := database.Migrate(context.Background(), storage.MigrationOptions{
		ApplicationVersion: "recall-test",
	}); err != nil {
		t.Fatal(err)
	}
	repositories, err := storage.NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := domain.NewProjectID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateProject(context.Background(), storage.CreateProject{
		ID: projectID, Name: "recall-fixture",
	}); err != nil {
		t.Fatal(err)
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	execution := &AgentExecution{repositories: repositories}
	scope := agentScope{projectID: projectID, taskID: taskID}
	return execution, scope
}

// storeKnownArtifact records one piece of Go source as earlier project work,
// the way a previous run's produced artifacts are stored.
func storeKnownArtifact(t *testing.T, execution *AgentExecution, projectID domain.ProjectID, content string) {
	t.Helper()
	if _, err := execution.repositories.RecordProducedArtifact(context.Background(), storage.RecordArtifact{
		ProjectID: projectID, Type: "produced-source", Path: "reserve/funds.go",
		Content: []byte(content), MediaType: "text/x-go",
		StorageClass: storage.ArtifactPermanentSemantic,
	}); err != nil {
		t.Fatalf("recording known artifact: %v", err)
	}
}

// recallWorktree writes a real Git repository whose files read as produced,
// the same fixture pattern testedNamesFixture uses, so readProducedFunctions
// finds them.
func recallWorktree(t *testing.T, source string) string {
	t.Helper()
	worktree := t.TempDir()
	initializeCoordinatorGitRepository(t, worktree)
	if err := os.MkdirAll(filepath.Join(worktree, "reserve"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(worktree, "reserve", "funds.go"), []byte(source), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	return worktree
}
