// Package redact removes credential material before persistence or delivery.
package redact

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/credentials"
)

const (
	Marker          = "[REDACTED]"
	truncatedMarker = "[TRUNCATED]"
	maxSecretBytes  = 2560
)

// Boundary identifies one mandatory redaction destination.
type Boundary string

const (
	BoundaryPromptPersistence Boundary = "prompt-persistence"
	BoundaryLogPersistence    Boundary = "log-persistence"
	BoundaryUIDelivery        Boundary = "ui-delivery"
	BoundaryDiagnosticExport  Boundary = "diagnostic-export"
)

// Limits bounds raw input before processing and redacted output afterward.
type Limits struct {
	MaximumInputBytes  int
	MaximumOutputBytes int
}

// Report is safe redaction metadata with no source content or span lengths.
type Report struct {
	Boundary        Boundary `json:"boundary"`
	Redactions      uint64   `json:"redactions"`
	InputTruncated  bool     `json:"input_truncated"`
	OutputTruncated bool     `json:"output_truncated"`
	EntropyEnabled  bool     `json:"entropy_enabled"`
}

// Result is one bounded redacted value.
type Result struct {
	Text   string
	Report Report
}

// Pipeline owns exact loaded values and common secret patterns.
type Pipeline struct {
	mu     sync.RWMutex
	exact  [][]byte
	limits Limits
	closed bool
}

// NewPipeline copies currently loaded credentials into the exact-match set.
func NewPipeline(loaded []credentials.Secret, limits Limits) (*Pipeline, error) {
	if limits.MaximumInputBytes < 4096 ||
		limits.MaximumOutputBytes < 1024 ||
		limits.MaximumOutputBytes > limits.MaximumInputBytes {
		return nil, errors.New("redaction limits are invalid")
	}
	pipeline := &Pipeline{limits: limits}
	for _, secret := range loaded {
		if err := secret.Use(func(value []byte) error {
			if len(value) > 0 {
				pipeline.exact = append(pipeline.exact, append([]byte(nil), value...))
			}
			return nil
		}); err != nil {
			pipeline.Close()
			return nil, err
		}
	}
	sort.Slice(pipeline.exact, func(left, right int) bool {
		return len(pipeline.exact[left]) > len(pipeline.exact[right])
	})
	return pipeline, nil
}

// Close clears the pipeline's owned exact-match material.
func (pipeline *Pipeline) Close() {
	if pipeline == nil {
		return
	}
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	for _, value := range pipeline.exact {
		clear(value)
	}
	pipeline.exact = nil
	pipeline.closed = true
}

// Redact applies the same pipeline at every supported boundary.
func (pipeline *Pipeline) Redact(boundary Boundary, input string) (Result, error) {
	if !boundary.valid() {
		return Result{}, errors.New("redaction boundary is invalid")
	}
	pipeline.mu.RLock()
	defer pipeline.mu.RUnlock()
	if pipeline.closed {
		return Result{}, errors.New("redaction pipeline is closed")
	}
	bounded, inputTruncated := boundInput([]byte(input), pipeline.limits.MaximumInputBytes)
	text := strings.ToValidUTF8(string(bounded), "\uFFFD")
	var count uint64
	for _, exact := range pipeline.exact {
		replacementCount := bytes.Count([]byte(text), exact)
		if replacementCount == 0 {
			continue
		}
		text = strings.ReplaceAll(text, string(exact), Marker)
		count += uint64(replacementCount)
	}
	for _, pattern := range patterns {
		var replacements int
		text = pattern.expression.ReplaceAllStringFunc(text, func(match string) string {
			replacements++
			return pattern.replace(match)
		})
		count += uint64(replacements)
	}
	text, outputTruncated := boundOutput(text, pipeline.limits.MaximumOutputBytes)
	return Result{
		Text: text,
		Report: Report{
			Boundary:        boundary,
			Redactions:      count,
			InputTruncated:  inputTruncated,
			OutputTruncated: outputTruncated,
			EntropyEnabled:  false,
		},
	}, nil
}

// Stream buffers a bounded sequence of raw chunks and redacts them together so
// a secret split at any chunk boundary is still matched.
type Stream struct {
	pipeline  *Pipeline
	boundary  Boundary
	buffer    []byte
	truncated bool
	finalized bool
}

// NewStream constructs a bounded chunk accumulator.
func (pipeline *Pipeline) NewStream(boundary Boundary) (*Stream, error) {
	if !boundary.valid() {
		return nil, errors.New("redaction boundary is invalid")
	}
	return &Stream{pipeline: pipeline, boundary: boundary}, nil
}

// Write adds raw bytes up to the pre-redaction input bound.
func (stream *Stream) Write(chunk []byte) (int, error) {
	if stream.finalized {
		return 0, errors.New("redaction stream is finalized")
	}
	accepted := len(chunk)
	remaining := stream.pipeline.limits.MaximumInputBytes - len(stream.buffer)
	if remaining <= 0 {
		stream.truncated = true
		return accepted, nil
	}
	if len(chunk) > remaining {
		stream.buffer = append(stream.buffer, chunk[:remaining]...)
		stream.truncated = true
		return accepted, nil
	}
	stream.buffer = append(stream.buffer, chunk...)
	return accepted, nil
}

