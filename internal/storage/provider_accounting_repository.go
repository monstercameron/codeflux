package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

type providerQueryer interface {
	queryRower
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

const (
	maximumProviderPriceComponents   = 128
	maximumProviderRequestAttempts   = 32
	maximumProviderAccountingRecords = 1024
	maximumProviderEvidenceRecords   = 4096
)

type ProviderConfigurationRevision struct {
	ID                string
	ProviderID        domain.ProviderID
	Revision          uint64
	AdapterName       string
	AdapterVersion    string
	ProviderVersion   string
	EndpointRedacted  string
	CapabilitiesJSON  string
	ContentSHA256     string
	ApprovalReference *string
	IdempotencyKey    string
	CreatedAt         time.Time
}

type CreateProviderConfigurationRevision struct {
	ID                     string
	ProviderID             domain.ProviderID
	ExpectedLatestRevision uint64
	AdapterName            string
	AdapterVersion         string
	ProviderVersion        string
	EndpointRedacted       string
	CapabilitiesJSON       string
	ContentSHA256          string
	ApprovalReference      *string
	IdempotencyKey         string
}

type ProviderPriceComponent struct {
	UsageKind            string
	ProviderSpecificKind *string
	MinorNumerator       int64
	TokenDenominator     int64
}

type ProviderPricingRevision struct {
	ID              string
	ProviderID      domain.ProviderID
	ModelIdentifier string
	ModelVersion    string
	PricingKnown    bool
	Currency        *domain.CurrencyCode
	SourceRedacted  *string
	EffectiveAt     time.Time
	CreatedAt       time.Time
	Components      []ProviderPriceComponent
}

type CreateProviderPricingRevision struct {
	ID              string
	ProviderID      domain.ProviderID
	ModelIdentifier string
	ModelVersion    string
	PricingKnown    bool
	Currency        *domain.CurrencyCode
	SourceRedacted  *string
	EffectiveAt     time.Time
	Components      []ProviderPriceComponent
}

type ProviderLogicalRequestState string

const (
	ProviderLogicalRequestPlanned        ProviderLogicalRequestState = "planned"
	ProviderLogicalRequestInFlight       ProviderLogicalRequestState = "in-flight"
	ProviderLogicalRequestSucceeded      ProviderLogicalRequestState = "succeeded"
	ProviderLogicalRequestFailed         ProviderLogicalRequestState = "failed"
	ProviderLogicalRequestCancelled      ProviderLogicalRequestState = "cancelled"
	ProviderLogicalRequestOutcomeUnknown ProviderLogicalRequestState = "outcome-unknown"
	ProviderLogicalRequestRetryExhausted ProviderLogicalRequestState = "retry-exhausted"
)

type ProviderAccountingStatus string

const (
	ProviderAccountingUnknown          ProviderAccountingStatus = "unknown"
	ProviderAccountingEstimated        ProviderAccountingStatus = "estimated"
	ProviderAccountingProviderReported ProviderAccountingStatus = "provider-reported"
	ProviderAccountingReconciled       ProviderAccountingStatus = "reconciled"
	ProviderAccountingDiscrepant       ProviderAccountingStatus = "discrepant"
)

type ProviderLogicalRequest struct {
	ID                              domain.ModelRequestID
	TaskID                          domain.TaskID
	RunID                           *domain.RunID
	ProviderID                      domain.ProviderID
	ProviderConfigurationRevisionID string
	AdapterName                     string
	AdapterVersion                  string
	ProviderVersion                 string
	ModelIdentifier                 string
	ModelVersion                    string
	PricingRevisionID               *string
	State                           ProviderLogicalRequestState
	RequestSHA256                   string
	IdempotencyKey                  string
	AccountingStatus                ProviderAccountingStatus
	StartedAt                       *time.Time
	CompletedAt                     *time.Time
	CreatedAt                       time.Time
	UpdatedAt                       time.Time
	Revision                        uint64
}

type PlanProviderLogicalRequest struct {
	ID                              domain.ModelRequestID
	TaskID                          domain.TaskID
	RunID                           *domain.RunID
	ProviderID                      domain.ProviderID
	ProviderConfigurationRevisionID string
	AdapterName                     string
	AdapterVersion                  string
	ProviderVersion                 string
	ModelIdentifier                 string
	ModelVersion                    string
	PricingRevisionID               *string
	RequestSHA256                   string
	IdempotencyKey                  string
}

type TransitionProviderLogicalRequest struct {
	ID               domain.ModelRequestID
	ExpectedRevision uint64
	From             ProviderLogicalRequestState
	To               ProviderLogicalRequestState
	AccountingStatus ProviderAccountingStatus
}

type ProviderRequestAttemptState string

const (
	ProviderRequestAttemptPrepared       ProviderRequestAttemptState = "prepared"
	ProviderRequestAttemptStarted        ProviderRequestAttemptState = "started"
	ProviderRequestAttemptStreaming      ProviderRequestAttemptState = "streaming"
	ProviderRequestAttemptSucceeded      ProviderRequestAttemptState = "succeeded"
	ProviderRequestAttemptFailed         ProviderRequestAttemptState = "failed"
	ProviderRequestAttemptCancelled      ProviderRequestAttemptState = "cancelled"
	ProviderRequestAttemptOutcomeUnknown ProviderRequestAttemptState = "outcome-unknown"
)

type ProviderRequestEffectStatus string

const (
	ProviderRequestEffectNone      ProviderRequestEffectStatus = "none"
	ProviderRequestEffectPossible  ProviderRequestEffectStatus = "possible"
	ProviderRequestEffectConfirmed ProviderRequestEffectStatus = "confirmed"
)

type ProviderRequestAttempt struct {
	ID                        string
	LogicalRequestID          domain.ModelRequestID
	AttemptNumber             uint64
	State                     ProviderRequestAttemptState
	ProviderRequestIDRedacted *string
	RequestIdempotencyKey     *string
	EffectStatus              ProviderRequestEffectStatus
	PartialStreamObserved     bool
	ErrorClass                *string
	Retryable                 bool
	RetryAfterMillis          *int64
	SafeMetadataJSON          *string
	StartedAt                 *time.Time
	FirstResponseAt           *time.Time
	CompletedAt               *time.Time
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	Revision                  uint64
}

type CreateProviderRequestAttempt struct {
	ID                    string
	LogicalRequestID      domain.ModelRequestID
	AttemptNumber         uint64
	RequestIdempotencyKey *string
}

type AbortPreparedProviderRequestAttemptBeforeIO struct {
	ID               string
	ExpectedRevision uint64
	ObservedAt       time.Time
}

type EnsureProviderAttemptAccounting struct {
	AttemptID  string
	ObservedAt time.Time
}

type TransitionProviderRequestAttempt struct {
	ID                        string
	ExpectedRevision          uint64
	From                      ProviderRequestAttemptState
	To                        ProviderRequestAttemptState
	ProviderRequestIDRedacted *string
	EffectStatus              ProviderRequestEffectStatus
	PartialStreamObserved     bool
	ErrorClass                *string
	Retryable                 bool
	RetryAfterMillis          *int64
	SafeMetadataJSON          *string
	FirstResponseAt           time.Time
	ObservedAt                time.Time
}

type ExactMinorCost struct {
	Numerator   int64
	Denominator int64
	Currency    domain.CurrencyCode
}

type ProviderAttemptAccounting struct {
	ID                  string
	AttemptID           string
	Sequence            uint64
	Source              string
	Usage               domain.TokenUsage
	PricingRevisionID   *string
	Cost                *ExactMinorCost
	DiscrepancyRedacted *string
	Partial             bool
	ProvenanceJSON      string
	CreatedAt           time.Time
}

type AppendProviderAttemptAccounting struct {
	ID                  string
	AttemptID           string
	Sequence            uint64
	Source              string
	Usage               domain.TokenUsage
	PricingRevisionID   *string
	Cost                *ExactMinorCost
	DiscrepancyRedacted *string
	Partial             bool
	ProvenanceJSON      string
}

type ProviderAttemptEvidence struct {
	ID              string
	AttemptID       string
	Sequence        uint64
	Kind            string
	Final           bool
	ContentSHA256   string
	SummaryRedacted string
	ByteCount       int64
	CreatedAt       time.Time
}

type AppendProviderAttemptEvidence struct {
	ID              string
	AttemptID       string
	Sequence        uint64
	Kind            string
	Final           bool
	ContentSHA256   string
	SummaryRedacted string
	ByteCount       int64
}

type ProviderRequestAccountingSummary struct {
	RequestID             domain.ModelRequestID
	AttemptCount          uint64
	Usage                 domain.TokenUsage
	Cost                  *ExactMinorCost
	AccountingComplete    bool
	Discrepancy           bool
	PartialStreamObserved bool
}

type ProviderAccountingOperations interface {
	CreateProviderConfigurationRevision(
		context.Context,
		CreateProviderConfigurationRevision,
	) (ProviderConfigurationRevision, error)
	CreateProviderPricingRevision(
		context.Context,
		CreateProviderPricingRevision,
	) (ProviderPricingRevision, error)
	PlanProviderLogicalRequest(
		context.Context,
		PlanProviderLogicalRequest,
	) (ProviderLogicalRequest, error)
	GetProviderLogicalRequest(
		context.Context,
		domain.ModelRequestID,
	) (ProviderLogicalRequest, error)
	TransitionProviderLogicalRequest(
		context.Context,
		TransitionProviderLogicalRequest,
	) (ProviderLogicalRequest, error)
	CreateProviderRequestAttempt(
		context.Context,
		CreateProviderRequestAttempt,
	) (ProviderRequestAttempt, error)
	TransitionProviderRequestAttempt(
		context.Context,
		TransitionProviderRequestAttempt,
	) (ProviderRequestAttempt, error)
	AbortPreparedProviderRequestAttemptBeforeIO(
		context.Context,
		AbortPreparedProviderRequestAttemptBeforeIO,
	) (ProviderRequestAttempt, error)
	EnsureProviderAttemptAccounting(
		context.Context,
		EnsureProviderAttemptAccounting,
	) error
	AppendProviderAttemptAccounting(
		context.Context,
		AppendProviderAttemptAccounting,
	) (ProviderAttemptAccounting, error)
	AppendProviderAttemptEvidence(
		context.Context,
		AppendProviderAttemptEvidence,
	) (ProviderAttemptEvidence, error)
	SummarizeProviderRequestAccounting(
		context.Context,
		domain.ModelRequestID,
	) (ProviderRequestAccountingSummary, error)
}

var _ ProviderAccountingOperations = (*Repositories)(nil)

func (repositories *Repositories) CreateProviderConfigurationRevision(
	ctx context.Context,
	input CreateProviderConfigurationRevision,
) (ProviderConfigurationRevision, error) {
	if err := validateProviderConfigurationRevision(input); err != nil {
		return ProviderConfigurationRevision{}, err
	}
	var revision ProviderConfigurationRevision
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findProviderConfigurationByIdempotency(
			ctx, transaction.sql, input.ProviderID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if !providerConfigurationMatches(existing, input) {
				return typedError(ErrConflict, "create provider configuration revision", errors.New("idempotency key was reused with different configuration"))
			}
			revision = existing
			return nil
		}
		var latest uint64
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT coalesce(max(revision), 0)
			 FROM provider_configuration_revisions WHERE provider_id = ?`,
			input.ProviderID,
		).Scan(&latest); err != nil {
			return classify("read latest provider configuration revision", err)
		}
		if latest != input.ExpectedLatestRevision {
			return typedError(ErrStaleRevision, "create provider configuration revision", errors.New("provider configuration revision changed"))
		}
		now, micros := repositories.timestamp()
		assigned := latest + 1
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO provider_configuration_revisions (
				id, provider_id, revision, adapter_name, adapter_version,
				provider_version,
				endpoint_redacted, capabilities_json, content_sha256,
				approval_reference, idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.ProviderID, assigned, input.AdapterName,
			input.AdapterVersion, input.ProviderVersion,
			input.EndpointRedacted, input.CapabilitiesJSON,
			input.ContentSHA256, nullableString(input.ApprovalReference),
			input.IdempotencyKey, micros,
		); err != nil {
			return repositoryWriteError("create provider configuration revision", err)
		}
		revision = ProviderConfigurationRevision{
			ID: input.ID, ProviderID: input.ProviderID, Revision: assigned,
			AdapterName: input.AdapterName, AdapterVersion: input.AdapterVersion,
			ProviderVersion:  input.ProviderVersion,
			EndpointRedacted: input.EndpointRedacted,
			CapabilitiesJSON: input.CapabilitiesJSON, ContentSHA256: input.ContentSHA256,
			ApprovalReference: cloneString(input.ApprovalReference),
			IdempotencyKey:    input.IdempotencyKey, CreatedAt: now,
		}
		return nil
	})
	return revision, err
}

func (repositories *Repositories) CreateProviderPricingRevision(
	ctx context.Context,
	input CreateProviderPricingRevision,
) (ProviderPricingRevision, error) {
	if err := validateProviderPricingRevision(input); err != nil {
		return ProviderPricingRevision{}, err
	}
	var snapshot ProviderPricingRevision
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findProviderPricingRevision(ctx, transaction.sql, input.ID)
		if err != nil {
			return err
		}
		if found {
			if !providerPricingMatches(existing, input) {
				return typedError(ErrConflict, "create provider pricing revision", errors.New("pricing revision identity was reused with different values"))
			}
			var sealCount int
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT count(*) FROM provider_pricing_revision_seals
				 WHERE pricing_revision_id = ? AND component_count = ?`,
				input.ID,
				len(existing.Components),
			).Scan(&sealCount); err != nil {
				return classify("verify provider pricing revision seal", err)
			}
			if sealCount != 1 {
				return typedError(
					ErrConflict,
					"create provider pricing revision",
					errors.New("pricing revision is not sealed"),
				)
			}
			snapshot = existing
			return nil
		}
		now, micros := repositories.timestamp()
		var currency any
		if input.Currency != nil {
			currency = string(*input.Currency)
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO provider_pricing_revisions (
				id, provider_id, model_identifier, model_version, currency,
				pricing_known, source_redacted, effective_at_unix_micros,
				created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.ProviderID, input.ModelIdentifier, input.ModelVersion,
			currency, boolInteger(input.PricingKnown), nullableString(input.SourceRedacted),
			input.EffectiveAt.UTC().UnixMicro(), micros,
		); err != nil {
			return repositoryWriteError("create provider pricing revision", err)
		}
		components := normalizedPriceComponents(input.Components)
		for _, component := range components {
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO provider_price_components (
					pricing_revision_id, usage_kind, provider_specific_kind,
					minor_numerator, token_denominator
				) VALUES (?, ?, ?, ?, ?)`,
				input.ID, component.UsageKind,
				nullableString(component.ProviderSpecificKind),
				component.MinorNumerator, component.TokenDenominator,
			); err != nil {
				return repositoryWriteError("create provider price component", err)
			}
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO provider_pricing_revision_seals (
				pricing_revision_id, component_count, sealed_at_unix_micros
			) VALUES (?, ?, ?)`,
			input.ID,
			len(components),
			micros,
		); err != nil {
			return repositoryWriteError("seal provider pricing revision", err)
		}
		snapshot = ProviderPricingRevision{
			ID: input.ID, ProviderID: input.ProviderID,
			ModelIdentifier: input.ModelIdentifier, ModelVersion: input.ModelVersion,
			PricingKnown: input.PricingKnown, Currency: cloneCurrency(input.Currency),
			SourceRedacted: cloneString(input.SourceRedacted),
			EffectiveAt:    input.EffectiveAt.UTC(), CreatedAt: now,
			Components: components,
		}
		return nil
	})
	return snapshot, err
}

