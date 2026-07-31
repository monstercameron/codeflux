// Package testselection deterministically selects validation commands from
// changed-file, repository-memory, baseline, risk, and user-acceptance facts.
// It selects commands only; execution and validation evidence live elsewhere.
package testselection

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaximumChangedFiles = 4096
	MaximumCandidates   = 1024
	MaximumArguments    = 128
	MaximumTextBytes    = 4096
	MaximumPathsPerFact = 4096
)

// CostClass is a stable, repository-supplied relative execution cost.
type CostClass string

const (
	CostTiny    CostClass = "tiny"
	CostFocused CostClass = "focused"
	CostPackage CostClass = "package"
	CostBroad   CostClass = "broad"
)

func (cost CostClass) valid() bool {
	return cost == CostTiny || cost == CostFocused || cost == CostPackage || cost == CostBroad
}

// Command is a structured argv command. No shell parsing or rewriting occurs,
// which preserves user-provided acceptance commands exactly.
type Command struct {
	Executable       string
	Arguments        []string
	WorkingDirectory string
	Cost             CostClass
}

// ChangedFile carries the deterministic package identity discovered for a
// repository-relative changed path. Package may be empty when unknown.
type ChangedFile struct {
	Path    string
	Package string
}

// PackageCheck selects one command for every changed file in Package.
type PackageCheck struct {
	Package string
	Command Command
}

// FileMapping is admitted deterministic project memory, never a similarity
// inference. ChangedPath must name a changed file in the request.
type FileMapping struct {
	ChangedPath string
	TestPaths   []string
	Command     Command
}

// BaselineFailure carries a command that failed before the active change and
// the changed files known to implicate it. Coverage may be unknown.
type BaselineFailure struct {
	Command      Command
	ChangedPaths []string
}

// AcceptanceCheck is an exact user-provided acceptance command with optional
// explicit changed-file coverage.
type AcceptanceCheck struct {
	Command      Command
	ChangedPaths []string
}

// Request is the complete bounded input to policy version 1.
type Request struct {
	ChangedFiles           []ChangedFile
	PackageChecks          []PackageCheck
	FileMappings           []FileMapping
	FailingBaselineChecks  []BaselineFailure
	RepositoryChecks       []Command
	AcceptanceChecks       []AcceptanceCheck
	ProtectedChange        bool
	RepositoryWideFeasible bool
}

// Reason is a stable explanation for one selected command.
type Reason string

const (
	ReasonChangedPackage       Reason = "changed-package"
	ReasonDeterministicMapping Reason = "deterministic-file-to-test-mapping"
	ReasonFailingBaseline      Reason = "failing-baseline-command"
	ReasonProtectedRepository  Reason = "protected-repository-wide"
	ReasonUserAcceptance       Reason = "user-provided-acceptance"
)

// SelectedCheck is one deduplicated command, its complete reasons, and the
// changed files it covers when known. Empty CoveredChangedFiles means unknown.
type SelectedCheck struct {
	Command             Command
	Reasons             []Reason
	CoveredChangedFiles []string
	MappedTestFiles     []string
}

// Result is ordered cheapest-first, with high-signal focused checks breaking
// cost ties ahead of broad fallback checks.
type Result struct {
	PolicyVersion uint32
	Checks        []SelectedCheck
}

const PolicyVersion uint32 = 1

