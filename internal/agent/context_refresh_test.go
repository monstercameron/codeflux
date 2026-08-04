package agent

import "testing"

// TestARefreshReplacesOnlyWhatItReRead is the merge the round depends on.
//
// A refresher answers for the files the run may change. Everything else in the
// context — the selection's excerpts, what the project already knows — comes
// from sources it cannot consult and has not gone stale, so replacing the whole
// list with the refresher's answer would throw away most of what the round is
// supposed to know.
func TestARefreshReplacesOnlyWhatItReRead(t *testing.T) {
	existing := []RepositoryContextItem{
		{Path: "go.mod:1-4", ContentRedacted: "module x"},
		{Path: "stats/stats.go", ContentRedacted: "old"},
		{Path: "notes", ContentRedacted: "what the project knows"},
	}
	fresh := []RepositoryContextItem{
		{Path: "stats/stats.go", ContentRedacted: "new"},
	}

	merged := mergeContextByPath(existing, fresh)
	if len(merged) != 3 {
		t.Fatalf("want 3 items, got %d: %+v", len(merged), merged)
	}
	byPath := map[string]string{}
	for _, item := range merged {
		byPath[item.Path] = item.ContentRedacted
	}
	if byPath["stats/stats.go"] != "new" {
		t.Errorf("the re-read file kept its stale content: %q",
			byPath["stats/stats.go"])
	}
	if byPath["go.mod:1-4"] != "module x" || byPath["notes"] != "what the project knows" {
		t.Errorf("a refresh discarded context it never read: %+v", merged)
	}
	// Order is stable, because a document that reshuffles between rounds costs
	// the model the work of re-locating everything in it.
	if merged[0].Path != "go.mod:1-4" || merged[2].Path != "notes" {
		t.Errorf("the refresh reordered the context: %+v", merged)
	}
}

// TestAFileCreatedThisAttemptIsAdded is the case the context could not have
// held.
//
// The context is read when the attempt begins. A file the attempt then creates
// was in no revision and on no disk at that moment, so it is not in the list to
// be replaced — it has to be appended, or a run patching the file it just wrote
// is still working from nothing.
func TestAFileCreatedThisAttemptIsAdded(t *testing.T) {
	merged := mergeContextByPath(
		[]RepositoryContextItem{{Path: "go.mod:1-4", ContentRedacted: "module x"}},
		[]RepositoryContextItem{{Path: "cmd/x/main.go", ContentRedacted: "package main"}},
	)
	if len(merged) != 2 {
		t.Fatalf("the created file was dropped: %+v", merged)
	}
	if merged[1].Path != "cmd/x/main.go" {
		t.Errorf("want the new file appended, got %+v", merged)
	}
}

// TestNoRefreshLeavesTheContextAlone is the control.
//
// A refresher with nothing to say — no patch steps, an unreadable worktree —
// must not empty the context. The round would then be reasoning about a project
// it cannot see, which is worse than reasoning about a stale one.
func TestNoRefreshLeavesTheContextAlone(t *testing.T) {
	existing := []RepositoryContextItem{
		{Path: "go.mod:1-4", ContentRedacted: "module x"},
	}
	if merged := mergeContextByPath(existing, nil); len(merged) != 1 {
		t.Errorf("an empty refresh emptied the context: %+v", merged)
	}
}
