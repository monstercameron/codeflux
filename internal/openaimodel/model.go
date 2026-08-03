// Package openaimodel implements the agent loop's model port over the OpenAI
// Responses API.
//
// The execution loop was complete and had nowhere to think: FixedModel is a
// one-method port and nothing implemented it, so a started task reached a
// worktree and stopped. This is that implementation. It owns the wire format
// and nothing else — budget reservation, tool authorization, and durable
// recording all stay above it, where they already were.
package openaimodel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/tracing"
)

// DefaultEndpoint is the Responses API.
const DefaultEndpoint = "https://api.openai.com/v1/responses"

// DefaultModel is the model a run uses unless told otherwise.
const DefaultModel = "gpt-5.6-luna"

// Options configure one model client.
type Options struct {
	// APIKey is never logged, never included in an error, and never written to
	// the event journal. It is held here and used only as a request header.
	APIKey   string
	Model    string
	Endpoint string
	Timeout  time.Duration
	Client   *http.Client
	// Price is what the provider charges per token. The loop refuses a turn
	// whose cost is unknown, and correctly: a run enforcing a budget it cannot
	// price is not enforcing anything. There is no default, because a guessed
	// rate produces a number that looks measured.
	Price providers.TokenPrice
	// StyleDirective is how this project wants its code shaped.
	//
	// It reaches the model while it is writing rather than being discovered at
	// review, which is the only point at which it can change anything. It is
	// separate from the boundary rules because it is a preference: nothing
	// enforces it, and a run that ignores it is still a valid run.
	StyleDirective string
	// Effort is how hard the model is asked to think before answering.
	//
	// It is a separate axis from the model itself and a much cheaper one to
	// climb: raising effort on a cheap model multiplies its reasoning tokens,
	// which are billed at the cheap model's rate, while moving to a stronger
	// model multiplies the rate on every token. Empty sends no level and lets
	// the provider decide.
	Effort string
	// RetryPolicy bounds physical attempts, backoff, and concurrency for every
	// request this client makes (PIPE-066a). The zero value is not "no retry":
	// providers.NewRetryExecutor fills every unset field with the same
	// defaults every other provider caller gets (three attempts, jittered
	// exponential backoff, a concurrency ceiling of four), so a caller that
	// says nothing about retries still gets bounded, backed-off, admission
	// controlled requests rather than a bare, unmediated HTTP call.
	RetryPolicy providers.RetryPolicy
}

// Model implements agent.FixedModel.
//
// Every physical request this client makes to the provider passes through
// transport (a providers.HTTPTransport, for bounded, non-redirecting HTTP
// I/O and Retry-After parsing) and retries (a providers.RetryExecutor, for
// bounded attempts, jittered exponential backoff, and the one admission point
// that bounds this client's concurrent requests). Before PIPE-066a this
// client issued a bare options.Client.Do(request) with none of that: no
// retry, no backoff, no Retry-After handling, no concurrency ceiling. That
// port was the only production implementation of agent.FixedModel, so a real
// run had no rate-limit handling of any kind regardless of how completely
// internal/providers and internal/coordinator's budgeted execution service
// implemented it elsewhere — nothing in the real path ever called them.
type Model struct {
	options   Options
	identity  providers.ModelIdentity
	price     providers.PriceSnapshot
	transport *providers.HTTPTransport
	retries   *providers.RetryExecutor
}

