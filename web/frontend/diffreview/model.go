// Package diffreview renders one bounded, read-only review surface over an
// already-computed, already-redacted task diff: changed-file status and line
// counts, category filters, a safe unified-diff view with whitespace
// visibility control, and links from diff hunks to plan steps, tool/edit
// events, and validation evidence. It does not own diff computation, git
// worktree state, task/plan/event/validation persistence, redaction, or
// editor-open authority. Callers must supply validated Props built from an
// upstream diff (for example internal/gitwork.TaskDiff) after redaction
// through internal/redact; diff text is always rendered as inert plain text
// and never interpreted as markup.
package diffreview

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/web/frontend/primitives"
)

const (
	MaximumChangedFiles           = 512
	MaximumFilePathBytes          = 4096
	MaximumLineCount              = 10_000_000
	MaximumHunksPerFile           = 64
	MaximumHunkHeaderBytes        = 512
	MaximumLinesPerHunk           = 400
	MaximumLineTextBytes          = 2048
	MaximumReasonTextBytes        = 1024
	MaximumValidationLabelBytes   = 256
	MaximumPlanStepLinksPerHunk   = 16
	MaximumToolEventLinksPerHunk  = 32
	MaximumValidationLinksPerHunk = 32
)

// ErrInvalidDiffReviewProps classifies every rejected diff review prop.
var ErrInvalidDiffReviewProps = errors.New("invalid diff review props")

// FilePath is a constructor-validated repository-relative path. Its private
// field prevents an unvalidated path from reaching a future editor-open
// dispatch (reserved seam; see Props.OnOpenInEditor).
type FilePath struct {
	value string
}

// NewFilePath validates a bounded, canonical, repository-relative path.
func NewFilePath(value string) (FilePath, error) {
	if !validRelativePath(value) {
		return FilePath{}, diffReviewError("path", "must be a bounded canonical repository-relative path")
	}
	return FilePath{value: value}, nil
}

func (fp FilePath) String() string  { return fp.value }
func (fp FilePath) IsZero() bool    { return fp.value == "" }
func (fp FilePath) Validate() error { _, err := NewFilePath(fp.value); return err }

func validRelativePath(value string) bool {
	if value == "" || len(value) > MaximumFilePathBytes || strings.TrimSpace(value) != value ||
		strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.Contains(value, ":") ||
		strings.ContainsRune(value, '\x00') || path.Clean(value) != value ||
		value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return false
	}
	return true
}

// FileChangeStatus is the declared change kind of one changed file.
type FileChangeStatus string

const (
	FileChangeStatusAdded    FileChangeStatus = "added"
	FileChangeStatusModified FileChangeStatus = "modified"
	FileChangeStatusDeleted  FileChangeStatus = "deleted"
	FileChangeStatusRenamed  FileChangeStatus = "renamed"
)

func (status FileChangeStatus) IsValid() bool {
	switch status {
	case FileChangeStatusAdded, FileChangeStatusModified, FileChangeStatusDeleted, FileChangeStatusRenamed:
		return true
	default:
		return false
	}
}

// AllFileChangeStatuses returns the complete declared status set.
func AllFileChangeStatuses() []FileChangeStatus {
	return []FileChangeStatus{
		FileChangeStatusAdded, FileChangeStatusModified, FileChangeStatusDeleted, FileChangeStatusRenamed,
	}
}

// FileCategory is the declared review-filter bucket of one changed file.
type FileCategory string

const (
	FileCategorySource        FileCategory = "source"
	FileCategoryTest          FileCategory = "test"
	FileCategoryGenerated     FileCategory = "generated"
	FileCategoryDependency    FileCategory = "dependency"
	FileCategoryConfiguration FileCategory = "configuration"
	FileCategoryOther         FileCategory = "other"
)

func (category FileCategory) IsValid() bool {
	switch category {
	case FileCategorySource, FileCategoryTest, FileCategoryGenerated,
		FileCategoryDependency, FileCategoryConfiguration, FileCategoryOther:
		return true
	default:
		return false
	}
}