// Finalize returns the single bounded redacted result.
func (stream *Stream) Finalize() (Result, error) {
	if stream.finalized {
		return Result{}, errors.New("redaction stream is finalized")
	}
	stream.finalized = true
	input := stream.buffer
	if stream.truncated {
		input, _ = boundInput(append(input, 0), stream.pipeline.limits.MaximumInputBytes)
	}
	result, err := stream.pipeline.Redact(stream.boundary, string(input))
	if err != nil {
		return Result{}, err
	}
	result.Report.InputTruncated = result.Report.InputTruncated || stream.truncated
	return result, nil
}

// RedactFields copies and redacts structured values before JSON serialization.
func (pipeline *Pipeline) RedactFields(
	boundary Boundary,
	fields map[string]any,
) ([]byte, Report, error) {
	if !boundary.valid() {
		return nil, Report{}, errors.New("redaction boundary is invalid")
	}
	report := Report{Boundary: boundary}
	redacted, err := pipeline.redactStructured(boundary, fields, &report)
	if err != nil {
		return nil, Report{}, err
	}
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return nil, Report{}, fmt.Errorf("serialize redacted fields: %w", err)
	}
	if len(encoded) > pipeline.limits.MaximumOutputBytes {
		encoded = []byte(`{"redacted":true,"truncated":true}`)
		report.OutputTruncated = true
	}
	report.EntropyEnabled = false
	return encoded, report, nil
}

func (pipeline *Pipeline) redactStructured(
	boundary Boundary,
	value any,
	report *Report,
) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			if sensitiveFieldName(key) {
				result[key] = Marker
				report.Redactions++
				continue
			}
			redacted, err := pipeline.redactStructured(boundary, child, report)
			if err != nil {
				return nil, err
			}
			result[key] = redacted
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			redacted, err := pipeline.redactStructured(boundary, child, report)
			if err != nil {
				return nil, err
			}
			result[index] = redacted
		}
		return result, nil
	case []string:
		result := make([]any, len(typed))
		for index, child := range typed {
			redacted, err := pipeline.redactStructured(boundary, child, report)
			if err != nil {
				return nil, err
			}
			result[index] = redacted
		}
		return result, nil
	case string:
		redacted, err := pipeline.Redact(boundary, typed)
		if err != nil {
			return nil, err
		}
		report.Redactions += redacted.Report.Redactions
		report.InputTruncated = report.InputTruncated || redacted.Report.InputTruncated
		report.OutputTruncated = report.OutputTruncated || redacted.Report.OutputTruncated
		return redacted.Text, nil
	case nil, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64,
		json.Number:
		return typed, nil
	default:
		return nil, fmt.Errorf("unsupported structured field type %T", value)
	}
}

func (boundary Boundary) valid() bool {
	switch boundary {
	case BoundaryPromptPersistence,
		BoundaryLogPersistence,
		BoundaryUIDelivery,
		BoundaryDiagnosticExport:
		return true
	default:
		return false
	}
}

type secretPattern struct {
	expression *regexp.Regexp
	replace    func(string) string
}

var patterns = []secretPattern{
	{
		expression: regexp.MustCompile(`(?s)-----BEGIN [^-]{0,64}PRIVATE KEY-----.*?-----END [^-]{0,64}PRIVATE KEY-----`),
		replace:    func(string) string { return Marker },
	},
	{
		expression: regexp.MustCompile(`(?i)\bBearer\s+(?:"[^"\r\n]{8,}"|'[^'\r\n]{8,}'|[A-Za-z0-9._~+/=-]{8,})`),
		replace: func(match string) string {
			index := strings.IndexAny(match, " \t")
			if index < 0 {
				return Marker
			}
			return match[:index] + " " + Marker
		},
	},
	{
		expression: regexp.MustCompile(`(?i)\b(authorization|x-api-key|api[-_ ]?key)\s*[:=]\s*(?:"[^"\r\n]{8,}"|'[^'\r\n]{8,}'|[^\s,;]{8,})`),
		replace: func(match string) string {
			index := strings.IndexAny(match, ":=")
			if index < 0 {
				return Marker
			}
			return strings.TrimSpace(match[:index]) + ": " + Marker
		},
	},
	{
		expression: regexp.MustCompile(`\bsk-(?:proj-|ant-)?[A-Za-z0-9_-]{16,}\b`),
		replace:    func(string) string { return Marker },
	},
	{
		expression: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
		replace:    func(string) string { return Marker },
	},
}

func sensitiveFieldName(name string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(name, "-", "_"))
	for _, fragment := range []string{
		"credential",
		"secret",
		"api_key",
		"access_token",
		"refresh_token",
		"password",
		"private_key",
		"authorization",
	} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func boundInput(input []byte, maximum int) ([]byte, bool) {
	if len(input) <= maximum {
		return input, false
	}
	keep := maximum - maxSecretBytes - len(truncatedMarker)
	if keep < 0 {
		keep = 0
	}
	keep = validUTF8Prefix(input, keep)
	result := append([]byte(nil), input[:keep]...)
	result = append(result, truncatedMarker...)
	return result, true
}

func boundOutput(output string, maximum int) (string, bool) {
	if len(output) <= maximum {
		return output, false
	}
	keep := maximum - len(truncatedMarker)
	if keep < 0 {
		keep = 0
	}
	keep = validUTF8Prefix([]byte(output), keep)
	return output[:keep] + truncatedMarker, true
}

func validUTF8Prefix(value []byte, maximum int) int {
	if maximum > len(value) {
		maximum = len(value)
	}
	for maximum > 0 && !utf8.Valid(value[:maximum]) {
		maximum--
	}
	return maximum
}