// Select applies the complete deterministic test-selection policy.
func Select(request Request) (Result, error) {
	changed, err := validateRequest(request)
	if err != nil {
		return Result{}, err
	}
	selected := make(map[string]*selectedCandidate)
	add := func(command Command, reason Reason, covered, tests []string) {
		key := commandKey(command)
		candidate, ok := selected[key]
		if !ok {
			candidate = &selectedCandidate{check: SelectedCheck{Command: cloneCommand(command)}}
			selected[key] = candidate
		} else if costPriority(command.Cost) > costPriority(candidate.check.Command.Cost) {
			// Conflicting duplicate estimates resolve conservatively and without
			// depending on candidate order.
			candidate.check.Command.Cost = command.Cost
		}
		candidate.check.Reasons = appendUniqueReason(candidate.check.Reasons, reason)
		candidate.check.CoveredChangedFiles = appendKnownChanged(candidate.check.CoveredChangedFiles, covered, changed)
		candidate.check.MappedTestFiles = appendUniquePaths(candidate.check.MappedTestFiles, tests)
	}

	for _, baseline := range request.FailingBaselineChecks {
		add(baseline.Command, ReasonFailingBaseline, baseline.ChangedPaths, nil)
	}
	for _, mapping := range request.FileMappings {
		if _, ok := changed[mapping.ChangedPath]; ok {
			add(mapping.Command, ReasonDeterministicMapping, []string{mapping.ChangedPath}, mapping.TestPaths)
		}
	}
	for _, packageCheck := range request.PackageChecks {
		var covered []string
		for changedPath, file := range changed {
			if file.Package == packageCheck.Package {
				covered = append(covered, changedPath)
			}
		}
		if len(covered) > 0 {
			add(packageCheck.Command, ReasonChangedPackage, covered, nil)
		}
	}
	for _, acceptance := range request.AcceptanceChecks {
		add(acceptance.Command, ReasonUserAcceptance, acceptance.ChangedPaths, nil)
	}
	if request.ProtectedChange && request.RepositoryWideFeasible {
		allChanged := make([]string, 0, len(changed))
		for changedPath := range changed {
			allChanged = append(allChanged, changedPath)
		}
		for _, command := range request.RepositoryChecks {
			add(command, ReasonProtectedRepository, allChanged, nil)
		}
	}

	result := Result{PolicyVersion: PolicyVersion, Checks: make([]SelectedCheck, 0, len(selected))}
	for _, candidate := range selected {
		sort.Slice(candidate.check.Reasons, func(i, j int) bool {
			return reasonPriority(candidate.check.Reasons[i]) < reasonPriority(candidate.check.Reasons[j])
		})
		sort.Strings(candidate.check.CoveredChangedFiles)
		sort.Strings(candidate.check.MappedTestFiles)
		result.Checks = append(result.Checks, candidate.check)
	}
	sort.Slice(result.Checks, func(i, j int) bool {
		left, right := result.Checks[i], result.Checks[j]
		if costPriority(left.Command.Cost) != costPriority(right.Command.Cost) {
			return costPriority(left.Command.Cost) < costPriority(right.Command.Cost)
		}
		leftReason, rightReason := bestReason(left.Reasons), bestReason(right.Reasons)
		if reasonPriority(leftReason) != reasonPriority(rightReason) {
			return reasonPriority(leftReason) < reasonPriority(rightReason)
		}
		return commandKey(left.Command) < commandKey(right.Command)
	})
	return result, nil
}

type selectedCandidate struct {
	check SelectedCheck
}

func validateRequest(request Request) (map[string]ChangedFile, error) {
	if len(request.ChangedFiles) > MaximumChangedFiles {
		return nil, errors.New("changed files exceed the selection bound")
	}
	candidates := len(request.PackageChecks) + len(request.FileMappings) + len(request.FailingBaselineChecks) + len(request.RepositoryChecks) + len(request.AcceptanceChecks)
	if candidates > MaximumCandidates {
		return nil, errors.New("test candidates exceed the selection bound")
	}
	changed := make(map[string]ChangedFile, len(request.ChangedFiles))
	for _, file := range request.ChangedFiles {
		if err := validateRelativePath("changed file", file.Path); err != nil {
			return nil, err
		}
		if _, duplicate := changed[file.Path]; duplicate {
			return nil, fmt.Errorf("changed file %q is duplicated", file.Path)
		}
		if file.Package != "" {
			if err := validateText("changed package", file.Package); err != nil {
				return nil, err
			}
		}
		changed[file.Path] = file
	}
	validateCommand := func(command Command) error {
		if err := validateText("command executable", command.Executable); err != nil {
			return err
		}
		if !command.Cost.valid() {
			return errors.New("command cost class is invalid")
		}
		if len(command.Arguments) > MaximumArguments {
			return errors.New("command arguments exceed the selection bound")
		}
		for _, argument := range command.Arguments {
			if err := validateArgument(argument); err != nil {
				return err
			}
		}
		if command.WorkingDirectory != "" {
			return validateRelativePath("command working directory", command.WorkingDirectory)
		}
		return nil
	}
	for _, item := range request.PackageChecks {
		if err := validateText("package check package", item.Package); err != nil {
			return nil, err
		}
		if err := validateCommand(item.Command); err != nil {
			return nil, err
		}
	}
	for _, item := range request.FileMappings {
		if err := validateRelativePath("file mapping changed path", item.ChangedPath); err != nil {
			return nil, err
		}
		if len(item.TestPaths) > MaximumPathsPerFact {
			return nil, errors.New("mapped test paths exceed the selection bound")
		}
		for _, testPath := range item.TestPaths {
			if err := validateRelativePath("mapped test path", testPath); err != nil {
				return nil, err
			}
		}
		if err := validateCommand(item.Command); err != nil {
			return nil, err
		}
	}
	for _, item := range request.FailingBaselineChecks {
		if err := validateCommand(item.Command); err != nil {
			return nil, err
		}
		if err := validateCoveragePaths(item.ChangedPaths, changed); err != nil {
			return nil, err
		}
	}
	for _, item := range request.AcceptanceChecks {
		if err := validateCommand(item.Command); err != nil {
			return nil, err
		}
		if err := validateCoveragePaths(item.ChangedPaths, changed); err != nil {
			return nil, err
		}
	}
	for _, command := range request.RepositoryChecks {
		if err := validateCommand(command); err != nil {
			return nil, err
		}
	}
	return changed, nil
}