// AllFileCategories returns the complete declared category set in the
// canonical filter-bar order.
func AllFileCategories() []FileCategory {
	return []FileCategory{
		FileCategorySource, FileCategoryTest, FileCategoryGenerated,
		FileCategoryDependency, FileCategoryConfiguration, FileCategoryOther,
	}
}

// CategoryFilter is one filter-bar toggle state for a declared category.
type CategoryFilter struct {
	Category FileCategory
	Active   bool
}

// LineCounts is an explicit Known/Unknown added-and-deleted line attribution.
// A binary file legitimately carries Known=false; the reason must say so
// rather than silently reporting zero.
type LineCounts struct {
	Known         bool
	Added         int
	Deleted       int
	UnknownReason string
}

func (counts LineCounts) Validate() error {
	if !counts.Known {
		if counts.Added != 0 || counts.Deleted != 0 {
			return diffReviewError("line_counts", "unknown line counts must not carry values")
		}
		return validateRequiredText("line_counts.unknown_reason", counts.UnknownReason, MaximumReasonTextBytes)
	}
	if counts.UnknownReason != "" {
		return diffReviewError("line_counts.unknown_reason", "must be empty when line counts are known")
	}
	if counts.Added < 0 || counts.Deleted < 0 || counts.Added > MaximumLineCount || counts.Deleted > MaximumLineCount {
		return diffReviewError("line_counts", "must be bounded and non-negative")
	}
	return nil
}

// ScopeAssessment is an explicit Known/Unknown determination of whether a
// changed file lies within the proposed plan scope. A determination cannot be
// asserted in-scope while unknown; callers without an accepted plan revision
// must report Known=false with a reason instead of defaulting to in-scope.
type ScopeAssessment struct {
	Known         bool
	InScope       bool
	UnknownReason string
}

func (scope ScopeAssessment) Validate() error {
	if !scope.Known {
		if scope.InScope {
			return diffReviewError("scope", "unknown scope must not assert in-scope")
		}
		return validateRequiredText("scope.unknown_reason", scope.UnknownReason, MaximumReasonTextBytes)
	}
	if scope.UnknownReason != "" {
		return diffReviewError("scope.unknown_reason", "must be empty when scope is known")
	}
	return nil
}

// OutOfScope reports whether the file is affirmatively known to lie outside
// the proposed plan scope. An unknown determination never reports true.
func (scope ScopeAssessment) OutOfScope() bool { return scope.Known && !scope.InScope }

// ValidationLink attaches one validation-run outcome to a diff hunk.
type ValidationLink struct {
	ID      domain.ValidationID
	Label   string
	State   domain.ValidationState
	Summary string
}

func (link ValidationLink) Validate() error {
	if link.ID.IsZero() || !link.State.IsValid() {
		return diffReviewError("validation_link", "must carry a typed identity and a declared state")
	}
	if err := validateRequiredText("validation_link.label", link.Label, MaximumValidationLabelBytes); err != nil {
		return err
	}
	return validateOptionalText("validation_link.summary", link.Summary, MaximumReasonTextBytes)
}

// DiffLineKind is the declared kind of one rendered unified-diff line.
type DiffLineKind string

const (
	DiffLineContext  DiffLineKind = "context"
	DiffLineAddition DiffLineKind = "addition"
	DiffLineDeletion DiffLineKind = "deletion"
)

func (kind DiffLineKind) IsValid() bool {
	switch kind {
	case DiffLineContext, DiffLineAddition, DiffLineDeletion:
		return true
	default:
		return false
	}
}

// DiffLine is one bounded, plain-text unified-diff line. Text is rendered
// through the GWC text node (never as markup) and must already be redacted.
type DiffLine struct {
	Kind               DiffLineKind
	Text               string
	OldLineNumberKnown bool
	OldLineNumber      uint64
	NewLineNumberKnown bool
	NewLineNumber      uint64
}

