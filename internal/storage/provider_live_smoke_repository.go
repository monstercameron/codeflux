package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

type LiveProviderSmokePricing struct {
	Currency       domain.CurrencyCode
	SourceRedacted string
	EffectiveAt    time.Time
	Components     []ProviderPriceComponent
}

type PrepareLiveProviderSmokeRequest struct {
	IdempotencyKey            string
	RepositoryPath            string
	RepositoryGitIdentity     string
	ProviderType              string
	ProviderDisplayName       string
	AdapterName               string
	AdapterVersion            string
	ProviderVersion           string
	EndpointRedacted          string
	CapabilitiesJSON          string
	OpaqueCredentialReference string
	ModelIdentifier           string
	ModelVersion              string
	RequestSHA256             string
	Pricing                   *LiveProviderSmokePricing
}

type LiveProviderSmokeRequest struct {
	ProjectID     domain.ProjectID
	RepositoryID  domain.RepositoryID
	ThreadID      domain.ThreadID
	TaskID        domain.TaskID
	RunID         domain.RunID
	ProviderID    domain.ProviderID
	Configuration ProviderConfigurationRevision
	Pricing       ProviderPricingRevision
	Request       ProviderLogicalRequest
}

type ProviderAttemptAttribution struct {
	Attempt    ProviderRequestAttempt
	Latency    time.Duration
	Accounting *ProviderAttemptAccounting
}

type ProviderRequestAttribution struct {
	Request    ProviderLogicalRequest
	Pricing    *ProviderPricingRevision
	Attempts   []ProviderAttemptAttribution
	Accounting ProviderRequestAccountingSummary
}

type FinalizeLiveProviderSmokeRequest struct {
	RequestID        domain.ModelRequestID
	ExpectedRevision uint64
	To               ProviderLogicalRequestState
	AccountingStatus ProviderAccountingStatus
}

type AbortLiveProviderSmokeRequestBeforeIO struct {
	RequestID        domain.ModelRequestID
	ExpectedRevision uint64
}

type LiveProviderSmokeOperations interface {
	PrepareLiveProviderSmokeRequest(
		context.Context,
		PrepareLiveProviderSmokeRequest,
	) (LiveProviderSmokeRequest, error)
	FinalizeLiveProviderSmokeRequest(
		context.Context,
		FinalizeLiveProviderSmokeRequest,
	) (ProviderRequestAttribution, error)
	AbortLiveProviderSmokeRequestBeforeIO(
		context.Context,
		AbortLiveProviderSmokeRequestBeforeIO,
	) (ProviderRequestAttribution, error)
	GetProviderRequestAttribution(
		context.Context,
		domain.ModelRequestID,
	) (ProviderRequestAttribution, error)
}

var _ LiveProviderSmokeOperations = (*Repositories)(nil)

