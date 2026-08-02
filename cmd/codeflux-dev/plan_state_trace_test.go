package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

// backtickedToken matches one inline-code token in the plan that could name a
// lifecycle state. Multi-word phrases, paths, and calls are excluded by shape.
var backtickedToken = regexp.MustCompile("`([A-Za-z][A-Za-z0-9-]*)`")

// planStateExemptions records inline-code tokens whose first word coincides
// with a lifecycle-state word while naming something that is not a state. Each
// entry is a narrow review record with its reason, not a blanket bypass.
var planStateExemptions = map[string]string{
	"RecoveryCard": "frontend component name in §27C, not a lifecycle state",
}

// declaredStateValues returns every state value declared by every state machine
// in internal/domain, keyed by the value itself.
func declaredStateValues() map[string]struct{} {
	values := make(map[string]struct{})
	add := func(value string) { values[value] = struct{}{} }

	for _, state := range domain.AllTaskStates() {
		add(string(state))
	}
	for _, state := range domain.AllRunStates() {
		add(string(state))
	}
	for _, state := range domain.AllCommandExecutionStates() {
		add(string(state))
	}
	for _, state := range []domain.ApprovalRequestState{
		domain.ApprovalRequestStatePending,
		domain.ApprovalRequestStateGranted,
		domain.ApprovalRequestStateDenied,
		domain.ApprovalRequestStateExpired,
		domain.ApprovalRequestStateCancelled,
	} {
		add(string(state))
	}
	for _, state := range []domain.CheckpointState{
		domain.CheckpointStateCreating,
		domain.CheckpointStateReady,
		domain.CheckpointStateFailed,
		domain.CheckpointStateInvalidated,
	} {
		add(string(state))
	}
	for _, state := range []domain.ValidationState{
		domain.ValidationStatePending,
		domain.ValidationStateRunning,
		domain.ValidationStatePassed,
		domain.ValidationStateFailed,
		domain.ValidationStateWaived,
		domain.ValidationStateSkipped,
		domain.ValidationStateCancelled,
		domain.ValidationStateInvalidated,
	} {
		add(string(state))
	}
	for _, state := range []domain.GraphRevisionState{
		domain.GraphRevisionStateDraft,
		domain.GraphRevisionStateValidating,
		domain.GraphRevisionStateCommitted,
		domain.GraphRevisionStateRejected,
		domain.GraphRevisionStateSuperseded,
		domain.GraphRevisionStateInvalidated,
	} {
		add(string(state))
	}
	for _, state := range []domain.ChangeAcceptanceState{
		domain.ChangeAcceptanceStatePending,
		domain.ChangeAcceptanceStateAccepted,
		domain.ChangeAcceptanceStateRepairRequested,
		domain.ChangeAcceptanceStateRejected,
		domain.ChangeAcceptanceStateRolledBack,
	} {
		add(string(state))
	}
	return values
}

// stateLeadingWords returns the set of words that begin a declared state value.
// A plan token whose first word is one of these is read as claiming to name a
// lifecycle state and must resolve to a declared one.
func stateLeadingWords(declared map[string]struct{}) map[string]struct{} {
	leading := make(map[string]struct{})
	for value := range declared {
		leading[strings.SplitN(value, "-", 2)[0]] = struct{}{}
	}
	return leading
}

// normalizePlanStateToken converts a CamelCase or kebab-case inline-code token
// to the kebab-case form the domain package declares.
func normalizePlanStateToken(token string) string {
	if strings.Contains(token, "-") {
		return strings.ToLower(token)
	}
	var builder strings.Builder
	for index, char := range token {
		if index > 0 && char >= 'A' && char <= 'Z' {
			builder.WriteByte('-')
		}
		builder.WriteRune(char)
	}
	return strings.ToLower(builder.String())
}

func readPlan(t *testing.T) string {
	t.Helper()
	root := repositoryRootForCommandGraph(t)
	source, err := os.ReadFile(filepath.Join(root, "docs", "plan.md"))
	if err != nil {
		t.Fatalf("docs/plan.md must exist and be tracked: %v", err)
	}
	return string(source)
}

// TestAUDIT001_EveryLifecycleStateNamedByThePlanIsDeclared covers AUDIT-001.
//
// The plan asserted a task reaches `AwaitingAcceptance`, a state no machine in
// internal/domain declares, so the acceptance definition named a lifecycle
// position that could never be reached. Prose cannot be trusted to stay aligned
// with the machine, so the trace is executable: any inline-code token whose
// leading word begins a declared state value is read as claiming to name a
// state, and must resolve to one.
func TestAUDIT001_EveryLifecycleStateNamedByThePlanIsDeclared(t *testing.T) {
	declared := declaredStateValues()
	leading := stateLeadingWords(declared)
	plan := readPlan(t)

	claimed := make(map[string]string)
	for _, match := range backtickedToken.FindAllStringSubmatch(plan, -1) {
		token := match[1]
		if _, exempt := planStateExemptions[token]; exempt {
			continue
		}
		normalized := normalizePlanStateToken(token)
		if _, isStateWord := leading[strings.SplitN(normalized, "-", 2)[0]]; !isStateWord {
			continue
		}
		claimed[token] = normalized
	}

	if len(claimed) == 0 {
		t.Fatal("no lifecycle state is named by the plan; the trace would pass vacuously")
	}

	var undeclared []string
	for token, normalized := range claimed {
		if _, ok := declared[normalized]; !ok {
			undeclared = append(undeclared, token+" -> "+normalized)
		}
	}
	sort.Strings(undeclared)
	if len(undeclared) > 0 {
		t.Fatalf(
			"docs/plan.md names %d lifecycle state(s) no machine in internal/domain declares: %s",
			len(undeclared), strings.Join(undeclared, ", "),
		)
	}
}

// TestAUDIT001_TaskLifecycleStatesNamedByThePlanParseThroughAllTaskStates
// covers AUDIT-001's narrower claim: a token the plan uses for the task
// lifecycle must parse through domain.AllTaskStates specifically, not merely
// through some other machine that happens to share the word.
func TestAUDIT001_TaskLifecycleStatesNamedByThePlanParseThroughAllTaskStates(t *testing.T) {
	taskStates := make(map[string]struct{})
	for _, state := range domain.AllTaskStates() {
		taskStates[string(state)] = struct{}{}
	}

	// "awaiting" begins only task and command-execution states, and every
	// awaiting-* position the plan discusses is a task lifecycle position.
	const taskLifecyclePrefix = "awaiting-"

	plan := readPlan(t)
	found := 0
	for _, match := range backtickedToken.FindAllStringSubmatch(plan, -1) {
		token := match[1]
		if _, exempt := planStateExemptions[token]; exempt {
			continue
		}
		normalized := normalizePlanStateToken(token)
		if !strings.HasPrefix(normalized, taskLifecyclePrefix) {
			continue
		}
		found++
		state := domain.TaskState(normalized)
		if !state.IsValid() {
			t.Errorf(
				"docs/plan.md names task state %q (%q), which does not parse through domain.AllTaskStates",
				token, normalized,
			)
		}
		if _, ok := taskStates[normalized]; !ok {
			t.Errorf("task state %q is absent from domain.AllTaskStates", normalized)
		}
	}
	if found == 0 {
		t.Fatal("the plan names no awaiting-* task state; the acceptance definition lost its lifecycle anchor")
	}
}