func (line DiffLine) Validate() error {
	if !line.Kind.IsValid() {
		return diffReviewError("diff_line.kind", "must be a declared diff line kind")
	}
	if !utf8.ValidString(line.Text) || len(line.Text) > MaximumLineTextBytes {
		return diffReviewError("diff_line.text", "must be valid UTF-8 and bounded")
	}
	for _, character := range line.Text {
		if unicode.IsControl(character) && character != '\t' {
			return diffReviewError("diff_line.text", "must not contain control characters other than tab")
		}
	}
	if line.Kind == DiffLineAddition && line.OldLineNumberKnown {
		return diffReviewError("diff_line.old_line_number", "an added line must not carry an old line number")
	}
	if line.Kind == DiffLineDeletion && line.NewLineNumberKnown {
		return diffReviewError("diff_line.new_line_number", "a deleted line must not carry a new line number")
	}
	return nil
}

// DiffHunk is one bounded unified-diff hunk with its typed links to plan
// steps, tool/edit events, and validation evidence (M20-062..064).
type DiffHunk struct {
	ID           string
	Header       string
	Lines        []DiffLine
	PlanSteps    []taskgraph.PlanStepLink
	ToolEventIDs []domain.EventID
	Validations  []ValidationLink
}

func (hunk DiffHunk) Validate() error {
	if strings.TrimSpace(hunk.ID) == "" {
		return diffReviewError("hunk.id", "must not be empty")
	}
	if err := validateRequiredText("hunk.header", hunk.Header, MaximumHunkHeaderBytes); err != nil {
		return err
	}
	if len(hunk.Lines) == 0 || len(hunk.Lines) > MaximumLinesPerHunk {
		return diffReviewError("hunk.lines", "must contain between one and the bounded line limit")
	}
	for _, line := range hunk.Lines {
		if err := line.Validate(); err != nil {
			return err
		}
	}
	if len(hunk.PlanSteps) > MaximumPlanStepLinksPerHunk {
		return diffReviewError("hunk.plan_steps", "exceeds the bounded link limit")
	}
	seenSteps := make(map[taskgraph.PlanStepLink]struct{}, len(hunk.PlanSteps))
	for _, step := range hunk.PlanSteps {
		if err := step.Validate(); err != nil {
			return fmt.Errorf("%w: hunk.plan_steps: %v", ErrInvalidDiffReviewProps, err)
		}
		if _, duplicate := seenSteps[step]; duplicate {
			return diffReviewError("hunk.plan_steps", "contains a duplicate link")
		}
		seenSteps[step] = struct{}{}
	}
	if len(hunk.ToolEventIDs) > MaximumToolEventLinksPerHunk {
		return diffReviewError("hunk.tool_event_ids", "exceeds the bounded link limit")
	}
	seenEvents := make(map[domain.EventID]struct{}, len(hunk.ToolEventIDs))
	for _, eventID := range hunk.ToolEventIDs {
		if eventID.IsZero() {
			return diffReviewError("hunk.tool_event_ids", "contains an empty identity")
		}
		if _, duplicate := seenEvents[eventID]; duplicate {
			return diffReviewError("hunk.tool_event_ids", "contains a duplicate identity")
		}
		seenEvents[eventID] = struct{}{}
	}
	if len(hunk.Validations) > MaximumValidationLinksPerHunk {
		return diffReviewError("hunk.validations", "exceeds the bounded link limit")
	}
	seenValidations := make(map[domain.ValidationID]struct{}, len(hunk.Validations))
	for _, link := range hunk.Validations {
		if err := link.Validate(); err != nil {
			return err
		}
		if _, duplicate := seenValidations[link.ID]; duplicate {
			return diffReviewError("hunk.validations", "contains a duplicate identity")
		}
		seenValidations[link.ID] = struct{}{}
	}
	return nil
}

// ChangedFile is one bounded changed-file summary plus its optional bounded
// unified-diff hunks.
type ChangedFile struct {
	Path                  FilePath
	PreviousPath          FilePath
	Status                FileChangeStatus
	Category              FileCategory
	Lines                 LineCounts
	Binary                bool
	FormattingChurn       bool
	Scope                 ScopeAssessment
	Hunks                 []DiffHunk
	DiffUnavailableReason string
}

