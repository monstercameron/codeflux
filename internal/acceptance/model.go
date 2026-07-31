// Package acceptance defines exact, immutable review, acceptance, repair,
// rejection, and rollback contracts over a final evidence report.
package acceptance

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	MaximumChecks       = 1024
	MaximumTargets      = 256
	MaximumTextBytes    = 8192
	MaximumIdentityText = 255
)

var (
	ErrInvalidReview         = errors.New("invalid acceptance review")
	ErrStaleReview           = errors.New("acceptance review is stale")
	ErrValidationRunning     = errors.New("required validation is still running")
	ErrAcknowledgementNeeded = errors.New("required validation acknowledgement is missing")
	ErrRequiredCheckBlocked  = errors.New("required validation disposition blocks acceptance")
)

type ReviewID string
type AcceptanceID string
type RepairRequestID string
type RejectionID string
type RollbackIntentID string

func (id ReviewID) String() string         { return string(id) }
func (id AcceptanceID) String() string     { return string(id) }
func (id RepairRequestID) String() string  { return string(id) }
func (id RejectionID) String() string      { return string(id) }
func (id RollbackIntentID) String() string { return string(id) }

func ParseReviewID(value string) (ReviewID, error) {
	if !validDigest(value) {
		return "", invalid("review_id", "must be a lowercase SHA-256 identity")
	}
	return ReviewID(value), nil
}
func ParseAcceptanceID(value string) (AcceptanceID, error) {
	if !validDigest(value) {
		return "", invalid("acceptance_id", "must be a lowercase SHA-256 identity")
	}
	return AcceptanceID(value), nil
}
func ParseRepairRequestID(value string) (RepairRequestID, error) {
	if !validDigest(value) {
		return "", invalid("repair_request_id", "must be a lowercase SHA-256 identity")
	}
	return RepairRequestID(value), nil
}
func ParseRejectionID(value string) (RejectionID, error) {
	if !validDigest(value) {
		return "", invalid("rejection_id", "must be a lowercase SHA-256 identity")
	}
	return RejectionID(value), nil
}
func ParseRollbackIntentID(value string) (RollbackIntentID, error) {
	if !validDigest(value) {
		return "", invalid("rollback_intent_id", "must be a lowercase SHA-256 identity")
	}
	return RollbackIntentID(value), nil
}

type CheckStatus string

const (
	CheckPending     CheckStatus = "pending"
	CheckRunning     CheckStatus = "running"
	CheckPassed      CheckStatus = "passed"
	CheckFailed      CheckStatus = "failed"
	CheckWaived      CheckStatus = "waived"
	CheckSkipped     CheckStatus = "skipped"
	CheckUnavailable CheckStatus = "unavailable"
	CheckCancelled   CheckStatus = "cancelled"
	CheckInvalidated CheckStatus = "invalidated"
)

func (status CheckStatus) IsValid() bool {
	switch status {
	case CheckPending, CheckRunning, CheckPassed, CheckFailed, CheckWaived,
		CheckSkipped, CheckUnavailable, CheckCancelled, CheckInvalidated:
		return true
	default:
		return false
	}
}

type RequiredCheck struct {
	ID     string
	Status CheckStatus
}

type Review struct {
	ID             ReviewID
	TaskID         domain.TaskID
	Revision       uint64
	ReportID       string
	DiffIdentity   string
	PlanRevision   uint64
	RequiredChecks []RequiredCheck
	OpenedBy       string
	IdempotencyKey string
	OpenedAt       time.Time
}

func (review Review) Validate() error {
	switch {
	case !validDigest(review.ID.String()):
		return invalid("review_id", "must be a lowercase SHA-256 identity")
	case review.TaskID.IsZero():
		return invalid("task_id", "must not be empty")
	case review.Revision == 0:
		return invalid("revision", "must be greater than zero")
	case !validDigest(review.ReportID), !validDigest(review.DiffIdentity):
		return invalid("report_binding", "must contain canonical report and diff identities")
	case review.PlanRevision == 0:
		return invalid("plan_revision", "must be greater than zero")
	case !validUTC(review.OpenedAt):
		return invalid("opened_at", "must be a non-zero UTC timestamp")
	}
	if err := bounded("opened_by", review.OpenedBy, MaximumIdentityText); err != nil {
		return err
	}
	if err := bounded("idempotency_key", review.IdempotencyKey, MaximumIdentityText); err != nil {
		return err
	}
	if len(review.RequiredChecks) > MaximumChecks {
		return invalid("required_checks", "exceeds the bounded check limit")
	}
	seen := map[string]struct{}{}
	for _, check := range review.RequiredChecks {
		if err := bounded("required_check.id", check.ID, MaximumIdentityText); err != nil || !check.Status.IsValid() {
			return invalid("required_checks", "contains an invalid check")
		}
		if _, duplicate := seen[check.ID]; duplicate {
			return invalid("required_checks", "contains a duplicate check")
		}
		seen[check.ID] = struct{}{}
	}
	return nil
}

