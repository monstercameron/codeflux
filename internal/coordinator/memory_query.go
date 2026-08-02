package coordinator

import (
	"context"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// MemoryArtifactReader is the storage read the memory query depends on.
type MemoryArtifactReader interface {
	ListLatestMemoryArtifactRevisionsForProject(
		ctx context.Context,
		projectID domain.ProjectID,
		limit int,
	) ([]storage.MemoryArtifactRevisionRecord, error)
	GetLatestMemoryArtifactRevision(
		ctx context.Context,
		artifactID domain.MemoryArtifactID,
	) (storage.MemoryArtifactRevisionRecord, error)
	GetMemoryArtifactLineage(
		ctx context.Context,
		artifactID domain.MemoryArtifactID,
	) (domain.MemoryArtifactLineage, error)
	GetMemoryArtifactProject(
		ctx context.Context,
		artifactID domain.MemoryArtifactID,
	) (domain.ProjectID, bool, error)
}

// MemoryQueryService answers the memory inspector's read.
//
// It reduces each stored revision to a listable summary here rather than in
// the transport layer, so the coordinator stays the only place that decides
// which part of a memory artifact is safe to show without opening it.
type MemoryQueryService struct {
	repositories MemoryArtifactReader
}

// NewMemoryQueryService binds the query to storage.
func NewMemoryQueryService(repositories MemoryArtifactReader) (*MemoryQueryService, error) {
	if repositories == nil {
		return nil, errors.New("memory query repositories are required")
	}
	return &MemoryQueryService{repositories: repositories}, nil
}

// ListMemoryArtifacts returns the newest revision of every live artifact in
// one project, newest first.
func (service *MemoryQueryService) ListMemoryArtifacts(
	ctx context.Context,
	query transport.MemoryArtifactListQuery,
) (transport.MemoryArtifactListView, error) {
	if query.ProjectID.IsZero() {
		return transport.MemoryArtifactListView{}, fmt.Errorf("%w: project is required", transport.ErrMemoryQueryInvalid)
	}
	records, err := service.repositories.ListLatestMemoryArtifactRevisionsForProject(ctx, query.ProjectID, query.Limit)
	if err != nil {
		return transport.MemoryArtifactListView{}, err
	}
	artifacts := make([]transport.MemoryArtifactSummaryView, 0, len(records))
	for _, record := range records {
		artifacts = append(artifacts, transport.MemoryArtifactSummaryView{
			ArtifactID:            record.ArtifactID,
			RevisionID:            record.RevisionID,
			RevisionNumber:        record.RevisionNumber,
			Kind:                  record.Kind,
			Maturity:              record.Maturity,
			Summary:               transport.SummarizeMemoryArtifactContent(record.Content),
			CreatedFromCorrection: record.CreatedFromCorrection,
			ScopeRepositoryID:     record.ScopeRepositoryID,
			CreatedAt:             record.CreatedAt,
		})
	}
	return transport.MemoryArtifactListView{Artifacts: artifacts}, nil
}

// GetMemoryArtifact returns one artifact's newest revision and its lineage.
//
// The project is checked before anything is read, and a mismatch is reported
// as "not found" rather than "not yours": telling a caller that an artifact
// exists in a project it may not read is itself a disclosure.
func (service *MemoryQueryService) GetMemoryArtifact(
	ctx context.Context,
	query transport.MemoryArtifactDetailQuery,
) (transport.MemoryArtifactDetailView, error) {
	if query.ProjectID.IsZero() || query.ArtifactID.IsZero() {
		return transport.MemoryArtifactDetailView{}, fmt.Errorf("%w: project and artifact are required", transport.ErrMemoryQueryInvalid)
	}
	owner, found, err := service.repositories.GetMemoryArtifactProject(ctx, query.ArtifactID)
	if err != nil {
		return transport.MemoryArtifactDetailView{}, err
	}
	if !found || owner != query.ProjectID {
		return transport.MemoryArtifactDetailView{}, fmt.Errorf("%w: artifact is not in this project", transport.ErrMemoryArtifactNotFound)
	}
	record, err := service.repositories.GetLatestMemoryArtifactRevision(ctx, query.ArtifactID)
	if err != nil {
		return transport.MemoryArtifactDetailView{}, err
	}
	lineage, err := service.repositories.GetMemoryArtifactLineage(ctx, query.ArtifactID)
	if err != nil {
		return transport.MemoryArtifactDetailView{}, err
	}
	view := transport.MemoryArtifactDetailView{
		Summary: transport.MemoryArtifactSummaryView{
			ArtifactID:            record.ArtifactID,
			RevisionID:            record.RevisionID,
			RevisionNumber:        record.RevisionNumber,
			Kind:                  record.Kind,
			Maturity:              record.Maturity,
			Summary:               transport.SummarizeMemoryArtifactContent(record.Content),
			CreatedFromCorrection: record.CreatedFromCorrection,
			ScopeRepositoryID:     record.ScopeRepositoryID,
			CreatedAt:             record.CreatedAt,
		},
		Fields:               transport.DescribeMemoryArtifactContent(record.Content),
		Lineage:              lineage,
		SupersedesRevisionID: record.SupersedesRevisionID,
		BindingState:         memoryBindingState(record.Binding),
		ContentSHA256:        record.ContentSHA256,
	}
	return view, nil
}

// memoryBindingState says in one phrase how firmly this artifact is pinned to
// a repository revision. An unknown binding reports its reason, because "we
// don't know which revision this was true of" is a materially different claim
// from "it is true of every revision".
func memoryBindingState(binding *storage.MemoryRevisionBinding) string {
	if binding == nil {
		return ""
	}
	if !binding.Known {
		if binding.UnknownReason != "" {
			return "Revision binding unknown: " + binding.UnknownReason
		}
		return "Revision binding unknown"
	}
	if binding.ExactRevision != "" {
		return "Bound to revision " + binding.ExactRevision
	}
	if binding.ValidFromRevision != "" && binding.ValidUntilRevision != "" {
		return "Valid from " + binding.ValidFromRevision + " until " + binding.ValidUntilRevision
	}
	if binding.ValidFromRevision != "" {
		return "Valid from revision " + binding.ValidFromRevision
	}
	if binding.ValidUntilRevision != "" {
		return "Valid until revision " + binding.ValidUntilRevision
	}
	return "Revision binding known"
}
