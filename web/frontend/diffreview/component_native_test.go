//go:build !js

package diffreview

import (
	"testing"

	taskgraph "codeflux.dev/codeflux/internal/graph"
)

func TestNativeDiffReviewDispatchesOnlyValidatedSelections(t *testing.T) {
	props := diffReviewFixture(t)
	var selected string
	props.OnSelectFile = func(path string) { selected = path }

	if !SelectFile(props, "internal/service/handler_test.go") || selected != "internal/service/handler_test.go" {
		t.Fatalf("valid selection was not dispatched, got %q", selected)
	}

	selected = ""
	if SelectFile(props, "does/not/exist.go") || selected != "" {
		t.Fatalf("unlisted path dispatched a selection: %q", selected)
	}

	invalid := props
	invalid.BaseRevision = "bad"
	if SelectFile(invalid, "internal/service/handler_test.go") {
		t.Fatal("selection dispatched against invalid props")
	}

	noHandler := props
	noHandler.OnSelectFile = nil
	if SelectFile(noHandler, "internal/service/handler_test.go") {
		t.Fatal("selection dispatched without a handler")
	}
}

func TestNativeDiffReviewTogglesCategoryFilterAndWhitespace(t *testing.T) {
	props := diffReviewFixture(t)
	var toggledCategory FileCategory
	var toggledState bool
	props.OnToggleCategory = func(category FileCategory, active bool) {
		toggledCategory, toggledState = category, active
	}

	if !ToggleCategory(props, FileCategoryTest) {
		t.Fatal("category toggle was not dispatched")
	}
	if toggledCategory != FileCategoryTest || toggledState != true {
		t.Fatalf("category toggle = (%s, %v), want (test, true)", toggledCategory, toggledState)
	}

	if ToggleCategory(props, FileCategory("invented")) {
		t.Fatal("unknown category was dispatched")
	}

	var whitespaceState bool
	whitespaceCalled := false
	props.OnToggleWhitespace = func(visible bool) { whitespaceState, whitespaceCalled = visible, true }
	if !ToggleWhitespace(props) || !whitespaceCalled || whitespaceState != true {
		t.Fatalf("whitespace toggle = (%v, called=%v), want (true, true)", whitespaceState, whitespaceCalled)
	}

	props.WhitespaceVisible = true
	whitespaceCalled = false
	props.OnToggleWhitespace = func(visible bool) { whitespaceState, whitespaceCalled = visible, true }
	if !ToggleWhitespace(props) || whitespaceState != false {
		t.Fatalf("whitespace toggle from true = %v, want false", whitespaceState)
	}
}

func TestNativeFlattenHunksProducesStableUniqueOrderedKeys(t *testing.T) {
	hunks := []DiffHunk{
		{ID: "hunk-a", Header: "@@ a @@", Lines: []DiffLine{{Kind: DiffLineContext, Text: "one"}, {Kind: DiffLineAddition, Text: "two"}}},
		{ID: "hunk-b", Header: "@@ b @@", Lines: []DiffLine{{Kind: DiffLineDeletion, Text: "three"}}},
	}
	rows := FlattenHunks(hunks)
	if len(rows) != 5 {
		t.Fatalf("flattened row count = %d, want 5", len(rows))
	}
	wantKinds := []HunkRowKind{HunkRowHeader, HunkRowLine, HunkRowLine, HunkRowHeader, HunkRowLine}
	seen := make(map[string]struct{}, len(rows))
	for index, row := range rows {
		if row.Kind != wantKinds[index] {
			t.Fatalf("row %d kind = %s, want %s", index, row.Kind, wantKinds[index])
		}
		if row.Key == "" {
			t.Fatalf("row %d has an empty key", index)
		}
		if _, duplicate := seen[row.Key]; duplicate {
			t.Fatalf("row %d key %q is not unique", index, row.Key)
		}
		seen[row.Key] = struct{}{}
	}
	if rows[0].Hunk.ID != "hunk-a" || rows[3].Hunk.ID != "hunk-b" {
		t.Fatal("flattened rows lost their owning hunk identity")
	}
}

func TestNativeRenderLineTextPreservesSemanticsWhileRevealingWhitespace(t *testing.T) {
	text := " \tvalue "
	if got := RenderLineText(text, false); got != text {
		t.Fatalf("RenderLineText(false) = %q, want unchanged %q", got, text)
	}
	got := RenderLineText(text, true)
	want := "·→value·"
	if got != want {
		t.Fatalf("RenderLineText(true) = %q, want %q", got, want)
	}
}

func TestNativeFilterFilesFollowsFilterPillConvention(t *testing.T) {
	props := diffReviewFixture(t)
	if got := FilterFiles(props.Files, props.CategoryFilters); len(got) != len(props.Files) {
		t.Fatalf("no active filters returned %d files, want all %d", len(got), len(props.Files))
	}
	active := make([]CategoryFilter, len(props.CategoryFilters))
	copy(active, props.CategoryFilters)
	for index := range active {
		if active[index].Category == FileCategorySource {
			active[index].Active = true
		}
	}
	got := FilterFiles(props.Files, active)
	if len(got) != 1 || got[0].Category != FileCategorySource {
		t.Fatalf("single active filter returned %+v, want exactly the source file", got)
	}
}

func TestNativeDiffHunkValidateRejectsUnsafeOrUnboundedLinks(t *testing.T) {
	base := DiffHunk{ID: "hunk-1", Header: "@@ h @@", Lines: []DiffLine{{Kind: DiffLineContext, Text: "line"}}}

	withBadStep := base
	withBadStep.PlanSteps = []taskgraph.PlanStepLink{{PlanRevision: 0, StepID: "x"}}
	if err := withBadStep.Validate(); err == nil {
		t.Fatal("hunk with an invalid plan-step link validated")
	}

	tooManyLines := base
	tooManyLines.Lines = make([]DiffLine, MaximumLinesPerHunk+1)
	for index := range tooManyLines.Lines {
		tooManyLines.Lines[index] = DiffLine{Kind: DiffLineContext, Text: "line"}
	}
	if err := tooManyLines.Validate(); err == nil {
		t.Fatal("hunk exceeding the bounded line limit validated")
	}

	additionWithOldNumber := base
	additionWithOldNumber.Lines = []DiffLine{{Kind: DiffLineAddition, Text: "x", OldLineNumberKnown: true, OldLineNumber: 1}}
	if err := additionWithOldNumber.Validate(); err == nil {
		t.Fatal("an added line carrying an old line number validated")
	}
}