func validateArgument(value string) error {
	if !utf8.ValidString(value) || len(value) > MaximumTextBytes {
		return errors.New("command argument must be valid UTF-8 and bounded")
	}
	for _, character := range value {
		if character == 0 {
			return errors.New("command argument must not contain NUL")
		}
	}
	return nil
}

func validateCoveragePaths(paths []string, changed map[string]ChangedFile) error {
	if len(paths) > MaximumPathsPerFact {
		return errors.New("covered changed paths exceed the selection bound")
	}
	for _, value := range paths {
		if err := validateRelativePath("covered changed path", value); err != nil {
			return err
		}
		if _, ok := changed[value]; !ok {
			return errors.New("covered changed path is not present in the changed-file set")
		}
	}
	return nil
}

func validateRelativePath(label, value string) error {
	if err := validateText(label, value); err != nil {
		return err
	}
	if strings.Contains(value, "\\") || strings.Contains(value, ":") || path.IsAbs(value) || path.Clean(value) != value || value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return errors.New(label + " must be a normalized repository-relative path")
	}
	return nil
}

func validateText(label, value string) error {
	if value == "" || value != strings.TrimSpace(value) || !utf8.ValidString(value) || len(value) > MaximumTextBytes {
		return errors.New(label + " must be non-empty, trimmed, valid UTF-8, and bounded")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return errors.New(label + " must not contain control characters")
		}
	}
	return nil
}

func commandKey(command Command) string {
	parts := make([]string, 0, len(command.Arguments)+3)
	parts = append(parts, command.WorkingDirectory, command.Executable)
	parts = append(parts, command.Arguments...)
	var builder strings.Builder
	for _, part := range parts {
		fmt.Fprintf(&builder, "%d:%s", len(part), part)
	}
	return builder.String()
}

func cloneCommand(command Command) Command {
	command.Arguments = slices.Clone(command.Arguments)
	return command
}

func appendKnownChanged(existing, values []string, changed map[string]ChangedFile) []string {
	for _, value := range values {
		if _, ok := changed[value]; ok && !slices.Contains(existing, value) {
			existing = append(existing, value)
		}
	}
	return existing
}

func appendUniquePaths(existing, values []string) []string {
	for _, value := range values {
		if !slices.Contains(existing, value) {
			existing = append(existing, value)
		}
	}
	return existing
}

func appendUniqueReason(existing []Reason, value Reason) []Reason {
	if !slices.Contains(existing, value) {
		return append(existing, value)
	}
	return existing
}

func bestReason(reasons []Reason) Reason {
	best := ReasonProtectedRepository
	for _, reason := range reasons {
		if reasonPriority(reason) < reasonPriority(best) {
			best = reason
		}
	}
	return best
}

func reasonPriority(reason Reason) int {
	switch reason {
	case ReasonFailingBaseline:
		return 0
	case ReasonDeterministicMapping:
		return 1
	case ReasonChangedPackage:
		return 2
	case ReasonUserAcceptance:
		return 3
	case ReasonProtectedRepository:
		return 4
	default:
		return 5
	}
}

func costPriority(cost CostClass) int {
	switch cost {
	case CostTiny:
		return 0
	case CostFocused:
		return 1
	case CostPackage:
		return 2
	case CostBroad:
		return 3
	default:
		return 4
	}
}
