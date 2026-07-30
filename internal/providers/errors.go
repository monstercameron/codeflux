package providers

import (
	"errors"
	"fmt"
	"time"
)

type FailureKind string

const (
	FailureTransport      FailureKind = "transport"
	FailureTimeout        FailureKind = "timeout"
	FailureRateLimited    FailureKind = "rate-limited"
	FailureAuthentication FailureKind = "authentication"
	FailureInvalidRequest FailureKind = "invalid-request"
	FailureSafety         FailureKind = "safety"
	FailureUnavailable    FailureKind = "unavailable"
	FailureCanceled       FailureKind = "canceled"
)

var (
	ErrTransport      = errors.New("provider transport failure")
	ErrTimeout        = errors.New("provider timeout")
	ErrRateLimited    = errors.New("provider rate limited")
	ErrAuthentication = errors.New("provider authentication failed")
	ErrInvalidRequest = errors.New("provider request invalid")
	ErrSafety         = errors.New("provider safety refusal")
	ErrUnavailable    = errors.New("provider unavailable")
	ErrCanceled       = errors.New("provider request canceled")
)

type Failure struct {
	Kind          FailureKind
	Operation     string
	Retryable     bool
	RetryAfter    time.Duration
	PartialEffect PartialEffectEvidence
	Cause         error
}

func (failure *Failure) Error() string {
	if failure == nil {
		return "provider failure"
	}
	if failure.Operation == "" {
		return fmt.Sprintf("provider %s failure", failure.Kind)
	}
	return fmt.Sprintf("provider %s failed: %s", failure.Operation, failure.Kind)
}

func (failure *Failure) Unwrap() error {
	if failure == nil {
		return nil
	}
	if failure.Cause != nil {
		return failure.Cause
	}
	switch failure.Kind {
	case FailureTransport:
		return ErrTransport
	case FailureTimeout:
		return ErrTimeout
	case FailureRateLimited:
		return ErrRateLimited
	case FailureAuthentication:
		return ErrAuthentication
	case FailureInvalidRequest:
		return ErrInvalidRequest
	case FailureSafety:
		return ErrSafety
	case FailureUnavailable:
		return ErrUnavailable
	case FailureCanceled:
		return ErrCanceled
	default:
		return nil
	}
}
