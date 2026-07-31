// Package validationbaseline models exact, honest pre-change versus post-change
// validation comparisons. It owns no command execution or repository access.
package validationbaseline

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

const (
	MaximumAttempts               = 8
	MinimumNondeterminismEvidence = 3
	MaximumIdentityBytes          = 512
	MaximumSafeReasonBytes        = 1024
)

var ErrInvalidBaselineEvidence = errors.New("invalid validation baseline evidence")

type ValidationError struct{ Field, Reason string }

func (failure *ValidationError) Error() string {
	return fmt.Sprintf("%s is invalid: %s", failure.Field, failure.Reason)
}
func (failure *ValidationError) Unwrap() error { return ErrInvalidBaselineEvidence }

// CommandBinding identifies both the immutable command definition and the
// executable that interpreted it. Comparisons never match by display text.
type CommandBinding struct {
	DefinitionID       string
	ExecutableIdentity string
}

func (binding CommandBinding) Validate() error {
	if !validSafeText(binding.DefinitionID, MaximumIdentityBytes) {
		return invalid("command.definition_id", "must be bounded safe text")
	}
	if !validSafeText(binding.ExecutableIdentity, MaximumIdentityBytes) {
		return invalid("command.executable_identity", "must be bounded safe text")
	}
	return nil
}

// RevisionBinding identifies the exact worktree content and dirty-diff state
// observed by one command execution.
type RevisionBinding struct {
	WorktreeRevision string
	DiffIdentity     string
}

func (binding RevisionBinding) Validate() error {
	if !validSafeText(binding.WorktreeRevision, MaximumIdentityBytes) {
		return invalid("revision.worktree", "must be bounded safe text")
	}
	if !validSafeText(binding.DiffIdentity, MaximumIdentityBytes) {
		return invalid("revision.diff", "must be bounded safe text")
	}
	return nil
}

type Binding struct {
	Command  CommandBinding
	Revision RevisionBinding
}

func (binding Binding) Validate() error {
	if err := binding.Command.Validate(); err != nil {
		return err
	}
	return binding.Revision.Validate()
}

type AttemptStatus string

const (
	AttemptPassed      AttemptStatus = "passed"
	AttemptFailed      AttemptStatus = "failed"
	AttemptUnavailable AttemptStatus = "unavailable"
)

func (status AttemptStatus) IsValid() bool {
	return status == AttemptPassed || status == AttemptFailed || status == AttemptUnavailable
}

type Attempt struct {
	Ordinal            uint32
	Status             AttemptStatus
	FailureFingerprint string
	UnavailableReason  string
}

func (attempt Attempt) Validate() error {
	if attempt.Ordinal == 0 || !attempt.Status.IsValid() {
		return invalid("attempt", "ordinal and status are required")
	}
	switch attempt.Status {
	case AttemptPassed:
		if attempt.FailureFingerprint != "" || attempt.UnavailableReason != "" {
			return invalid("attempt", "passed evidence cannot carry failure or unavailable detail")
		}
	case AttemptFailed:
		if !validSafeText(attempt.FailureFingerprint, MaximumIdentityBytes) || attempt.UnavailableReason != "" {
			return invalid("attempt.failure_fingerprint", "failed evidence requires one bounded fingerprint")
		}
	case AttemptUnavailable:
		if attempt.FailureFingerprint != "" || !validSafeText(attempt.UnavailableReason, MaximumSafeReasonBytes) {
			return invalid("attempt.unavailable_reason", "unavailable evidence requires an explicit safe reason")
		}
	}
	return nil
}

type Evidence struct {
	binding  Binding
	attempts []Attempt
}

func NewEvidence(binding Binding, attempts []Attempt) (Evidence, error) {
	evidence := Evidence{binding: binding, attempts: slices.Clone(attempts)}
	if err := evidence.Validate(); err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}

func (evidence Evidence) Binding() Binding    { return evidence.binding }
func (evidence Evidence) Attempts() []Attempt { return slices.Clone(evidence.attempts) }

func (evidence Evidence) Validate() error {
	if err := evidence.binding.Validate(); err != nil {
		return err
	}
	if len(evidence.attempts) == 0 || len(evidence.attempts) > MaximumAttempts {
		return invalid("attempts", "must contain a bounded non-empty observation set")
	}
	for index, attempt := range evidence.attempts {
		if err := attempt.Validate(); err != nil {
			return err
		}
		if attempt.Ordinal != uint32(index+1) {
			return invalid("attempts", "ordinals must be contiguous and ordered")
		}
		if attempt.Status == AttemptUnavailable && len(evidence.attempts) != 1 {
			return invalid("attempts", "unavailable evidence cannot be mixed with executed attempts")
		}
	}
	return nil
}

func validSafeText(value string, maximum int) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func invalid(field, reason string) error { return &ValidationError{Field: field, Reason: reason} }
