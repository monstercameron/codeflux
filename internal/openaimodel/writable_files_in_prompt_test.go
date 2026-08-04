package openaimodel

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/executor"
)

// TestTheRoundSaysWhichFilesMayBeWritten is the constraint that was only ever
// stated after it had been broken.
//
// repository_context holds files a run may read and files it may write, and
// nothing in the document told them apart. On a generated workspace that
// difference bites immediately: the module's stub main.go sits in the list
// beside the files the plan owns, and it is a main package in a project being
// asked for a program, so it reads as the obvious place to write. Runs edited
// it, were refused, and spent the round learning what they could have been told
// at the start of it.
//
// Proven to discriminate: against the previous implementation what_to_do_now is
// the same sentence for every plan and names no file at all.
func TestTheRoundSaysWhichFilesMayBeWritten(t *testing.T) {
	rendered := observationJSON(agent.ModelInput{
		Round: 3,
		RepositoryContext: []agent.RepositoryContextItem{
			{Path: "main.go", ContentRedacted: "package main\n\nfunc main() {}"},
			{Path: "cmd/stats/main.go", ContentRedacted: "package main"},
		},
		Plan: agent.PlanProjection{Steps: []agent.PlanStep{
			{
				ID: "edit-1", Kind: agent.StepKindEdit,
				State: agent.StepPending, MaterialEdit: true,
				ValidationRequired: true,
				SummaryRedacted:    "Write cmd/stats/main.go",
				ExpectedFiles:      []string{"cmd/stats/main.go"},
				CompletionTools:    []executor.ToolName{executor.ToolApplyEdit},
			},
		}},
	})

	if !strings.Contains(rendered, "cmd/stats/main.go") {
		t.Fatalf("the round never names the file the plan owns:\n%s", rendered)
	}
	// The advice has to be in the instruction, not only in the context listing,
	// or it says nothing the context did not already say.
	instruction := rendered
	if cut := strings.Index(rendered, "what_to_do_now"); cut >= 0 {
		instruction = rendered[cut:]
	}
	if !strings.Contains(instruction, "cmd/stats/main.go") {
		t.Errorf("what_to_do_now does not name the writable files, so the "+
			"constraint is still only stated in the refusal:\n%s", instruction)
	}
}

// TestAPlanWithNothingOpenSaysSo is the control.
//
// Every step closed is a real state — it is what a run reaching its verification
// step looks like — and the advice has to remain a true sentence there rather
// than an empty list that reads as "write anywhere".
func TestAPlanWithNothingOpenSaysSo(t *testing.T) {
	rendered := observationJSON(agent.ModelInput{
		Round: 9,
		Plan: agent.PlanProjection{Steps: []agent.PlanStep{
			{
				ID: "edit-1", Kind: agent.StepKindEdit,
				State: agent.StepFailed, MaterialEdit: true,
				ValidationRequired: true,
				SummaryRedacted:    "Write cmd/stats/main.go",
				ExpectedFiles:      []string{"cmd/stats/main.go"},
				CompletionTools:    []executor.ToolName{executor.ToolApplyEdit},
			},
		}},
	})
	// Scoped to the instruction. The plan listing names the file legitimately:
	// a failed step is still part of the plan, and hiding it would hide what
	// the run tried.
	instruction := rendered
	if cut := strings.Index(rendered, "what_to_do_now"); cut >= 0 {
		instruction = rendered[cut:]
	}
	if strings.Contains(instruction, "cmd/stats/main.go") {
		t.Errorf("a step nothing can be bound to was offered as writable:\n%s",
			instruction)
	}
	if !strings.Contains(instruction, "no file you may change") {
		t.Errorf("the round should say plainly that nothing is writable:\n%s",
			instruction)
	}
}