func (repositories *Repositories) PrepareLiveProviderSmokeRequest(
	ctx context.Context,
	input PrepareLiveProviderSmokeRequest,
) (LiveProviderSmokeRequest, error) {
	if err := validateLiveProviderSmokeInput(input); err != nil {
		return LiveProviderSmokeRequest{}, err
	}
	idempotencyHash := sha256Hex(input.IdempotencyKey)
	inputHash := liveProviderSmokeInputHash(input)
	ids, err := newLiveProviderSmokeIDs()
	if err != nil {
		return LiveProviderSmokeRequest{}, err
	}
	var fixture liveProviderSmokeFixture
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findLiveProviderSmokeFixture(
			ctx, transaction.sql, idempotencyHash,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.InputSHA256 != inputHash {
				return typedError(
					ErrConflict,
					"prepare live provider smoke request",
					errors.New("idempotency key was reused with different live-provider inputs"),
				)
			}
			fixture = existing
			return nil
		}
		now, micros := repositories.timestamp()
		projectID, repositoryID, err := ensureLiveSmokeRepository(
			ctx, transaction, ids.ProjectID, ids.RepositoryID, input, micros,
		)
		if err != nil {
			return err
		}
		providerID, err := ensureLiveSmokeProvider(
			ctx, transaction, ids.ProviderID, input, micros,
		)
		if err != nil {
			return err
		}
		configuration, err := ensureLiveSmokeConfiguration(
			ctx, transaction, providerID, ids.ConfigurationID,
			idempotencyHash, input, now, micros,
		)
		if err != nil {
			return err
		}
		pricing, err := insertLiveSmokePricing(
			ctx, transaction, providerID, ids.PricingID, input, now, micros,
		)
		if err != nil {
			return err
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO threads (
				id, project_id, repository_id, title,
				created_at_unix_micros, updated_at_unix_micros
			) VALUES (?, ?, ?, 'Live provider diagnostics', ?, ?)`,
			ids.ThreadID, projectID, repositoryID, micros, micros,
		); err != nil {
			return repositoryWriteError("create live smoke thread", err)
		}
		taskKey := "live-smoke-" + idempotencyHash[:32]
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO tasks (
				id, thread_id, repository_id, state, policy_preset,
				reasoning_effort, risk_level, required_assurance,
				idempotency_key, created_at_unix_micros,
				updated_at_unix_micros
			) VALUES (
				?, ?, ?, 'running', 'correctness', 'standard', 'routine',
				'runtime-only', ?, ?, ?
			)`,
			ids.TaskID, ids.ThreadID, repositoryID, taskKey, micros, micros,
		); err != nil {
			return repositoryWriteError("create live smoke task", err)
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO task_settings_bindings (
				task_id, settings_revision, bound_at_unix_micros
			) VALUES (?, 0, ?)`,
			ids.TaskID, micros,
		); err != nil {
			return repositoryWriteError("bind live smoke task settings", err)
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO runs (
				id, task_id, state, attempt, task_revision, idempotency_key,
				created_at_unix_micros, updated_at_unix_micros
			) VALUES (?, ?, 'running', 1, 0, ?, ?, ?)`,
			ids.RunID, ids.TaskID, taskKey, micros, micros,
		); err != nil {
			return repositoryWriteError("create live smoke run", err)
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO task_events (
				id, task_id, run_id, sequence, event_type, payload_json,
				idempotency_key, created_at_unix_micros
			) VALUES (
				?, ?, ?, 1, 'provider.live-smoke.started',
				'{"diagnostic":"live-provider-smoke"}', ?, ?
			)`,
			ids.EventID, ids.TaskID, ids.RunID, taskKey, micros,
		); err != nil {
			return repositoryWriteError("record live smoke start", err)
		}
		requestKey := "live-smoke-request-" + idempotencyHash[:32]
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO provider_logical_requests (
				id, task_id, run_id, provider_id,
				provider_configuration_revision_id, adapter_name,
				adapter_version, provider_version, model_identifier,
				model_version, pricing_revision_id, state, request_sha256,
				idempotency_key, accounting_status,
				started_at_unix_micros, created_at_unix_micros,
				updated_at_unix_micros
			) VALUES (
				?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'in-flight', ?, ?,
				'unknown', ?, ?, ?
			)`,
			ids.RequestID, ids.TaskID, ids.RunID, providerID,
			configuration.ID, configuration.AdapterName,
			configuration.AdapterVersion, configuration.ProviderVersion,
			input.ModelIdentifier, input.ModelVersion, pricing.ID,
			input.RequestSHA256, requestKey, micros, micros, micros,
		); err != nil {
			return repositoryWriteError("create live smoke logical request", err)
		}
		fixture = liveProviderSmokeFixture{
			IdempotencySHA256: idempotencyHash, InputSHA256: inputHash,
			ProjectID: projectID, RepositoryID: repositoryID,
			ThreadID: ids.ThreadID, TaskID: ids.TaskID, RunID: ids.RunID,
			ProviderID: providerID, ConfigurationRevisionID: configuration.ID,
			PricingRevisionID: pricing.ID, LogicalRequestID: ids.RequestID,
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO provider_live_smoke_fixtures (
				idempotency_sha256, input_sha256, project_id, repository_id,
				thread_id, task_id, run_id, provider_id,
				configuration_revision_id, pricing_revision_id,
				logical_request_id, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fixture.IdempotencySHA256, fixture.InputSHA256,
			fixture.ProjectID, fixture.RepositoryID, fixture.ThreadID,
			fixture.TaskID, fixture.RunID, fixture.ProviderID,
			fixture.ConfigurationRevisionID, fixture.PricingRevisionID,
			fixture.LogicalRequestID, micros,
		); err != nil {
			return repositoryWriteError("record live smoke fixture", err)
		}
		return nil
	})
	if err != nil {
		return LiveProviderSmokeRequest{}, err
	}
	return repositories.loadLiveProviderSmokeRequest(ctx, fixture)
}

