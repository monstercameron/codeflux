package worker

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

const ProtocolVersion = 1

const (
	maxStartupBytes       = 64 << 10
	maxControlReasonBytes = 2 << 10
	maxReportSummaryBytes = 8 << 10
)

// StartupParameters are the complete credential-free worker bootstrap contract.
type StartupParameters struct {
	ProtocolVersion     int
	TaskID              domain.TaskID
	RunID               domain.RunID
	WorktreePath        string
	PolicyRevision      uint64
	ToolSchemaVersion   int
	CoordinatorEndpoint string
	SessionToken        string
	ContainerCommand    []string
}

func (parameters StartupParameters) Validate() error {
	if parameters.ProtocolVersion != ProtocolVersion {
		return errors.New("worker protocol version mismatch")
	}
	if parameters.TaskID.IsZero() || parameters.RunID.IsZero() {
		return errors.New("worker task and run IDs are required")
	}
	if !filepath.IsAbs(parameters.WorktreePath) ||
		filepath.Dir(filepath.Clean(parameters.WorktreePath)) == filepath.Clean(parameters.WorktreePath) {
		return errors.New("worker worktree must be an absolute non-root path")
	}
	if parameters.ToolSchemaVersion < 1 {
		return errors.New("worker tool schema version is invalid")
	}
	endpoint, err := url.Parse(parameters.CoordinatorEndpoint)
	if err != nil || endpoint.Scheme != "http" || endpoint.Host == "" ||
		endpoint.User != nil || endpoint.Path != "" || endpoint.RawPath != "" ||
		endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
		return errors.New("worker coordinator endpoint is invalid")
	}
	host, _, err := net.SplitHostPort(endpoint.Host)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("worker coordinator endpoint must be loopback")
	}
	if len(parameters.SessionToken) < 32 || len(parameters.SessionToken) > 255 ||
		strings.TrimSpace(parameters.SessionToken) != parameters.SessionToken {
		return errors.New("worker session token is invalid")
	}
	for _, character := range parameters.SessionToken {
		if !((character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_') {
			return errors.New("worker session token is invalid")
		}
	}
	if len(parameters.ContainerCommand) > 64 {
		return errors.New("worker container command exceeds bounds")
	}
	for _, argument := range parameters.ContainerCommand {
		if argument == "" || strings.ContainsRune(argument, 0) {
			return errors.New("worker container argument is invalid")
		}
	}
	return nil
}

type ControlKind string

const (
	ControlPause      ControlKind = "pause"
	ControlResume     ControlKind = "resume"
	ControlCancel     ControlKind = "cancel"
	ControlCheckpoint ControlKind = "checkpoint"
	ControlShutdown   ControlKind = "shutdown"
)

type StatusKind string

const (
	StatusStarting     StatusKind = "starting"
	StatusRunning      StatusKind = "running"
	StatusPaused       StatusKind = "paused"
	StatusCheckpointed StatusKind = "checkpointed"
	StatusStopping     StatusKind = "stopping"
	StatusExited       StatusKind = "exited"
	StatusFailed       StatusKind = "failed"
)

// Message is one ordered authenticated coordinator/worker fact.
type Message struct {
	ProtocolVersion int
	TaskID          domain.TaskID
	RunID           domain.RunID
	Sequence        uint64
	SessionToken    string
	Heartbeat       *Heartbeat
	Control         *Control
	Status          *Status
	ToolEvent       *ToolEvent
}

type Heartbeat struct {
	WorkerPID      int
	LeaseID        string
	State          StatusKind
	ObservedAt     time.Time
	LastCheckpoint string
}

type Control struct {
	Kind   ControlKind
	Reason string
}

type Status struct {
	Kind       StatusKind
	Summary    string
	OccurredAt time.Time
}

type ToolEvent struct {
	RequestID string
	State     string
	Summary   string
}

