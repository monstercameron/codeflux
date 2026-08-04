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
	// Context counts the unprefixed lines this hunk carries. It is what makes
	// the hunk findable at all: a hunk with none is a bare line, and a bare
	// line is wherever that text happens to occur, or nowhere.
	Context int
	// Anchor is file text this hunk is inside, used only to say which of
	// several identical places is meant. It is never applied and never
	// required to match; it narrows where Before is looked for and nothing
	// else.
	//
	// It comes from the two forms a patch uses to name a scope: a heading on
	// the "@@" line, and a preceding hunk of nothing but context.
	Anchor string
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
	// Every other file this patch names, so a patch spanning two files can be
	// refused with both of them rather than silently applied to one.
	var alsoNamed []string
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

	// Whether the patch has already said it is over.
	//
	// It matters because an unprefixed line is accepted as context, which is
	// right inside a hunk and wrong after the end marker: a run that wrote
	// "*** End Patch" and then a stray "EOF" — the shape a shell heredoc
	// leaves behind, and one models reproduce — had EOF folded into the hunk's
	// context, so the hunk was searched for as the file's own text plus a line
	// no file has ever contained and could not match anything. Ladder rung 9 on
	// 2026-08-03 failed 24 of its 34 patches, and this is what the tool
	// reported it was looking for.
	ended := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "*** Update File:"),
			strings.HasPrefix(trimmed, "*** Add File:"),
			strings.HasPrefix(trimmed, "--- "),
			strings.HasPrefix(trimmed, "+++ "):
			closeHunk()
			ended = false
			if named := patchHeaderPath(trimmed); named != "" {
				if request.Path == "" {
					request.Path = named
				}
				if named != request.Path {
					alsoNamed = append(alsoNamed, named)
				}
			}
		case strings.HasPrefix(trimmed, "*** Begin Patch"):
			// Envelope, and the start of a fresh one: whatever came before is
			// finished and anything after this belongs to the patch again.
			closeHunk()
			ended = false
		case strings.HasPrefix(trimmed, "*** End Patch"):
			closeHunk()
			ended = true
		case strings.HasPrefix(trimmed, "diff --git"),
			strings.HasPrefix(trimmed, "index "):
			// Envelope. Carries nothing this needs.
		case ended:
			// Past the end marker. Trailing text is not part of any hunk.
		case strings.HasPrefix(trimmed, "@@"):
			closeHunk()
			// "@@ func TestFoo(t *testing.T) {" names the scope the change is
			// inside, and the whole line was being discarded. Kept as an
			// anchor, it is what tells a "}" which "}" is meant.
			current = &PatchHunk{Anchor: hunkHeaderAnchor(trimmed)}
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
			countContext(current, line[1:])
		default:
			// An unprefixed line is context written without its leading space,
			// which is common and harmless to accept.
			before = append(before, line)
			after = append(after, line)
			countContext(current, line)
		}
	}
	closeHunk()

	switch {
	// A patch that changes two files is refused rather than applied to one.
	//
	// The request carries one path and one flat list of hunks, so a second
	// "*** Update File:" header used to overwrite the path while its hunks
	// joined the same list: every hunk was then searched for in whichever file
	// was named last. That is how a hunk written for main.go came to be
	// reported as matching thirty-nine places in main_test.go — it was being
	// looked for in a file it was never about.
	//
	// Refused rather than partially applied, because a tool call is bound to
	// one plan step and one step names one file: applying half of a patch would
	// attribute work to a step that did not ask for it, and silently dropping
	// the other half would lose changes the run believes it made. Ladder rung 7
	// on 2026-08-03 failed twenty of twenty-three patches with this among the
	// causes.
	case len(alsoNamed) > 0:
		return request, fmt.Errorf(
			"this patch changes more than one file: %s and %s. Send one "+
				"apply-patch call per file, each with its own "+
				"\"*** Update File: <path>\" header and only that file's hunks",
			request.Path, strings.Join(alsoNamed, ", "))
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
	// A hunk that changes nothing is not applied — but it is not noise either.
	//
	// Models emit them for orientation, and refusing the whole patch for one
	// costs the round, so they were dropped. Dropping was too much: a block of
	// context immediately before a change is how a patch names the scope the
	// change is inside, and it is exactly what makes "	}\n}" mean one closing
	// brace rather than every closing brace in the file. Ladder rung 9 on
	// 2026-08-03 sent the same patch six times in a row and was told to add
	// more context each time, with the context it needed in the hunk above;
	// 103 of that run's 109 patch failures were "matches N places".
	//
	// So it carries forward as the next hunk's anchor instead. A context block
	// after the last change still anchors nothing and is still dropped, which
	// is the case forgiveness was added for.
	changing := request.Hunks[:0:0]
	pending := ""
	for _, hunk := range request.Hunks {
		if hunk.Added == 0 && hunk.Removed == 0 {
			// The nearest preceding one wins: it is the innermost scope named,
			// and an outer scope cannot contradict it.
			if strings.TrimSpace(hunk.Before) != "" {
				pending = hunk.Before
			}
			continue
		}
		// A heading on the hunk's own marker line is more specific than a
		// block that preceded it, so it is not overwritten.
		if hunk.Anchor == "" {
			hunk.Anchor = pending
		}
		pending = ""
		changing = append(changing, hunk)
	}
	if len(changing) == 0 {
		return request, errors.New(
			"every hunk in this patch is context: none of them has a line " +
				"prefixed \"+\" or \"-\", so it describes the file without " +
				"changing it")
	}
	request.Hunks = changing
	for index, hunk := range request.Hunks {
		if strings.TrimSpace(hunk.Before) == "" && hunk.Removed == 0 {
			return request, fmt.Errorf(
				"hunk %d adds lines with no context, so it does not say where "+
					"they go. Include the unprefixed lines it belongs after",
				index+1)
		}
	}
	return request, nil
}

