package main

import (
	"errors"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/shell"
)

var (
	errMemoryResourceMalformed        = errors.New("authoritative memory response is malformed")
	errMountedMemoryScopeUnavailable  = errors.New("project memory scope is unavailable")
	errMountedMemoryBridgeUnavailable = errors.New("project memory bridge is unavailable")
)

// decodeMemoryArtifactRows converts a coordinator memory listing into list
// rows.
//
// A row whose identity or kind the browser cannot parse is refused outright
// rather than shown with blanks: a memory artifact is a claim the agent acts
// on, and a row that misstates which artifact it is would send a correction to
// the wrong fact.
func decodeMemoryArtifactRows(response *codefluxv1.ListMemoryArtifactsResponse) ([]shell.MemoryArtifactRow, error) {
	if response == nil {
		return nil, errMemoryResourceMalformed
	}
	rows := make([]shell.MemoryArtifactRow, 0, len(response.GetArtifacts()))
	for _, summary := range response.GetArtifacts() {
		row, err := decodeMemoryArtifactRow(summary)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func decodeMemoryArtifactRow(summary *codefluxv1.MemoryArtifactSummary) (shell.MemoryArtifactRow, error) {
	if summary == nil {
		return shell.MemoryArtifactRow{}, errMemoryResourceMalformed
	}
	artifactID := summary.GetArtifactId()
	revisionID := summary.GetRevisionId()
	if artifactID.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MEMORY_ARTIFACT ||
		revisionID.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MEMORY_ARTIFACT_REVISION {
		return shell.MemoryArtifactRow{}, errMemoryResourceMalformed
	}
	kind := domain.MemoryArtifactKind(summary.GetKind())
	maturity := domain.MaturityState(summary.GetMaturity())
	if !kind.IsValid() || !maturity.IsValid() {
		return shell.MemoryArtifactRow{}, errMemoryResourceMalformed
	}
	row := shell.MemoryArtifactRow{
		ArtifactID:            artifactID.GetValue(),
		RevisionID:            revisionID.GetValue(),
		Kind:                  kind,
		Maturity:              maturity,
		Summary:               summary.GetSummary().GetValue(),
		ScopeRepository:       summary.GetScopeRepositoryId().GetValue(),
		RevisionNumber:        summary.GetRevisionNumber(),
		CreatedFromCorrection: summary.GetCreatedFromCorrection(),
	}
	if stamp := summary.GetCreatedAt(); stamp != nil {
		row.CreatedAt = stamp.AsTime()
	}
	return row, nil
}

// decodeMemoryArtifactDetail converts one artifact read into the detail the
// inspector renders.
func decodeMemoryArtifactDetail(response *codefluxv1.GetMemoryArtifactResponse) (shell.MemoryArtifactDetail, error) {
	if response == nil {
		return shell.MemoryArtifactDetail{}, errMemoryResourceMalformed
	}
	row, err := decodeMemoryArtifactRow(response.GetSummary())
	if err != nil {
		return shell.MemoryArtifactDetail{}, err
	}
	detail := shell.MemoryArtifactDetail{
		Row:                  row,
		SupersedesRevisionID: response.GetSupersedesRevisionId().GetValue(),
		BindingState:         response.GetBindingState(),
		ContentDigest:        response.GetContentSha256(),
	}
	for _, field := range response.GetFields() {
		detail.Fields = append(detail.Fields, shell.MemoryDetailField{
			Label: field.GetLabel(), Value: field.GetValue().GetValue(),
		})
	}
	lineage := response.GetLineage()
	detail.OriginKnown = lineage.GetOriginKnown()
	detail.OriginArtifactID = lineage.GetOriginArtifactId().GetValue()
	detail.OriginUnknownReason = lineage.GetOriginUnknownReason()
	detail.SupportingEpisodesKnown = lineage.GetSupportingEpisodesKnown()
	detail.SupportingEpisodesUnknownReason = lineage.GetSupportingEpisodesUnknownReason()
	detail.DerivedFrom = memoryIdentityValues(lineage.GetDerivedFrom())
	detail.InfluencedBy = memoryIdentityValues(lineage.GetInfluencedBy())
	detail.SupportingEpisodes = memoryIdentityValues(lineage.GetSupportingEpisodes())
	return detail, nil
}

func memoryIdentityValues(values []*codefluxv1.StableIdentity) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.GetValue())
	}
	return result
}
