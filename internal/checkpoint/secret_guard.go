package checkpoint

import (
	"errors"

	"codeflux.dev/codeflux/internal/redact"
)

// RedactionPipeline is implemented by redact.Pipeline.
type RedactionPipeline interface {
	Redact(redact.Boundary, string) (redact.Result, error)
}

// RedactionSecretGuard uses the application redaction pipeline as a rejection
// gate. Checkpoint identities are never silently rewritten.
type RedactionSecretGuard struct {
	pipeline RedactionPipeline
}

func NewRedactionSecretGuard(
	pipeline RedactionPipeline,
) (*RedactionSecretGuard, error) {
	if pipeline == nil {
		return nil, errors.New("checkpoint redaction pipeline is required")
	}
	return &RedactionSecretGuard{pipeline: pipeline}, nil
}

func (guard *RedactionSecretGuard) EnsureCheckpointSecretFree(
	value string,
) error {
	if guard == nil || guard.pipeline == nil {
		return errors.New("checkpoint secret guard is unavailable")
	}
	result, err := guard.pipeline.Redact(
		redact.BoundaryLogPersistence,
		value,
	)
	if err != nil {
		return err
	}
	if result.Report.Redactions != 0 ||
		result.Report.InputTruncated ||
		result.Report.OutputTruncated ||
		result.Text != value {
		return errors.New(
			"checkpoint state contains credential material or exceeds the redaction bound",
		)
	}
	return nil
}

var _ SecretGuard = (*RedactionSecretGuard)(nil)
