package transport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	// ErrMemoryQueryInvalid reports a memory read the coordinator refused
	// because the query itself was malformed rather than because the data was
	// missing.
	ErrMemoryQueryInvalid = errors.New("memory query is invalid")
	// ErrMemoryArtifactNotFound reports an artifact that does not exist in the
	// project the caller named -- including one that exists elsewhere, which
	// is reported the same way so the answer discloses nothing.
	ErrMemoryArtifactNotFound = errors.New("memory artifact not found")
)

// MemoryArtifactListQuery reads the live memory of one project.
//
// The project is not optional and has no default. Memory is the store where a
// missing scope would let one project's learned facts be read under another
// project's name, so the boundary is carried in the query rather than assumed
// from ambient session state.
type MemoryArtifactListQuery struct {
	ProjectID domain.ProjectID
	Limit     int
}

// MemoryArtifactSummaryView is one artifact at its newest revision, reduced to
// what a list can honestly show: identity, kind, governed maturity, and a
// single redacted line. Content stays behind the detail read.
type MemoryArtifactSummaryView struct {
	ArtifactID            domain.MemoryArtifactID
	RevisionID            domain.MemoryArtifactRevisionID
	RevisionNumber        uint64
	Kind                  domain.MemoryArtifactKind
	Maturity              domain.MaturityState
	Summary               string
	CreatedFromCorrection bool
	ScopeRepositoryID     *domain.RepositoryID
	CreatedAt             time.Time
}

// MemoryArtifactListView is one page of project memory.
type MemoryArtifactListView struct {
	Artifacts []MemoryArtifactSummaryView
}

// MemoryArtifactDetailQuery reads one artifact's newest revision.
//
// The project travels with the artifact identity so the read can prove the
// artifact belongs to the project the caller named, rather than returning
// whatever artifact happens to carry that identifier.
type MemoryArtifactDetailQuery struct {
	ProjectID  domain.ProjectID
	ArtifactID domain.MemoryArtifactID
}

// MemoryArtifactField is one labelled line of an artifact's typed content.
type MemoryArtifactField struct {
	Label string
	Value string
}

// MemoryArtifactDetailView is one artifact at its newest revision, flattened
// into lines a reader can scan, plus the lineage that explains its origin.
type MemoryArtifactDetailView struct {
	Summary              MemoryArtifactSummaryView
	Fields               []MemoryArtifactField
	Lineage              domain.MemoryArtifactLineage
	SupersedesRevisionID *domain.MemoryArtifactRevisionID
	BindingState         string
	ContentSHA256        string
}

// MemoryQueryApplication is the coordinator-side read the service depends on.
type MemoryQueryApplication interface {
	ListMemoryArtifacts(context.Context, MemoryArtifactListQuery) (MemoryArtifactListView, error)
	GetMemoryArtifact(context.Context, MemoryArtifactDetailQuery) (MemoryArtifactDetailView, error)
}

// MemoryService serves the memory inspector.
//
// It is deliberately read-only. Memory artifacts are written by the
// coordinator from evidence it observed, and maturity advances through
// governed transitions; exposing a write here would let the browser launder an
// unevidenced assertion into a durable fact the agent later treats as true.
type MemoryService struct {
	codefluxv1.UnimplementedMemoryServiceServer
	application MemoryQueryApplication
}

// NewMemoryService binds the service to a coordinator read.
func NewMemoryService(application MemoryQueryApplication) (*MemoryService, error) {
	if application == nil {
		return nil, errors.New("memory query application is required")
	}
	return &MemoryService{application: application}, nil
}

// ListMemoryArtifacts returns the newest revision of every live artifact the
// named project has learned, newest first.
func (service *MemoryService) ListMemoryArtifacts(
	ctx context.Context,
	request *codefluxv1.ListMemoryArtifactsRequest,
) (*codefluxv1.ListMemoryArtifactsResponse, error) {
	if request == nil {
		return nil, &RequestValidationError{Field: "request", Reason: "is required"}
	}
	projectID, err := ProjectIDFromProto(request.GetProjectId())
	if err != nil {
		return nil, err
	}
	query := MemoryArtifactListQuery{ProjectID: projectID, Limit: int(request.GetPage().GetLimit())}
	view, err := service.application.ListMemoryArtifacts(ctx, query)
	if err != nil {
		return nil, mapMemoryQueryError(err)
	}
	artifacts := make([]*codefluxv1.MemoryArtifactSummary, 0, len(view.Artifacts))
	for _, item := range view.Artifacts {
		converted, conversionErr := memoryArtifactSummaryToProto(item)
		if conversionErr != nil {
			return nil, conversionErr
		}
		artifacts = append(artifacts, converted)
	}
	return &codefluxv1.ListMemoryArtifactsResponse{
		Artifacts: artifacts,
		Page:      &codefluxv1.PageInfo{},
	}, nil
}

