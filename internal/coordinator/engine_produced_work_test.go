package coordinator

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The structural checks in engine_produces_program_test.go decide whether a run
// laid its work out acceptably, and until now nothing decided whether they
// decide correctly. They are the checks most likely to be quietly wrong,
// because they only ever run behind a paid model call: a containment rule that
// never matches, or a purity rule that matches its own workspace, would report
// a clean result forever and nobody would see it. These run in milliseconds
// against a written-out tree and need no provider key.

// writeTree writes a worktree and returns its path.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestTheCommandIsFoundWhereverTheRunPutIt(t *testing.T) {
	work := readProducedWork(t, writeTree(t, map[string]string{
		"tools/runner/start.go": "package main\n\nfunc main() {}\n",
		"rules/rules.go":        "package rules\n\nfunc Decide() int { return 1 }\n",
	}))
	if work.command != "tools/runner" {
		t.Errorf("the command was found at %q, want tools/runner", work.command)
	}
	if filepath.Base(work.entry) != "start.go" {
		t.Errorf("the entry file is %q, want start.go", work.entry)
	}
}

// A file that says package main without declaring main is a package a run
// might legitimately write; treating it as the command would build the wrong
// thing.
func TestAPackageMainWithoutMainIsNotTheCommand(t *testing.T) {
	work := readProducedWork(t, writeTree(t, map[string]string{
		"helpers.go": "package main\n\nfunc help() {}\n",
		"cli/go.go":  "package main\n\nfunc main() {}\n",
	}))
	if work.command != "cli" {
		t.Errorf("the command was found at %q, want cli", work.command)
	}
}

func TestImportsAreReadRatherThanMatchedFor(t *testing.T) {
	work := readProducedWork(t, writeTree(t, map[string]string{
		"app/main.go": "package main\n\nfunc main() {}\n",
		// "net" appears as a string and inside another path, and the import is
		// aliased. Substring matching gets every one of these wrong.
		"store/store.go": "package store\n\n" +
			"import (\n\tsql \"database/sql\"\n\t\"net/http\"\n)\n\n" +
			"var Driver = \"net\"\n\nvar _ = sql.ErrNoRows\nvar _ = http.StatusOK\n",
	}))
	var store producedFile
	for _, file := range work.files {
		if file.directory == "store" {
			store = file
		}
	}
	if got := len(store.imports); got != 2 {
		t.Fatalf("read %d imports, want 2: %v", got, store.imports)
	}
	// net and net/http share a prefix and are two packages, not one library.
	if importers := work.importers("net"); len(importers) != 0 {
		t.Errorf("net is imported by nobody and %v was reported", importers)
	}
	if importers := work.importers("net/http"); len(importers) != 1 {
		t.Errorf("net/http is imported by store and %v was reported", importers)
	}
}

// A library is one dependency however many of its packages are used, or
// containment could be satisfied by importing a different corner of it.
func TestContainmentCountsAWholeLibraryAsOneDependency(t *testing.T) {
	work := readProducedWork(t, writeTree(t, map[string]string{
		"app/main.go": "package main\n\nfunc main() {}\n",
		"a/a.go":      "package a\n\nimport \"example.com/lib\"\n\nvar _ = lib.X\n",
		"b/b.go":      "package b\n\nimport \"example.com/lib/errors\"\n\nvar _ = errors.X\n",
		"c/c.go":      "package c\n\nimport \"example.com/libation\"\n\nvar _ = libation.X\n",
	}))
	importers := work.importers("example.com/lib")
	if len(importers) != 2 {
		t.Errorf("the library is reachable from a and b, and %v was reported",
			importers)
	}
}

func TestTheWorkspaceIsNotAWayOutOfAPackage(t *testing.T) {
	work := readProducedWork(t, writeTree(t, map[string]string{
		"app/main.go": "package main\n\nfunc main() {}\n",
		"rules/rules.go": "package rules\n\n" +
			"import \"" + workspaceModule + "/types\"\n\nvar _ = types.X\n",
		"adapter/adapter.go": "package adapter\n\n" +
			"import \"example.com/driver\"\n\nvar _ = driver.X\n",
	}))
	for _, file := range work.files {
		switch file.directory {
		case "rules":
			if file.reachesOutward() {
				t.Error("a package built on another workspace package reaches out")
			}
		case "adapter":
			if !file.reachesOutward() {
				t.Error("a package driving a third party does not reach out")
			}
		}
	}
}

func TestStrictlyImpureSeparatesTheStandardLibraryFromTheRest(t *testing.T) {
	for _, importPath := range []string{
		"fmt", "os", "time", "net/http", "example.com/driver",
	} {
		if !strictlyImpure(importPath) {
			t.Errorf("%s is a way out and was allowed", importPath)
		}
	}
	for _, importPath := range []string{
		"strings", "sort", "errors", "cmp", "math", workspaceModule + "/types",
	} {
		if strictlyImpure(importPath) {
			t.Errorf("%s is a calculation and was refused", importPath)
		}
	}
}