func (review Review) Clone() Review {
	review.RequiredChecks = slices.Clone(review.RequiredChecks)
	return review
}

type Gate struct {
	CanAccept               bool
	Stale                   bool
	RunningCheckIDs         []string
	AcknowledgementCheckIDs []string
	BlockingCheckIDs        []string
	Reason                  string
}

// EvaluateAcceptance computes the user-visible and storage-enforced gate.
// Acknowledgements apply only to required failed or waived checks.
func EvaluateAcceptance(review Review, currentReportID, currentDiffIdentity string, acknowledgements []string) (Gate, error) {
	return evaluateAcceptance(review, currentReportID, currentDiffIdentity, review.RequiredChecks, acknowledgements)
}

// EvaluateAcceptanceWithLiveChecks overlays authoritative validation-controller
// state for the exact required check set captured by the review. This permits
// a renewed validation to block acceptance while it is pending or running,
// even though the sealed evidence report contains terminal results.
func EvaluateAcceptanceWithLiveChecks(
	review Review,
	currentReportID string,
	currentDiffIdentity string,
	liveChecks []RequiredCheck,
	acknowledgements []string,
) (Gate, error) {
	if err := exactRequiredCheckSet(review.RequiredChecks, liveChecks); err != nil {
		return Gate{}, err
	}
	return evaluateAcceptance(review, currentReportID, currentDiffIdentity, liveChecks, acknowledgements)
}

func evaluateAcceptance(review Review, currentReportID, currentDiffIdentity string, checks []RequiredCheck, acknowledgements []string) (Gate, error) {
	if err := review.Validate(); err != nil {
		return Gate{}, err
	}
	if !validDigest(currentReportID) || !validDigest(currentDiffIdentity) {
		return Gate{}, invalid("current_binding", "must contain canonical report and diff identities")
	}
	gate := Gate{}
	if currentReportID != review.ReportID || currentDiffIdentity != review.DiffIdentity {
		gate.Stale = true
		gate.Reason = "The report or diff changed after this review opened. Open the renewed review before deciding."
		return gate, nil
	}
	acknowledged := map[string]struct{}{}
	for _, id := range acknowledgements {
		if err := bounded("acknowledgement", id, MaximumIdentityText); err != nil {
			return Gate{}, err
		}
		if _, duplicate := acknowledged[id]; duplicate {
			return Gate{}, invalid("acknowledgements", "contains a duplicate check")
		}
		acknowledged[id] = struct{}{}
	}
	acknowledgeable := map[string]struct{}{}
	for _, check := range checks {
		switch check.Status {
		case CheckPending, CheckRunning:
			gate.RunningCheckIDs = append(gate.RunningCheckIDs, check.ID)
		case CheckFailed, CheckWaived:
			acknowledgeable[check.ID] = struct{}{}
			if _, ok := acknowledged[check.ID]; !ok {
				gate.AcknowledgementCheckIDs = append(gate.AcknowledgementCheckIDs, check.ID)
			}
		case CheckSkipped, CheckUnavailable, CheckCancelled, CheckInvalidated:
			gate.BlockingCheckIDs = append(gate.BlockingCheckIDs, check.ID)
		}
	}
	for id := range acknowledged {
		if _, ok := acknowledgeable[id]; !ok {
			return Gate{}, invalid("acknowledgements", "may only name required failed or waived checks")
		}
	}
	switch {
	case len(gate.RunningCheckIDs) > 0:
		gate.Reason = "Required validations are still running."
	case len(gate.AcknowledgementCheckIDs) > 0:
		gate.Reason = "Explicit acknowledgement is required for each failed or waived required check."
	case len(gate.BlockingCheckIDs) > 0:
		gate.Reason = "A required skipped, unavailable, cancelled, or invalidated check blocks acceptance."
	default:
		gate.CanAccept = true
	}
	return gate, nil
}