func (repositories *Repositories) GetProviderRequestAttribution(
	ctx context.Context,
	requestID domain.ModelRequestID,
) (ProviderRequestAttribution, error) {
	request, err := repositories.GetProviderLogicalRequest(ctx, requestID)
	if err != nil {
		return ProviderRequestAttribution{}, err
	}
	attribution := ProviderRequestAttribution{Request: request}
	if request.PricingRevisionID != nil {
		pricing, found, err := findProviderPricingRevision(
			ctx, repositories.database.sql, *request.PricingRevisionID,
		)
		if err != nil {
			return ProviderRequestAttribution{}, err
		}
		if !found {
			return ProviderRequestAttribution{}, typedError(
				ErrNotFound, "get provider request attribution", sql.ErrNoRows,
			)
		}
		attribution.Pricing = &pricing
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT attempt.id, (
		     SELECT accounting.id
		     FROM provider_attempt_accounting AS accounting
		     WHERE accounting.attempt_id = attempt.id
		     ORDER BY accounting.sequence DESC
		     LIMIT 1
		 )
		 FROM provider_request_attempts AS attempt
		 WHERE attempt.logical_request_id = ?
		 ORDER BY attempt.attempt_number
		 LIMIT ?`,
		requestID, maximumProviderRequestAttempts+1,
	)
	if err != nil {
		return ProviderRequestAttribution{}, classify("list provider request attribution", err)
	}
	type attemptRow struct {
		id           string
		accountingID sql.NullString
	}
	var attemptRows []attemptRow
	for rows.Next() {
		var row attemptRow
		if err := rows.Scan(&row.id, &row.accountingID); err != nil {
			rows.Close()
			return ProviderRequestAttribution{}, classify("scan provider request attribution", err)
		}
		attemptRows = append(attemptRows, row)
	}
	if err := rows.Close(); err != nil {
		return ProviderRequestAttribution{}, classify("close provider request attribution", err)
	}
	if len(attemptRows) > maximumProviderRequestAttempts {
		return ProviderRequestAttribution{}, errors.New("provider request attempt attribution exceeds bound")
	}
	for _, row := range attemptRows {
		attempt, err := getProviderRequestAttempt(
			ctx, repositories.database.sql, row.id,
		)
		if err != nil {
			return ProviderRequestAttribution{}, err
		}
		item := ProviderAttemptAttribution{Attempt: attempt}
		if attempt.StartedAt != nil && attempt.CompletedAt != nil {
			item.Latency = attempt.CompletedAt.Sub(*attempt.StartedAt)
		}
		if row.accountingID.Valid {
			accounting, found, err := findProviderAttemptAccountingByID(
				ctx, repositories.database.sql, row.accountingID.String,
			)
			if err != nil {
				return ProviderRequestAttribution{}, err
			}
			if !found {
				return ProviderRequestAttribution{}, typedError(
					ErrNotFound, "get provider attempt attribution", sql.ErrNoRows,
				)
			}
			item.Accounting = &accounting
		}
		attribution.Attempts = append(attribution.Attempts, item)
	}
	attribution.Accounting, err = repositories.SummarizeProviderRequestAccounting(
		ctx, requestID,
	)
	if err != nil {
		return ProviderRequestAttribution{}, err
	}
	return attribution, nil
}

func (repositories *Repositories) FinalizeLiveProviderSmokeRequest(
	ctx context.Context,
	input FinalizeLiveProviderSmokeRequest,
) (ProviderRequestAttribution, error) {
	return repositories.finalizeLiveProviderSmokeRequest(ctx, input, false)
}

func (repositories *Repositories) AbortLiveProviderSmokeRequestBeforeIO(
	ctx context.Context,
	input AbortLiveProviderSmokeRequestBeforeIO,
) (ProviderRequestAttribution, error) {
	if input.RequestID.IsZero() {
		return ProviderRequestAttribution{}, errors.New("live smoke request ID is required")
	}
	return repositories.finalizeLiveProviderSmokeRequest(
		ctx,
		FinalizeLiveProviderSmokeRequest{
			RequestID: input.RequestID, ExpectedRevision: input.ExpectedRevision,
			To:               ProviderLogicalRequestFailed,
			AccountingStatus: ProviderAccountingUnknown,
		},
		true,
	)
}

func (repositories *Repositories) finalizeLiveProviderSmokeRequest(
	ctx context.Context,
	input FinalizeLiveProviderSmokeRequest,
	beforeExternalIO bool,
) (ProviderRequestAttribution, error) {
	if input.RequestID.IsZero() ||
		!slices.Contains(
			[]ProviderLogicalRequestState{
				ProviderLogicalRequestSucceeded,
				ProviderLogicalRequestFailed,
				ProviderLogicalRequestCancelled,
				ProviderLogicalRequestOutcomeUnknown,
				ProviderLogicalRequestRetryExhausted,
			},
			input.To,
		) ||
		!providerAccountingStatusValid(input.AccountingStatus) {
		return ProviderRequestAttribution{}, errors.New("live smoke final state is invalid")
	}
	eventID, err := domain.NewEventID()
	if err != nil {
		return ProviderRequestAttribution{}, err
	}
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		fixture, found, err := findLiveProviderSmokeFixtureByRequest(
			ctx, transaction.sql, input.RequestID,
		)
		if err != nil {
			return err
		}
		if !found {
			return typedError(
				ErrNotFound, "finalize live provider smoke request", sql.ErrNoRows,
			)
		}
		request, err := getProviderLogicalRequest(
			ctx, transaction.sql, input.RequestID,
		)
		if err != nil {
			return err
		}
		logicalNeedsTransition := request.State == ProviderLogicalRequestInFlight
		if logicalNeedsTransition && request.Revision != input.ExpectedRevision {
			return typedError(
				ErrStaleRevision, "finalize live provider smoke request",
				errors.New("provider logical request revision changed"),
			)
		}
		if !logicalNeedsTransition &&
			(request.State != input.To ||
				request.AccountingStatus != input.AccountingStatus ||
				request.Revision != input.ExpectedRevision &&
					request.Revision != input.ExpectedRevision+1) {
			return typedError(
				ErrConflict, "finalize live provider smoke request",
				errors.New("provider logical request terminal result changed"),
			)
		}
		var total, nonterminal, missingAccounting int
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT count(*),
			        coalesce(sum(CASE WHEN state IN (
			            'succeeded', 'failed', 'cancelled', 'outcome-unknown'
			        ) THEN 0 ELSE 1 END), 0),
			        coalesce(sum(CASE WHEN EXISTS (
			            SELECT 1 FROM provider_attempt_accounting AS accounting
			            WHERE accounting.attempt_id = attempt.id
			        ) THEN 0 ELSE 1 END), 0)
			 FROM provider_request_attempts AS attempt
			 WHERE logical_request_id = ?`,
			input.RequestID,
		).Scan(&total, &nonterminal, &missingAccounting); err != nil {
			return classify("verify live smoke attempt evidence", err)
		}
		if beforeExternalIO && total != 0 {
			return typedError(
				ErrConflict, "abort live provider smoke request before I/O",
				errors.New("a physical attempt was already durably prepared"),
			)
		}
		zeroAttemptTerminal := slices.Contains(
			[]ProviderLogicalRequestState{
				ProviderLogicalRequestFailed,
				ProviderLogicalRequestCancelled,
			},
			input.To,
		)
		if !beforeExternalIO &&
			(nonterminal != 0 || missingAccounting != 0 ||
				total == 0 && !zeroAttemptTerminal) {
			return typedError(
				ErrConflict, "finalize live provider smoke request",
				errors.New("every physical attempt must be terminal with usage evidence"),
			)
		}
		derivedState := request.State
		derivedAccounting := request.AccountingStatus
		if logicalNeedsTransition {
			derivedState = input.To
			derivedAccounting = ProviderAccountingUnknown
		}
		if logicalNeedsTransition && total != 0 {
			var lastState ProviderRequestAttemptState
			var lastRetryable int
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT state, retryable
				 FROM provider_request_attempts
				 WHERE logical_request_id = ?
				 ORDER BY attempt_number DESC
				 LIMIT 1`,
				input.RequestID,
			).Scan(&lastState, &lastRetryable); err != nil {
				return classify("derive live smoke final attempt", err)
			}
			var terminal bool
			derivedState, terminal = liveSmokeStateFromLastAttempt(
				request.State,
				lastState,
				lastRetryable != 0,
			)
			if !terminal {
				return typedError(
					ErrConflict,
					"finalize live provider smoke request",
					errors.New("last physical attempt is not terminal"),
				)
			}
			var usageKnown, discrepancy int
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT coalesce(min(usage_known), 0),
				        coalesce(max(discrepancy), 0)
				 FROM provider_attempt_accounting
				 WHERE attempt_id IN (
				     SELECT id FROM provider_request_attempts
				     WHERE logical_request_id = ?
				 )
				   AND sequence = (
				       SELECT max(latest.sequence)
				       FROM provider_attempt_accounting AS latest
				       WHERE latest.attempt_id =
				             provider_attempt_accounting.attempt_id
				   )`,
				input.RequestID,
			).Scan(&usageKnown, &discrepancy); err != nil {
				return classify("derive live smoke accounting status", err)
			}
			switch {
			case discrepancy != 0:
				derivedAccounting = ProviderAccountingDiscrepant
			case usageKnown != 0:
				derivedAccounting = ProviderAccountingProviderReported
			}
		}
		if input.To != derivedState ||
			input.AccountingStatus != derivedAccounting {
			return typedError(
				ErrConflict,
				"finalize live provider smoke request",
				errors.New(
					"caller final state disagrees with durable attempt evidence",
				),
			)
		}
		taskState, runState := liveSmokeTerminalStates(input.To)
		_, micros := repositories.timestamp()
		if logicalNeedsTransition {
			if _, err := transaction.sql.ExecContext(
				ctx,
				`UPDATE provider_logical_requests SET state = ?,
					accounting_status = ?, completed_at_unix_micros = ?,
					updated_at_unix_micros = ?, revision = revision + 1
				 WHERE id = ? AND revision = ? AND state = 'in-flight'`,
				input.To, input.AccountingStatus, micros, micros,
				input.RequestID, input.ExpectedRevision,
			); err != nil {
				return repositoryWriteError("finalize live smoke logical request", err)
			}
		}
		var currentTaskState, currentRunState string
		if err := transaction.sql.QueryRowContext(
			ctx, `SELECT state FROM tasks WHERE id = ?`, fixture.TaskID,
		).Scan(&currentTaskState); err != nil {
			return classify("read live smoke task state", err)
		}
		if err := transaction.sql.QueryRowContext(
			ctx, `SELECT state FROM runs WHERE id = ?`, fixture.RunID,
		).Scan(&currentRunState); err != nil {
			return classify("read live smoke run state", err)
		}
		var finalEventCount int
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT count(*) FROM task_events
			 WHERE task_id = ? AND idempotency_key = ?`,
			fixture.TaskID, "live-smoke-final-"+input.RequestID.String(),
		).Scan(&finalEventCount); err != nil {
			return classify("read live smoke final event", err)
		}
		if currentTaskState == taskState && currentRunState == runState &&
			finalEventCount == 1 {
			return nil
		}
		if finalEventCount != 0 {
			return typedError(
				ErrConflict, "finalize live provider smoke request",
				errors.New("live smoke final event disagrees with task or run state"),
			)
		}
		runResult, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE runs SET state = ?, updated_at_unix_micros = ?,
				revision = revision + 1
			 WHERE id = ? AND task_id = ? AND state = 'running'`,
			runState, micros, fixture.RunID, fixture.TaskID,
		)
		if err != nil {
			return repositoryWriteError("finalize live smoke run", err)
		}
		if changed, _ := runResult.RowsAffected(); changed != 1 {
			return typedError(
				ErrConflict, "finalize live provider smoke request",
				errors.New("live smoke run is not running"),
			)
		}
		taskResult, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE tasks SET state = ?,
				failure_reason = CASE WHEN ? IN ('failed', 'recovery-required')
				    THEN 'live provider smoke request did not complete successfully'
				    ELSE failure_reason END,
				cancellation_reason = CASE WHEN ? = 'cancelled'
				    THEN 'live provider smoke request was cancelled'
				    ELSE cancellation_reason END,
				updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ? AND state = 'running'`,
			taskState, taskState, taskState, micros, fixture.TaskID,
		)
		if err != nil {
			return repositoryWriteError("finalize live smoke task", err)
		}
		if changed, _ := taskResult.RowsAffected(); changed != 1 {
			return typedError(
				ErrConflict, "finalize live provider smoke request",
				errors.New("live smoke task is not running"),
			)
		}
		var sequence uint64
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT coalesce(max(sequence), 0) + 1
			 FROM task_events WHERE task_id = ?`,
			fixture.TaskID,
		).Scan(&sequence); err != nil {
			return classify("allocate live smoke final event sequence", err)
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO task_events (
				id, task_id, run_id, sequence, event_type, payload_json,
				idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, '{"diagnostic":"live-provider-smoke"}', ?, ?)`,
			eventID, fixture.TaskID, fixture.RunID, sequence,
			"provider.live-smoke."+string(input.To),
			"live-smoke-final-"+input.RequestID.String(), micros,
		); err != nil {
			return repositoryWriteError("record live smoke completion", err)
		}
		return nil
	})
	if err != nil {
		return ProviderRequestAttribution{}, err
	}
	return repositories.GetProviderRequestAttribution(ctx, input.RequestID)
}

type liveProviderSmokeFixture struct {
	IdempotencySHA256       string
	InputSHA256             string
	ProjectID               domain.ProjectID
	RepositoryID            domain.RepositoryID
	ThreadID                domain.ThreadID
	TaskID                  domain.TaskID
	RunID                   domain.RunID
	ProviderID              domain.ProviderID
	ConfigurationRevisionID string
	PricingRevisionID       string
	LogicalRequestID        domain.ModelRequestID
}

type liveProviderSmokeIDs struct {
	ProjectID       domain.ProjectID
	RepositoryID    domain.RepositoryID
	ThreadID        domain.ThreadID
	TaskID          domain.TaskID
	RunID           domain.RunID
	ProviderID      domain.ProviderID
	RequestID       domain.ModelRequestID
	EventID         domain.EventID
	ConfigurationID string
	PricingID       string
}

func newLiveProviderSmokeIDs() (liveProviderSmokeIDs, error) {
	var ids liveProviderSmokeIDs
	var err error
	if ids.ProjectID, err = domain.NewProjectID(); err != nil {
		return ids, err
	}
	if ids.RepositoryID, err = domain.NewRepositoryID(); err != nil {
		return ids, err
	}
	if ids.ThreadID, err = domain.NewThreadID(); err != nil {
		return ids, err
	}
	if ids.TaskID, err = domain.NewTaskID(); err != nil {
		return ids, err
	}
	if ids.RunID, err = domain.NewRunID(); err != nil {
		return ids, err
	}
	if ids.ProviderID, err = domain.NewProviderID(); err != nil {
		return ids, err
	}
	if ids.RequestID, err = domain.NewModelRequestID(); err != nil {
		return ids, err
	}
	if ids.EventID, err = domain.NewEventID(); err != nil {
		return ids, err
	}
	ids.ConfigurationID = ids.RequestID.String() + "-configuration"
	ids.PricingID = ids.RequestID.String() + "-pricing"
	return ids, nil
}

func ensureLiveSmokeRepository(
	ctx context.Context,
	transaction *Transaction,
	newProjectID domain.ProjectID,
	newRepositoryID domain.RepositoryID,
	input PrepareLiveProviderSmokeRequest,
	micros int64,
) (domain.ProjectID, domain.RepositoryID, error) {
	var projectID domain.ProjectID
	var repositoryID domain.RepositoryID
	err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT project_id, id FROM repositories
		 WHERE canonical_path = ? AND deleted_at_unix_micros IS NULL`,
		input.RepositoryPath,
	).Scan(&projectID, &repositoryID)
	if err == nil {
		var gitIdentity string
		if err := transaction.sql.QueryRowContext(
			ctx, `SELECT git_identity FROM repositories WHERE id = ?`,
			repositoryID,
		).Scan(&gitIdentity); err != nil {
			return domain.ProjectID{}, domain.RepositoryID{}, classify("read live smoke repository", err)
		}
		if gitIdentity != input.RepositoryGitIdentity {
			return domain.ProjectID{}, domain.RepositoryID{}, typedError(
				ErrConflict, "prepare live provider smoke request",
				errors.New("repository path is already bound to a different Git identity"),
			)
		}
		return projectID, repositoryID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.ProjectID{}, domain.RepositoryID{}, classify("find live smoke repository", err)
	}
	if _, err := transaction.sql.ExecContext(
		ctx,
		`INSERT INTO projects (
			id, name, created_at_unix_micros, updated_at_unix_micros
		) VALUES (?, 'Codeflux live-provider diagnostics', ?, ?)`,
		newProjectID, micros, micros,
	); err != nil {
		return domain.ProjectID{}, domain.RepositoryID{}, repositoryWriteError("create live smoke project", err)
	}
	if _, err := transaction.sql.ExecContext(
		ctx,
		`INSERT INTO repositories (
			id, project_id, canonical_path, git_identity,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?)`,
		newRepositoryID, newProjectID, input.RepositoryPath,
		input.RepositoryGitIdentity, micros, micros,
	); err != nil {
		return domain.ProjectID{}, domain.RepositoryID{}, repositoryWriteError("create live smoke repository", err)
	}
	return newProjectID, newRepositoryID, nil
}