// GetMemoryArtifact returns one artifact's newest revision, rendered as
// labelled lines, together with the lineage that explains where it came from.
func (service *MemoryService) GetMemoryArtifact(
	ctx context.Context,
	request *codefluxv1.GetMemoryArtifactRequest,
) (*codefluxv1.GetMemoryArtifactResponse, error) {
	if request == nil {
		return nil, &RequestValidationError{Field: "request", Reason: "is required"}
	}
	projectID, err := ProjectIDFromProto(request.GetProjectId())
	if err != nil {
		return nil, err
	}
	artifactID, err := MemoryArtifactIDFromProto(request.GetArtifactId())
	if err != nil {
		return nil, err
	}
	view, err := service.application.GetMemoryArtifact(ctx, MemoryArtifactDetailQuery{
		ProjectID: projectID, ArtifactID: artifactID,
	})
	if err != nil {
		return nil, mapMemoryQueryError(err)
	}
	summary, err := memoryArtifactSummaryToProto(view.Summary)
	if err != nil {
		return nil, err
	}
	fields := make([]*codefluxv1.MemoryArtifactField, 0, len(view.Fields))
	for _, field := range view.Fields {
		fields = append(fields, &codefluxv1.MemoryArtifactField{
			Label: field.Label, Value: redactedGraphText(field.Value),
		})
	}
	lineage, err := memoryArtifactLineageToProto(view.Lineage)
	if err != nil {
		return nil, err
	}
	response := &codefluxv1.GetMemoryArtifactResponse{
		Summary: summary, Fields: fields, Lineage: lineage,
		BindingState: view.BindingState, ContentSha256: view.ContentSHA256,
	}
	if view.SupersedesRevisionID != nil {
		response.SupersedesRevisionId, err = MemoryArtifactRevisionIDToProto(*view.SupersedesRevisionID)
		if err != nil {
			return nil, err
		}
	}
	return response, nil
}

func memoryArtifactLineageToProto(lineage domain.MemoryArtifactLineage) (*codefluxv1.MemoryArtifactLineageView, error) {
	result := &codefluxv1.MemoryArtifactLineageView{
		OriginKnown:                     lineage.OriginKnown,
		OriginUnknownReason:             lineage.OriginUnknownReason,
		SupportingEpisodesKnown:         lineage.SupportingEpisodesKnown,
		SupportingEpisodesUnknownReason: lineage.SupportingEpisodesUnknownReason,
	}
	var err error
	if result.DerivedFrom, err = memoryArtifactIdentitiesToProto(lineage.DerivedFrom); err != nil {
		return nil, err
	}
	if result.InfluencedBy, err = memoryArtifactIdentitiesToProto(lineage.InfluencedBy); err != nil {
		return nil, err
	}
	if lineage.OriginKnown && !lineage.OriginArtifactID.IsZero() {
		if result.OriginArtifactId, err = MemoryArtifactIDToProto(lineage.OriginArtifactID); err != nil {
			return nil, err
		}
	}
	for _, episode := range lineage.SupportingEpisodes {
		converted, conversionErr := EpisodeIDToProto(episode)
		if conversionErr != nil {
			return nil, conversionErr
		}
		result.SupportingEpisodes = append(result.SupportingEpisodes, converted)
	}
	return result, nil
}

func memoryArtifactIdentitiesToProto(values []domain.MemoryArtifactID) ([]*codefluxv1.StableIdentity, error) {
	result := make([]*codefluxv1.StableIdentity, 0, len(values))
	for _, value := range values {
		converted, err := MemoryArtifactIDToProto(value)
		if err != nil {
			return nil, err
		}
		result = append(result, converted)
	}
	return result, nil
}

func mapMemoryQueryError(err error) error {
	switch {
	case errors.Is(err, ErrMemoryQueryInvalid):
		return &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT,
			SafeMessage: "The memory query is invalid.",
		}
	case errors.Is(err, ErrMemoryArtifactNotFound):
		return &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_NOT_FOUND,
			SafeMessage: "That memory artifact is not in this project.",
		}
	default:
		return err
	}
}

func memoryArtifactSummaryToProto(view MemoryArtifactSummaryView) (*codefluxv1.MemoryArtifactSummary, error) {
	artifactID, err := MemoryArtifactIDToProto(view.ArtifactID)
	if err != nil {
		return nil, err
	}
	revisionID, err := MemoryArtifactRevisionIDToProto(view.RevisionID)
	if err != nil {
		return nil, err
	}
	summary := &codefluxv1.MemoryArtifactSummary{
		ArtifactId:            artifactID,
		RevisionId:            revisionID,
		Kind:                  string(view.Kind),
		Maturity:              string(view.Maturity),
		Summary:               redactedGraphText(view.Summary),
		CreatedFromCorrection: view.CreatedFromCorrection,
		CreatedAt:             timestamppb.New(view.CreatedAt),
		RevisionNumber:        view.RevisionNumber,
	}
	if view.ScopeRepositoryID != nil {
		summary.ScopeRepositoryId, err = RepositoryIDToProto(*view.ScopeRepositoryID)
		if err != nil {
			return nil, err
		}
	}
	return summary, nil
}

