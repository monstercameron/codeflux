package taskprojection

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrCommandInFlight     = errors.New("task command already owns an idempotency key")
	ErrCommandKeyMismatch  = errors.New("task command settlement key does not match")
	ErrInvalidCommandState = errors.New("task command state is invalid")
)

type CommandStatus string

const (
	CommandIdle         CommandStatus = "idle"
	CommandBusy         CommandStatus = "busy"
	CommandCommitted    CommandStatus = "committed"
	CommandStale        CommandStatus = "stale"
	CommandDenied       CommandStatus = "denied"
	CommandDisconnected CommandStatus = "disconnected"
	CommandFailed       CommandStatus = "failed"
)

type CommandKey string

func ParseCommandKey(raw string) (CommandKey, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 16 || len(raw) > 128 {
		return "", errors.New("command idempotency key length is invalid")
	}
	for _, character := range raw {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_' {
			continue
		}
		return "", errors.New("command idempotency key contains an invalid character")
	}
	return CommandKey(raw), nil
}

type CommandState struct {
	Action              ActionKind
	Key                 CommandKey
	ExpectedRevision    uint64
	Status              CommandStatus
	ChangedEntity       string
	SafeExplanation     string
	RefreshRequested    bool
	SettlementUncertain bool
}

func (state CommandState) OwnsKey() bool {
	switch state.Status {
	case CommandBusy, CommandStale, CommandDenied, CommandDisconnected, CommandFailed:
		return state.Key != ""
	default:
		return false
	}
}

func BeginCommand(
	current CommandState,
	action ActionKind,
	key CommandKey,
	expectedRevision uint64,
) (CommandState, error) {
	if current.OwnsKey() {
		return current, ErrCommandInFlight
	}
	if _, err := ParseCommandKey(string(key)); err != nil {
		return current, err
	}
	if action == "" || expectedRevision == 0 {
		return current, ErrInvalidCommandState
	}
	return CommandState{
		Action: action, Key: key, ExpectedRevision: expectedRevision, Status: CommandBusy,
	}, nil
}

type CommandSettlementKind string

const (
	SettlementCommitted    CommandSettlementKind = "committed"
	SettlementStale        CommandSettlementKind = "stale"
	SettlementDenied       CommandSettlementKind = "denied"
	SettlementDisconnected CommandSettlementKind = "disconnected"
	SettlementFailed       CommandSettlementKind = "failed"
)

type CommandSettlement struct {
	Key                   CommandKey
	Kind                  CommandSettlementKind
	AuthoritativeRevision uint64
	ChangedEntity         string
	SafeExplanation       string
}

func SettleCommand(current CommandState, settlement CommandSettlement) (CommandState, error) {
	if current.Status != CommandBusy || current.Key == "" {
		return current, ErrInvalidCommandState
	}
	if settlement.Key != current.Key {
		return current, ErrCommandKeyMismatch
	}
	next := current
	switch settlement.Kind {
	case SettlementCommitted:
		if settlement.AuthoritativeRevision < current.ExpectedRevision {
			return current, fmt.Errorf("%w: committed revision predates expectation", ErrInvalidCommandState)
		}
		next.Status = CommandCommitted
		next.SafeExplanation = settlement.SafeExplanation
	case SettlementStale:
		if settlement.AuthoritativeRevision <= current.ExpectedRevision ||
			strings.TrimSpace(settlement.ChangedEntity) == "" {
			return current, fmt.Errorf("%w: stale settlement lacks newer entity revision", ErrInvalidCommandState)
		}
		next.Status = CommandStale
		next.ChangedEntity = settlement.ChangedEntity
		next.SafeExplanation = settlement.SafeExplanation
		next.RefreshRequested = true
	case SettlementDenied:
		next.Status = CommandDenied
		next.SafeExplanation = settlement.SafeExplanation
	case SettlementDisconnected:
		next.Status = CommandDisconnected
		next.SafeExplanation = settlement.SafeExplanation
		next.SettlementUncertain = true
	case SettlementFailed:
		next.Status = CommandFailed
		next.SafeExplanation = settlement.SafeExplanation
	default:
		return current, ErrInvalidCommandState
	}
	return next, nil
}

func AbandonCommand(current CommandState, key CommandKey) (CommandState, error) {
	if !current.OwnsKey() || current.Key != key {
		return current, ErrCommandKeyMismatch
	}
	return CommandState{Status: CommandIdle}, nil
}