func exactRequiredCheckSet(reviewed, live []RequiredCheck) error {
	if len(reviewed) != len(live) {
		return invalid("live_required_checks", "must contain the exact reviewed required-check set")
	}
	ids := make(map[string]struct{}, len(reviewed))
	for _, check := range reviewed {
		ids[check.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(live))
	for _, check := range live {
		if !check.Status.IsValid() {
			return invalid("live_required_checks", "contains an invalid status")
		}
		if _, ok := ids[check.ID]; !ok {
			return invalid("live_required_checks", "must contain the exact reviewed required-check set")
		}
		if _, duplicate := seen[check.ID]; duplicate {
			return invalid("live_required_checks", "contains a duplicate check")
		}
		seen[check.ID] = struct{}{}
	}
	return nil
}

func AcceptanceGateError(gate Gate) error {
	switch {
	case gate.Stale:
		return ErrStaleReview
	case len(gate.RunningCheckIDs) > 0:
		return ErrValidationRunning
	case len(gate.AcknowledgementCheckIDs) > 0:
		return ErrAcknowledgementNeeded
	case len(gate.BlockingCheckIDs) > 0:
		return ErrRequiredCheckBlocked
	default:
		return nil
	}
}

type RepairTargetKind string

const (
	RepairTargetValidation RepairTargetKind = "validation"
	RepairTargetHunk       RepairTargetKind = "hunk"
)

type RepairTarget struct {
	Kind RepairTargetKind
	ID   string
}

func (target RepairTarget) Validate() error {
	if target.Kind != RepairTargetValidation && target.Kind != RepairTargetHunk {
		return invalid("repair_target.kind", "must be validation or hunk")
	}
	return bounded("repair_target.id", target.ID, MaximumIdentityText)
}

type RepairRequest struct {
	ID                    RepairRequestID
	ReviewID              ReviewID
	TaskID                domain.TaskID
	ReviewRevision        uint64
	ReportID              string
	DiffIdentity          string
	PreviousPlanRevision  uint64
	NewPlanRevision       uint64
	PreRepairCheckpointID domain.CheckpointID
	Feedback              string
	Targets               []RepairTarget
	RequestedBy           string
	IdempotencyKey        string
	CreatedAt             time.Time
}

func (request RepairRequest) Validate() error {
	switch {
	case !validDigest(request.ID.String()), !validDigest(request.ReviewID.String()):
		return invalid("repair.identity", "must contain canonical identities")
	case request.TaskID.IsZero() || request.PreRepairCheckpointID.IsZero():
		return invalid("repair.scope", "must bind a task and checkpoint")
	case request.ReviewRevision == 0 || request.PreviousPlanRevision == 0 || request.NewPlanRevision <= request.PreviousPlanRevision:
		return invalid("repair.lineage", "must advance the reviewed plan revision")
	case !validDigest(request.ReportID) || !validDigest(request.DiffIdentity):
		return invalid("repair.binding", "must bind canonical report and diff identities")
	case len(request.Targets) == 0 || len(request.Targets) > MaximumTargets:
		return invalid("repair.targets", "must contain a bounded non-empty selection")
	case !validUTC(request.CreatedAt):
		return invalid("created_at", "must be a non-zero UTC timestamp")
	}
	if err := bounded("repair.feedback", request.Feedback, MaximumTextBytes); err != nil {
		return err
	}
	if err := bounded("repair.requested_by", request.RequestedBy, MaximumIdentityText); err != nil {
		return err
	}
	if err := bounded("repair.idempotency_key", request.IdempotencyKey, MaximumIdentityText); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, target := range request.Targets {
		if err := target.Validate(); err != nil {
			return err
		}
		key := string(target.Kind) + "\x00" + target.ID
		if _, duplicate := seen[key]; duplicate {
			return invalid("repair.targets", "contains a duplicate target")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (request RepairRequest) Clone() RepairRequest {
	request.Targets = slices.Clone(request.Targets)
	return request
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validUTC(value time.Time) bool { return !value.IsZero() && value.Location() == time.UTC }

func bounded(field, value string, maximum int) error {
	if value == "" || value != strings.TrimSpace(value) || len(value) > maximum || !utf8.ValidString(value) {
		return invalid(field, "must be non-empty, trimmed, valid UTF-8, and bounded")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return invalid(field, "must not contain control characters")
		}
	}
	return nil
}

func invalid(field, reason string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidReview, field, reason)
}
