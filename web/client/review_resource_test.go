package main

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/web/frontend/diffreview"
)

func TestParseReviewUnifiedDiffPreservesTypedProvenanceAndLineNumbers(t *testing.T) {
	path, err := diffreview.NewFilePath("internal/review/service.go")
	if err != nil {
		t.Fatal(err)
	}
	eventID, err := domain.NewEventID()
	if err != nil {
		t.Fatal(err)
	}
	validationID, err := domain.NewValidationID()
	if err != nil {
		t.Fatal(err)
	}
	links := reviewFileLinks{
		steps:  []taskgraph.PlanStepLink{{PlanRevision: 7, StepID: "mount-review"}},
		events: []domain.EventID{eventID},
		validations: []diffreview.ValidationLink{{
			ID: validationID, Label: "go test", State: domain.ValidationStatePassed,
		}},
	}
	hunks, err := parseReviewUnifiedDiff(
		"diff --git a/internal/review/service.go b/internal/review/service.go\n"+
			"--- a/internal/review/service.go\n+++ b/internal/review/service.go\n"+
			"@@ -10,2 +10,3 @@ func review()\n context\n-old\n+new\n+more\n",
		[]diffreview.ChangedFile{{Path: path}},
		map[string]reviewFileLinks{path.String(): links},
	)
	if err != nil {
		t.Fatalf("parse review diff: %v", err)
	}
	got := hunks[path.String()]
	if len(got) != 1 || len(got[0].Lines) != 4 {
		t.Fatalf("hunks = %#v", got)
	}
	if got[0].PlanSteps[0] != links.steps[0] || got[0].ToolEventIDs[0] != eventID || got[0].Validations[0].ID != validationID {
		t.Fatalf("typed provenance was not preserved: %#v", got[0])
	}
	if !got[0].Lines[2].NewLineNumberKnown || got[0].Lines[2].NewLineNumber != 11 || got[0].Lines[2].OldLineNumberKnown {
		t.Fatalf("addition line attribution = %#v", got[0].Lines[2])
	}
}
