package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/coordinator"
	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
)

func TestRunLiveGateRequiresExplicitSafeInputsAndWarns(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	database := filepath.Join(t.TempDir(), "live.sqlite")
	code := runLiveGateWithRuntime(context.Background(), &stdout, &stderr, commandInvocation{
		JSON:          true,
		Provider:      "openai",
		Model:         "model-fixture",
		ModelRevision: "revision-fixture",
		CredentialRef: "os://codeflux-openai/default",
		Database:      database,
	}, liveRuntime{
		credentialStore: credentials.NewUnavailableStore("test"),
		providerFactory: newLiveProvider,
	})
	if code != exitUnavailable {
		t.Fatalf("run-live exit = %d, stderr=%q", code, stderr.String())
	}
	var result liveGateResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if result.Provider != "openai" || result.CredentialRef != "os://codeflux-openai/default" ||
		result.Database != database || !strings.Contains(result.Warning, "REAL COST") {
		t.Fatalf("live gate result = %#v", result)
	}
}

func TestRunLiveGateRejectsRawSecretAndRepositoryDatabase(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rawSecret := "os://sk-" + strings.Repeat("A", 24)
	code := runLiveGate(context.Background(), &stdout, &stderr, commandInvocation{
		Provider:      "openai",
		Model:         "model-fixture",
		ModelRevision: "revision-fixture",
		CredentialRef: rawSecret,
		Database:      filepath.Join(t.TempDir(), "live.sqlite"),
	})
	if code != exitUsage || !strings.Contains(stderr.String(), "non-secret") {
		t.Fatalf("raw-secret gate = %d, %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	repository, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	code = runLiveGate(context.Background(), &stdout, &stderr, commandInvocation{
		Provider:      "anthropic",
		Model:         "model-fixture",
		ModelRevision: "revision-fixture",
		CredentialRef: "os://codeflux-anthropic/default",
		Database:      filepath.Join(repository, ".artifacts", "live.sqlite"),
	})
	if code != exitUsage || !strings.Contains(stderr.String(), "outside the source repository") {
		t.Fatalf("repository database gate = %d, %q", code, stderr.String())
	}
}

func TestRunLiveGateRejectsOutsideSymlinkIntoRepository(t *testing.T) {
	repository, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(repository, ".artifacts")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "outside-db-link")
	if runtime.GOOS == "windows" {
		if output, err := exec.Command(
			"cmd.exe",
			"/c",
			"mklink",
			"/J",
			link,
			target,
		).CombinedOutput(); err != nil {
			t.Skipf(
				"junction unavailable on this platform: %v (%s)",
				err,
				strings.TrimSpace(string(output)),
			)
		}
		t.Cleanup(func() {
			if err := os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("remove test junction: %v", err)
			}
		})
	} else if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable on this platform: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runLiveGate(
		context.Background(),
		&stdout,
		&stderr,
		commandInvocation{
			Provider: "openai", Model: "fixture-model",
			ModelRevision: "fixture-revision",
			CredentialRef: "os://openai/live-test",
			Database:      filepath.Join(link, "live.sqlite"),
		},
	)
	if code != exitUsage ||
		!strings.Contains(stderr.String(), "outside the source repository") {
		resolvedRepository, _ := resolvePathThroughExistingAncestor(repository)
		resolvedDatabase, _ := resolvePathThroughExistingAncestor(
			filepath.Join(link, "live.sqlite"),
		)
		t.Fatalf(
			"symlink repository database gate = %d, stderr=%q, repository=%q database=%q",
			code,
			stderr.String(),
			resolvedRepository,
			resolvedDatabase,
		)
	}
}