func (repositories *Repositories) PlanProviderLogicalRequest(
	ctx context.Context,
	input PlanProviderLogicalRequest,
) (ProviderLogicalRequest, error) {
	if err := validatePlanProviderLogicalRequest(input); err != nil {
		return ProviderLogicalRequest{}, err
	}
	var request ProviderLogicalRequest
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findProviderLogicalRequestByIdempotency(
			ctx, transaction.sql, input.TaskID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if !providerLogicalRequestMatches(existing, input) {
				return typedError(ErrConflict, "plan provider logical request", errors.New("idempotency key was reused with a different request"))
			}
			request = existing
			return nil
		}
		if input.RunID != nil {
			if err := verifyRunBelongsToTask(
				ctx, transaction, *input.RunID, input.TaskID,
				"plan provider logical request",
			); err != nil {
				return err
			}
		}
		now, micros := repositories.timestamp()
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO provider_logical_requests (
				id, task_id, run_id, provider_id,
				provider_configuration_revision_id, adapter_name,
				adapter_version, provider_version, model_identifier, model_version,
				pricing_revision_id, state, request_sha256, idempotency_key,
				accounting_status, created_at_unix_micros,
				updated_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'planned', ?, ?, 'unknown', ?, ?)`,
			input.ID, input.TaskID, nullableRunID(input.RunID), input.ProviderID,
			input.ProviderConfigurationRevisionID, input.AdapterName,
			input.AdapterVersion, input.ProviderVersion,
			input.ModelIdentifier, input.ModelVersion,
			nullableString(input.PricingRevisionID), input.RequestSHA256,
			input.IdempotencyKey, micros, micros,
		); err != nil {
			return repositoryWriteError("plan provider logical request", err)
		}
		request = ProviderLogicalRequest{
			ID: input.ID, TaskID: input.TaskID, RunID: cloneRunID(input.RunID),
			ProviderID:                      input.ProviderID,
			ProviderConfigurationRevisionID: input.ProviderConfigurationRevisionID,
			AdapterName:                     input.AdapterName, AdapterVersion: input.AdapterVersion,
			ProviderVersion: input.ProviderVersion,
			ModelIdentifier: input.ModelIdentifier, ModelVersion: input.ModelVersion,
			PricingRevisionID: cloneString(input.PricingRevisionID),
			State:             ProviderLogicalRequestPlanned, RequestSHA256: input.RequestSHA256,
			IdempotencyKey:   input.IdempotencyKey,
			AccountingStatus: ProviderAccountingUnknown,
			CreatedAt:        now, UpdatedAt: now,
		}
		return nil
	})
	return request, err
}

func (repositories *Repositories) GetProviderLogicalRequest(
	ctx context.Context,
	id domain.ModelRequestID,
) (ProviderLogicalRequest, error) {
	if id.IsZero() {
		return ProviderLogicalRequest{}, errors.New("provider logical request ID must not be empty")
	}
	return getProviderLogicalRequest(ctx, repositories.database.sql, id)
}

func (repositories *Repositories) TransitionProviderLogicalRequest(
	ctx context.Context,
	input TransitionProviderLogicalRequest,
) (ProviderLogicalRequest, error) {
	if input.ID.IsZero() || !providerLogicalTransitionAllowed(input.From, input.To) ||
		!providerAccountingStatusValid(input.AccountingStatus) {
		return ProviderLogicalRequest{}, errors.New("provider logical request transition is invalid")
	}
	var request ProviderLogicalRequest
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := getProviderLogicalRequest(ctx, transaction.sql, input.ID)
		if err != nil {
			return err
		}
		if current.State == input.To &&
			current.AccountingStatus == input.AccountingStatus &&
			current.Revision == input.ExpectedRevision+1 {
			request = current
			return nil
		}
		if current.Revision != input.ExpectedRevision {
			return typedError(ErrStaleRevision, "transition provider logical request", errors.New("provider logical request revision changed"))
		}
		if current.State != input.From {
			return typedError(ErrConflict, "transition provider logical request", errors.New("provider logical request state changed"))
		}
		now, micros := repositories.timestamp()
		var started any
		if current.StartedAt != nil {
			started = current.StartedAt.UnixMicro()
		} else if input.To == ProviderLogicalRequestInFlight ||
			providerLogicalTerminal(input.To) {
			started = micros
		}
		var completed any
		if providerLogicalTerminal(input.To) {
			completed = micros
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE provider_logical_requests SET state = ?,
				accounting_status = ?, started_at_unix_micros = ?,
				completed_at_unix_micros = ?, updated_at_unix_micros = ?,
				revision = revision + 1
			 WHERE id = ? AND revision = ? AND state = ?`,
			input.To, input.AccountingStatus, started, completed, micros,
			input.ID, input.ExpectedRevision, input.From,
		)
		if err != nil {
			return repositoryWriteError("transition provider logical request", err)
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return typedError(ErrStaleRevision, "transition provider logical request", errors.New("provider logical request revision changed"))
		}
		request, err = getProviderLogicalRequest(ctx, transaction.sql, input.ID)
		_ = now
		return err
	})
	return request, err
}

