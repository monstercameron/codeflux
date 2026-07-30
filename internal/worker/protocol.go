package worker

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

const ProtocolVersion = 1

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
		endpoint.Path != "" || endpoint.RawQuery != "" {
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
	return nil
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