func ensureLiveSmokeProvider(
	ctx context.Context,
	transaction *Transaction,
	newProviderID domain.ProviderID,
	input PrepareLiveProviderSmokeRequest,
	micros int64,
) (domain.ProviderID, error) {
	var providerID domain.ProviderID
	err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT id FROM providers
		 WHERE provider_type = ? AND display_name = ?`,
		input.ProviderType, input.ProviderDisplayName,
	).Scan(&providerID)
	if errors.Is(err, sql.ErrNoRows) {
		providerID = newProviderID
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO providers (
				id, display_name, provider_type, enabled,
				created_at_unix_micros, updated_at_unix_micros
			) VALUES (?, ?, ?, 1, ?, ?)`,
			providerID, input.ProviderDisplayName, input.ProviderType,
			micros, micros,
		); err != nil {
			return domain.ProviderID{}, repositoryWriteError("create live smoke provider", err)
		}
	} else if err != nil {
		return domain.ProviderID{}, classify("find live smoke provider", err)
	}
	var credentialReference string
	err = transaction.sql.QueryRowContext(
		ctx,
		`SELECT opaque_reference FROM provider_credential_references
		 WHERE provider_id = ?`,
		providerID,
	).Scan(&credentialReference)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO provider_credential_references (
				provider_id, opaque_reference, created_at_unix_micros,
				updated_at_unix_micros
			) VALUES (?, ?, ?, ?)`,
			providerID, input.OpaqueCredentialReference, micros, micros,
		); err != nil {
			return domain.ProviderID{}, repositoryWriteError("bind live smoke credential reference", err)
		}
	case err != nil:
		return domain.ProviderID{}, classify("read live smoke credential reference", err)
	case credentialReference != input.OpaqueCredentialReference:
		return domain.ProviderID{}, typedError(
			ErrConflict, "prepare live provider smoke request",
			errors.New("provider is already bound to a different credential reference"),
		)
	}
	return providerID, nil
}

func ensureLiveSmokeConfiguration(
	ctx context.Context,
	transaction *Transaction,
	providerID domain.ProviderID,
	newID string,
	idempotencyHash string,
	input PrepareLiveProviderSmokeRequest,
	now time.Time,
	micros int64,
) (ProviderConfigurationRevision, error) {
	contentHash := liveSmokeConfigurationHash(input)
	var id string
	err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT id FROM provider_configuration_revisions
		 WHERE provider_id = ? AND content_sha256 = ?
		 ORDER BY revision DESC LIMIT 1`,
		providerID, contentHash,
	).Scan(&id)
	if err == nil {
		return getProviderConfigurationRevisionByID(ctx, transaction.sql, id)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ProviderConfigurationRevision{}, classify("find live smoke configuration", err)
	}
	var latest uint64
	if err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT coalesce(max(revision), 0)
		 FROM provider_configuration_revisions WHERE provider_id = ?`,
		providerID,
	).Scan(&latest); err != nil {
		return ProviderConfigurationRevision{}, classify("read live smoke configuration revision", err)
	}
	approval := "run-live:" + idempotencyHash[:32]
	key := "live-smoke-config-" + contentHash[:32]
	if _, err := transaction.sql.ExecContext(
		ctx,
		`INSERT INTO provider_configuration_revisions (
			id, provider_id, revision, adapter_name, adapter_version,
			provider_version, endpoint_redacted, capabilities_json,
			content_sha256, approval_reference, idempotency_key,
			created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newID, providerID, latest+1, input.AdapterName, input.AdapterVersion,
		input.ProviderVersion, input.EndpointRedacted, input.CapabilitiesJSON,
		contentHash, approval, key, micros,
	); err != nil {
		return ProviderConfigurationRevision{}, repositoryWriteError("create live smoke configuration", err)
	}
	return ProviderConfigurationRevision{
		ID: newID, ProviderID: providerID, Revision: latest + 1,
		AdapterName: input.AdapterName, AdapterVersion: input.AdapterVersion,
		ProviderVersion:  input.ProviderVersion,
		EndpointRedacted: input.EndpointRedacted,
		CapabilitiesJSON: input.CapabilitiesJSON, ContentSHA256: contentHash,
		ApprovalReference: &approval, IdempotencyKey: key, CreatedAt: now,
	}, nil
}