// New builds a model client.
func New(options Options) (*Model, error) {
	if strings.TrimSpace(options.APIKey) == "" {
		return nil, errors.New(
			"an OpenAI API key is required; set OPENAI_KEY in .env or the environment")
	}
	if strings.TrimSpace(options.Model) == "" {
		options.Model = DefaultModel
	}
	if strings.TrimSpace(options.Endpoint) == "" {
		options.Endpoint = DefaultEndpoint
	}
	if options.Timeout <= 0 {
		// A model round can legitimately take minutes on a hard task. The bound
		// exists so a hung connection ends the round rather than the run.
		options.Timeout = 5 * time.Minute
	}
	if !options.Price.Input.Known || !options.Price.Output.Known {
		return nil, errors.New(
			"input and output token prices are required; a run cannot enforce a " +
				"budget it cannot price")
	}
	transport, err := providers.NewHTTPTransport(providers.TransportOptions{
		HTTPClient: options.Client, RequestTimeout: options.Timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("build the model's HTTP transport: %w", err)
	}
	retries, err := providers.NewRetryExecutor(
		options.RetryPolicy, noDurableAttemptRecorder{},
	)
	if err != nil {
		return nil, fmt.Errorf("build the model's retry executor: %w", err)
	}
	model := &Model{
		options: options, transport: transport, retries: retries,
		identity: providers.ModelIdentity{
			Provider: providers.ProviderIdentity{
				Adapter: "openai-responses", AdapterVersion: "1",
				Provider: "openai", ProviderVersion: "responses-v1",
			},
			Model: options.Model, Revision: options.Model,
		},
	}
	model.price = providers.PriceSnapshot{
		ID: "configured", Model: model.identity, Price: options.Price,
		Source: "operator-configured",
	}
	return model, nil
}

// Identity reports what this client speaks to.
func (model *Model) Identity() providers.ModelIdentity { return model.identity }

// noDurableAttemptRecorder satisfies providers.AttemptRecorder without
// persisting anything.
//
// A durable per-attempt ledger belongs to the storage-backed execution path
// (providerExecutionService / BudgetedProviderExecutionService in
// internal/coordinator), which owns a *storage.Repositories and a
// domain.TaskID-scoped logical request row neither of which this port has:
// agent.FixedModel.ObserveThink receives only a context and a ModelInput. Not
// recording here does not lose the retry and backoff behaviour those attempts
// still get — the admission ceiling, the backoff schedule, and the
// Retry-After handling are all enforced by providers.RetryExecutor regardless
// of what its recorder does — it only means this client's individual physical
// attempts are not separately queryable after the fact, the same gap that
// existed before this port had any retry executor to call.
type noDurableAttemptRecorder struct{}

func (noDurableAttemptRecorder) PrepareProviderAttempt(
	context.Context, providers.PhysicalAttempt,
) error {
	return nil
}

func (noDurableAttemptRecorder) CompleteProviderAttempt(
	context.Context, providers.AttemptRecord,
) error {
	return nil
}

// ObserveThink runs one round: it sends the observation and returns the tool
// calls the model chose and whether it considers the work done.
//
// Every physical attempt is bounded, backed off, and admitted through
// model.retries (PIPE-066a) rather than issued directly: a 429 or overload
// response is retried with jittered exponential backoff or the provider's own
// Retry-After, and the shared admission point bounds how many requests this
// client has in flight at once and narrows under sustained backpressure,
// exactly as internal/providers.RetryExecutor is tested to do for every other
// caller that already went through it.
func (model *Model) ObserveThink(
	ctx context.Context,
	input agent.ModelInput,
) (agent.ModelTurn, error) {
	wire := model.buildRequest(input)
	body, err := json.Marshal(wire)
	if err != nil {
		return agent.ModelTurn{}, fmt.Errorf("encode the model request: %w", err)
	}
	// What was actually sent, for somebody watching the run.
	//
	// The instruction the coordinator composed is visible in its own trace, and
	// this is the thing that reached the provider: the same text plus the
	// repository context, the plan, the tools, and whatever the previous round
	// left behind. When a run does something inexplicable, the explanation is
	// almost always in the difference between those two.
	traceRequest(model.identity.Model, wire)
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		return agent.ModelTurn{}, err
	}
	hash := sha256.Sum256(body)
	identity := providers.RequestIdentity{
		ModelRequestID: requestID,
		Provider:       model.identity.Provider,
		Model:          model.identity,
		// The Responses API call this client makes is not declared against any
		// provider-native idempotency scope, so Idempotency stays the
		// unsupported zero value and the logical key below is this client's
		// own, used only to satisfy RetryExecutor's identity validation, not
		// to deduplicate against the provider.
		IdempotencyKey: requestID.String(),
		RequestHash:    hex.EncodeToString(hash[:]),
	}

	var reply responsesReply
	_, executeErr := model.retries.Execute(
		ctx, identity,
		func(attemptCtx context.Context, _ providers.PhysicalAttempt) providers.AttemptOutcome {
			var decoded responsesReply
			_, requestErr := model.transport.DoJSON(attemptCtx, providers.HTTPRequest{
				Method:   http.MethodPost,
				Endpoint: model.options.Endpoint,
				Header: http.Header{
					"Content-Type":  []string{"application/json"},
					"Authorization": []string{"Bearer " + model.options.APIKey},
				},
				Body: body,
				// This is the fixed, operator-configured provider endpoint every
				// run has always called; requiring explicit remote approval here
				// would be a new gate this ticket does not add, not a preserved
				// one. See ValidateProviderEndpoint: without this, the transport
				// would refuse the request outright, which is not what any
				// existing run does today.
				EndpointPolicy: providers.EndpointPolicy{AllowRemote: true},
			}, &decoded)
			if requestErr != nil {
				return providers.AttemptOutcome{Err: classifyModelAttemptError(requestErr)}
			}
			reply = decoded
			return providers.AttemptOutcome{}
		},
	)
	if executeErr != nil {
		return agent.ModelTurn{}, executeErr
	}
	turn, err := model.decodeTurn(input, requestID, reply)
	traceReply(model.identity.Model, turn, err)
	return turn, err
}

