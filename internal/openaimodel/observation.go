package openaimodel

import (
	"encoding/json"
	"strings"

	"codeflux.dev/codeflux/internal/agent"
)

// observation is the JSON document one round hands to the model.
//
// It replaces a prose message assembled from string concatenation, and the
// reason is not tidiness. The prose version fenced repository files and tool
// output in triple backticks to separate untrusted content from the
// instruction, and a fence is closable by the content it is fencing: a file
// holding three backticks ends the block, and everything after it reads as
// instruction. JSON has no such hole — a string is a string however many
// backticks are in it, and the escaping is the language's rather than ours.
//
// It also stops the prompt teaching a habit the tools then refuse. A model
// shown fenced code every round writes fenced code back, and the write tool
// rejects it; ladder rung 6 spent four turns on exactly that, and the prompt
// was the one demonstrating the format.
//
// Field names are chosen to be read by a model rather than by this package.
// "acceptance_examples" says what it is; "AcceptanceExamplesRedacted" would say
// what our storage layer calls it.
type observation struct {
	Round             uint32               `json:"round"`
	RepositoryContext []observedFile       `json:"repository_context,omitempty"`
	Plan              []observedStep       `json:"plan,omitempty"`
	History           []string             `json:"what_has_happened,omitempty"`
	LastToolResults   []observedToolResult `json:"results_of_your_last_tool_calls,omitempty"`
	AvailableTools    []string             `json:"tools_you_may_call,omitempty"`
	WhatToDoNow       string               `json:"what_to_do_now"`
}

// observedFile is one repository file the loop chose to show.
type observedFile struct {
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
}

// observedStep is one plan step, as the model needs to see it.
type observedStep struct {
	State   string   `json:"state"`
	Summary string   `json:"summary"`
	Files   []string `json:"files,omitempty"`
}

// observedToolResult is what one tool did last round.
//
// Stdout and stderr are separate fields rather than one blob, because they mean
// different things and a model reading a single stream has to guess which it is
// looking at.
type observedToolResult struct {
	Tool      string `json:"tool"`
	State     string `json:"state"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Stdout    string `json:"stdout,omitempty"`
	Stderr    string `json:"stderr,omitempty"`
	Truncated bool   `json:"output_truncated,omitempty"`
}

// observationJSON renders the round as the document the model receives.
//
// Indented, because it is read by something that reads indentation, and the
// tokens an indent costs are repaid the first time a nested field is
// unambiguous at a glance.
func observationJSON(input agent.ModelInput) string {
	document := observation{
		Round: input.Round,
		// The writable files are named every round, not only in the refusal
		// after a write lands somewhere it should not have.
		//
		// repository_context holds files the run may read and files it may
		// write, and nothing in the document told them apart. On a generated
		// workspace the difference matters immediately: the module's stub
		// main.go sits in that list beside the files the plan owns, and it is a
		// main package in a project being asked for a program, so it reads as
		// the obvious place to write. Runs edited it repeatedly, were refused,
		// and spent the round finding out what they could have been told at the
		// start of it.
		WhatToDoNow: "Work through the plan in order. " +
			agent.WritableFilesAdvice(input.Plan) +
			" Only the tools listed in tools_you_may_call are available this " +
			"round. Call one, or reply that the work is finished.",
	}
	for _, item := range input.RepositoryContext {
		document.RepositoryContext = append(document.RepositoryContext,
			observedFile{
				Path:    item.Path,
				Content: strings.TrimSpace(item.ContentRedacted),
			})
	}
	for _, step := range input.Plan.Steps {
		summary := strings.TrimSpace(step.SummaryRedacted)
		if summary == "" {
			summary = string(step.Kind)
		}
		document.Plan = append(document.Plan, observedStep{
			State:   string(step.State),
			Summary: summary,
			Files:   step.ExpectedFiles,
		})
	}
	for _, event := range input.FactualEvents {
		if summary := strings.TrimSpace(event.SummaryRedacted); summary != "" {
			document.History = append(document.History,
				event.Type+": "+summary)
			continue
		}
		document.History = append(document.History, event.Type)
	}
	for _, result := range input.PreviousResults {
		document.LastToolResults = append(document.LastToolResults,
			observedToolResult{
				Tool:      result.Tool,
				State:     result.State,
				ExitCode:  result.ExitCode,
				Summary:   strings.TrimSpace(result.SummaryRedacted),
				Stdout:    strings.TrimSpace(result.StdoutRedacted),
				Stderr:    strings.TrimSpace(result.StderrRedacted),
				Truncated: result.Truncated,
			})
	}
	for _, declaration := range input.ApprovedTools {
		if toolIsCurrentlyUsable(input, declaration.Name) {
			document.AvailableTools = append(
				document.AvailableTools, declaration.Name)
		}
	}

	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		// Nothing here can fail to marshal: every field is a string, a number,
		// a bool or a slice of those. If that ever changes, a round with no
		// observation is worse than one with a plain sentence, so there is a
		// sentence.
		return "The observation for this round could not be encoded. Read the " +
			"repository and continue from the plan."
	}
	return string(encoded)
}