func insertLiveSmokePricing(
	ctx context.Context,
	transaction *Transaction,
	providerID domain.ProviderID,
	id string,
	input PrepareLiveProviderSmokeRequest,
	now time.Time,
	micros int64,
) (ProviderPricingRevision, error) {
	snapshot := ProviderPricingRevision{
		ID: id, ProviderID: providerID, ModelIdentifier: input.ModelIdentifier,
		ModelVersion: input.ModelVersion, EffectiveAt: now, CreatedAt: now,
	}
	var currency any
	var source any
	if input.Pricing != nil {
		snapshot.PricingKnown = true
		snapshot.Currency = cloneCurrency(&input.Pricing.Currency)
		snapshot.SourceRedacted = cloneString(&input.Pricing.SourceRedacted)
		snapshot.EffectiveAt = input.Pricing.EffectiveAt.UTC()
		snapshot.Components = normalizedPriceComponents(input.Pricing.Components)
		currency = string(input.Pricing.Currency)
		source = input.Pricing.SourceRedacted
	}
	if _, err := transaction.sql.ExecContext(
		ctx,
		`INSERT INTO provider_pricing_revisions (
			id, provider_id, model_identifier, model_version, currency,
			pricing_known, source_redacted, effective_at_unix_micros,
			created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, providerID, input.ModelIdentifier, input.ModelVersion, currency,
		boolInteger(snapshot.PricingKnown), source,
		snapshot.EffectiveAt.UnixMicro(), micros,
	); err != nil {
		return ProviderPricingRevision{}, repositoryWriteError("create live smoke pricing", err)
	}
	for _, component := range snapshot.Components {
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO provider_price_components (
				pricing_revision_id, usage_kind, provider_specific_kind,
				minor_numerator, token_denominator
			) VALUES (?, ?, ?, ?, ?)`,
			id, component.UsageKind,
			nullableString(component.ProviderSpecificKind),
			component.MinorNumerator, component.TokenDenominator,
		); err != nil {
			return ProviderPricingRevision{}, repositoryWriteError("create live smoke price component", err)
		}
	}
	if _, err := transaction.sql.ExecContext(
		ctx,
		`INSERT INTO provider_pricing_revision_seals (
			pricing_revision_id, component_count, sealed_at_unix_micros
		) VALUES (?, ?, ?)`,
		id,
		len(snapshot.Components),
		micros,
	); err != nil {
		return ProviderPricingRevision{}, repositoryWriteError(
			"seal live smoke pricing",
			err,
		)
	}
	return snapshot, nil
}