func (repositories *Repositories) CreateProviderRequestAttempt(
	ctx context.Context,
	input CreateProviderRequestAttempt,
) (ProviderRequestAttempt, error) {
	if err := validateCreateProviderRequestAttempt(input); err != nil {
		return ProviderRequestAttempt{}, err
	}
	var attempt ProviderRequestAttempt
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findProviderRequestAttemptByNumber(
			ctx, transaction.sql, input.LogicalRequestID, input.AttemptNumber,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID ||
				!equalOptionalString(existing.RequestIdempotencyKey, input.RequestIdempotencyKey) {
				return typedError(ErrConflict, "create provider request attempt", errors.New("attempt number was reused with different identity"))
			}
			attempt = existing
			return nil
		}
		request, err := getProviderLogicalRequest(ctx, transaction.sql, input.LogicalRequestID)
		if err != nil {
			return err
		}
		if request.State != ProviderLogicalRequestInFlight {
			return typedError(ErrConflict, "create provider request attempt", errors.New("logical request is not in flight"))
		}
		now, micros := repositories.timestamp()
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO provider_request_attempts (
				id, logical_request_id, attempt_number, state,
				request_idempotency_key, effect_status,
				created_at_unix_micros, updated_at_unix_micros
			) VALUES (?, ?, ?, 'prepared', ?, 'none', ?, ?)`,
			input.ID, input.LogicalRequestID, input.AttemptNumber,
			nullableString(input.RequestIdempotencyKey), micros, micros,
		); err != nil {
			return repositoryWriteError("create provider request attempt", err)
		}
		attempt = ProviderRequestAttempt{
			ID: input.ID, LogicalRequestID: input.LogicalRequestID,
			AttemptNumber:         input.AttemptNumber,
			State:                 ProviderRequestAttemptPrepared,
			RequestIdempotencyKey: cloneString(input.RequestIdempotencyKey),
			EffectStatus:          ProviderRequestEffectNone,
			CreatedAt:             now, UpdatedAt: now,
		}
		return nil
	})
	return attempt, err
}

func (repositories *Repositories) TransitionProviderRequestAttempt(
	ctx context.Context,
	input TransitionProviderRequestAttempt,
) (ProviderRequestAttempt, error) {
	if err := validateProviderRequestAttemptTransition(input); err != nil {
		return ProviderRequestAttempt{}, err
	}
	var attempt ProviderRequestAttempt
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := getProviderRequestAttempt(ctx, transaction.sql, input.ID)
		if err != nil {
			return err
		}
		if current.Revision == input.ExpectedRevision+1 &&
			providerAttemptTransitionMatches(current, input) {
			attempt = current
			return nil
		}
		if current.Revision != input.ExpectedRevision {
			return typedError(ErrStaleRevision, "transition provider request attempt", errors.New("provider request attempt revision changed"))
		}
		if current.State != input.From {
			return typedError(ErrConflict, "transition provider request attempt", errors.New("provider request attempt state changed"))
		}
		if providerEffectStatusRank(input.EffectStatus) <
			providerEffectStatusRank(current.EffectStatus) {
			return typedError(ErrConflict, "transition provider request attempt", errors.New("provider effect evidence cannot regress"))
		}
		observed := input.ObservedAt.UTC()
		var started any
		if current.StartedAt != nil {
			started = current.StartedAt.UnixMicro()
		} else if input.To != ProviderRequestAttemptPrepared {
			started = observed.UnixMicro()
		}
		var firstResponse any
		if current.FirstResponseAt != nil {
			if !input.FirstResponseAt.IsZero() &&
				current.FirstResponseAt.UnixMicro() != input.FirstResponseAt.UnixMicro() {
				return typedError(
					ErrConflict, "transition provider request attempt",
					errors.New("provider first-response timestamp cannot change"),
				)
			}
			firstResponse = current.FirstResponseAt.UnixMicro()
		} else if !input.FirstResponseAt.IsZero() {
			if current.StartedAt != nil &&
				input.FirstResponseAt.Before(*current.StartedAt) {
				return typedError(
					ErrConflict, "transition provider request attempt",
					errors.New("provider first response precedes attempt start"),
				)
			}
			firstResponse = input.FirstResponseAt.UnixMicro()
		} else if input.To == ProviderRequestAttemptStreaming ||
			input.PartialStreamObserved {
			firstResponse = observed.UnixMicro()
		}
		var completed any
		if providerAttemptTerminal(input.To) {
			if current.FirstResponseAt != nil &&
				observed.Before(*current.FirstResponseAt) {
				return typedError(
					ErrConflict, "transition provider request attempt",
					errors.New("provider attempt completion precedes first response"),
				)
			}
			completed = observed.UnixMicro()
		}
		_, micros := repositories.timestamp()
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE provider_request_attempts SET state = ?,
				provider_request_id_redacted = coalesce(?, provider_request_id_redacted),
				effect_status = ?,
				partial_stream_observed = max(partial_stream_observed, ?),
				error_class = ?, retryable = ?,
				retry_after_millis = ?,
				safe_metadata_json = coalesce(?, safe_metadata_json),
				started_at_unix_micros = ?, first_response_at_unix_micros = ?,
				completed_at_unix_micros = ?, updated_at_unix_micros = ?,
				revision = revision + 1
			 WHERE id = ? AND revision = ? AND state = ?`,
			input.To, nullableString(input.ProviderRequestIDRedacted),
			input.EffectStatus, boolInteger(input.PartialStreamObserved),
			nullableString(input.ErrorClass), boolInteger(input.Retryable),
			nullableInt64(input.RetryAfterMillis),
			nullableString(input.SafeMetadataJSON), started, firstResponse,
			completed, micros, input.ID, input.ExpectedRevision, input.From,
		)
		if err != nil {
			return repositoryWriteError("transition provider request attempt", err)
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return typedError(ErrStaleRevision, "transition provider request attempt", errors.New("provider request attempt revision changed"))
		}
		attempt, err = getProviderRequestAttempt(ctx, transaction.sql, input.ID)
		return err
	})
	return attempt, err
}

func (repositories *Repositories) AbortPreparedProviderRequestAttemptBeforeIO(
	ctx context.Context,
	input AbortPreparedProviderRequestAttemptBeforeIO,
) (ProviderRequestAttempt, error) {
	if err := validateBounded("provider request attempt ID", input.ID, 255); err != nil {
		return ProviderRequestAttempt{}, err
	}
	if input.ObservedAt.IsZero() || input.ObservedAt.Location() != time.UTC {
		return ProviderRequestAttempt{}, errors.New(
			"pre-I/O provider abort requires a UTC observation time",
		)
	}
	var attempt ProviderRequestAttempt
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := getProviderRequestAttempt(ctx, transaction.sql, input.ID)
		if err != nil {
			return err
		}
		accountingID := input.ID + "-pre-io-accounting"
		if current.State == ProviderRequestAttemptCancelled &&
			current.Revision == input.ExpectedRevision+1 {
			var count int
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT count(*) FROM provider_attempt_accounting
				 WHERE id = ? AND attempt_id = ?`,
				accountingID,
				input.ID,
			).Scan(&count); err != nil {
				return classify("verify pre-I/O provider abort accounting", err)
			}
			if count != 1 {
				return typedError(
					ErrConflict,
					"abort prepared provider attempt before I/O",
					errors.New("cancelled pre-I/O attempt lacks accounting"),
				)
			}
			attempt = current
			return nil
		}
		if current.Revision != input.ExpectedRevision ||
			current.State != ProviderRequestAttemptPrepared {
			return typedError(
				ErrConflict,
				"abort prepared provider attempt before I/O",
				errors.New("provider attempt is not the expected prepared revision"),
			)
		}
		_, updatedMicros := repositories.timestamp()
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE provider_request_attempts SET
				state = 'cancelled', effect_status = 'none',
				error_class = 'cancelled', retryable = 0,
				started_at_unix_micros = NULL,
				first_response_at_unix_micros = NULL,
				completed_at_unix_micros = NULL,
				updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ? AND revision = ? AND state = 'prepared'`,
			updatedMicros,
			input.ID,
			input.ExpectedRevision,
		)
		if err != nil {
			return repositoryWriteError("abort prepared provider attempt before I/O", err)
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return typedError(
				ErrStaleRevision,
				"abort prepared provider attempt before I/O",
				errors.New("provider attempt revision changed"),
			)
		}
		var pricingID sql.NullString
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT request.pricing_revision_id
			 FROM provider_request_attempts AS physical
			 JOIN provider_logical_requests AS request
			   ON request.id = physical.logical_request_id
			 WHERE physical.id = ?`,
			input.ID,
		).Scan(&pricingID); err != nil {
			return classify("read pre-I/O provider pricing", err)
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO provider_attempt_accounting (
				id, attempt_id, sequence, source, usage_known,
				input_tokens, cached_input_tokens, cache_write_tokens,
				output_tokens, reasoning_tokens, provider_specific_json,
				pricing_revision_id, cost_known, cost_minor_numerator,
				cost_minor_denominator, currency, discrepancy,
				discrepancy_redacted, partial, provenance_json,
				created_at_unix_micros
			) VALUES (
				?, ?, 1, 'provider-final', 0,
				NULL, NULL, NULL, NULL, NULL, NULL,
				?, 0, NULL, NULL, NULL, 0, NULL, 0,
				'{"source":"pre-io-abort","usage":"unknown"}', ?
			)`,
			accountingID,
			input.ID,
			nullableString(nullStringPointer(pricingID)),
			updatedMicros,
		); err != nil {
			return repositoryWriteError("record pre-I/O provider abort accounting", err)
		}
		attempt, err = getProviderRequestAttempt(ctx, transaction.sql, input.ID)
		return err
	})
	return attempt, err
}

