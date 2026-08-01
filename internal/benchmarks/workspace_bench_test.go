package benchmarks

import (
	"context"
	"fmt"
	"testing"

	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/testfixtures"
	"codeflux.dev/codeflux/internal/workspace"
)

// repositoryScale is one M22-079 fixture size.
//
// The sizes are named after what they represent to a user rather than after an
// exact file count, and the counts are chosen so the three are separated by an
// order of magnitude: a scale sweep whose points are close together cannot
// show a superlinear cost.
type repositoryScale struct {
	name     string
	packages int
	perFile  int
}

func repositoryScales() []repositoryScale {
	return []repositoryScale{
		{name: "small", packages: 3, perFile: 2},
		{name: "medium", packages: 30, perFile: 4},
		{name: "large", packages: 200, perFile: 6},
	}
}

// syntheticGoRepository builds a Go module of the requested shape on top of the
// shared clean fixture, so the benchmark measures the mapper against realistic
// package and symbol structure rather than one enormous file.
func syntheticGoRepository(tb testing.TB, scale repositoryScale) string {
	tb.Helper()
	files := testfixtures.CleanGoRepositoryFiles()
	for index := range scale.packages {
		name := fmt.Sprintf("pkg%03d", index)
		source := fmt.Sprintf("package %s\n\n", name)
		for symbol := range scale.perFile {
			source += fmt.Sprintf(
				"// Operation%d is synthetic benchmark surface.\nfunc Operation%d(value int) int { return value + %d }\n\n",
				symbol, symbol, symbol,
			)
		}
		files["internal/"+name+"/"+name+".go"] = source
	}
	root := tb.TempDir()
	fixture, err := testfixtures.NewRepositoryFixture(context.Background(), root, files)
	if err != nil {
		tb.Fatalf("build %s repository fixture: %v", scale.name, err)
	}
	return fixture.Root
}

func benchmarkRepositoryMapMap(tb testing.TB, root string) workspace.RepositoryMap {
	tb.Helper()
	snapshot, err := workspace.DiscoverRepository(context.Background(), root, workspace.ExecRunner{})
	if err != nil {
		tb.Fatalf("discover repository: %v", err)
	}
	repositoryMap, err := workspace.BuildRepositoryMap(context.Background(), snapshot, workspace.ExecRunner{})
	if err != nil {
		tb.Fatalf("build repository map: %v", err)
	}
	return repositoryMap
}

// BenchmarkRepositoryMap is M22-079. It runs at every declared scale, so a
// regression that only appears on a large repository cannot hide behind a fast
// small one.
func BenchmarkRepositoryMap(b *testing.B) {
	LogEnvironment(b)
	for _, scale := range repositoryScales() {
		b.Run(DescribeScale(scale.name), func(b *testing.B) {
			root := syntheticGoRepository(b, scale)
			snapshot, err := workspace.DiscoverRepository(context.Background(), root, workspace.ExecRunner{})
			if err != nil {
				b.Fatalf("discover repository: %v", err)
			}
			var built workspace.RepositoryMap
			Measure(b, nil, func() {
				built, err = workspace.BuildRepositoryMap(
					context.Background(), snapshot, workspace.ExecRunner{},
				)
				if err != nil {
					b.Fatalf("build repository map: %v", err)
				}
			})
			// A benchmark that measured an empty result would report an
			// impressive and meaningless number.
			if len(built.Packages) == 0 {
				b.Fatalf("%s repository map produced no packages", scale.name)
			}
			b.ReportMetric(float64(len(built.Packages)), "packages")
		})
	}
}

// BenchmarkContextSelection is M22-080.
//
// Selection runs against a real repository map and a real redaction pipeline,
// because both are on the path a task actually takes and either could dominate
// the cost.
func BenchmarkContextSelection(b *testing.B) {
	LogEnvironment(b)
	root := syntheticGoRepository(b, repositoryScale{name: "medium", packages: 30, perFile: 4})
	repositoryMap := benchmarkRepositoryMapMap(b, root)

	pipeline, err := redact.NewPipeline(nil, redact.Limits{
		MaximumInputBytes:  1 << 20,
		MaximumOutputBytes: 1 << 19,
	})
	if err != nil {
		b.Fatalf("build redaction pipeline: %v", err)
	}
	b.Cleanup(pipeline.Close)

	query := workspace.ContextQuery{
		Requirement:            "add pagination to the reservation list",
		IncludeRelevantHistory: true,
		Budget: workspace.ContextBudget{
			MaxFiles: 24, MaxBytes: 256 << 10, MaxEstimatedTokens: 64 << 10,
		},
	}

	var manifest workspace.ContextManifest
	Measure(b, nil, func() {
		manifest, err = workspace.SelectContext(
			context.Background(), root, repositoryMap, query, workspace.ExecRunner{}, pipeline,
		)
		if err != nil {
			b.Fatalf("select context: %v", err)
		}
	})
	if len(manifest.Items) == 0 {
		b.Fatal("context selection produced no items")
	}
	b.ReportMetric(float64(len(manifest.Items)), "items")
}
