package openaimodel

import (
	"encoding/json"
	"strings"

	"codeflux.dev/codeflux/internal/agent"
)

// responsesRequest is the Responses API request this client sends.
type responsesRequest struct {
	Model      string          `json:"model"`
	Input      []responsesTurn `json:"input"`
	Tools      []responsesTool `json:"tools,omitempty"`
	ToolChoice string          `json:"tool_choice,omitempty"`
	Store      bool            `json:"store"`
	// Reasoning is how hard the model is asked to think before answering. It
	// is omitted rather than defaulted when nothing asked for a level, because
	// sending a level the caller did not choose would silently change what
	// every request costs.
	Reasoning *responsesReasoning `json:"reasoning,omitempty"`
	// Text carries a schema the answer must satisfy, when the caller asked for
	// one. Omitted otherwise, because a request that names a format the caller
	// never chose would change what the model is allowed to say.
	Text *responsesText `json:"text,omitempty"`
}

// responsesText is the Responses API's output-format field.
type responsesText struct {
	Format responsesFormat `json:"format"`
}

// responsesFormat pins the answer to a JSON schema.
//
// Strict, always. A non-strict schema is a suggestion, and a suggestion is
// what the prose instructions already were: "answer with one behaviour per
// line and nothing else" produced numbered lines, headings and bullets often
// enough that the parser that reads it back has a case for each.
type responsesFormat struct {
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

// responsesReasoning carries the effort level for one request.
type responsesReasoning struct {
	Effort string `json:"effort"`
}

// reasoningFor renders a configured effort level, or nothing.
//
// Nothing is not the same as a default. A request that carries no level lets
// the provider apply its own; one that carries a level the caller never chose
// would change what every round costs while reading, in the configuration, as
// though nobody had asked for anything.
func reasoningFor(effort string) *responsesReasoning {
	if strings.TrimSpace(effort) == "" {
		return nil
	}
	return &responsesReasoning{Effort: effort}
}

// responsesTurn is one message in the conversation sent to the model.
type responsesTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// responsesTool is one function the model may call.
type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// responsesReply is the answer.
type responsesReply struct {
	ID     string            `json:"id"`
	Output []responsesOutput `json:"output"`
	Usage  responsesUsage    `json:"usage"`
}

type responsesOutput struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Content   []responsesPart `json:"content"`
}

type responsesPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesUsage struct {
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	TotalTokens        int64 `json:"total_tokens"`
	InputTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

// systemInstruction is what the model is told it is doing.
//
// It states the boundary rather than describing a persona: the agent works in
// an isolated Git worktree, it may only act through the declared tools, and it
// says when it is done rather than padding a round. Everything it is not
// allowed to do is enforced above this text, because instructions are a request
// and the tool authority is the actual limit.
const systemInstruction = `You are a software engineer working inside an isolated Git worktree.

Rules:
- Act only through the tools you are given. You have no other way to affect anything.
- Every path is relative to the worktree root, with no leading "./", no trailing slash, no "..", and no absolute paths. Write "cmd/tool/main.go", not "./cmd/tool/main.go".
- A path is never empty and never ".". The worktree root itself cannot be named; to see it, read the repository context you were given.
- Read before you write. Do not guess a file's contents.
- Write complete, working code. No placeholders, no TODOs, no elided bodies.
- Match the conventions already in the repository: its naming, its error handling, its comment density.
- When the work is finished, stop calling tools and reply with one short paragraph saying what you changed.
- If you cannot proceed without a decision only a person can make, stop calling tools and reply beginning with "Needs direction:".

Each round you are given one JSON object describing where the work stands. Its fields are:
- round: which round this is.
- repository_context: files you have been shown, each {path, content}. The content is exactly what is in the file.
- plan: the steps, each {state, summary, files}. Work them in order.
- what_has_happened: durable facts recorded so far.
- results_of_your_last_tool_calls: each {tool, state, exit_code, summary, stdout, stderr}.
- tools_you_may_call: the only tools available this round.

Never wrap code in backticks or a Markdown fence when you send it to a tool. A tool argument is the literal content: source goes to the write tool exactly as it should appear in the file, and a patch goes to the patch tool exactly as its format describes. Fences are refused, and they cost you the round.`

// buildRequest turns one observation into one request.
func (model *Model) buildRequest(input agent.ModelInput) responsesRequest {
	request := responsesRequest{
		Model: model.options.Model,
		Input: []responsesTurn{
			{Role: "system", Content: model.instruction()},
			{Role: "user", Content: observationJSON(input)},
		},
		// The model must not be forced to call a tool: a round where the right
		// answer is "this is finished" has to be expressible, or the run can
		// never end on its own.
		ToolChoice: "auto",
		Reasoning:  reasoningFor(model.options.Effort),
		// Nothing is stored on the provider's side. The durable record of this
		// run is the local journal, and a second copy off the machine is
		// exactly what this product promises not to make.
		Store: false,
	}
	// A request that must answer in a schema declares no tools and makes no
	// tool choice. The two are alternatives, not companions: an answer
	// constrained to a JSON object cannot also be a function call, and a
	// provider handed both has to pick one, which is a decision this code
	// should be making rather than discovering.
	if schema := input.StructuredOutput; schema != nil {
		request.ToolChoice = ""
		request.Text = &responsesText{Format: responsesFormat{
			Type:   "json_schema",
			Name:   schema.Name,
			Schema: schema.Schema,
			Strict: true,
		}}
		return request
	}
	// Only tools an open plan step can accept are declared. A tool offered
	// while no step can receive its result is a round the loop will reject: it
	// let a run open by testing the whole repository, which completed the one
	// verification step, buried the model in unrelated output, and left the
	// actual work with nowhere to land.
	for _, declaration := range input.ApprovedTools {
		if !toolIsCurrentlyUsable(input, declaration.Name) {
			continue
		}
		request.Tools = append(request.Tools, responsesTool{
			Type: "function", Name: declaration.Name,
			Description: declaration.Description,
			Parameters:  declaration.InputSchema,
		})
	}
	return request
}

// toolIsCurrentlyUsable reports whether an open step can accept this tool.
func toolIsCurrentlyUsable(input agent.ModelInput, tool string) bool {
	for _, step := range input.Plan.Steps {
		if step.State == agent.StepImplemented || step.State == agent.StepValidated ||
			step.State == agent.StepSkipped {
			continue
		}
		for _, completion := range step.CompletionTools {
			if string(completion) == tool {
				return true
			}
		}
	}
	return false
}

// instruction is the whole system message for one run.
//
// The boundary rules are fixed; the house style is not. A project decides how
// its code should be shaped, and that decision has to reach the model at the
// moment it is writing, not be discovered at review. Appending it here rather
// than folding it into the fixed text keeps the two separable: the rules are
// what the run may do, the style is what the project prefers, and only one of
// them is enforced elsewhere.
func (model *Model) instruction() string {
	style := strings.TrimSpace(model.options.StyleDirective)
	if style == "" {
		return systemInstruction
	}
	return systemInstruction + "\n\nHouse style:\n" + style
}
