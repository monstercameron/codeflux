package validation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	MaximumCapturedOutputBytes = 64 << 10
	MaximumParsedResultBytes   = 64 << 10
)

var (
	ErrInvalidValidationRun = errors.New("invalid validation run")
)

type ExecutableIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type WorktreeDiffBinding struct {
	WorktreeRevision string
	DiffIdentity     string
}

type RunIntent struct {
	ID                    domain.ValidationID
	TaskID                domain.TaskID
	RunID                 domain.RunID
	ProfileName           ProfileName
	ProfileVersion        string
	ProfileDigest         string
	CheckID               string
	CheckClass            CheckClass
	Required              bool
	WorktreeRevision      string
	DiffIdentity          string
	CommandDefinitionJSON string
	CommandFingerprint    string
	Executable            ExecutableIdentity
	Timeout               time.Duration
	IntentDigest          string
	IdempotencyKey        string
	CreatedAt             time.Time
}

type RunResult struct {
	ValidationRunID      domain.ValidationID
	State                domain.ValidationState
	ExitCode             int
	Duration             time.Duration
	TimedOut             bool
	Cancelled            bool
	StdoutRedacted       string
	StderrRedacted       string
	StdoutTruncated      bool
	StderrTruncated      bool
	ParserName           string
	ParseSucceeded       bool
	ParsedResultJSON     string
	RawRedactedSummary   string
	ObservedDiffIdentity string
	ResultDigest         string
	CompletedAt          time.Time
}

type Invalidation struct {
	ValidationRunID      domain.ValidationID
	PreviousDiffIdentity string
	CurrentDiffIdentity  string
	Reason               string
	CreatedAt            time.Time
}

type RequiredRerun struct {
	CheckID              string
	PreviousRunID        domain.ValidationID
	PreviousDiffIdentity string
	CurrentDiffIdentity  string
}

func (intent RunIntent) Validate() error {
	switch {
	case intent.ID.IsZero(), intent.TaskID.IsZero(), intent.RunID.IsZero():
		return ErrInvalidValidationRun
	case !intent.ProfileName.IsValid() || intent.ProfileVersion != ProfileVersionV1:
		return ErrInvalidValidationRun
	case !lowerHexDigest(intent.ProfileDigest), !intent.CheckClass.IsValid():
		return ErrInvalidValidationRun
	case strings.TrimSpace(intent.CheckID) != intent.CheckID || intent.CheckID == "" || len(intent.CheckID) > 255:
		return ErrInvalidValidationRun
	case !validGitRevision(intent.WorktreeRevision):
		return ErrInvalidValidationRun
	case !lowerHexDigest(intent.DiffIdentity), !lowerHexDigest(intent.CommandFingerprint):
		return ErrInvalidValidationRun
	case !json.Valid([]byte(intent.CommandDefinitionJSON)) || len(intent.CommandDefinitionJSON) > 16<<10:
		return ErrInvalidValidationRun
	case strings.TrimSpace(intent.Executable.Path) != intent.Executable.Path || intent.Executable.Path == "" || len(intent.Executable.Path) > 4096:
		return ErrInvalidValidationRun
	case !lowerHexDigest(intent.Executable.SHA256):
		return ErrInvalidValidationRun
	case intent.Timeout < time.Second || intent.Timeout > 30*time.Minute:
		return ErrInvalidValidationRun
	case strings.TrimSpace(intent.IdempotencyKey) != intent.IdempotencyKey || intent.IdempotencyKey == "" || len(intent.IdempotencyKey) > 255:
		return ErrInvalidValidationRun
	}
	digest, err := intent.computeDigest()
	if err != nil || intent.IntentDigest != digest {
		return ErrInvalidValidationRun
	}
	return nil
}

