package state

import "time"

// AnnouncementKind is restricted to meaningful state changes. Routine stream
// traffic and token deltas have no kind and therefore cannot be announced.
type AnnouncementKind string

const (
	AnnouncementConnection        AnnouncementKind = "connection"
	AnnouncementApproval          AnnouncementKind = "approval"
	AnnouncementPause             AnnouncementKind = "pause"
	AnnouncementCompletion        AnnouncementKind = "completion"
	AnnouncementFailure           AnnouncementKind = "failure"
	AnnouncementValidationFailure AnnouncementKind = "validation-failure"
	AnnouncementRecovery          AnnouncementKind = "recovery"
)

// Announcement is always polite; assertive announcements are intentionally not
// represented in the frontend contract.
type Announcement struct {
	Kind    AnnouncementKind
	Message string
	At      time.Time
}

type AnnouncerState struct {
	MinimumInterval time.Duration
	Last            Announcement
}

// Accept rate-limits repeated announcements while allowing a different
// high-value state change through immediately.
func (s AnnouncerState) Accept(next Announcement) (AnnouncerState, bool) {
	if !validAnnouncementKind(next.Kind) || next.Message == "" {
		return s, false
	}
	interval := s.MinimumInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if next.Kind == s.Last.Kind && next.Message == s.Last.Message && next.At.Sub(s.Last.At) < interval {
		return s, false
	}
	s.MinimumInterval = interval
	s.Last = next
	return s, true
}

func validAnnouncementKind(kind AnnouncementKind) bool {
	switch kind {
	case AnnouncementConnection, AnnouncementApproval, AnnouncementPause,
		AnnouncementCompletion, AnnouncementFailure, AnnouncementValidationFailure,
		AnnouncementRecovery:
		return true
	default:
		return false
	}
}