func (message Message) Validate(expectedToken string) error {
	if message.ProtocolVersion != ProtocolVersion ||
		message.TaskID.IsZero() || message.RunID.IsZero() ||
		message.Sequence == 0 {
		return errors.New("worker message envelope is invalid")
	}
	if !AuthenticateSession(expectedToken, message.SessionToken) {
		return errors.New("worker message authentication failed")
	}
	payloads := 0
	for _, present := range []bool{
		message.Heartbeat != nil, message.Control != nil,
		message.Status != nil, message.ToolEvent != nil,
	} {
		if present {
			payloads++
		}
	}
	if payloads != 1 {
		return errors.New("worker message must contain exactly one payload")
	}
	switch {
	case message.Heartbeat != nil:
		return message.Heartbeat.validate()
	case message.Control != nil:
		return message.Control.Validate()
	case message.Status != nil:
		return message.Status.validate()
	case message.ToolEvent != nil:
		return message.ToolEvent.validate()
	}
	return nil
}

func (heartbeat Heartbeat) validate() error {
	if heartbeat.WorkerPID < 1 || heartbeat.ObservedAt.IsZero() ||
		!validHeartbeatStatus(heartbeat.State) ||
		!validOptionalIdentifier(heartbeat.LeaseID) ||
		!validOptionalIdentifier(heartbeat.LastCheckpoint) {
		return errors.New("worker heartbeat is invalid")
	}
	return nil
}

// Validate checks a coordinator control before a worker acts on it.
func (control Control) Validate() error {
	switch control.Kind {
	case ControlPause, ControlResume, ControlCancel, ControlCheckpoint,
		ControlShutdown:
	default:
		return errors.New("worker control kind is invalid")
	}
	if len(control.Reason) > maxControlReasonBytes ||
		strings.ContainsRune(control.Reason, 0) {
		return errors.New("worker control reason is invalid")
	}
	return nil
}

func (status Status) validate() error {
	if !validStatus(status.Kind) || status.OccurredAt.IsZero() ||
		len(status.Summary) > maxReportSummaryBytes ||
		strings.ContainsRune(status.Summary, 0) {
		return errors.New("worker status report is invalid")
	}
	return nil
}

func (event ToolEvent) validate() error {
	if !validRequiredIdentifier(event.RequestID) ||
		!validRequiredIdentifier(event.State) ||
		len(event.Summary) > maxReportSummaryBytes ||
		strings.ContainsRune(event.Summary, 0) {
		return errors.New("worker tool report is invalid")
	}
	return nil
}

func validHeartbeatStatus(status StatusKind) bool {
	switch status {
	case StatusStarting, StatusRunning, StatusPaused, StatusStopping:
		return true
	default:
		return false
	}
}

func validStatus(status StatusKind) bool {
	switch status {
	case StatusStarting, StatusRunning, StatusPaused, StatusCheckpointed,
		StatusStopping, StatusExited, StatusFailed:
		return true
	default:
		return false
	}
}

func validRequiredIdentifier(value string) bool {
	return value != "" && len(value) <= 255 &&
		strings.TrimSpace(value) == value && !strings.ContainsRune(value, 0)
}

func validOptionalIdentifier(value string) bool {
	return value == "" || validRequiredIdentifier(value)
}

func AuthenticateSession(expected, presented string) bool {
	if len(expected) < 32 || len(expected) != len(presented) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(presented)) == 1
}

type ReconnectPolicy struct {
	MaximumAttempts int
	InitialDelay    time.Duration
	MaximumDelay    time.Duration
}

func (policy ReconnectPolicy) Validate() error {
	if policy.MaximumAttempts < 1 || policy.MaximumAttempts > 10 ||
		policy.InitialDelay < 10*time.Millisecond ||
		policy.MaximumDelay < policy.InitialDelay ||
		policy.MaximumDelay > 30*time.Second {
		return errors.New("worker reconnect policy is outside supported bounds")
	}
	return nil
}

func (policy ReconnectPolicy) Delay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	delay := policy.InitialDelay
	for index := 1; index < attempt && delay < policy.MaximumDelay; index++ {
		delay = min(delay*2, policy.MaximumDelay)
	}
	return delay
}

func decodeSingleJSON(reader io.Reader, maximum int64, target any) error {
	limited := &io.LimitedReader{R: reader, N: maximum + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contains trailing data")
		}
		return err
	}
	if limited.N == 0 {
		return errors.New("JSON exceeds size limit")
	}
	return nil
}
