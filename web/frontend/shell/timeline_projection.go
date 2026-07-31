package shell

import (
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
)

var timelineFixtureEpoch = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

func timelineCardsForMessages(messages []state.MessageView) []timelinecard.Card {
	cards := make([]timelinecard.Card, 0, len(messages))
	for _, message := range messages {
		cards = append(cards, timelineCardForMessage(message))
	}
	return cards
}

func timelineCardForMessage(message state.MessageView) timelinecard.Card {
	occurredAt := timelineFixtureEpoch.Add(time.Duration(message.Sequence) * time.Minute)
	card := timelinecard.Card{
		Sequence:   message.Sequence,
		StableKey:  safeTimelineKey(message),
		OccurredAt: occurredAt,
	}
	switch strings.ToLower(strings.TrimSpace(message.Role)) {
	case "requirement":
		card.Kind = timelinecard.KindRequirement
		card.Requirement = &timelinecard.Requirement{
			Goal: message.Body,
			Constraints: []string{
				"Use typed Go and GoWebComponents v5",
				"Keep correctness-bearing state inspectable",
			},
			ExplicitIdentities: []string{"docs/plan.md", "TODOS.md"},
			RiskCues:           []string{"Deep-route startup", "focus restoration", "responsive state"},
		}
	case "forecast":
		card.Kind = timelinecard.KindForecast
		card.Forecast = &timelinecard.Forecast{
			Model: "GPT-5", Effort: "High", ValidationProfile: "Strict",
			UncertaintyReasons: []string{message.Body},
		}
	case "plan":
		card.Kind = timelinecard.KindPlan
		card.Plan = &timelinecard.Plan{
			Revision: 1, Summary: message.Body,
			Scope: []string{"Frontend shell", "Browser acceptance evidence"},
			Steps: []timelinecard.PlanStep{
				{Ordinal: 1, Title: "Design tokens and route shell", Status: timelinecard.PlanStepComplete},
				{Ordinal: 2, Title: "Typed timeline and composer", Status: timelinecard.PlanStepActive},
				{Ordinal: 3, Title: "Adversarial browser refinement", Status: timelinecard.PlanStepPending},
			},
			ExpectedFiles:      []string{"web/frontend", "web/client"},
			PlannedChecks:      []string{"Go tests", "WASM build", "browser E2E", "visual comparison"},
			Assumptions:        []string{"The loopback coordinator remains the only network origin"},
			CompletionCriteria: []string{"All milestone gates pass with replay-safe state"},
		}
	case "execution":
		output, _ := timelinecard.NewRedactedOutput(
			"Current generated-build activity",
			"Rendering typed components\nBuilding the WASM client\nRunning focused browser assertions",
			48,
			512,
		)
		card.Kind = timelinecard.KindTool
		card.Tool = &timelinecard.ToolActivity{
			ExecutionID: "execution-m17", Tool: "codeflux-dev",
			Purpose: message.Body, Scope: "web/frontend", State: "running",
			Duration: 54 * time.Second, ExitStatus: "pending",
			Summary: "The generated frontend is rebuilding from the current Go source.",
			Output:  output,
		}
	case "validation":
		output, _ := timelinecard.NewRedactedOutput(
			"Validation evidence",
			"go test ./web/frontend/... PASS\nWASM build PASS\nBrowser E2E running",
			48,
			512,
		)
		card.Kind = timelinecard.KindValidation
		card.Validation = &timelinecard.Validation{
			ID: "m17-frontend-gates", Status: timelinecard.ValidationRunning,
			Required: true, Summary: message.Body,
			RevisionBinding: "current worktree", Baseline: "origin/main",
			Output: output,
		}
	default:
		status := timelinecard.MessageComplete
		if message.Pending {
			status = timelinecard.MessageProvisional
		}
		card.Kind = timelinecard.KindMessage
		card.Message = &timelinecard.Message{
			ID: message.ID, Role: message.Role, Body: message.Body,
			Status: status, OccurredAt: occurredAt,
		}
	}
	return card
}

func safeTimelineKey(message state.MessageView) string {
	if strings.TrimSpace(message.ID) != "" {
		return message.ID
	}
	return fmt.Sprintf("sequence:%d", message.Sequence)
}
