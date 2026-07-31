package timelinecard

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type ApprovalResolution struct {
	State      string
	ResolvedBy string
	ResolvedAt time.Time
}

// InterruptMessage makes incomplete provider output explicit without changing
// the safe text already received.
func InterruptMessage(current Message) Message {
	if current.Status == MessageProvisional {
		current.Status = MessageInterrupted
	}
	return current
}

// ResolveApproval is idempotent for duplicate committed delivery and rejects a
// conflicting second decision.
func ResolveApproval(current Approval, resolution ApprovalResolution) (Approval, bool, error) {
	if current.ID == "" || strings.TrimSpace(resolution.ResolvedBy) == "" || resolution.ResolvedAt.IsZero() {
		return current, false, fmt.Errorf("approval resolution is incomplete")
	}
	switch resolution.State {
	case "granted", "denied", "expired", "cancelled":
	default:
		return current, false, fmt.Errorf("approval resolution state %q is invalid", resolution.State)
	}
	if !current.ActionsAvailable() {
		if current.State == resolution.State && current.ResolvedBy == resolution.ResolvedBy && current.ResolvedAt.Equal(resolution.ResolvedAt) {
			return current, false, nil
		}
		return current, false, fmt.Errorf("approval %s already resolved", current.ID)
	}
	next := current
	next.State = resolution.State
	next.ResolvedBy = resolution.ResolvedBy
	next.ResolvedAt = resolution.ResolvedAt
	return next, true, nil
}

// ApplyPlanRevision preserves immutable history, supersedes the previous plan,
// and resets approval on the new revision.
func ApplyPlanRevision(history []Plan, next Plan) ([]Plan, error) {
	if next.Revision == 0 || strings.TrimSpace(next.Summary) == "" {
		return slices.Clone(history), fmt.Errorf("plan revision is incomplete")
	}
	result := slices.Clone(history)
	prior := make([]uint64, 0, len(result))
	for index := range result {
		if result[index].Revision >= next.Revision {
			return slices.Clone(history), fmt.Errorf("plan revision must increase")
		}
		result[index].Superseded = true
		result[index].ApprovalPending = false
		prior = append(prior, result[index].Revision)
	}
	next.Superseded = false
	next.ApprovalPending = true
	next.PriorRevisions = prior
	return append(result, next), nil
}

type LatencyPresentation struct {
	Phase    string
	ShowStop bool
	Waiting  bool
}

// FirstMessageLatency replaces an indefinite spinner with phase and Stop once
// the deterministic threshold is reached.
func FirstMessageLatency(startedAt, now time.Time, threshold time.Duration, phase string) LatencyPresentation {
	if startedAt.IsZero() || now.Before(startedAt) || threshold <= 0 {
		return LatencyPresentation{}
	}
	exceeded := now.Sub(startedAt) >= threshold
	return LatencyPresentation{Phase: strings.TrimSpace(phase), ShowStop: exceeded, Waiting: !exceeded}
}
