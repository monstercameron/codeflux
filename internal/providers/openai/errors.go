package openai

import (
	"context"
	"errors"
	"net/http"

	"codeflux.dev/codeflux/internal/providers"
)

type errorEnvelope struct {
	Error struct {
		Type string `json:"type"`
		Code string `json:"code"`
	} `json:"error"`
}

func classifyFailure(
	operation string,
	err error,
	partial providers.PartialEffectEvidence,
) error {
	if err == nil {
		return nil
	}
	var existing *providers.Failure
	if errors.As(err, &existing) {
		existing.PartialEffect = partial
		if hasPartialEffect(partial) {
			existing.Retryable = false
		}
		return existing
	}
	failure := &providers.Failure{
		Operation: operation, PartialEffect: partial, Cause: err,
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, providers.ErrCanceled):
		failure.Kind = providers.FailureCanceled
		failure.Retryable = false
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, providers.ErrTimeout):
		failure.Kind = providers.FailureTimeout
		failure.Retryable = !hasPartialEffect(partial)
	case errors.Is(err, providers.ErrAuthentication):
		failure.Kind = providers.FailureAuthentication
	case errors.Is(err, providers.ErrInvalidRequest):
		failure.Kind = providers.FailureInvalidRequest
	default:
		var status *providers.HTTPStatusError
		if errors.As(err, &status) {
			var envelope errorEnvelope
			_ = status.DecodeBody(&envelope)
			failure.RetryAfter = status.RetryAfter
			switch {
			case envelope.Error.Code == "content_policy_violation":
				failure.Kind = providers.FailureSafety
			case status.StatusCode == http.StatusUnauthorized ||
				status.StatusCode == http.StatusForbidden:
				failure.Kind = providers.FailureAuthentication
			case status.StatusCode == http.StatusRequestTimeout:
				failure.Kind = providers.FailureTimeout
				failure.Retryable = !hasPartialEffect(partial)
			case status.StatusCode == http.StatusTooManyRequests:
				failure.Kind = providers.FailureRateLimited
				failure.Retryable = !hasPartialEffect(partial)
			case status.StatusCode >= http.StatusInternalServerError:
				failure.Kind = providers.FailureUnavailable
				failure.Retryable = !hasPartialEffect(partial)
			default:
				failure.Kind = providers.FailureInvalidRequest
			}
		} else {
			failure.Kind = providers.FailureTransport
			failure.Retryable = !hasPartialEffect(partial)
		}
	}
	failure.Cause = errors.Join(failureSentinel(failure.Kind), err)
	return failure
}

func hasPartialEffect(partial providers.PartialEffectEvidence) bool {
	return partial.StreamedOutput || partial.ToolCall || partial.ProviderAck
}

func failureSentinel(kind providers.FailureKind) error {
	switch kind {
	case providers.FailureTransport:
		return providers.ErrTransport
	case providers.FailureTimeout:
		return providers.ErrTimeout
	case providers.FailureRateLimited:
		return providers.ErrRateLimited
	case providers.FailureAuthentication:
		return providers.ErrAuthentication
	case providers.FailureInvalidRequest:
		return providers.ErrInvalidRequest
	case providers.FailureSafety:
		return providers.ErrSafety
	case providers.FailureUnavailable:
		return providers.ErrUnavailable
	case providers.FailureCanceled:
		return providers.ErrCanceled
	default:
		return providers.ErrTransport
	}
}