// Key is the stable list/selection identity for this file.
func (file ChangedFile) Key() string { return file.Path.String() }

func (file ChangedFile) Validate() error {
	if err := file.Path.Validate(); err != nil {
		return err
	}
	if !file.Status.IsValid() {
		return diffReviewError("file.status", "must be a declared change status")
	}
	if file.Status == FileChangeStatusRenamed {
		if file.PreviousPath.IsZero() || file.PreviousPath.String() == file.Path.String() {
			return diffReviewError("file.previous_path", "a renamed file requires a distinct previous path")
		}
		if err := file.PreviousPath.Validate(); err != nil {
			return err
		}
	} else if !file.PreviousPath.IsZero() {
		return diffReviewError("file.previous_path", "must be empty unless the file was renamed")
	}
	if !file.Category.IsValid() {
		return diffReviewError("file.category", "must be a declared file category")
	}
	if err := file.Lines.Validate(); err != nil {
		return err
	}
	if file.Binary && file.Lines.Known {
		return diffReviewError("file.line_counts", "a binary file must not report known line counts")
	}
	if err := file.Scope.Validate(); err != nil {
		return err
	}
	if len(file.Hunks) > MaximumHunksPerFile {
		return diffReviewError("file.hunks", "exceeds the bounded hunk limit")
	}
	if file.Binary && len(file.Hunks) != 0 {
		return diffReviewError("file.hunks", "a binary file must not carry rendered hunks")
	}
	seenHunks := make(map[string]struct{}, len(file.Hunks))
	for _, hunk := range file.Hunks {
		if err := hunk.Validate(); err != nil {
			return err
		}
		if _, duplicate := seenHunks[hunk.ID]; duplicate {
			return diffReviewError("file.hunks", "contains a duplicate hunk identity")
		}
		seenHunks[hunk.ID] = struct{}{}
	}
	if len(file.Hunks) != 0 && file.DiffUnavailableReason != "" {
		return diffReviewError("file.diff_unavailable_reason", "must be empty when hunks are present")
	}
	return validateOptionalText("file.diff_unavailable_reason", file.DiffUnavailableReason, MaximumReasonTextBytes)
}

// Props binds a validated diff review to Mode, view state, and dispatch
// callbacks. Authoritative diff, plan, event, and validation state remain
// outside this package; Props carries a fully modeled read-only snapshot.
type Props struct {
	Mode primitives.Mode

	DiffIdentity string
	BaseRevision string
	HeadRevision string

	Files           []ChangedFile
	CategoryFilters []CategoryFilter

	SelectedPath      string
	WhitespaceVisible bool
	ActiveHunkRowKey  string

	OnSelectFile          func(string)
	OnToggleCategory      func(FileCategory, bool)
	OnToggleWhitespace    func(bool)
	OnActiveHunkRowChange func(string)
	OnOpenPlanStep        func(taskgraph.PlanStepLink)
	OnOpenToolEvent       func(domain.EventID)
	OnOpenValidation      func(domain.ValidationID)

	// OnOpenInEditor dispatches only a prevalidated workspace-relative path.
	// The coordinator remains responsible for canonical containment checks.
	OnOpenInEditor func(validatedPath string, line uint32)
}

