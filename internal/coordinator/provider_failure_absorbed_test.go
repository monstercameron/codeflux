package coordinator

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/providers"
)

// TestProviderFailuresAreTheKindAnAttemptAbsorbs pins which errors cost an
// attempt rather than the run.
//
// Observed on ladder rung 2 on 2026-08-03: the run escalated to a higher rung,
// the very next model call failed with "provider retry budget exhausted:
// provider call the model failed: transport", and the whole run ended — six
// attempts of work that had already passed their gates, discarded because the
// seventh could not reach the provider.
//
// The retry executor has already spent its own budget by the time this is
// reached, so absorbing it here is not a second retry layer; it is the attempt
// loop doing what it does for a malformed turn, which the code had already
// decided was the right shape for "a failure that is not the work's fault".
func TestProviderFailuresAreTheKindAnAttemptAbsorbs(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		err      error
		absorbed bool
	}{
		{"retry budget exhausted", providers.ErrRetryBudgetExhausted, true},
		{"transport failure", providers.ErrTransport, true},
		{"rate limited", providers.ErrRateLimited, true},
		{"wrapped, as the loop reports it", fmt.Errorf(
			"agent fixed model turn: %w", providers.ErrRetryBudgetExhausted), true},
		{"a database that will not open", errors.New("open database: no such file"), false},
		{"authentication, which retrying cannot fix", providers.ErrAuthentication, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			absorbed := errors.Is(testCase.err, providers.ErrRetryBudgetExhausted) ||
				errors.Is(testCase.err, providers.ErrTransport) ||
				errors.Is(testCase.err, providers.ErrRateLimited)
			if absorbed != testCase.absorbed {
				t.Errorf("absorbed = %t, want %t for %v",
					absorbed, testCase.absorbed, testCase.err)
			}
		})
	}
}

// TestTheProviderFailureInstructionSaysTheWorkSurvived is the part most likely
// to be misread by the next attempt.
//
// Nothing is rolled back when a model call fails, so an instruction that read
// like a refusal would invite a rewrite of work that was already accepted.
func TestTheProviderFailureInstructionSaysTheWorkSurvived(t *testing.T) {
	instruction := providerFailureInstruction(providers.ErrTransport)
	for _, wanted := range []string{
		"could not reach the model",
		"Nothing was rolled back",
		"still in the worktree",
		providers.ErrTransport.Error(),
	} {
		if !strings.Contains(instruction, wanted) {
			t.Errorf("the instruction should contain %q, got %q",
				wanted, instruction)
		}
	}
}