func TestInterfacesAreCountedWhereTheyAreDeclared(t *testing.T) {
	work := readProducedWork(t, writeTree(t, map[string]string{
		"app/main.go": "package main\n\nfunc main() {}\n",
		"ports/ports.go": "package ports\n\n" +
			"type Clock interface{ Now() int }\n\n" +
			"type Store interface {\n\tGet(string) (string, error)\n}\n\n" +
			"type Pair struct{ A, B string }\n",
	}))
	total := 0
	for _, file := range work.files {
		total += file.interfaces
	}
	if total != 2 {
		t.Errorf("counted %d interfaces, want 2", total)
	}
}

// A file that does not parse must not take the whole check down with it: the
// build step reports the syntax error a moment later, with a better message.
func TestABrokenFileDoesNotStopTheStructuralChecks(t *testing.T) {
	work := readProducedWork(t, writeTree(t, map[string]string{
		"app/main.go":    "package main\n\nfunc main() {}\n",
		"broken/oops.go": "package broken\n\nfunc Nope( {\n",
	}))
	if work.command != "app" {
		t.Errorf("the command was lost to an unrelated broken file: %q", work.command)
	}
	if got := len(work.paths()); got != 2 {
		t.Errorf("read %d files, want 2", got)
	}
}

// The rung table is a specification, and these are the properties of it that
// hold whether or not anybody has a provider key. Checking them here rather
// than by running the ladder is the difference between a defect found in
// milliseconds and one found after two hundred and fifty model calls.

func TestTheRungsAreNumberedWithoutAGap(t *testing.T) {
	rungs := ladderRungs()
	if len(rungs) == 0 {
		t.Fatal("the ladder is empty")
	}
	for index, rung := range rungs {
		number, _, found := strings.Cut(rung.name, " ")
		if !found {
			t.Errorf("rung %q does not begin with its number", rung.name)
			continue
		}
		want := strconv.Itoa(index + 1)
		if number != want {
			t.Errorf("the rung at position %d is numbered %s: %q",
				index+1, number, rung.name)
		}
	}
}

// Two rungs sharing a ticket is silent: in the shared mode the second one's
// intake matches the first one's idempotency key, returns the first one's task,
// and the suite reports a pass for a program it never asked for.
func TestEveryRungHasATicketOfItsOwn(t *testing.T) {
	seen := map[string]string{}
	for _, rung := range ladderRungs() {
		ticket := rung.ticket()
		if ticket == "rung-" {
			t.Errorf("rung %q produced an empty ticket", rung.name)
		}
		if first, taken := seen[ticket]; taken {
			t.Errorf("%q and %q share the ticket %s", first, rung.name, ticket)
		}
		seen[ticket] = rung.name
	}
}

// No requirement may name a file, a package or a directory: the layout is the
// run's to decide, and a requirement that dictates one is grading the run
// against a design nobody asked it to have.
func TestNoRungTellsTheRunWhereToPutAnything(t *testing.T) {
	naming := regexp.MustCompile(
		`\b[\w/]+\.go\b|\binternal/|\bcmd/|\bpackage main\b`)
	for _, rung := range ladderRungs() {
		if found := naming.FindAllString(rung.requirement, -1); len(found) > 0 {
			t.Errorf("rung %q names %v", rung.name, found)
		}
	}
}

// A rung that judges output it never declared cannot fail for the right
// reason, and one whose acceptance block is empty tells the model nothing.
func TestEveryRungDeclaresWhatItExpects(t *testing.T) {
	for _, rung := range ladderRungs() {
		if strings.TrimSpace(rung.expected) == "" {
			t.Errorf("rung %q expects nothing", rung.name)
		}
		if !strings.Contains(rung.acceptanceBlock(), rung.expected) {
			t.Errorf("rung %q would be judged by something it was not shown",
				rung.name)
		}
	}
}

// An argument holding a space is split by the acceptance block's own format,
// so the model is shown a different invocation from the one it is judged by.
func TestNoRungPassesAnArgumentTheAcceptanceBlockWouldSplit(t *testing.T) {
	for _, rung := range ladderRungs() {
		for _, argument := range rung.arguments {
			if strings.Contains(argument, " ") {
				t.Errorf("rung %q passes %q, which the acceptance block splits",
					rung.name, argument)
			}
		}
	}
}

func TestPackagesCanBeListedWithAndWithoutTheCommand(t *testing.T) {
	work := readProducedWork(t, writeTree(t, map[string]string{
		"app/main.go":    "package main\n\nfunc main() {}\n",
		"app/wire.go":    "package main\n\nvar wired = true\n",
		"rules/rules.go": "package rules\n\nvar X = 1\n",
	}))
	if got := work.packages(true); len(got) != 2 {
		t.Errorf("with the command there are 2 packages, got %v", got)
	}
	if got := work.packages(false); len(got) != 1 || got[0] != "rules" {
		t.Errorf("without the command there is only rules, got %v", got)
	}
}
