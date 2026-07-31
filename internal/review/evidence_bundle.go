package review

import (
	"errors"
	"slices"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	reportevidence "codeflux.dev/codeflux/internal/evidence"
	taskgraph "codeflux.dev/codeflux/internal/graph"
)

const (
	MaximumDiffBytes        = 3 << 20
	MaximumChangedFiles     = 512
	MaximumLinksPerFile     = 32
	MaximumPlanLinksPerFile = 16
)

var (
	ErrEvidenceUnavailable = errors.New("review evidence is unavailable")
	ErrEvidenceTooLarge    = errors.New("review evidence exceeds the product query bounds")
)

type EvidenceQuery struct {
	TaskID      domain.TaskID
	ReportID    string
	IncludeDiff bool
}

type ValidationAttribution struct {
	ID      domain.ValidationID
	State   domain.ValidationState
	Label   string
	Summary string
}

type FileAttribution struct {
	PlanSteps   []taskgraph.PlanStepLink
	EventIDs    []domain.EventID
	Validations []ValidationAttribution
}

// DiffFile and Diff are review-owned read models. The coordinator converts the
// Git adapter result at the application boundary so this leaf package never
// imports storage or Git services.
type DiffFile struct {
	Path                 string
	PreviousPath         string
	AddedLines           int
	DeletedLines         int
	Category             string
	Binary               bool
	FormattingChurn      bool
	OutsideApprovedScope bool
}

type Diff struct {
	Identity     string
	TaskID       domain.TaskID
	BaseRevision string
	HeadRevision string
	UnifiedDiff  string
	Files        []DiffFile
}

type EvidenceBundle struct {
	Report            reportevidence.Report
	Diff              *Diff
	DiffMatchesReport bool
	FileAttributions  map[string]FileAttribution
}

// FileAttributionForPath resolves exact files and explicit directory scopes
// ending in a slash. Arbitrary string prefixes never become directory scope.
func FileAttributionForPath(values map[string]FileAttribution, path string) FileAttribution {
	result := values[path]
	for scope, value := range values {
		if scope == path || !strings.HasSuffix(scope, "/") || !strings.HasPrefix(path, scope) {
			continue
		}
		result.PlanSteps = append(result.PlanSteps, value.PlanSteps...)
		for _, eventID := range value.EventIDs {
			if !slices.Contains(result.EventIDs, eventID) {
				result.EventIDs = append(result.EventIDs, eventID)
			}
		}
		for _, validation := range value.Validations {
			if !slices.ContainsFunc(result.Validations, func(existing ValidationAttribution) bool {
				return existing.ID == validation.ID
			}) {
				result.Validations = append(result.Validations, validation)
			}
		}
	}
	return result
}
