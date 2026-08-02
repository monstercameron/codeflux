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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
)

// DefaultEndpoint is the Responses API.
const DefaultEndpoint = "https://api.openai.com/v1/responses"

// DefaultModel is the model a run uses unless told otherwise.
const DefaultModel = "gpt-5.6-luna"

// maximumResponseBytes bounds what is read back.
//
// A response is parsed into memory, so an unbounded read is an unbounded
// allocation driven by a remote party.
const maximumResponseBytes = 8 << 20

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
}

// Model implements agent.FixedModel.
type Model struct {
	options  Options
	identity providers.ModelIdentity
	price    providers.PriceSnapshot
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
	if options.Client == nil {
		options.Client = &http.Client{Timeout: options.Timeout}
	}
	if !options.Price.Input.Known || !options.Price.Output.Known {
		return nil, errors.New(
			"input and output token prices are required; a run cannot enforce a " +
				"budget it cannot price")
	}
	model := &Model{
		options: options,
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

// ObserveThink runs one round: it sends the observation and returns the tool
// calls the model chose and whether it considers the work done.
func (model *Model) ObserveThink(
	ctx context.Context,
	input agent.ModelInput,
) (agent.ModelTurn, error) {
	body, err := json.Marshal(model.buildRequest(input))
	if err != nil {
		return agent.ModelTurn{}, fmt.Errorf("encode the model request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, model.options.Endpoint, bytes.NewReader(body))
	if err != nil {
		return agent.ModelTurn{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+model.options.APIKey)

	response, err := model.options.Client.Do(request)
	if err != nil {
		// The URL is included but the key is in a header, so it cannot reach a
		// log through this error.
		return agent.ModelTurn{}, fmt.Errorf("call the model: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	payload, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes))
	if err != nil {
		return agent.ModelTurn{}, fmt.Errorf("read the model response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return agent.ModelTurn{}, modelStatusError(response.StatusCode, payload)
	}
	return model.decodeTurn(input, payload)
}

// modelStatusError reports a refusal in the provider's own words.
//
// The message is what the provider said, bounded, because "the model call
// failed" is the same sentence for an expired key, a rate limit, and a model
// name that does not exist — three different things to do about it.
func modelStatusError(status int, payload []byte) error {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(payload, &envelope)
	message := strings.TrimSpace(envelope.Error.Message)
	if message == "" {
		message = strings.TrimSpace(string(payload))
	}
	const bound = 512
	if len(message) > bound {
		message = message[:bound] + "…"
	}
	if message == "" {
		return fmt.Errorf("the model refused the request with status %d", status)
	}
	return fmt.Errorf("the model refused the request (%d): %s", status, message)
}

// decodeTurn reads the provider's answer into the loop's vocabulary.
func (model *Model) decodeTurn(
	input agent.ModelInput,
	payload []byte,
) (agent.ModelTurn, error) {
	var response responsesReply
	if err := json.Unmarshal(payload, &response); err != nil {
		return agent.ModelTurn{}, fmt.Errorf("decode the model response: %w", err)
	}
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		return agent.ModelTurn{}, err
	}
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