func (props Props) Validate() error {
	if err := validateRequiredText("diff_identity", props.DiffIdentity, MaximumReasonTextBytes); err != nil {
		return err
	}
	if !validRevision(props.BaseRevision) {
		return diffReviewError("base_revision", "must be a 40- or 64-character lowercase hexadecimal revision")
	}
	if !validRevision(props.HeadRevision) {
		return diffReviewError("head_revision", "must be a 40- or 64-character lowercase hexadecimal revision")
	}
	if len(props.Files) > MaximumChangedFiles {
		return diffReviewError("files", "exceeds the bounded file limit")
	}
	seenFiles := make(map[string]struct{}, len(props.Files))
	for _, file := range props.Files {
		if err := file.Validate(); err != nil {
			return err
		}
		if _, duplicate := seenFiles[file.Key()]; duplicate {
			return diffReviewError("files", "contains a duplicate path")
		}
		seenFiles[file.Key()] = struct{}{}
	}
	if err := validateCategoryFilters(props.CategoryFilters); err != nil {
		return err
	}
	if props.SelectedPath != "" {
		if _, ok := FindChangedFile(props.Files, props.SelectedPath); !ok {
			return diffReviewError("selected_path", "must reference a file present in the changed-file list")
		}
	}
	if len(props.Files) > 0 {
		if props.OnSelectFile == nil {
			return diffReviewError("on_select_file", "is required when changed files are present")
		}
		if props.OnToggleWhitespace == nil {
			return diffReviewError("on_toggle_whitespace", "is required when changed files are present")
		}
	}
	if len(props.CategoryFilters) > 0 && props.OnToggleCategory == nil {
		return diffReviewError("on_toggle_category", "is required when category filters are present")
	}
	return nil
}

func validateCategoryFilters(filters []CategoryFilter) error {
	all := AllFileCategories()
	if len(filters) != len(all) {
		return diffReviewError("category_filters", "must declare exactly the complete category set")
	}
	seen := make(map[FileCategory]struct{}, len(filters))
	for index, filter := range filters {
		if filter.Category != all[index] {
			return diffReviewError("category_filters", "must appear in the canonical category order")
		}
		if _, duplicate := seen[filter.Category]; duplicate {
			return diffReviewError("category_filters", "contains a duplicate category")
		}
		seen[filter.Category] = struct{}{}
	}
	return nil
}

// FindChangedFile returns the file at targetPath, if present.
func FindChangedFile(files []ChangedFile, targetPath string) (ChangedFile, bool) {
	for _, file := range files {
		if file.Path.String() == targetPath {
			return file, true
		}
	}
	return ChangedFile{}, false
}

// ActiveCategorySet reduces the ordered filter list to a lookup set.
func ActiveCategorySet(filters []CategoryFilter) map[FileCategory]bool {
	set := make(map[FileCategory]bool, len(filters))
	for _, filter := range filters {
		set[filter.Category] = filter.Active
	}
	return set
}

// FilterFiles applies the category filter bar. Matching the common
// filter-pill convention, no active category shows every file; one or more
// active categories restrict the list to exactly those categories.
func FilterFiles(files []ChangedFile, filters []CategoryFilter) []ChangedFile {
	active := ActiveCategorySet(filters)
	anyActive := false
	for _, isActive := range active {
		if isActive {
			anyActive = true
			break
		}
	}
	if !anyActive {
		return slices.Clone(files)
	}
	result := make([]ChangedFile, 0, len(files))
	for _, file := range files {
		if active[file.Category] {
			result = append(result, file)
		}
	}
	return result
}

// OutOfScopeFiles returns files affirmatively flagged outside the proposed
// plan scope (M20-065).
func OutOfScopeFiles(files []ChangedFile) []ChangedFile {
	result := make([]ChangedFile, 0)
	for _, file := range files {
		if file.Scope.OutOfScope() {
			result = append(result, file)
		}
	}
	return result
}

// FormattingChurnFiles returns files flagged with broad formatting churn
// (M20-066).
func FormattingChurnFiles(files []ChangedFile) []ChangedFile {
	result := make([]ChangedFile, 0)
	for _, file := range files {
		if file.FormattingChurn {
			result = append(result, file)
		}
	}
	return result
}

// BinaryOrGeneratedFiles returns files flagged binary or generated
// (M20-067).
func BinaryOrGeneratedFiles(files []ChangedFile) []ChangedFile {
	result := make([]ChangedFile, 0)
	for _, file := range files {
		if file.Binary || file.Category == FileCategoryGenerated {
			result = append(result, file)
		}
	}
	return result
}

