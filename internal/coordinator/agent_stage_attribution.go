package coordinator

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

// changedLineRange is one contiguous, inclusive span of line numbers this run
// changed in one file, expressed against the file's current content — the
// content in the worktree right now, not the base revision's.
type changedLineRange struct {
	Start int
	End   int
}

// overlaps reports whether two spans share at least one line.
func (span changedLineRange) overlaps(other changedLineRange) bool {
	return span.Start <= other.End && other.Start <= span.End
}

// changeAttribution is what one run is answerable for: the changed line
// ranges derived from the base revision its worktree was created at
// (PIPE-111), which is the honest reference point because the worktree's own
// HEAD moves every time the run commits to it. A single-revision `git diff
// <base>` compares that revision against the current working tree, which
// already folds committed-since-base changes and any still-uncommitted edit
// together — exactly what a run answerable for its own worktree needs, with
// no separate case for "committed" versus "in progress".
type changeAttribution struct {
	// Established is false when the base revision could not be resolved or
	// the diff against it could not be read. A consumer must fail toward
	// including everything when this is false, never toward excluding it —
	// under-attributing on a failure would silently shrink what every gate
	// examines while still reporting the gate as scoped (PIPE-111 design
	// caution).
	Established bool
	// BaseRevision is the revision the ranges are computed against. Empty
	// when Established is false.
	BaseRevision string
	// Ranges is every changed line range, keyed by repository-relative,
	// slash-separated path. A path is present only when it has at least one
	// changed range; ranges within one file are merged and sorted.
	Ranges map[string][]changedLineRange
}

// Touches reports whether any changed line in the named file falls inside the
// given span. It fails toward "yes" when attribution was never established.
func (attribution changeAttribution) Touches(file string, span changedLineRange) bool {
	if !attribution.Established {
		return true
	}
	for _, changed := range attribution.Ranges[file] {
		if span.overlaps(changed) {
			return true
		}
	}
	return false
}

// TouchesLine is Touches for a single line, which is what a finding located
// at one source position — rather than a declaration's whole span — needs.
func (attribution changeAttribution) TouchesLine(file string, line int) bool {
	return attribution.Touches(file, changedLineRange{Start: line, End: line})
}

// ChangedLineCount totals the changed lines this run is answerable for across
// every file, for evidence reporting rather than for any gate decision.
func (attribution changeAttribution) ChangedLineCount() int {
	total := 0
	for _, ranges := range attribution.Ranges {
		for _, span := range ranges {
			total += span.End - span.Start + 1
		}
	}
	return total
}

// attributedScope names which produced declarations this run is answerable
// for (PIPE-111a): the ones whose own span — including a doc comment, when
// one is attached — overlaps a changed line range.
//
// Established distinguishes two facts a caller must not conflate: a run that
// changed no declaration at all (Established true, Names empty — a
// legitimate refactor, configuration change, or dependency bump that
// produces no new or touched function, per PIPE-124) and a run whose
// attribution could not be computed (Established false). Reading only Names
// cannot tell these apart, and a gate that cannot tell them apart would treat
// a computation failure as proof the first happened.
type attributedScope struct {
	Established bool
	Names       map[string]bool
}

// Contains reports whether one produced declaration is inside this run's
// attribution, failing toward inclusion when attribution was never
// established.
func (scope attributedScope) Contains(name string) bool {
	if !scope.Established {
		return true
	}
	return scope.Names[name]
}

// Count reports how many declarations are attributed, for evidence.
func (scope attributedScope) Count() int {
	return len(scope.Names)
}

// attributeDeclarations maps changed line ranges onto the declarations they
// fall inside (PIPE-111a): a one-line fix inside a thirty-function file
// attributes exactly the one function whose span the changed line falls in,
// not the other twenty-nine — because a gate that widens attribution to every
// declaration in a touched file is the same over-attribution PIPE-111 exists
// to remove, only pushed down one level.
func attributeDeclarations(
	functions []producedFunction, attribution changeAttribution,
) attributedScope {
	if !attribution.Established {
		return attributedScope{}
	}
	names := make(map[string]bool, len(functions))
	for _, function := range functions {
		span := changedLineRange{Start: function.StartLine, End: function.EndLine}
		if attribution.Touches(function.File, span) {
			names[function.Name] = true
		}
	}
	return attributedScope{Established: true, Names: names}
}

