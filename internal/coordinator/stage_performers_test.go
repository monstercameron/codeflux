package coordinator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/pipeline"
)

// TestPIPE004a_EveryDeclaredStagePerformerExists closes the gap that let the
// PIPE-004 table drift.
//
// pipeline.Checks binds each stage to the function that decides it, so that a
// reader can go from a ledger row to the code that produced it. ValidateChecks
// only requires the name to be non-empty and free of spaces, so a name that
// resolves to nothing passes — and six of the thirty-seven did. Four named
// functions that have never existed anywhere in this repository
// (checkDecompositionCoverage, synthesiseAtomCases, checkAtomPropertyTests,
// checkAssembly); two named functions that were deleted as unreachable while
// the table kept pointing at them (documentAtoms, measureCoverage).
//
// The failure mode is quiet and specific: the binding still reads as
// authoritative, so a reader chasing a satisfied row is sent to a function that
// is not there, and concludes the table is stale rather than that the stage is
// unperformed. PIPE-017 had already corrected two such names by hand, which is
// evidence that hand-checking does not hold.
//
// A performer is either a plain function name or Receiver.Method. Both forms
// are resolved against the declarations in this package.
func TestPIPE004a_EveryDeclaredStagePerformerExists(t *testing.T) {
	declared, err := declaredFunctionsInPackage(".")
	if err != nil {
		t.Fatalf("parse package declarations: %v", err)
	}
	if len(declared) == 0 {
		t.Fatal("no declarations parsed, so this test would pass vacuously")
	}

	var missing []string
	for _, check := range pipeline.Checks {
		name := check.Performer
		if index := strings.LastIndex(name, "."); index >= 0 {
			name = name[index+1:]
		}
		if declared[name] {
			continue
		}
		stage, _ := pipeline.StageByNumber(check.Stage)
		missing = append(missing, check.Performer+
			" (stage "+stage.Name+")")
	}

	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("pipeline.Checks names %d performer(s) that do not exist:\n  %s\n\n"+
			"A binding that resolves to nothing sends a reader chasing a ledger row "+
			"to a function that is not there. Point the entry at the function that "+
			"really decides the stage — or, if nothing does, say so through "+
			"MaySatisfy and Unestablished rather than naming an absent one",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// declaredFunctionsInPackage returns every function and method name declared in
// the .go files of one directory, tests included. Tests count because a
// performer is allowed to live beside the code it checks, and excluding them
// would report a real binding as missing.
func declaredFunctionsInPackage(directory string) (map[string]bool, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	fileSet := token.NewFileSet()
	declared := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(
			fileSet,
			filepath.Join(directory, entry.Name()),
			nil,
			parser.SkipObjectResolution,
		)
		if err != nil {
			return nil, err
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name == nil {
				continue
			}
			declared[function.Name.Name] = true
		}
	}
	return declared, nil
}
