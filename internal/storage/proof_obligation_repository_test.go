package storage

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
)

// TestPIPE016_AnObligationIsRaisedOnceAndDischargedByName covers the durable
// half of PIPE-016.
//
// docs/plan.md §9 makes the proof obligation the unit of assurance. What
// existed was two stages that stated obligations in prose and reported
// satisfied for having stated them, and two later stages that reported
// satisfied without reference to anything the first two said.
func TestPIPE016_AnObligationIsRaisedOnceAndDischargedByName(t *testing.T) {
	repositories := openTestRepositories(t)
	taskID := seedObligationTask(t, repositories, 8100)

	raised, err := repositories.RaiseProofObligation(t.Context(),
		RaiseProofObligation{
			TaskID: taskID, Attempt: 1, Kind: ObligationComposition,
			Subject: "Compose", StatementRedacted: "joining the parts answers the whole",
		})
	if err != nil {
		t.Fatalf("raising: %v", err)
	}
	if raised.State != ObligationOpen {
		t.Errorf("a fresh obligation is %q, want open", raised.State)
	}

	// Raising the same claim again is the same claim, not a second one.
	if _, err := repositories.RaiseProofObligation(t.Context(),
		RaiseProofObligation{
			TaskID: taskID, Attempt: 1, Kind: ObligationComposition,
			Subject: "Compose", StatementRedacted: "restated",
		}); err != nil {
		t.Fatalf("re-raising: %v", err)
	}
	listed, err := repositories.ListProofObligations(t.Context(), taskID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("re-raising produced %d obligations, want 1", len(listed))
	}

	if err := repositories.DischargeProofObligation(t.Context(), taskID, 1,
		ObligationComposition, "Compose", pipeline.StageMoleculeVerification,
		"exercised as a composition"); err != nil {
		t.Fatalf("discharging: %v", err)
	}
	listed, err = repositories.ListProofObligations(t.Context(), taskID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if listed[0].State != ObligationDischarged {
		t.Fatalf("state after discharge = %q", listed[0].State)
	}
	if listed[0].DischargedByStage == nil ||
		*listed[0].DischargedByStage != pipeline.StageMoleculeVerification {
		t.Error("the discharge does not name the stage that performed it")
	}
}

// TestPIPE016_DischargingSomethingNeverRaisedIsRefused keeps a discharge tied
// to a claim.
func TestPIPE016_DischargingSomethingNeverRaisedIsRefused(t *testing.T) {
	repositories := openTestRepositories(t)
	taskID := seedObligationTask(t, repositories, 8110)

	if err := repositories.DischargeProofObligation(t.Context(), taskID, 1,
		ObligationControlPath, "NeverRaised", pipeline.StageControlFlow, "",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("discharging an unraised obligation = %v, want ErrNotFound", err)
	}
}

// TestPIPE016_AnObligationIsScopedToItsAttempt proves a later attempt does not
// inherit an earlier one's discharge.
func TestPIPE016_AnObligationIsScopedToItsAttempt(t *testing.T) {
	repositories := openTestRepositories(t)
	taskID := seedObligationTask(t, repositories, 8120)

	for _, attempt := range []uint64{1, 2} {
		if _, err := repositories.RaiseProofObligation(t.Context(),
			RaiseProofObligation{
				TaskID: taskID, Attempt: attempt, Kind: ObligationControlPath,
				Subject: "Branchy", StatementRedacted: "every path terminates",
			}); err != nil {
			t.Fatalf("raising for attempt %d: %v", attempt, err)
		}
	}
	if err := repositories.DischargeProofObligation(t.Context(), taskID, 1,
		ObligationControlPath, "Branchy", pipeline.StageControlFlow, "settled",
	); err != nil {
		t.Fatal(err)
	}

	second, err := repositories.ListProofObligations(t.Context(), taskID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].State != ObligationOpen {
		t.Fatalf("the second attempt inherited a discharge: %+v", second)
	}
}

// TestPIPE016_AnUninterpretableObligationIsRefused keeps the list readable.
func TestPIPE016_AnUninterpretableObligationIsRefused(t *testing.T) {
	repositories := openTestRepositories(t)
	taskID := seedObligationTask(t, repositories, 8130)

	base := RaiseProofObligation{
		TaskID: taskID, Attempt: 1, Kind: ObligationComposition,
		Subject: "Thing", StatementRedacted: "something holds",
	}
	for name, mutate := range map[string]func(*RaiseProofObligation){
		"no subject":   func(r *RaiseProofObligation) { r.Subject = "" },
		"no statement": func(r *RaiseProofObligation) { r.StatementRedacted = "" },
		"no attempt":   func(r *RaiseProofObligation) { r.Attempt = 0 },
		"unknown kind": func(r *RaiseProofObligation) { r.Kind = "invented" },
	} {
		t.Run(name+" is refused", func(t *testing.T) {
			input := base
			mutate(&input)
			if _, err := repositories.RaiseProofObligation(t.Context(), input); err == nil {
				t.Fatal("an uninterpretable obligation was stored")
			}
		})
	}
}

// seedObligationTask creates the parent rows an obligation needs.
func seedObligationTask(
	t *testing.T,
	repositories *Repositories,
	seed int,
) domain.TaskID {
	t.Helper()
	projectID := testProjectID(t, seed)
	repositoryID := testRepositoryID(t, seed+1)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	threadID, _ := domain.NewThreadID()
	taskID, _ := domain.NewTaskID()
	if _, err := repositories.CreateThread(t.Context(), CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID,
		Title: "Obligations",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateTask(t.Context(), CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset:      domain.PolicyPresetBalanced,
		ReasoningEffort:   domain.ReasoningEffortStandard,
		RiskLevel:         domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		SettingsRevision:  0,
		IdempotencyKey:    "obligation-task",
	}); err != nil {
		t.Fatal(err)
	}
	return taskID
}