func (repositories *Repositories) loadLiveProviderSmokeRequest(
	ctx context.Context,
	fixture liveProviderSmokeFixture,
) (LiveProviderSmokeRequest, error) {
	configuration, err := getProviderConfigurationRevisionByID(
		ctx, repositories.database.sql, fixture.ConfigurationRevisionID,
	)
	if err != nil {
		return LiveProviderSmokeRequest{}, err
	}
	pricing, found, err := findProviderPricingRevision(
		ctx, repositories.database.sql, fixture.PricingRevisionID,
	)
	if err != nil {
		return LiveProviderSmokeRequest{}, err
	}
	if !found {
		return LiveProviderSmokeRequest{}, typedError(
			ErrNotFound, "load live smoke pricing", sql.ErrNoRows,
		)
	}
	request, err := getProviderLogicalRequest(
		ctx, repositories.database.sql, fixture.LogicalRequestID,
	)
	if err != nil {
		return LiveProviderSmokeRequest{}, err
	}
	return LiveProviderSmokeRequest{
		ProjectID: fixture.ProjectID, RepositoryID: fixture.RepositoryID,
		ThreadID: fixture.ThreadID, TaskID: fixture.TaskID, RunID: fixture.RunID,
		ProviderID: fixture.ProviderID, Configuration: configuration,
		Pricing: pricing, Request: request,
	}, nil
}