// resolveBaseRevision reads the exact revision this run's worktree was
// created at from the durable worktree binding, rather than from git state
// inside the worktree itself: the worktree's own HEAD moves every time the
// run commits to it, so nothing observable inside the worktree alone can be
// trusted to still say where it started. The binding recorded it once, at
// creation, and never moves it (PIPE-111).
func (execution *AgentExecution) resolveBaseRevision(
	ctx context.Context, taskID domain.TaskID,
) string {
	if execution.worktrees == nil {
		return ""
	}
	binding, err := execution.worktrees.GetWorktreeBinding(ctx, taskID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(binding.BaseRevision)
}

// resolveAttribution derives the full change attribution for one run. It is
// the one place every stage that needs it should call from, so the base
// revision is read and the diff is taken the same way everywhere rather than
// once per stage.
//
// It used to also parse the produced source and return the declaration-level
// attributedScope alongside it, but neither of its two callers has ever used
// that second value — each discards it with `_` — so every call paid for a
// parse whose result was thrown away before this run ever looked at it
// (PIPE-057). Every real consumer of an attributed scope (checkComplexity,
// checkSimplification, findCompletenessGaps) already parses its own file list
// and calls attributeDeclarations itself, because each needs the parse
// anyway to do its own job; resolving a scope here bought nothing they were
// not already doing. If a future caller needs the scope without needing the
// parse for anything else, compute it the same way those do:
// attributeDeclarations(functions, attribution) over whatever it already
// parsed.
func (execution *AgentExecution) resolveAttribution(
	ctx context.Context, scope agentScope,
) changeAttribution {
	baseRevision := execution.resolveBaseRevision(ctx, scope.taskID)
	return deriveChangeAttribution(ctx, scope.worktree, baseRevision)
}

// attributionFiles returns every file this run's attribution says changed,
// sorted. It is PIPE-111's changed-line-range mechanism doubling as the
// changed-file list it exists to replace: any file with at least one changed
// range belongs to this run's declarations, whether that file was committed,
// left uncommitted, or created outright.
func attributionFiles(attribution changeAttribution) []string {
	files := make([]string, 0, len(attribution.Ranges))
	for file := range attribution.Ranges {
		files = append(files, file)
	}
	sort.Strings(files)
	return files
}

// attributionOrProducedFiles is the file list a stage should scan: the
// attributed set when attribution was established, or producedGoFiles'
// git-status view otherwise — the correct fail-toward-inclusion default for a
// run whose base revision is unknown, and the exact behaviour every one of
// these stages had before attribution existed.
func attributionOrProducedFiles(
	worktree string, attribution changeAttribution,
) ([]string, error) {
	if !attribution.Established {
		return producedGoFiles(worktree)
	}
	return attributionFiles(attribution), nil
}

// attributedFunctions parses declarations from exactly the files attribution
// says changed, rather than from producedGoFiles' own git-status view.
// producedGoFiles sees only uncommitted edits and goes blind the moment a run
// commits to its own worktree — precisely the scenario PIPE-111's design
// caution names as the honest case to handle, not an edge case to tolerate
// being wrong about. When attribution could not be established, this falls
// back to the old, whole-git-status enumeration, which is the correct
// fail-toward-inclusion default.
func attributedFunctions(
	worktree string, attribution changeAttribution,
) ([]producedFunction, error) {
	files, err := attributionOrProducedFiles(worktree, attribution)
	if err != nil {
		return nil, err
	}
	return parseProducedFunctions(worktree, files)
}

// deriveChangeAttribution computes the line ranges a run is answerable for,
// against the base revision rather than against HEAD at check time (PIPE-111).
//
// An empty base revision, or any failure reading the diff, reports
// Established: false rather than an empty-but-established attribution — the
// two must never read the same way to a caller, per the design caution that
// under-attribution must be visible as a failure, not silently indistinguishable
// from a run that legitimately touched nothing.
func deriveChangeAttribution(
	ctx context.Context, worktree, baseRevision string,
) changeAttribution {
	baseRevision = strings.TrimSpace(baseRevision)
	if baseRevision == "" {
		return changeAttribution{}
	}
	tracked, err := diffLineRanges(ctx, worktree, baseRevision)
	if err != nil {
		return changeAttribution{}
	}
	untracked, err := untrackedGoFileRanges(ctx, worktree)
	if err != nil {
		return changeAttribution{}
	}
	ranges := make(map[string][]changedLineRange, len(tracked)+len(untracked))
	for file, fileRanges := range tracked {
		ranges[file] = append(ranges[file], fileRanges...)
	}
	for file, fileRanges := range untracked {
		ranges[file] = append(ranges[file], fileRanges...)
	}
	for file := range ranges {
		ranges[file] = mergeLineRanges(ranges[file])
	}
	return changeAttribution{
		Established: true, BaseRevision: baseRevision, Ranges: ranges,
	}
}

// hunkHeaderPattern matches a unified-diff hunk header's new-file half:
// "@@ -old[,count] +new[,count] @@". Only the new-file side is read, because
// attribution is expressed in the current content's line numbers.
var hunkHeaderPattern = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// diffLineRanges reads the unified diff between the base revision and the
// worktree's current state and returns the changed line ranges it finds, one
// file at a time.
//
// `git diff <base>` — a single revision, no second one — compares that
// revision against the working tree directly, which is what makes this
// correct for a worktree the run has been committing to: it is not a diff
// against HEAD, which would show nothing for anything already committed, and
// it is not a diff against the index alone, which would miss it too. One
// command captures committed-since-base and still-uncommitted changes
// together, because from the base revision's point of view they are the same
// kind of fact — content that differs from where the run started.
func diffLineRanges(
	ctx context.Context, worktree, baseRevision string,
) (map[string][]changedLineRange, error) {
	command := exec.CommandContext(ctx, "git", "diff", "--unified=0",
		"--no-color", "--no-ext-diff", "-M", baseRevision, "--", "*.go")
	command.Dir = worktree
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("diff against the base revision: %s",
			strings.TrimSpace(string(output)))
	}
	ranges := map[string][]changedLineRange{}
	currentFile := ""
	for _, line := range strings.Split(string(output), "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			path := strings.TrimPrefix(line, "+++ ")
			if path == "/dev/null" {
				// The file was deleted since the base revision. It carries no
				// current-content lines to attribute anything to, and nothing
				// declared in it survives to be examined by a later stage.
				currentFile = ""
				continue
			}
			currentFile = filepath.ToSlash(strings.TrimPrefix(path, "b/"))
		case strings.HasPrefix(line, "@@ "):
			if currentFile == "" {
				continue
			}
			match := hunkHeaderPattern.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			start, convErr := strconv.Atoi(match[1])
			if convErr != nil {
				continue
			}
			count := 1
			if match[2] != "" {
				count, convErr = strconv.Atoi(match[2])
				if convErr != nil {
					continue
				}
			}
			if count == 0 {
				// A pure deletion inserts no line of its own into the new file;
				// unified-diff convention places it right after the given line
				// (or before line 1, when start is 0). That position is
				// attributed rather than nothing, because the deletion still
				// changed whatever declaration surrounds it.
				at := start
				if at < 1 {
					at = 1
				}
				ranges[currentFile] = append(ranges[currentFile],
					changedLineRange{Start: at, End: at})
				continue
			}
			ranges[currentFile] = append(ranges[currentFile],
				changedLineRange{Start: start, End: start + count - 1})
		}
	}
	return ranges, nil
}

