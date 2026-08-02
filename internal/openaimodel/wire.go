package openaimodel

import (
	"encoding/json"
	"fmt"
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
- If you cannot proceed without a decision only a person can make, stop calling tools and reply beginning with "Needs direction:".`

// buildRequest turns one observation into one request.
func (model *Model) buildRequest(input agent.ModelInput) responsesRequest {
	request := responsesRequest{
		Model: model.options.Model,
		Input: []responsesTurn{
			{Role: "system", Content: model.instruction()},
			{Role: "user", Content: observationText(input)},
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

// observationText renders the round's observation.
//
// It is assembled here rather than accumulated across rounds because the loop
// hands over a complete bounded observation each time: what the task is, what
// the plan is, what has happened, and what the last tools returned. A client
// keeping its own running transcript would drift from the record the
// coordinator is keeping, and the coordinator's is the one people read.
func observationText(input agent.ModelInput) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Round %d.\n\n", input.Round)

	if len(input.RepositoryContext) > 0 {
		builder.WriteString("Repository context:\n")
		for _, item := range input.RepositoryContext {
			fmt.Fprintf(&builder, "- %s\n", describeContext(item))
		}
		builder.WriteString("\n")
	}
	if len(input.Plan.Steps) > 0 {
		builder.WriteString("Plan:\n")
		for _, step := range input.Plan.Steps {
			fmt.Fprintf(&builder, "- %s\n", describeStep(step))
		}
		builder.WriteString("\n")
	}
	if len(input.FactualEvents) > 0 {
		builder.WriteString("What has happened:\n")
		for _, event := range input.FactualEvents {
			fmt.Fprintf(&builder, "- %s\n", describeEvent(event))
		}
		builder.WriteString("\n")
	}
	if len(input.PreviousResults) > 0 {
		builder.WriteString("Results of your last tool calls:\n")
		for _, result := range input.PreviousResults {
			fmt.Fprintf(&builder, "%s\n", describeFeedback(result))
		}
		builder.WriteString("\n")
	}
	builder.WriteString(
		"Work through the plan in order. Only the tools the next open step can " +
			"accept are available to you. Call them, or reply that the work is " +
			"finished.")
	return builder.String()
}

// describeContext renders one repository file the loop selected.
func describeContext(item agent.RepositoryContextItem) string {
	content := strings.TrimSpace(item.ContentRedacted)
	if content == "" {
		return item.Path
	}
	return item.Path + "\n```\n" + content + "\n```"
}

// describeStep renders one plan step.
//
// The state leads the line and the files follow on their own. It used to be
// appended as "(pending)" straight after the summary, and a step summary ends
// with the requirement's own words: a request to print "Hello" was rendered as
// "...print Hello (pending)" and the model printed exactly that, parenthetical
// included. Nothing may abut the free text on the right, because free text is
// the one field whose ending cannot be predicted.
func describeStep(step agent.PlanStep) string {
	summary := strings.TrimSpace(step.SummaryRedacted)
	if summary == "" {
		summary = string(step.Kind)
	}
	line := fmt.Sprintf("[%s] %s", step.State, summary)
	if len(step.ExpectedFiles) > 0 {
		line += "\n  files: " + strings.Join(step.ExpectedFiles, ", ")
	}
	return line
}

// describeEvent renders one durable fact.
func describeEvent(event agent.FactualEvent) string {
	summary := strings.TrimSpace(event.SummaryRedacted)
	if summary == "" {
		return event.Type
	}
	return event.Type + ": " + summary
}

// describeFeedback renders what one tool actually did.
//
// Output and error text are both included when present, because a model told
// only that a command failed will guess at why, and the guess is usually a
// second failing command.
func describeFeedback(result agent.ToolFeedback) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "- %s (%s", result.Tool, result.State)
	if result.IsError || result.ExitCode != 0 {
		fmt.Fprintf(&builder, ", exit %d", result.ExitCode)
	}
	builder.WriteString(")")
	if summary := strings.TrimSpace(result.SummaryRedacted); summary != "" {
		builder.WriteString(": " + summary)
	}
	for label, text := range map[string]string{
		"stdout": result.StdoutRedacted, "stderr": result.StderrRedacted,
	} {
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			fmt.Fprintf(&builder, "\n  %s:\n```\n%s\n```", label, trimmed)
		}
	}
	if result.Truncated {
		builder.WriteString("\n  (output truncated)")
	}
	return builder.String()
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