// traceRequest writes the outgoing prompt to the live trace.
//
// Excerpted rather than printed whole: a request carries the whole repository
// context and the plan, which is thousands of lines the reader already knows,
// and burying the instruction in them is the same as not printing it. The head
// and the tail are where the instruction and the specific ask live, and the
// omitted count keeps the excerpt honest about the size of what was sent.
func traceRequest(model string, request responsesRequest) {
	if !tracing.Enabled() {
		return
	}
	tools := make([]string, 0, len(request.Tools))
	for _, declared := range request.Tools {
		tools = append(tools, declared.Name)
	}
	effort := ""
	if request.Reasoning != nil {
		effort = " effort=" + request.Reasoning.Effort
	}
	schema := ""
	if request.Text != nil {
		schema = " schema=" + request.Text.Format.Name
	}
	tracing.Printf("prompt", "→ %s%s%s tools=[%s]",
		model, effort, schema, strings.Join(tools, " "))
	for _, turn := range request.Input {
		if turn.Role == "system" {
			continue
		}
		tracing.Block("prompt", "  "+turn.Role+":",
			tracing.Excerpt(turn.Content, 24, 12))
	}
}

// traceReply writes what came back.
//
// Tool calls are named with their arguments excerpted, because a file write's
// argument is the whole file and the question a watcher has is which file, not
// what is in it — the file is on disk a moment later either way.
func traceReply(model string, turn agent.ModelTurn, err error) {
	if !tracing.Enabled() {
		return
	}
	if err != nil {
		tracing.Printf("reply", "← %s failed: %s", model, err.Error())
		return
	}
	tracing.Printf("reply", "← %s %d tool call(s), %d token(s) out",
		model, len(turn.ToolCalls), turn.Usage.OutputTokens)
	for _, call := range turn.ToolCalls {
		tracing.Printf("reply", "  calls %s(%s)", call.Call.Name,
			tracing.OneLine(string(call.Call.Arguments), 160))
	}
	if message := strings.TrimSpace(turn.MessageRedacted); message != "" {
		tracing.Block("reply", "  said:", tracing.Excerpt(message, 20, 8))
	}
}

