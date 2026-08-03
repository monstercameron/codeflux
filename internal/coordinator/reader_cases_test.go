package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readerWorktreeSource is the shape rung 5 produced: a function that reads
// structured input from a stream and reports on it.
const readerWorktreeSource = `package main

import (
	"encoding/json"
	"fmt"
	"io"
)

type entry struct {
	Name   string ` + "`json:\"name\"`" + `
	Amount int    ` + "`json:\"amount\"`" + `
}

// readEntries decodes the entries in a stream of JSON objects.
func readEntries(input io.Reader) ([]entry, error) {
	decoder := json.NewDecoder(input)
	var entries []entry
	for {
		var next entry
		if err := decoder.Decode(&next); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("reading entries: %w", err)
		}
		entries = append(entries, next)
	}
	return entries, nil
}
`

// readerWorktreeTests exercises the reader over every kind of input the case
// ladder asks about, using its own values throughout.
const readerWorktreeTests = `package main

import (
	"strings"
	"testing"
)

func TestReadEntries(t *testing.T) {
	for _, input := range []string{
		"",
		"{\"name\":\"a\",\"amount\":1}",
		"not json at all",
		"  {\"name\":\"b\",\"amount\":2}  ",
		"{\"name\":\"c\"}\n\t{\"name\":\"d\"}\n",
		"{\"name\":\"caf\u00e9\"}",
		strings.Repeat("{\"name\":\"x\"}\n", 20000),
		"x",
	} {
		_, _ = readEntries(strings.NewReader(input))
	}
}
`

// TestAReaderLadderIsSatisfiableByARealSuite drives the whole path: parse the
// produced source, synthesise the ladder from the signatures, and ask whether
// the suite tried each case.
//
// Rung 5 was refused eight cases on exactly this shape, which failed stage 7
// and blocked thirteen hard stages behind it. The refused cases were
// strings.NewReader("") and strings.NewReader("!!! not the expected shape") —
// a run had to guess three exclamation marks and a particular sentence.
func TestAReaderLadderIsSatisfiableByARealSuite(t *testing.T) {
	worktree := t.TempDir()
	initializeCoordinatorGitRepository(t, worktree)
	writeReaderFile(t, worktree, "cmd/generated/main.go", readerWorktreeSource)
	writeReaderFile(t, worktree, "cmd/generated/main_test.go", readerWorktreeTests)

	owed, err := untriedCases(worktree)
	if err != nil {
		t.Fatalf("the produced source could not be examined: %v", err)
	}
	for name, cases := range owed {
		for _, candidate := range cases {
			t.Errorf("%s: %s (%s) is reported untried by a suite that reads "+
				"every kind of input", name, candidate.Shape, candidate.Why)
		}
	}
}

// TestAReaderLadderStillCatchesASuiteThatTriesNothing keeps the fix from
// becoming a rubber stamp: the gate must still report a real gap.
func TestAReaderLadderStillCatchesASuiteThatTriesNothing(t *testing.T) {
	worktree := t.TempDir()
	initializeCoordinatorGitRepository(t, worktree)
	writeReaderFile(t, worktree, "cmd/generated/main.go", readerWorktreeSource)
	writeReaderFile(t, worktree, "cmd/generated/main_test.go", `package main

import (
	"strings"
	"testing"
)

func TestReadEntries(t *testing.T) {
	_, _ = readEntries(strings.NewReader("{\"name\":\"a\",\"amount\":1}"))
}
`)
	owed, err := untriedCases(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if len(owed) == 0 {
		t.Error("a suite trying one ordinary input reported no untried cases, " +
			"so the gate has stopped asking anything")
	}
}

func writeReaderFile(t *testing.T, worktree, relative, body string) {
	t.Helper()
	path := filepath.Join(worktree, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(relative, ".go") {
		t.Fatalf("%s is not a Go file", relative)
	}
}