func findLiveProviderSmokeFixture(
	ctx context.Context,
	queries queryRower,
	idempotencyHash string,
) (liveProviderSmokeFixture, bool, error) {
	var fixture liveProviderSmokeFixture
	err := queries.QueryRowContext(
		ctx,
		`SELECT idempotency_sha256, input_sha256, project_id, repository_id,
		        thread_id, task_id, run_id, provider_id,
		        configuration_revision_id, pricing_revision_id,
		        logical_request_id
		 FROM provider_live_smoke_fixtures
		 WHERE idempotency_sha256 = ?`,
		idempotencyHash,
	).Scan(
		&fixture.IdempotencySHA256, &fixture.InputSHA256,
		&fixture.ProjectID, &fixture.RepositoryID, &fixture.ThreadID,
		&fixture.TaskID, &fixture.RunID, &fixture.ProviderID,
		&fixture.ConfigurationRevisionID, &fixture.PricingRevisionID,
		&fixture.LogicalRequestID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return liveProviderSmokeFixture{}, false, nil
	}
	if err != nil {
		return liveProviderSmokeFixture{}, false, classify("find live smoke fixture", err)
	}
	return fixture, true, nil
}

func findLiveProviderSmokeFixtureByRequest(
	ctx context.Context,
	queries queryRower,
	requestID domain.ModelRequestID,
) (liveProviderSmokeFixture, bool, error) {
	var idempotencyHash string
	err := queries.QueryRowContext(
		ctx,
		`SELECT idempotency_sha256 FROM provider_live_smoke_fixtures
		 WHERE logical_request_id = ?`,
		requestID,
	).Scan(&idempotencyHash)
	if errors.Is(err, sql.ErrNoRows) {
		return liveProviderSmokeFixture{}, false, nil
	}
	if err != nil {
		return liveProviderSmokeFixture{}, false, classify("find live smoke fixture by request", err)
	}
	return findLiveProviderSmokeFixture(ctx, queries, idempotencyHash)
}

func liveSmokeTerminalStates(
	state ProviderLogicalRequestState,
) (taskState string, runState string) {
	switch state {
	case ProviderLogicalRequestSucceeded:
		return "completed", "completed"
	case ProviderLogicalRequestCancelled:
		return "cancelled", "cancelled"
	case ProviderLogicalRequestOutcomeUnknown:
		return "recovery-required", "recovery-required"
	case ProviderLogicalRequestRetryExhausted:
		return "paused", "paused"
	default:
		return "failed", "failed"
	}
}

func liveSmokeStateFromLastAttempt(
	logicalState ProviderLogicalRequestState,
	attemptState ProviderRequestAttemptState,
	retryable bool,
) (ProviderLogicalRequestState, bool) {
	switch attemptState {
	case ProviderRequestAttemptSucceeded:
		return ProviderLogicalRequestSucceeded, true
	case ProviderRequestAttemptCancelled:
		return ProviderLogicalRequestCancelled, true
	case ProviderRequestAttemptOutcomeUnknown:
		return ProviderLogicalRequestOutcomeUnknown, true
	case ProviderRequestAttemptFailed:
		if logicalState == ProviderLogicalRequestCancelled && retryable {
			// Cancellation may arrive during the bounded retry wait, after the
			// last failed attempt was already durable.
			return ProviderLogicalRequestCancelled, true
		}
		if retryable {
			return ProviderLogicalRequestRetryExhausted, true
		}
		return ProviderLogicalRequestFailed, true
	default:
		return "", false
	}
}