// classifyModelAttemptError turns one physical attempt's transport failure
// into the Failure vocabulary providers.RetryExecutor's decide function reads
// to choose retry, backoff and admission-narrowing behaviour (PIPE-066a;
// PIPE-067's jitter and Retry-After handling; PIPE-069's narrowing on
// backpressure).
//
// A rate limit or server overload is retryable and carries whatever
// Retry-After the provider sent, which the executor honours exactly rather
// than computing its own backoff. An authentication failure and an otherwise
// unrecognised status are not retried: retrying an expired key or a malformed
// request produces the same refusal every time, so spending attempts on it
// only delays reporting the failure that was already final. A transport-level
// error below the HTTP status line — a dropped connection, a timeout, a
// context cancellation reaching the wire — is retried the same way any other
// provider transport hiccup is.
//
// Cause is always the joined pair of the descriptive provider message and
// the package sentinel for the chosen Kind, never the message alone. Failure
// itself only prioritises Cause over synthesising a sentinel from Kind when
// Cause is non-nil (see Failure.Unwrap), so a Cause carrying just the prose
// message would silently break every errors.Is(err, providers.ErrRateLimited)
// check the retry executor and its caller make — RetryExecutor's own
// decide() decides "retry or not" that way, not by Kind.
func classifyModelAttemptError(err error) error {
	var status *providers.HTTPStatusError
	if errors.As(err, &status) {
		cause := modelStatusError(status)
		switch {
		case status.StatusCode == http.StatusTooManyRequests:
			return &providers.Failure{
				Kind: providers.FailureRateLimited, Operation: "call the model",
				Retryable: true, RetryAfter: status.RetryAfter,
				Cause: errors.Join(cause, providers.ErrRateLimited),
			}
		case status.StatusCode >= http.StatusInternalServerError:
			return &providers.Failure{
				Kind: providers.FailureUnavailable, Operation: "call the model",
				Retryable: true, RetryAfter: status.RetryAfter,
				Cause: errors.Join(cause, providers.ErrUnavailable),
			}
		case status.StatusCode == http.StatusUnauthorized ||
			status.StatusCode == http.StatusForbidden:
			return &providers.Failure{
				Kind: providers.FailureAuthentication, Operation: "call the model",
				Retryable: false, Cause: errors.Join(cause, providers.ErrAuthentication),
			}
		default:
			return &providers.Failure{
				Kind: providers.FailureInvalidRequest, Operation: "call the model",
				Retryable: false, Cause: errors.Join(cause, providers.ErrInvalidRequest),
			}
		}
	}
	// Not a status the provider sent back at all: a dial failure, a timeout, a
	// canceled context. RetryExecutor's own decide() checks ctx.Err() and
	// context.Canceled ahead of consulting Retryable, so a canceled or
	// deadline-exceeded run still ends as AttemptCanceled regardless of this
	// classification — this only governs genuine transport hiccups. err is
	// joined rather than substituted so a wrapped context.Canceled or
	// context.DeadlineExceeded inside it still satisfies errors.Is downstream.
	return &providers.Failure{
		Kind: providers.FailureTransport, Operation: "call the model",
		Retryable: true, Cause: errors.Join(err, providers.ErrTransport),
	}
}

// modelStatusError reports a refusal in the provider's own words.
//
// The message is what the provider said, bounded, because "the model call
// failed" is the same sentence for an expired key, a rate limit, and a model
// name that does not exist — three different things to do about it.
func modelStatusError(status *providers.HTTPStatusError) error {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	_ = status.DecodeBody(&envelope)
	message := strings.TrimSpace(envelope.Error.Message)
	if message == "" {
		// No structured error field, or a non-JSON body: fall back to the raw
		// text exactly as the pre-PIPE-066a version did, passed through
		// unchanged rather than through a real redaction policy, since it is
		// the provider's own error text about this request, not user content.
		if raw, summaryErr := status.RedactedBodySummary(
			func(value string) string { return value },
		); summaryErr == nil {
			message = strings.TrimSpace(raw)
		}
	}
	const bound = 512
	if len(message) > bound {
		message = message[:bound] + "…"
	}
	if message == "" {
		return fmt.Errorf("the model refused the request with status %d", status.StatusCode)
	}
	return fmt.Errorf("the model refused the request (%d): %s", status.StatusCode, message)
}

