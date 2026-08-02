package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
)

// ObligationKind names what a proof obligation is about (PIPE-016, §9).
type ObligationKind string

const (
	// ObligationComposition is what joining parts must guarantee.
	ObligationComposition ObligationKind = "composition"
	// ObligationControlPath is what one declared path must do.
	ObligationControlPath ObligationKind = "control-path"
	// ObligationEffect is what a unit may reach for.
	ObligationEffect ObligationKind = "effect"
	// ObligationReviewFinding is a critic's finding raised as a claim.
	ObligationReviewFinding ObligationKind = "review-finding"
)

// ObligationState is where an obligation stands.
type ObligationState string

const (
	ObligationOpen       ObligationState = "open"
	ObligationDischarged ObligationState = "discharged"
	ObligationRegressed  ObligationState = "regressed"
)

// ProofObligation is one durable claim and its standing.
type ProofObligation struct {
	ID                string
	TaskID            domain.TaskID
	Attempt           uint64
	Kind              ObligationKind
	Subject           string
	StatementRedacted string
	State             ObligationState
	DischargedByStage *pipeline.Number
	DischargedDetail  string
	RaisedAt          time.Time
}

// RaiseProofObligation declares one claim that must later be established.
type RaiseProofObligation struct {
	TaskID            domain.TaskID
	Attempt           uint64
	Kind              ObligationKind
	Subject           string
	StatementRedacted string
}

// RaiseProofObligation records an obligation, or leaves an existing one alone.
//
// Raising the same claim twice in one attempt is not two claims: the identity
// is what the obligation is about, so a stage re-stating it must not reset a
// discharge that already happened.
func (repositories *Repositories) RaiseProofObligation(
	ctx context.Context,
	input RaiseProofObligation,
) (ProofObligation, error) {
	if err := input.validate(); err != nil {
		return ProofObligation{}, err
	}
	moment, micros := repositories.timestamp()
	identity := digestOfObligation(
		input.TaskID.String(), input.Attempt, input.Kind, input.Subject)

	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO proof_obligations
		   (id, task_id, attempt, kind, subject, statement_redacted, state,
		    raised_at_unix_micros)
		 VALUES (?, ?, ?, ?, ?, ?, 'open', ?)
		 ON CONFLICT (task_id, attempt, kind, subject) DO NOTHING`,
		identity, input.TaskID, input.Attempt, string(input.Kind),
		input.Subject, input.StatementRedacted, micros,
	); err != nil {
		return ProofObligation{}, classify("raise proof obligation", err)
	}
	return ProofObligation{
		ID: identity, TaskID: input.TaskID, Attempt: input.Attempt,
		Kind: input.Kind, Subject: input.Subject,
		StatementRedacted: input.StatementRedacted,
		State:             ObligationOpen, RaisedAt: moment,
	}, nil
}

func (input RaiseProofObligation) validate() error {
	switch {
	case input.TaskID.IsZero():
		return errors.New("task ID must not be empty")
	case input.Attempt == 0:
		return errors.New("attempt must be at least one")
	case strings.TrimSpace(input.Subject) == "":
		return errors.New("an obligation must name what it is about")
	case strings.TrimSpace(input.StatementRedacted) == "":
		return errors.New("an obligation must state what has to hold")
	}
	switch input.Kind {
	case ObligationComposition, ObligationControlPath,
		ObligationEffect, ObligationReviewFinding:
		return nil
	default:
		return errors.New("unknown proof obligation kind")
	}
}

// DischargeProofObligation records that a stage established a claim.
//
// The discharging stage is required. Recording a discharge without naming what
// performed it would reproduce the defect this table repairs: a claim with
// nothing behind it.
func (repositories *Repositories) DischargeProofObligation(
	ctx context.Context,
	taskID domain.TaskID,
	attempt uint64,
	kind ObligationKind,
	subject string,
	stage pipeline.Number,
	detail string,
) error {
	if taskID.IsZero() || attempt == 0 || strings.TrimSpace(subject) == "" {
		return errors.New("task, attempt, and subject are required to discharge")
	}
	_, micros := repositories.timestamp()
	result, err := repositories.database.sql.ExecContext(
		ctx,
		`UPDATE proof_obligations
		    SET state = 'discharged', discharged_by_stage = ?,
		        discharged_detail_redacted = ?, settled_at_unix_micros = ?
		  WHERE task_id = ? AND attempt = ? AND kind = ? AND subject = ?`,
		int(stage), boundedDetail(detail), micros,
		taskID, attempt, string(kind), subject,
	)
	if err != nil {
		return classify("discharge proof obligation", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return classify("confirm obligation discharge", err)
	}
	if affected == 0 {
		return typedError(ErrNotFound, "discharge proof obligation",
			errors.New("no obligation with that identity"))
	}
	return nil
}

// ListProofObligations returns one attempt's obligations in raise order.
func (repositories *Repositories) ListProofObligations(
	ctx context.Context,
	taskID domain.TaskID,
	attempt uint64,
) ([]ProofObligation, error) {
	if taskID.IsZero() {
		return nil, errors.New("task ID must not be empty")
	}
	if attempt == 0 {
		attempt = 1
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT id, kind, subject, statement_redacted, state,
		        discharged_by_stage, raised_at_unix_micros
		   FROM proof_obligations
		  WHERE task_id = ? AND attempt = ?
		  ORDER BY raised_at_unix_micros, subject`,
		taskID, attempt,
	)
	if err != nil {
		return nil, classify("list proof obligations", err)
	}
	defer func() { _ = rows.Close() }()

	var obligations []ProofObligation
	for rows.Next() {
		var (
			obligation ProofObligation
			kind       string
			state      string
			stage      *int64
			micros     int64
		)
		if err := rows.Scan(&obligation.ID, &kind, &obligation.Subject,
			&obligation.StatementRedacted, &state, &stage, &micros); err != nil {
			return nil, classify("scan proof obligation", err)
		}
		obligation.TaskID = taskID
		obligation.Attempt = attempt
		obligation.Kind = ObligationKind(kind)
		obligation.State = ObligationState(state)
		obligation.RaisedAt = time.UnixMicro(micros).UTC()
		if stage != nil {
			number := pipeline.Number(*stage)
			obligation.DischargedByStage = &number
		}
		obligations = append(obligations, obligation)
	}
	return obligations, rows.Err()
}

// boundedDetail keeps a discharge note inside the column's bound.
func boundedDetail(detail string) string {
	const limit = 2048
	if len(detail) > limit {
		return detail[:limit]
	}
	return detail
}

// digestOfObligation names an obligation from what it is about.
//
// The identity is derived rather than minted, matching digestOfStage: raising
// the same claim twice collides on its own key instead of producing a second
// answer to one question. The subject is hashed because a function name can be
// longer than the column allows and can contain anything Go permits.
func digestOfObligation(
	taskID string,
	attempt uint64,
	kind ObligationKind,
	subject string,
) string {
	sum := sha256.Sum256([]byte(taskID + "|" + string(kind) + "|" + subject))
	return fmt.Sprintf("obl:%s:%d:%s", taskID, attempt,
		hex.EncodeToString(sum[:8]))
}