// untrackedGoFileRanges attributes every line of a Go file this run created
// outright. A file Git has never seen has no base-revision content to diff
// against, and the honest range for a brand-new file is all of it, not
// nothing — the same fail-toward-inclusion rule that governs a computation
// failure applies here to a file with no baseline to compare against.
func untrackedGoFileRanges(
	ctx context.Context, worktree string,
) (map[string][]changedLineRange, error) {
	command := exec.CommandContext(ctx, "git", "ls-files",
		"--others", "--exclude-standard")
	command.Dir = worktree
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list untracked files: %s",
			strings.TrimSpace(string(output)))
	}
	ranges := map[string][]changedLineRange{}
	for _, path := range strings.Split(string(output), "\n") {
		path = strings.TrimSpace(path)
		if path == "" || !strings.HasSuffix(path, ".go") {
			continue
		}
		content, readErr := os.ReadFile(
			filepath.Join(worktree, filepath.FromSlash(path)))
		if readErr != nil || len(content) == 0 {
			continue
		}
		lines := strings.Count(string(content), "\n") + 1
		ranges[filepath.ToSlash(path)] = []changedLineRange{{Start: 1, End: lines}}
	}
	return ranges, nil
}

// repositoryGoFiles lists every Go file physically present in the worktree
// right now, by walking it directly rather than asking Git for a changed or
// tracked subset.
//
// It exists for evidence searches, not for declaration scope: whether a
// declaration is tested can be proven by a file this run never touched — an
// existing test whose coverage already reaches new code, for instance — so a
// search restricted to the attributed or changed file set would misreport a
// real, pre-existing test as absent (PIPE-112). Declaration scope itself must
// stay attribution-limited; only the search for evidence about an attributed
// declaration needs the whole worktree.
func repositoryGoFiles(worktree string) ([]string, error) {
	var files []string
	walkErr := filepath.WalkDir(worktree,
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, relErr := filepath.Rel(worktree, path)
			if relErr != nil {
				return relErr
			}
			if entry.IsDir() {
				if relative == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			relative = filepath.ToSlash(relative)
			if !strings.HasSuffix(relative, ".go") ||
				strings.HasPrefix(relative, ".codeflux") {
				return nil
			}
			files = append(files, relative)
			return nil
		})
	if walkErr != nil {
		return nil, fmt.Errorf("walk the worktree for Go files: %w", walkErr)
	}
	sort.Strings(files)
	return files, nil
}

// mergeLineRanges sorts and coalesces overlapping or adjacent spans, so a
// file touched by several nearby hunks reports one clean range rather than
// several that happen to abut.
func mergeLineRanges(ranges []changedLineRange) []changedLineRange {
	if len(ranges) == 0 {
		return nil
	}
	sorted := make([]changedLineRange, len(ranges))
	copy(sorted, ranges)
	sort.Slice(sorted, func(first, second int) bool {
		return sorted[first].Start < sorted[second].Start
	})
	merged := []changedLineRange{sorted[0]}
	for _, next := range sorted[1:] {
		last := &merged[len(merged)-1]
		if next.Start <= last.End+1 {
			if next.End > last.End {
				last.End = next.End
			}
			continue
		}
		merged = append(merged, next)
	}
	return merged
}