func (repositories *Repositories) EnsureProviderAttemptAccounting(
	ctx context.Context,
	input EnsureProviderAttemptAccounting,
) error {
	if err := validateBounded(
		"provider request attempt ID",
		input.AttemptID,
		255,
	); err != nil {
		return err
	}
	if input.ObservedAt.IsZero() || input.ObservedAt.Location() != time.UTC {
		return errors.New(
			"provider accounting fallback requires a UTC observation time",
		)
	}
	return repositories.database.RunInTransaction(
		ctx,
		func(transaction *Transaction) error {
			var count int
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT count(*) FROM provider_attempt_accounting
				 WHERE attempt_id = ?`,
				input.AttemptID,
			).Scan(&count); err != nil {
				return classify("check provider attempt accounting", err)
			}
			if count != 0 {
				return nil
			}
			var pricingID sql.NullString
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT request.pricing_revision_id
				 FROM provider_request_attempts AS physical
				 JOIN provider_logical_requests AS request
				   ON request.id = physical.logical_request_id
				 WHERE physical.id = ?`,
				input.AttemptID,
			).Scan(&pricingID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return typedError(
						ErrNotFound,
						"ensure provider attempt accounting",
						err,
					)
				}
				return classify("read provider accounting fallback pricing", err)
			}
			_, createdMicros := repositories.timestamp()
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO provider_attempt_accounting (
					id, attempt_id, sequence, source, usage_known,
					input_tokens, cached_input_tokens, cache_write_tokens,
					output_tokens, reasoning_tokens, provider_specific_json,
					pricing_revision_id, cost_known, cost_minor_numerator,
					cost_minor_denominator, currency, discrepancy,
					discrepancy_redacted, partial, provenance_json,
					created_at_unix_micros
				) VALUES (
					?, ?, 1, 'provider-final', 0,
					NULL, NULL, NULL, NULL, NULL, NULL,
					?, 0, NULL, NULL, NULL, 0, NULL, 0,
					'{"schema_version":1,"source":"terminal-fallback","usage":"unknown"}',
					?
				)`,
				input.AttemptID+"-accounting-fallback",
				input.AttemptID,
				nullableString(nullStringPointer(pricingID)),
				createdMicros,
			); err != nil {
				return repositoryWriteError(
					"ensure provider attempt accounting",
					err,
				)
			}
			return nil
		},
	)
}

func (repositories *Repositories) AppendProviderAttemptAccounting(
	ctx context.Context,
	input AppendProviderAttemptAccounting,
) (ProviderAttemptAccounting, error) {
	if err := validateProviderAttemptAccounting(input); err != nil {
		return ProviderAttemptAccounting{}, err
	}
	var record ProviderAttemptAccounting
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findProviderAttemptAccountingByID(
			ctx, transaction.sql, input.ID,
		)
		if err != nil {
			return err
		}
		if found {
			if !providerAttemptAccountingMatches(existing, input) {
				return typedError(ErrConflict, "append provider attempt accounting", errors.New("accounting identity was reused with different evidence"))
			}
			record = existing
			return nil
		}
		var logicalPricing sql.NullString
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT request.pricing_revision_id
			 FROM provider_request_attempts AS attempt
			 JOIN provider_logical_requests AS request
			   ON request.id = attempt.logical_request_id
			 WHERE attempt.id = ?`,
			input.AttemptID,
		).Scan(&logicalPricing); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return typedError(ErrNotFound, "append provider attempt accounting", err)
			}
			return classify("read provider request pricing", err)
		}
		if input.Cost != nil {
			if !logicalPricing.Valid || input.PricingRevisionID == nil ||
				logicalPricing.String != *input.PricingRevisionID {
				return typedError(ErrConflict, "append provider attempt accounting", errors.New("cost is not bound to the logical request pricing revision"))
			}
			if input.Usage.Known {
				pricing, found, err := findProviderPricingRevision(
					ctx, transaction.sql, *input.PricingRevisionID,
				)
				if err != nil {
					return err
				}
				if !found || !pricing.PricingKnown {
					return typedError(ErrConflict, "append provider attempt accounting", errors.New("known cost requires a known pricing revision"))
				}
				calculated, err := calculateProviderUsageCost(pricing, input.Usage)
				if err != nil {
					return err
				}
				if !exactCostEqual(&calculated, input.Cost) {
					return typedError(ErrConflict, "append provider attempt accounting", errors.New("cost does not match usage and pricing revision"))
				}
			}
		}
		now, micros := repositories.timestamp()
		values, err := accountingSQLValues(input)
		if err != nil {
			return err
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO provider_attempt_accounting (
				id, attempt_id, sequence, source, usage_known,
				input_tokens, cached_input_tokens, cache_write_tokens,
				output_tokens, reasoning_tokens, provider_specific_json,
				pricing_revision_id, cost_known, cost_minor_numerator,
				cost_minor_denominator, currency, discrepancy,
				discrepancy_redacted, partial, provenance_json,
				created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.AttemptID, input.Sequence, input.Source,
			values.usageKnown, values.input, values.cachedInput,
			values.cacheWrite, values.output, values.reasoning,
			values.providerSpecific, nullableString(input.PricingRevisionID),
			values.costKnown, values.costNumerator, values.costDenominator,
			values.currency, boolInteger(input.DiscrepancyRedacted != nil),
			nullableString(input.DiscrepancyRedacted), boolInteger(input.Partial),
			input.ProvenanceJSON, micros,
		); err != nil {
			return repositoryWriteError("append provider attempt accounting", err)
		}
		record = ProviderAttemptAccounting{
			ID: input.ID, AttemptID: input.AttemptID, Sequence: input.Sequence,
			Source: input.Source, Usage: cloneTokenUsage(input.Usage),
			PricingRevisionID:   cloneString(input.PricingRevisionID),
			Cost:                cloneExactCost(input.Cost),
			DiscrepancyRedacted: cloneString(input.DiscrepancyRedacted),
			Partial:             input.Partial, ProvenanceJSON: input.ProvenanceJSON,
			CreatedAt: now,
		}
		return nil
	})
	return record, err
}

func (repositories *Repositories) AppendProviderAttemptEvidence(
	ctx context.Context,
	input AppendProviderAttemptEvidence,
) (ProviderAttemptEvidence, error) {
	if err := validateProviderAttemptEvidence(input); err != nil {
		return ProviderAttemptEvidence{}, err
	}
	var evidence ProviderAttemptEvidence
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findProviderAttemptEvidenceByID(ctx, transaction.sql, input.ID)
		if err != nil {
			return err
		}
		if found {
			if !providerAttemptEvidenceMatches(existing, input) {
				return typedError(ErrConflict, "append provider attempt evidence", errors.New("evidence identity was reused with different facts"))
			}
			evidence = existing
			return nil
		}
		now, micros := repositories.timestamp()
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO provider_attempt_evidence (
				id, attempt_id, sequence, kind, final, content_sha256,
				summary_redacted, byte_count, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.AttemptID, input.Sequence, input.Kind,
			boolInteger(input.Final), input.ContentSHA256,
			input.SummaryRedacted, input.ByteCount, micros,
		); err != nil {
			return repositoryWriteError("append provider attempt evidence", err)
		}
		evidence = ProviderAttemptEvidence{
			ID: input.ID, AttemptID: input.AttemptID, Sequence: input.Sequence,
			Kind: input.Kind, Final: input.Final,
			ContentSHA256:   input.ContentSHA256,
			SummaryRedacted: input.SummaryRedacted,
			ByteCount:       input.ByteCount, CreatedAt: now,
		}
		return nil
	})
	return evidence, err
}

