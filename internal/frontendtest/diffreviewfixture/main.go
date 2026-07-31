//go:build js && wasm

// Command diffreviewfixture mounts the typed diffreview.DiffReview surface
// with an interactive, stateful wrapper so a headless browser can exercise
// category filtering, file selection, whitespace visibility, and the
// M20-058..067 review affordances end to end.
package main

import (
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/diffreview"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/GoWebComponents/v5/utils"
)

func main() {
	ui.Render(ui.CreateElement(diffReviewFixture), "#app")
	utils.WaitForever()
}

func diffReviewFixture() ui.Node {
	return html.Main(html.Props{
		DataAttr: html.DataAttribute{Name: "testid", Value: "diff-review-fixture"},
	},
		html.H1(html.Props{Text: "Diff review mounted fixture"}),
		ui.CreateElement(diffReviewBoundary),
	)
}

func diffReviewBoundary() ui.Node {
	mode := primitives.Mode{Theme: design.ThemeDark, Density: design.DensityComfortable}
	filters := ui.UseState(mustFixtureFilters())
	selected := ui.UseState("internal/service/handler.go")
	whitespaceVisible := ui.UseState(false)
	activeHunkRow := ui.UseState("")

	props := diffreview.Props{
		Mode:              mode,
		DiffIdentity:      "fixture-diff-identity",
		BaseRevision:      strings.Repeat("a", 40),
		HeadRevision:      strings.Repeat("b", 40),
		Files:             fixtureFiles(),
		CategoryFilters:   filters.Get(),
		SelectedPath:      selected.Get(),
		WhitespaceVisible: whitespaceVisible.Get(),
		ActiveHunkRowKey:  activeHunkRow.Get(),

		OnSelectFile: selected.Set,
		OnToggleCategory: func(category diffreview.FileCategory, active bool) {
			next := append([]diffreview.CategoryFilter(nil), filters.Get()...)
			for index := range next {
				if next[index].Category == category {
					next[index].Active = active
				}
			}
			filters.Set(next)
		},
		OnToggleWhitespace:    whitespaceVisible.Set,
		OnActiveHunkRowChange: activeHunkRow.Set,
	}
	return ui.CreateElement(diffreview.DiffReview, props)
}

func mustFixtureFilters() []diffreview.CategoryFilter {
	categories := diffreview.AllFileCategories()
	filters := make([]diffreview.CategoryFilter, len(categories))
	for index, category := range categories {
		filters[index] = diffreview.CategoryFilter{Category: category}
	}
	return filters
}

