package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// PatchRequest is one textual patch against one existing file.
//
// The format is the one the models already reach for unprompted — ladder rung 6
// sent it four times at a tool that could not take it:
//
//	*** Update File: cmd/generated/main.go
//	@@
//	-func main() {
//	+// main evaluates the expression and prints its result.
//	+func main() {
//	 	if err := run(os.Args[1:], os.Stdout); err != nil {
//
// A hunk is matched by its whole before-text, context lines included, and must
// match exactly once. That is what makes it safe without line numbers: a line
// number goes stale the moment anything above it moves, and a mis-numbered hunk
// either fails obscurely or applies in the wrong place. Context does not go
// stale — it either still reads that way or it does not.
//
// It is also why a bare "+" line is not enough on its own. An insertion says
// what to add and not where, and where is the whole difficulty.
type PatchRequest struct {
	Path  string
	Hunks []PatchHunk
}

// PatchHunk is one contiguous change with the context that locates it.
type PatchHunk struct {
	// Before is the text as it stands: context lines and removed lines, in
	// order, exactly as they appear in the file.
	Before string
	// After is the text as it should stand: context lines and added lines.
	After string
	// Added and Removed count what this hunk does, for the size limits.
	Added   int
	Removed int
}

// PatchOutcome is what applying a patch did, for the record and the reply.
type PatchOutcome struct {
	Path        string
	BeforeSHA   string
	AfterSHA    string
	LinesAdded  int
	LinesRemove int
	Hunks       int
}

// Summary is the one line that goes back to the model.
//
// Deliberately not the file. What the next round needs is that the edit landed,
// where, and how much moved; if it needs the contents it can read them, which
// costs the same tokens once rather than every round for the rest of the run.
func (outcome PatchOutcome) Summary() string {
	return fmt.Sprintf(
		"patched %s\n  base:   %s\n  result: %s\n  changed: +%d/-%d lines in "+
			"%d hunk(s)",
		outcome.Path, outcome.BeforeSHA, outcome.AfterSHA,
		outcome.LinesAdded, outcome.LinesRemove, outcome.Hunks)
}

// ParsePatch reads the textual patch format.
//
// Strict about the frame and forgiving about the decoration: the file header
// and at least one hunk marker are required, because they are what says which
// file and where one change ends and the next begins, and a patch missing
// either is not a patch this can apply safely. Everything else — a trailing
// end marker, blank lines between hunks — is accepted or ignored.
func ParsePatch(raw string) (PatchRequest, error) {
	var request PatchRequest
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")

	var current *PatchHunk
	var before, after []string
	closeHunk := func() {
		if current == nil {
			return
		}
		current.Before = strings.Join(before, "\n")
		current.After = strings.Join(after, "\n")
		request.Hunks = append(request.Hunks, *current)
		current, before, after = nil, nil, nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "*** Update File:"),
			strings.HasPrefix(trimmed, "*** Add File:"),
			strings.HasPrefix(trimmed, "--- "),
			strings.HasPrefix(trimmed, "+++ "):
			closeHunk()
			if named := patchHeaderPath(trimmed); named != "" {
				request.Path = named
			}
		case strings.HasPrefix(trimmed, "*** Begin Patch"),
			strings.HasPrefix(trimmed, "*** End Patch"),
			strings.HasPrefix(trimmed, "diff --git"),
			strings.HasPrefix(trimmed, "index "):
			// Envelope. Carries nothing this needs.
		case strings.HasPrefix(trimmed, "@@"):
			closeHunk()
			current = &PatchHunk{}
		case current == nil:
			// Text outside any hunk. Prose, or a stray line; either way it
			// changes nothing and saying so would be noise.
		case strings.HasPrefix(line, "-"):
			before = append(before, line[1:])
			current.Removed++
		case strings.HasPrefix(line, "+"):
			after = append(after, line[1:])
			current.Added++
		case strings.HasPrefix(line, " "):
			before = append(before, line[1:])
			after = append(after, line[1:])
		default:
			// An unprefixed line is context written without its leading space,
			// which is common and harmless to accept.
			before = append(before, line)
			after = append(after, line)
		}
	}
	closeHunk()

	switch {
	case strings.TrimSpace(request.Path) == "":
		return request, errors.New(
			"the patch names no file: begin it with " +
				"\"*** Update File: <path>\"")
	case len(request.Hunks) == 0:
		return request, errors.New(
			"the patch has no hunks in it: mark each change with a line " +
				"reading \"@@\", then the lines to remove prefixed \"-\", the " +
				"lines to add prefixed \"+\", and enough unprefixed lines " +
				"around them to say where the change goes")
	}
	for index, hunk := range request.Hunks {
		if hunk.Added == 0 && hunk.Removed == 0 {
			return request, fmt.Errorf(
				"hunk %d changes nothing: it has context but no line prefixed "+
					"\"+\" or \"-\"", index+1)
		}
		if strings.TrimSpace(hunk.Before) == "" && hunk.Removed == 0 {
			return request, fmt.Errorf(
				"hunk %d adds lines with no context, so it does not say where "+
					"they go. Include the unprefixed lines it belongs after",
				index+1)
		}
	}
	return request, nil
}

// patchHeaderPath pulls the file out of a header line.
func patchHeaderPath(line string) string {
	for _, prefix := range []string{
		"*** Update File:", "*** Add File:", "+++ ", "--- ",
	} {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		named := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		named = strings.TrimPrefix(named, "a/")
		named = strings.TrimPrefix(named, "b/")
		if named == "" || named == "/dev/null" {
			return ""
		}
		return named
	}
	return ""
}