// decodeTurn reads the provider's answer into the loop's vocabulary.
func (model *Model) decodeTurn(
	input agent.ModelInput,
	requestID domain.ModelRequestID,
	response responsesReply,
) (agent.ModelTurn, error) {
	turn := agent.ModelTurn{
		ModelRequestID: requestID,
		Model:          model.identity,
		Usage: providers.Usage{
			Known: response.Usage.TotalTokens > 0,
			// The provider counted these; nothing here estimates. An estimated
			// count recorded as measured would corrupt every cost comparison
			// made against it afterwards.
			Source:          providers.UsageSourceProvider,
			InputTokens:     response.Usage.InputTokens,
			OutputTokens:    response.Usage.OutputTokens,
			ReasoningTokens: response.Usage.OutputTokensDetails.ReasoningTokens,
			CachedInputTokens: response.Usage.InputTokensDetails.
				CachedTokens,
		},
	}
	// The cost is computed from the provider's own token counts against the
	// configured rate, in exact rational arithmetic. Nothing here rounds, and
	// nothing here estimates.
	turn.Cost = model.price.Cost(turn.Usage)
	var narration []string
	// Steps claimed earlier in this same turn. A step is completed by one tool
	// call, so two calls in one round cannot share one: the second would be
	// bound to a step the first had already finished, and the loop would refuse
	// the whole turn as malformed — ending a run that was going perfectly well.
	claimed := map[string]bool{}
	for _, item := range response.Output {
		switch item.Type {
		case "function_call":
			call, callErr := toolCallOf(item)
			if callErr != nil {
				return agent.ModelTurn{}, callErr
			}
			step := planStepFor(input, call.Name, call.Arguments, claimed)
			if step == "" {
				// Nothing open can accept this call. Dropping it costs one
				// wasted decision; reporting it would cost the whole run.
				continue
			}
			claimed[step] = true
			turn.ToolCalls = append(turn.ToolCalls, agent.ModelToolCall{
				Call: call, PlanStepID: step,
			})
		case "message":
			for _, part := range item.Content {
				if text := strings.TrimSpace(part.Text); text != "" {
					narration = append(narration, text)
				}
			}
		}
	}
	// Completion is read from what the model did, not from a field it could
	// assert. A round with no tool calls and something to say is a round that
	// believes it is finished; the loop above still decides whether it is.
	// What the model said is carried on the turn as well as scanned. It was
	// scanned for a completion keyword and then discarded, so a caller that
	// asked the model a question rather than asking it to work had no way to
	// read the answer.
	turn.MessageRedacted = strings.Join(narration, "\n")
	if len(turn.ToolCalls) == 0 {
		turn.Completion = completionOf(turn.MessageRedacted)
	}
	return turn, nil
}

// toolCallOf converts one function call.
func toolCallOf(item responsesOutput) (providers.ToolCall, error) {
	name := strings.TrimSpace(item.Name)
	if name == "" {
		return providers.ToolCall{}, errors.New("the model requested a tool with no name")
	}
	arguments := json.RawMessage(item.Arguments)
	if len(bytes.TrimSpace(arguments)) == 0 {
		arguments = json.RawMessage("{}")
	}
	if !json.Valid(arguments) {
		// Malformed arguments are refused rather than repaired. Guessing what
		// the model meant is how a write lands on the wrong path.
		return providers.ToolCall{}, fmt.Errorf(
			"the model requested %s with arguments that are not valid JSON", name)
	}
	identifier := item.CallID
	if strings.TrimSpace(identifier) == "" {
		identifier = item.ID
	}
	arguments = canonicalizePathArguments(arguments)
	return providers.ToolCall{ID: identifier, Name: name, Arguments: arguments}, nil
}

