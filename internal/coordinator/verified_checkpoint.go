package coordinator

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// verifiedCheckpoint is the last state of the worktree that was known good.
//
// A run's later attempts are refinements, and a refinement can make things
// worse. Ladder rung 3 reached a state where its tests passed, was sent back
// for coverage, rewrote the test file, and ended six attempts later with a
// suite that failed — and the passing version was gone, because every attempt
// edits the same worktree in place. The run reported a failure it had already
// solved once.
//
// Keeping a copy costs a directory of small text files. Losing verified work
// costs the whole run, and costs it after paying for every attempt.
type verifiedCheckpoint struct {
	// store is where the copy lives. Kept beside the worktree rather than
	// inside it, so nothing the run does can see or edit its own safety net.
	store string
	// taken is whether anything has ever been captured. A checkpoint that was
	// never taken must never be restored: an empty directory would look like a
	// run that deleted its own work.
	taken bool
	// digest identifies the captured state, so a record can name which revision
	// was verified rather than asserting that one was.
	digest string
	// reason is what held when the copy was taken, for the terminal record.
	reason string
}

// newVerifiedCheckpoint prepares a store beside the worktree.
func newVerifiedCheckpoint(worktree string) *verifiedCheckpoint {
	return &verifiedCheckpoint{
		store: filepath.Join(filepath.Dir(worktree),
			filepath.Base(worktree)+".verified"),
	}
}

// capture copies the worktree as it now stands.
//
// Called only where the run has just established something worth keeping, so
// the copy is never of a state nobody checked. Overwrites the previous copy:
// the point is the last good state, not every good state, and keeping a history
// would need a policy for choosing between them that nothing here has.
func (checkpoint *verifiedCheckpoint) capture(worktree, reason string) error {
	if checkpoint == nil {
		return nil
	}
	if err := os.RemoveAll(checkpoint.store); err != nil {
		return err
	}
	digest, err := copyProducedTree(worktree, checkpoint.store)
	if err != nil {
		// A failed capture leaves no checkpoint rather than half of one. Half a
		// worktree restored over a whole one is worse than no restore at all.
		_ = os.RemoveAll(checkpoint.store)
		return err
	}
	checkpoint.taken = true
	checkpoint.digest = digest
	checkpoint.reason = reason
	tracef("checkpoint", "captured %s — %s", digest, reason)
	return nil
}

// restore puts the captured state back.
//
// Reports whether it restored anything, so a caller can say "the run ended on
// its verified revision" and "the run ended where it stopped" as the different
// facts they are.
func (checkpoint *verifiedCheckpoint) restore(worktree string) bool {
	if checkpoint == nil || !checkpoint.taken {
		return false
	}
	// Produced files are removed first. A restore that only overwrites leaves
	// behind whatever a later attempt added, which is a state that was never
	// verified wearing the label of one that was.
	if err := removeProducedFiles(worktree); err != nil {
		return false
	}
	if _, err := copyProducedTree(checkpoint.store, worktree); err != nil {
		return false
	}
	tracef("checkpoint", "restored %s — %s", checkpoint.digest, checkpoint.reason)
	return true
}

// copyProducedTree copies every file that is part of the produced work and
// returns a digest of what it copied.
//
// Version control and build output are skipped. A .git directory is the
// worktree's own bookkeeping rather than the run's work, and copying one is
// both large and, restored over a live repository, actively harmful.
func copyProducedTree(from, to string) (string, error) {
	var paths []string
	err := filepath.Walk(from, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, relErr := filepath.Rel(from, path)
		if relErr != nil {
			return relErr
		}
		if skipFromCheckpoint(relative) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(to, relative), 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		paths = append(paths, relative)
		return copyOneFile(path, filepath.Join(to, relative))
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	sum := sha256.New()
	for _, relative := range paths {
		body, readErr := os.ReadFile(filepath.Join(from, relative))
		if readErr != nil {
			continue
		}
		sum.Write([]byte(relative))
		sum.Write(body)
	}
	return hex.EncodeToString(sum.Sum(nil))[:12], nil
}

// removeProducedFiles clears the worktree of everything a checkpoint would
// have captured, leaving version control and build output alone.
func removeProducedFiles(worktree string) error {
	entries, err := os.ReadDir(worktree)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if skipFromCheckpoint(entry.Name()) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(worktree, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// skipFromCheckpoint names what is not part of the produced work.
func skipFromCheckpoint(relative string) bool {
	if relative == "." || relative == "" {
		return false
	}
	first := strings.Split(filepath.ToSlash(relative), "/")[0]
	switch first {
	case ".git", ".verified":
		return true
	}
	// Built binaries are reproduced by building. Copying one is slow, and a
	// restored binary older than the source beside it is a lie about what the
	// source does.
	switch strings.ToLower(filepath.Ext(relative)) {
	case ".exe", ".out", ".test":
		return true
	}
	return false
}

// copyOneFile writes one file, creating the directory it belongs in.
func copyOneFile(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	destination, err := os.Create(to)
	if err != nil {
		return err
	}
	defer func() { _ = destination.Close() }()
	_, err = io.Copy(destination, source)
	return err
}