// SummarizeMemoryArtifactContent renders one artifact as a single sentence a
// reader can judge without opening it.
//
// Each kind gets the field that actually distinguishes one artifact of that
// kind from another — the statement, the command, the source file — rather
// than a uniform label, because a list of eight rows all reading "repository
// fact" tells the reader nothing about what the agent believes.
func SummarizeMemoryArtifactContent(content domain.MemoryArtifactContent) string {
	switch {
	case content.RepositoryFact != nil:
		return firstNonEmptyMemoryText(content.RepositoryFact.Statement, "Repository fact")
	case content.ReviewedCommand != nil:
		command := strings.TrimSpace(strings.Join(content.ReviewedCommand.Argv, " "))
		return firstNonEmptyMemoryText(command, "Reviewed command")
	case content.FileToTestMapping != nil:
		source := strings.TrimSpace(content.FileToTestMapping.SourcePath)
		if source == "" {
			return "File-to-test mapping"
		}
		return fmt.Sprintf("%s is covered by %s", source, memoryCountPhrase(len(content.FileToTestMapping.TestPaths), "test"))
	case content.RepositoryConvention != nil:
		return firstNonEmptyMemoryText(content.RepositoryConvention.Statement, "Repository convention")
	case content.AcceptedRegressionCase != nil:
		return firstNonEmptyMemoryText(content.AcceptedRegressionCase.ExpectedOutcome, "Accepted regression case")
	case content.ExecutionRecipe != nil:
		return firstNonEmptyMemoryText(content.ExecutionRecipe.PlanSummary, "Execution recipe")
	case content.AtomReference != nil:
		return firstNonEmptyMemoryText(content.AtomReference.CanonicalName, "Executable atom reference")
	case content.ObservationHypothesis != nil:
		return firstNonEmptyMemoryText(content.ObservationHypothesis.Statement, "Observation")
	default:
		return "Memory artifact"
	}
}

// DescribeMemoryArtifactContent flattens one artifact's typed payload into
// labelled lines.
//
// Flattening here rather than in the browser keeps the eight content shapes in
// one place: the interface renders label/value pairs and never has to know
// which fields a reviewed command carries versus a regression case. Labels are
// written for a reader, not derived from field names.
func DescribeMemoryArtifactContent(content domain.MemoryArtifactContent) []MemoryArtifactField {
	switch {
	case content.RepositoryFact != nil:
		value := content.RepositoryFact
		return appendMemoryFields(nil,
			"Statement", value.Statement,
			"Category", string(value.Category),
			"Repository", value.Repository.String(),
		)
	case content.ReviewedCommand != nil:
		value := content.ReviewedCommand
		return appendMemoryFields(nil,
			"Command", strings.Join(value.Argv, " "),
			"Purpose", string(value.Purpose),
			"Working directory", value.WorkingDirectory,
			"Repository", value.Repository.String(),
		)
	case content.FileToTestMapping != nil:
		value := content.FileToTestMapping
		return appendMemoryFields(nil,
			"Source file", value.SourcePath,
			"Covering tests", strings.Join(value.TestPaths, "\n"),
			"Repository", value.Repository.String(),
		)
	case content.RepositoryConvention != nil:
		value := content.RepositoryConvention
		return appendMemoryFields(nil,
			"Convention", value.Statement,
			"Applies to", value.Scope,
			"Repository", value.Repository.String(),
		)
	case content.AcceptedRegressionCase != nil:
		value := content.AcceptedRegressionCase
		return appendMemoryFields(nil,
			"Expected outcome", value.ExpectedOutcome,
			"Classification", string(value.Classification),
			"Reproducible input", value.ReproducibleInput,
			"Oracle", value.Oracle,
			"Demonstrated failure", value.DemonstratedFailure.String(),
			"Repository", value.Repository.String(),
		)
	case content.ExecutionRecipe != nil:
		value := content.ExecutionRecipe
		return appendMemoryFields(nil,
			"Plan", value.PlanSummary,
			"Applies when", value.ApplicabilityStatement,
			"Required commands", strings.Join(value.RequiredCommands, "\n"),
			"Expected failure signals", strings.Join(value.ExpectedFailureSignals, "\n"),
			"Required validation profile", value.RequiredValidationProfile,
			"Repository", value.Repository.String(),
		)
	case content.AtomReference != nil:
		value := content.AtomReference
		return appendMemoryFields(nil,
			"Canonical name", value.CanonicalName,
			"Atom", value.Atom.String(),
			"Repository", value.Repository.String(),
		)
	case content.ObservationHypothesis != nil:
		value := content.ObservationHypothesis
		return appendMemoryFields(nil,
			"Observation", value.Statement,
			"Evidence strength", string(value.Strength),
			"Repository", value.Repository.String(),
		)
	default:
		return nil
	}
}

// appendMemoryFields adds label/value pairs, dropping any whose value is
// empty: a blank line labelled "Working directory" tells the reader nothing
// and reads like a rendering failure.
func appendMemoryFields(fields []MemoryArtifactField, pairs ...string) []MemoryArtifactField {
	for index := 0; index+1 < len(pairs); index += 2 {
		value := strings.TrimSpace(pairs[index+1])
		if value == "" {
			continue
		}
		fields = append(fields, MemoryArtifactField{Label: pairs[index], Value: value})
	}
	return fields
}

func firstNonEmptyMemoryText(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

func memoryCountPhrase(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}
