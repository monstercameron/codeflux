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
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/buildinfo"
	"codeflux.dev/codeflux/internal/coordinator"
	"codeflux.dev/codeflux/internal/credentials"
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
	now             func() time.Time
}

func defaultLiveRuntime() liveRuntime {
	return liveRuntime{
		credentialStore: credentials.NewPlatformStore(),
		providerFactory: newLiveProvider,
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

	const warning = "LIVE PROVIDER REQUEST CAN INCUR REAL COST; pricing is unknown and will never be displayed as zero"
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
	gitIdentity, err := liveRepositoryGitIdentity(ctx, repository)
	if err != nil {
		result.Status = "failed"
		result.Reason = "identify live-provider repository"
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
			RequestSHA256: requestHash, Pricing: nil,
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
	identity := providers.ProviderIdentity{
		Adapter: adapterName, AdapterVersion: adapterVersion,
		Provider: invocation.Provider, ProviderVersion: providerVersion,
	}
	modelIdentity := providers.ModelIdentity{
		Provider: identity, Model: invocation.Model,
		Revision: invocation.ModelRevision,
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
	execution, err := coordinator.NewProviderExecutionService(
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
		result.Reason = "initialize live-provider execution"
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
	started := runtime.now()
	executionResult, err := execution.Execute(
		ctx,
		coordinator.ExecuteProviderRequest{
			Request: request,
			PriceSnapshot: &providers.PriceSnapshot{
				ID: fixture.Pricing.ID, Model: modelIdentity,
				EffectiveAt: fixture.Pricing.EffectiveAt,
				CapturedAt:  fixture.Pricing.CreatedAt,
				Source:      "unknown",
			},
		},
	)
	finalizeContext, finalizeCancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		10*time.Second,
	)
	attribution, attributionErr := repositories.GetProviderRequestAttribution(
		finalizeContext,
		fixture.Request.ID,
	)
	if attributionErr == nil {
		if attribution.Request.State == storage.ProviderLogicalRequestInFlight &&
			attribution.Accounting.AttemptCount == 0 {
			_, attributionErr = repositories.AbortLiveProviderSmokeRequestBeforeIO(
				finalizeContext,
				storage.AbortLiveProviderSmokeRequestBeforeIO{
					RequestID:        fixture.Request.ID,
					ExpectedRevision: attribution.Request.Revision,
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

func safeLiveFailureReason(err error) string {
	switch {
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