func getProviderConfigurationRevisionByID(
	ctx context.Context,
	queries queryRower,
	id string,
) (ProviderConfigurationRevision, error) {
	var row ProviderConfigurationRevision
	var approval sql.NullString
	var created int64
	err := queries.QueryRowContext(
		ctx,
		`SELECT id, provider_id, revision, adapter_name, adapter_version,
		        provider_version, endpoint_redacted, capabilities_json,
		        content_sha256, approval_reference, idempotency_key,
		        created_at_unix_micros
		 FROM provider_configuration_revisions WHERE id = ?`,
		id,
	).Scan(
		&row.ID, &row.ProviderID, &row.Revision, &row.AdapterName,
		&row.AdapterVersion, &row.ProviderVersion, &row.EndpointRedacted,
		&row.CapabilitiesJSON, &row.ContentSHA256, &approval,
		&row.IdempotencyKey, &created,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderConfigurationRevision{}, typedError(
			ErrNotFound, "get provider configuration revision", err,
		)
	}
	if err != nil {
		return ProviderConfigurationRevision{}, classify("get provider configuration revision", err)
	}
	row.ApprovalReference = nullStringPointer(approval)
	row.CreatedAt = repositoryTime(created)
	return row, nil
}

func validateLiveProviderSmokeInput(input PrepareLiveProviderSmokeRequest) error {
	if err := validateBounded("live smoke idempotency key", input.IdempotencyKey, 255); err != nil {
		return err
	}
	if !filepath.IsAbs(input.RepositoryPath) {
		return errors.New("live smoke repository path must be absolute")
	}
	if err := validateBounded("repository path", input.RepositoryPath, 4096); err != nil {
		return err
	}
	if err := validateBounded("repository Git identity", input.RepositoryGitIdentity, 512); err != nil {
		return err
	}
	if !slices.Contains([]string{"openai", "anthropic"}, input.ProviderType) {
		return errors.New("live smoke provider type is unsupported")
	}
	for label, value := range map[string]string{
		"provider display name": input.ProviderDisplayName,
		"adapter name":          input.AdapterName,
		"adapter version":       input.AdapterVersion,
		"provider version":      input.ProviderVersion,
		"redacted endpoint":     input.EndpointRedacted,
		"model identifier":      input.ModelIdentifier,
		"model version":         input.ModelVersion,
	} {
		maximum := 255
		if label == "redacted endpoint" {
			maximum = 2048
		}
		if err := validateBounded(label, value, maximum); err != nil {
			return err
		}
	}
	endpoint, err := url.Parse(input.EndpointRedacted)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" ||
		endpoint.User != nil {
		return errors.New("live smoke endpoint must be a redacted HTTPS URL without user information")
	}
	if err := validateJSONBounded(input.CapabilitiesJSON, 65536); err != nil {
		return fmt.Errorf("live smoke capabilities: %w", err)
	}
	if err := validateOpaqueCredentialReference(input.OpaqueCredentialReference); err != nil {
		return err
	}
	if !validSHA256(input.RequestSHA256) {
		return errors.New("live smoke request hash is invalid")
	}
	if input.Pricing != nil {
		if input.Pricing.EffectiveAt.IsZero() {
			return errors.New("live smoke pricing effective time is required")
		}
		if err := validateBounded("redacted pricing source", input.Pricing.SourceRedacted, 2048); err != nil {
			return err
		}
		pricing := CreateProviderPricingRevision{
			ID: "live-smoke-pricing-validation", ProviderID: mustValidationProviderID(),
			ModelIdentifier: input.ModelIdentifier, ModelVersion: input.ModelVersion,
			PricingKnown: true, Currency: &input.Pricing.Currency,
			SourceRedacted: &input.Pricing.SourceRedacted,
			EffectiveAt:    input.Pricing.EffectiveAt,
			Components:     input.Pricing.Components,
		}
		if err := validateProviderPricingRevision(pricing); err != nil {
			return err
		}
	}
	return nil
}

func mustValidationProviderID() domain.ProviderID {
	id, _ := domain.ParseProviderID(
		"prv_00000000-0000-7000-8000-000000000000",
	)
	return id
}

func liveProviderSmokeInputHash(input PrepareLiveProviderSmokeRequest) string {
	fields := []string{
		input.RepositoryPath, input.RepositoryGitIdentity, input.ProviderType,
		input.ProviderDisplayName, input.AdapterName, input.AdapterVersion,
		input.ProviderVersion, input.EndpointRedacted, input.CapabilitiesJSON,
		input.OpaqueCredentialReference, input.ModelIdentifier,
		input.ModelVersion, input.RequestSHA256,
	}
	if input.Pricing == nil {
		fields = append(fields, "pricing:unknown")
	} else {
		fields = append(
			fields, string(input.Pricing.Currency),
			input.Pricing.SourceRedacted,
			input.Pricing.EffectiveAt.UTC().Format(time.RFC3339Nano),
		)
		for _, component := range normalizedPriceComponents(input.Pricing.Components) {
			category := ""
			if component.ProviderSpecificKind != nil {
				category = *component.ProviderSpecificKind
			}
			fields = append(
				fields, component.UsageKind, category,
				fmt.Sprint(component.MinorNumerator),
				fmt.Sprint(component.TokenDenominator),
			)
		}
	}
	return hashLengthPrefixedStrings(fields)
}

func liveSmokeConfigurationHash(input PrepareLiveProviderSmokeRequest) string {
	return hashLengthPrefixedStrings([]string{
		input.AdapterName, input.AdapterVersion, input.ProviderVersion,
		input.EndpointRedacted, input.CapabilitiesJSON,
	})
}

func hashLengthPrefixedStrings(fields []string) string {
	hash := sha256.New()
	var length [8]byte
	for _, field := range fields {
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(field))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