// ApplyPatch applies every hunk, or refuses without changing anything.
//
// Everything is checked before anything is written. A patch that half-applies
// is worse than one that does not apply: the file is then in a state neither
// the model nor the coordinator asked for, and the run's next decision is made
// about that state.
func ApplyPatch(existing string, request PatchRequest) (string, PatchOutcome, error) {
	patched := existing
	for index, hunk := range request.Hunks {
		occurrences := strings.Count(patched, hunk.Before)
		switch occurrences {
		case 1:
			patched = strings.Replace(patched, hunk.Before, hunk.After, 1)
		case 0:
			return "", PatchOutcome{}, fmt.Errorf(
				"hunk %d does not match anything in %s. Its context and the "+
					"lines it removes must appear exactly as they are in the "+
					"file, indentation included — read the file and copy them "+
					"rather than retyping them.\n\nIt was looking for:\n%s",
				index+1, request.Path, indentForMessage(hunk.Before))
		default:
			return "", PatchOutcome{}, fmt.Errorf(
				"hunk %d matches %d places in %s, so it does not say which is "+
					"meant. Add more unprefixed context lines around it until "+
					"it matches once", index+1, occurrences, request.Path)
		}
	}
	added, removed := 0, 0
	for _, hunk := range request.Hunks {
		added += hunk.Added
		removed += hunk.Removed
	}
	return patched, PatchOutcome{
		Path:        request.Path,
		BeforeSHA:   hashOfContent(existing),
		AfterSHA:    hashOfContent(patched),
		LinesAdded:  added,
		LinesRemove: removed,
		Hunks:       len(request.Hunks),
	}, nil
}

// indentForMessage renders a hunk's target inside a refusal, bounded.
func indentForMessage(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) > 12 {
		lines = append(lines[:12], "  … and more")
	}
	for index, line := range lines {
		lines[index] = "  | " + line
	}
	return strings.Join(lines, "\n")
}

// hashOfContent is the short digest a patch result reports.
//
// Short because it travels in prompts and is read by a model as often as by
// this code. Twelve hex characters distinguish anything a run will encounter
// and stay readable in a sentence.
func hashOfContent(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])[:12]
}

// ReadFileForPatch reads the file a patch targets.
func ReadFileForPatch(target string) (string, error) {
	content, err := os.ReadFile(target)
	if err != nil {
		return "", errors.New(
			"there is no file at that path to patch; use write-file to " +
				"create one")
	}
	if len(content) > maximumFileToolBytes {
		return "", fmt.Errorf(
			"the file is %d bytes, past the %d-byte limit",
			len(content), maximumFileToolBytes)
	}
	return string(content), nil
}

// PatchLimits bound how much one patch may change.
//
// A patch tool removes the accidental rewrite; it does not remove the
// deliberate one. A model can rewrite a file hunk by hunk just as thoroughly as
// it can in one write, and the reasons not to are the same: every line it
// touches is a chance to drop a function, drift a signature, or reword an
// acceptance-sensitive literal.
//
// So the size is bounded by what is being asked for. A comment repair that
// rewrites forty lines has not understood the request, and the refusal says so
// while the working program is still there.
type PatchLimits struct {
	// MaximumChangedLines is added plus removed, across every hunk.
	MaximumChangedLines int
	// MaximumHunks bounds how scattered one change may be. A repair touching
	// eight places is usually several repairs, and several repairs made at once
	// are several chances to be wrong with one verification between them.
	MaximumHunks int
	// MaximumFileShare is how much of the file may change, from 0 to 1.
	//
	// A patch that replaces most of a file is a rewrite wearing a patch's
	// clothes, and it should be sent as one so that it reads as one.
	MaximumFileShare float64
}

// UnboundedPatch is the default for ordinary work: a run building something is
// entitled to write as much of it as it needs.
var UnboundedPatch = PatchLimits{}

// WithinLimits reports whether an applied patch stayed inside its allowance.
//
// Checked after application rather than before, because the interesting numbers
// — how many lines actually moved, how much of the file that is — are not
// knowable from the patch text alone.
func (limits PatchLimits) WithinLimits(
	outcome PatchOutcome, fileLines int,
) error {
	changed := outcome.LinesAdded + outcome.LinesRemove
	if limits.MaximumChangedLines > 0 && changed > limits.MaximumChangedLines {
		return fmt.Errorf(
			"this patch changes %d lines and what was asked for allows %d. "+
				"Make the smallest change that satisfies it: if the work "+
				"genuinely needs more than that, say so in your reply rather "+
				"than doing it here",
			changed, limits.MaximumChangedLines)
	}
	if limits.MaximumHunks > 0 && outcome.Hunks > limits.MaximumHunks {
		return fmt.Errorf(
			"this patch changes %d separate places and what was asked for "+
				"allows %d. Several repairs at once are several chances to be "+
				"wrong with one verification between them",
			outcome.Hunks, limits.MaximumHunks)
	}
	if limits.MaximumFileShare > 0 && fileLines > 0 {
		share := float64(changed) / float64(fileLines)
		if share > limits.MaximumFileShare {
			return fmt.Errorf(
				"this patch changes %.0f%% of %s, past the %.0f%% a change of "+
					"this kind allows. A patch that replaces most of a file is "+
					"a rewrite, and should be sent as one so that it reads as "+
					"one",
				share*100, outcome.Path, limits.MaximumFileShare*100)
		}
	}
	return nil
}