// TotalLineCounts sums known added/deleted counts across files and reports
// whether every file's line counts were known.
func TotalLineCounts(files []ChangedFile) (added, deleted int, allKnown bool) {
	allKnown = true
	for _, file := range files {
		if !file.Lines.Known {
			allKnown = false
			continue
		}
		added += file.Lines.Added
		deleted += file.Lines.Deleted
	}
	return added, deleted, allKnown
}

// RenderLineText is the pure whitespace-visibility transform (M20-061). It
// never changes line semantics, only the visible glyphs for spaces and tabs.
func RenderLineText(text string, whitespaceVisible bool) string {
	if !whitespaceVisible {
		return text
	}
	var builder strings.Builder
	builder.Grow(len(text))
	for _, character := range text {
		switch character {
		case ' ':
			builder.WriteRune('·')
		case '\t':
			builder.WriteRune('→')
		default:
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

// HunkRowKind identifies one flattened unified-diff row.
type HunkRowKind string

const (
	HunkRowHeader HunkRowKind = "header"
	HunkRowLine   HunkRowKind = "line"
)

// HunkRow is one flattened, virtualizable row of a rendered unified diff:
// either a hunk header (carrying its plan-step, tool-event, and validation
// links) or one diff line.
type HunkRow struct {
	Key       string
	Kind      HunkRowKind
	Hunk      DiffHunk
	Line      DiffLine
	LineIndex int
}

// FlattenHunks expands bounded hunks into a bounded, stably keyed row list
// suitable for primitives.VirtualList.
func FlattenHunks(hunks []DiffHunk) []HunkRow {
	rows := make([]HunkRow, 0, len(hunks))
	for _, hunk := range hunks {
		rows = append(rows, HunkRow{Key: hunk.ID + "\x00header", Kind: HunkRowHeader, Hunk: hunk})
		for lineIndex, line := range hunk.Lines {
			rows = append(rows, HunkRow{
				Key:  fmt.Sprintf("%s\x00line\x00%d", hunk.ID, lineIndex),
				Kind: HunkRowLine, Hunk: hunk, Line: line, LineIndex: lineIndex,
			})
		}
	}
	return rows
}

// SelectFile dispatches a validated file selection. It is shared by browser
// clicks and native tests so out-of-list selections cannot be dispatched.
func SelectFile(props Props, targetPath string) bool {
	if props.Validate() != nil || props.OnSelectFile == nil {
		return false
	}
	if _, ok := FindChangedFile(props.Files, targetPath); !ok {
		return false
	}
	props.OnSelectFile(targetPath)
	return true
}

// ToggleCategory dispatches a validated category-filter flip.
func ToggleCategory(props Props, category FileCategory) bool {
	if props.Validate() != nil || props.OnToggleCategory == nil || !category.IsValid() {
		return false
	}
	for _, filter := range props.CategoryFilters {
		if filter.Category == category {
			props.OnToggleCategory(category, !filter.Active)
			return true
		}
	}
	return false
}

// ToggleWhitespace dispatches the validated whitespace-visibility flip
// (M20-061).
func ToggleWhitespace(props Props) bool {
	if props.Validate() != nil || props.OnToggleWhitespace == nil {
		return false
	}
	props.OnToggleWhitespace(!props.WhitespaceVisible)
	return true
}

func validateRequiredText(field, value string, maximum int) error {
	if strings.TrimSpace(value) == "" {
		return diffReviewError(field, "must not be empty")
	}
	return validateText(field, value, maximum)
}

func validateOptionalText(field, value string, maximum int) error {
	if value == "" {
		return nil
	}
	return validateText(field, value, maximum)
}

func validateText(field, value string, maximum int) error {
	if strings.TrimSpace(value) != value || !utf8.ValidString(value) || len(value) > maximum {
		return diffReviewError(field, "must be trimmed, valid UTF-8, and bounded")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return diffReviewError(field, "must not contain control characters")
		}
	}
	return nil
}

func validRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func diffReviewError(field, reason string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidDiffReviewProps, field, reason)
}