func TestParseCommandInvocationReadsLiveOptions(t *testing.T) {
	invocation, err := parseCommandInvocation([]string{
		"--provider=anthropic",
		"--model", "model-fixture",
		"--model-revision=revision-fixture",
		"--credential-ref", "os://codeflux-anthropic/default",
		"--database", "C:\\data\\codeflux.sqlite",
		"--json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Provider != "anthropic" ||
		invocation.Model != "model-fixture" ||
		invocation.ModelRevision != "revision-fixture" ||
		invocation.CredentialRef != "os://codeflux-anthropic/default" ||
		invocation.Database != "C:\\data\\codeflux.sqlite" ||
		!invocation.JSON {
		t.Fatalf("parsed invocation = %#v", invocation)
	}
}

func TestRunLiveGateExecutesOneAttributedContentFreeSmoke(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var requestID domain.ModelRequestID
	databasePath := filepath.Join(t.TempDir(), "live.sqlite")
	store := liveTestCredentialStore{secret: []byte("test-secret-never-print")}
	code := runLiveGateWithRuntime(
		context.Background(),
		&stdout,
		&stderr,
		commandInvocation{
			JSON: true, Provider: "openai",
			Model: "fixture-model", ModelRevision: "fixture-revision",
			CredentialRef: "os://openai/live-test",
			Database:      databasePath,
		},
		liveRuntime{
			credentialStore: store,
			pricing:         liveKnownPricing(time.Now()),
			providerFactory: func(
				_ *coordinator.ProviderCredentialSource,
				fixture storage.LiveProviderSmokeRequest,
				model providers.ModelIdentity,
				_ providers.ModelCapabilities,
			) (providers.ModelProvider, error) {
				requestID = fixture.Request.ID
				usage := providers.Usage{
					Known: true, Source: providers.UsageSourceProvider,
					InputTokens: 5, OutputTokens: 1,
				}
				return &liveTestProvider{
					identity: model.Provider,
					events: []providers.StreamEvent{
						{
							Sequence: 1, Kind: providers.StreamEventMetadata,
							Metadata: &providers.RedactedProviderMetadata{
								RequestID:  fixture.Request.ID.String(),
								ResponseID: "provider-response-fixture",
								Fields: map[string]string{
									"request_status": "accepted",
								},
							},
						},
						{
							Sequence: 2, Kind: providers.StreamEventTextDelta,
							Text: "OK",
						},
						{
							Sequence: 3, Kind: providers.StreamEventUsage,
							Usage: &usage,
						},
						{
							Sequence: 4, Kind: providers.StreamEventFinal,
							Final: &providers.FinalResponse{
								Identity:   model,
								StopReason: providers.StopReasonCompleted,
								Usage:      usage,
								Metadata: providers.RedactedProviderMetadata{
									RequestID:  fixture.Request.ID.String(),
									ResponseID: "provider-response-fixture",
									Fields: map[string]string{
										"request_status": "accepted",
									},
								},
								PartialEffect: providers.PartialEffectEvidence{
									ProviderAck: true, StreamedOutput: true,
									LastSequence: 3,
								},
							},
						},
					},
				}, nil
			},
			now: time.Now,
		},
	)
	if code != exitSuccess {
		t.Fatalf(
			"run-live exit = %d, stdout=%q stderr=%q",
			code,
			stdout.String(),
			stderr.String(),
		)
	}
	var result liveGateResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" ||
		result.UsageStatus != "provider-reported" ||
		result.CostStatus != "known" ||
		result.Attempts != 1 {
		t.Fatalf("run-live result = %#v", result)
	}
	for _, forbidden := range []string{"OK", "test-secret-never-print"} {
		if strings.Contains(stdout.String(), forbidden) ||
			strings.Contains(stderr.String(), forbidden) {
			t.Fatalf("run-live output exposed %q", forbidden)
		}
	}

	database, err := storage.Open(
		context.Background(),
		storage.OpenOptions{Path: databasePath},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeContext, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		if err := database.Close(closeContext); err != nil {
			t.Errorf("close live test database: %v", err)
		}
	})
	repositories, err := storage.NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	attribution, err := repositories.GetProviderRequestAttribution(
		context.Background(),
		requestID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if attribution.Request.State != storage.ProviderLogicalRequestSucceeded ||
		attribution.Request.ModelVersion != "fixture-revision" ||
		attribution.Pricing == nil || !attribution.Pricing.PricingKnown ||
		len(attribution.Attempts) != 1 ||
		attribution.Attempts[0].Attempt.ProviderRequestIDRedacted == nil ||
		*attribution.Attempts[0].Attempt.ProviderRequestIDRedacted !=
			"provider-response-fixture" ||
		attribution.Attempts[0].Attempt.SafeMetadataJSON == nil {
		t.Fatalf("run-live attribution = %#v", attribution)
	}
}

func TestRunLiveGateBudgetBlocksUnknownAndInsufficientCostBeforeStream(
	t *testing.T,
) {
	tests := []struct {
		name       string
		pricing    *storage.LiveProviderSmokePricing
		budget     func() (domain.TaskBudget, error)
		wantReason string
		wantPaused bool
	}{
		{
			name:       "unknown pricing",
			wantReason: "pricing is unknown",
		},
		{
			name:    "insufficient hard cap",
			pricing: liveExpensivePricing(time.Now()),
			budget: func() (domain.TaskBudget, error) {
				budget, err := newLiveTaskBudget()
				budget.WarningCost.MinorUnits = 1
				budget.HardStopCost.MinorUnits = 1
				return budget, err
			},
			wantReason: "hard budget",
			wantPaused: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			streamCalls := 0
			var fixture storage.LiveProviderSmokeRequest
			databasePath := filepath.Join(t.TempDir(), "live.sqlite")
			runtime := liveRuntime{
				credentialStore: liveTestCredentialStore{
					secret: []byte("test-secret-never-print"),
				},
				pricing:       test.pricing,
				budgetFactory: test.budget,
				providerFactory: func(
					_ *coordinator.ProviderCredentialSource,
					prepared storage.LiveProviderSmokeRequest,
					model providers.ModelIdentity,
					_ providers.ModelCapabilities,
				) (providers.ModelProvider, error) {
					fixture = prepared
					return &liveTestProvider{
						identity:    model.Provider,
						streamCalls: &streamCalls,
					}, nil
				},
				now: time.Now,
			}
			code := runLiveGateWithRuntime(
				context.Background(),
				&stdout,
				&stderr,
				commandInvocation{
					JSON: true, Provider: "openai",
					Model: "fixture-model", ModelRevision: "fixture-revision",
					CredentialRef: "os://openai/live-test",
					Database:      databasePath,
				},
				runtime,
			)
			if code != exitFailure {
				t.Fatalf(
					"run-live exit = %d, stdout=%q stderr=%q",
					code, stdout.String(), stderr.String(),
				)
			}
			if streamCalls != 0 {
				t.Fatalf("provider Stream calls = %d, want zero", streamCalls)
			}
			var result liveGateResult
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Attempts != 0 ||
				!strings.Contains(result.Reason, test.wantReason) {
				t.Fatalf("run-live result = %#v", result)
			}
			if test.wantPaused {
				assertLiveBudgetExhaustedPersistence(
					t,
					databasePath,
					fixture,
				)
			}
		})
	}
}

type liveTestCredentialStore struct {
	secret []byte
}

func (liveTestCredentialStore) Create(
	context.Context,
	credentials.Reference,
	credentials.Secret,
) error {
	return errors.New("not supported")
}

func (liveTestCredentialStore) Update(
	context.Context,
	credentials.Reference,
	credentials.Secret,
) error {
	return errors.New("not supported")
}

func (store liveTestCredentialStore) Retrieve(
	context.Context,
	credentials.Reference,
) (credentials.Secret, error) {
	return credentials.NewSecret(store.secret)
}

func (liveTestCredentialStore) Test(
	context.Context,
	credentials.Reference,
) error {
	return nil
}

func (liveTestCredentialStore) Delete(
	context.Context,
	credentials.Reference,
) error {
	return errors.New("not supported")
}

type liveTestProvider struct {
	identity    providers.ProviderIdentity
	events      []providers.StreamEvent
	streamCalls *int
}

func (provider *liveTestProvider) ProviderIdentity() providers.ProviderIdentity {
	return provider.identity
}

func (*liveTestProvider) ListModels(
	context.Context,
) ([]providers.ModelDescriptor, error) {
	return nil, nil
}

func (*liveTestProvider) Capabilities(
	context.Context,
	providers.ModelIdentity,
) (providers.ModelCapabilities, error) {
	return providers.ModelCapabilities{}, nil
}

func (provider *liveTestProvider) Stream(
	context.Context,
	providers.ModelRequest,
) (providers.ModelStream, error) {
	if provider.streamCalls != nil {
		(*provider.streamCalls)++
	}
	return &liveTestStream{events: provider.events}, nil
}

func liveKnownPricing(now time.Time) *storage.LiveProviderSmokePricing {
	return &storage.LiveProviderSmokePricing{
		Currency:       domain.CurrencyCode("USD"),
		SourceRedacted: "test immutable pricing",
		EffectiveAt:    now.UTC(),
		Components: []storage.ProviderPriceComponent{
			{
				UsageKind: "input", MinorNumerator: 1,
				TokenDenominator: 1_000_000,
			},
			{
				UsageKind: "output", MinorNumerator: 1,
				TokenDenominator: 1_000_000,
			},
		},
	}
}

func liveExpensivePricing(now time.Time) *storage.LiveProviderSmokePricing {
	pricing := liveKnownPricing(now)
	for index := range pricing.Components {
		pricing.Components[index].TokenDenominator = 1
	}
	return pricing
}

func assertLiveBudgetExhaustedPersistence(
	t *testing.T,
	path string,
	fixture storage.LiveProviderSmokeRequest,
) {
	t.Helper()
	if fixture.TaskID.IsZero() || fixture.Request.ID.IsZero() {
		t.Fatal("budget-exhausted live fixture was not persisted")
	}
	ctx := context.Background()
	database, err := storage.Open(ctx, storage.OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeContext, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		if err := database.Close(closeContext); err != nil {
			t.Errorf("close budget-exhausted live database: %v", err)
		}
	})
	repositories, err := storage.NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := repositories.GetTask(ctx, fixture.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	attribution, err := repositories.GetProviderRequestAttribution(
		ctx,
		fixture.Request.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if task.State != domain.TaskStatePaused ||
		attribution.Request.State != storage.ProviderLogicalRequestCancelled ||
		attribution.Accounting.AttemptCount != 0 {
		t.Fatalf(
			"budget-exhausted persistence task=%q request=%q attempts=%d",
			task.State,
			attribution.Request.State,
			attribution.Accounting.AttemptCount,
		)
	}
}

func (*liveTestProvider) Cancel(
	context.Context,
	domain.ModelRequestID,
) error {
	return nil
}

type liveTestStream struct {
	events []providers.StreamEvent
	index  int
	closed bool
}

func (stream *liveTestStream) Recv(
	ctx context.Context,
) (providers.StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return providers.StreamEvent{}, err
	}
	if stream.closed || stream.index >= len(stream.events) {
		return providers.StreamEvent{}, io.EOF
	}
	event := stream.events[stream.index]
	stream.index++
	return event, nil
}

func (stream *liveTestStream) Close() error {
	stream.closed = true
	return nil
}
