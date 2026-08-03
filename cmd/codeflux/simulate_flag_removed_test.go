package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestAUDIT029a_StartRefusesTheSimulateFlag covers AUDIT-029a.
//
// `codeflux start --simulate` used to be accepted and threaded into
// ApplicationOptions.SimulateExecution, but nothing in the coordinator ever
// read that field: the flag changed the printed banner and nothing else. A
// flag a user can pass, that is documented, and that changes no coordinator
// behavior is worse than no flag at all, so both were removed together.
//
// parseStartArguments refuses every flag it does not know (see
// TestM23_001_StartRefusesNonLoopbackAddresses), so the regression this
// guards against is `--simulate` silently starting to parse again — which
// would mean either the flag or its wiring came back only partially.
func TestAUDIT029a_StartRefusesTheSimulateFlag(t *testing.T) {
	if _, err := parseStartArguments([]string{"--simulate"}); err == nil {
		t.Fatal("start accepted --simulate; the coordinator has no simulated-execution " +
			"mode, so this flag must be refused as unknown rather than silently parsed")
	}
}

// simulatedExecutionSymbols are the identifiers that made up the removed,
// unconsumed capability.
var simulatedExecutionSymbols = []string{
	"SimulateExecution",
	"--simulate",
}

// TestAUDIT029a_NoProductionSourceDeclaresSimulatedExecution proves the
// capability stays removed rather than merely absent from this file today.
//
// It walks internal/ and cmd/, skipping tests, for any occurrence of the
// removed flag or field. If either reappears, the coordinator either needs a
// real executor this time or the reintroduction is a mistake — either way
// this should fail loudly rather than let the flag drift back to reporting a
// capability nobody built.
func TestAUDIT029a_NoProductionSourceDeclaresSimulatedExecution(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
		t.Fatalf("repository root %q has no go.mod: %v", root, statErr)
	}

	var hits []string
	for _, directory := range []string{"internal", "cmd"} {
		_ = filepath.WalkDir(filepath.Join(root, directory),
			func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
					return nil
				}
				if strings.HasSuffix(path, "_test.go") {
					// This test and its siblings are allowed to name the removed
					// symbols; what must stay gone is production reach.
					return nil
				}
				content, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil
				}
				body := string(content)
				for _, symbol := range simulatedExecutionSymbols {
					if !strings.Contains(body, symbol) {
						continue
					}
					relative, _ := filepath.Rel(root, path)
					hits = append(hits, filepath.ToSlash(relative)+": "+symbol)
				}
				return nil
			})
	}

	sort.Strings(hits)
	if len(hits) > 0 {
		t.Fatalf("the removed simulated-execution capability reappeared in production "+
			"source at %d site(s):\n  %s\n\nAUDIT-029a removed ApplicationOptions."+
			"SimulateExecution and `codeflux start --simulate` because the flag changed "+
			"no coordinator behavior. If a simulated-execution mode is wanted again, it "+
			"needs a real executor and a new TODO, not the old declaration back",
			len(hits), strings.Join(hits, "\n  "))
	}
}
