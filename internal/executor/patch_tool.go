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