// canonicalizePathArguments rewrites a path to its canonical relative spelling.
//
// The loop refuses a path that is not already canonical, and a model will
// write "./cmd", "cmd/", and "cmd" for the same directory. Normalizing the
// spelling is not guessing at intent: it is the same path written the one way
// the rest of the system accepts. Anything that is not merely a spelling
// difference — an absolute path, a parent traversal — is left exactly as it
// came so it is still refused.
func canonicalizePathArguments(arguments json.RawMessage) json.RawMessage {
	var fields map[string]any
	if err := json.Unmarshal(arguments, &fields); err != nil {
		return arguments
	}
	changed := false
	for _, key := range []string{"path", "file", "directory"} {
		raw, present := fields[key]
		if !present {
			continue
		}
		value, ok := raw.(string)
		if !ok {
			continue
		}
		canonical := path.Clean(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")))
		if canonical == value || canonical == "." || canonical == ".." ||
			strings.HasPrefix(canonical, "../") || strings.HasPrefix(canonical, "/") {
			continue
		}
		fields[key] = canonical
		changed = true
	}
	if !changed {
		return arguments
	}
	rewritten, err := json.Marshal(fields)
	if err != nil {
		return arguments
	}
	return rewritten
}

// completionOf reads a completion signal out of what the model said.
func completionOf(narration string) agent.CompletionSignal {
	lowered := strings.ToLower(narration)
	switch {
	case strings.Contains(lowered, "needs direction"),
		strings.Contains(lowered, "cannot proceed"),
		strings.Contains(lowered, "need more information"):
		return agent.CompletionNeedsDirection
	case narration == "":
		// Nothing done and nothing said is not a claim of completion. Treating
		// it as one would end a run that had simply stalled.
		return agent.CompletionNone
	default:
		return agent.CompletionImplementationComplete
	}
}

// planStepFor names the step a tool call belongs to.
//
// A step is scoped to the files it may touch and is completed by one tool, so
// the binding matches on both: first an open step that declares this exact
// path, then an open step that accepts this tool at all. Matching on the tool
// alone bound a write of the second file to the step that owned the first, and
// the loop refused it for being outside that step's scope — correctly.
func planStepFor(
	input agent.ModelInput,
	tool string,
	arguments json.RawMessage,
	claimed map[string]bool,
) string {
	requested := pathArgumentOf(arguments)
	var toolMatch string
	for _, step := range input.Plan.Steps {
		if step.State == agent.StepImplemented || step.State == agent.StepValidated ||
			step.State == agent.StepSkipped || claimed[step.ID] {
			continue
		}
		accepts := false
		for _, completion := range step.CompletionTools {
			if string(completion) == tool {
				accepts = true
				break
			}
		}
		if !accepts {
			continue
		}
		if requested != "" {
			for _, expected := range step.ExpectedFiles {
				if expected == requested {
					return step.ID
				}
			}
		}
		if toolMatch == "" {
			toolMatch = step.ID
		}
	}
	return toolMatch
}

// pathArgumentOf reads the path a call names, if it names one.
func pathArgumentOf(arguments json.RawMessage) string {
	var fields map[string]any
	if err := json.Unmarshal(arguments, &fields); err != nil {
		return ""
	}
	value, _ := fields["path"].(string)
	return value
}

// Price reports the rates this client was configured with.
//
// The coordinator registers a provider's declared rates alongside the requests
// made against it, so that what a run was charged can be checked later against
// what it was told the rates were. Only the operator knows them, so they are
// reported rather than discovered.
func (model *Model) Price() providers.TokenPrice { return model.price.Price }