func (repositories *Repositories) SummarizeProviderRequestAccounting(
	ctx context.Context,
	requestID domain.ModelRequestID,
) (ProviderRequestAccountingSummary, error) {
	if requestID.IsZero() {
		return ProviderRequestAccountingSummary{}, errors.New("provider logical request ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT attempt.id, attempt.partial_stream_observed,
		        accounting.usage_known, accounting.input_tokens,
		        accounting.cached_input_tokens, accounting.cache_write_tokens,
		        accounting.output_tokens, accounting.reasoning_tokens,
		        accounting.provider_specific_json, accounting.cost_known,
		        accounting.cost_minor_numerator,
		        accounting.cost_minor_denominator, accounting.currency,
		        accounting.discrepancy
		 FROM provider_request_attempts AS attempt
		 LEFT JOIN provider_attempt_accounting AS accounting
		   ON accounting.attempt_id = attempt.id
		  AND accounting.sequence = (
		      SELECT max(latest.sequence)
		      FROM provider_attempt_accounting AS latest
		      WHERE latest.attempt_id = attempt.id
		  )
		 WHERE attempt.logical_request_id = ?
		 ORDER BY attempt.attempt_number`,
		requestID,
	)
	if err != nil {
		return ProviderRequestAccountingSummary{}, classify("summarize provider request accounting", err)
	}
	defer rows.Close()
	summary := ProviderRequestAccountingSummary{
		RequestID: requestID, AccountingComplete: true,
		Usage: domain.TokenUsage{Known: true},
	}
	usageComplete := true
	var totalCost *big.Rat
	var costCurrency domain.CurrencyCode
	for rows.Next() {
		var (
			attemptID       string
			partial         int
			usageKnown      sql.NullInt64
			input           sql.NullInt64
			cachedInput     sql.NullInt64
			cacheWrite      sql.NullInt64
			output          sql.NullInt64
			reasoning       sql.NullInt64
			providerJSON    sql.NullString
			costKnown       sql.NullInt64
			costNumerator   sql.NullInt64
			costDenominator sql.NullInt64
			currency        sql.NullString
			discrepancy     sql.NullInt64
		)
		if err := rows.Scan(
			&attemptID, &partial, &usageKnown, &input, &cachedInput,
			&cacheWrite, &output, &reasoning, &providerJSON, &costKnown,
			&costNumerator, &costDenominator, &currency, &discrepancy,
		); err != nil {
			return ProviderRequestAccountingSummary{}, classify("scan provider request accounting", err)
		}
		_ = attemptID
		summary.AttemptCount++
		summary.PartialStreamObserved = summary.PartialStreamObserved || partial != 0
		if !usageKnown.Valid || usageKnown.Int64 == 0 ||
			!costKnown.Valid || costKnown.Int64 == 0 {
			summary.AccountingComplete = false
		}
		if discrepancy.Valid && discrepancy.Int64 != 0 {
			summary.Discrepancy = true
		}
		if usageKnown.Valid && usageKnown.Int64 != 0 {
			if err := addUsageToSummary(
				&summary.Usage, input.Int64, cachedInput.Int64,
				cacheWrite.Int64, output.Int64, reasoning.Int64,
				providerJSON.String,
			); err != nil {
				return ProviderRequestAccountingSummary{}, err
			}
		} else {
			usageComplete = false
		}
		if costKnown.Valid && costKnown.Int64 != 0 {
			parsedCurrency, err := domain.ParseCurrencyCode(currency.String)
			if err != nil {
				return ProviderRequestAccountingSummary{}, fmt.Errorf("scan provider cost currency: %w", err)
			}
			if totalCost == nil {
				totalCost = new(big.Rat)
				costCurrency = parsedCurrency
			} else if parsedCurrency != costCurrency {
				return ProviderRequestAccountingSummary{}, errors.New("provider request accounting spans multiple currencies")
			}
			totalCost.Add(
				totalCost,
				new(big.Rat).SetFrac(
					big.NewInt(costNumerator.Int64),
					big.NewInt(costDenominator.Int64),
				),
			)
		}
	}
	if err := rows.Err(); err != nil {
		return ProviderRequestAccountingSummary{}, classify("iterate provider request accounting", err)
	}
	if summary.AttemptCount == 0 {
		if _, err := repositories.GetProviderLogicalRequest(ctx, requestID); err != nil {
			return ProviderRequestAccountingSummary{}, err
		}
		summary.AccountingComplete = false
		summary.Usage.Known = false
		return summary, nil
	}
	if !usageComplete {
		summary.Usage = domain.TokenUsage{}
	}
	if summary.AccountingComplete && totalCost != nil {
		if !totalCost.Num().IsInt64() || !totalCost.Denom().IsInt64() {
			return ProviderRequestAccountingSummary{}, errors.New("provider request cost exceeds exact storage range")
		}
		summary.Cost = &ExactMinorCost{
			Numerator: totalCost.Num().Int64(), Denominator: totalCost.Denom().Int64(),
			Currency: costCurrency,
		}
	}
	return summary, nil
}

type accountingValues struct {
	usageKnown       int
	input            any
	cachedInput      any
	cacheWrite       any
	output           any
	reasoning        any
	providerSpecific any
	costKnown        int
	costNumerator    any
	costDenominator  any
	currency         any
}

func accountingSQLValues(input AppendProviderAttemptAccounting) (accountingValues, error) {
	values := accountingValues{}
	if input.Usage.Known {
		values.usageKnown = 1
		values.input = int64(input.Usage.Input)
		values.cachedInput = int64(input.Usage.CachedInput)
		values.cacheWrite = int64(input.Usage.CacheWrite)
		values.output = int64(input.Usage.Output)
		values.reasoning = int64(input.Usage.Reasoning)
		if len(input.Usage.ProviderSpecific) != 0 {
			encoded, err := json.Marshal(input.Usage.ProviderSpecific)
			if err != nil {
				return accountingValues{}, fmt.Errorf("encode provider-specific usage: %w", err)
			}
			values.providerSpecific = string(encoded)
		}
	}
	if input.Cost != nil {
		normalized, err := normalizeExactCost(*input.Cost)
		if err != nil {
			return accountingValues{}, err
		}
		values.costKnown = 1
		values.costNumerator = normalized.Numerator
		values.costDenominator = normalized.Denominator
		values.currency = string(normalized.Currency)
	}
	return values, nil
}

func validateProviderConfigurationRevision(input CreateProviderConfigurationRevision) error {
	if input.ProviderID.IsZero() {
		return errors.New("provider ID must not be empty")
	}
	if err := validateBounded("configuration revision ID", input.ID, 255); err != nil {
		return err
	}
	if err := validateBounded("adapter name", input.AdapterName, 255); err != nil {
		return err
	}
	if err := validateBounded("adapter version", input.AdapterVersion, 255); err != nil {
		return err
	}
	if err := validateBounded("provider version", input.ProviderVersion, 255); err != nil {
		return err
	}
	if err := validateBounded("redacted endpoint", input.EndpointRedacted, 2048); err != nil {
		return err
	}
	if err := validateJSONBounded(input.CapabilitiesJSON, 65536); err != nil {
		return fmt.Errorf("provider capabilities: %w", err)
	}
	if !validSHA256(input.ContentSHA256) {
		return errors.New("provider configuration content hash is invalid")
	}
	if err := validateBounded("idempotency key", input.IdempotencyKey, 255); err != nil {
		return err
	}
	if input.ApprovalReference != nil {
		if err := validateBounded("approval reference", *input.ApprovalReference, 255); err != nil {
			return err
		}
	}
	return nil
}

func validateProviderPricingRevision(input CreateProviderPricingRevision) error {
	if err := validateBounded("pricing revision ID", input.ID, 255); err != nil {
		return err
	}
	if input.ProviderID.IsZero() {
		return errors.New("provider ID must not be empty")
	}
	if err := validateBounded("model identifier", input.ModelIdentifier, 255); err != nil {
		return err
	}
	if err := validateBounded("model version", input.ModelVersion, 255); err != nil {
		return err
	}
	if input.EffectiveAt.IsZero() {
		return errors.New("pricing effective time is required")
	}
	if input.PricingKnown {
		if input.Currency == nil {
			return errors.New("known pricing requires a currency")
		}
		if _, err := domain.ParseCurrencyCode(string(*input.Currency)); err != nil {
			return err
		}
		if len(input.Components) == 0 {
			return errors.New("known pricing requires at least one component")
		}
		if len(input.Components) > maximumProviderPriceComponents {
			return errors.New("provider pricing has too many components")
		}
	} else if input.Currency != nil || len(input.Components) != 0 {
		return errors.New("unknown pricing must not carry currency or components")
	}
	if input.SourceRedacted != nil {
		if err := validateBounded("redacted pricing source", *input.SourceRedacted, 2048); err != nil {
			return err
		}
	}
	seen := make(map[string]struct{}, len(input.Components))
	for _, component := range input.Components {
		if !providerUsageKindValid(component.UsageKind) ||
			component.MinorNumerator < 0 || component.TokenDenominator <= 0 {
			return errors.New("provider price component is invalid")
		}
		key := component.UsageKind
		if component.UsageKind == "provider-specific" {
			if component.ProviderSpecificKind == nil {
				return errors.New("provider-specific price component requires a category")
			}
			if err := validateBounded("provider-specific price category", *component.ProviderSpecificKind, 255); err != nil {
				return err
			}
			key += "\x00" + *component.ProviderSpecificKind
		} else if component.ProviderSpecificKind != nil {
			return errors.New("standard price component must not carry a provider-specific category")
		}
		if _, duplicate := seen[key]; duplicate {
			return errors.New("provider price component is duplicated")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validatePlanProviderLogicalRequest(input PlanProviderLogicalRequest) error {
	if input.ID.IsZero() || input.TaskID.IsZero() || input.ProviderID.IsZero() {
		return errors.New("logical request, task, and provider IDs are required")
	}
	if input.RunID != nil && input.RunID.IsZero() {
		return errors.New("run ID must not be empty")
	}
	for label, value := range map[string]string{
		"provider configuration revision ID": input.ProviderConfigurationRevisionID,
		"adapter name":                       input.AdapterName,
		"adapter version":                    input.AdapterVersion,
		"provider version":                   input.ProviderVersion,
		"model identifier":                   input.ModelIdentifier,
		"model version":                      input.ModelVersion,
		"idempotency key":                    input.IdempotencyKey,
	} {
		if err := validateBounded(label, value, 255); err != nil {
			return err
		}
	}
	if input.PricingRevisionID == nil {
		return errors.New(
			"logical provider request requires an explicit pricing revision",
		)
	}
	if err := validateBounded(
		"pricing revision ID",
		*input.PricingRevisionID,
		255,
	); err != nil {
		return err
	}
	if !validSHA256(input.RequestSHA256) {
		return errors.New("logical request hash is invalid")
	}
	return nil
}

func validateCreateProviderRequestAttempt(input CreateProviderRequestAttempt) error {
	if err := validateBounded("provider request attempt ID", input.ID, 255); err != nil {
		return err
	}
	if input.LogicalRequestID.IsZero() || input.AttemptNumber == 0 ||
		input.AttemptNumber > maximumProviderRequestAttempts {
		return errors.New("logical request ID and bounded positive attempt number are required")
	}
	if input.RequestIdempotencyKey != nil {
		return validateBounded("provider request idempotency key", *input.RequestIdempotencyKey, 255)
	}
	return nil
}

func validateProviderRequestAttemptTransition(input TransitionProviderRequestAttempt) error {
	if err := validateBounded("provider request attempt ID", input.ID, 255); err != nil {
		return err
	}
	if !providerAttemptTransitionAllowed(input.From, input.To) ||
		!providerEffectStatusValid(input.EffectStatus) ||
		input.ObservedAt.IsZero() ||
		input.ObservedAt.Location() != time.UTC {
		return errors.New("provider request attempt transition is invalid")
	}
	if input.ProviderRequestIDRedacted != nil {
		if err := validateBounded("redacted provider request ID", *input.ProviderRequestIDRedacted, 512); err != nil {
			return err
		}
	}
	if input.ErrorClass != nil && !providerErrorClassValid(*input.ErrorClass) {
		return errors.New("provider error class is invalid")
	}
	if input.RetryAfterMillis != nil && *input.RetryAfterMillis < 0 {
		return errors.New("provider retry-after must not be negative")
	}
	if input.SafeMetadataJSON != nil {
		if err := validateJSONBounded(*input.SafeMetadataJSON, 65536); err != nil {
			return fmt.Errorf("safe provider metadata: %w", err)
		}
	}
	if !input.FirstResponseAt.IsZero() &&
		(input.FirstResponseAt.Location() != time.UTC ||
			input.FirstResponseAt.After(input.ObservedAt)) {
		return errors.New("provider first-response timestamp must be UTC and not after observation")
	}
	if input.To == ProviderRequestAttemptSucceeded &&
		(input.ErrorClass != nil || input.Retryable) {
		return errors.New("successful provider attempt must not carry an error")
	}
	return nil
}

func validateProviderAttemptAccounting(input AppendProviderAttemptAccounting) error {
	if err := validateBounded("provider attempt accounting ID", input.ID, 255); err != nil {
		return err
	}
	if err := validateBounded("provider request attempt ID", input.AttemptID, 255); err != nil {
		return err
	}
	if input.Sequence == 0 || input.Sequence > maximumProviderAccountingRecords ||
		!slices.Contains(
			[]string{"estimated", "provider-partial", "provider-final", "reconciled"},
			input.Source,
		) {
		return errors.New("provider attempt accounting sequence or source is invalid")
	}
	if err := input.Usage.Validate(); err != nil {
		return err
	}
	if !tokenUsageFitsSQLite(input.Usage) {
		return errors.New("provider usage exceeds SQLite integer range")
	}
	if input.Source == "provider-partial" && !input.Partial ||
		input.Source == "provider-final" && input.Partial {
		return errors.New("provider accounting partial flag disagrees with source")
	}
	if input.Cost != nil {
		if input.PricingRevisionID == nil {
			return errors.New("known provider cost requires a pricing revision")
		}
		if _, err := normalizeExactCost(*input.Cost); err != nil {
			return err
		}
	} else if input.PricingRevisionID != nil && strings.TrimSpace(*input.PricingRevisionID) == "" {
		return errors.New("pricing revision ID must not be empty")
	}
	if input.DiscrepancyRedacted != nil {
		if err := validateBounded("redacted accounting discrepancy", *input.DiscrepancyRedacted, 2048); err != nil {
			return err
		}
	}
	if err := validateJSONBounded(input.ProvenanceJSON, 65536); err != nil {
		return fmt.Errorf("provider accounting provenance: %w", err)
	}
	return nil
}

func validateProviderAttemptEvidence(input AppendProviderAttemptEvidence) error {
	if err := validateBounded("provider attempt evidence ID", input.ID, 255); err != nil {
		return err
	}
	if err := validateBounded("provider request attempt ID", input.AttemptID, 255); err != nil {
		return err
	}
	if input.Sequence == 0 || input.Sequence > maximumProviderEvidenceRecords ||
		!slices.Contains(
			[]string{"partial-output", "partial-tool-call", "effect-observed", "late-response-discarded"},
			input.Kind,
		) {
		return errors.New("provider attempt evidence sequence or kind is invalid")
	}
	if strings.HasPrefix(input.Kind, "partial-") && input.Final {
		return errors.New("partial provider evidence cannot be final")
	}
	if !validSHA256(input.ContentSHA256) {
		return errors.New("provider attempt evidence content hash is invalid")
	}
	if err := validateBounded("redacted provider evidence summary", input.SummaryRedacted, 4096); err != nil {
		return err
	}
	if input.ByteCount < 0 {
		return errors.New("provider attempt evidence byte count must not be negative")
	}
	return nil
}

func normalizeExactCost(cost ExactMinorCost) (ExactMinorCost, error) {
	if cost.Numerator < 0 || cost.Denominator <= 0 {
		return ExactMinorCost{}, errors.New("exact provider cost must be non-negative with a positive denominator")
	}
	currency, err := domain.ParseCurrencyCode(string(cost.Currency))
	if err != nil {
		return ExactMinorCost{}, err
	}
	divisor := greatestCommonDivisor(cost.Numerator, cost.Denominator)
	return ExactMinorCost{
		Numerator:   cost.Numerator / divisor,
		Denominator: cost.Denominator / divisor,
		Currency:    currency,
	}, nil
}

func greatestCommonDivisor(left, right int64) int64 {
	for right != 0 {
		left, right = right, left%right
	}
	if left == 0 {
		return 1
	}
	return left
}

func providerLogicalTransitionAllowed(from, to ProviderLogicalRequestState) bool {
	switch from {
	case ProviderLogicalRequestPlanned:
		return to == ProviderLogicalRequestInFlight ||
			to == ProviderLogicalRequestCancelled
	case ProviderLogicalRequestInFlight:
		return providerLogicalTerminal(to)
	default:
		return false
	}
}

func providerLogicalTerminal(state ProviderLogicalRequestState) bool {
	return slices.Contains([]ProviderLogicalRequestState{
		ProviderLogicalRequestSucceeded, ProviderLogicalRequestFailed,
		ProviderLogicalRequestCancelled, ProviderLogicalRequestOutcomeUnknown,
		ProviderLogicalRequestRetryExhausted,
	}, state)
}

func providerAttemptTransitionAllowed(from, to ProviderRequestAttemptState) bool {
	switch from {
	case ProviderRequestAttemptPrepared:
		return to == ProviderRequestAttemptStarted ||
			to == ProviderRequestAttemptCancelled
	case ProviderRequestAttemptStarted:
		return to == ProviderRequestAttemptStreaming ||
			providerAttemptTerminal(to)
	case ProviderRequestAttemptStreaming:
		return providerAttemptTerminal(to)
	default:
		return false
	}
}

func providerAttemptTerminal(state ProviderRequestAttemptState) bool {
	return slices.Contains([]ProviderRequestAttemptState{
		ProviderRequestAttemptSucceeded, ProviderRequestAttemptFailed,
		ProviderRequestAttemptCancelled, ProviderRequestAttemptOutcomeUnknown,
	}, state)
}

func providerAccountingStatusValid(status ProviderAccountingStatus) bool {
	return slices.Contains([]ProviderAccountingStatus{
		ProviderAccountingUnknown, ProviderAccountingEstimated,
		ProviderAccountingProviderReported, ProviderAccountingReconciled,
		ProviderAccountingDiscrepant,
	}, status)
}

func providerEffectStatusValid(status ProviderRequestEffectStatus) bool {
	return slices.Contains([]ProviderRequestEffectStatus{
		ProviderRequestEffectNone, ProviderRequestEffectPossible,
		ProviderRequestEffectConfirmed,
	}, status)
}

func providerEffectStatusRank(status ProviderRequestEffectStatus) int {
	switch status {
	case ProviderRequestEffectNone:
		return 0
	case ProviderRequestEffectPossible:
		return 1
	case ProviderRequestEffectConfirmed:
		return 2
	default:
		return -1
	}
}

func providerErrorClassValid(class string) bool {
	return slices.Contains([]string{
		"timeout", "retryable", "rate-limit", "authentication",
		"invalid-request", "safety", "unavailable", "cancelled", "unknown",
	}, class)
}

func providerUsageKindValid(kind string) bool {
	return slices.Contains([]string{
		"input", "cached-input", "cache-write", "output", "reasoning",
		"provider-specific",
	}, kind)
}

func findProviderConfigurationByIdempotency(
	ctx context.Context,
	queries queryRower,
	providerID domain.ProviderID,
	idempotencyKey string,
) (ProviderConfigurationRevision, bool, error) {
	var row ProviderConfigurationRevision
	var created int64
	var approval sql.NullString
	err := queries.QueryRowContext(
		ctx,
		`SELECT id, provider_id, revision, adapter_name, adapter_version,
		        provider_version, endpoint_redacted, capabilities_json, content_sha256,
		        approval_reference, idempotency_key, created_at_unix_micros
		 FROM provider_configuration_revisions
		 WHERE provider_id = ? AND idempotency_key = ?`,
		providerID, idempotencyKey,
	).Scan(
		&row.ID, &row.ProviderID, &row.Revision, &row.AdapterName,
		&row.AdapterVersion, &row.ProviderVersion,
		&row.EndpointRedacted, &row.CapabilitiesJSON,
		&row.ContentSHA256, &approval, &row.IdempotencyKey, &created,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderConfigurationRevision{}, false, nil
	}
	if err != nil {
		return ProviderConfigurationRevision{}, false, classify("find provider configuration revision", err)
	}
	row.ApprovalReference = nullStringPointer(approval)
	row.CreatedAt = repositoryTime(created)
	return row, true, nil
}

func findProviderPricingRevision(
	ctx context.Context,
	queries providerQueryer,
	id string,
) (ProviderPricingRevision, bool, error) {
	var row ProviderPricingRevision
	var currency, source sql.NullString
	var known int
	var effective, created int64
	err := queries.QueryRowContext(
		ctx,
		`SELECT id, provider_id, model_identifier, model_version, currency,
		        pricing_known, source_redacted, effective_at_unix_micros,
		        created_at_unix_micros
		 FROM provider_pricing_revisions WHERE id = ?`,
		id,
	).Scan(
		&row.ID, &row.ProviderID, &row.ModelIdentifier, &row.ModelVersion,
		&currency, &known, &source, &effective, &created,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderPricingRevision{}, false, nil
	}
	if err != nil {
		return ProviderPricingRevision{}, false, classify("find provider pricing revision", err)
	}
	row.PricingKnown = known != 0
	if currency.Valid {
		parsed, err := domain.ParseCurrencyCode(currency.String)
		if err != nil {
			return ProviderPricingRevision{}, false, err
		}
		row.Currency = &parsed
	}
	row.SourceRedacted = nullStringPointer(source)
	row.EffectiveAt = repositoryTime(effective)
	row.CreatedAt = repositoryTime(created)
	rows, err := queries.QueryContext(
		ctx,
		`SELECT usage_kind, provider_specific_kind, minor_numerator,
		        token_denominator
		 FROM provider_price_components WHERE pricing_revision_id = ?
		 ORDER BY usage_kind, provider_specific_kind`,
		id,
	)
	if err != nil {
		return ProviderPricingRevision{}, false, classify("list provider price components", err)
	}
	defer rows.Close()
	for rows.Next() {
		var component ProviderPriceComponent
		var category sql.NullString
		if err := rows.Scan(
			&component.UsageKind, &category, &component.MinorNumerator,
			&component.TokenDenominator,
		); err != nil {
			return ProviderPricingRevision{}, false, classify("scan provider price component", err)
		}
		component.ProviderSpecificKind = nullStringPointer(category)
		row.Components = append(row.Components, component)
	}
	if err := rows.Err(); err != nil {
		return ProviderPricingRevision{}, false, classify("iterate provider price components", err)
	}
	return row, true, nil
}

func findProviderLogicalRequestByIdempotency(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	idempotencyKey string,
) (ProviderLogicalRequest, bool, error) {
	var id domain.ModelRequestID
	if err := queries.QueryRowContext(
		ctx,
		`SELECT id FROM provider_logical_requests
		 WHERE task_id = ? AND idempotency_key = ?`,
		taskID, idempotencyKey,
	).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return ProviderLogicalRequest{}, false, nil
	} else if err != nil {
		return ProviderLogicalRequest{}, false, classify("find provider logical request", err)
	}
	row, err := getProviderLogicalRequest(ctx, queries, id)
	return row, err == nil, err
}

func getProviderLogicalRequest(
	ctx context.Context,
	queries queryRower,
	id domain.ModelRequestID,
) (ProviderLogicalRequest, error) {
	var row ProviderLogicalRequest
	var runID, pricing sql.NullString
	var started, completed sql.NullInt64
	var created, updated int64
	err := queries.QueryRowContext(
		ctx,
		`SELECT id, task_id, run_id, provider_id,
		        provider_configuration_revision_id, adapter_name,
		        adapter_version, provider_version, model_identifier, model_version,
		        pricing_revision_id, state, request_sha256, idempotency_key,
		        accounting_status, started_at_unix_micros,
		        completed_at_unix_micros, created_at_unix_micros,
		        updated_at_unix_micros, revision
		 FROM provider_logical_requests WHERE id = ?`,
		id,
	).Scan(
		&row.ID, &row.TaskID, &runID, &row.ProviderID,
		&row.ProviderConfigurationRevisionID, &row.AdapterName,
		&row.AdapterVersion, &row.ProviderVersion,
		&row.ModelIdentifier, &row.ModelVersion,
		&pricing, &row.State, &row.RequestSHA256, &row.IdempotencyKey,
		&row.AccountingStatus, &started, &completed, &created, &updated,
		&row.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderLogicalRequest{}, typedError(ErrNotFound, "get provider logical request", err)
	}
	if err != nil {
		return ProviderLogicalRequest{}, classify("get provider logical request", err)
	}
	if runID.Valid {
		parsed, err := domain.ParseRunID(runID.String)
		if err != nil {
			return ProviderLogicalRequest{}, err
		}
		row.RunID = &parsed
	}
	row.PricingRevisionID = nullStringPointer(pricing)
	row.StartedAt = nullTimePointer(started)
	row.CompletedAt = nullTimePointer(completed)
	row.CreatedAt = repositoryTime(created)
	row.UpdatedAt = repositoryTime(updated)
	return row, nil
}

func findProviderRequestAttemptByNumber(
	ctx context.Context,
	queries queryRower,
	requestID domain.ModelRequestID,
	number uint64,
) (ProviderRequestAttempt, bool, error) {
	var id string
	if err := queries.QueryRowContext(
		ctx,
		`SELECT id FROM provider_request_attempts
		 WHERE logical_request_id = ? AND attempt_number = ?`,
		requestID, number,
	).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return ProviderRequestAttempt{}, false, nil
	} else if err != nil {
		return ProviderRequestAttempt{}, false, classify("find provider request attempt", err)
	}
	row, err := getProviderRequestAttempt(ctx, queries, id)
	return row, err == nil, err
}

func getProviderRequestAttempt(
	ctx context.Context,
	queries queryRower,
	id string,
) (ProviderRequestAttempt, error) {
	var row ProviderRequestAttempt
	var providerRequestID, idempotencyKey, errorClass, metadata sql.NullString
	var retryAfter, started, firstResponse, completed sql.NullInt64
	var partial, retryable int
	var created, updated int64
	err := queries.QueryRowContext(
		ctx,
		`SELECT id, logical_request_id, attempt_number, state,
		        provider_request_id_redacted, request_idempotency_key,
		        effect_status, partial_stream_observed, error_class, retryable,
		        retry_after_millis, safe_metadata_json,
		        started_at_unix_micros, first_response_at_unix_micros,
		        completed_at_unix_micros, created_at_unix_micros,
		        updated_at_unix_micros, revision
		 FROM provider_request_attempts WHERE id = ?`,
		id,
	).Scan(
		&row.ID, &row.LogicalRequestID, &row.AttemptNumber, &row.State,
		&providerRequestID, &idempotencyKey, &row.EffectStatus, &partial,
		&errorClass, &retryable, &retryAfter, &metadata, &started,
		&firstResponse, &completed, &created, &updated, &row.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderRequestAttempt{}, typedError(ErrNotFound, "get provider request attempt", err)
	}
	if err != nil {
		return ProviderRequestAttempt{}, classify("get provider request attempt", err)
	}
	row.ProviderRequestIDRedacted = nullStringPointer(providerRequestID)
	row.RequestIdempotencyKey = nullStringPointer(idempotencyKey)
	row.PartialStreamObserved = partial != 0
	row.ErrorClass = nullStringPointer(errorClass)
	row.Retryable = retryable != 0
	row.RetryAfterMillis = nullInt64Pointer(retryAfter)
	row.SafeMetadataJSON = nullStringPointer(metadata)
	row.StartedAt = nullTimePointer(started)
	row.FirstResponseAt = nullTimePointer(firstResponse)
	row.CompletedAt = nullTimePointer(completed)
	row.CreatedAt = repositoryTime(created)
	row.UpdatedAt = repositoryTime(updated)
	return row, nil
}

func findProviderAttemptAccountingByID(
	ctx context.Context,
	queries queryRower,
	id string,
) (ProviderAttemptAccounting, bool, error) {
	var row ProviderAttemptAccounting
	var (
		usageKnown, costKnown, discrepancy, partial       int
		input, cachedInput, cacheWrite, output, reasoning sql.NullInt64
		providerJSON, pricing, currency, discrepancyText  sql.NullString
		costNumerator, costDenominator                    sql.NullInt64
		created                                           int64
	)
	err := queries.QueryRowContext(
		ctx,
		`SELECT id, attempt_id, sequence, source, usage_known,
		        input_tokens, cached_input_tokens, cache_write_tokens,
		        output_tokens, reasoning_tokens, provider_specific_json,
		        pricing_revision_id, cost_known, cost_minor_numerator,
		        cost_minor_denominator, currency, discrepancy,
		        discrepancy_redacted, partial, provenance_json,
		        created_at_unix_micros
		 FROM provider_attempt_accounting WHERE id = ?`,
		id,
	).Scan(
		&row.ID, &row.AttemptID, &row.Sequence, &row.Source, &usageKnown,
		&input, &cachedInput, &cacheWrite, &output, &reasoning,
		&providerJSON, &pricing, &costKnown, &costNumerator,
		&costDenominator, &currency, &discrepancy, &discrepancyText,
		&partial, &row.ProvenanceJSON, &created,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderAttemptAccounting{}, false, nil
	}
	if err != nil {
		return ProviderAttemptAccounting{}, false, classify("find provider attempt accounting", err)
	}
	row.Usage.Known = usageKnown != 0
	if row.Usage.Known {
		row.Usage.Input = domain.TokenCount(input.Int64)
		row.Usage.CachedInput = domain.TokenCount(cachedInput.Int64)
		row.Usage.CacheWrite = domain.TokenCount(cacheWrite.Int64)
		row.Usage.Output = domain.TokenCount(output.Int64)
		row.Usage.Reasoning = domain.TokenCount(reasoning.Int64)
		if providerJSON.Valid {
			if err := json.Unmarshal([]byte(providerJSON.String), &row.Usage.ProviderSpecific); err != nil {
				return ProviderAttemptAccounting{}, false, fmt.Errorf("decode provider-specific usage: %w", err)
			}
		}
	}
	row.PricingRevisionID = nullStringPointer(pricing)
	if costKnown != 0 {
		parsed, err := domain.ParseCurrencyCode(currency.String)
		if err != nil {
			return ProviderAttemptAccounting{}, false, err
		}
		row.Cost = &ExactMinorCost{
			Numerator: costNumerator.Int64, Denominator: costDenominator.Int64,
			Currency: parsed,
		}
	}
	if discrepancy != 0 {
		row.DiscrepancyRedacted = nullStringPointer(discrepancyText)
	}
	row.Partial = partial != 0
	row.CreatedAt = repositoryTime(created)
	return row, true, nil
}

func findProviderAttemptEvidenceByID(
	ctx context.Context,
	queries queryRower,
	id string,
) (ProviderAttemptEvidence, bool, error) {
	var row ProviderAttemptEvidence
	var final int
	var created int64
	err := queries.QueryRowContext(
		ctx,
		`SELECT id, attempt_id, sequence, kind, final, content_sha256,
		        summary_redacted, byte_count, created_at_unix_micros
		 FROM provider_attempt_evidence WHERE id = ?`,
		id,
	).Scan(
		&row.ID, &row.AttemptID, &row.Sequence, &row.Kind, &final,
		&row.ContentSHA256, &row.SummaryRedacted, &row.ByteCount, &created,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderAttemptEvidence{}, false, nil
	}
	if err != nil {
		return ProviderAttemptEvidence{}, false, classify("find provider attempt evidence", err)
	}
	row.Final = final != 0
	row.CreatedAt = repositoryTime(created)
	return row, true, nil
}

func providerConfigurationMatches(
	existing ProviderConfigurationRevision,
	input CreateProviderConfigurationRevision,
) bool {
	return existing.ID == input.ID &&
		existing.Revision == input.ExpectedLatestRevision+1 &&
		existing.AdapterName == input.AdapterName &&
		existing.AdapterVersion == input.AdapterVersion &&
		existing.ProviderVersion == input.ProviderVersion &&
		existing.EndpointRedacted == input.EndpointRedacted &&
		existing.CapabilitiesJSON == input.CapabilitiesJSON &&
		existing.ContentSHA256 == input.ContentSHA256 &&
		equalOptionalString(existing.ApprovalReference, input.ApprovalReference)
}

func providerPricingMatches(
	existing ProviderPricingRevision,
	input CreateProviderPricingRevision,
) bool {
	return existing.ID == input.ID && existing.ProviderID == input.ProviderID &&
		existing.ModelIdentifier == input.ModelIdentifier &&
		existing.ModelVersion == input.ModelVersion &&
		existing.PricingKnown == input.PricingKnown &&
		equalOptionalCurrency(existing.Currency, input.Currency) &&
		equalOptionalString(existing.SourceRedacted, input.SourceRedacted) &&
		existing.EffectiveAt.Equal(input.EffectiveAt.UTC()) &&
		slices.EqualFunc(
			existing.Components, normalizedPriceComponents(input.Components),
			func(left, right ProviderPriceComponent) bool {
				return left.UsageKind == right.UsageKind &&
					equalOptionalString(left.ProviderSpecificKind, right.ProviderSpecificKind) &&
					left.MinorNumerator == right.MinorNumerator &&
					left.TokenDenominator == right.TokenDenominator
			},
		)
}

func providerLogicalRequestMatches(
	existing ProviderLogicalRequest,
	input PlanProviderLogicalRequest,
) bool {
	return existing.ID == input.ID &&
		equalOptionalRunID(existing.RunID, input.RunID) &&
		existing.ProviderID == input.ProviderID &&
		existing.ProviderConfigurationRevisionID == input.ProviderConfigurationRevisionID &&
		existing.AdapterName == input.AdapterName &&
		existing.AdapterVersion == input.AdapterVersion &&
		existing.ProviderVersion == input.ProviderVersion &&
		existing.ModelIdentifier == input.ModelIdentifier &&
		existing.ModelVersion == input.ModelVersion &&
		equalOptionalString(existing.PricingRevisionID, input.PricingRevisionID) &&
		existing.RequestSHA256 == input.RequestSHA256
}

func providerAttemptAccountingMatches(
	existing ProviderAttemptAccounting,
	input AppendProviderAttemptAccounting,
) bool {
	return existing.AttemptID == input.AttemptID &&
		existing.Sequence == input.Sequence && existing.Source == input.Source &&
		tokenUsageEqual(existing.Usage, input.Usage) &&
		equalOptionalString(existing.PricingRevisionID, input.PricingRevisionID) &&
		exactCostEqual(existing.Cost, input.Cost) &&
		equalOptionalString(existing.DiscrepancyRedacted, input.DiscrepancyRedacted) &&
		existing.Partial == input.Partial &&
		existing.ProvenanceJSON == input.ProvenanceJSON
}

func providerAttemptEvidenceMatches(
	existing ProviderAttemptEvidence,
	input AppendProviderAttemptEvidence,
) bool {
	return existing.AttemptID == input.AttemptID &&
		existing.Sequence == input.Sequence && existing.Kind == input.Kind &&
		existing.Final == input.Final && existing.ContentSHA256 == input.ContentSHA256 &&
		existing.SummaryRedacted == input.SummaryRedacted &&
		existing.ByteCount == input.ByteCount
}

func providerAttemptTransitionMatches(
	current ProviderRequestAttempt,
	input TransitionProviderRequestAttempt,
) bool {
	if current.State != input.To || current.EffectStatus != input.EffectStatus ||
		current.Retryable != input.Retryable ||
		!equalOptionalString(current.ErrorClass, input.ErrorClass) ||
		!equalOptionalInt64(current.RetryAfterMillis, input.RetryAfterMillis) ||
		input.PartialStreamObserved && !current.PartialStreamObserved {
		return false
	}
	if input.ProviderRequestIDRedacted != nil &&
		!equalOptionalString(
			current.ProviderRequestIDRedacted,
			input.ProviderRequestIDRedacted,
		) {
		return false
	}
	if !input.FirstResponseAt.IsZero() &&
		(current.FirstResponseAt == nil ||
			current.FirstResponseAt.UnixMicro() != input.FirstResponseAt.UnixMicro()) {
		return false
	}
	observedMicros := input.ObservedAt.UTC().UnixMicro()
	switch {
	case input.To == ProviderRequestAttemptStarted:
		if current.StartedAt == nil ||
			current.StartedAt.UnixMicro() != observedMicros {
			return false
		}
	case input.To == ProviderRequestAttemptStreaming &&
		input.FirstResponseAt.IsZero():
		if current.FirstResponseAt == nil ||
			current.FirstResponseAt.UnixMicro() != observedMicros {
			return false
		}
	case providerAttemptTerminal(input.To):
		if current.CompletedAt == nil ||
			current.CompletedAt.UnixMicro() != observedMicros {
			return false
		}
	}
	return input.SafeMetadataJSON == nil ||
		equalOptionalString(current.SafeMetadataJSON, input.SafeMetadataJSON)
}

func normalizedPriceComponents(input []ProviderPriceComponent) []ProviderPriceComponent {
	output := slices.Clone(input)
	for index := range output {
		divisor := greatestCommonDivisor(
			output[index].MinorNumerator,
			output[index].TokenDenominator,
		)
		output[index].MinorNumerator /= divisor
		output[index].TokenDenominator /= divisor
		output[index].ProviderSpecificKind = cloneString(
			output[index].ProviderSpecificKind,
		)
	}
	slices.SortFunc(output, func(left, right ProviderPriceComponent) int {
		leftKey := left.UsageKind
		rightKey := right.UsageKind
		if left.ProviderSpecificKind != nil {
			leftKey += "\x00" + *left.ProviderSpecificKind
		}
		if right.ProviderSpecificKind != nil {
			rightKey += "\x00" + *right.ProviderSpecificKind
		}
		return strings.Compare(leftKey, rightKey)
	})
	return output
}

func calculateProviderUsageCost(
	pricing ProviderPricingRevision,
	usage domain.TokenUsage,
) (ExactMinorCost, error) {
	if !pricing.PricingKnown || pricing.Currency == nil || !usage.Known {
		return ExactMinorCost{}, errors.New("known pricing and usage are required to calculate provider cost")
	}
	components := make(map[string]ProviderPriceComponent, len(pricing.Components))
	for _, component := range pricing.Components {
		key := component.UsageKind
		if component.ProviderSpecificKind != nil {
			key += "\x00" + *component.ProviderSpecificKind
		}
		components[key] = component
	}
	counts := map[string]domain.TokenCount{
		"input":        usage.Input,
		"cached-input": usage.CachedInput,
		"cache-write":  usage.CacheWrite,
		"output":       usage.Output,
		"reasoning":    usage.Reasoning,
	}
	for key, count := range usage.ProviderSpecific {
		counts["provider-specific\x00"+key] = count
	}
	total := new(big.Rat)
	for key, count := range counts {
		if count == 0 {
			continue
		}
		component, found := components[key]
		if !found {
			return ExactMinorCost{}, fmt.Errorf(
				"provider pricing has no component for nonzero usage category %q",
				key,
			)
		}
		total.Add(
			total,
			new(big.Rat).SetFrac(
				new(big.Int).Mul(
					new(big.Int).SetUint64(uint64(count)),
					big.NewInt(component.MinorNumerator),
				),
				big.NewInt(component.TokenDenominator),
			),
		)
	}
	if !total.Num().IsInt64() || !total.Denom().IsInt64() {
		return ExactMinorCost{}, errors.New("calculated provider cost exceeds exact storage range")
	}
	return ExactMinorCost{
		Numerator: total.Num().Int64(), Denominator: total.Denom().Int64(),
		Currency: *pricing.Currency,
	}, nil
}

func tokenUsageFitsSQLite(usage domain.TokenUsage) bool {
	return uint64(usage.Input) <= math.MaxInt64 &&
		uint64(usage.CachedInput) <= math.MaxInt64 &&
		uint64(usage.CacheWrite) <= math.MaxInt64 &&
		uint64(usage.Output) <= math.MaxInt64 &&
		uint64(usage.Reasoning) <= math.MaxInt64
}

func addUsageToSummary(
	total *domain.TokenUsage,
	input, cachedInput, cacheWrite, output, reasoning int64,
	providerJSON string,
) error {
	values := []*domain.TokenCount{
		&total.Input, &total.CachedInput, &total.CacheWrite,
		&total.Output, &total.Reasoning,
	}
	additions := []int64{input, cachedInput, cacheWrite, output, reasoning}
	for index := range values {
		if additions[index] < 0 ||
			uint64(*values[index]) > math.MaxUint64-uint64(additions[index]) {
			return errors.New("provider request token accounting overflow")
		}
		*values[index] += domain.TokenCount(additions[index])
	}
	if providerJSON == "" {
		return nil
	}
	var specific map[string]domain.TokenCount
	if err := json.Unmarshal([]byte(providerJSON), &specific); err != nil {
		return fmt.Errorf("decode provider-specific usage: %w", err)
	}
	if total.ProviderSpecific == nil {
		total.ProviderSpecific = make(map[string]domain.TokenCount)
	}
	for key, count := range specific {
		if total.ProviderSpecific[key] > domain.TokenCount(math.MaxUint64)-count {
			return errors.New("provider-specific token accounting overflow")
		}
		total.ProviderSpecific[key] += count
	}
	return nil
}

func validateJSONBounded(value string, maximum int) error {
	if len(value) < 2 || len(value) > maximum || !json.Valid([]byte(value)) {
		return errors.New("value must be bounded valid JSON")
	}
	return nil
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	output := *value
	return &output
}

func cloneRunID(value *domain.RunID) *domain.RunID {
	if value == nil {
		return nil
	}
	output := *value
	return &output
}

func cloneCurrency(value *domain.CurrencyCode) *domain.CurrencyCode {
	if value == nil {
		return nil
	}
	output := *value
	return &output
}

func cloneExactCost(value *ExactMinorCost) *ExactMinorCost {
	if value == nil {
		return nil
	}
	normalized, err := normalizeExactCost(*value)
	if err != nil {
		output := *value
		return &output
	}
	return &normalized
}

func cloneTokenUsage(value domain.TokenUsage) domain.TokenUsage {
	output := value
	if value.ProviderSpecific != nil {
		output.ProviderSpecific = make(map[string]domain.TokenCount, len(value.ProviderSpecific))
		for key, count := range value.ProviderSpecific {
			output.ProviderSpecific[key] = count
		}
	}
	return output
}

func equalOptionalString(left, right *string) bool {
	return left == nil && right == nil ||
		left != nil && right != nil && *left == *right
}

func equalOptionalRunID(left, right *domain.RunID) bool {
	return left == nil && right == nil ||
		left != nil && right != nil && *left == *right
}

func equalOptionalCurrency(left, right *domain.CurrencyCode) bool {
	return left == nil && right == nil ||
		left != nil && right != nil && *left == *right
}

func equalOptionalInt64(left, right *int64) bool {
	return left == nil && right == nil ||
		left != nil && right != nil && *left == *right
}

func tokenUsageEqual(left, right domain.TokenUsage) bool {
	if left.Known != right.Known || left.Input != right.Input ||
		left.CachedInput != right.CachedInput ||
		left.CacheWrite != right.CacheWrite || left.Output != right.Output ||
		left.Reasoning != right.Reasoning ||
		len(left.ProviderSpecific) != len(right.ProviderSpecific) {
		return false
	}
	for key, count := range left.ProviderSpecific {
		if right.ProviderSpecific[key] != count {
			return false
		}
	}
	return true
}

func exactCostEqual(left, right *ExactMinorCost) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	normalizedLeft, leftErr := normalizeExactCost(*left)
	normalizedRight, rightErr := normalizeExactCost(*right)
	return leftErr == nil && rightErr == nil && normalizedLeft == normalizedRight
}
