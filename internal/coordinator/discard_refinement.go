package coordinator

import (
	"os"
	"path/filepath"
	"strings"
)

// discardRefinement throws away a refinement that broke something, and puts the
// verified revision back for the next attempt to work from.
//
// A refinement is a candidate, not a commit. Every attempt edited the worktree
// in place, so a failed candidate became the next candidate's baseline: ladder
// rung 5 had a correct, tested, accepted program at 33.8 seconds, changed its
// output while adding two blind-spot tests, and then spent attempts three, four
// and five repairing a descendant of the broken version — losing a function the
// tests named, drifting a signature, and referencing an undefined identifier,
// none of which existed in the program it started from.
//
//	verified checkpoint
//	    ↓
//	isolated refinement candidate
//	    ↓
//	build + tests + acceptance
//	    ├─ pass → promote the candidate
//	    └─ fail → discard it, keep the checkpoint
//
// The failed candidate is not simply deleted. What it tried is the most useful
// thing the next attempt can be told — it is a hypothesis that has been run —
// so the changed files are kept beside the worktree and named in the
// instruction. The next attempt gets the diagnosis and the good baseline, which
// is the pair it needs and had neither of.
//
// Returns the sentence to append to the send-back, or nothing when there is no
// verified revision to go back to. A run that has never had one has nothing
// better than what it is holding.
func discardRefinement(
	checkpoint *verifiedCheckpoint,
	worktree string,
	narrator *narratingExecutor,
) string {
	if checkpoint == nil || !checkpoint.taken {
		return ""
	}
	broken := filepath.Join(
		filepath.Dir(worktree), filepath.Base(worktree)+".discarded")
	_ = os.RemoveAll(broken)
	kept, _ := copyProducedTree(worktree, broken)
	if !checkpoint.restore(worktree) {
		return ""
	}
	// Everything the run believed is now about a tree that no longer exists.
	if narrator != nil {
		narrator.ranValidation = false
		narrator.validationFailed = false
		narrator.filesChangedSinceValidation = false
		narrator.lastTestFingerprint = ""
	}
	tracef("checkpoint", "discarded refinement %s; worktree reset to %s",
		kept, checkpoint.digest)

	var note strings.Builder
	note.WriteString(
		"\n\nThe worktree has been put back to the last revision that " +
			"compiled, passed its tests and matched the acceptance examples. " +
			"Your edits from this attempt are gone, deliberately: they broke " +
			"something that was working, and building the next attempt on top " +
			"of them would compound the damage rather than repair it.\n\n")
	note.WriteString(
		"So you are starting from working code, not from what you just " +
			"wrote. Read the files before changing them — they are not what " +
			"you left. Make the smallest change that addresses the failure " +
			"above, and change nothing else.")
	return note.String()
}
