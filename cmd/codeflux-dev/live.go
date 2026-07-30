package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/buildinfo"
	"codeflux.dev/codeflux/internal/coordinator"
	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/forecast"
	"codeflux.dev/codeflux/internal/policy"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/providers/anthropic"
	"codeflux.dev/codeflux/internal/providers/openai"
	"codeflux.dev/codeflux/internal/storage"
)

const liveRequestTimeout = 90 * time.Second

type liveGateResult struct {
	SchemaVersion int    `json:"schema_version"`
	Status        string `json:"status"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	ModelRevision string `json:"model_revision"`
	CredentialRef string `json:"credential_reference"`
	Database      string `json:"database"`
	UsageStatus   string `json:"usage_status"`
	CostStatus    string `json:"cost_status"`
	Attempts      uint64 `json:"attempts"`
	LatencyMillis int64  `json:"latency_millis"`
	Warning       string `json:"warning"`
	Reason        string `json:"reason,omitempty"`
}

type liveProviderFactory func(
	*coordinator.ProviderCredentialSource,
	storage.LiveProviderSmokeRequest,
	providers.ModelIdentity,
	providers.ModelCapabilities,
) (providers.ModelProvider, error)

type liveRuntime struct {
	credentialStore credentials.Store
	providerFactory liveProviderFactory
	pricing         *storage.LiveProviderSmokePricing
	budgetFactory   func() (domain.TaskBudget, error)
	now             func() time.Time
}

func defaultLiveRuntime() liveRuntime {
	return liveRuntime{
		credentialStore: credentials.NewPlatformStore(),
		providerFactory: newLiveProvider,
		budgetFactory:   newLiveTaskBudget,
		now:             time.Now,
	}
}

func runLiveGate(
	ctx context.Context,
	stdout,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	return runLiveGateWithRuntime(
		ctx, stdout, stderr, invocation, defaultLiveRuntime(),
	)
}

func runLiveGateWithRuntime(
	ctx context.Context,
	stdout,
	stderr io.Writer,
	invocation commandInvocation,
	runtime liveRuntime,
) int {
	if code := validateCommandRoot("run-live", invocation, stderr); code != exitSuccess {
		return code
	}
	if invocation.Provider == "" || invocation.Model == "" ||
		invocation.ModelRevision == "" || invocation.CredentialRef == "" ||
		invocation.Database == "" {
		fmt.Fprintln(
			stderr,
			"codeflux-dev run-live: --provider, --model, --model-revision, --credential-ref, and --database are all required",
		)
		return exitUsage
	}
	if !validLiveIdentityValue(invocation.Model) ||
		!validLiveIdentityValue(invocation.ModelRevision) {
		fmt.Fprintln(
			stderr,
			"codeflux-dev run-live: --model and --model-revision must be trimmed values of at most 255 bytes",
		)
		return exitUsage
	}
	if invocation.Provider != "openai" && invocation.Provider != "anthropic" {
		fmt.Fprintf(stderr, "codeflux-dev run-live: unsupported explicit provider %q\n", invocation.Provider)
		return exitUsage
	}
	reference, err := parseLiveCredentialReference(invocation.CredentialRef)
	if err != nil || looksLikeProviderSecret(invocation.CredentialRef) {
		fmt.Fprintln(stderr, "codeflux-dev run-live: credential reference must be a non-secret os://service/account reference")
		return exitUsage
	}
	repository, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev run-live: repository: %v\n", err)
		return exitFailure
	}
	databasePath, err := filepath.Abs(invocation.Database)
	if err != nil || !filepath.IsAbs(invocation.Database) {
		fmt.Fprintln(stderr, "codeflux-dev run-live: --database must be an absolute non-test path")
		return exitUsage
	}
	resolvedRepository, err := resolvePathThroughExistingAncestor(repository)
	if err != nil {
		fmt.Fprintln(stderr, "codeflux-dev run-live: cannot resolve repository boundary")
		return exitFailure
	}
	resolvedDatabase, err := resolvePathThroughExistingAncestor(databasePath)
	if err != nil {
		fmt.Fprintln(stderr, "codeflux-dev run-live: cannot resolve database boundary")
		return exitUsage
	}
	relative, err := filepath.Rel(resolvedRepository, resolvedDatabase)
	if err == nil && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		fmt.Fprintln(stderr, "codeflux-dev run-live: --database must be outside the source repository")
		return exitUsage
	}
	if runtime.credentialStore == nil || runtime.providerFactory == nil {
		fmt.Fprintln(stderr, "codeflux-dev run-live: live runtime dependencies are unavailable")
		return exitFailure
	}
	if runtime.now == nil {
		runtime.now = time.Now
	}
	if runtime.budgetFactory == nil {
		runtime.budgetFactory = newLiveTaskBudget
	}

	warning := "LIVE PROVIDER REQUEST CAN INCUR REAL COST; pricing is unknown and will never be displayed as zero"
	if runtime.pricing != nil {
		warning = "LIVE PROVIDER REQUEST CAN INCUR REAL COST; the immutable price snapshot and hard budget will be enforced"
	}
	result := liveGateResult{
		SchemaVersion: 1,
		Status:        "unavailable",
		Provider:      invocation.Provider,
		Model:         invocation.Model,
		ModelRevision: invocation.ModelRevision,
		CredentialRef: invocation.CredentialRef,
		Database:      databasePath,
		UsageStatus:   "unknown",
		CostStatus:    "unknown",
		Warning:       warning,
	}
	if !invocation.JSON {
		fmt.Fprintf(stdout, "WARNING: %s\n", warning)
		fmt.Fprintf(
			stdout,
			"Provider: %s\nModel: %s\nModel revision: %s\nCredential reference: %s\nDatabase: %s\n",
			result.Provider,
			result.Model,
			result.ModelRevision,
			result.CredentialRef,
			result.Database,
		)
	} else {
		fmt.Fprintf(
			stderr,
			"WARNING: %s; provider=%s model=%s revision=%s cost=unknown\n",
			warning,
			result.Provider,
			result.Model,
			result.ModelRevision,
		)
	}
	if err := runtime.credentialStore.Test(ctx, reference); err != nil {
		result.Reason = "configured operating-system credential is unavailable"
		return finishLiveGate(stdout, stderr, invocation, result, exitUnavailable)
	}

	database, err := storage.Open(
		ctx,
		storage.OpenOptions{Path: databasePath},
	)
	if err != nil {
		result.Status = "failed"
		result.Reason = "open external live-provider database"
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	defer func() {
		closeContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = database.Close(closeContext)
	}()
	if _, err := database.Migrate(
		ctx,
		storage.MigrationOptions{
			ApplicationVersion: buildinfo.Current().Version,
			BackupDirectory: filepath.Join(
				filepath.Dir(databasePath),
				"codeflux-live-backups",
			),
		},
	); err != nil {
		result.Status = "failed"
		result.Reason = "migrate external live-provider database"
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	repositories, err := storage.NewRepositories(database, runtime.now)
	if err != nil {
		result.Status = "failed"
		result.Reason = "initialize live-provider repositories"
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}

	capabilities := liveProviderCapabilities(invocation.Provider)
	capabilitiesJSON := []byte(
		`{"configured_for_live_smoke":true,"streaming":true}`,
	)
	requestHash, err := liveRequestHash(invocation)
	if err != nil {
		result.Status = "failed"
		result.Reason = "hash live-provider request"
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	idempotencyKey, err := newLiveIdempotencyKey()
	if err != nil {
		result.Status = "failed"
		result.Reason = "create live-provider request identity"
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	adapterName, adapterVersion, providerVersion, endpoint :=
		liveProviderConfiguration(invocation.Provider)
	identity := providers.ProviderIdentity{
		Adapter: adapterName, AdapterVersion: adapterVersion,
		Provider: invocation.Provider, ProviderVersion: providerVersion,
	}
	modelIdentity := providers.ModelIdentity{
		Provider: identity, Model: invocation.Model,
		Revision: invocation.ModelRevision,
	}
	gitIdentity, err := liveRepositoryGitIdentity(ctx, repository)
	if err != nil {
		result.Status = "failed"
		result.Reason = "identify live-provider repository"
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	taskBudget, err := runtime.budgetFactory()
	if err != nil {
		result.Status = "failed"
		result.Reason = "create fixed live-provider budget"
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	selectedPolicy, err := policy.Select(policy.SelectionInput{
		BaselineModelRevision: invocation.ModelRevision,
		Override: &policy.ManualOverride{
			Model: modelIdentity, Reasoning: domain.ReasoningEffortMaximum,
			Actor:              "codeflux-dev",
			AuthorityReference: "run-live explicit provider and model selection",
			Reason:             "attributable live-provider diagnostic",
		},
	})
	if err != nil {
		result.Status = "failed"
		result.Reason = "select fixed live-provider execution policy"
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	effortForecast, err := forecast.Generate(forecast.Input{
		RepositoryRevision:       gitIdentity,
		TaskFingerprint:          requestHash,
		TaskClass:                forecast.TaskClassSmallChange,
		Policy:                   selectedPolicy,
		ToolConfigurationVersion: "live-smoke-tools-v1",
		ValidationProfileVersion: "live-smoke-runtime-v1",
	})
	if err != nil {
		result.Status = "failed"
		result.Reason = "generate live-provider effort forecast"
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	eligibility, err := forecast.NewCounterfactualEligibility(
		false,
		[]string{"live-provider-diagnostic"},
	)
	if err != nil {
		result.Status = "failed"
		result.Reason = "build live-provider shadow eligibility"
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	fixture, err := repositories.PrepareLiveProviderSmokeRequest(
		ctx,
		storage.PrepareLiveProviderSmokeRequest{
			IdempotencyKey: idempotencyKey,
			RepositoryPath: repository, RepositoryGitIdentity: gitIdentity,
			ProviderType:        invocation.Provider,
			ProviderDisplayName: "Codeflux " + invocation.Provider + " live smoke",
			AdapterName:         adapterName, AdapterVersion: adapterVersion,
			ProviderVersion: providerVersion, EndpointRedacted: endpoint,
			CapabilitiesJSON:          string(capabilitiesJSON),
			OpaqueCredentialReference: invocation.CredentialRef,
			ModelIdentifier:           invocation.Model, ModelVersion: invocation.ModelRevision,
			RequestSHA256: requestHash, Pricing: runtime.pricing,
			Policy: selectedPolicy, Forecast: effortForecast,
			Eligibility: eligibility, Budget: taskBudget,
		},
	)
	if err != nil {
		result.Status = "failed"
		result.Reason = "prepare attributable live-provider request"
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	credentialSource, err := coordinator.NewProviderCredentialSource(
		runtime.credentialStore,
		repositories,
	)
	if err != nil {
		result.Status = "failed"
		result.Reason = "initialize live-provider credential boundary"
		if abortErr := abortLiveProviderBeforeIO(repositories, fixture); abortErr != nil {
			result.Reason += "; durable pre-I/O abort also failed"
		}
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	adapter, err := runtime.providerFactory(
		credentialSource, fixture, modelIdentity, capabilities,
	)
	if err != nil {
		result.Status = "failed"
		result.Reason = "initialize explicit live-provider adapter"
		if abortErr := abortLiveProviderBeforeIO(repositories, fixture); abortErr != nil {
			result.Reason += "; durable pre-I/O abort also failed"
		}
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	providerIdempotency := invocation.Provider == "openai"
	idempotency := providers.RequestIdempotency{}
	if providerIdempotency {
		idempotency = providers.RequestIdempotency{
			ProviderSupported: true,
			Key:               fixture.Request.IdempotencyKey,
			ProviderScope:     "openai-responses",
		}
	}
	request := providers.ModelRequest{
		Identity: providers.RequestIdentity{
			ModelRequestID: fixture.Request.ID,
			Provider:       identity,
			Model:          modelIdentity,
			Idempotency:    idempotency,
			IdempotencyKey: fixture.Request.IdempotencyKey,
			RequestHash:    requestHash,
		},
		Messages: []providers.Message{{
			Role: providers.MessageRoleUser,
			Content: []providers.ContentPart{{
				Kind: providers.ContentKindText,
				Text: "Reply with exactly OK.",
			}},
		}},
		MaximumTokens: 16,
		Idempotency:   idempotency,
		Deadline:      runtime.now().Add(liveRequestTimeout),
	}
	priceSnapshot, err := liveProviderPriceSnapshot(
		fixture.Pricing,
		modelIdentity,
		taskBudget.HardStopCost.Currency,
	)
	if err != nil {
		result.Status = "failed"
		result.Reason = "build immutable live-provider price snapshot"
		if abortErr := abortLiveProviderBeforeIO(repositories, fixture); abortErr != nil {
			result.Reason += "; durable pre-I/O abort also failed"
		}
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	budgetedExecution, err := coordinator.NewBudgetedProviderExecutionService(
		adapter,
		repositories,
		providers.RetryPolicy{
			MaximumAttempts:       2,
			InitialDelay:          500 * time.Millisecond,
			MaximumBackoff:        2 * time.Second,
			MaximumCumulativeWait: 3 * time.Second,
		},
	)
	if err != nil {
		result.Status = "failed"
		result.Reason = "initialize budgeted live-provider execution"
		if abortErr := abortLiveProviderBeforeIO(repositories, fixture); abortErr != nil {
			result.Reason += "; durable pre-I/O abort also failed"
		}
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	approvedUsage := providers.Usage{
		Known: true, Source: providers.UsageSourceEstimated,
		InputTokens: 32, OutputTokens: 64,
	}
	started := runtime.now()
	budgetedResult, err := budgetedExecution.Execute(
		ctx,
		coordinator.ExecuteBudgetedProviderRequest{
			BudgetID:                taskBudget.ID,
			TaskID:                  fixture.TaskID,
			RunID:                   fixture.RunID,
			PreflightRevision:       fixture.Preflight.Revision,
			ExpectedBudgetRevision:  fixture.Budget.Revision,
			ApprovedUsagePerAttempt: approvedUsage,
			Provider: coordinator.ExecuteProviderRequest{
				Request: request, EstimatedUsage: approvedUsage,
				PriceSnapshot: priceSnapshot,
			},
		},
	)
	executionResult := budgetedResult.Provider
	finalizeContext, finalizeCancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		10*time.Second,
	)
	attribution, attributionErr := repositories.GetProviderRequestAttribution(
		finalizeContext,
		fixture.Request.ID,
	)
	if attributionErr == nil {
		if (attribution.Request.State == storage.ProviderLogicalRequestPlanned ||
			attribution.Request.State == storage.ProviderLogicalRequestInFlight) &&
			attribution.Accounting.AttemptCount == 0 {
			blockReason := storage.LiveProviderPreIOAborted
			switch {
			case errors.Is(err, coordinator.ErrProviderBudgetPriceUnknown):
				blockReason = storage.LiveProviderPreIOPriceUnknown
			case errors.Is(err, storage.ErrBudgetExhausted):
				blockReason = storage.LiveProviderPreIOBudgetExhausted
			}
			_, attributionErr = repositories.AbortLiveProviderSmokeRequestBeforeIO(
				finalizeContext,
				storage.AbortLiveProviderSmokeRequestBeforeIO{
					RequestID:        fixture.Request.ID,
					ExpectedRevision: attribution.Request.Revision,
					Reason:           blockReason,
				},
			)
		} else {
			target := attribution.Request.State
			accountingStatus := attribution.Request.AccountingStatus
			if target == storage.ProviderLogicalRequestInFlight {
				target = storage.ProviderLogicalRequestOutcomeUnknown
				if len(attribution.Attempts) != 0 {
					last := attribution.Attempts[len(attribution.Attempts)-1].Attempt
					switch last.State {
					case storage.ProviderRequestAttemptSucceeded:
						target = storage.ProviderLogicalRequestSucceeded
					case storage.ProviderRequestAttemptCancelled:
						target = storage.ProviderLogicalRequestCancelled
					case storage.ProviderRequestAttemptOutcomeUnknown:
						target = storage.ProviderLogicalRequestOutcomeUnknown
					case storage.ProviderRequestAttemptFailed:
						target = storage.ProviderLogicalRequestFailed
						if last.Retryable {
							target = storage.ProviderLogicalRequestRetryExhausted
						}
					}
				}
				switch {
				case attribution.Accounting.Discrepancy:
					accountingStatus = storage.ProviderAccountingDiscrepant
				case attribution.Accounting.Usage.Known:
					accountingStatus = storage.ProviderAccountingProviderReported
				default:
					accountingStatus = storage.ProviderAccountingUnknown
				}
			}
			_, attributionErr = repositories.FinalizeLiveProviderSmokeRequest(
				finalizeContext,
				storage.FinalizeLiveProviderSmokeRequest{
					RequestID:        fixture.Request.ID,
					ExpectedRevision: attribution.Request.Revision,
					To:               target,
					AccountingStatus: accountingStatus,
				},
			)
		}
	}
	finalizeCancel()
	err = errors.Join(err, attributionErr)
	result.LatencyMillis = runtime.now().Sub(started).Milliseconds()
	result.Attempts = executionResult.Accounting.AttemptCount
	if executionResult.Accounting.Usage.Known {
		result.UsageStatus = "provider-reported"
	}
	if executionResult.Accounting.Cost != nil {
		result.CostStatus = "known"
	}
	if err != nil {
		result.Status = "failed"
		result.Reason = safeLiveFailureReason(err)
		return finishLiveGate(stdout, stderr, invocation, result, exitFailure)
	}
	result.Status = "succeeded"
	return finishLiveGate(stdout, stderr, invocation, result, exitSuccess)
}

func finishLiveGate(
	stdout,
	stderr io.Writer,
	invocation commandInvocation,
	result liveGateResult,
	code int,
) int {
	if invocation.JSON {
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintf(stderr, "codeflux-dev run-live: encode result: %v\n", err)
			return exitFailure
		}
	} else {
		fmt.Fprintf(
			stdout,
			"Status: %s\nUsage: %s\nCost: %s\nAttempts: %d\nLatency: %d ms\n",
			result.Status,
			result.UsageStatus,
			result.CostStatus,
			result.Attempts,
			result.LatencyMillis,
		)
		if result.Reason != "" {
			fmt.Fprintf(stderr, "codeflux-dev run-live: %s\n", result.Reason)
		}
	}
	return code
}

func newLiveProvider(
	credentials *coordinator.ProviderCredentialSource,
	fixture storage.LiveProviderSmokeRequest,
	model providers.ModelIdentity,
	capabilities providers.ModelCapabilities,
) (providers.ModelProvider, error) {
	transport, err := providers.NewHTTPTransport(providers.TransportOptions{})
	if err != nil {
		return nil, err
	}
	switch model.Provider.Provider {
	case "openai":
		return openai.New(openai.Config{
			ProviderID: fixture.ProviderID,
			Endpoint:   "https://api.openai.com/v1", RemoteApproved: true,
			Model: model.Model, ModelRevision: model.Revision,
			AdapterVersion:  model.Provider.AdapterVersion,
			ProviderVersion: model.Provider.ProviderVersion,
			Capabilities:    capabilities, Credentials: credentials,
			Transport: transport,
		})
	case "anthropic":
		return anthropic.New(anthropic.Config{
			BaseURL: "https://api.anthropic.com",
			Model:   model, Capabilities: capabilities, Transport: transport,
			UseCredential: func(
				ctx context.Context,
				operation func([]byte) error,
			) error {
				return credentials.Use(ctx, fixture.ProviderID, operation)
			},
			RemoteApproved: true,
		})
	default:
		return nil, errors.New("live provider is unsupported")
	}
}

func parseLiveCredentialReference(value string) (credentials.Reference, error) {
	if !strings.HasPrefix(value, "os://") ||
		strings.ContainsAny(value, "\x00\r\n") {
		return credentials.Reference{}, errors.New("credential reference is invalid")
	}
	parts := strings.Split(strings.TrimPrefix(value, "os://"), "/")
	if len(parts) != 2 {
		return credentials.Reference{}, errors.New("credential reference is invalid")
	}
	return credentials.NewReference(parts[0], parts[1])
}

func liveProviderConfiguration(provider string) (
	adapter,
	adapterVersion,
	providerVersion,
	endpoint string,
) {
	if provider == "anthropic" {
		return "anthropic-messages", "1", "messages-2023-06-01",
			"https://api.anthropic.com"
	}
	return "openai-responses", "1", "responses-v1",
		"https://api.openai.com/v1"
}

func liveProviderCapabilities(_ string) providers.ModelCapabilities {
	return providers.ModelCapabilities{
		Streaming: true,
	}
}

func validLiveIdentityValue(value string) bool {
	return strings.TrimSpace(value) == value &&
		value != "" &&
		len(value) <= 255 &&
		!strings.ContainsAny(value, "\x00\r\n")
}

func resolvePathThroughExistingAncestor(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(absolute)
	var suffix []string
	for {
		if _, statErr := os.Lstat(current); statErr == nil {
			break
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("path has no existing ancestor")
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
	resolved, err := resolveExistingPath(current)
	if err != nil {
		return "", err
	}
	for index := len(suffix) - 1; index >= 0; index-- {
		resolved = filepath.Join(resolved, suffix[index])
	}
	return filepath.Abs(resolved)
}

func liveRequestHash(invocation commandInvocation) (string, error) {
	payload, err := json.Marshal(struct {
		SchemaVersion int      `json:"schema_version"`
		Provider      string   `json:"provider"`
		Model         string   `json:"model"`
		ModelRevision string   `json:"model_revision"`
		Messages      []string `json:"messages"`
		MaximumTokens int      `json:"maximum_tokens"`
	}{
		SchemaVersion: 1,
		Provider:      invocation.Provider,
		Model:         invocation.Model,
		ModelRevision: invocation.ModelRevision,
		Messages:      []string{"user:Reply with exactly OK."},
		MaximumTokens: 16,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func newLiveIdempotencyKey() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "run-live-" + hex.EncodeToString(value[:]), nil
}

func liveRepositoryGitIdentity(ctx context.Context, repository string) (string, error) {
	common, err := gitOutput(ctx, repository, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(repository, common)
	}
	return filepath.Abs(common)
}

func newLiveTaskBudget() (domain.TaskBudget, error) {
	budgetID, err := domain.NewBudgetID()
	if err != nil {
		return domain.TaskBudget{}, err
	}
	usd := domain.CurrencyCode("USD")
	return domain.TaskBudget{
		ID:                    budgetID,
		WarningCost:           domain.Money{Currency: usd, MinorUnits: 50},
		HardStopCost:          domain.Money{Currency: usd, MinorUnits: 100},
		WarningTokens:         96,
		HardStopTokens:        192,
		WarningWallClock:      domain.Milliseconds((30 * time.Second).Milliseconds()),
		HardStopWallClock:     domain.Milliseconds(liveRequestTimeout.Milliseconds()),
		MaximumProviderCalls:  2,
		MaximumRepairRounds:   0,
		MaximumToolExecutions: 0,
	}, nil
}

func liveProviderPriceSnapshot(
	revision storage.ProviderPricingRevision,
	model providers.ModelIdentity,
	fallbackCurrency domain.CurrencyCode,
) (*providers.PriceSnapshot, error) {
	snapshot := &providers.PriceSnapshot{
		ID: revision.ID, Model: model,
		EffectiveAt: revision.EffectiveAt,
		CapturedAt:  revision.CreatedAt,
		Source:      "explicitly-unknown",
		Price: providers.TokenPrice{
			Input:            providers.UnknownAmount(string(fallbackCurrency)),
			CachedInput:      providers.UnknownAmount(string(fallbackCurrency)),
			CacheWrite:       providers.UnknownAmount(string(fallbackCurrency)),
			Output:           providers.UnknownAmount(string(fallbackCurrency)),
			Reasoning:        providers.UnknownAmount(string(fallbackCurrency)),
			ProviderSpecific: map[string]providers.ExactAmount{},
		},
	}
	if !revision.PricingKnown {
		return snapshot, nil
	}
	if revision.Currency == nil || revision.SourceRedacted == nil {
		return nil, errors.New("known provider pricing lacks currency or source")
	}
	snapshot.Source = *revision.SourceRedacted
	currency := string(*revision.Currency)
	for _, component := range revision.Components {
		price, err := liveProviderComponentPrice(currency, component)
		if err != nil {
			return nil, err
		}
		switch component.UsageKind {
		case "input":
			snapshot.Price.Input = price
		case "cached-input":
			snapshot.Price.CachedInput = price
		case "cache-write":
			snapshot.Price.CacheWrite = price
		case "output":
			snapshot.Price.Output = price
		case "reasoning":
			snapshot.Price.Reasoning = price
		case "provider-specific":
			if component.ProviderSpecificKind == nil {
				return nil, errors.New("provider-specific price lacks category")
			}
			snapshot.Price.ProviderSpecific[*component.ProviderSpecificKind] = price
		default:
			return nil, errors.New("provider price has unsupported usage category")
		}
	}
	return snapshot, nil
}

func liveProviderComponentPrice(
	currency string,
	component storage.ProviderPriceComponent,
) (providers.ExactAmount, error) {
	numerator := new(big.Int).Mul(
		big.NewInt(component.MinorNumerator),
		big.NewInt(1_000_000),
	)
	denominator := big.NewInt(component.TokenDenominator)
	common := new(big.Int).GCD(nil, nil, numerator, denominator)
	if common.Sign() == 0 {
		common.SetInt64(1)
	}
	numerator.Quo(numerator, common)
	denominator.Quo(denominator, common)
	if !numerator.IsInt64() || !denominator.IsInt64() {
		return providers.ExactAmount{},
			errors.New("provider price exceeds runtime exact integer range")
	}
	return providers.NewExactAmount(
		currency,
		numerator.Int64(),
		denominator.Int64(),
	)
}

func safeLiveFailureReason(err error) string {
	switch {
	case errors.Is(err, coordinator.ErrProviderBudgetPriceUnknown):
		return "live-provider request blocked before I/O because pricing is unknown"
	case errors.Is(err, storage.ErrBudgetExhausted):
		return "live-provider request blocked before I/O by the hard budget"
	case errors.Is(err, storage.ErrBudgetReconciliationPending):
		return "live-provider usage was preserved for durable budget reconciliation"
	case errors.Is(err, providers.ErrAuthentication):
		return "provider rejected the configured credential"
	case errors.Is(err, providers.ErrRateLimited):
		return "provider rate limit exhausted the bounded live smoke"
	case errors.Is(err, providers.ErrSafety):
		return "provider safety policy stopped the live smoke"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, providers.ErrTimeout), errors.Is(err, providers.ErrCanceled):
		return "live-provider request was canceled or timed out"
	default:
		return "live-provider request failed; inspect the redacted external database evidence"
	}
}

func abortLiveProviderBeforeIO(
	repositories *storage.Repositories,
	fixture storage.LiveProviderSmokeRequest,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := repositories.AbortLiveProviderSmokeRequestBeforeIO(
		ctx,
		storage.AbortLiveProviderSmokeRequestBeforeIO{
			RequestID:        fixture.Request.ID,
			ExpectedRevision: fixture.Request.Revision,
		},
	)
	return err
}

func looksLikeProviderSecret(value string) bool {
	for _, secret := range secretPatterns {
		if secret.pattern.Match(bytes.TrimSpace([]byte(value))) {
			return true
		}
	}
	return false
}
