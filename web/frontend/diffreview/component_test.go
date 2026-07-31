package diffreview

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestDiffReviewRendersChangedFileListWithStatusAndLineCounts(t *testing.T) {
	props := diffReviewFixture(t)
	markup := renderDiffReview(t, props)

	wants := []string{
		`data-component="diff-review"`, `data-read-only="true"`, `data-bounded="true"`,
		`data-file-count="6"`, `data-filtered-count="6"`,
		`data-component="diff-review-file-row"`,
		`data-path="internal/service/handler.go"`, `data-status="modified"`,
		`data-path="internal/service/handler_test.go"`, `data-status="added"`,
		`data-path="vendor/example.com/pkg/pkg.go"`, `data-status="deleted"`,
		`data-path=".codeflux/config.yaml"`, `data-status="renamed"`,
		"Modified", "Added", "Deleted", "Renamed",
		"+12 -3", "+40 -0", "+0 -45",
	}
	for _, want := range wants {
		if !strings.Contains(markup, want) {
			t.Errorf("diff review markup missing %q\n%s", want, markup)
		}
	}
}

func TestDiffReviewFiltersByCategory(t *testing.T) {
	props := diffReviewFixture(t)
	for index := range props.CategoryFilters {
		props.CategoryFilters[index].Active = props.CategoryFilters[index].Category == FileCategoryTest
	}
	markup := renderDiffReview(t, props)

	if !strings.Contains(markup, `data-filtered-count="1"`) {
		t.Fatalf("category filter did not narrow the file list\n%s", markup)
	}
	if !strings.Contains(markup, `data-path="internal/service/handler_test.go"`) {
		t.Fatalf("filtered list missing the test-category file\n%s", markup)
	}
	// The selected file panel intentionally keeps showing the already-selected
	// source file even when it is filtered out of the list; assert on the
	// file-row-only "status" marker instead of the shared "path" attribute,
	// which also appears in the selected-file panel.
	if strings.Contains(markup, `data-status="modified"`) {
		t.Fatalf("filtered list retained a non-matching source file row\n%s", markup)
	}
}

func TestDiffReviewRendersSafeBoundedDiffWithWhitespaceVisibility(t *testing.T) {
	props := diffReviewFixture(t)
	plain := renderDiffReview(t, props)
	for _, want := range []string{
		`data-component="diff-review-selected-file"`, `data-path="internal/service/handler.go"`,
		`data-component="diff-review-hunk-header"`, `@@ -10,3 +10,4 @@ func Handle()`,
		`data-component="diff-review-line"`, `data-kind="context"`, `data-kind="deletion"`, `data-kind="addition"`,
		// Diff text is rendered through the escaping text node (never as
		// markup), so quotes are HTML-entity-escaped rather than literal.
		`log.Println(&#34;handling&#34;)`, `log.Println(&#34;handling request&#34;)`,
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("plain diff markup missing %q\n%s", want, plain)
		}
	}
	// "·" is also the legitimate unknown-gutter-number placeholder, so scope
	// the negative assertion to the tab-reveal marker "→", which this package
	// only emits from RenderLineText's whitespace-visibility transform.
	if !strings.Contains(plain, "\tlog.Println") || strings.Contains(plain, "→") {
		t.Fatalf("whitespace markers rendered while WhitespaceVisible=false\n%s", plain)
	}

	props.WhitespaceVisible = true
	visible := renderDiffReview(t, props)
	if !strings.Contains(visible, "→log.Println") {
		t.Fatalf("whitespace-visible diff did not reveal the leading tab\n%s", visible)
	}
}

func TestDiffReviewLinksHunksToPlanStepsToolEventsAndValidation(t *testing.T) {
	props := diffReviewFixture(t)
	markup := renderDiffReview(t, props)

	for _, want := range []string{
		`data-plan-step-count="1"`, `data-tool-event-count="1"`, `data-validation-count="1"`,
		"Plan revision 3 · step implement-handler",
		props.Files[0].Hunks[0].ToolEventIDs[0].String(),
		props.Files[0].Hunks[0].Validations[0].ID.String(),
		"go test ./internal/service/...", "Passed", "All tests passed.",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("diff review markup missing hunk link %q\n%s", want, markup)
		}
	}
}