// countContext records a context line, if it is one that could locate anything.
//
// Blank lines do not count. Every patch ends with a newline, so splitting one
// yields a final empty element that lands in the unprefixed branch — count that
// and no hunk ever has zero context, which would make "this hunk carries no
// context" a message that could never be printed. A blank line inside a hunk is
// excluded for the same reason it is excluded at the end: it locates nothing.
func countContext(hunk *PatchHunk, line string) {
	if strings.TrimSpace(line) != "" {
		hunk.Context++
	}
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
	// Hunks are ordered, and each one is looked for after the last one landed.
	//
	// Searching the whole file for every hunk independently asks a question a
	// diff does not pose. A unified diff is a sequence: the second hunk belongs
	// after the first, and that is what makes context like "	}," or "		{"
	// unambiguous in a table-driven test where it occurs twenty times. Counting
	// globally, the same context is ambiguous everywhere, and the patch is
	// refused for saying nothing wrong.
	//
	// Ladder rung 7 on 2026-08-03 failed twenty of twenty-three patches, and
	// forty-seven of about fifty failures were "matches N places, so it does
	// not say which is meant".
	//
	// The cursor only ever resolves ambiguity; it never creates a failure. A
	// hunk the remainder cannot place falls back to the whole-file search this
	// has always done, so a patch whose hunks arrive out of order still applies
	// exactly as before.
	cursor := 0
	for index, hunk := range request.Hunks {
		// The scope the hunk named, if it named one and the file has it.
		//
		// Searching from just after it is what makes "	}\n}" mean this
		// function's closing brace rather than every function's. An anchor that
		// resolves to nothing is ignored rather than refused: it only ever
		// narrows where Before is looked for, so failing on it would turn a
		// stale scope name into a lost patch.
		from := cursor
		if hunk.Anchor != "" {
			if at := uniqueIndexIn(patched[cursor:], hunk.Anchor); at >= 0 {
				from = cursor + at + len(hunk.Anchor)
			}
		}
		if at := uniqueIndexIn(patched[from:], hunk.Before); at >= 0 {
			absolute := from + at
			patched = patched[:absolute] + hunk.After +
				patched[absolute+len(hunk.Before):]
			cursor = absolute + len(hunk.After)
			continue
		}
		if at := uniqueIndexIn(patched[cursor:], hunk.Before); at >= 0 {
			absolute := cursor + at
			patched = patched[:absolute] + hunk.After +
				patched[absolute+len(hunk.Before):]
			cursor = absolute + len(hunk.After)
			continue
		}
		occurrences := strings.Count(patched, hunk.Before)
		switch occurrences {
		case 1:
			at := strings.Index(patched, hunk.Before)
			patched = strings.Replace(patched, hunk.Before, hunk.After, 1)
			cursor = at + len(hunk.After)
		case 0:
			// A hunk with no context is reported as what it is.
			//
			// "Does not match anything" reads as "your text is wrong" and sends
			// a run to re-check characters it has already copied correctly. The
			// likelier fault in a bare hunk is that it is bare: one line with
			// nothing around it matches wherever that text happens to occur, or
			// nowhere at all. Every one of the 58 measured no-match failures on
			// ladder rung 16 on 2026-08-04 carried exactly one line and no
			// context — 58 of 58, which is not a tendency, it is the whole
			// failure mode.
			if hunk.Context == 0 {
				return "", PatchOutcome{}, fmt.Errorf(
					"hunk %d carries no context, so there is nothing to locate "+
						"it by, and the line it names is not in %s as written. "+
						"Put at least two unprefixed lines above and below the "+
						"change, copied exactly from the file.\n\nIt was "+
						"looking for:\n%s",
					index+1, request.Path, indentForMessage(hunk.Before))
			}
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

// hunkHeaderAnchor is the file text a "@@" line names, or empty when it names
// none.
//
// A hunk header carries up to two things after its marker: a line range, which
// is bookkeeping about a file this tool does not have, and a section heading,
// which is a line copied from the file and is exactly the anchor an ambiguous
// hunk is missing. The range is dropped and the heading is kept.
//
// Empty for a bare "@@", and empty for a header that is only a range: an anchor
// that is not file text would match nothing and turn a good patch into a
// refusal, which is worse than the ambiguity it was meant to resolve.
func hunkHeaderAnchor(line string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "@@"))
	// A trailing "@@" closes a range, and everything after it is the heading.
	if closing := strings.Index(rest, "@@"); closing >= 0 {
		// One leading space is the format's separator, not indentation.
		return strings.TrimRight(
			strings.TrimPrefix(rest[closing+2:], " "), " \t")
	}
	// An unclosed range is bookkeeping and nothing else.
	if strings.HasPrefix(rest, "-") || strings.HasPrefix(rest, "+") {
		return ""
	}
	return strings.TrimRight(rest, " \t")
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
// HashOfContent is exported so a prompt can name the revision a patch will be
// written against. A hunk that fails because the file moved is a different
// problem from one that never matched, and only a named revision tells them
// apart.
func HashOfContent(content string) string { return hashOfContent(content) }

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

// uniqueIndexIn reports where text occurs in body when it occurs exactly once,
// and -1 otherwise.
//
// Used to place a hunk in what remains of the file after the previous hunk,
// which is where a diff says it belongs. Returning -1 for both "nowhere" and
// "several places" is deliberate: the caller falls back to searching the whole
// file in either case, so this can only add an answer, never remove one.
func uniqueIndexIn(body, text string) int {
	if text == "" {
		return -1
	}
	first := strings.Index(body, text)
	if first < 0 {
		return -1
	}
	if strings.Contains(body[first+len(text):], text) {
		return -1
	}
	return first
}