func fixtureFiles() []diffreview.ChangedFile {
	sourcePath := mustFixturePath("internal/service/handler.go")
	testPath := mustFixturePath("internal/service/handler_test.go")
	generatedPath := mustFixturePath("internal/service/handler.pb.go")
	dependencyPath := mustFixturePath("vendor/example.com/pkg/pkg.go")
	configPath := mustFixturePath(".codeflux/config.yaml")
	previousConfigPath := mustFixturePath("config/settings.yaml")
	binaryPath := mustFixturePath("assets/logo.png")

	sourceFile := diffreview.ChangedFile{
		Path: sourcePath, Status: diffreview.FileChangeStatusModified, Category: diffreview.FileCategorySource,
		Lines: diffreview.LineCounts{Known: true, Added: 12, Deleted: 3},
		Scope: diffreview.ScopeAssessment{Known: true, InScope: true},
		Hunks: []diffreview.DiffHunk{{
			ID: "hunk-1", Header: "@@ -10,3 +10,4 @@ func Handle()",
			Lines: []diffreview.DiffLine{
				{Kind: diffreview.DiffLineContext, Text: "func Handle(w http.ResponseWriter, r *http.Request) {", OldLineNumberKnown: true, OldLineNumber: 10, NewLineNumberKnown: true, NewLineNumber: 10},
				{Kind: diffreview.DiffLineDeletion, Text: "\tlog.Println(\"handling\")", OldLineNumberKnown: true, OldLineNumber: 11},
				{Kind: diffreview.DiffLineAddition, Text: "\tlog.Println(\"handling request\")", NewLineNumberKnown: true, NewLineNumber: 11},
			},
			PlanSteps:    []taskgraph.PlanStepLink{{PlanRevision: 3, StepID: "implement-handler"}},
			ToolEventIDs: []domain.EventID{mustFixtureEventID()},
			Validations: []diffreview.ValidationLink{{
				ID: mustFixtureValidationID(), Label: "go test ./internal/service/...",
				State: domain.ValidationStatePassed, Summary: "All tests passed.",
			}},
		}},
	}
	testFile := diffreview.ChangedFile{
		Path: testPath, Status: diffreview.FileChangeStatusAdded, Category: diffreview.FileCategoryTest,
		Lines: diffreview.LineCounts{Known: true, Added: 40, Deleted: 0},
		Scope: diffreview.ScopeAssessment{Known: true, InScope: true},
		Hunks: []diffreview.DiffHunk{{
			ID: "hunk-2", Header: "@@ -0,0 +1,3 @@",
			Lines: []diffreview.DiffLine{
				{Kind: diffreview.DiffLineAddition, Text: "func TestHandle(t *testing.T) {", NewLineNumberKnown: true, NewLineNumber: 1},
				{Kind: diffreview.DiffLineAddition, Text: "\tHandle(nil, nil)", NewLineNumberKnown: true, NewLineNumber: 2},
				{Kind: diffreview.DiffLineAddition, Text: "}", NewLineNumberKnown: true, NewLineNumber: 3},
			},
		}},
	}
	generatedFile := diffreview.ChangedFile{
		Path: generatedPath, Status: diffreview.FileChangeStatusModified, Category: diffreview.FileCategoryGenerated,
		Lines:                 diffreview.LineCounts{Known: true, Added: 5, Deleted: 2},
		Scope:                 diffreview.ScopeAssessment{Known: true, InScope: true},
		DiffUnavailableReason: "Generated file diffs are not rendered.",
	}
	dependencyFile := diffreview.ChangedFile{
		Path: dependencyPath, Status: diffreview.FileChangeStatusDeleted, Category: diffreview.FileCategoryDependency,
		Lines: diffreview.LineCounts{Known: true, Added: 0, Deleted: 45},
		Scope: diffreview.ScopeAssessment{Known: true, InScope: false},
	}
	configFile := diffreview.ChangedFile{
		Path: configPath, PreviousPath: previousConfigPath, Status: diffreview.FileChangeStatusRenamed, Category: diffreview.FileCategoryConfiguration,
		Lines: diffreview.LineCounts{Known: true, Added: 300, Deleted: 300}, FormattingChurn: true,
		Scope: diffreview.ScopeAssessment{UnknownReason: "no plan revision has been approved for this task yet"},
	}
	binaryFile := diffreview.ChangedFile{
		Path: binaryPath, Status: diffreview.FileChangeStatusAdded, Category: diffreview.FileCategoryOther, Binary: true,
		Lines: diffreview.LineCounts{UnknownReason: "binary content has no line-based diff"},
		Scope: diffreview.ScopeAssessment{Known: true, InScope: true},
	}
	return []diffreview.ChangedFile{sourceFile, testFile, generatedFile, dependencyFile, configFile, binaryFile}
}

func mustFixturePath(value string) diffreview.FilePath {
	path, err := diffreview.NewFilePath(value)
	if err != nil {
		panic(err)
	}
	return path
}

func mustFixtureEventID() domain.EventID {
	value, err := domain.ParseEventID("evt_01890f3c-4a00-7abc-8def-012345678901")
	if err != nil {
		panic(err)
	}
	return value
}

func mustFixtureValidationID() domain.ValidationID {
	value, err := domain.ParseValidationID("val_01890f3c-4a00-7abc-8def-012345678902")
	if err != nil {
		panic(err)
	}
	return value
}