func (intent RunIntent) computeDigest() (string, error) {
	canonical := struct {
		ID                    string             `json:"id"`
		TaskID                string             `json:"task_id"`
		RunID                 string             `json:"run_id"`
		ProfileName           ProfileName        `json:"profile_name"`
		ProfileVersion        string             `json:"profile_version"`
		ProfileDigest         string             `json:"profile_digest"`
		CheckID               string             `json:"check_id"`
		CheckClass            CheckClass         `json:"check_class"`
		Required              bool               `json:"required"`
		WorktreeRevision      string             `json:"worktree_revision"`
		DiffIdentity          string             `json:"diff_identity"`
		CommandDefinitionJSON string             `json:"command_definition_json"`
		CommandFingerprint    string             `json:"command_fingerprint"`
		Executable            ExecutableIdentity `json:"executable"`
		TimeoutNanoseconds    int64              `json:"timeout_nanoseconds"`
	}{
		ID: intent.ID.String(), TaskID: intent.TaskID.String(), RunID: intent.RunID.String(),
		ProfileName: intent.ProfileName, ProfileVersion: intent.ProfileVersion,
		ProfileDigest: intent.ProfileDigest, CheckID: intent.CheckID,
		CheckClass: intent.CheckClass, Required: intent.Required,
		WorktreeRevision: intent.WorktreeRevision, DiffIdentity: intent.DiffIdentity,
		CommandDefinitionJSON: intent.CommandDefinitionJSON,
		CommandFingerprint:    intent.CommandFingerprint, Executable: intent.Executable,
		TimeoutNanoseconds: int64(intent.Timeout),
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return digestBytes(encoded), nil
}

func SealRunIntent(intent RunIntent) (RunIntent, error) {
	intent.IntentDigest = ""
	digest, err := intent.computeDigest()
	if err != nil {
		return RunIntent{}, err
	}
	intent.IntentDigest = digest
	if err := intent.Validate(); err != nil {
		return RunIntent{}, err
	}
	return intent, nil
}

func (result RunResult) Validate() error {
	if result.ValidationRunID.IsZero() || !result.State.IsValid() ||
		(result.State != domain.ValidationStatePassed && result.State != domain.ValidationStateFailed && result.State != domain.ValidationStateCancelled) ||
		result.Duration < 0 || len(result.StdoutRedacted) > MaximumCapturedOutputBytes ||
		len(result.StderrRedacted) > MaximumCapturedOutputBytes ||
		strings.TrimSpace(result.ParserName) != result.ParserName || result.ParserName == "" || len(result.ParserName) > 64 ||
		!json.Valid([]byte(result.ParsedResultJSON)) || len(result.ParsedResultJSON) > MaximumParsedResultBytes ||
		strings.TrimSpace(result.RawRedactedSummary) != result.RawRedactedSummary || result.RawRedactedSummary == "" || len(result.RawRedactedSummary) > MaximumCapturedOutputBytes ||
		!lowerHexDigest(result.ObservedDiffIdentity) {
		return ErrInvalidValidationRun
	}
	if result.State == domain.ValidationStatePassed && (result.ExitCode != 0 || result.TimedOut || result.Cancelled) {
		return ErrInvalidValidationRun
	}
	digest, err := result.computeDigest()
	if err != nil || result.ResultDigest != digest {
		return ErrInvalidValidationRun
	}
	return nil
}

func (result RunResult) computeDigest() (string, error) {
	canonical := struct {
		ValidationRunID      string                 `json:"validation_run_id"`
		State                domain.ValidationState `json:"state"`
		ExitCode             int                    `json:"exit_code"`
		DurationNanoseconds  int64                  `json:"duration_nanoseconds"`
		TimedOut             bool                   `json:"timed_out"`
		Cancelled            bool                   `json:"cancelled"`
		StdoutRedacted       string                 `json:"stdout_redacted"`
		StderrRedacted       string                 `json:"stderr_redacted"`
		StdoutTruncated      bool                   `json:"stdout_truncated"`
		StderrTruncated      bool                   `json:"stderr_truncated"`
		ParserName           string                 `json:"parser_name"`
		ParseSucceeded       bool                   `json:"parse_succeeded"`
		ParsedResultJSON     string                 `json:"parsed_result_json"`
		RawRedactedSummary   string                 `json:"raw_redacted_summary"`
		ObservedDiffIdentity string                 `json:"observed_diff_identity"`
	}{
		ValidationRunID: result.ValidationRunID.String(), State: result.State,
		ExitCode: result.ExitCode, DurationNanoseconds: int64(result.Duration),
		TimedOut: result.TimedOut, Cancelled: result.Cancelled,
		StdoutRedacted: result.StdoutRedacted, StderrRedacted: result.StderrRedacted,
		StdoutTruncated: result.StdoutTruncated, StderrTruncated: result.StderrTruncated,
		ParserName: result.ParserName, ParseSucceeded: result.ParseSucceeded,
		ParsedResultJSON: result.ParsedResultJSON, RawRedactedSummary: result.RawRedactedSummary,
		ObservedDiffIdentity: result.ObservedDiffIdentity,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return digestBytes(encoded), nil
}

func SealRunResult(result RunResult) (RunResult, error) {
	result.ResultDigest = ""
	digest, err := result.computeDigest()
	if err != nil {
		return RunResult{}, err
	}
	result.ResultDigest = digest
	if err := result.Validate(); err != nil {
		return RunResult{}, err
	}
	return result, nil
}

func commandDefinition(check Check) (string, error) {
	definition := struct {
		Class              CheckClass `json:"class"`
		Arguments          []string   `json:"arguments"`
		Source             string     `json:"source"`
		CommandFingerprint string     `json:"command_fingerprint"`
		TimeoutNanoseconds int64      `json:"timeout_nanoseconds"`
	}{
		Class: check.Class, Arguments: slices.Clone(check.Arguments), Source: check.Source,
		CommandFingerprint: check.CommandFingerprint, TimeoutNanoseconds: int64(check.Timeout),
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return "", fmt.Errorf("encode validation command definition: %w", err)
	}
	return string(encoded), nil
}

func lowerHexDigest(value string) bool {
	return len(value) == 64 && lowerHex(value)
}

func validGitRevision(value string) bool {
	return (len(value) == 40 || len(value) == 64) && lowerHex(value)
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