func TestDiffReviewFlagsOutOfScopeFormattingChurnAndBinaryOrGenerated(t *testing.T) {
	props := diffReviewFixture(t)
	markup := renderDiffReview(t, props)

	for _, want := range []string{
		`data-component="diff-review-warnings"`, `data-state="present"`,
		"Out of plan scope", "vendor/example.com/pkg/pkg.go",
		"Broad formatting churn", ".codeflux/config.yaml",
		"Binary or generated changes", "internal/service/handler.pb.go", "assets/logo.png",
		`data-out-of-plan-scope="true"`,
		`data-formatting-churn="true"`,
		`data-binary="true"`,
		`data-category="dependency"`, `data-category="configuration"`, `data-category="other"`, `data-category="generated"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("diff review markup missing warning evidence %q\n%s", want, markup)
		}
	}
}

func TestDiffReviewUsesExplicitUnknownTextWithoutInventingZero(t *testing.T) {
	props := diffReviewFixture(t)
	markup := renderDiffReview(t, props)

	for _, want := range []string{
		"Unknown — binary content has no line-based diff.",
		"Unknown — no plan revision has been approved for this task yet.",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("diff review markup missing unknown-state text %q\n%s", want, markup)
		}
	}
	for _, forbidden := range []string{
		`data-field="line-counts">+0 -0<`,
	} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("unknown line counts rendered as a false-confident zero via %q\n%s", forbidden, markup)
		}
	}
}

func TestDiffReviewRejectsInvalidPropsWithoutPanicking(t *testing.T) {
	props := diffReviewFixture(t)
	props.BaseRevision = "not-a-revision"
	markup := renderDiffReview(t, props)
	if !strings.Contains(markup, "Diff review unavailable") || !strings.Contains(markup, "base_revision") {
		t.Fatalf("invalid props did not render the bounded failure alert\n%s", markup)
	}

	if _, err := NewFilePath("../outside.go"); !errors.Is(err, ErrInvalidDiffReviewProps) {
		t.Fatalf("unsafe path error = %v", err)
	}
	if _, err := NewFilePath("C:/outside.go"); !errors.Is(err, ErrInvalidDiffReviewProps) {
		t.Fatalf("drive-style path error = %v", err)
	}

	props = diffReviewFixture(t)
	props.Files[0].Binary = true
	if err := props.Validate(); !errors.Is(err, ErrInvalidDiffReviewProps) {
		t.Fatalf("binary file with hunks error = %v", err)
	}

	template := diffReviewFixture(t).Files[1]
	template.Hunks = nil
	files := make([]ChangedFile, MaximumChangedFiles+1)
	for index := range files {
		file := template
		path, err := NewFilePath(fmt.Sprintf("internal/service/generated%04d.go", index))
		if err != nil {
			t.Fatal(err)
		}
		file.Path = path
		files[index] = file
	}
	props = diffReviewFixture(t)
	props.Files = files
	props.SelectedPath = ""
	if err := props.Validate(); !errors.Is(err, ErrInvalidDiffReviewProps) {
		t.Fatalf("unbounded file list error = %v", err)
	}
}

func renderDiffReview(t *testing.T, props Props) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.CreateElement(DiffReview, props))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func diffReviewFixture(t *testing.T) Props {
	t.Helper()

	sourcePath, err := NewFilePath("internal/service/handler.go")
	mustNoError(t, err)
	testPath, err := NewFilePath("internal/service/handler_test.go")
	mustNoError(t, err)
	generatedPath, err := NewFilePath("internal/service/handler.pb.go")
	mustNoError(t, err)
	dependencyPath, err := NewFilePath("vendor/example.com/pkg/pkg.go")
	mustNoError(t, err)
	configPath, err := NewFilePath(".codeflux/config.yaml")
	mustNoError(t, err)
	previousConfigPath, err := NewFilePath("config/settings.yaml")
	mustNoError(t, err)
	binaryPath, err := NewFilePath("assets/logo.png")
	mustNoError(t, err)

	eventID := mustEventID(t)
	validationID := mustValidationID(t)

	sourceFile := ChangedFile{
		Path: sourcePath, Status: FileChangeStatusModified, Category: FileCategorySource,
		Lines: LineCounts{Known: true, Added: 12, Deleted: 3},
		Scope: ScopeAssessment{Known: true, InScope: true},
		Hunks: []DiffHunk{{
			ID: "hunk-1", Header: "@@ -10,3 +10,4 @@ func Handle()",
			Lines: []DiffLine{
				{Kind: DiffLineContext, Text: "func Handle(w http.ResponseWriter, r *http.Request) {", OldLineNumberKnown: true, OldLineNumber: 10, NewLineNumberKnown: true, NewLineNumber: 10},
				{Kind: DiffLineDeletion, Text: "\tlog.Println(\"handling\")", OldLineNumberKnown: true, OldLineNumber: 11},
				{Kind: DiffLineAddition, Text: "\tlog.Println(\"handling request\")", NewLineNumberKnown: true, NewLineNumber: 11},
			},
			PlanSteps:    []taskgraph.PlanStepLink{{PlanRevision: 3, StepID: "implement-handler"}},
			ToolEventIDs: []domain.EventID{eventID},
			Validations: []ValidationLink{{
				ID: validationID, Label: "go test ./internal/service/...",
				State: domain.ValidationStatePassed, Summary: "All tests passed.",
			}},
		}},
	}
	testFile := ChangedFile{
		Path: testPath, Status: FileChangeStatusAdded, Category: FileCategoryTest,
		Lines: LineCounts{Known: true, Added: 40, Deleted: 0},
		Scope: ScopeAssessment{Known: true, InScope: true},
		Hunks: []DiffHunk{{
			ID: "hunk-2", Header: "@@ -0,0 +1,3 @@",
			Lines: []DiffLine{
				{Kind: DiffLineAddition, Text: "func TestHandle(t *testing.T) {", NewLineNumberKnown: true, NewLineNumber: 1},
				{Kind: DiffLineAddition, Text: "\tHandle(nil, nil)", NewLineNumberKnown: true, NewLineNumber: 2},
				{Kind: DiffLineAddition, Text: "}", NewLineNumberKnown: true, NewLineNumber: 3},
			},
		}},
	}
	generatedFile := ChangedFile{
		Path: generatedPath, Status: FileChangeStatusModified, Category: FileCategoryGenerated,
		Lines:                 LineCounts{Known: true, Added: 5, Deleted: 2},
		Scope:                 ScopeAssessment{Known: true, InScope: true},
		DiffUnavailableReason: "Generated file diffs are not rendered.",
	}
	dependencyFile := ChangedFile{
		Path: dependencyPath, Status: FileChangeStatusDeleted, Category: FileCategoryDependency,
		Lines: LineCounts{Known: true, Added: 0, Deleted: 45},
		Scope: ScopeAssessment{Known: true, InScope: false},
	}
	configFile := ChangedFile{
		Path: configPath, PreviousPath: previousConfigPath, Status: FileChangeStatusRenamed, Category: FileCategoryConfiguration,
		Lines: LineCounts{Known: true, Added: 300, Deleted: 300}, FormattingChurn: true,
		Scope: ScopeAssessment{UnknownReason: "no plan revision has been approved for this task yet"},
	}
	binaryFile := ChangedFile{
		Path: binaryPath, Status: FileChangeStatusAdded, Category: FileCategoryOther, Binary: true,
		Lines: LineCounts{UnknownReason: "binary content has no line-based diff"},
		Scope: ScopeAssessment{Known: true, InScope: true},
	}

	filters := make([]CategoryFilter, 0, len(AllFileCategories()))
	for _, category := range AllFileCategories() {
		filters = append(filters, CategoryFilter{Category: category})
	}

	return Props{
		DiffIdentity:    "diffid-0123456789abcdef",
		BaseRevision:    strings.Repeat("a", 40),
		HeadRevision:    strings.Repeat("b", 40),
		Files:           []ChangedFile{sourceFile, testFile, generatedFile, dependencyFile, configFile, binaryFile},
		CategoryFilters: filters,
		SelectedPath:    sourcePath.String(),

		OnSelectFile:          func(string) {},
		OnToggleCategory:      func(FileCategory, bool) {},
		OnToggleWhitespace:    func(bool) {},
		OnActiveHunkRowChange: func(string) {},
	}
}

func mustNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustEventID(t *testing.T) domain.EventID {
	t.Helper()
	value, err := domain.NewEventID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustValidationID(t *testing.T) domain.ValidationID {
	t.Helper()
	value, err := domain.NewValidationID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}
